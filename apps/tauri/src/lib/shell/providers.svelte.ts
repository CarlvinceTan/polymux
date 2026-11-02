import type { Provider, ProviderModel, ProviderOption } from '../api/contract';
import type { Session } from '../session.svelte';

export type ProviderGroup = { id: string; name: string; connections: Provider[]; models: ProviderModel[] };
const presets: Record<string, string> = { ollama: 'Ollama', lmstudio: 'LM Studio', elevenlabs: 'ElevenLabs', custom: 'Custom' };

export class ProviderStore {
  providers = $state.raw<Provider[] | undefined>();
  options = $state.raw<ProviderOption[] | undefined>();
  issue = $state('');
  pending = $state(0);
  private revision = 0;
  private loading?: Promise<void>;

  constructor(readonly session: Session) {
    try {
      const saved = JSON.parse(localStorage.getItem(this.key) ?? '{}');
      this.providers = saved.providers;
      this.options = saved.options;
    } catch { /* A failed cache does not prevent using the server. */ }
  }

  private get key() { return `polymux.models.${this.session.address}`; }

  private persist() {
    try {
      const saved = JSON.parse(localStorage.getItem(this.key) ?? '{}');
      const providers = this.providers?.map(({ id, type, name, enabled, order, models, status, keyHint }) =>
        ({ id, type, name, enabled, order, models, status, keyHint, baseURL: '' }));
      localStorage.setItem(this.key, JSON.stringify({ ...saved, providers, options: this.options }));
    } catch { /* The in-memory state remains available. */ }
  }

  async refresh(): Promise<void> {
    if (this.loading) {
      await this.loading;
      if (!this.pending) return this.refresh();
      return;
    }
    if (this.pending) return;
    const revision = this.revision;
    this.loading = (async () => {
      const [providers, options] = await Promise.allSettled([this.session.client.providers(), this.session.client.providerOptions()]);
      if (options.status === 'fulfilled') this.options = options.value;
      if (revision === this.revision && !this.pending && providers.status === 'fulfilled') {
        this.providers = providers.value;
        this.issue = '';
      }
      this.persist();
    })();
    try { await this.loading; } finally { this.loading = undefined; }
  }

  begin() { this.pending++; this.revision++; }
  end() { this.pending--; this.revision++; }
  accept(providers: Provider[]) { this.revision++; this.providers = providers; this.persist(); }
  merge(updates: Provider[]) {
    const list = [...(this.providers ?? [])];
    for (const update of updates) {
      const index = list.findIndex((item) => item.id === update.id);
      if (index < 0) list.push(update); else list[index] = update;
    }
    this.accept(list);
  }
  remove(id: string) { this.accept((this.providers ?? []).filter((item) => item.id !== id)); }

  get groups(): ProviderGroup[] {
    const names = Object.fromEntries((this.options ?? []).map((option) => [option.type, option.name]));
    const groups: ProviderGroup[] = [];
    for (const provider of this.providers ?? []) {
      let group = groups.find((item) => item.id === provider.type);
      if (!group) {
        group = { id: provider.type, name: names[provider.type] ?? presets[provider.type] ?? provider.name, connections: [], models: [] };
        groups.push(group);
      }
      group.connections.push(provider);
      for (const model of provider.models) {
        const existing = group.models.find((item) => item.id === model.id);
        const enabled = provider.enabled && model.enabled;
        if (existing) existing.enabled ||= enabled;
        else group.models.push({ ...model, enabled });
      }
    }
    return groups;
  }
}
