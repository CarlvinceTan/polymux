<script lang="ts">
  import type { Provider, Settings, SettingsPatch } from '../api/contract';
  import type { Session } from '../session.svelte';
  import type { ProviderStore } from './providers.svelte';
  import ProviderList from './ProviderList.svelte';
  import VoiceSettings from './VoiceSettings.svelte';

  let { session, store, onProvider }: { session: Session; store: ProviderStore; onProvider: (type?: string) => void } = $props();
  type Defaults = Pick<Settings, 'defaultProvider' | 'defaultModel' | 'defaultReasoning'>;
  type Group = { id: string; name: string; connections: Provider[] };
  type Choice = { id: string; provider: string; model: string; name: string; group: string };
  const presets: Record<string, string> = { ollama: 'Ollama', lmstudio: 'LM Studio', elevenlabs: 'ElevenLabs', custom: 'Custom' };
  const reasoning = ['off', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const;
  function readCache(): { defaults?: Defaults; providers?: Provider[]; names?: Record<string, string> } {
    try { return JSON.parse(localStorage.getItem(`polymux.models.${session.address}`) ?? '{}'); }
    catch { return {}; }
  }
  const cached = readCache();
  let defaults = $state<Defaults | undefined>(cached.defaults);
  const providers = $derived(store.providers ?? []);
  const names = $derived(Object.fromEntries((store.options ?? []).map((option) => [option.type, option.name])));
  let open = $state(false);
  let query = $state('');
  let highlighted = $state<string | null>(null);
  let busy = $state(false);
  let issue = $state('');
  let search = $state<HTMLInputElement>();
  let picker = $state<HTMLDivElement>();
  let reasoningOpen = $state(false);
  let reasoningRow = $state<HTMLDivElement>();
  let reasoningTrigger = $state<HTMLButtonElement>();
  let reasoningMenu = $state<HTMLDivElement>();
  let revision = 0;

  function cache() {
    try {
      const saved = JSON.parse(localStorage.getItem(`polymux.models.${session.address}`) ?? '{}');
      localStorage.setItem(`polymux.models.${session.address}`, JSON.stringify({ ...saved, defaults }));
    } catch { /* Keep the state in memory. */ }
  }

  $effect(() => {
    // Re-read after the connection returns, without replacing an in-flight edit.
    session.online;
    const current = revision;
    void session.client.settings().then((value) => {
      if (current !== revision || busy) return;
      defaults = { defaultProvider: value.defaultProvider, defaultModel: value.defaultModel, defaultReasoning: value.defaultReasoning };
      cache();
    }).catch(() => undefined);
    void store.refresh();
  });

  $effect(() => { if (open && search) search.focus(); });
  $effect(() => {
    if (reasoningOpen && reasoningMenu) reasoningMenu.querySelector<HTMLButtonElement>('[aria-checked="true"]')?.focus();
  });

  function format(value: string) {
    return value.replace(/(?<=\d)-(?=\d)/g, '.').replaceAll('-', ' ').trim().split(/\s+/).map((word) => word.charAt(0).toUpperCase() + word.slice(1)).join(' ');
  }

  function groupProviders(list: Provider[]): Group[] {
    const groups: Group[] = [];
    for (const provider of list) {
      let group = groups.find((item) => item.id === provider.type);
      if (!group) {
        group = { id: provider.type, name: names[provider.type] ?? presets[provider.type] ?? provider.name, connections: [] };
        groups.push(group);
      }
      group.connections.push(provider);
    }
    return groups;
  }

  const groups = $derived(groupProviders(providers.filter((provider) => provider.enabled && (provider.type in presets || !!provider.status))));
  const choices = $derived.by(() => {
    const terms = query.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
    const result: Choice[] = [];
    for (const group of groups) {
      const models = new Map<string, { tags: string[]; sources: string[] }>();
      for (const connection of group.connections) {
        for (const model of connection.models) {
          if (!model.enabled) continue;
          const existing = models.get(model.id);
          if (existing) existing.sources.push(connection.id);
          else models.set(model.id, { tags: model.tags, sources: [connection.id] });
        }
      }
      for (const [model, item] of models) {
        const title = `${format(model)} ${format(group.name)}`;
        if ((item.tags.length && !item.tags.includes('text')) || !terms.every((term) => `${group.name} ${model} ${title}`.toLocaleLowerCase().includes(term))) continue;
        result.push({ id: `${group.id}/${model}`, provider: item.sources.includes(defaults?.defaultProvider ?? '') ? defaults!.defaultProvider : item.sources[0], model, name: title, group: group.name });
      }
    }
    return result;
  });
  const selectedName = $derived.by(() => {
    if (!defaults?.defaultModel || !defaults.defaultProvider) return 'Server default';
    const group = groupProviders(providers).find((item) => item.connections.some((connection) => connection.id === defaults?.defaultProvider));
    return `${format(defaults.defaultModel)} ${format(group?.name ?? defaults.defaultProvider)}`;
  });

  async function save(patch: SettingsPatch) {
    if (!defaults || busy) return;
    const previous = { ...defaults };
    defaults = { ...defaults, ...patch };
    revision++;
    busy = true;
    issue = '';
    cache();
    try {
      const value = await session.client.updateSettings(patch);
      defaults = { defaultProvider: value.defaultProvider, defaultModel: value.defaultModel, defaultReasoning: value.defaultReasoning };
    } catch {
      defaults = previous;
      issue = 'Couldn’t save model settings.';
    } finally { busy = false; revision++; cache(); }
  }

  function choose(provider: string, model: string) {
    open = false;
    query = '';
    highlighted = null;
    if (provider !== defaults?.defaultProvider || model !== defaults.defaultModel) void save({ defaultProvider: provider, defaultModel: model });
  }

  function keys(event: KeyboardEvent) {
    const options = [...(!query.trim() ? [{ id: '', provider: '', model: '' }] : []), ...choices];
    if (event.key === 'Escape') { open = false; event.preventDefault(); }
    else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const index = options.findIndex((item) => item.id === highlighted);
      const next = event.key === 'ArrowDown' ? Math.min(index + 1, options.length - 1) : Math.max(index < 0 ? options.length - 1 : index - 1, 0);
      if (options[next]) highlighted = options[next].id;
    } else if (event.key === 'Enter') {
      const selected = options.find((item) => item.id === highlighted) ?? (query.trim() ? options[0] : undefined);
      if (selected) { event.preventDefault(); choose(selected.provider, selected.model); }
    }
  }

  function reasoningKeys(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      reasoningOpen = false;
      reasoningTrigger?.focus();
    } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const buttons = Array.from(reasoningMenu?.querySelectorAll<HTMLButtonElement>('[role="radio"]') ?? []);
      const index = buttons.indexOf(event.currentTarget as HTMLButtonElement);
      const next = event.key === 'ArrowDown' ? Math.min(index + 1, buttons.length - 1) : Math.max(index - 1, 0);
      buttons[next]?.focus();
    }
  }

  function chooseReasoning(value: Settings['defaultReasoning']) {
    reasoningOpen = false;
    reasoningTrigger?.focus();
    if (value !== defaults?.defaultReasoning) void save({ defaultReasoning: value });
  }
