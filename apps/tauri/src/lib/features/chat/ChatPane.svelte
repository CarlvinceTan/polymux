<script module lang="ts">
  import type {MessageData} from './Message.svelte';
  import type {AgentActivityItem} from './AgentActivity.svelte';
  export type ChatMessage = MessageData & {
    activities?: AgentActivityItem[];
    startedAt?: string;
    completedAt?: string;
    /** Renderer-only delivery state for an optimistic Team group message. */
    teamDelivery?: 'pending' | 'failed';
  };

  export const JUMP_TO_LATEST_THRESHOLD = 160;
  export const STICK_TO_LATEST_THRESHOLD = 20;

  export interface ScrollMetrics {
    scrollHeight: number;
    scrollTop: number;
    clientHeight: number;
  }

  export function distanceFromBottom({scrollHeight, scrollTop, clientHeight}: ScrollMetrics): number {
    return Math.max(0, scrollHeight - scrollTop - clientHeight);
  }

  export function conversationScrollState(metrics: ScrollMetrics) {
    const distance = distanceFromBottom(metrics);
    return {
      distance,
      stickToLatest: distance <= STICK_TO_LATEST_THRESHOLD,
      showJumpToLatest: metrics.scrollHeight > metrics.clientHeight + 4 && distance > JUMP_TO_LATEST_THRESHOLD,
    };
  }
</script>

<script lang="ts">
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
    if (!node || !column?.contains(node)) {
      // The target is not in this pane. Hand control back instead of leaving
      // the request pending, which would keep auto-follow switched off.
      onMessageRevealed();
      return;
    }
    node.scrollIntoView({behavior: 'instant', block: 'center'});
    measure();
    onMessageRevealed();
  }

  export let devices: import('@polymux/protocol').TeamHostDto[] = [];
  export let deviceId = '';
  export let deviceLocked = false;
  export let onDeviceChange: (id: string) => void = () => {};
  import {onMount, tick} from 'svelte';
  import Icon from '../../shared/components/Icon.svelte';
  import WelcomeChatPane from './WelcomeChatPane.svelte';
  import Message from './Message.svelte';
  import PromptInput from './PromptInput.svelte';
  import AgentActivity from './AgentActivity.svelte';
  import QueuedMessages, {type QueuedMessage} from './QueuedMessages.svelte';
  import GoalBar, {type ActiveGoal} from './GoalBar.svelte';
  import type {ReasoningEffort} from '@polymux/protocol';
  import {t} from '../../../i18n';

  export let messages: ChatMessage[] = [];
  export let running = false;
  export let onFork: ((id: string) => Promise<void>) | undefined = undefined;
  export let onShare: ((id: string) => void) | undefined = undefined;
  /** Empty means the composer's own default, which follows the language. */
  export let placeholder = '';
  export let queued: QueuedMessage[] = [];
  export let goal: ActiveGoal | null = null;
  export let speechMode = false;
  export let speechModeEnabled = true;
  export let dictationAutoStopSeconds: number | null = 6;
  export let showJumpToLatest = false;
  export let onSend: (text: string, files: File[], asGoal: boolean, immediate: boolean) => void = () => {};
  export let onStop: () => void = () => {};
  export let onVoice: () => void = () => {};
  export let reasoning: ReasoningEffort = 'medium';
  export let onReasoningChange: (value: ReasoningEffort) => void = () => {};
  export let onEdit: (id: string, text: string, files: File[]) => void = () => {};
  /** A link in a message: the host decides which browser surface opens it. */
  export let onOpenLink: (url: string, title: string, anchor?: DOMRect) => void = () => {};
  export let onOpenFilePath: (path: string, anchor?: DOMRect) => void = () => {};
  export let onQueueHeight: (height: number) => void = () => {};
  export let onJumpAvailability: (show: boolean) => void = () => {};
  export let onSteerQueued: (id: string) => void = () => {};
  export let onRemoveQueued: (id: string) => void = () => {};
  export let onEditQueued: (id: string) => void = () => {};
  export let onReorderQueued: (sourceId: string, targetId: string) => void = () => {};
  /** Text to drop into the composer, e.g. a queued message pulled back for edits. */
  export let insertion: {id: string; text: string} | null = null;
  export let onInsertionApplied: () => void = () => {};
  export let onFileDragActiveChange: (active: boolean) => void = () => {};
  export let draftKey = 'new';
  export let onEditGoal: (text: string) => void = () => {};
  export let onToggleGoalPaused: () => void = () => {};
  export let onDeleteGoal: () => void = () => {};

  let column: HTMLDivElement;
  let composer: HTMLDivElement | undefined;
  /**
   * What the list reserves at its foot. The composer is fixed, so the list has
   * to hold open space of its own for the last message to clear it — and that
   * space is measured rather than assumed, because the composer's height moves
   * with the queue, the goal bar and a prompt grown to several lines.
   *
   * It reaches the prompt's own top edge, not the composer's: the band above the
   * prompt is the fade the conversation is meant to pass under. The gap the
   * reader sees is then the last message's own bottom margin, which is the same
   * margin that spaces every other pair of messages.
   */
  let composerReserve = 0;
  let stickToLatest = true;

  $: onQueueHeight((queued.length ? Math.min(queued.length, 4) * 40 + 24 : 0) + (goal ? 50 : 0));
  $: if (messages.length || running) void followLatest(messages.length, running);

  /* Steering appends the user's message *after* the assistant that is still
     writing, so "last message" stops meaning "the live turn" the moment a
     steer lands — and an assistant with no text yet would read as Stopped.
     The live turn is the last assistant message instead. */
  $: liveIndex = lastAssistantIndex(messages);

  function lastAssistantIndex(list: ChatMessage[]): number {
    for (let index = list.length - 1; index >= 0; index -= 1) {
      if (list[index].role === 'assistant') return index;
    }
    return -1;
  }

  async function followLatest(_count: number, _running: boolean): Promise<void> {
    await tick();
    if (messageTarget) return;
    if (column && stickToLatest) column.scrollTop = column.scrollHeight;
    measure();
  }

  /** Height from the topmost thing the reader must clear down to the window's
      foot: the goal bar or queue when either is stacked above the prompt,
      otherwise the prompt itself. Falls back to the whole composer if none of
      them is mounted, as in speech mode. */
  function reserveFor(node: HTMLDivElement): number {
    const leading = node.querySelector('.goal-bar, .queued-messages, .polymux-prompt-shell');
    if (!leading) return node.getBoundingClientRect().height;
    return Math.max(0, window.innerHeight - leading.getBoundingClientRect().top);
  }

  function measure(): void {
    if (!column) return;
    if (composer) composerReserve = reserveFor(composer);
    const state = conversationScrollState(column);
    stickToLatest = state.stickToLatest;
    onJumpAvailability(state.showJumpToLatest);
  }

  /**
   * Deliberate motion, unlike the jump `followLatest` makes: this is a button
   * the reader pressed, and travelling the distance is what tells them where
   * the bottom was relative to where they had been reading.
   */
  function scrollToLatest(): void {
    if (!column) return;
    const reduceMotion = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
    column.scrollTo({top: column.scrollHeight, behavior: reduceMotion ? 'auto' : 'smooth'});
  }

  /** Watches the composer for its whole life: it is mounted and unmounted with
      the conversation, so a one-off observer set up here would miss it. */
  function trackComposer(node: HTMLDivElement) {
    composer = node;
    measure();
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure);
    observer?.observe(node);
    return {
      destroy: () => {
        observer?.disconnect();
        composer = undefined;
        composerReserve = 0;
      },
    };
  }

  onMount(() => {
    measure();
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure);
    if (column) observer?.observe(column);
    window.addEventListener('resize', measure);
    return () => {
      observer?.disconnect();
      window.removeEventListener('resize', measure);
    };
  });
