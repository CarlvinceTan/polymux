<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import SidebarBar from './SidebarBar.svelte';
  import ChatList from './ChatList.svelte';
  import ChatPane from './ChatPane.svelte';
  import ProfilePanel from './ProfilePanel.svelte';
  import AvatarEditor from '../avatar/AvatarEditor.svelte';
  import NewChatMenu from './NewChatMenu.svelte';
  import SettingsPanel from './SettingsPanel.svelte';
  import SearchPanel from './SearchPanel.svelte';
  import RowMenu from './RowMenu.svelte';
  import ConnectionStatus from './ConnectionStatus.svelte';
  import WarningBubble from './WarningBubble.svelte';
  import type { Session } from '../session.svelte';
  import type { Agent, Thread } from '../api/contract';
  import { isOffline, reason } from '../api/errors';
  import { isTauri } from '../api/bridge';
  import emptyChat from '../../assets/icons/chat-empty.png';

  let { session, onConnect }: { session: Session; onConnect: (address: string) => Promise<void> } = $props();

  const SIDEBAR_MIN = 260;
  const SIDEBAR_MAX = 400;
  const PROFILE_MIN = 300;
  const PROFILE_MAX = 480;

  let sidebarWidth = $state(clamp(+(localStorage.getItem('sidebarWidth') ?? 320)));
  let sidebarCollapsed = $state(localStorage.getItem('sidebarCollapsed') === '1');
  let profileWidth = $state(clamp(+(localStorage.getItem('profileWidth') ?? 320), PROFILE_MIN, PROFILE_MAX));
  let profileOpen = $state(untrack(() => localStorage.getItem(`lastPanel.${session.address}`) === '1'));
  let newChatMenu = $state(false);
  let newChatKind = $state<'bot' | 'group' | undefined>();
  let newChatSection = $state<string | undefined>();
  let settingsOpen = $state(false);
  let settingsTab = $state('general');
  let searching = $state(false);
  let searchScope = $state<string | undefined>();
  let connectionDismissed = $state(false);
  let searchTarget = $state<{ id: string; threadID: string; messageID?: string; partIndex?: number }>();
  let menuThread = $state<{ thread: Thread; point: { x: number; y: number } } | undefined>(undefined);
  let avatarTarget = $state<Agent>();
  let deleteTarget = $state<{ thread: Thread; agent?: Agent }>();
  let deleting = $state(false);
  let deleteError = $state('');
  let cancelDelete = $state<HTMLButtonElement>();
  let chatList = $state<{ createSection: (thread?: Thread) => void }>();
  let newChatPoint = $state({ x: 168, y: 44 });
  let resizing = $state(false);
  let sidebarDrag: { x: number; width: number; snapped: boolean } | undefined;

  function clamp(value: number, min = SIDEBAR_MIN, max = SIDEBAR_MAX): number {
    return Math.min(max, Math.max(min, value));
  }

  $effect(() => {
    localStorage.setItem('sidebarWidth', String(sidebarWidth));
    localStorage.setItem('sidebarCollapsed', sidebarCollapsed ? '1' : '0');
    localStorage.setItem('profileWidth', String(profileWidth));
    localStorage.setItem(`lastPanel.${session.address}`, profileOpen ? '1' : '0');
  });

  onMount(() => {
    if (!isTauri) return;
    let disposed = false;
    const unlisteners: (() => void)[] = [];
    void import('@tauri-apps/api/event').then(async ({ listen }) => {
      const stops = await Promise.all([
        listen('open-settings', () => { settingsTab = 'general'; settingsOpen = true; }),
        listen('check-updates', () => { settingsTab = 'updates'; settingsOpen = true; }),
      ]);
      if (disposed) stops.forEach((stop) => stop());
      else unlisteners.push(...stops);
    }).catch(() => undefined);
    return () => { disposed = true; unlisteners.forEach((stop) => stop()); };
  });

  const selected = $derived(session.selectedChat);

  $effect(() => { session.notifications.visibleThread = selected?.id; });
  $effect(() => {
    if (!session.notifications.routeID) return;
    const thread = session.notifications.destination;
    session.notifications.destination = undefined;
    if (thread && session.threads.some((entry) => entry.id === thread)) session.selection = thread;
  });

  $effect(() => {
    if (selected && !deleting) void session.open(selected);
  });

  function requestDelete(thread: Thread) {
    if (deleting) return;
    const group = thread.id !== thread.agentID || thread.agentIDs.length > 1;
    const agent = group ? undefined : session.agentsFor(thread)[0];
    if (!group && !agent) return;
    deleteTarget = { thread, agent };
    void tick().then(() => cancelDelete?.focus());
  }

  function deleteKeys(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      deleteTarget = undefined;
    } else if (event.key === 'Tab') {
      const buttons = (event.currentTarget as HTMLElement).querySelectorAll<HTMLButtonElement>('button');
      if (event.shiftKey && document.activeElement === buttons[0]) {
        event.preventDefault();
        buttons[buttons.length - 1]?.focus();
      } else if (!event.shiftKey && document.activeElement === buttons[buttons.length - 1]) {
        event.preventDefault();
        buttons[0]?.focus();
      }
    }
  }

  async function deleteChat(target: { thread: Thread; agent?: Agent }) {
    if (deleting) return;
    deleteTarget = undefined;
    deleteError = '';
    deleting = true;
    session.beginSidebarChange();
    session.threads = session.threads.filter((thread) => thread.id !== target.thread.id);
    let saved = false;
    try {
      if (target.agent) {
        await session.client.deleteAgent(target.agent.id);
        session.agents = session.agents.filter((agent) => agent.id !== target.agent?.id);
      } else {
        await session.client.deleteThread(target.thread.id);
      }
      saved = true;
      if (session.selection === target.thread.id) {
        session.selection = session.selectedChat?.id;
        if (!target.agent) profileOpen = false;
      }
    } catch (error) {
      if (!session.threads.some((thread) => thread.id === target.thread.id)) {
        session.threads = [...session.threads, target.thread];
      }
      deleteError = target.agent
        ? isOffline(error) ? `You’re offline, so ${target.agent.name} wasn’t deleted.` : reason(error, `Couldn’t delete ${target.agent.name}.`)
        : reason(error, 'Couldn’t delete this chat.');
    } finally {
      session.endSidebarChange();
      deleting = false;
      if (saved) void session.refresh().catch(() => {});
    }
  }

  function dragSidebar(event: PointerEvent) {
    if (event.button !== 0 || sidebarCollapsed) return;
    event.preventDefault();
    event.currentTarget instanceof HTMLElement && event.currentTarget.setPointerCapture(event.pointerId);
    sidebarDrag = { x: event.clientX, width: sidebarWidth, snapped: false };
    resizing = true;
  }

  function moveSidebar(event: PointerEvent) {
    if (!sidebarDrag || sidebarDrag.snapped) return;
    const width = sidebarDrag.width + event.clientX - sidebarDrag.x;
    if (width < SIDEBAR_MIN / 2) {
      sidebarDrag.snapped = true;
      resizing = false;
      sidebarCollapsed = true;
    } else {
      sidebarWidth = clamp(width);
    }
  }

  function endSidebar() {
    sidebarDrag = undefined;
    resizing = false;
  }

  function resizeSidebar(event: KeyboardEvent) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End', 'Enter'].includes(event.key)) return;
    event.preventDefault();
    if (event.key === 'Enter') sidebarCollapsed = !sidebarCollapsed;
    else if (event.key === 'Home') sidebarWidth = SIDEBAR_MIN;
    else if (event.key === 'End') sidebarWidth = SIDEBAR_MAX;
    else sidebarWidth = clamp(sidebarWidth + (event.key === 'ArrowRight' ? 10 : -10));
  }

  function maxProfile() {
    return Math.max(PROFILE_MIN, Math.min(PROFILE_MAX, window.innerWidth - (sidebarCollapsed ? 0 : sidebarWidth) - 421));
  }

  function dragProfile(event: PointerEvent) {
    const startX = event.clientX;
    const startWidth = profileWidth;
    const move = (next: PointerEvent) => {
      const width = startWidth - (next.clientX - startX);
      if (width < PROFILE_MIN - 150) {
        profileOpen = false;
        up();
        return;
      }
      profileWidth = Math.min(maxProfile(), Math.max(PROFILE_MIN, width));
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }
</script>

<div class="shell" class:collapsed={sidebarCollapsed} class:resizing class:profile-open={profileOpen && !!selected} style:--sidebar-current="{sidebarCollapsed ? 0 : sidebarWidth}px">
  <div class="sidebar-bar" style:width="{sidebarCollapsed ? 132 : sidebarWidth}px">
    <SidebarBar
      {searching}
      collapsed={sidebarCollapsed}
      onSearch={() => { searchScope = undefined; searching = !searching; }}
      onNewChat={(point) => {
        newChatPoint = point;
        newChatKind = undefined;
        newChatSection = undefined;
        newChatMenu = true;
      }}
      onSettings={() => { settingsTab = 'general'; settingsOpen = true; }}
      onReveal={() => (sidebarCollapsed = false)}
    />
  </div>
  <aside id="sidebar-chats" aria-label="Sidebar" class="sidebar" style:width={sidebarCollapsed ? '0px' : `${sidebarWidth}px`}>
    <div class="sidebar-inner">
      <div class="scroll">
        <ChatList bind:this={chatList} {session} onMenu={(thread, point) => (menuThread = { thread, point })}
          onNewBot={(sectionID) => { newChatSection = sectionID; newChatKind = 'bot'; newChatMenu = true; }} />
      </div>
      {#if !session.threads.length && !session.visibleSections.length}
        <div class="empty-side">No chats yet.</div>
      {/if}
      {#if !selected}<div class="side-status"><ConnectionStatus connected={session.online} bind:dismissed={connectionDismissed} /></div>{/if}
    </div>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (ARIA window splitters are focusable separators.) -->
    <div class="sidebar-handle" role="separator" aria-label="Sidebar width" aria-orientation="vertical"
      aria-controls="sidebar-chats"
      aria-valuemin={SIDEBAR_MIN} aria-valuemax={SIDEBAR_MAX} aria-valuenow={sidebarWidth}
      tabindex={sidebarCollapsed ? -1 : 0} onkeydown={resizeSidebar} onpointerdown={dragSidebar}
      onpointermove={moveSidebar} onpointerup={endSidebar} onpointercancel={endSidebar} onlostpointercapture={endSidebar}
    ></div>
  </aside>

  <main class="chat">
    {#if selected}
      <ChatPane {session} thread={selected} destination={searchTarget} onProfile={() => (profileOpen = !profileOpen)} />
    {:else}
      <div class="empty-chat">
        <div class="empty-icon" aria-hidden="true" style:mask-image="url('{emptyChat}')"></div>
        <span>No chats yet.</span>
        <button class="new-bot glass" onclick={() => { newChatSection = undefined; newChatKind = 'bot'; newChatMenu = true; }}>
          <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M12 5v14M5 12h14"></path>
          </svg>
          New Bot
        </button>
      </div>
    {/if}
    {#if selected}<div class="chat-status"><ConnectionStatus connected={session.online} bind:dismissed={connectionDismissed} /></div>{/if}
    {#if deleteError}<div class="delete-status"><WarningBubble text={deleteError} dismiss={() => deleteError = ''} /></div>{/if}
  </main>

  {#if profileOpen && selected}
    <aside class="profile" style:width="{profileWidth}px">
      <div class="profile-handle" role="separator" aria-orientation="vertical" onpointerdown={dragProfile}
      ></div>
      <ProfilePanel {session} thread={selected} onClose={() => (profileOpen = false)} onSearch={() => { searchScope = selected.id; searching = true; }}
        onAvatar={(agent) => (avatarTarget = agent)}
        onLocate={(messageID, partIndex) => { searchTarget = { id: crypto.randomUUID(), threadID: selected.id, messageID, partIndex }; }} />
    </aside>
  {/if}

  {#if newChatMenu}
    <NewChatMenu
      {session}
      initial={newChatKind}
      sectionID={newChatSection}
      point={newChatPoint}
      onClose={() => (newChatMenu = false)}
      onCreated={(thread) => {
        newChatMenu = false;
        session.selection = thread.id;
      }}
    />
  {/if}

  {#if settingsOpen}
    <SettingsPanel {session} {onConnect} initialTab={settingsTab} onClose={() => (settingsOpen = false)} />
  {/if}

  {#if searching}
    <SearchPanel
      {session}
      scope={searchScope}
      onClose={() => (searching = false)}
      onPick={(thread, result) => {
        searchTarget = { id: crypto.randomUUID(), threadID: thread.id, messageID: result.messageID };
        session.selection = thread.id;
        searching = false;
      }}
    />
  {/if}

  {#if menuThread}
    <RowMenu
      {session}
      thread={menuThread.thread}
      point={menuThread.point}
      onClose={() => (menuThread = undefined)}
      onProfile={() => {
        profileOpen = true;
        menuThread = undefined;
      }}
      onDelete={requestDelete}
      onAvatar={(agent) => (avatarTarget = agent)}
      onCreateSection={(thread) => { menuThread = undefined; chatList?.createSection(thread); }}
    />
  {/if}

  {#if avatarTarget}
    {#key avatarTarget.id}<AvatarEditor {session} agent={avatarTarget} onClose={() => (avatarTarget = undefined)} />{/key}
  {/if}

  {#if deleteTarget}
    {@const target = deleteTarget}
    <div class="confirm-shade">
      <div class="confirm" role="alertdialog" aria-modal="true" aria-label={target.agent ? 'Delete agent?' : 'Delete chat?'} aria-describedby="delete-description" tabindex="-1" onkeydown={deleteKeys}>
        <strong>{target.agent ? 'Delete agent?' : 'Delete chat?'}</strong>
        <p id="delete-description">{target.agent ? `This will permanently remove ${target.agent.name} and their conversation.` : `This permanently removes ${target.thread.title} and its messages. Its bots stay available.`}</p>
        <div class="confirm-actions">
          <button bind:this={cancelDelete} onclick={() => deleteTarget = undefined}>Cancel</button>
          <button class="danger" onclick={() => void deleteChat(target)}>Delete</button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .shell {
    height: 100vh;
    display: flex;
    position: relative;
    overflow: hidden;
  }
  .sidebar {
    position: relative;
    background: var(--sidebar-bg);
    flex: none;
    transition: width 0.22s ease-in-out;
    overflow: visible;
  }
  .sidebar-bar {
    position: absolute;
    top: 0;
    left: 0;
    z-index: 6;
    transition: width 0.22s ease-in-out;
  }
  .resizing .sidebar,
  .resizing .sidebar-bar {
    transition: none;
  }
  .sidebar-inner {
    height: 100%;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    position: relative;
  }
  .scroll {
    flex: 1;
    overflow-y: auto;
    scrollbar-width: none;
    padding-top: var(--titlebar-height);
  }
  .scroll::-webkit-scrollbar {
    display: none;
  }
  .sidebar-handle {
    position: absolute;
    top: 0;
    right: -3px;
    width: 6px;
    height: 100%;
    cursor: col-resize;
    z-index: 5;
    touch-action: none;
  }
  .sidebar-handle:hover {
    background: var(--row-hover);
  }
  .empty-side {
    position: absolute;
    inset: var(--titlebar-height) 0 0;
    display: grid;
    place-items: center;
    pointer-events: none;
    font-size: 13px;
    color: var(--text-muted);
  }
  .side-status, .chat-status { position: absolute; display: flex; justify-content: center; pointer-events: none; }
  .side-status { left: 12px; right: 12px; bottom: 8px; }
  .chat-status { left: 12px; right: 12px; bottom: 70px; }
  .side-status :global(.warning), .chat-status :global(.warning) { pointer-events: auto; }
  .delete-status { position: absolute; bottom: 110px; left: 12px; right: 12px; display: flex; justify-content: center; }
  .confirm-shade { position: fixed; inset: 0; z-index: 300; display: grid; place-items: center; background: rgba(0,0,0,.25); }
  .confirm { width: min(380px, calc(100vw - 48px)); box-sizing: border-box; padding: 20px; border-radius: 16px; background: var(--panel-bg); box-shadow: 0 12px 40px rgba(0,0,0,.24); }
  .confirm p { color: var(--text-muted); line-height: 1.4; }
  .confirm-actions { display: flex; gap: 8px; justify-content: flex-end; }
  .confirm-actions button { padding: 7px 12px; border-radius: 8px; background: var(--glass-field); }
  .confirm-actions .danger { color: var(--danger); }
  .chat {
    flex: 1;
    min-width: var(--chat-min);
    display: flex;
    flex-direction: column;
    background: var(--window-bg);
    position: relative;
  }
  .empty-chat {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
    box-sizing: border-box;
    padding-top: 52px;
    color: var(--text);
  }
  .empty-icon {
    width: 96px;
    height: 75px;
    flex: none;
    background: var(--text);
    opacity: 0.588;
    mask-size: 100% 100%;
  }
  :global(:root[data-mode='dark']) .empty-icon { opacity: 0.647; }
  .empty-chat span {
    color: var(--text-muted);
    transform: translateY(1px);
  }
  .new-bot {
    box-sizing: border-box;
    display: flex;
    align-items: center;
    gap: 6px;
    height: 24px;
    padding: 0 13.5px;
    border-radius: 8px;
    font-size: 13px;
    color: color-mix(in srgb, var(--text) 38%, transparent);
    transform: translate(0.5px, 0.5px);
  }
  :global(:root[data-mode='dark']) .new-bot { color: color-mix(in srgb, var(--text) 32%, transparent); }
  .profile {
    flex: 0 1 auto;
    min-width: 0;
    max-width: max(300px, calc(100vw - var(--sidebar-current) - 421px));
    position: relative;
    background: var(--panel-bg);
    overflow: hidden;
    transition: width 0.2s ease-out;
  }
  .profile-handle {
    position: absolute;
    top: 0;
    left: -4px;
    width: 8px;
    height: 100%;
    cursor: col-resize;
    z-index: 5;
  }
  @media (max-width: 1059px) {
    .shell.profile-open .sidebar,
    .shell.profile-open .sidebar-bar,
    .shell.profile-open .chat { display: none; }
    .shell.profile-open .profile { flex: 1 1 auto; width: 100% !important; max-width: none; }
    .shell.profile-open .profile-handle { display: none; }
  }
  @media (prefers-reduced-motion: reduce) {
    .sidebar,
    .sidebar-bar,
    .profile {
      transition: none;
    }
  }
</style>
