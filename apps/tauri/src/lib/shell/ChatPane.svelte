<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import type { Session } from '../session.svelte';
  import type { Message, ReplyContext, Thread } from '../api/contract';
  import type { ChatDraftMention } from '../api/models';
  import AvatarStack from '../avatar/AvatarStack.svelte';
  import AgentAvatar from '../avatar/AgentAvatar.svelte';
  import { activeActivities, activityLine } from '../features/chat/activityLine';
  import Composer from './Composer.svelte';
  import AttachmentView from './AttachmentView.svelte';
  import ChoiceCard from './ChoiceCard.svelte';
  import CredentialCard from './CredentialCard.svelte';
  import VaultCard from './VaultCard.svelte';
  import ReactionMenu from './ReactionMenu.svelte';
  import ReplyQuote from './ReplyQuote.svelte';
  import VoicePanel from './VoicePanel.svelte';
  import WarningBubble from './WarningBubble.svelte';
  import { copyText } from '../shared/clipboard';
  import { progressiveTime } from '../time';

  /** One conversation: floating header, transcript and composer, as the reference transcript lays them out. */
  let {
    session,
    thread,
    destination,
    onProfile,
  }: {
    session: Session;
    thread: Thread;
    destination?: { id: string; threadID: string; messageID?: string; partIndex?: number };
    onProfile: () => void;
  } = $props();

  let scroller: HTMLDivElement | undefined = $state();
  let atBottom = $state(true);
  let showLatest = $state(false);
  let highlighted = $state<string>();
  let highlightTimer: ReturnType<typeof setTimeout> | undefined;
  let actionTimer: ReturnType<typeof setTimeout> | undefined;
  let actionIssue = $state<{ threadID: string; messageID: string; text: string }>();
  let revealed = '';
  let jumping = false;
  let reactionMenu = $state<{ id: string; threadID: string; point: { x: number; y: number }; part?: number }>();
  let localTarget = $state<{ id: string; threadID: string; messageID: string; partIndex?: number }>();
  let highlightPart = $state<number>();
  let reactionTarget: HTMLElement | undefined;
  let dismissedIssue = $state<string>();
  let dragging = $state(false);
  let dragDepth = 0;
  let scrollDate = $state('');
  let dateTimer: ReturnType<typeof setTimeout> | undefined;
  let voiceOpen = $state(false);

  onDestroy(() => { clearTimeout(dateTimer); clearTimeout(highlightTimer); clearTimeout(actionTimer); });

  const agents = $derived(session.agentsFor(thread));
  const threadID = $derived(thread.id);
  const isGroup = $derived(thread.id !== thread.agentID || thread.agentIDs.length > 1);
  const title = $derived(isGroup ? thread.title : (agents[0]?.name ?? thread.title));
  const role = $derived(isGroup ? '' : (agents[0]?.role ?? '').trim());
  const messages = $derived(session.messages[thread.id] ?? []);
  const active = $derived(session.online ? activeActivities(agents, Object.values(session.runs), messages, thread.id) : []);
  const activityText = $derived(activityLine(active));
  const creator = $derived.by(() => {
    if (isGroup) return undefined;
    const id = agents[0]?.createdBy;
    return id ? session.agent(id)?.name : undefined;
  });
  const displayed = $derived(messages.filter((message) =>
    message.parts.some((part) => !!part.text || !!part.attachment || part.kind === 'choice' || part.kind === 'credential' || part.kind === 'vault') || !!message.reply
    || session.reactions(thread.id, message).length > 0 || (isOutgoing(message) && message.id.startsWith('local-'))));
  const draft = $derived(session.drafts[thread.id]?.value.text ?? '');
  const mentions = $derived(session.drafts[thread.id]?.value.mentions ?? []);
  const reply = $derived(session.drafts[thread.id]?.value.reply);
  const files = $derived(session.files[thread.id] ?? []);
  const serverIssue = $derived.by(() => {
    const message = messages.at(-1);
    if (!session.online || !message || message.id === dismissedIssue) return;
    const issue = session.issue(message);
    return issue?.message ? { id: message.id, message: issue.message } : undefined;
  });
  const topInset = 96;

  function startsGroup(index: number): boolean {
    if (index === 0) return true;
    const previous = new Date(displayed[index - 1].createdAt).getTime();
    const current = new Date(displayed[index].createdAt).getTime();
    return new Date(previous).toDateString() !== new Date(current).toDateString() || current - previous > 1800_000;
  }

  function isOutgoing(message: Message): boolean {
    return message.role === 'user' && !message.agentID;
  }

  function canReact(message: Message): boolean {
    return message.seq > 0 && ['done', 'error'].includes(message.status);
  }

  function hasMenu(message: Message): boolean {
    return canReact(message) || message.parts.some((part) => !!part.text || !!part.attachment);
  }

  function reactionAuthors(message: Message, emoji: string): string {
    return session.reactions(thread.id, message).filter((item) => item.emoji === emoji)
      .map((item) => item.agentID ? session.agent(item.agentID)?.name ?? 'Bot' : 'You').join(', ');
  }

  function quote(message: Message, index?: number): ReplyContext | undefined {
    const partIndex = index ?? message.parts.findIndex((part) => !!part.text || !!part.attachment);
    const part = message.parts[partIndex];
    if (!part || (!part.text && !part.attachment)) return;
    return { messageID: message.id, role: message.role, text: part.text ?? '', attachment: part.attachment ? $state.snapshot(part.attachment) : undefined, agentID: message.agentID, partIndex };
  }

  function author(reply: ReplyContext): string {
    return reply.role === 'user' ? 'You' : (session.agent(reply.agentID)?.name ?? agents[0]?.name ?? 'Bot');
  }

  function messageAuthor(message: Message): string {
    return message.role === 'user' ? 'You' : (session.agent(message.agentID)?.name ?? agents[0]?.name ?? 'Bot');
  }

  function actionError(threadID: string, messageID: string, text: string) {
    clearTimeout(actionTimer);
    actionIssue = { threadID, messageID, text };
    actionTimer = setTimeout(() => { actionIssue = undefined; }, 6000);
  }

  function clearAction(threadID: string, messageID: string) {
    if (actionIssue?.threadID !== threadID || actionIssue.messageID !== messageID) return;
    clearTimeout(actionTimer);
    actionIssue = undefined;
  }

  async function copy(message: Message, text: string) {
    const id = thread.id;
    if (await copyText(text)) clearAction(id, message.id);
    else actionError(id, message.id, 'Couldn’t copy this message.');
  }

  async function share(message: Message, text: string) {
    const id = thread.id;
    try { await navigator.share({ text }); clearAction(id, message.id); }
    catch (error) { if (!(error instanceof DOMException && error.name === 'AbortError')) actionError(id, message.id, 'Couldn’t share this message.'); }
  }

  function showReactions(message: Message, point: { x: number; y: number }, target: HTMLElement, part?: number) {
    if (!hasMenu(message)) return;
    reactionTarget = target;
    reactionMenu = { id: message.id, threadID: thread.id, point, part };
  }

  function menuMessage(id: string): Message | undefined {
    const exact = messages.find((item) => item.id === id);
    if (exact || !id.startsWith('local-')) return exact;
    const accepted = id.slice(6).replace(/-/g, '');
    return messages.find((item) => item.id === accepted && item.seq > 0);
  }

  function closeReactions() {
    const message = reactionMenu && menuMessage(reactionMenu.id);
    const row = message && [...(scroller?.querySelectorAll<HTMLElement>('[data-message-id]') ?? [])]
      .find((element) => element.dataset.messageId === message.id);
    const target = reactionTarget?.isConnected ? reactionTarget : row?.querySelector<HTMLElement>('[role="button"]');
    reactionMenu = undefined;
    target?.focus({ preventScroll: true });
    reactionTarget = undefined;
  }

  function messageKeys(event: KeyboardEvent, message: Message, part?: number) {
    if (!['ContextMenu', 'Enter', ' '].includes(event.key) && !(event.shiftKey && event.key === 'F10')) return;
    event.preventDefault();
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    showReactions(message, { x: rect.left, y: rect.bottom }, event.currentTarget as HTMLElement, part);
  }

  function scrollLatest() {
    void tick().then(() => { if (!jumping) scroller?.scrollTo({ top: scroller.scrollHeight }); });
  }

  $effect(() => {
    const id = threadID;
    void session.follow(id);
    return () => {
      session.stop(id);
      clearTimeout(highlightTimer);
      clearTimeout(actionTimer);
      actionIssue = undefined;
      highlighted = undefined;
      jumping = false;
    };
  });

  $effect(() => {
    if (atBottom) scrollLatest();
  });

  $effect(() => {
    destination?.id;
    localTarget = undefined;
  });

  $effect(() => {
    const target = localTarget ?? destination;
    const id = threadID;
    const history = session.messages[id];
    if (!target?.messageID || target.threadID !== id || !history || revealed === target.id) return;
    let cancelled = false;
    clearTimeout(highlightTimer);
    jumping = true;
    atBottom = false;
    void (async () => {
      const messageID = target.messageID!;
      if (!history.some((message) => message.id === messageID)) {
        const seq = history.filter((message) => message.seq > 0).map((message) => message.seq);
        if (!seq.length) { jumping = false; onScroll(); return; }
        try {
          const page = await session.client.messages(id, Math.min(...seq));
          if (cancelled) return;
          const current = session.messages[id] ?? [];
          const known = new Set(current.map((message) => message.id));
          const earlier = page.filter((message) => !known.has(message.id));
          if (earlier.length) session.messages[id] = [...earlier, ...current];
          else { jumping = false; onScroll(); }
        } catch { if (!cancelled) { jumping = false; onScroll(); } }
        return;
      }
      await tick();
      if (cancelled) return;
      if (!scroller) { jumping = false; return; }
      const row = [...scroller.querySelectorAll<HTMLElement>('[data-message-id]')]
        .find((element) => element.dataset.messageId === messageID);
      if (!row) { jumping = false; onScroll(); return; }
      revealed = target.id;
      const part = target.partIndex !== undefined ? row.querySelector<HTMLElement>(`[data-part-index="${target.partIndex}"]`) : undefined;
      const anchor = part ?? row;
      const top = anchor.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop;
      const visible = scroller.clientHeight - topInset;
      const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
      scroller.scrollTo({ top: top - topInset - (visible - anchor.offsetHeight) / 2, behavior: reduced ? 'instant' : 'smooth' });
      highlighted = messageID;
      highlightPart = target.partIndex;
      clearTimeout(highlightTimer);
      highlightTimer = setTimeout(() => { highlighted = undefined; jumping = false; onScroll(); }, 1200);
    })();
    return () => { cancelled = true; };
  });

  function onScroll() {
    if (!scroller || jumping) return;
    const left = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
    atBottom = left <= (atBottom ? 90 : 45);
    showLatest = !atBottom;
    const top = scroller.getBoundingClientRect().top + topInset + 8;
    for (const row of scroller.querySelectorAll<HTMLElement>('[data-message-id]')) {
      if (row.getBoundingClientRect().bottom <= top) continue;
      const message = messages.find((entry) => entry.id === row.dataset.messageId);
      if (!message) continue;
      scrollDate = progressiveTime(new Date(message.createdAt), true);
      clearTimeout(dateTimer);
      dateTimer = setTimeout(() => { scrollDate = ''; }, 900);
      break;
    }
  }

  function hasFiles(event: DragEvent): boolean {
    return !!event.dataTransfer?.types.includes('Files');
  }

  function dragEnter(event: DragEvent) {
    if (!hasFiles(event)) return;
    event.preventDefault();
    dragDepth++;
    dragging = true;
  }

  function dragOver(event: DragEvent) {
    if (!hasFiles(event)) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy';
  }

  function dragLeave(event: DragEvent) {
    if (!hasFiles(event)) return;
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) dragging = false;
  }

  function dropFiles(event: DragEvent) {
    if (!hasFiles(event)) return;
    event.preventDefault();
    dragDepth = 0;
    dragging = false;
    const files = Array.from(event.dataTransfer?.files ?? []);
    if (files.length) session.addFiles(thread.id, files);
  }

  async function send(text: string, tokens: ChatDraftMention[], targets: string[]) {
    if (!text.trim() && !files.length) return false;
    const selected = [...files];
    atBottom = true;
    const currentReply = reply;
    const sendingThread = thread;
    const saved = await session.send(sendingThread, text.trim(), [], currentReply, selected, targets);
    if (saved && JSON.stringify(session.drafts[sendingThread.id]?.value.reply) === JSON.stringify(currentReply)) session.saveReply(sendingThread.id, undefined);
    const current = session.drafts[sendingThread.id]?.value;
    if (saved && current?.text === text && JSON.stringify(current.mentions ?? []) === JSON.stringify(tokens)) void session.saveDraft(sendingThread.id, '', []);
    scroller?.scrollTo({ top: scroller.scrollHeight });
    return saved;
  }

  function input(text: string, tokens: ChatDraftMention[]) {
    void session.saveDraft(thread.id, text, tokens);
  }
