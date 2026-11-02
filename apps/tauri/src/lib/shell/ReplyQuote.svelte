<script lang="ts">
  import type { ReplyContext } from '../api/contract';
  import type { APIClient } from '../api/client';

  let { reply, author, client, onJump }: { reply: ReplyContext; author: string; client: APIClient; onJump?: () => void } = $props();
  let image = $state('');
  const summary = $derived(reply.text.trim() || reply.attachment?.name || 'Message');

  $effect(() => {
    const attachment = reply.attachment;
    image = '';
    if (!attachment || attachment.kind !== 'image') return;
    const url = attachment.thumbnailURL || attachment.url;
    if (url.startsWith('blob:')) { image = url; return; }
    const abort = new AbortController();
    let objectURL = '';
    void client.attachment(url, abort.signal).then((blob) => {
      if (abort.signal.aborted) return;
      objectURL = URL.createObjectURL(blob);
      image = objectURL;
    }).catch(() => {});
    return () => { abort.abort(); if (objectURL) URL.revokeObjectURL(objectURL); };
  });
</script>

{#snippet content()}
  {#if onJump}
    <span class="label">{author}:</span>{#if !reply.attachment}<span class="bodytext">{reply.text}</span>{/if}
  {:else}
    <span class="words"><span class="author">{author}</span><span class="summary">{summary}</span></span>
  {/if}
  {#if reply.attachment}
    <span class="attachment">{#if image}<img src={image} alt="" />{:else}<svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.4" aria-hidden="true"><path d="M5 3h9l5 5v13H5zM14 3v5h5" /></svg>{/if}</span>
  {/if}
{/snippet}

{#if onJump}
  <button class="quote bubble" aria-label="Reply to {author}: {reply.attachment?.name || reply.text}" title="Show original message" onclick={onJump}>
    {@render content()}
  </button>
{:else}
  <div class="quote preview" aria-label="Reply to {author}: {summary}">{@render content()}</div>
{/if}

<style>
  .quote { display: flex; align-items: flex-start; gap: 10px; min-width: 0; text-align: left; font-size: 12px; }
  .preview { position: relative; padding: 3px 0 3px 11px; flex: 1; }
  .preview::before { content: ''; position: absolute; left: 0; top: 3px; bottom: 3px; width: 3px; border-radius: 2px; background: color-mix(in srgb, var(--text) 22%, transparent); }
  .bubble { padding: 10px 12px; border-radius: 12px; background: color-mix(in srgb, var(--text) 12%, var(--bubble-in)); max-width: 550px; min-width: 160px; }
  .words { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .label { flex: none; color: var(--text-muted); }
  .bodytext { color: var(--text-muted); white-space: pre-wrap; overflow-wrap: anywhere; }
  .author { font-weight: 600; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .summary { color: var(--text-muted); display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 3; line-clamp: 3; overflow: hidden; overflow-wrap: anywhere; }
  .attachment { flex: none; width: 52px; height: 52px; display: grid; place-items: center; color: var(--text-muted); border-radius: 8px; background: var(--row-hover); overflow: hidden; }
  .attachment img { width: 100%; height: 100%; object-fit: cover; }
  .bubble .attachment { width: 48px; height: 48px; }
</style>
