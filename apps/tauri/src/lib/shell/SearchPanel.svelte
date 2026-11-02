<script lang="ts">
  import type { Session } from '../session.svelte';
  import type { SearchResult, Thread } from '../api/contract';
  import AvatarStack from '../avatar/AvatarStack.svelte';

  /** Search across chats, bots, messages, media and files, as the reference search panel does. */
  let {
    session,
    scope,
    onClose,
    onPick,
  }: {
    session: Session;
    scope?: string;
    onClose: () => void;
    onPick: (thread: Thread, result: SearchResult) => void;
  } = $props();

  const filters = [
    { title: 'All', value: 'all' }, { title: 'Chats', value: 'conversations' },
    { title: 'Bots', value: 'bots' }, { title: 'Messages', value: 'messages' },
    { title: 'Media', value: 'media' }, { title: 'Files', value: 'files' },
  ];
  const visibleFilters = $derived(scope ? filters.filter((item) => !['conversations', 'bots'].includes(item.value)) : filters);
  const labels: Record<string, string> = {
    conversations: 'Chat', bots: 'Bot', messages: 'Message', media: 'Media', files: 'File',
  };
  let filter = $state('all');
  let query = $state('');
  let found = $state<SearchResult[]>([]);
  let searching = $state(false);
  let selected = $state<string | undefined>();
  let generation = 0;
  const clean = $derived(query.trim());
  const results = $derived.by(() => {
    if (clean) return found;
    if (scope) return [];
    return session.ordered.flatMap((thread): SearchResult[] => {
      const agent = session.agentsFor(thread)[0];
      if (thread.id === thread.agentID && agent && (filter === 'all' || filter === 'bots')) {
        return [{ id: `bot/${agent.id}`, kind: 'bots', title: agent.name, snippet: agent.role,
          threadID: thread.id, agentID: agent.id, score: 0 }];
      }
      if (filter !== 'conversations' && !(filter === 'all' && thread.id !== thread.agentID)) return [];
      return [{ id: `conversation/${thread.id}`, kind: 'conversations', title: thread.title,
        snippet: session.agentsFor(thread).map((entry) => entry.name).join(', '),
        threadID: thread.id, agentID: thread.agentID, score: 0 }];
    });
  });

  $effect(() => {
    const value = clean;
    const kind = filter;
    const current = ++generation;
    selected = undefined;
    found = [];
    searching = !!value;
    if (!value) return;
    const timer = setTimeout(() => {
      void session.client.search(value, kind, scope).then((items) => {
        if (generation === current) found = items;
      }).catch(() => {}).finally(() => {
        if (generation === current) searching = false;
      });
    }, 250);
    return () => clearTimeout(timer);
  });

  async function pick(result: SearchResult) {
    let thread = session.threads.find((entry) => entry.id === result.threadID);
    if (!thread) {
      await session.refresh().catch(() => undefined);
      thread = session.threads.find((entry) => entry.id === result.threadID);
    }
    if (thread) onPick(thread, result);
  }

  function focus(node: HTMLInputElement) { node.focus(); }

  function keys(event: KeyboardEvent) {
    if (event.key === 'Escape') { onClose(); return; }
    if (event.key === 'Enter') {
      const result = results.find((item) => item.id === selected) ?? results[0];
      if (result) { event.preventDefault(); void pick(result); }
    } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      if (!results.length) return;
      event.preventDefault();
      const offset = event.key === 'ArrowDown' ? 1 : -1;
      const index = results.findIndex((item) => item.id === selected);
      const current = index < 0 ? (offset > 0 ? -1 : results.length) : index;
      selected = results[Math.max(0, Math.min(results.length - 1, current + offset))].id;
    }
  }
</script>

