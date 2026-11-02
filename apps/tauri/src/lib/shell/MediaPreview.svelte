<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import type { Attachment } from '../api/contract';
  import type { Session } from '../session.svelte';
  import { isTauri } from '../api/bridge';
  import { activateModalDialog, trapModalFocus } from '../shared/dialogFocus';
  import { fileSize } from '../shared/fileSize';
  import { mediaFile } from './mediaCache';
  import { openNativeMedia, type NativeMediaAction } from './nativeMedia';
  import { saveMedia } from './saveMedia';

  let { attachment, session, onClose }: { attachment: Attachment; session: Session; onClose: () => void } = $props();
  let dialog = $state<HTMLDialogElement>();
  let closeButton = $state<HTMLButtonElement>();
  let source = $state('');
  let text = $state<string>();
  let file = $state<Blob>();
  let loading = $state(true);
  let issue = $state('');
  let saving = $state(false);
  let saveIssue = $state('');
  let actionIssue = $state('');
  let actionBusy = $state<NativeMediaAction | 'browser' | undefined>();
  let ownsSource = false;
  let controller: AbortController | undefined;
  let saveController: AbortController | undefined;
  let actionController: AbortController | undefined;
  let releaseFocus: (() => void) | undefined;
  let closed = false;

  const isText = $derived(attachment.mimeType.startsWith('text/') || /\.(md|txt|csv|json|xml|log|ts|js|css|html?)$/i.test(attachment.name));
  const isPdf = $derived(attachment.mimeType === 'application/pdf' || attachment.name.toLowerCase().endsWith('.pdf'));

  onMount(() => {
    dialog?.showModal();
    if (dialog) releaseFocus = activateModalDialog(dialog);
    closeButton?.focus({ preventScroll: true });

    const active = attachment.url.startsWith('blob:') ? undefined : new AbortController();
    controller = active;
    if (!active) {
      source = attachment.url;
      loading = false;
    } else {
      void mediaFile(session.address, attachment.url, session.client, active.signal)
        .then(async (blob) => {
          if (active.signal.aborted) return;
          file = blob;
          if (isText && blob.size <= 1_048_576) {
            const content = await blob.text();
            if (active.signal.aborted) return;
            text = content;
          }
          source = URL.createObjectURL(blob);
          ownsSource = true;
        })
        .catch(() => { if (!active.signal.aborted) issue = 'Couldn’t open this file.'; })
        .finally(() => { if (!active.signal.aborted) loading = false; });
    }

    return () => {
      active?.abort();
      releaseFocus?.();
      releaseSource();
    };
  });

  onDestroy(() => {
    controller?.abort();
    saveController?.abort();
    actionController?.abort();
  });

  function releaseSource() {
    if (source && ownsSource) URL.revokeObjectURL(source);
    ownsSource = false;
    source = '';
  }

  function close() {
    if (closed) return;
    closed = true;
    dialog?.close();
  }

  async function save() {
    if (saving || !file) return;
    saving = true;
    saveIssue = '';
    saveController = new AbortController();
    try { await saveMedia(file, attachment.name, saveController.signal); }
    catch { saveIssue = 'Couldn’t save this file.'; }
    finally { saving = false; }
  }

  async function nativeAction(action: NativeMediaAction) {
    if (actionBusy || !file) return;
    actionBusy = action;
    actionIssue = '';
    actionController?.abort();
    actionController = new AbortController();
    try { await openNativeMedia(file, attachment.name, action, actionController.signal); }
    catch { actionIssue = action === 'share' ? 'Couldn’t open sharing for this file.' : 'Couldn’t open this file.'; }
    finally { actionBusy = undefined; }
  }

  function openInBrowser() {
    if (!source) return;
    window.open(source, '_blank', 'noopener');
  }

  async function shareInBrowser() {
    if (!file) return;
    actionBusy = 'browser';
    actionIssue = '';
    try {
      const shared = new File([file], attachment.name, { type: file.type || attachment.mimeType });
      if (navigator.share && (!navigator.canShare || navigator.canShare({ files: [shared] }))) await navigator.share({ files: [shared], title: attachment.name });
      else actionIssue = 'Sharing files isn’t available in this browser.';
    } catch (error) {
      if ((error as DOMException)?.name !== 'AbortError') actionIssue = 'Couldn’t open sharing for this file.';
    } finally { actionBusy = undefined; }
  }
</script>

