<script lang="ts">
  import { onDestroy, tick, untrack } from 'svelte';
  import type { DraftFile } from '../session.svelte';
  import OpenMenu from '../shared/components/OpenMenu.svelte';
  import CameraPanel from './CameraPanel.svelte';
  import ReplyQuote from './ReplyQuote.svelte';
  import type { Agent, ReplyContext } from '../api/contract';
  import type { ChatDraftMention } from '../api/models';
  import type { APIClient } from '../api/client';
  import { createDictation, nativeSpeech } from './dictation';
  import { DictationDraft } from './dictationDraft';
  import Waveform from './Waveform.svelte';
  import { mentionChoices, mentionQuery, mentionTargets, updateMentions, type TextRange } from './mentions';
  /** The message field: glass circle to add, a growing field, and send or dictate. */
  let {
    value = '',
    mentions = [],
    members = [],
    memberIDs = [],
    threadID,
    onSend,
    onInput,
    files = [],
    onFiles,
    onRemove,
    reply,
    replyAuthor,
    client,
    onCancelReply,
  }: {
    value?: string;
    threadID: string;
    mentions?: ChatDraftMention[];
    members?: Agent[];
    memberIDs?: string[];
    onSend: (text: string, mentions: ChatDraftMention[], targets: string[]) => Promise<boolean>;
    onInput?: (text: string, mentions: ChatDraftMention[]) => void;
    files?: DraftFile[];
    onFiles: (files: File[]) => void;
    onRemove: (id: string) => void;
    reply?: ReplyContext;
    replyAuthor: string;
    client: APIClient;
    onCancelReply: () => void;
  } = $props();

  const text = $derived(value);
  let field: HTMLTextAreaElement | undefined = $state();
  let picker: HTMLInputElement | undefined = $state();
  let photoPicker: HTMLInputElement | undefined = $state();
  let menu = $state<DOMRect>();
  let camera = $state(false);
  let handingOff = false;
  let phase = $state<'idle' | 'preparing' | 'recording' | 'stopping'>('idle');
  let level = $state(0);
  let issue = $state('');
  let mentionIssue = $state('');
  let focused = $state(false);
  let editRange: TextRange | undefined;
  const query = $derived(focused && phase !== 'recording' ? mentionQuery(text, mentions, members.length) : undefined);
  const choices = $derived(query ? mentionChoices(members, query.term) : []);
  let insertion = { location: 0, length: 0 };
  let lastText = untrack(() => value);
  let ownerThread = untrack(() => threadID);
  let disposed = false;
  const draft = new DictationDraft();
  const dictation = createDictation(nativeSpeech, {
    state(next, volume, message) {
      if (disposed) return;
      const wasRecording = phase === 'recording';
      phase = next;
      level = volume;
      issue = message ?? '';
      if (next === 'idle') draft.stop();
      if (wasRecording && next !== 'recording' && ownerThread === threadID && document.hasFocus()) {
        const owner = ownerThread;
        void tick().then(() => {
          if (!disposed && ownerThread === owner && threadID === owner && phase !== 'recording') {
            field?.focus();
            field?.setSelectionRange(insertion.location, insertion.location);
          }
        });
      }
    },
    transcript(transcript, start, final) {
      if (disposed || ownerThread !== threadID || !field) return;
      const result = draft.update(transcript, start, final, field.value, insertion);
      if (!result) return;
      insertion = { location: result.cursor, length: 0 };
      const tokens = updateMentions(mentions, lastText, result.text);
      lastText = result.text;
      field.value = result.text;
      onInput?.(result.text, tokens);
      field.setSelectionRange(result.cursor, result.cursor);
      resize();
    },
  });

  onDestroy(() => { disposed = true; dictation.stop(false); });

  $effect(() => {
    if (value !== lastText) { draft.edit(lastText, value); lastText = value; }
    resize();
  });

  $effect(() => {
    if (threadID !== ownerThread) {
      dictation.stop(false);
      draft.stop();
      mentionIssue = '';
      editRange = undefined;
      ownerThread = threadID;
    }
  });

  $effect(() => { if (reply?.messageID) field?.focus(); });
  $effect(() => {
    if (mentionIssue && mentionTargets(mentions, text, new Set(memberIDs))) mentionIssue = '';
  });

  function resize() {
    if (!field) return;
    field.style.height = 'auto';
    field.style.height = `${Math.min(field.scrollHeight, 7 * 20 + 18)}px`;
  }

  async function submit() {
    if (handingOff || (!text.trim() && !files.length)) return;
    const tokens = $state.snapshot(mentions);
    const targets = mentionTargets(tokens, text, new Set(memberIDs));
    if (!targets) { mentionIssue = 'A mentioned agent left this chat. Remove or reselect the mention before sending.'; return; }
    mentionIssue = '';
    dictation.stop(false);
    handingOff = true;
    const sent = text;
    try {
      await onSend(sent, tokens, targets);
    } finally { handingOff = false; }
    void Promise.resolve().then(resize);
  }

  function toggleDictation() {
    if (phase === 'recording' || phase === 'preparing') { dictation.stop(true); return; }
    if (phase !== 'idle') return;
    insertion = { location: field?.selectionStart ?? text.length, length: (field?.selectionEnd ?? text.length) - (field?.selectionStart ?? text.length) };
    ownerThread = threadID;
    draft.stop();
    menu = undefined;
    void dictation.start();
  }

  function visibility() { if (document.hidden) dictation.stop(false); }

  function mention(agent: Agent) {
    if (!query || !field) return;
    const replaced = { location: query.start, length: text.length - query.start };
    const value = text.slice(0, query.start) + `@${agent.name} `;
    const kept = updateMentions(mentions, text, value, replaced);
    if (agent.id !== 'everyone') kept.push({ id: agent.id, text: `@${agent.name}`, location: query.start, length: agent.name.length + 1 });
    draft.edit(lastText, value);
    lastText = value;
    field.value = value;
    mentionIssue = '';
    onInput?.(value, kept);
    field.focus();
    field.setSelectionRange(value.length, value.length);
    resize();
  }

  function keydown(event: KeyboardEvent) {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      if (choices[0]) mention(choices[0]);
      else void submit();
    }
  }
