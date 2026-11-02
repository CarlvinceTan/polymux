<script lang="ts">
  import type { Attachment } from '../api/contract';
  import type { Session } from '../session.svelte';
  import { mediaFile } from './mediaCache';

  let { session, attachment, choose }: { session: Session; attachment: Attachment; choose: () => void } = $props();
  let src = $state('');
  $effect(() => {
    const url = attachment.kind === 'image' ? attachment.thumbnailURL || attachment.url : attachment.thumbnailURL;
    let live = true;
    let objectURL = '';
    const controller = new AbortController();
    src = '';
    if (url?.startsWith('blob:')) { src = url; return; }
    if (url) void mediaFile(session.address, url, session.client, controller.signal).then((blob) => {
      if (!live) return;
      src = objectURL = URL.createObjectURL(blob);
    }).catch(() => undefined);
    return () => { live = false; controller.abort(); if (objectURL) URL.revokeObjectURL(objectURL); };
  });
</script>

<button class="tile" aria-label="Show {attachment.name} in chat" onclick={choose}>
  {#if src}<img {src} alt="" />{:else}
    <svg viewBox="0 0 24 24" width="32" height="32" fill="none" stroke="currentColor" stroke-width="1.2" aria-hidden="true">
      {#if attachment.kind === 'video'}<rect x="3" y="4" width="18" height="16" rx="2"></rect><path d="M8 4v16M16 4v16M3 9h5M16 9h5M3 15h5M16 15h5"></path>
      {:else}<rect x="3" y="4" width="18" height="16" rx="2"></rect><circle cx="8" cy="9" r="1.5"></circle><path d="m4 18 5-5 3 3 3-4 5 6"></path>{/if}
    </svg>
  {/if}
  {#if attachment.kind === 'video'}<span class="play" aria-hidden="true">▶</span>{/if}
</button>

<style>
  .tile { position:relative; aspect-ratio:1; min-width:0; width:100%; border:0; border-radius:var(--radius-card); overflow:hidden; background:var(--row-hover); color:var(--text-muted); display:grid; place-items:center; padding:0; cursor:pointer; }
  img { width:100%; height:100%; object-fit:cover; }
  .tile:focus-visible { outline:2px solid var(--focus-ring); outline-offset:-2px; }
  .play { position:absolute; left:6px; bottom:6px; border-radius:50%; width:24px; height:24px; display:grid; place-items:center; background:rgba(0,0,0,.65); color:white; font-size:11px; }
</style>
