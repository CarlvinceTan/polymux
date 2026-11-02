<script lang="ts">
  import type {TeamGroupDto, BotDto} from '@polymux/protocol';
  import {onDestroy, tick} from 'svelte';
  import MessageInput from '../../shared/components/MessageInput.svelte';
  import Icon from '../../shared/components/Icon.svelte';
  import OpenMenu, {type OpenAnchor} from '../../shared/components/OpenMenu.svelte';
  import {loadAgentDraft, saveAgentDraft} from '../../shared/state/composerDrafts';
  import Message from '../chat/Message.svelte';
  import type {ChatMessage} from '../chat/ChatPane.svelte';
  import type {QueuedMessage} from '../chat/QueuedMessages.svelte';
  import BloubAvatar from './BloubAvatar.svelte';
  import GroupAvatar from './GroupAvatar.svelte';
  import {deviceTypeIconName} from '../../shared/deviceTypeIcon';
  import {bloubActivityForTeamStatus, bloubExpressionForMessage, bloubExpressionForTeamStatus} from './bloub/expression';
  import {teamMessageSpeaker} from './messageIdentity';

  export let messageTarget: {messageId: string} | null = null;
  export let onMessageRevealed: () => void = () => {};
  let revealingTarget: {messageId: string} | null = null;
  $: if (messageTarget && messageTarget !== revealingTarget && messages.some((message) => message.id === messageTarget?.messageId)) {
    revealingTarget = messageTarget;
    void revealMessage(messageTarget);
  }

  async function revealMessage(target: {messageId: string}): Promise<void> {
    stickToLatest = false;
    await tick();
    if (messageTarget !== target) return;
    const node = document.getElementById(`message-${target.messageId}`);
    if (!node || !thread?.contains(node)) {
      // As in the assistant pane: an unreachable target must not leave the
      // request pending and auto-follow permanently off.
      onMessageRevealed();
      return;
    }
    node.scrollIntoView({behavior: 'instant', block: 'center'});
    measureScroll();
    onMessageRevealed();
  }

  export let bot: BotDto | null = null;
  export let group: TeamGroupDto | null = null;
  export let groupMembers: BotDto[] = [];
  export let messages: ChatMessage[] = [];
  export let running = false;
  /** Dispatch is distinct from an Agent run: it must not animate old replies. */
  export let sending = false;
  export let queued: QueuedMessage[] = [];
  export let insertion: {id: string; text: string} | null = null;
  export let dictationAutoStopSeconds: number | null = 6;
  export let attachmentsEnabled = true;
  export let onInsertionApplied: () => void = () => {};
  export let onSend: (text: string, files: File[], asGoal: boolean, immediate: boolean) => void = () => {};
  export let onEdit: (id: string, text: string, files: File[]) => void = () => {};
  /** Opening the bot or group editor from the empty conversation. */
  export let onEditIdentity: () => void = () => {};
  export let onOpenLink: (url: string, title: string, anchor?: DOMRect) => void = () => {};
  export let onOpenFilePath: (path: string, anchor?: DOMRect) => void = () => {};
  export let onSteerQueued: (id: string) => void = () => {};
  export let onRemoveQueued: (id: string) => void = () => {};
  export let onEditQueued: (id: string) => void = () => {};

  const activeConversationId = group?.conversationId ?? bot?.conversationId ?? 'team';
  const draftKey = `team:${activeConversationId}`;
  let draft = loadAgentDraft(draftKey);
  let files: File[] = [];
  let field: HTMLTextAreaElement | null = null;
  let messageInput: MessageInput;
  let fileInput: HTMLInputElement;
  let thread: HTMLDivElement;
  let stickToLatest = true;
  let awayFromLatest = false;
  let toolsAnchor: OpenAnchor | null = null;
  let appliedInsertion = '';
  let dragDepth = 0;
  let draggingFiles = false;

  $: liveIndex = lastAssistantIndex(messages);
  $: conversationName = group?.name ?? bot?.name ?? 'Team';
  $: speakerContext = bot ? bot : {
    id: group?.id ?? 'team-group',
    conversationId: group?.conversationId ?? activeConversationId,
    name: group?.name ?? 'Team',
    role: 'Team group',
    avatar: null,
  };
  $: workingMembers = group
    ? groupMembers.filter((candidate) => candidate.status === 'working')
    : bot?.status === 'working' ? [bot] : [];
  $: detachedTyping = workingMembers.length > 0 && !running;
  $: saveAgentDraft(draftKey, draft);
  $: if (messages.length || running || queued.length) void followLatest(messages.length, running, queued.length);
  $: if (insertion && insertion.id !== appliedInsertion) void applyInsertion(insertion);

  onDestroy(() => saveAgentDraft(draftKey, draft));

  function lastAssistantIndex(items: ChatMessage[]): number {
    for (let index = items.length - 1; index >= 0; index -= 1)
      if (items[index].role === 'assistant') return index;
    return -1;
  }

  function openTools(event: MouseEvent): void {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    toolsAnchor = toolsAnchor ? null : {rect};
  }

  function chooseTool(value: string): void {
    toolsAnchor = null;
    if (value === 'attach') fileInput.click();
    if (value === 'record') void messageInput.startRecording();
  }

  function selectedFiles(event: Event): void {
    addFiles(Array.from((event.currentTarget as HTMLInputElement).files ?? []));
    (event.currentTarget as HTMLInputElement).value = '';
  }

  function addFiles(next: File[]): void {
    const known = new Set(files.map(fileKey));
    files = [...files, ...next.filter((file) => !known.has(fileKey(file)))];
  }

  function fileKey(file: File): string {
    return `${file.name}\0${file.size}\0${file.lastModified}`;
  }

  function removeFile(target: File): void {
    const key = fileKey(target);
    files = files.filter((file) => fileKey(file) !== key);
  }

  function submit(): void {
    const text = draft.trim();
    if (!text && !files.length) return;
    const attachments = files;
    draft = '';
    files = [];
    onSend(text, attachments, false, running);
    void tick().then(() => field?.focus({preventScroll: true}));
  }

  function composerKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      submit();
    }
  }

  async function applyInsertion(next: {id: string; text: string}): Promise<void> {
    appliedInsertion = next.id;
    draft = next.text;
    onInsertionApplied();
    await tick();
    field?.focus({preventScroll: true});
    field?.setSelectionRange(draft.length, draft.length);
  }

  async function followLatest(_messages: number, _running: boolean, _queued: number): Promise<void> {
    await tick();
    if (messageTarget) return;
    if (thread && stickToLatest) thread.scrollTop = thread.scrollHeight;
    measureScroll();
  }

  function measureScroll(): void {
    if (!thread) return;
    const distance = Math.max(0, thread.scrollHeight - thread.scrollTop - thread.clientHeight);
    stickToLatest = distance <= 20;
    awayFromLatest = thread.scrollHeight > thread.clientHeight + 4 && distance > 160;
  }

  function scrollToLatest(): void {
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    thread?.scrollTo({top: thread.scrollHeight, behavior: reduced ? 'auto' : 'smooth'});
  }

  function acceptsFiles(event: DragEvent): boolean {
    return attachmentsEnabled && Array.from(event.dataTransfer?.types ?? []).includes('Files');
  }

  function dragEnter(event: DragEvent): void {
    if (!acceptsFiles(event)) return;
    event.preventDefault();
    dragDepth += 1;
    draggingFiles = true;
  }

  function dragOver(event: DragEvent): void {
    if (!acceptsFiles(event)) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy';
  }

  function dragLeave(event: DragEvent): void {
    if (!acceptsFiles(event)) return;
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) draggingFiles = false;
  }

  function dropFiles(event: DragEvent): void {
    if (!acceptsFiles(event)) return;
    event.preventDefault();
    addFiles(Array.from(event.dataTransfer?.files ?? []));
    dragDepth = 0;
    draggingFiles = false;
    void tick().then(() => field?.focus({preventScroll: true}));
  }
