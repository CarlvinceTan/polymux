<script lang="ts">
  import type {TeamGroupDto, BotDto, PinnableWorkspaceView} from '@polymux/protocol';
  import {afterUpdate, beforeUpdate, tick, type ComponentProps} from 'svelte';
  import {fade} from 'svelte/transition';
  import Icon from '../../shared/components/Icon.svelte';
  import BloubAvatar from '../team/BloubAvatar.svelte';
  import GroupAvatar from '../team/GroupAvatar.svelte';
  import {deviceTypeIconName} from '../../shared/deviceTypeIcon';
  import OpenMenu, {type OpenAnchor, type OpenChoice} from '../../shared/components/OpenMenu.svelte';
  import {MAIN_UI_ICON_SIZE, MAIN_UI_ICON_STROKE_WIDTH} from '../../shared/layout/iconSizing';
  import type {PanelMode} from '../../shared/state/panels';
  import {t, type MessageKey} from '../../../i18n';

  type PinnedView = PinnableWorkspaceView;
  type IconName = ComponentProps<typeof Icon>['name'];

  export let title = '';
  export let showTitle = false;
  export let bot: BotDto | null = null;
  export let teamGroup: TeamGroupDto | null = null;
  export let teamGroupMembers: BotDto[] = [];
  /** A Team conversation can run for a long time on its Host. The title bar is
   * the only chrome every Team pane shares, so the stop control lives here. */
  export let teamRunning = false;
  export let showSummary = false;
  export let hideNewChat = false;
  export let showChatToggle = true;
  export let chatDrawerOpen = false;
  export let mode: PanelMode = 'none';
  export let onRename: (title: string) => void = () => {};
  export let onEditTeam: () => void = () => {};
  export let onStopTeam: () => void = () => {};
  export let onEditTeamGroup: () => void = () => {};
  export let onToggleChatDrawer: () => void = () => {};
  export let onNewChat: () => void = () => {};
  export let onTogglePanel: (mode: 'summary' | 'workspace') => void = () => {};
  export let pinnedViews: PinnedView[] = [];
  export let onOpenView: (kind: PinnedView) => void = () => {};
  export let onOpenViewInNewWindow: (kind: PinnedView) => void = () => {};
  export let onReorderPinnedViews: (views: PinnedView[]) => void = () => {};
  export let showExtensionPrompt = false;
  export let onInstallExtension: () => void = () => {};
  export let onDismissExtension: () => void = () => {};
  export let showUpdatePrompt = false;
  export let onInstallUpdate: () => void = () => {};

  /** Icons that come and go with the drawer or the panel mode fade rather than
   * blink: short enough not to lag the click that caused it, long enough that
   * the row does not look like it is snapping between states. */
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const iconFade = {duration: reducedMotion ? 0 : 120};
  /** Chrome that enters with the drawer — the pinned views — waits for the
   * drawer's edge to travel out past it before appearing — at the narrowest
   * width that edge is still left of the icons for most of the 440ms slide.
   * Leaving is the reverse and already reads right, so only the entrance is
   * held back. */
  const settledFadeIn = {duration: reducedMotion ? 0 : 140, delay: reducedMotion ? 0 : 260};

  let editing = false;
  let draft = '';
  let input: HTMLInputElement;
  let titleButton: HTMLButtonElement;
  let titleControl: HTMLElement;
  let previousCenter: number | null = null;
  let renderedPosition = `${chatDrawerOpen}:${mode}`;
  let positionChanged = false;
  $: titleControl = editing ? input : titleButton;
  $: titleInputSize = Math.max(8, Math.min(48, draft.length + 2));

  beforeUpdate(() => {
    const position = `${chatDrawerOpen}:${mode}`;
    positionChanged = position !== renderedPosition;
    previousCenter = positionChanged && titleControl
      ? titleControl.getBoundingClientRect().left + titleControl.getBoundingClientRect().width / 2
      : null;
  });

  afterUpdate(() => {
    if (!positionChanged || previousCenter === null || !titleControl) return;
    const rect = titleControl.getBoundingClientRect();
    const delta = previousCenter - (rect.left + rect.width / 2);
    renderedPosition = `${chatDrawerOpen}:${mode}`;
    positionChanged = false;
    if (Math.abs(delta) < .5) return;
    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    titleControl.animate(
      [{transform: `translateX(${delta}px)`}, {transform: 'translateX(0)'}],
      {duration: reducedMotion ? 1 : 440, easing: 'cubic-bezier(.45,0,.55,1)'},
    );
  });

  async function startEditing(): Promise<void> {
    draft = title;
    editing = true;
    await tick();
    input?.focus();
    input?.select();
  }

  function save(): void {
    if (!editing) return;
    const next = draft.trim();
    editing = false;
    if (next && next !== title) onRename(next);
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Enter') {
      event.preventDefault();
      save();
    } else if (event.key === 'Escape') {
      event.preventDefault();
      editing = false;
    }
  }

  const pinnedViewIcons: Record<PinnedView, IconName> = {drive: 'drive', calendar: 'calendar', hub: 'chat', tasks: 'tasks', phone: 'phone', locker: 'key', media: 'image', terminal: 'terminal', ide: 'code', finance: 'banknote', usage: 'chart'};
  const pinnedViewLabels: Record<PinnedView, MessageKey> = {drive: 'workspace.drive', calendar: 'workspace.calendar', hub: 'workspace.hub', tasks: 'workspace.tasks', phone: 'workspace.phone', locker: 'workspace.locker', media: 'workspace.media', terminal: 'workspace.terminal', ide: 'workspace.ide', finance: 'workspace.finance', usage: 'workspace.usage'};
  let pinnedMenu: {view: PinnedView; anchor: OpenAnchor} | null = null;
  let pinnedMenuChoices: OpenChoice[];
  $: pinnedMenuChoices = [
    {value: 'unpin', label: $t('titlebar.unpinView'), icon: 'pin-filled'},
    {value: 'new-window', label: $t('titlebar.openSeparateWindow'), icon: 'send'},
  ];
  function openPinnedMenu(event: MouseEvent, view: PinnedView): void {
    event.preventDefault();
    event.stopPropagation();
    pinnedMenu = {view, anchor: {rect: (event.currentTarget as HTMLElement).getBoundingClientRect()}};
  }

  function choosePinnedMenu(value: string): void {
    const view = pinnedMenu?.view;
    pinnedMenu = null;
    if (!view) return;
    if (value === 'unpin') onReorderPinnedViews(pinnedViews.filter((item) => item !== view));
    else if (value === 'new-window') onOpenViewInNewWindow(view);
  }

  let dragIndex: number | null = null;
  let dragOverIndex: number | null = null;

  function onDragStart(event: DragEvent, index: number): void {
    dragIndex = index;
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move';
      event.dataTransfer.setData('text/plain', String(index));
    }
  }

  function onDragOver(event: DragEvent, index: number): void {
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
    dragOverIndex = index;
  }

  function onDrop(event: DragEvent, index: number): void {
    event.preventDefault();
    if (dragIndex !== null && dragIndex !== index) {
      const reordered = [...pinnedViews];
      const [moved] = reordered.splice(dragIndex, 1);
      reordered.splice(index, 0, moved);
      onReorderPinnedViews(reordered);
    }
    dragIndex = null;
    dragOverIndex = null;
  }

  function onDragEnd(): void {
    dragIndex = null;
    dragOverIndex = null;
  }