</script>

<svelte:document onclick={(event) => {
  if (open && !picker?.contains(event.target as Node)) open = false;
  if (reasoningOpen && !reasoningRow?.contains(event.target as Node)) reasoningOpen = false;
}} />
<section>
  <h2>Models</h2>
  <div class="rows">
  <div class="row model-row" bind:this={picker}>
    <button class="model-button" aria-label="Default model, {selectedName}" aria-expanded={open} disabled={!defaults || busy} onclick={() => { open = !open; reasoningOpen = false; query = ''; highlighted = null; }}>
      <span>Default model</span><span class="value">{selectedName} <span aria-hidden="true">⌃⌄</span></span>
    </button>
    {#if open}
      <div class="picker" style:height="{Math.min(400, Math.max(160, (providers.reduce((total, provider) => total + provider.models.length, 0) + 2) * 24 + 60))}px">
        <div class="search"><svg aria-hidden="true" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="7" cy="7" r="4.5"></circle><path d="m10.5 10.5 3 3"></path></svg><input bind:this={search} bind:value={query} oninput={() => (highlighted = null)} onkeydown={keys} placeholder="Search models" aria-label="Search models" /></div>
        <div class="options" role="radiogroup" aria-label="Models">
          {#if !query.trim()}<button role="radio" class:highlighted={highlighted === ''} aria-checked={!defaults?.defaultModel} onclick={() => choose('', '')}><span class="check" aria-hidden="true">{!defaults?.defaultModel ? '✓' : ''}</span>Not set</button>{/if}
          {#each groups as group (group.id)}
            {@const matches = choices.filter((choice) => choice.group === group.name && choice.id.startsWith(`${group.id}/`))}
            {#if matches.length}
              <div class="heading">{format(group.name)}</div>
              {#each matches as choice (choice.id)}
                {@const selected = defaults?.defaultModel === choice.model && group.connections.some((connection) => connection.id === defaults?.defaultProvider)}
                <button role="radio" class:highlighted={highlighted === choice.id} aria-label={choice.name} aria-checked={selected} onclick={() => choose(choice.provider, choice.model)}><span class="check" aria-hidden="true">{selected ? '✓' : ''}</span>{format(choice.model)}</button>
              {/each}
            {/if}
          {/each}
          {#if !choices.length && (query.trim() || !defaults?.defaultModel)}<div class="empty">No models found.</div>{/if}
          {#if !choices.length && !query.trim() && defaults?.defaultModel}
            <div class="heading">{format(groupProviders(providers).find((group) => group.connections.some((connection) => connection.id === defaults?.defaultProvider))?.name ?? defaults.defaultProvider)}</div>
            <button role="radio" aria-checked="true" onclick={() => choose(defaults!.defaultProvider, defaults!.defaultModel)}><span class="check" aria-hidden="true">✓</span>{format(defaults.defaultModel)}</button>
          {/if}
        </div>
      </div>
    {/if}
  </div>
  <div class="row model-row" bind:this={reasoningRow}>
    <button class="model-button" bind:this={reasoningTrigger} aria-label="Reasoning, {format(defaults?.defaultReasoning ?? 'off')}" aria-expanded={reasoningOpen} disabled={!defaults || busy} onclick={() => { reasoningOpen = !reasoningOpen; open = false; }}>
      <span>Reasoning</span><span class="value">{format(defaults?.defaultReasoning ?? 'off')} <span aria-hidden="true">⌃⌄</span></span>
    </button>
    {#if reasoningOpen}
      <div class="reasoning-menu" role="radiogroup" aria-label="Reasoning options" bind:this={reasoningMenu}>
        {#each reasoning as value (value)}
          <button role="radio" aria-checked={defaults?.defaultReasoning === value} onkeydown={reasoningKeys} onclick={() => chooseReasoning(value)}><span class="check" aria-hidden="true">{defaults?.defaultReasoning === value ? '✓' : ''}</span>{format(value)}</button>
        {/each}
      </div>
    {/if}
  </div>
  </div>
  {#if issue}<p role="status">{issue}</p>{/if}
</section>
<ProviderList {store} {onProvider} />
<VoiceSettings {session} providers={providers} />

<style>
  section { margin-bottom: 22px; }
  h2 { font-size: 13px; font-weight: 600; margin: 0 0 10px; }
  .rows { border-radius: 10px; background: var(--sidebar-bg); }
  .row { display: flex; align-items: center; justify-content: space-between; min-height: 36px; padding: 0 10px; font-size: 13px; }
  .row + .row { border-top: 1px solid var(--hairline); }
  .model-row { position: relative; }
  .model-button { display: flex; align-items: center; justify-content: space-between; gap: 16px; width: 100%; min-width: 0; min-height: 36px; text-align: left; }
  .value { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-muted); }
  .picker { box-sizing: border-box; position: absolute; z-index: 3; top: calc(100% - 8px); right: 0; width: min(280px, 100%); display: flex; flex-direction: column; border: 1px solid var(--hairline); border-radius: 12px; background: var(--window-bg); box-shadow: 0 8px 24px #0003; }
  .search { box-sizing: border-box; flex: none; display: flex; align-items: center; gap: 8px; height: 40px; padding: 0 12px; border-bottom: 1px solid var(--hairline); color: var(--text-muted); }
  input { width: 100%; min-width: 0; padding: 0; border: 0; background: transparent; color: var(--text); font: inherit; outline: none; }
  .options { position: relative; flex: 1; min-height: 0; overflow-y: auto; padding: 4px; }
  .options button { display: flex; align-items: center; width: 100%; min-height: 24px; padding: 0 8px; border-radius: 6px; text-align: left; overflow-wrap: anywhere; }
  .options button:hover, .options button.highlighted { background: var(--row-hover); }
  .reasoning-menu { position: absolute; z-index: 3; top: calc(100% + 4px); right: 0; box-sizing: border-box; width: min(200px, 100%); padding: 4px; border: 1px solid var(--hairline); border-radius: 12px; background: var(--window-bg); box-shadow: 0 8px 24px #0003; }
  .reasoning-menu button { display: flex; align-items: center; width: 100%; min-height: 24px; padding: 0 8px; border-radius: 6px; text-align: left; }
  .reasoning-menu button:hover, .reasoning-menu button:focus-visible { background: var(--row-hover); }
  .check { width: 24px; flex: none; }
  .heading { padding: 8px 8px 4px 32px; font-size: 12px; font-weight: 600; }
  .empty { position: absolute; inset: 0; display: grid; place-items: center; pointer-events: none; }
  p { margin: 8px 2px 0; font-size: 11px; color: var(--text-muted); }
</style>