</script>

<!-- Only this column scrolls. It is never transformed, filtered or contained:
     the composer is fixed inside it, and any of those would make this element
     its containing block and collapse it to the column's width. -->
<div class="conversation-column" bind:this={column} onscroll={measure}>
  {#if messages.length === 0}
    <div class:voice-welcome={speechMode} class="empty-state">
      <WelcomeChatPane {devices} {deviceId} {deviceLocked} {onDeviceChange}
        showComposer={!speechMode}
        active={running}
        {speechModeEnabled}
        {dictationAutoStopSeconds}
        {placeholder}
        {onSend}
        {onStop}
        {onVoice}
        {reasoning}
        {onReasoningChange}
        {insertion}
        {onInsertionApplied}
        {onFileDragActiveChange}
        {draftKey}
      />
    </div>
  {:else}
    <div class="message-list" aria-live="polite" style={composerReserve ? `--composer-reserve:${composerReserve}px` : ''}>
      {#each messages as message, index (message.id)}
        {@const activityVisible = message.role === 'assistant' && Boolean(message.activities?.length || (running && index === liveIndex))}
        {#if activityVisible}
          <AgentActivity
            activities={message.activities ?? []}
            startedAt={message.startedAt}
            completedAt={message.completedAt}
            streaming={running && index === liveIndex}
          />
        {/if}
        <Message {message} streaming={running && index === liveIndex} {activityVisible} {onShare} {onFork} {onEdit} {onOpenLink} {onOpenFilePath}/>
      {/each}
    </div>

    {#if showJumpToLatest}
      <button type="button" class="scroll-to-latest" aria-label={$t('chat.scrollToBottom')} data-tooltip="none" onclick={scrollToLatest}>
        <Icon name="arrow-down" size={18}/>
      </button>
    {/if}

    <div class="sticky-composer" use:trackComposer>
      <div class="composer-column-content">
        {#if goal}
          <GoalBar {goal} onEdit={onEditGoal} onTogglePaused={onToggleGoalPaused} onDelete={onDeleteGoal}/>
        {/if}
        {#if queued.length}
          <QueuedMessages
            items={queued}
            onSteer={onSteerQueued}
            onEdit={onEditQueued}
            onDelete={onRemoveQueued}
            onReorder={onReorderQueued}
          />
        {/if}
        {#if !speechMode}
          {#key draftKey}
            <PromptInput {devices} {deviceId} {deviceLocked} {onDeviceChange} active={running} {speechModeEnabled} {dictationAutoStopSeconds} {placeholder} {onSend} {onStop} {onVoice} {reasoning} {onReasoningChange} {insertion} {onInsertionApplied} {onFileDragActiveChange} {draftKey}/>
          {/key}
        {/if}
      </div>
    </div>
  {/if}
</div>
