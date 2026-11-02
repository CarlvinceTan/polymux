<script lang="ts">
  import type { ProviderGroup, ProviderStore } from './providers.svelte';

  let { store, onProvider }: { store: ProviderStore; onProvider: (type?: string) => void } = $props();
  let reordering = $state(false);
  let working = $state(false);
  let issue = $state('');

  function summary(group: ProviderGroup) {
    const count = group.models.length;
    const active = group.models.filter((model) => model.enabled).length;
    const status = count > 0 && count === active ? 'all active' : `${active} active`;
    const prefix = group.connections.length > 1 ? `${group.connections.length} connections · ` : '';
    return `${prefix}${count} ${count === 1 ? 'model' : 'models'} (${status})`;
  }

  async function move(id: string, offset: number) {
    const groups = [...store.groups];
    const index = groups.findIndex((group) => group.id === id);
    const target = index + offset;
    if (working || target < 0 || target >= groups.length) return;
    const previous = store.providers ?? [];
    groups.splice(target, 0, groups.splice(index, 1)[0]);
    const ordered = groups.flatMap((group) => group.connections).map((provider, order) => ({ ...provider, order }));
    working = true; issue = ''; store.begin(); store.accept(ordered);
    try { store.accept(await store.session.client.orderProviders(ordered.map((provider) => provider.id))); }
    catch { store.accept(previous); issue = 'Couldn’t save provider order.'; }
    finally { store.end(); working = false; }
  }

  async function refresh() {
    if (working) return;
    working = true; issue = '';
    await store.refresh();
    const providers = store.providers ?? [];
    store.begin();
    const results = await Promise.allSettled(providers.filter((provider) => provider.type !== 'elevenlabs').map((provider) => store.session.client.discoverModels(provider.id)));
    const updated = results.flatMap((result) => result.status === 'fulfilled' ? [result.value] : []);
    store.merge(updated);
    if (results.some((result) => result.status === 'rejected')) issue = 'Couldn’t refresh some models.';
    store.end(); working = false;
  }
</script>

<section>
  <h2>Providers <span class="actions"><button aria-label="Refresh providers" title="Refresh providers" disabled={working || store.pending > 0} onclick={() => void refresh()}>↻</button><button aria-label="Reorder providers" title="Reorder providers" aria-pressed={reordering} disabled={working || store.pending > 0 || store.groups.length < 2} onclick={() => (reordering = !reordering)}>⇅</button><button aria-label="Add provider" title="Add provider" disabled={working || store.pending > 0} onclick={() => onProvider()}>+</button></span></h2>
  <div class="rows">
    {#each store.groups as group, index (group.id)}
      <div class="row">
        <button class="provider" aria-label="Edit {group.name}" aria-describedby="provider-summary-{index}" disabled={working || store.pending > 0} onclick={() => onProvider(group.id)}><span><span class="name">{group.name}</span><small id="provider-summary-{index}">{summary(group)}</small></span>{#if !reordering}<span aria-hidden="true">›</span>{/if}</button>
        {#if reordering}<button aria-label="Move {group.name} up" disabled={working || index === 0} onclick={() => void move(group.id, -1)}>↑</button><button aria-label="Move {group.name} down" disabled={working || index === store.groups.length - 1} onclick={() => void move(group.id, 1)}>↓</button>{/if}
      </div>
    {:else}
      {#if store.providers}<div class="empty">No providers yet.</div>{/if}
    {/each}
  </div>
  {#if issue || store.issue}<p role="status">{issue || store.issue}</p>{/if}
</section>

<style>
  section { margin-bottom: 22px; }
  h2 { display: flex; align-items: center; justify-content: space-between; font-size: 13px; font-weight: 600; margin: 0 0 10px; }
  .actions { display: flex; align-items: center; gap: 4px; }
  .actions button, .row > button:not(.provider) { width: 24px; height: 24px; border-radius: 6px; font-size: 16px; }
  .actions button:hover, .row > button:hover { background: var(--row-hover); }
  button:disabled { opacity: .4; }
  .rows { background: var(--sidebar-bg); border-radius: 10px; }
  .row { display: flex; align-items: center; min-width: 0; padding: 0 10px; }
  .row + .row { border-top: 1px solid var(--hairline); }
  .provider { display: flex; justify-content: space-between; align-items: center; flex: 1; min-width: 0; min-height: 48px; text-align: left; font-size: 13px; }
  .provider > span:first-child { min-width: 0; }
  .name, small { display: block; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  small { margin-top: 2px; font-size: 11px; color: var(--text-muted); }
  .empty { min-height: 36px; display: grid; place-items: center; font-size: 13px; }
  p { margin: 8px 2px 0; font-size: 11px; color: var(--text-muted); }
</style>
