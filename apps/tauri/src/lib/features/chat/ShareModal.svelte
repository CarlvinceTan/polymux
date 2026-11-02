<script lang="ts">
  import {onMount} from 'svelte';
  import {fade} from 'svelte/transition';
  import Icon from '../../shared/components/Icon.svelte';
  import {copyText} from '../../shared/clipboard';
  import {activateModalDialog, trapModalFocus} from '../../shared/dialogFocus';
  export let create: () => Promise<{url: string; expiresAt: number}>;
  export let onClose: () => void;
  let dialog: HTMLDivElement;
  let url = '';
  let error = '';
  let copied = false;
  let busy = false;
  let alive = true;
  async function generate() {
    busy = true; error = '';
    try { const result = await create(); if (alive) url = result.url; }
    catch (e) { if (alive) error = e instanceof Error ? e.message : 'Could not share conversation'; }
    finally { if (alive) busy = false; }
  }
  async function copy() {
    copied = await copyText(url);
    if (!copied) error = 'Could not copy link. Select the link to copy it manually.';
  }
  onMount(() => { const restore = activateModalDialog(dialog); void generate(); return () => { alive = false; restore(); }; });
</script>

<div class="share-backdrop" transition:fade={{duration: 120}} role="presentation" onclick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
  <div class="share-modal" bind:this={dialog} role="dialog" aria-modal="true" aria-labelledby="share-title" tabindex="-1" onkeydown={(event) => { trapModalFocus(event, dialog); if (event.key === 'Escape') { event.stopPropagation(); onClose(); } }}>
    <header><h2 id="share-title">Share conversation</h2><button type="button" aria-label="Close" onclick={onClose}><Icon name="close" size={18}/></button></header>
    <p>Anyone with the link can view this snapshot for 1 day.</p>
    <div class="share-link" aria-busy={busy}>
      <input readonly value={url} placeholder={busy ? 'Creating link…' : 'Link unavailable'} aria-label="Share link" onclick={(event) => event.currentTarget.select()}/>
      <button type="button" disabled={!url} aria-label={copied ? 'Copied' : 'Copy link'} onclick={copy}><Icon name={copied ? 'check' : 'copy'} size={18}/>{#if copied}<span>Copied</span>{/if}</button>
    </div>
    <div aria-live="polite">{#if error}<p role="alert">{error}</p>{#if !url}<button type="button" disabled={busy} onclick={generate}>Try again</button>{/if}{/if}</div>
  </div>
</div>
<style>
  .share-backdrop { position: fixed; inset: 0; z-index: 1000; background: #0005; display: grid; place-items: center; padding: 24px; }
  .share-modal { width: min(480px, 100%); padding: 24px; background: var(--surface); color: var(--on-surface); border: 1px solid var(--outline); border-radius: 20px; outline: none; }
  header { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
  h2 { font-size: 18px; font-weight: 550; margin: 0; }
  p { font-size: 13px; color: var(--secondary); margin: 16px 0; }
  button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; border: 0; background: none; color: var(--secondary); cursor: pointer; padding: 4px; }
  button:hover { color: var(--on-surface); }
  button:disabled { opacity: .4; cursor: default; }
  .share-link { display: flex; align-items: center; gap: 8px; border: 1px solid var(--outline); border-radius: 10px; background: var(--input-surface); padding: 10px 12px; }
  input { flex: 1; min-width: 0; border: 0; outline: none; background: transparent; color: inherit; font: inherit; font-size: 13px; text-overflow: ellipsis; }
  .share-link:focus-within { outline: 2px solid var(--focus-ring); outline-offset: 2px; }
</style>