</script>

<!-- The shell controls and title stay as separate fixed boxes. The title is
     centred against the full window rather than the chat grid, so opening a
     drawer never shifts the conversation name. -->
<div class="left-controls" aria-label={$t('titlebar.chatControls')}>
  {#if showChatToggle}
    <button
      type="button"
      class="title-bar-icon-button"
      aria-label={$t('titlebar.toggleChats')}
      aria-pressed={chatDrawerOpen}
      data-tooltip-label={$t('titlebar.chats')}
      data-tooltip-align="start"
      onclick={onToggleChatDrawer}
    >
      <Icon name="panel-left" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/>
    </button>
  {/if}
  {#if !hideNewChat}
    <button
      type="button"
      class="title-bar-icon-button new-chat-button"
      transition:fade={iconFade}
      aria-label={$t('titlebar.newChat')}
      data-tooltip-label={$t('titlebar.newChat')}
      data-tooltip-align="start"
      onclick={onNewChat}
    >
      <Icon name="new-chat" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/>
    </button>
  {/if}
</div>

<div class="top-controls" aria-label={$t('titlebar.panels')}>
  <!-- Sits ahead of the panel icons so it reads as a notice about the app
       rather than another panel toggle. It is a link with its own dismiss,
       not a toggle, so the two actions are separate controls. -->
  {#if showUpdatePrompt}
    <span class="extension-chip update-chip" transition:fade={iconFade}>
      <button
        type="button"
        class="extension-chip-install"
        aria-label={$t('update.restart')}
        onclick={onInstallUpdate}
      >
        <Icon name="download" size={14}/>
        {$t('update.restart')}
      </button>
    </span>
  {:else if showExtensionPrompt}
    <span class="extension-chip" transition:fade={iconFade}>
      <button
        type="button"
        class="extension-chip-install"
        onclick={onInstallExtension}
      >
        <Icon name="download" size={14}/>
        {$t('extension.install')}
      </button>
      <!-- No tooltip and no hover state: the divider and the glyph already say
           what it does, and a second tooltip next to the chip's own reads as
           two competing labels. Opting out by name is what the search fields'
           × does — dropping the label alone is not enough, since an icon-only
           button falls back to its aria-label. The aria-label stays, so a
           screen reader still names it. -->
      <button
        type="button"
        class="extension-chip-dismiss"
        aria-label={$t('extension.dismiss')}
        data-tooltip="none"
        onclick={onDismissExtension}
      >
        <Icon name="close" size={14}/>
      </button>
    </span>
  {/if}
  {#if mode !== 'workspace'}
    {#each pinnedViews as view, i (view)}
      <button
        type="button"
        class="title-bar-icon-button"
        class:drag-over={dragOverIndex === i && dragIndex !== i}
        draggable={true}
        in:fade={settledFadeIn}
        out:fade={iconFade}
        aria-label={$t(pinnedViewLabels[view])}
        data-tooltip-label={pinnedMenu?.view === view ? undefined : $t(pinnedViewLabels[view])}
        data-tooltip-align="end"
        ondragstart={(e: DragEvent) => onDragStart(e, i)}
        ondragover={(e: DragEvent) => onDragOver(e, i)}
        ondrop={(e: DragEvent) => onDrop(e, i)}
        ondragend={onDragEnd}
        oncontextmenu={(event) => openPinnedMenu(event, view)}
        onclick={() => onOpenView(view)}
      >
        <Icon name={pinnedViewIcons[view]} size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/>
      </button>
    {/each}
  {/if}
  {#if showSummary && mode !== 'workspace'}
    <button
      type="button"
      class="title-bar-icon-button"
      class:active={mode === 'summary'}
      aria-label={$t('titlebar.toggleSummary')}
      transition:fade={iconFade}
      aria-pressed={mode === 'summary'}
      data-tooltip-label={$t('titlebar.summary')}
      data-tooltip-align="end"
      onclick={() => onTogglePanel('summary')}
    >
      <Icon name="summary" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/>
    </button>
  {/if}
  <button
    type="button"
    class="title-bar-icon-button"
    class:active={mode === 'workspace'}
    aria-label={$t('titlebar.toggleWorkspace')}
    aria-pressed={mode === 'workspace'}
    data-tooltip-label={$t('titlebar.workspace')}
    data-tooltip-align="end"
    onclick={() => onTogglePanel('workspace')}
  >
    <Icon name="panel" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/>
  </button>
</div>

<OpenMenu
  choices={pinnedMenuChoices}
  anchor={pinnedMenu?.anchor ?? null}
  onChoose={choosePinnedMenu}
  onClose={() => pinnedMenu = null}
/>


{#if bot || teamGroup}
  <header class="conversation-title-bar team-conversation-title-bar" aria-label={`Conversation with ${bot?.name ?? teamGroup?.name}`}>
    <div class="team-conversation-title">
      <button
        class="team-identity"
        type="button"
        aria-label={bot ? `Edit bot ${bot.name}` : `Edit group ${teamGroup?.name}`}
        aria-haspopup="dialog"
        data-tooltip="none"
        onclick={() => { if (bot) onEditTeam(); else onEditTeamGroup(); }}
      >
      {#if teamGroup}
        <GroupAvatar members={teamGroupMembers} size={26} label={`${teamGroup.name} group avatar`}/>
      {:else if bot}
        <!-- Static like the group avatar: the title bar is chrome, and the
             chat pane already carries the live expression. -->
        <BloubAvatar avatar={bot.avatar} expression="neutral" size={26} animated={false} centerSilhouette paper="var(--app-surface)" label={`${bot.name} avatar`}/>
      {/if}
      <strong>{bot?.name ?? teamGroup?.name}</strong>
      <i aria-hidden="true"></i>
      <span>{#if bot}{bot.role} · <Icon name={deviceTypeIconName(bot.deviceType)} size={12} strokeWidth={1.4}/> {bot.hostName}{:else}{teamGroupMembers.length} agents · {teamGroupMembers.map((member) => member.name).join(', ')}{/if}</span>
      </button>
      {#if teamRunning}
        <button
          type="button"
          class="team-stop"
          aria-label={$t('composer.stopAgent')}
          onclick={onStopTeam}
        ><Icon name="stop" size={13}/><span>{$t('composer.stopAgent')}</span></button>
      {/if}
    </div>
  </header>
{:else if showTitle}
  <header class="conversation-title-bar" aria-label={$t('titlebar.conversationTitle')}>
    {#if editing}
      <input bind:this={input} bind:value={draft} size={titleInputSize} aria-label={$t('titlebar.renameConversation')} onkeydown={keydown} onblur={save}/>
    {:else}
      <button bind:this={titleButton} type="button" title={title} aria-label={$t('titlebar.renameConversationNamed', {title})} data-tooltip="none" onclick={startEditing}>{title}</button>
    {/if}
  </header>
{/if}

<style>
  .team-conversation-title-bar{place-items:center start;box-sizing:border-box;padding:0 16px}
  :global(main:not(.chat-drawer-open)) .team-conversation-title-bar{padding-left:calc(var(--chrome-inset) + 88px)}
  .team-conversation-title{pointer-events:auto;min-width:0;max-width:min(520px,calc(100% - 150px));height:100%;display:flex;align-items:center;gap:9px;overflow:hidden;-webkit-app-region:no-drag}
  .team-conversation-title>.team-identity{all:unset;box-sizing:border-box;min-width:0;display:flex;align-items:center;gap:9px;height:30px;padding:0 7px;border-radius:7px;cursor:pointer;transition:background-color 120ms ease}
  .team-conversation-title>.team-identity:hover,.team-conversation-title>.team-identity:focus-visible{background:var(--neutral-100)}
  .team-conversation-title>.team-identity:focus-visible{outline:2px solid var(--focus-ring);outline-offset:-2px}
  @media (prefers-reduced-motion:reduce){.team-conversation-title>.team-identity{transition:none}}
  .team-identity>strong,.team-identity>span{min-width:0;overflow:hidden;margin:0;text-overflow:ellipsis;white-space:nowrap}
  .team-identity>strong{max-width:220px;color:var(--neutral-950);font-size:13px;font-weight:590;letter-spacing:-.01em}
  .team-identity>i{width:1px;height:14px;flex:none;background:var(--neutral-300)}
  .team-identity>span{max-width:240px;color:var(--neutral-700);font-size:11.5px;font-weight:450}
  /* Stops an in-flight Team run without leaving the conversation. Kept to the
     height of the identity chip so the header still reads as one row. */
  .team-conversation-title>.team-stop{display:flex;align-items:center;gap:6px;height:26px;flex:none;padding:0 11px;border:1px solid var(--neutral-300);border-radius:13px;background:var(--app-surface);color:var(--neutral-900);font:inherit;font-size:11.5px;font-weight:550;cursor:pointer;transition:background-color 120ms ease,border-color 120ms ease}
  .team-conversation-title>.team-stop:hover{background:var(--neutral-100)}
  .team-conversation-title>.team-stop:focus-visible{outline:2px solid var(--focus-ring);outline-offset:2px}
  @media (prefers-reduced-motion:reduce){.team-conversation-title>.team-stop{transition:none}}
  /* The Host's own glyph, drawn inline so the name keeps truncating as one line.
     Centred on the text's x-height, the weight the eye reads in a run that is
     mostly lowercase (the host name has no ascenders), rather than on its cap
     height the way .link-icon does: this glyph is nearly as tall as the role's
     capitals, so a cap-centred one sits visibly high. TeamChatPane draws the
     same line for an empty conversation and must carry the same three values —
     tests/team-identity-alignment.spec.ts holds them together. */
  .team-identity>span :global(svg){display:inline-block;margin-right:3px;vertical-align:calc(.5ex - 6px)}
</style>
