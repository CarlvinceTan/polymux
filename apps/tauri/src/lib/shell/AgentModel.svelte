<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import type { Agent, Provider, UpdateAgentRequest } from '../api/contract';
  import type { Session } from '../session.svelte';
  import { ProviderStore } from './providers.svelte';

  let { session, agent }: { session: Session; agent: Agent } = $props();
  const store = untrack(() => new ProviderStore(session));
  const levels = ['off', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const;
  type Choice = { provider: string; model: string; name: string; group: string };

  let provider = $state(untrack(() => agent.provider ?? ''));
  let model = $state(untrack(() => agent.model ?? ''));
  let reasoning = $state(untrack(() => agent.reasoning ?? 'off'));
  let saving = $state(false);
  let issue = $state('');
  let open = $state(false);
  let reasoningOpen = $state(false);
  let query = $state('');
  let trigger = $state<HTMLButtonElement>();
  let reasoningTrigger = $state<HTMLButtonElement>();
  let search = $state<HTMLInputElement>();
  let point = $state({ x: 0, y: 0 });
  let reasoningPoint = $state({ x: 0, y: 0 });

  $effect(() => {
    agent.id;
    const nextProvider = agent.provider ?? '';
    const nextModel = agent.model ?? '';
    const nextReasoning = agent.reasoning ?? 'off';
    if (saving) return;
    provider = nextProvider;
    model = nextModel;
    reasoning = nextReasoning;
  });

  onMount(() => { void store.refresh(); });

  $effect(() => {
    if (open && search) search.focus();
  });

  const groups = $derived.by(() => {
    const names = Object.fromEntries((store.options ?? []).map((option) => [option.type, option.name]));
    const result: { id: string; name: string; connections: Provider[]; models: Map<string, { sources: string[]; tags: string[] }> }[] = [];
    for (const connection of store.providers ?? []) {
      if (!connection.enabled) continue;
      let group = result.find((entry) => entry.id === connection.type);
      if (!group) {
        group = { id: connection.type, name: names[connection.type] ?? connection.name, connections: [], models: new Map() };
        result.push(group);
      }
      group.connections.push(connection);
      for (const item of connection.models) {
        if (!item.enabled) continue;
        const existing = group.models.get(item.id);
        if (existing) existing.sources.push(connection.id);
        else group.models.set(item.id, { sources: [connection.id], tags: item.tags });
      }
    }
    return result;
  });

  const choices = $derived.by(() => {
    const terms = query.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
    const result: Choice[] = [];
    for (const group of groups) {
      for (const [id, item] of group.models) {
        if ((item.tags.length && !item.tags.includes('text')) || !terms.every((term) => `${group.name} ${id}`.toLocaleLowerCase().includes(term))) continue;
        result.push({
          provider: item.sources.includes(provider) ? provider : item.sources[0],
          model: id,
          name: `${format(id)} ${format(group.name)}`,
          group: group.name,
        });
      }
    }
    return result;
  });

  const providerName = $derived((store.providers ?? []).find((connection) => connection.id === provider)?.name ?? (store.providers ? 'Unavailable' : ''));
  const unavailable = $derived(!!store.providers && !!provider && !groups.some((group) => group.connections.some((connection) => connection.id === provider)));

  function format(value: string) {
    return value.replace(/(?<=\d)-(?=\d)/g, '.').replaceAll('-', ' ').trim().split(/\s+/).map((word) => word.charAt(0).toUpperCase() + word.slice(1)).join(' ');
  }

  function place(target: HTMLButtonElement | undefined, width: number, height: number) {
    if (!target) return { x: 0, y: 0 };
    const rect = target.getBoundingClientRect();
    const x = Math.min(Math.max(8, rect.right - width), window.innerWidth - width - 8);
    const below = rect.bottom + 4;
    return { x, y: below + height > window.innerHeight - 8 ? Math.max(8, rect.top - height - 4) : below };
  }

  function showPicker() {
    if (saving) return;
    reasoningOpen = false;
    query = '';
    open = !open;
    if (!open) return;
    point = place(trigger, 300, 320);
    void store.refresh();
  }

  function showReasoning() {
    if (saving) return;
    open = false;
    reasoningOpen = !reasoningOpen;
    if (reasoningOpen) reasoningPoint = place(reasoningTrigger, 180, levels.length * 26 + 8);
  }

  async function save(patch: UpdateAgentRequest) {
    if (saving) return;
    const previous = { provider, model, reasoning };
    if (patch.provider !== undefined) provider = patch.provider;
    if (patch.model !== undefined) model = patch.model;
    if (patch.reasoning !== undefined) reasoning = patch.reasoning;
    saving = true;
    issue = '';
    try {
      const profile = await session.client.updateAgent(agent.id, patch);
      provider = profile.agent.provider ?? '';
      model = profile.agent.model ?? '';
      reasoning = profile.agent.reasoning ?? 'off';
      session.agents = session.agents.map((entry) => entry.id === profile.agent.id ? { ...entry, ...profile.agent } : entry);
    } catch {
      provider = previous.provider;
      model = previous.model;
      reasoning = previous.reasoning;
      issue = 'Couldn’t save model settings.';
    } finally {
      saving = false;
    }
  }

  function choose(choice: Choice) {
    open = false;
    if (choice.provider !== provider || choice.model !== model) void save({ provider: choice.provider, model: choice.model });
  }

  function chooseLevel(value: typeof levels[number]) {
    reasoningOpen = false;
    if (value !== reasoning) void save({ reasoning: value });
  }

  function pickerKeys(event: KeyboardEvent) {
    if (event.key === 'Escape') { open = false; trigger?.focus(); }
  }

  $effect(() => {
    if (!open && !reasoningOpen) return;
    const close = (event: Event) => {
      if (event.type === 'scroll') {
        const target = event.target;
        if (target instanceof Node && (trigger?.contains(target) || reasoningTrigger?.contains(target))) return;
        open = false;
        reasoningOpen = false;
      }
    };
    document.addEventListener('scroll', close, true);
    return () => document.removeEventListener('scroll', close, true);
  });
</script>

<svelte:document onclick={(event) => {
  const target = event.target as Node;
  if (open && !trigger?.contains(target) && !(target instanceof Element && target.closest('.picker'))) open = false;
  if (reasoningOpen && !reasoningTrigger?.contains(target) && !(target instanceof Element && target.closest('.reasoning-menu'))) reasoningOpen = false;
}} />

<section>
  <div class="heading"><span>Model</span></div>
  <div class="card">
    <button class="row trigger" bind:this={trigger} aria-label="Model, {model || 'Not set'} {providerName}" aria-expanded={open} disabled={saving} onclick={showPicker}>
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">
        <rect x="6" y="6" width="12" height="12" rx="2"></rect><path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"></path>
      </svg>
      <span class="label">{model ? format(model) : 'Not set'}{#if model && providerName}<span class="provider"> {providerName}</span>{/if}</span>
      <span class="chevrons" aria-hidden="true">⌃⌄</span>
    </button>
    <div class="divider"></div>
    <button class="row trigger" bind:this={reasoningTrigger} aria-label="Reasoning, {format(reasoning)}" aria-expanded={reasoningOpen} disabled={saving} onclick={showReasoning}>
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">
        <path d="M12 3a9 9 0 1 0 9 9 7 7 0 0 1-9-9Z"></path>
      </svg>
      <span class="label">{format(reasoning)}</span>
      <span class="chevrons" aria-hidden="true">⌃⌄</span>
    </button>
  </div>
  {#if issue}<p role="status">{issue}</p>
  {:else if unavailable}<p role="status">Provider unavailable. Choose another model.</p>{/if}
</section>

{#if open}
  <div class="picker" style:left="{point.x}px" style:top="{point.y}px" role="dialog" aria-label="Choose model" tabindex="-1" onkeydown={pickerKeys}>
    <div class="search">
      <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><circle cx="7" cy="7" r="4.5"></circle><path d="m10.5 10.5 3 3"></path></svg>
      <input bind:this={search} bind:value={query} placeholder="Search models" aria-label="Search models" />
    </div>
    <div class="options" role="listbox" aria-label="Models">
      {#if !query.trim()}
        <button role="option" aria-selected={!model} onclick={() => { open = false; if (model) void save({ provider: '', model: '' }); }}><span class="check">{!model ? '✓' : ''}</span>Not set</button>
      {/if}
      {#each groups as group (group.id)}
        {@const matches = choices.filter((choice) => choice.group === group.name)}
        {#if matches.length}
          <div class="group">{format(group.name)}</div>
          {#each matches as choice (`${choice.provider}/${choice.model}`)}
            <button role="option" aria-selected={choice.model === model && choice.provider === provider} onclick={() => choose(choice)}>
              <span class="check">{choice.model === model && choice.provider === provider ? '✓' : ''}</span>{format(choice.model)}
            </button>
          {/each}
        {/if}
      {/each}
      {#if !choices.length}<div class="empty">{query.trim() ? 'No models found.' : 'No providers configured.'}</div>{/if}
    </div>
  </div>
{/if}

{#if reasoningOpen}
  <div class="reasoning-menu" style:left="{reasoningPoint.x}px" style:top="{reasoningPoint.y}px" role="listbox" aria-label="Reasoning options">
    {#each levels as level (level)}
      <button role="option" aria-selected={reasoning === level} onclick={() => chooseLevel(level)}><span class="check">{reasoning === level ? '✓' : ''}</span>{format(level)}</button>
    {/each}
  </div>
{/if}

<style>
  .heading { display: flex; align-items: baseline; justify-content: space-between; font-size: 13px; font-weight: 600; padding: 0 4px 10px; }
  .card { background: var(--window-bg); border-radius: 16px; padding: 6px 8px; }
  .row { display: flex; align-items: center; gap: 8px; width: 100%; height: 34px; padding: 0 6px; font-size: 13px; text-align: left; }
  .row:disabled { opacity: 0.6; }
  .label { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .provider { color: var(--text-muted); }
  .chevrons { flex: none; font-size: 10px; color: var(--text-muted); letter-spacing: -3px; }
  .divider { height: 1px; margin: 0 8px; background: var(--hairline); }
  p { margin: 7px 4px 0; font-size: 11px; color: var(--text-muted); }
  .picker, .reasoning-menu { position: fixed; z-index: 40; box-sizing: border-box; padding: 4px; border: 1px solid var(--hairline); border-radius: 12px; background: var(--window-bg); box-shadow: 0 10px 30px rgba(0, 0, 0, 0.22); }
  .picker { width: 300px; height: 320px; display: flex; flex-direction: column; }
  .search { flex: none; display: flex; align-items: center; gap: 8px; height: 38px; padding: 0 10px; border-bottom: 1px solid var(--hairline); color: var(--text-muted); }
  .search input { flex: 1; min-width: 0; border: 0; outline: 0; background: none; color: var(--text); font: inherit; }
  .options { flex: 1; min-height: 0; overflow: auto; padding-top: 2px; }
  .options button, .reasoning-menu button { display: flex; align-items: center; width: 100%; min-height: 26px; padding: 0 7px; border-radius: 7px; text-align: left; font-size: 12px; }
  .options button:hover, .options button:focus-visible, .reasoning-menu button:hover, .reasoning-menu button:focus-visible { background: var(--row-hover); }
  .check { width: 22px; flex: none; }
  .group { padding: 7px 7px 3px 29px; font-size: 11px; font-weight: 600; color: var(--text-muted); }
  .empty { padding: 24px 10px; text-align: center; font-size: 12px; color: var(--text-muted); }
  .reasoning-menu { width: 180px; }
</style>