<dialog bind:this={dialog} aria-label={attachment.name} onclose={onClose}
  onkeydown={(event) => { if (event.key === 'Escape') { event.preventDefault(); close(); } else if (dialog) trapModalFocus(event, dialog); }}>
  <header>
    <div class="identity">
      <span class="kind" aria-hidden="true">
        {#if attachment.kind === 'image'}
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3" y="4" width="18" height="16" rx="2"></rect><circle cx="8" cy="9" r="1.5"></circle><path d="m4 18 5-5 3 3 3-4 5 6"></path></svg>
        {:else if attachment.kind === 'video'}
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3" y="4" width="18" height="16" rx="2"></rect><path d="m10 9 5 3-5 3z"></path></svg>
        {:else if attachment.kind === 'audio'}
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M3 10v4M7 7v10M11 4v16M15 7v10M19 9v6M22 11v2"></path></svg>
        {:else if attachment.kind === 'html'}
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="9"></circle><path d="M3 12h18M12 3c-4 4-4 14 0 18M12 3c4 4 4 14 0 18"></path></svg>
        {:else}
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M5 3h9l5 5v13H5zM14 3v5h5M8 12h8M8 16h6"></path></svg>
        {/if}
      </span>
      <span class="name" title={attachment.name}>{attachment.name}</span>
      <span class="size">{fileSize(attachment.size)}</span>
    </div>
    <button class="close" bind:this={closeButton} aria-label="Close preview" onclick={close}>
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6 6 18M6 6l12 12"></path></svg>
    </button>
  </header>

  <div class="preview" class:centered={loading || !!issue || attachment.kind === 'file'}>
    {#if loading}
      <span class="loading" role="status">Loading</span>
    {:else if issue}
      <span role="status">{issue}</span>
    {:else if attachment.kind === 'image'}
      <img src={source} alt={attachment.name} />
    {:else if attachment.kind === 'video'}
      <!-- svelte-ignore a11y_media_has_caption (Attachment media has no caption track.) -->
      <video src={source} controls playsinline aria-label={attachment.name}></video>
    {:else if attachment.kind === 'audio'}
      <audio src={source} controls aria-label={attachment.name}></audio>
    {:else if attachment.kind === 'html'}
      <iframe src={source} title={attachment.name} sandbox="allow-scripts"></iframe>
    {:else if isPdf}
      <iframe src={source} title={attachment.name}></iframe>
    {:else if text !== undefined}
      <pre>{text}</pre>
    {:else}
      <div class="details">
        <span>{attachment.mimeType || 'File'}</span>
        <span>{fileSize(attachment.size)}</span>
        <span>Preview unavailable. Save the file to open it.</span>
      </div>
    {/if}
  </div>

  <footer>
    {#if saveIssue || actionIssue}<span class="issue" role="status">{saveIssue || actionIssue}</span>{/if}
    {#if source && isTauri}
      <button class="action" disabled={!!actionBusy || loading || !file} onclick={() => void nativeAction('quicklook')}>{actionBusy === 'quicklook' ? 'Opening…' : 'Quick Look'}</button>
      <button class="action" disabled={!!actionBusy || loading || !file} onclick={() => void nativeAction('open')}>Open</button>
      <button class="action" disabled={!!actionBusy || loading || !file} onclick={() => void nativeAction('share')}>{actionBusy === 'share' ? 'Sharing…' : 'Share'}</button>
      <button class="save" disabled={saving || loading || !file} onclick={() => void save()}>{saving ? 'Saving…' : 'Save…'}</button>
    {:else if source}
      <button class="action" onclick={openInBrowser}>Open</button>
      <button class="action" disabled={actionBusy === 'browser'} onclick={() => void shareInBrowser()}>Share</button>
      <a class="save" href={source} download={attachment.name}>Save</a>
    {/if}
  </footer>
</dialog>

<style>
  dialog {
    width: min(760px, calc(100vw - 64px));
    height: min(620px, calc(100vh - 64px));
    padding: 0;
    border: 1px solid var(--hairline);
    border-radius: 16px;
    background: var(--window-bg);
    color: var(--text);
    box-shadow: 0 24px 70px rgba(0, 0, 0, 0.3);
    overflow: hidden;
  }
  dialog::backdrop { background: rgba(0, 0, 0, 0.42); }
  header { height: 48px; display: flex; align-items: center; gap: 12px; padding: 0 10px 0 14px; border-bottom: 1px solid var(--hairline); }
  .identity { min-width: 0; flex: 1; display: flex; align-items: center; gap: 8px; }
  .kind { flex: none; display: grid; place-items: center; width: 26px; height: 26px; border-radius: 7px; background: var(--row-hover); color: var(--text-muted); }
  .name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 550; }
  .size { flex: none; font-size: 11px; color: var(--text-muted); font-variant-numeric: tabular-nums; }
  .close { flex: none; width: 30px; height: 30px; display: grid; place-items: center; border-radius: 8px; color: var(--text-muted); }
  .close:hover, .close:focus-visible { background: var(--row-hover); color: var(--text); }
  .preview { height: calc(100% - 100px); min-height: 0; display: grid; place-items: center; padding: 14px; box-sizing: border-box; overflow: auto; }
  .preview.centered { color: var(--text-muted); font-size: 12px; }
  img { max-width: 100%; max-height: 100%; object-fit: contain; border-radius: 10px; }
  video { max-width: 100%; max-height: 100%; border-radius: 10px; background: #000; }
  audio { width: min(460px, 100%); }
  iframe { width: 100%; height: 100%; border: 0; border-radius: 10px; background: white; }
  pre { align-self: stretch; justify-self: stretch; margin: 0; padding: 14px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; border-radius: 10px; background: var(--sidebar-bg); font: 12px/1.55 ui-monospace, SFMono-Regular, Menlo, monospace; user-select: text; }
  .details { display: flex; flex-direction: column; align-items: center; gap: 5px; text-align: center; }
  .details span:first-child { color: var(--text); font-size: 13px; }
  footer { height: 52px; display: flex; align-items: center; justify-content: flex-end; gap: 10px; padding: 0 14px; border-top: 1px solid var(--hairline); }
  .issue { margin-right: auto; font-size: 11px; color: var(--text-muted); }
  .action, .save { display: inline-flex; align-items: center; height: 28px; padding: 0 11px; border-radius: 8px; background: var(--glass-field); border: 0.5px solid var(--glass-border); color: var(--text); font-size: 12px; text-decoration: none; }
  .action:hover:not(:disabled), .action:focus-visible, .save:hover, .save:focus-visible { background: var(--row-hover); }
  .action:disabled, .save:disabled { opacity: 0.5; }
</style>
