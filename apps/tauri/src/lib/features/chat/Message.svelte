<script module lang="ts">
  import type {AgentMessageOriginDto} from '@polymux/protocol';
  import {polymuxApi} from '../../api/polymux';
  import {onThemeChange} from '../../shared/theme';

  /** Site icons for links, asked for once per site and shared by every message
   * on screen — the same handful of sites recur across a conversation. */
  let linkFavicons = new Map<string, Promise<string | null>>();

  // Held per colour scheme, like every other favicon: a site serves one mark
  // for light chrome and another for dark, so what was resolved before a theme
  // change is the wrong icon after it.
  // Refreshed in place rather than by clearing `faviconAsked` and waiting for
  // the observer: it watches for links being added, and a theme change adds
  // none.
  onThemeChange(() => {
    linkFavicons = new Map();
    for (const image of document.querySelectorAll<HTMLImageElement>('img[data-link-favicon]')) {
      const source = image.dataset.linkFavicon;
      if (!source) continue;
      void linkFavicon(source).then((dataUrl) => {
        if (dataUrl) image.src = dataUrl;
      });
    }
  });

  function linkFavicon(url: string): Promise<string | null> {
    let pending = linkFavicons.get(url);
    if (!pending) {
      pending = polymuxApi().browser.favicon(url).catch(() => null);
      linkFavicons.set(url, pending);
    }
    return pending;
  }

  export type MessageRole = 'user' | 'assistant';
  export type MessageFeedback = 'up' | 'down' | null;

  export type MessageData = {
    id: string;
    role: MessageRole;
    text: string;
    files?: string[];
    /** Native paths for `files`, retained separately so the UI keeps its short
     * labels while Copy can place the actual attachment on the clipboard. */
    filePaths?: string[];
    feedback?: MessageFeedback;
    sentAt?: string;
    asGoal?: boolean;
    runId?: string;
    /** Agent-authored provenance. It is presented on the user side without
     * ever becoming a user-authority message in durable history. */
    origin?: AgentMessageOriginDto;
  };
</script>