<div class="scrim" role="presentation" onclick={(event) => { if (event.target === event.currentTarget) onClose(); }} onkeydown={keys}>
  <div class="panel" role="dialog" aria-label="Search" tabindex="-1">
    <div class="title">Search</div>
    <button class="close glass" aria-label="Close search" onclick={onClose}>
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M18 6 6 18M6 6l12 12"></path>
      </svg>
    </button>

    <div class="field">
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.9" class="lens">
        <circle cx="10.5" cy="10.5" r="6.5"></circle>
        <path d="m15.5 15.5 4 4"></path>
      </svg>
      <input bind:value={query} placeholder={scope ? 'Search this chat' : 'Search'} aria-label={scope ? 'Search this chat' : 'Search everything'} use:focus />
    </div>

    <div class="filters">
      {#each visibleFilters as item (item.value)}
        <button class="filter" class:on={filter === item.value} onclick={() => (filter = item.value)}>{item.title}</button>
      {/each}
    </div>

    <div class="results">
      {#each results as result (result.id)}
        {@const agent = result.agentID ? session.agent(result.agentID) : undefined}
        <button class="result" class:selected={selected === result.id}
          onmouseenter={() => (selected = result.id)} onclick={() => void pick(result)}>
          {#if agent}
            <AvatarStack agents={[agent]} size={40} />
          {:else}
            <span class="result-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.6">
                <path d="M5 4h10l4 4v12H5zM15 4v4h4M8 12h8M8 16h6"></path>
              </svg>
            </span>
          {/if}
          <div class="lines">
            <span class="name">{result.title}</span>
            {#if result.snippet}<span class="sub">{result.snippet}</span>{/if}
          </div>
          <span class="kind">{labels[result.kind] ?? ''}</span>
        </button>
      {/each}
      {#if clean && !results.length && !searching}
        <div class="empty">No results found.</div>
      {/if}
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.24);
    display: grid;
    place-items: center;
    z-index: 50;
  }
  .panel {
    position: relative;
    width: 620px;
    height: 596px;
    max-width: calc(100vw - 40px);
    max-height: calc(100vh - 60px);
    background: var(--window-bg);
    border-radius: var(--radius-panel);
    box-shadow: 0 20px 50px rgba(0, 0, 0, 0.25);
    display: flex;
    flex-direction: column;
    padding: 0 14px 14px;
  }
  .title {
    height: 72px;
    display: grid;
    place-items: center;
    font-weight: 600;
  }
  .close {
    position: absolute;
    top: 18px;
    right: 18px;
    width: 36px;
    height: 36px;
    border-radius: 50%;
    display: grid;
    place-items: center;
  }
  .field {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 37px;
    padding: 0 12px;
    border-radius: 18px;
    background: var(--sidebar-bg);
    margin-bottom: 14px;
  }
  .lens {
    color: var(--text-muted);
    flex: none;
  }
  input {
    flex: 1;
    border: none;
    outline: none;
    background: none;
    font-size: 13px;
    color: var(--text);
    user-select: text;
  }
  input::placeholder {
    color: var(--text-muted);
  }
  .filters {
    display: flex;
    gap: 4px;
    padding: 0 0 8px;
  }
  .filter {
    padding: 5px 12px;
    border-radius: 999px;
    font-size: 12px;
    color: var(--text);
  }
  .filter.on {
    background: var(--row-hover);
    font-weight: 500;
  }
  .filters .filter:not(.on):hover {
    background: var(--row-hover);
  }
  .results {
    flex: 1;
    overflow-y: auto;
    scrollbar-width: none;
    padding: 6px 0;
  }
  .results::-webkit-scrollbar {
    display: none;
  }
  .result {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    padding: 12px 10px;
    border-radius: 10px;
    text-align: left;
  }
  .result:hover, .result.selected {
    background: var(--row-hover);
  }
  .result-icon {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    flex: none;
    color: var(--text-muted);
  }
  .lines {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .name {
    font-size: 13px;
    font-weight: 600;
  }
  .sub {
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .kind {
    font-size: 11px;
    color: var(--text-muted);
  }
  .empty {
    height: 100%;
    display: grid;
    place-items: center;
    color: var(--text-muted);
  }
</style>