</script>

<div class="pane" role="region" aria-label={title} ondragenter={dragEnter} ondragover={dragOver} ondragleave={dragLeave} ondrop={dropFiles}>
  <div class="transcript" bind:this={scroller} onscroll={onScroll}>
    <div class="content" style:padding-top="{topInset}px">
      {#if creator}<div class="creator">Created by {creator}</div>{/if}
      {#each displayed as message, index (message.id)}
        {@const reactions = session.reactions(thread.id, message)}
        {@const key = session.reactionKey(thread.id, message.id)}
        {@const parts = message.parts.some((part) => !!part.text || !!part.attachment || part.kind === 'credential' || part.kind === 'vault')}
        {#if startsGroup(index)}
          <div class="status">{progressiveTime(new Date(message.createdAt), true)}</div>
        {/if}
        <div class="row" class:outgoing={isOutgoing(message)} data-message-id={message.id}>
          <div class="message-column" class:outgoing={isOutgoing(message)}>
          {#if isGroup && (message.role === 'assistant' || (message.role === 'user' && message.agentID))}
            <span class="author">{messageAuthor(message)}</span>
          {/if}
          {#each message.parts as part, partIndex (partIndex)}
            {#if part.kind === 'choice'}
              <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (The group exposes the question's reaction menu to keyboard users.) -->
              <div class="part" class:highlighted={highlighted === message.id && highlightPart === partIndex} data-part-index={partIndex}
                role="group" aria-label="Question: {part.question ?? ''}" tabindex={canReact(message) ? 0 : -1}
                onkeydown={(event) => { if (event.target === event.currentTarget) messageKeys(event, message, partIndex); }}
                oncontextmenu={(event) => { if (canReact(message)) { event.preventDefault(); showReactions(message, { x: event.clientX, y: event.clientY }, event.currentTarget, partIndex); } }}>
                <ChoiceCard {part} {message} {session} outgoing={isOutgoing(message)} />
              </div>
            {/if}
          {/each}
          {#if parts}
          {@const attached = message.parts.some((part) => !!part.attachment)}
          <!-- svelte-ignore a11y_no_noninteractive_tabindex (Attachment messages expose their controls as a group.) -->
          <div class="bubble" class:outgoing={isOutgoing(message)} class:highlighted={highlighted === message.id && highlightPart === undefined}
            role={attached ? 'group' : 'button'} tabindex={!attached && hasMenu(message) ? 0 : -1} aria-disabled={!attached && !hasMenu(message)} onkeydown={(event) => { if (!attached) messageKeys(event, message); }}
            oncontextmenu={(event) => { if (hasMenu(message)) { event.preventDefault(); const part = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-part-index]') : undefined; showReactions(message, { x: event.clientX, y: event.clientY }, event.currentTarget, part ? Number(part.dataset.partIndex) : undefined); } }}>
            {#each message.parts as part, partIndex (partIndex)}
              <div class="part" class:highlighted={highlighted === message.id && highlightPart === partIndex} data-part-index={partIndex}>
                {#if part.kind === 'credential' && part.credential}<CredentialCard request={part.credential} {message} {session} />
                {:else if part.kind === 'vault' && part.vault}<VaultCard request={part.vault} {message} {session} />
                {:else if part.text}<div class="text">{part.text}</div>{:else if part.attachment}<AttachmentView attachment={part.attachment} {session} />{/if}
              </div>
            {/each}
          </div>
          {/if}
          {#if message.reply}<ReplyQuote reply={message.reply} author={author(message.reply)} client={session.client} onJump={() => { localTarget = { id: crypto.randomUUID(), threadID: thread.id, messageID: message.reply!.messageID, partIndex: message.reply!.partIndex }; }} />{/if}
          {#if reactions.length}
            <div class="reactions">
              {#each [...new Set(reactions.map((item) => item.emoji))].sort() as emoji (emoji)}
                {@const count = reactions.filter((item) => item.emoji === emoji).length}
                <button class="reaction glass" aria-label="Reaction {emoji}, {count}, {reactionAuthors(message, emoji)}"
                  aria-pressed={reactions.some((item) => item.emoji === emoji && !item.agentID)}
                  title={reactionAuthors(message, emoji)} disabled={!canReact(message) || !!session.reactionPending[key]}
                  onclick={() => void session.react(thread.id, message, emoji)}>{emoji} {count}</button>
              {/each}
            </div>
          {/if}
          {#if session.reactionIssues[key]}<span class="reaction-issue" role="status">{session.reactionIssues[key]}</span>{/if}
          {#if actionIssue?.threadID === thread.id && actionIssue.messageID === message.id}<span class="reaction-issue" role="status">{actionIssue.text}</span>{/if}
          {#if isOutgoing(message) && message.seq === 0 && message.id.startsWith('local-') && (message.status === 'pending' || message.status === 'error')}
            {#if session.sending[thread.id] && message.status === 'pending'}
              <span class="send-status">Sending…</span>
            {:else}
              <div class="send-status">{message.status === 'error' ? 'Not sent' : 'Not sent yet'} · <button aria-label="Delete unsent message" disabled={!!session.sending[thread.id]} onclick={() => void session.discard(thread.id, message.id)}>Delete</button></div>
            {/if}
          {/if}
          </div>
        </div>
      {/each}
      {#if active.length}
        <div class="typing" aria-label={activityText}>
          <span class="typing-avatar"><AgentAvatar agent={active[0].agent} size={28} thinking /></span>
          <span class="typing-text">{activityText}</span>
        </div>
      {/if}
      {#if !messages.length}
        <div class="no-messages">No messages yet.</div>
      {/if}
    </div>
  </div>

  {#if reactionMenu && reactionMenu.threadID === thread.id}
    {@const message = menuMessage(reactionMenu.id)}
    {@const selected = message && quote(message, reactionMenu.part)}
    {#if message}<ReactionMenu point={reactionMenu.point} reactions={session.reactions(thread.id, message)} showChoices={canReact(message)} busy={!!session.reactionPending[session.reactionKey(thread.id, message.id)]}
      onCopy={selected?.text ? () => { void copy(message, selected.text); closeReactions(); } : undefined}
      onShare={selected?.text ? () => { void share(message, selected.text); closeReactions(); } : undefined}
      canShare={typeof navigator.share === 'function'}
      onClose={closeReactions} onReply={message.seq > 0 && quote(message, reactionMenu.part) ? () => { session.saveReply(thread.id, quote(message, reactionMenu?.part)); closeReactions(); } : undefined}
      onChoose={(emoji) => { void session.react(thread.id, message, emoji); closeReactions(); }} />{/if}
  {/if}

  <div class="fade"></div>

  {#if scrollDate}<div class="scroll-date" aria-hidden="true">{scrollDate}</div>{/if}

  {#if dragging}
    <div class="drop-zone" aria-hidden="true">
      <svg viewBox="0 0 24 24" width="24" height="24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M12 16V4M7 9l5-5 5 5M5 20h14"></path></svg>
      <span>Drop files to attach</span>
    </div>
  {/if}

  <div class="header">
    <button class="chat-header" onclick={onProfile}>
      <AvatarStack {agents} size={80} height={46} runs={Object.values(session.runs)} messages={session.messages[thread.id] ?? []} conversationID={thread.id} connected={session.online} />
      <div class="pill glass">
        <span class="title">{title}</span>
        {#if role}<span class="role">{role}</span>{/if}
      </div>
    </button>
  </div>

  <button class="speech glass" aria-label="Speech conversation" onclick={() => (voiceOpen = true)}>
    <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" stroke-width="1.7">
      <path d="M6.5 3h2l1.5 4.5-2 1.5a11 11 0 0 0 6 6l1.5-2L20 14.5v2a2.5 2.5 0 0 1-2.7 2.5A16.5 16.5 0 0 1 4 6.7 2.5 2.5 0 0 1 6.5 3Z"
      ></path>
    </svg>
  </button>

  {#if showLatest}
    <button class="latest glass" onclick={() => { jumping = false; atBottom = true; scrollLatest(); }} aria-label="Scroll to latest">
      <svg viewBox="0 0 24 24" width="11" height="11" fill="none" stroke="currentColor" stroke-width="2.2">
        <path d="m6 9 6 6 6-6"></path>
      </svg>
    </button>
  {/if}

  <div class="composer">
    {#if serverIssue}
      <div class="server-issue"><WarningBubble text={serverIssue.message} dismiss={() => (dismissedIssue = serverIssue?.id)} /></div>
    {/if}
    {#if session.draftIssues[thread.id]}
      <div class="draft-issue" role="status">{session.draftIssues[thread.id]}</div>
    {/if}
    {#if session.outboxIssues[thread.id]}
      <div class="draft-issue" role="status">{session.outboxIssues[thread.id]}</div>
    {/if}
    <Composer threadID={thread.id} value={draft} {mentions} members={isGroup ? agents : []} memberIDs={isGroup ? thread.agentIDs : []} {files} {reply} replyAuthor={reply ? author(reply) : ''} client={session.client} onCancelReply={() => session.saveReply(thread.id, undefined)} onSend={send} onInput={input}
      onFiles={(added) => session.addFiles(thread.id, added)} onRemove={(id) => session.removeFile(thread.id, id)} />
  </div>
  {#if voiceOpen}<VoicePanel {session} {thread} onClose={() => (voiceOpen = false)} />{/if}
</div>

<style>
  .pane {
    position: relative;
    display: flex;
    flex-direction: column;
    height: 100%;
    min-width: 0;
  }
  .draft-issue {
    color: var(--text-muted);
    font-size: 12px;
    padding: 0 4px 6px;
  }
  .server-issue { position: absolute; bottom: calc(100% + 8px); left: 14px; right: 14px; display: flex; justify-content: center; }
  .send-status { color: var(--text-muted); font-size: 11px; }
  .send-status button { color: inherit; font: inherit; border: 0; background: none; padding: 0; text-decoration: underline; cursor: pointer; }
  .send-status button:disabled { opacity: 0.5; cursor: default; }
  .transcript {
    flex: 1;
    overflow-y: auto;
    scrollbar-width: none;
    position: relative;
  }
  .transcript::-webkit-scrollbar {
    display: none;
  }
  .content {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 0 20px 14px;
    min-height: 100%;
    box-sizing: border-box;
  }
  .status {
    text-align: center;
    font-size: 10px;
    font-weight: 500;
    color: var(--text-muted);
    padding: 14px 0;
  }
  .creator {
    text-align: center;
    font-size: 10px;
    font-weight: 500;
    color: var(--text-muted);
    padding: 0 0 2px;
  }
  .author {
    padding: 0 12px;
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
  }
  .typing {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    color: var(--text-muted);
    font-size: 12px;
    animation: typing-in 0.18s ease-out;
  }
  .typing-avatar { width: 28px; height: 28px; display: grid; place-items: center; flex: none; }
  .typing-text { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  @keyframes typing-in {
    from { opacity: 0; transform: translateY(2px); }
    to { opacity: 1; transform: none; }
  }
  @media (prefers-reduced-motion: reduce) {
    .typing { animation: none; }
  }
  .row {
    display: flex;
    align-items: flex-end;
    gap: 36px;
  }
  .row.outgoing {
    justify-content: flex-end;
  }
  .message-column { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; min-width: 0; }
  .message-column.outgoing { align-items: flex-end; }
  .reactions { display: flex; max-width: 100%; gap: 6px; overflow-x: auto; }
  .reaction { flex: none; padding: 3px 7px; border-radius: var(--radius-row); font-size: 12px; line-height: 16px; }
  .reaction[aria-pressed='true'] { background: color-mix(in srgb, var(--text) 14%, var(--glass-field)); }
  .reaction-issue { color: var(--text-muted); font-size: 11px; }
  .bubble {
    position: relative;
    background: var(--bubble-in);
    color: var(--text);
    border-radius: var(--radius-bubble);
    padding: 12px;
    font-size: 13px;
    line-height: 17px;
    max-width: 550px;
    box-sizing: border-box;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    user-select: text;
  }
  .bubble.outgoing {
    background: var(--bubble-out);
  }
  .bubble::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: inherit;
    border: 2px solid color-mix(in srgb, var(--text) 35%, transparent);
    background: color-mix(in srgb, var(--unread-dot) 8%, transparent);
    pointer-events: none;
    opacity: 0;
    transition: opacity 0.25s ease-out;
  }
  .bubble.highlighted::after {
    opacity: 1;
    transition-duration: 0.2s;
  }
  .part { position: relative; }
  .part.highlighted::after { content: ''; position: absolute; inset: -4px; border: 2px solid color-mix(in srgb, var(--text) 35%, transparent); border-radius: var(--radius-bubble); background: color-mix(in srgb, var(--unread-dot) 8%, transparent); pointer-events: none; }
  @media (prefers-reduced-motion: reduce) {
    .bubble::after { transition: none; }
  }
  .no-messages {
    position: absolute;
    inset: 0;
    box-sizing: border-box;
    padding-top: 90px;
    display: grid;
    place-items: center;
    font-size: 13px;
    color: var(--text-muted);
    pointer-events: none;
  }
  .fade {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 110px;
    background: linear-gradient(var(--window-bg) 0%, var(--window-bg) 55%, transparent 100%);
    pointer-events: none;
  }
  .scroll-date {
    position: absolute;
    top: 62px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 3;
    padding: 4px 9px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--window-bg) 82%, transparent);
    border: 0.5px solid var(--glass-border);
    color: var(--text-muted);
    font-size: 10px;
    font-weight: 500;
    pointer-events: none;
    animation: scroll-date-in 0.12s ease-out;
  }
  @keyframes scroll-date-in { from { opacity: 0; } to { opacity: 1; } }
  @media (prefers-reduced-motion: reduce) { .scroll-date { animation: none; } }
  .drop-zone {
    position: absolute;
    inset: 10px;
    z-index: 4;
    display: grid;
    place-content: center;
    justify-items: center;
    gap: 8px;
    border: 1.5px dashed color-mix(in srgb, var(--unread-dot) 55%, var(--hairline));
    border-radius: 18px;
    background: color-mix(in srgb, var(--window-bg) 88%, transparent);
    color: var(--text-muted);
    font-size: 12px;
    pointer-events: none;
    animation: drop-in 0.12s ease-out;
  }
  @keyframes drop-in { from { opacity: 0; } to { opacity: 1; } }
  @media (prefers-reduced-motion: reduce) { .drop-zone { animation: none; } }
  .header {
    position: absolute;
    top: 8px;
    left: 0;
    right: 0;
    display: flex;
    justify-content: center;
    pointer-events: none;
  }
  .chat-header {
    display: flex;
    flex-direction: column;
    align-items: center;
    max-width: 260px;
    pointer-events: auto;
  }
  .pill {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    margin-top: -4px;
    padding: 6px 16px;
    border-radius: 999px;
    max-width: 260px;
  }
  .title {
    font-size: 13px;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 228px;
  }
  .role {
    font-size: 10px;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 228px;
  }
  .speech {
    position: absolute;
    top: 8px;
    right: 12px;
    width: 36px;
    height: 36px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: var(--text);
    z-index: 2;
  }
  .speech:hover,
  .latest:hover {
    filter: brightness(0.97);
  }
  .latest {
    position: absolute;
    left: 50%;
    transform: translateX(-50%);
    bottom: 76px;
    padding: 4px 8px;
    border-radius: 999px;
    display: grid;
    place-items: center;
    color: var(--text-muted);
    z-index: 2;
  }
  .composer {
    position: relative;
    padding: 10px 14px 14px;
    flex: none;
  }
</style>
