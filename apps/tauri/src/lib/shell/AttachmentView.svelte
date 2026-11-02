<script lang="ts">
  import type { Attachment } from '../api/contract';
  import type { Session } from '../session.svelte';
  import { fileSize } from '../shared/fileSize';
  import { mediaFile } from './mediaCache';
  import MediaPreview from './MediaPreview.svelte';

  let { attachment, session }: { attachment: Attachment; session: Session } = $props();
  let src = $state('');
  let unavailable = $state(false);
  let previewing = $state(false);

  const visual = $derived(attachment.kind === 'image' || attachment.kind === 'video');

  $effect(() => {
    const url = attachment.kind === 'image' ? attachment.thumbnailURL || attachment.url : attachment.thumbnailURL;
    let live = true;
    let objectURL = '';
    const controller = new AbortController();
    src = '';
    unavailable = false;
    if (!url) return;
    if (url.startsWith('blob:')) { src = url; return; }
    void mediaFile(session.address, url, session.client, controller.signal).then((blob) => {
      if (!live) return;
      src = objectURL = URL.createObjectURL(blob);
    }).catch(() => { if (live) unavailable = true; });
    return () => { live = false; controller.abort(); if (objectURL) URL.revokeObjectURL(objectURL); };
  });

  function open() {
    previewing = true;
  }
</script>

<button class="attachment" class:visual class:video={attachment.kind === 'video'} aria-label="View {attachment.name}" onclick={open}>
  {#if visual}
    {#if src}<img src={src} alt={attachment.name} />{:else}
      <span class="placeholder" class:failed={unavailable}>
        <svg viewBox="0 0 24 24" width="28" height="28" fill="none" stroke="currentColor" stroke-width="1.2" aria-hidden="true">
          {#if attachment.kind === 'video'}<rect x="3" y="4" width="18" height="16" rx="2"></rect><path d="m10 9 5 3-5 3z"></path>
          {:else}<rect x="3" y="4" width="18" height="16" rx="2"></rect><circle cx="8" cy="9" r="1.5"></circle><path d="m4 18 5-5 3 3 3-4 5 6"></path>{/if}
        </svg>
      </span>
    {/if}
    {#if attachment.kind === 'video'}<span class="play" aria-hidden="true"><svg viewBox="0 0 24 24" width="19" height="19" fill="currentColor"><path d="m9 7 8 5-8 5z"></path></svg></span>{/if}
    <span class="caption"><span title={attachment.name}>{attachment.name}</span><small>{fileSize(attachment.size)}</small></span>
  {:else}
    <span class="glyph" aria-hidden="true">
      {#if attachment.kind === 'audio'}
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M3 10v4M7 7v10M11 4v16M15 7v10M19 9v6M22 11v2"></path></svg>
      {:else if attachment.kind === 'html'}
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="9"></circle><path d="M3 12h18M12 3c-4 4-4 14 0 18M12 3c4 4 4 14 0 18"></path></svg>
      {:else}
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M5 3h9l5 5v13H5zM14 3v5h5M8 12h8M8 16h6"></path></svg>
      {/if}
    </span>
    <span class="details"><span title={attachment.name}>{attachment.name}</span><small>{attachment.kind === 'html' ? 'Visual card' : attachment.kind === 'audio' ? 'Audio' : fileSize(attachment.size)}</small></span>
    <span class="open" aria-hidden="true">
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M9 5h10v10M19 5 8 16"></path></svg>
    </span>
  {/if}
</button>

{#if previewing}<MediaPreview {attachment} {session} onClose={() => (previewing = false)} />{/if}

<style>
  .attachment { display: flex; flex-direction: column; align-items: stretch; gap: 5px; width: 100%; max-width: 280px; min-width: 200px; padding: 0; border: 0; background: none; color: var(--text-muted); text-align: left; cursor: default; }
  .attachment.visual { width: fit-content; min-width: 0; max-width: 320px; }
  img { display: block; max-width: 320px; max-height: 280px; object-fit: contain; border-radius: 10px; }
  .placeholder { width: 280px; height: 180px; display: grid; place-items: center; border-radius: 10px; background: var(--row-hover); color: var(--text-muted); }
  .placeholder.failed { background: var(--row-hover); }
  .video { position: relative; }
  .video img { width: 280px; height: 190px; object-fit: cover; background: #111; }
  .play { position: absolute; top: 78px; left: 120px; width: 40px; height: 40px; display: grid; place-items: center; border-radius: 50%; background: rgba(0, 0, 0, 0.58); color: white; }
  .caption { display: flex; align-items: baseline; gap: 6px; min-width: 0; max-width: 320px; }
  .caption > span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }
  .caption small { flex: none; font-size: 10px; opacity: 0.8; }
  .attachment:not(.visual) { flex-direction: row; align-items: center; gap: 9px; box-sizing: border-box; padding: 8px 9px; border-radius: 10px; background: var(--row-hover); }
  .attachment:not(.visual):hover { background: color-mix(in srgb, var(--text) 8%, var(--row-hover)); }
  .glyph { flex: none; width: 30px; height: 30px; display: grid; place-items: center; border-radius: 8px; background: color-mix(in srgb, var(--text) 6%, transparent); color: var(--text-muted); }
  .details { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .details span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); font-size: 12px; }
  .details small { color: var(--text-muted); font-size: 10px; }
  .open { flex: none; color: var(--text-muted); }
  .attachment:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 2px; }
</style>
