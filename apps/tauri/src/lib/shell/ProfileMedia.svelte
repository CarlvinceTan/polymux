<script lang="ts">
  import type { Attachment } from '../api/contract';
  import type { Session } from '../session.svelte';
  import { fileSize } from '../shared/fileSize';
  import MediaTile from './MediaTile.svelte';
  import MediaPreview from './MediaPreview.svelte';

  type Item = { attachment: Attachment; messageID: string; partIndex: number };
  let { session, items, tab, onLocate }: { session: Session; items: Item[]; tab: 'Media' | 'Files'; onLocate: (messageID: string, partIndex: number) => void } = $props();
  const media = $derived(items.filter(({ attachment }) => ['image', 'video', 'audio'].includes(attachment.kind)));
  const files = $derived(items.filter(({ attachment }) => !['image', 'video', 'audio'].includes(attachment.kind)));
  const shown = $derived(tab === 'Media' ? media : files);
  const images = $derived(media.filter(({ attachment }) => attachment.kind !== 'audio'));
  const audio = $derived(media.filter(({ attachment }) => attachment.kind === 'audio'));
  let selected = $state<Attachment>();
</script>

<section class="gallery">
  <div class="heading"><span>{tab}</span><span class="count">{shown.length}</span></div>
  <div class="card" class:empty={!shown.length} aria-label={tab === 'Media' ? 'Media gallery' : 'Files list'}>
    {#if !shown.length}<span class="none">No {tab.toLowerCase()} yet</span>
    {:else if tab === 'Media'}
      <div class="grid">
        {#each images as item (`${item.messageID}:${item.partIndex}`)}
          <MediaTile {session} attachment={item.attachment} choose={() => onLocate(item.messageID, item.partIndex)} />
        {/each}
      </div>
      {#each audio as item (`${item.messageID}:${item.partIndex}`)}{@render row(item.attachment)}{/each}
    {:else}
      {#each files as item (`${item.messageID}:${item.partIndex}`)}{@render row(item.attachment)}{/each}
    {/if}
  </div>
</section>

{#snippet row(attachment: Attachment)}
  <button class="file" aria-label="View {attachment.name}" onclick={() => (selected = attachment)}>
    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
      {#if attachment.kind === 'audio'}<path d="M3 10v4M7 7v10M11 4v16M15 7v10M19 9v6M22 11v2"></path>
      {:else if attachment.kind === 'html'}<circle cx="12" cy="12" r="9"></circle><path d="M3 12h18M12 3c-4 4-4 14 0 18M12 3c4 4 4 14 0 18"></path>
      {:else}<path d="M5 3h9l5 5v13H5zM14 3v5h5M8 12h8M8 16h6"></path>{/if}
    </svg>
    <span class="details"><span title={attachment.name}>{attachment.name}</span><small>{fileSize(attachment.size)}</small></span>
  </button>
{/snippet}

{#if selected}<MediaPreview attachment={selected} {session} onClose={() => (selected = undefined)} />{/if}

<style>
  .gallery { flex:1; min-height:0; display:flex; flex-direction:column; }
  .heading { display:flex; align-items:baseline; justify-content:space-between; font-size:13px; font-weight:600; padding:0 4px 10px; }
  .count { font-size:11px; font-weight:400; color:var(--text-muted); font-variant-numeric:tabular-nums; }
  .card { flex:1; min-height:160px; overflow:auto; padding:4px; background:var(--window-bg); border-radius:var(--radius-card); scrollbar-width:none; }
  .card.empty { display:grid; place-items:center; }
  .none { font-size:13px; color:var(--text-muted); text-align:center; }
  .grid { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:4px; }
  .file { display:flex; align-items:center; gap:6px; width:100%; min-width:0; padding:6px 8px; border-radius:8px; text-align:left; color:var(--text); cursor:pointer; }
  .file:hover { background:var(--row-hover); }
  .file:focus-visible { outline:2px solid var(--focus-ring); outline-offset:-2px; }
  .file svg { width:24px; flex:none; color:var(--text-muted); }
  .details { display:flex; min-width:0; flex-direction:column; gap:2px; font-size:13px; }
  .details span { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  small { font-size:11px; color:var(--text-muted); }
</style>