</script>

<section
  class:dragging={draggingFiles}
  class="team-chat-pane"
  aria-label={`Conversation with ${conversationName}`}
  ondragenter={dragEnter}
  ondragover={dragOver}
  ondragleave={dragLeave}
  ondrop={dropFiles}
>
  <div bind:this={thread} class="team-chat-thread" aria-live="polite" onscroll={measureScroll}>
    {#if messages.length === 0 && !detachedTyping}
      <div class="team-chat-empty">
        <button type="button" class="team-chat-empty-identity" aria-label={`Edit ${conversationName}`} onclick={onEditIdentity}>
          {#if group}
            <GroupAvatar members={groupMembers} size={58} label={`${group.name} group avatar`}/>
            <strong>Message {group.name}</strong>
            <span>{groupMembers.map((candidate) => candidate.name).join(', ')}</span>
          {:else if bot}
            <BloubAvatar
              avatar={bot.avatar}
              expression={bloubExpressionForTeamStatus(bot.status, bot.preview)}
              activity={bloubActivityForTeamStatus(bot.status)}
              size={54}
              label={`${bot.name} avatar`}
            />
            <strong>Message {bot.name}</strong>
            <span>{bot.role} · <Icon name={deviceTypeIconName(bot.deviceType)} size={12} strokeWidth={1.4}/> {bot.hostName}</span>
          {/if}
        </button>
      </div>
    {/if}

    <div class="team-chat-messages">
      {#each messages as message, index (message.id)}
        {@const streaming = running && index === liveIndex}
        {@const speaker = teamMessageSpeaker(message, speakerContext)}
        <div class:agent={speaker.side === 'agent'} class:human={speaker.side === 'human'} class:peer={speaker.source === 'peer'} class:with-footer={speaker.side === 'agent' && Boolean(message.sentAt || (message.text && !streaming))} data-team-speaker={speaker.key} class="team-chat-message-row">
          {#if speaker.side === 'agent'}
            <span class:agent-fallback={!speaker.avatar} class="team-chat-avatar" aria-hidden={speaker.avatar ? undefined : 'true'}>
              {#if speaker.avatar}
                <BloubAvatar
                  avatar={speaker.avatar}
                  expression={bloubExpressionForMessage({text: message.text, streaming})}
                  activity={streaming ? (message.text.trim() ? 'working' : 'thinking') : 'idle'}
                  size={28}
                  animated={streaming}
                  paper="var(--neutral-100)"
                  label={`${speaker.name} avatar`}
                />
              {:else}
                <Icon name="bot" size={15}/>
              {/if}
            </span>
          {/if}
          <div class="team-chat-message">
            <Message
              {message}
              {streaming}
              activityVisible={false}
              respondingLabel={`${speaker.name} is typing`}
              showOriginAvatar={false}
              originBadge="role"
              {onEdit}
              {onOpenLink}
              {onOpenFilePath}
            />
            {#if group && message.teamDelivery}
              <span class:failed={message.teamDelivery === 'failed'} class="team-chat-delivery" role="status">
                {message.teamDelivery === 'failed' ? 'Not sent' : 'Sending…'}
              </span>
            {/if}
          </div>
        </div>
      {/each}

      {#if detachedTyping}
        <div class="team-chat-message-row agent detached-typing">
          <span class:group={Boolean(group)} class="team-chat-avatar">
            {#if group}
              <GroupAvatar members={workingMembers} size={28} label={`${workingMembers.map((candidate) => candidate.name).join(' and ')} working`}/>
            {:else if bot}
              <BloubAvatar avatar={bot.avatar} expression="attentive" activity="working" size={28} paper="var(--neutral-100)" label={`${bot.name} avatar`}/>
            {/if}
          </span>
          <span class="team-chat-typing" role="status" aria-label={group ? `${workingMembers.map((candidate) => candidate.name).join(' and ')} working` : `${bot?.name ?? 'Agent'} is typing`}><i></i><i></i><i></i></span>
        </div>
      {/if}

      {#each queued as item (item.id)}
        <div class="team-chat-queued-row">
          <div class="team-chat-queued-bubble">
            {#if item.text}<p>{item.text}</p>{/if}
            {#if item.files?.length}
              {#each item.files as file (file.name)}<span class="team-chat-queued-file"><Icon name="attach" size={12}/>{file.name}</span>{/each}
            {/if}
          </div>
          <span class="team-chat-queued-state">Queued</span>
          <span class="team-chat-queued-actions">
            <button type="button" aria-label="Send queued message now" onclick={() => onSteerQueued(item.id)}><Icon name="send" size={12}/></button>
            <button type="button" aria-label="Edit queued message" onclick={() => onEditQueued(item.id)}><Icon name="edit" size={12}/></button>
            <button type="button" aria-label="Delete queued message" onclick={() => onRemoveQueued(item.id)}><Icon name="trash" size={12}/></button>
          </span>
        </div>
      {/each}
    </div>
  </div>

  {#if awayFromLatest}
    <button type="button" class="team-chat-to-latest" aria-label="Scroll to latest message" onclick={scrollToLatest}><Icon name="arrow-down" size={14}/></button>
  {/if}

  <footer class="team-chat-footer" aria-busy={sending}>
    {#if files.length}
      <div class="team-chat-files" aria-label="Attachments">
        {#each files as file (fileKey(file))}
          <span><Icon name="attach" size={12}/><em title={file.name}>{file.name}</em><button type="button" aria-label={`Remove ${file.name}`} onclick={() => removeFile(file)}><Icon name="close" size={11}/></button></span>
        {/each}
      </div>
    {/if}
    <div class="team-chat-composer-row">
      {#if attachmentsEnabled}
        <input bind:this={fileInput} class="visually-hidden" type="file" multiple tabindex="-1" aria-hidden="true" onchange={selectedFiles}/>
        <button
          type="button"
          class:active={Boolean(toolsAnchor)}
          class="team-chat-add"
          aria-label="More"
          data-tooltip="none"
          aria-haspopup="menu"
          aria-expanded={Boolean(toolsAnchor)}
          onclick={openTools}
        ><Icon name="plus" size={16}/></button>
      {/if}
      <div class="team-chat-composer">
        <MessageInput bind:this={messageInput} bind:field bind:value={draft} placeholder={`Message ${conversationName}`} hasAttachments={files.length > 0} {dictationAutoStopSeconds} onSend={submit} onkeydown={composerKeydown} onSendRecording={(file) => onSend('', [file], false, running)} />
      </div>
    </div>
  </footer>

  {#if draggingFiles}<div class="team-chat-drop" aria-hidden="true"><Icon name="attach" size={18}/><span>Drop files to attach</span></div>{/if}
</section>

<OpenMenu
  choices={[{value: 'attach', label: 'Attach files', icon: 'attach'}, {value: 'record', label: 'Record voice message', icon: 'mic'}]}
  anchor={toolsAnchor}
  ariaLabel="Message actions"
  onChoose={chooseTool}
  onClose={() => toolsAnchor = null}
/>

<style>
  .team-chat-pane{position:relative;grid-column:2;grid-row:1;min-width:0;height:100vh;box-sizing:border-box;display:flex;flex-direction:column;padding-top:var(--app-topbar-height);overflow:hidden;background:var(--main-panel-background);color:var(--neutral-900)}
  .team-chat-thread{min-height:0;flex:1;overflow-y:auto;overscroll-behavior:contain;padding:18px 18px 10px;scrollbar-width:none}.team-chat-thread::-webkit-scrollbar{display:none}.team-chat-messages{width:100%;min-height:100%;display:flex;box-sizing:border-box;flex-direction:column;justify-content:flex-end;gap:10px}
  .team-chat-empty{min-height:100%;display:flex;flex:1;flex-direction:column;align-items:center;justify-content:center;padding:30px;text-align:center}.team-chat-empty-identity{display:flex;flex-direction:column;align-items:center;appearance:none;border:0;border-radius:14px;padding:12px 18px;background:transparent;color:inherit;font:inherit;text-align:center;cursor:pointer}.team-chat-empty-identity strong{margin-top:10px;color:var(--neutral-800);font-size:13px;font-weight:570;transition:color .15s ease}.team-chat-empty-identity span{margin-top:2px;color:var(--secondary);font-size:11.5px;font-weight:450;transition:color .15s ease}
  /* The same role-and-Host line the title bar draws (.team-identity>span in
     TitleBar.svelte): same type, and the device glyph centred on the text's
     x-height rather than on its line box, so the two read as one line. Keep the
     three values there and here in step — tests/team-identity-alignment.spec.ts
     fails if they drift. */
  .team-chat-empty-identity span :global(svg){display:inline-block;margin-right:3px;vertical-align:calc(.5ex - 6px)}.team-chat-empty-identity:hover strong,.team-chat-empty-identity:focus-visible strong{color:var(--neutral-950)}.team-chat-empty-identity:hover span,.team-chat-empty-identity:focus-visible span{color:var(--neutral-700)}.team-chat-empty-identity:focus-visible{outline:2px solid var(--focus-ring);outline-offset:-2px}@media (prefers-reduced-motion:reduce){.team-chat-empty-identity strong,.team-chat-empty-identity span{transition:none}}
  .team-chat-message-row{width:100%;min-width:0;display:flex;align-items:flex-end;gap:7px}.team-chat-message-row.human{justify-content:flex-end;padding-left:52px;box-sizing:border-box}.team-chat-message-row.agent{padding-right:52px;box-sizing:border-box}.team-chat-avatar{width:28px;height:28px;display:grid;place-items:center;flex:none;color:var(--neutral-600)}.team-chat-avatar.agent-fallback{border-radius:50%;background:var(--neutral-100)}.team-chat-message-row.agent.with-footer .team-chat-avatar{margin-bottom:18px}.team-chat-message{min-width:0;max-width:76%}.team-chat-message-row.human .team-chat-message{margin-left:auto}
  .team-chat-message :global(.message){max-width:100%;margin:0;color:var(--neutral-900);font-size:12.5px;line-height:1.48}.team-chat-message :global(.message:not(.assistant)){max-width:100%}.team-chat-message :global(.message-content){overflow:hidden}.team-chat-message-row.agent :global(.message-content){width:fit-content;max-width:100%;box-sizing:border-box;border-radius:13px;padding:8px 11px;background:var(--neutral-100)}.team-chat-message-row.human :global(.message-content){border:0;border-radius:13px;padding:8px 11px;background:var(--neutral-950);color:var(--app-bg)}
  .team-chat-message-row.human :global(.message-content p){color:inherit}.team-chat-message :global(.message-peer-origin){display:flex;align-items:baseline;gap:5px;margin:0 2px 4px;color:var(--secondary);font-size:11px;line-height:1.2}.team-chat-message :global(.message-peer-origin span){min-width:0;display:flex;align-items:baseline;gap:5px;overflow:hidden}.team-chat-message :global(.message-peer-origin strong){overflow:hidden;color:var(--neutral-800);font-weight:620;text-overflow:ellipsis;white-space:nowrap}.team-chat-message :global(.message-peer-origin small){overflow:hidden;color:var(--secondary);text-overflow:ellipsis;white-space:nowrap}.team-chat-message :global(.message-peer-origin i){flex:none;border:1px solid var(--neutral-300);border-radius:5px;padding:1px 4px;color:var(--neutral-700);font-size:11px;font-style:normal;font-weight:650;letter-spacing:.03em;text-transform:uppercase}.team-chat-message :global(.message-peer-origin i.role-badge){max-width:min(220px,50%);overflow:hidden;border-color:var(--team-role-outline);border-radius:6px;padding:1px 6px;background:var(--team-role-surface);color:var(--team-role-text);font-weight:500;letter-spacing:0;text-overflow:ellipsis;text-transform:none;white-space:nowrap}.team-chat-message :global(.markdown-body p){margin:.55em 0}.team-chat-message :global(.markdown-body :is(h1,h2,h3,h4)){color:inherit}.team-chat-message :global(.message-footer){min-height:16px;margin-top:2px;color:var(--secondary);opacity:1}.team-chat-message :global(.message-actions){display:none}.team-chat-message :global(.message-time){color:var(--secondary);font-size:11px;line-height:14px}.team-chat-message-row.agent :global(.message-footer){padding-left:2px}.team-chat-message :global(.message-files){gap:6px;margin-top:5px}.team-chat-message :global(.file-card){padding:8px 10px;border-radius:10px;box-shadow:none}.team-chat-message :global(.file-icon){width:30px;height:30px}.team-chat-message :global(.file-copy strong){font-size:11.5px}.team-chat-message :global(.file-copy small),.team-chat-message :global(.open-in){font-size:11px}
  .team-chat-delivery{display:block;margin:2px 3px 0;color:var(--secondary);font-size:11px;text-align:right}.team-chat-delivery.failed{color:var(--danger-600)}
  .team-chat-typing{display:inline-flex;align-items:center;gap:5px;border-radius:13px;padding:11px 13px;background:var(--neutral-100)}.team-chat-typing i{width:6px;height:6px;border-radius:50%;background:var(--secondary);animation:team-typing 1.2s ease-in-out infinite}.team-chat-typing i:nth-child(2){animation-delay:.2s}.team-chat-typing i:nth-child(3){animation-delay:.4s}.detached-typing .team-chat-avatar{margin-bottom:0}@keyframes team-typing{0%,60%,100%{opacity:.38;transform:scale(1)}30%{opacity:1;transform:scale(1.3)}}
  .team-chat-queued-row{display:flex;flex-wrap:wrap;align-items:center;justify-content:flex-end;gap:4px 6px;padding-left:52px}.team-chat-queued-bubble{max-width:76%;border-radius:13px;padding:8px 11px;background:color-mix(in srgb,var(--neutral-950) 68%,transparent);color:var(--app-bg);opacity:.72}.team-chat-queued-bubble p{margin:0;font-size:12.5px;line-height:1.48;white-space:pre-wrap}.team-chat-queued-file{display:flex;align-items:center;gap:4px;margin-top:4px;font-size:11px}.team-chat-queued-state{color:var(--secondary);font-size:11px}.team-chat-queued-actions{display:flex}.team-chat-queued-actions button{width:22px;height:22px;display:grid;place-items:center;border:0;border-radius:6px;padding:0;background:transparent;color:var(--neutral-700);cursor:pointer}.team-chat-queued-actions button:hover{color:var(--neutral-900)}
  .team-chat-footer{position:relative;flex:none;padding:0 14px 14px;background:linear-gradient(to bottom,transparent,var(--app-bg) 18px)}.team-chat-composer-row{width:100%;display:flex;align-items:flex-end;gap:6px;margin-top:10px}.team-chat-add{width:36px;height:42px;display:grid;place-items:center;flex:none;border:0;padding:0;background:transparent;color:var(--neutral-700);cursor:pointer}.team-chat-add:hover,.team-chat-add.active{color:var(--neutral-950)}
  .team-chat-composer{position:relative;min-width:0;display:flex;align-items:flex-end;gap:4px;flex:1;box-sizing:border-box;border:1px solid var(--neutral-200);border-radius:21px;padding:4px 5px 4px 14px;background:var(--app-surface)}.team-chat-composer:focus-within{border-color:var(--focus-ring);box-shadow:0 0 0 1px var(--focus-ring)}
  .team-chat-files{width:100%;display:flex;flex-wrap:wrap;gap:5px;margin-bottom:-4px;padding-top:10px}.team-chat-files>span{min-width:0;max-width:220px;height:25px;display:flex;align-items:center;gap:5px;border:1px solid var(--neutral-200);border-radius:8px;padding:0 5px 0 8px;background:var(--app-surface);color:var(--secondary);font-size:11px}.team-chat-files em{min-width:0;overflow:hidden;flex:1;font-style:normal;text-overflow:ellipsis;white-space:nowrap}.team-chat-files button{width:18px;height:18px;display:grid;place-items:center;border:0;border-radius:5px;padding:0;background:transparent;color:var(--neutral-700);cursor:pointer}.team-chat-files button:hover{color:var(--neutral-900)}
  .team-chat-to-latest{position:absolute;z-index:3;right:50%;bottom:72px;width:32px;height:32px;display:grid;place-items:center;border:1px solid var(--neutral-200);border-radius:50%;padding:0;background:var(--app-translucent);color:var(--neutral-700);box-shadow:0 4px 14px rgb(0 0 0/.16);transform:translateX(50%);cursor:pointer;backdrop-filter:blur(10px)}
  .team-chat-drop{pointer-events:none;position:absolute;z-index:12;inset:var(--app-topbar-height) 0 0;display:flex;align-items:center;justify-content:center;gap:7px;background:var(--file-drop-pane-tint);color:var(--neutral-800);font-size:12px;font-weight:560;backdrop-filter:blur(2px)}
  :global(main.workspace-expanded) .team-chat-pane{pointer-events:none}
  @media (max-width:640px){.team-chat-thread{padding-inline:12px}.team-chat-message-row.human{padding-left:24px}.team-chat-message-row.agent{padding-right:24px}.team-chat-message{max-width:84%}.team-chat-footer{padding-inline:10px}}
  @media (prefers-reduced-motion:reduce){.team-chat-typing i{animation:none}}
</style>
