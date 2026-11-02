import type { Agent, AvatarDesign } from '../api/contract';
import { isOffline, isTransient } from '../api/errors';

type Client = { updateAvatar(id: string, design: AvatarDesign): Promise<Agent> };
type Listener = (design: AvatarDesign, issue: string) => void;
type Apply = (design: AvatarDesign) => void;
type Saved = { saved: AvatarDesign; draft: AvatarDesign };

const stores = new Map<string, AvatarEdit>();
const copy = (value: AvatarDesign): AvatarDesign => JSON.parse(JSON.stringify(value));
const same = (left: AvatarDesign, right: AvatarDesign): boolean => JSON.stringify(left) === JSON.stringify(right);
const keyFor = (address: string, id: string): string => `polymux.avatar.${JSON.stringify([address, id])}`;

export const defaultAvatar: AvatarDesign = {
  shape: 'cercle', color: 'encre', expression: 'neutre', followCursor: true,
  animates: true, playSequence: false, blocks: [],
};

export function avatarEdit(address: string, agent: Agent, client: Client, apply: Apply): AvatarEdit {
  const key = keyFor(address, agent.id);
  let store = stores.get(key);
  if (!store) { store = new AvatarEdit(key, agent, client, apply); stores.set(key, store); }
  else store.use(agent, client, apply);
  return store;
}

/** Keep unsaved designs visible across server refreshes. */
export function overlayAvatars(address: string, agents: Agent[]): Agent[] {
  return agents.map((agent) => {
    const pending = stores.get(keyFor(address, agent.id))?.pending;
    return pending ? { ...agent, avatar: pending } : agent;
  });
}

/** Resume edits that were still pending when the app closed. */
export function restoreAvatars(address: string, agents: Agent[], client: Client, apply: (id: string, avatar: AvatarDesign) => void): Agent[] {
  for (const agent of agents) {
    try {
      if (localStorage.getItem(keyFor(address, agent.id))) avatarEdit(address, agent, client, (avatar) => apply(agent.id, avatar)).resume();
    } catch { /* Keep loading when local storage is unavailable. */ }
  }
  return overlayAvatars(address, agents);
}

export function stopAvatars(address: string, agents: Agent[]): void {
  for (const agent of agents) stores.get(keyFor(address, agent.id))?.stop();
}

export class AvatarEdit {
  private saved: AvatarDesign;
  private draft: AvatarDesign;
  private listeners = new Set<Listener>();
  private timer?: ReturnType<typeof setTimeout>;
  private saving = false;
  private active = true;
  private issue = '';
  private id: string;

  constructor(private key: string, agent: Agent, private client: Client, private apply: Apply) {
    this.id = agent.id;
    this.saved = copy(agent.avatar ?? defaultAvatar);
    this.draft = copy(this.saved);
    try {
      const value = JSON.parse(localStorage.getItem(key) ?? 'null') as Saved | null;
      if (value?.saved?.shape && value.draft?.shape && !same(value.saved, value.draft)) {
        this.saved = value.saved;
        this.draft = value.draft;
      }
    } catch { /* Keep the editor usable without cached data. */ }
  }

  use(agent: Agent, client: Client, apply: Apply): void {
    this.client = client;
    this.apply = apply;
    if (!this.dirty && !this.saving) this.saved = this.draft = copy(agent.avatar ?? defaultAvatar);
  }

  connect(listener: Listener): () => void {
    this.active = true;
    this.listeners.add(listener);
    if (this.dirty) { this.apply(copy(this.draft)); this.schedule(600); }
    this.notify();
    return () => {
      this.listeners.delete(listener);
      if (!this.listeners.size) { clearTimeout(this.timer); this.timer = undefined; void this.save(); }
    };
  }

  edit(value: AvatarDesign): void {
    if (same(this.draft, value)) return;
    this.draft = copy(value);
    this.issue = '';
    this.persist();
    this.apply(copy(this.draft));
    this.notify();
    this.schedule(600);
  }

  private get dirty(): boolean { return !same(this.saved, this.draft); }
  get value(): AvatarDesign { return copy(this.draft); }
  get pending(): AvatarDesign | undefined { return this.dirty || this.saving ? copy(this.draft) : undefined; }
  resume(): void { this.active = true; if (this.dirty) this.schedule(600); }
  stop(): void { this.active = false; clearTimeout(this.timer); this.timer = undefined; }

  private schedule(delay: number): void {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => { this.timer = undefined; void this.save(); }, delay);
  }

  private async save(): Promise<void> {
    if (!this.dirty || this.saving) return;
    this.saving = true;
    const sent = copy(this.draft);
    let again = false;
    try {
      const accepted = await this.client.updateAvatar(this.id, sent);
      this.saved = copy(accepted.avatar ?? sent);
      if (same(this.draft, sent)) this.draft = copy(this.saved);
      this.issue = '';
      this.persist();
      this.apply(copy(this.draft));
      this.notify();
      again = this.dirty;
    } catch (error) {
      this.issue = isOffline(error) ? 'Not saved yet.' : 'Couldn’t save the avatar.';
      this.persist();
      this.notify();
      if (this.active && isTransient(error)) this.schedule(5000);
    } finally {
      this.saving = false;
      if (this.active && again) this.schedule(this.listeners.size ? 600 : 0);
    }
  }

  private persist(): void {
    try {
      if (this.dirty) localStorage.setItem(this.key, JSON.stringify({ saved: this.saved, draft: this.draft } satisfies Saved));
      else localStorage.removeItem(this.key);
    }
    catch { /* Keep the edit in memory. */ }
  }

  private notify(): void {
    for (const listener of this.listeners) listener(copy(this.draft), this.issue);
  }
}
