import type { Agent, AgentProfile, UpdateAgentRequest } from '../api/contract';
import { isOffline, isTransient } from '../api/errors';

type Client = {
  agentProfile(id: string): Promise<AgentProfile>;
  updateAgent(id: string, patch: UpdateAgentRequest): Promise<AgentProfile>;
};
type Values = { name: string; role: string; prompt: string };
type Field = keyof Values;
type Listener = (values: Values, loaded: boolean, issue: string, accepted?: Agent) => void;
type Saved = { saved: Values; values: Values };

const stores = new Map<string, ProfileEdit>();
const fields: Field[] = ['name', 'role', 'prompt'];
const clean = (values: Values): Values => ({ name: values.name.trim(), role: values.role.trim(), prompt: values.prompt.trim() });
const same = (a: Values, b: Values): boolean => fields.every((field) => a[field] === b[field]);
const profileValues = (profile: AgentProfile): Values => ({ name: profile.agent.name, role: profile.agent.role, prompt: profile.prompt ?? '' });

/** Keeps an edit while the panel closes or a request is still in flight. */
export function profileEdit(address: string, agent: Agent, client: Client): ProfileEdit {
  const key = `polymux.profile.${JSON.stringify([address, agent.id])}`;
  let store = stores.get(key);
  if (!store) { store = new ProfileEdit(key, agent, client); stores.set(key, store); }
  else store.use(client);
  return store;
}

export class ProfileEdit {
  private saved: Values;
  private values: Values;
  private loaded = false;
  private saving = false;
  private version = 0;
  private issue = '';
  private timer?: ReturnType<typeof setTimeout>;
  private watcher?: ReturnType<typeof setInterval>;
  private listeners = new Set<Listener>();
  private id: string;

  constructor(private key: string, agent: Agent, private client: Client) {
    this.id = agent.id;
    this.saved = { name: agent.name, role: agent.role, prompt: '' };
    this.values = { ...this.saved };
    try {
      const raw = localStorage.getItem(key);
      if (raw) {
        const value = JSON.parse(raw);
        if (fields.every((field) => typeof value.saved?.[field] === 'string' && typeof value.values?.[field] === 'string')) {
          this.saved = value.saved;
          this.values = value.values;
          this.loaded = true;
        } else if (typeof value.saved === 'string' && typeof value.prompt === 'string') {
          // Preserve instruction drafts written before name and role were editable.
          this.saved.prompt = value.saved;
          this.values.prompt = value.prompt;
          this.loaded = true;
        }
      }
    } catch { /* Keep the editor usable without cached data. */ }
  }

  use(client: Client): void { this.client = client; }

  connect(listener: Listener): () => void {
    this.listeners.add(listener);
    this.notify();
    if (!this.watcher) {
      void this.refresh();
      this.watcher = setInterval(() => void this.refresh(), 5000);
      if (this.dirty) this.schedule(700);
    }
    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size) return;
      clearInterval(this.watcher);
      this.watcher = undefined;
      clearTimeout(this.timer);
      this.timer = undefined;
      void this.save();
    };
  }

  edit(field: Field, value: string): void {
    if ((field === 'prompt' && !this.loaded) || value === this.values[field]) return;
    this.values = { ...this.values, [field]: value };
    this.issue = '';
    this.persist();
    this.notify();
    this.schedule(700);
  }

  private get dirty(): boolean { return !same(clean(this.values), this.saved); }

  private merge(update: Values, baseline: Values): void {
    const current = clean(this.values);
    for (const field of fields) {
      if (current[field] === baseline[field] && update[field] !== baseline[field]) this.values[field] = update[field];
    }
    this.saved = update;
  }

  private async refresh(): Promise<void> {
    if (this.saving) return;
    const version = this.version;
    try {
      const fresh = await this.client.agentProfile(this.id);
      if (version !== this.version || this.saving) return;
      this.merge(profileValues(fresh), this.saved);
      this.loaded = true;
      this.persist();
      this.notify();
      if (this.dirty && !this.timer) this.schedule(700);
    } catch { /* Keep the cached profile when offline. */ }
  }

  private schedule(delay: number): void {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => { this.timer = undefined; void this.save(); }, delay);
  }

  private async save(): Promise<void> {
    const sent = clean(this.values);
    if (!this.loaded || !sent.name || !this.dirty || this.saving) return;
    this.saving = true;
    this.version++;
    let flush = false;
    try {
      const accepted = await this.client.updateAgent(this.id, sent);
      this.merge(profileValues(accepted), sent);
      this.issue = '';
      this.persist();
      this.notify(accepted.agent);
      if (this.dirty && !this.timer) {
        if (this.listeners.size) this.schedule(700);
        else flush = true;
      }
    } catch (error) {
      this.issue = isOffline(error) ? 'Not saved yet.' : 'Couldn’t save this change.';
      this.persist();
      this.notify();
      if (isTransient(error) && this.listeners.size) this.schedule(5000);
    } finally {
      this.saving = false;
      if (flush) void this.save();
    }
  }

  private persist(): void {
    try { localStorage.setItem(this.key, JSON.stringify({ saved: this.saved, values: this.values } satisfies Saved)); }
    catch { /* Keep the edit in memory. */ }
  }

  private notify(accepted?: Agent): void {
    for (const listener of this.listeners) listener({ ...this.values }, this.loaded, this.issue, accepted);
  }
}