<script lang="ts">
  import {onDestroy, tick} from 'svelte';
  import {copyText} from '../../shared/clipboard';
  import {displayTime} from '../../shared/displayTime';
  import {renderMarkdown} from './markdown';
  import Icon from '../../shared/components/Icon.svelte';
  import MessageAction from './MessageAction.svelte';
  import MemoryCitations from './MemoryCitations.svelte';
  import {extractMemoryCitations} from './memoryCitations';
  import BloubAvatar from '../team/BloubAvatar.svelte';
  import {bloubExpressionForMessage} from '../team/bloub/expression';
  import {t, translate} from '../../../i18n';

  export let message: MessageData;
  export let streaming = false;
  /** True when an AgentActivity block is rendered above this message; its
   * live shimmer row is the working indicator, so the dots stand down. */
  export let activityVisible = false;
  /** A conversational surface can name the person who is typing instead of
   * falling back to the generic Assistant label. */
  export let respondingLabel = '';
  /** Team bubbles already carry the speaker avatar beside the bubble. */
  export let showOriginAvatar = true;
  /** Team already establishes that every non-human speaker is an agent. Its
   * badge carries the useful distinction — that bot's role — instead. */
  export let originBadge: 'agent' | 'role' = 'agent';
  /** A transcript rather than a conversation — a delegated task's run, which
   * nobody can reply to. Copy stays; editing and feedback would both be
   * addressed to an agent that is not listening. */
  export let readOnly = false;
  export let publicView = false;
  export let onFork: ((id: string) => Promise<void>) | undefined = undefined;
  export let onShare: ((id: string) => void) | undefined = undefined;
  export let onEdit: (id: string, text: string, files: File[]) => void = () => {};
  export let onOpenFile: (name: string) => void = () => {};
  /** `anchor` is the link's own box in the viewport: what opens it is a choice,
   * and the menu that asks belongs under the words that were clicked rather
   * than wherever inside them the pointer landed. */
  export let onOpenLink: (url: string, title: string, anchor?: DOMRect) => void = () => {};
  /** A file link inside the reply, distinct from onOpenFile's attachments. */
  export let onOpenFilePath: (path: string, anchor?: DOMRect) => void = () => {};

  let forking = false;
  let forkError = '';
  async function forkMessage() {
    if (forking || !onFork) return;
    forking = true;
    forkError = '';
    try { await onFork(message.id); }
    catch (error) { forkError = error instanceof Error ? error.message : 'Could not fork chat'; }
    finally { forking = false; }
  }

  let editing = false;
  let draft = '';
  let copied = false;
  let editArea: HTMLTextAreaElement;
  let editFileInput: HTMLInputElement;
  let editFiles: File[] = [];
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  let renderedMarkdown = '';
  let renderFrame = 0;
  let pendingSource = '';

  $: cited = message.role === 'assistant' ? extractMemoryCitations(message.text) : {text: message.text, memories: []};
  $: scheduleMarkdown(message.role === 'assistant' ? cited.text : '', streaming);
  $: sentTime = formatMessageTime(message.sentAt);

  /**
   * Re-parsing the whole message for every streamed token saturates the main
   * thread, which stalls the activity timer and stutters its gradient. While
   * streaming, renders are coalesced to one per animation frame; a settled
   * message renders immediately so the final output is never a frame behind.
   */
  function scheduleMarkdown(source: string, isStreaming: boolean): void {
    if (!source) {
      cancelPendingRender();
      renderedMarkdown = '';
      return;
    }
    pendingSource = source;
    if (!isStreaming || typeof requestAnimationFrame === 'undefined') {
      cancelPendingRender();
      renderedMarkdown = renderMarkdown(pendingSource);
      return;
    }
    if (renderFrame) return;
    renderFrame = requestAnimationFrame(() => {
      renderFrame = 0;
      renderedMarkdown = renderMarkdown(pendingSource);
    });
  }

  function cancelPendingRender(): void {
    if (!renderFrame) return;
    cancelAnimationFrame(renderFrame);
    renderFrame = 0;
  }

  onDestroy(() => {
    cancelPendingRender();
    if (copyTimer) clearTimeout(copyTimer);
  });

  async function startEdit(): Promise<void> {
    draft = message.text;
    editFiles = [];
    editing = true;
    await tick();
    editArea?.focus();
  }

  function submitEdit(): void {
    const text = draft.trim();
    if (!text && !message.files?.length && !editFiles.length) return;
    editing = false;
    onEdit(message.id, text, editFiles);
    editFiles = [];
  }

  function cancelEdit(): void {
    editing = false;
    editFiles = [];
  }

  function selectEditFiles(event: Event): void {
    const input = event.currentTarget as HTMLInputElement;
    const next = Array.from(input.files ?? []);
    const seen = new Set(editFiles.map((file) => `${file.name}\0${file.size}\0${file.lastModified}`));
    editFiles = [...editFiles, ...next.filter((file) => !seen.has(`${file.name}\0${file.size}\0${file.lastModified}`))];
    input.value = '';
  }

  function editKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') cancelEdit();
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      submitEdit();
    }
  }

  async function copyMessage(): Promise<void> {
    // The label only flips to Copied once the text is actually on the clipboard.
    const succeeded = message.text
      ? await copyText(cited.text)
      : message.filePaths?.[0]
        ? await polymuxApi().clipboard.write({kind: 'file', path: message.filePaths[0]})
        : await copyText(message.files?.join('\n') ?? '');
    if (!succeeded) return;
    copied = true;
    if (copyTimer) clearTimeout(copyTimer);
    copyTimer = setTimeout(() => copied = false, 1400);
  }

  async function interactWithMarkdown(event: MouseEvent): Promise<void> {
    const target = event.target instanceof Element ? event.target : null;
    const copy = target?.closest<HTMLButtonElement>('[data-markdown-copy]');
    if (copy) {
      // Resolved from the block rather than the button's sibling, so the copy
      // action keeps working wherever it sits inside the code block header.
      const code = copy.closest('.markdown-code-block')?.querySelector('pre code')?.textContent ?? '';
      copy.textContent = translate(await copyText(code) ? 'common.copied' : 'common.copyFailed');
      setTimeout(() => copy.textContent = translate('common.copy'), 1400);
      return;
    }

    const anchor = target?.closest<HTMLAnchorElement>('.markdown-body a[href]');
    if (!anchor) return;
    const url = new URL(anchor.href, window.location.href);
    // A file link opens in its own application rather than the browser, and
    // the path is taken from the dataset the renderer decoded, not from the
    // href, so no one has to re-parse a file url here.
    if (publicView) {
      if (!['http:', 'https:'].includes(url.protocol)) event.preventDefault();
      else { anchor.target = '_blank'; anchor.rel = 'noopener noreferrer'; }
      return;
    }
    if (url.protocol === 'file:') {
      const filePath = anchor.dataset.filePath;
      if (!filePath) return;
      event.preventDefault();
      onOpenFilePath(filePath, anchor.getBoundingClientRect());
      return;
    }
    if (!['http:', 'https:'].includes(url.protocol)) return;
    event.preventDefault();
    onOpenLink(url.href, anchor.textContent?.trim() || url.hostname, anchor.getBoundingClientRect());
  }

  function markdownInteractions(node: HTMLElement) {
    const click = (event: MouseEvent) => void interactWithMarkdown(event);
    // A link shows its globe until the site's own icon actually arrives,
    // so a missing or broken favicon needs no handling of its own. Load fires
    // on the image rather than bubbling, so it is caught on the way down.
    const loaded = (event: Event) => {
      const image = event.target;
      if (image instanceof HTMLImageElement && image.dataset.linkFavicon !== undefined) image.classList.add('loaded');
    };
    // The markdown carries which site each link icon belongs to, not the icon
    // itself, so the bytes are asked for here. A streaming message rewrites
    // its html as it arrives, so new links are picked up as they appear.
    const fillFavicons = () => {
      if (publicView) return;
      for (const image of node.querySelectorAll<HTMLImageElement>('img[data-link-favicon]')) {
        const source = image.dataset.linkFavicon;
        if (!source || image.dataset.faviconAsked !== undefined) continue;
        image.dataset.faviconAsked = '';
        void linkFavicon(source).then((dataUrl) => {
          if (dataUrl) image.src = dataUrl;
        });
      }
    };
    const observer = new MutationObserver(fillFavicons);
    observer.observe(node, {childList: true, subtree: true});
    fillFavicons();
    node.addEventListener('click', click);
    node.addEventListener('load', loaded, true);
    return {destroy: () => {
      observer.disconnect();
      node.removeEventListener('click', click);
      node.removeEventListener('load', loaded, true);
    }};
  }

  function formatMessageTime(value?: string): string {
    return value ? displayTime(value) : '';
  }