</script>

<svelte:document onvisibilitychange={visibility} />
<div class="input-layout">
<div class="composer">
  <button class="control glass" aria-label="Add attachment" onclick={(event) => (menu = event.currentTarget.getBoundingClientRect())}>
    <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.7">
      <path d="M12 5v14M5 12h14"></path>
    </svg>
  </button>

  <div class="field glass" class:expanded={phase !== 'recording' && (text.includes('\n') || files.length > 0 || !!reply)}>
    {#if choices.length}
      <div class="mention-list glass" aria-label="Mention a member">
        {#each choices as member, index (member.id)}
          <button class={['mention', { first: index === 0 }]} aria-label={members.filter((item) => item.name === member.name).length > 1 ? `Mention ${member.name}, ${member.role}, ${member.id.slice(0, 6)}` : `Mention ${member.name}`}
            onmousedown={(event) => event.preventDefault()} onclick={() => mention(member)}>
            <span>@{member.name}</span>
            {#if member.id === 'everyone'}<small>All members</small>{/if}
            {#if index === 0}<svg class="return" width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" aria-hidden="true"><path d="M13 3v6H4m3-3L4 9l3 3" /></svg>{/if}
          </button>
        {/each}
      </div>
    {/if}
    {#if reply && phase !== 'recording'}
      <div class="reply"><ReplyQuote {reply} author={replyAuthor} {client} /><button aria-label="Cancel reply" onclick={onCancelReply}><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18" /></svg></button></div>
    {/if}
    {#if files.length && phase !== 'recording'}
      <div class="previews" aria-label="Attachments">
        {#each files as item (item.id)}
          <div class="preview">
            {#if item.file.type.startsWith('image/')}
              <img src={item.url} alt={item.file.name} />
            {:else}
              <svg viewBox="0 0 24 24" width="28" height="28" fill="none" stroke="currentColor" stroke-width="1.3" aria-hidden="true"><path d="M5 3h9l5 5v13H5zM14 3v5h5M8 12h8M8 16h6"></path></svg>
              <span class="filename" title={item.file.name}>{item.file.name}</span>
            {/if}
            <button class="remove glass" aria-label="Remove {item.file.name}" onclick={() => onRemove(item.id)}>
              <svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M6 6l12 12M18 6 6 18"></path></svg>
            </button>
          </div>
        {/each}
      </div>
    {/if}
    <textarea
      bind:this={field}
      value={text}
      rows="1"
      placeholder="Message"
      aria-label="Message"
      hidden={phase === 'recording'}
      onfocus={() => (focused = true)}
      onblur={() => (focused = false)}
      onbeforeinput={(event) => { editRange = { location: event.currentTarget.selectionStart, length: event.currentTarget.selectionEnd - event.currentTarget.selectionStart }; }}
      oninput={(event) => {
        const tokens = updateMentions(mentions, lastText, event.currentTarget.value, editRange);
        editRange = undefined;
        draft.edit(lastText, event.currentTarget.value);
        lastText = event.currentTarget.value;
        mentionIssue = '';
        resize();
        onInput?.(event.currentTarget.value, tokens);
      }}
      onkeydown={keydown}
    ></textarea>
    {#if phase === 'recording'}<div class="recording"><Waveform {level} /></div>{/if}
  </div>

  {#if (text.trim() || files.length) && phase !== 'recording'}
    <button class="control glass" aria-label="Send message" onclick={submit}>
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.2">
        <path d="M12 19V5M6 11l6-6 6 6"></path>
      </svg>
    </button>
  {:else}
    <button class="control glass" aria-label={phase === 'recording' ? 'Pause dictation' : phase === 'preparing' ? 'Cancel dictation' : 'Start dictation'} title={phase === 'recording' ? 'Listening' : phase === 'preparing' ? 'Preparing' : undefined} disabled={phase === 'stopping'} onclick={toggleDictation}>
      {#if phase === 'recording'}<svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M7 4h4v16H7zm6 0h4v16h-4z" /></svg>
      {:else if phase === 'preparing'}<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18" /></svg>
      {:else}<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
        <rect x="9" y="3" width="6" height="11" rx="3"></rect>
        <path d="M5.5 11.5a6.5 6.5 0 0 0 13 0M12 18v3"></path>
      </svg>{/if}
    </button>
  {/if}
</div>
{#if issue}<div class="dictation-issue" role="status">{issue}</div>{/if}
{#if mentionIssue}<div class="dictation-issue" role="status">{mentionIssue}</div>{/if}
</div>

<input bind:this={picker} class="hidden" type="file" multiple aria-label="Choose files" onchange={(event) => { onFiles(Array.from(event.currentTarget.files ?? [])); event.currentTarget.value = ''; }} />
<input bind:this={photoPicker} class="hidden" type="file" accept="image/*,video/*" multiple aria-label="Choose photos" onchange={(event) => { onFiles(Array.from(event.currentTarget.files ?? [])); event.currentTarget.value = ''; }} />
{#if menu}
  <OpenMenu ariaLabel="Add attachment" anchor={{ rect: menu }} choices={[
    { value: 'files', label: 'Files', icon: 'file' },
    { value: 'photos', label: 'Photos', icon: 'image' },
    { value: 'camera', label: 'Camera' },
  ]} onClose={() => (menu = undefined)} onChoose={(choice) => {
    menu = undefined;
    if (choice === 'files') picker?.click();
    else if (choice === 'photos') photoPicker?.click();
    else camera = true;
  }} />
{/if}
{#if camera}<CameraPanel onClose={() => (camera = false)} onCapture={(file) => { onFiles([file]); camera = false; }} />{/if}

<style>
  .composer {
    display: flex;
    align-items: flex-end;
    gap: 12px;
  }
  .input-layout { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
  .dictation-issue { padding-left: 48px; font-size: 10px; color: var(--text-muted); }
  .control {
    width: 32px;
    height: 32px;
    flex: none;
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: var(--text);
  }
  .control:hover {
    filter: brightness(0.97);
  }
  .field {
    position: relative;
    box-sizing: border-box;
    flex: 1;
    min-width: 0;
    border-radius: 16px;
    padding: 7px 15px;
    min-height: 32px;
    display: flex;
    flex-direction: column;
    align-items: stretch;
  }
  .recording { height: 20px; }
  .mention-list { position: absolute; z-index: 5; left: 0; bottom: calc(100% + 8px); width: min(320px, 100%); max-height: 160px; overflow-y: auto; padding: 4px; box-sizing: border-box; border-radius: 16px; }
  .mention { display: flex; align-items: center; gap: 8px; min-height: 38px; width: 100%; padding: 8px 12px; border: 0; border-radius: 10px; background: none; color: var(--text); text-align: left; font: inherit; }
  .mention.first, .mention:hover, .mention:focus-visible { background: color-mix(in srgb, var(--text) 10%, transparent); }
  .mention > span:first-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mention small, .mention .return { color: var(--text-muted); font-size: 11px; }
  .mention small { margin-left: auto; }
  .mention .return { margin-left: auto; }
  .field.expanded {
    border-radius: 24px;
    padding: 12px 15px;
    align-items: stretch;
  }
  .hidden { display: none; }
  .reply { display: flex; align-items: flex-start; gap: 8px; margin-bottom: 10px; }
  .reply button { color: var(--text-muted); flex: none; }
  .previews { display: flex; gap: 10px; overflow-x: auto; margin-bottom: 10px; scrollbar-width: none; }
  .previews::-webkit-scrollbar { display: none; }
  .preview { flex: none; position: relative; width: 80px; height: 80px; border-radius: 12px; background: var(--row-hover); color: var(--text-muted); display: grid; place-items: center; overflow: hidden; }
  .preview img { width: 100%; height: 100%; object-fit: cover; }
  .filename { position: absolute; bottom: 7px; left: 7px; right: 7px; font-size: 10px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .remove { position: absolute; right: 4px; top: 4px; width: 18px; height: 18px; display: grid; place-items: center; border-radius: 50%; opacity: 0; }
  .preview:hover .remove, .preview:focus-within .remove { opacity: 1; }
  textarea {
    width: 100%;
    resize: none;
    border: none;
    outline: none;
    background: none;
    font-size: 13px;
    line-height: 20px;
    max-height: 158px;
    overflow-y: auto;
    scrollbar-width: none;
    padding: 0;
    user-select: text;
  }
  textarea::-webkit-scrollbar {
    display: none;
  }
  textarea::placeholder {
    color: var(--text-muted);
  }
</style>