</script>

<article id={`message-${message.id}`} class:assistant={message.role === 'assistant'} class:peer={Boolean(message.origin)} class:editing class="message message-group">
  {#if editing}
    <div class="message-edit-shell">
      <textarea bind:this={editArea} bind:value={draft} aria-label={$t('message.edit')} rows="3" onkeydown={editKeydown}></textarea>
      {#if editFiles.length}
        <div class="message-edit-attachments" aria-label={$t('message.newAttachments')}>
          {#each editFiles as file (`${file.name}-${file.size}-${file.lastModified}`)}<span><Icon name="file" size={12}/>{file.name}</span>{/each}
        </div>
      {/if}
      <div class="message-edit-controls">
        <input bind:this={editFileInput} class="visually-hidden" type="file" multiple tabindex="-1" aria-hidden="true" onchange={selectEditFiles}/>
        <button type="button" class="edit-attach" aria-label={$t('message.attachFiles')} data-tooltip-label={$t('message.attach')} onclick={() => editFileInput.click()}><Icon name="attach" size={16}/></button>
        <span class="message-edit-actions">
          <button type="button" onclick={cancelEdit}>{$t('common.cancel')}</button>
          <button type="button" class="save" onclick={submitEdit}>{$t('composer.send')}</button>
        </span>
      </div>
    </div>
  {:else}
    {#if message.origin}
      <div class="message-peer-origin" aria-label={`Message from ${message.origin.name}${message.origin.role ? `, ${message.origin.role}` : ''}`}>
        {#if showOriginAvatar && message.origin.avatar}<BloubAvatar avatar={message.origin.avatar} expression={bloubExpressionForMessage({text: message.text, streaming})} size={19} animated={false}/>{/if}
        <span><strong>{message.origin.name}</strong>{#if originBadge === 'agent' && message.origin.role}<small>{message.origin.role}</small>{/if}</span>
        {#if originBadge === 'role'}
          {#if message.origin.role}<i class="role-badge">{message.origin.role}</i>{/if}
        {:else}<i>Agent</i>{/if}
      </div>
    {/if}
    {#if message.text || message.role === 'assistant'}
      <div class="message-content" use:markdownInteractions>
        {#if message.text}
          {#if message.role === 'assistant'}
            <div class="markdown-body">{@html renderedMarkdown}</div>
          {:else}
            <p>{message.text}</p>
          {/if}
        {:else if streaming && !activityVisible}
          <span class="thinking" role="status" aria-label={respondingLabel || $t('message.responding')}><i></i><i></i><i></i></span>
        {:else if streaming}
          <!-- The live activity row above carries the working shimmer; pulse
               dots beneath it would be a second, redundant indicator. -->
        {:else}
          <!-- A stopped or cancelled turn leaves no text behind: say so rather
               than pulse dots at a run that is already over. -->
          <p class="message-stopped">{$t('message.stopped')}</p>
        {/if}
      </div>
    {/if}

    {#if forkError}<p role="alert">{forkError}</p>{/if}
    {#if message.files?.length}
      <div class:standalone={!message.text} class="message-files">
        {#each message.files as file (file)}
          <button type="button" class="file-card" onclick={() => onOpenFile(file)}>
            <span class="file-icon"><Icon name="file" size={22}/></span>
            <span class="file-copy"><strong>{file}</strong><small>{$t('message.attachment')}</small></span>
            <span class="open-in"><Icon name="expand" size={14}/>{$t('common.open')}</span>
          </button>
        {/each}
      </div>
    {/if}

    {#if sentTime || message.role === 'user' || (message.text && !streaming)}
      <div class="message-footer">
        {#if sentTime}<time class="message-time" datetime={message.sentAt}>{sentTime}</time>{/if}
        {#if message.role === 'user' || (message.text && !streaming)}
          <div class="message-actions" aria-label={message.role === 'user' ? $t('message.userActions') : $t('message.assistantActions')}>
            <MessageAction icon={copied ? 'check' : 'copy'} label={copied ? $t('common.copied') : $t('common.copy')} onAction={copyMessage}/>
            {#if message.role === 'user' && !message.origin && !readOnly}<MessageAction icon="edit" label={$t('common.edit')} onAction={startEdit}/>{/if}
            {#if message.role === 'assistant' && !readOnly}
              {#if onShare}<MessageAction icon="share" label="Share" onAction={() => onShare?.(message.id)}/>{/if}
              {#if cited.memories.length && !publicView}<MemoryCitations memories={cited.memories}/>{/if}
              {#if onFork}<MessageAction icon="fork" label="Fork" onAction={forkMessage}/>{/if}
            {/if}
          </div>
        {/if}
        {#if message.asGoal}<span class="message-goal-label"><Icon name="goal" size={15}/>{$t('message.sentAsGoal')}</span>{/if}
      </div>
    {/if}
  {/if}
</article>
