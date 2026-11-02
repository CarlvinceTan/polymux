<script module lang="ts">
  export type ChatDrawerMode = 'assistant' | 'team';
  export type ChatEntry = {
    id: string;
    title: string;
    createdAt: number;
    updatedAt: number;
    running?: boolean;
    avatar?: import('@polymux/protocol').TeamAvatarDto;
    role?: string;
    /** Extra text the search modal matches besides the visible title. */
    keywords?: string;
    /** Conversation message bodies used to render search snippets. */
    searchMessages?: {id: string; text: string}[];
  };
</script>

<script lang="ts">
  import type {TeamGroupDto, BotDto} from '@polymux/protocol';
  import {onDestroy, onMount, tick} from 'svelte';
  import {groupChatsByRecency, type ChatHistoryGroupLabel} from './chatSessions';
  import {
    chatFoldersStorageKey,
    createChatFolder,
    deleteChatFolder,
    folderForChat,
    loadChatFolders,
    moveChatToFolder,
    renameChatFolder,
    toggleChatFolder,
    uniqueChatFolderName,
    type ChatFolder,
  } from './chatFolders';
  import {MAIN_UI_ICON_SIZE, MAIN_UI_ICON_STROKE_WIDTH, SETTINGS_ICON_SIZE, SETTINGS_ICON_STROKE_WIDTH} from '../../shared/layout/iconSizing';
  import {MENU_EDGE_MARGIN, clampToMenuEdge} from '../../shared/layout/menuPlacement';
  import AccountMenu from '../account/AccountMenu.svelte';
  import type {AccountStatusDto} from '@polymux/protocol';
  import {
    MIN_CHAT_DRAWER_WIDTH,
    SPLIT_LAYOUT_MIN_WIDTH,
    chatDrawerOvershootMinimisesWorkspace,
    clampPanelWidth,
    chatDrawerResizeBounds,
  } from '../../shared/layout/layoutSizing';
  import TeamRoleLabel from '../team/TeamRoleLabel.svelte';
  import {scrollFade} from '../../shared/scrollFade';
  import Icon from '../../shared/components/Icon.svelte';
  import BloubAvatar from '../team/BloubAvatar.svelte';
  import GroupAvatar from '../team/GroupAvatar.svelte';
  import {bloubActivityForTeamStatus, bloubExpressionForTeamStatus} from '../team/bloub/expression';
  import {chatPinsStorageKey, loadChatPins, toggleChatPin} from './chatPins';
  import ChatListMenu from './ChatListMenu.svelte';
  import {
    chatListPreferencesStorageKey,
    filterChatList,
    loadChatListPreferences,
    saveChatListPreferences,
    sortChatList,
    type ChatListPreferences,
  } from './chatListPreferences';
  import {activeLocale, plural, t, translate, type MessageKey} from '../../../i18n';
  import {clockTime} from '../../shared/displayTime';

  const SECTION_CHEVRON_SIZE = 22;
  // At 22px, this draws a 1.28px stroke for a slightly stronger section disclosure.
  const SECTION_CHEVRON_STROKE_WIDTH = 1.4;

  /** The group label doubles as the section's identity — which sections are
   * collapsed is keyed by it — so it stays an English constant and only the
   * wording on screen comes from the catalog. */
  const groupLabels: Record<ChatHistoryGroupLabel, MessageKey> = {
    'Today': 'chats.today',
    'Yesterday': 'chats.yesterday',
    'This week': 'chats.thisWeek',
    'Last week': 'chats.lastWeek',
    'This month': 'chats.thisMonth',
    'Last month': 'chats.lastMonth',
    'Earlier': 'chats.earlier',
  };
  const foldersSectionId = 'folder:folders';

  export let chats: ChatEntry[] = [];
  export let mode: ChatDrawerMode = 'assistant';
  export let bots: BotDto[] = [];
  export let teamGroups: TeamGroupDto[] = [];
  export let activeId = '';
  export let open = false;
  export let width = 280;
  export let resizing = false;
  export let reservedWidth = 0;
  export let onOpen: (id: string) => void = () => {};
  export let onRename: (id: string, title: string) => void = () => {};
  export let onShare: (id: string) => void = () => {};
  export let onDuplicate: (id: string) => Promise<void> = async () => {};
  export let onArchive: (id: string) => void = () => {};
  export let onResize: (width: number) => void = () => {};
  export let onResizeState: (resizing: boolean) => void = () => {};
  /** True while the workspace is filling the remaining column. An overshoot
   * past the chat max then becomes a minimise, not a stuck ceiling. */
  export let workspaceExpanded = false;
  export let onMinimiseWorkspace: () => void = () => {};
  export let onModeChange: (mode: ChatDrawerMode) => void = () => {};
  export let onOpenTeam: (id: string) => void = () => {};
  export let onOpenTeamGroup: (id: string) => void = () => {};
  export let onAddTeam: () => void = () => {};
  export let onNewBot: () => void = () => {};
  export let onEditTeam: (id: string) => void = () => {};
  export let onEditTeamGroup: (id: string) => void = () => {};
  export let onDeleteTeam: (id: string) => void = () => {};
  export let onDeleteTeamGroup: (id: string) => void = () => {};
  export let account: AccountStatusDto | null = null;
  /** The gear toggles Settings closed when that view is already expanded. */
  export let onToggleSettings: () => void = () => {};
  export let settingsExpanded = false;
  export let onOpenConnections: () => void = () => {};
  export let connectionsExpanded = false;
  export let onOpenHost: (anchor: HTMLButtonElement) => void = () => {};
  export let onSignIn: () => void = () => {};
  export let onSignOut: () => void = () => {};
  export let onDocumentation: () => void;
  export let onNewFolderChat: (folderId: string) => void = () => {};
  export let onNewChat: () => void = () => {};
  export let onSearchChats: () => void = () => {};

  let drawer: HTMLElement;
  let menu: {kind: 'chat' | 'folder' | 'team' | 'team-group'; id: string} | null = null;
  let menuElement: HTMLDivElement | undefined;
  let menuAnchor:
    | {kind: 'trigger'; left: number; right: number; bottom: number}
    | {kind: 'point'; x: number; y: number}
    | null = null;
  let menuLeft = 0;
  let menuTop = 0;
  let menuPlaced = false;
  let moveMenuOpen = false;
  let moveMenu: HTMLDivElement;
  let moveTrigger: HTMLButtonElement;
  let moveLeft = 0;
  let moveTop = 0;
  let movePlaced = false;
  let duplicateError = '';
  let draggedChatId: string | null = null;
  let dropFolderId: string | null = null;
  let dropUnfiledList = false;
  let dropPinnedList = false;

  async function showMoveMenu(focus = false): Promise<void> {
    moveMenuOpen = true;
    movePlaced = false;
    await tick();
    if (!menuElement || !moveMenu || !moveTrigger) return;
    const parent = menuElement.getBoundingClientRect();
    const anchor = moveTrigger.getBoundingClientRect();
    const bounds = drawer.getBoundingClientRect();
    const child = moveMenu.getBoundingClientRect();
    const left = parent.right + 5 + child.width <= window.innerWidth - MENU_EDGE_MARGIN
      ? parent.right + 5 : parent.left - child.width - 5;
    moveLeft = clampToMenuEdge(left, child.width, window.innerWidth) - bounds.left;
    moveTop = clampToMenuEdge(anchor.top + anchor.height / 2 - child.height / 2,
      child.height, window.innerHeight) - bounds.top;
    movePlaced = true;
    await tick();
    if (focus) moveMenu.querySelector<HTMLButtonElement>('button')?.focus();
  }

  function moveKeys(event: KeyboardEvent): void {
    if (event.key === 'ArrowRight' && event.target === moveTrigger) {
      event.preventDefault(); event.stopPropagation(); void showMoveMenu(true);
    } else if (moveMenuOpen && (event.key === 'ArrowLeft' || event.key === 'Escape')) {
      event.preventDefault(); event.stopPropagation(); moveMenuOpen = false; moveTrigger?.focus();
    } else if (moveMenuOpen && moveMenu?.contains(event.target as Node) && ['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      const buttons = Array.from(moveMenu.querySelectorAll<HTMLButtonElement>('button'));
      const index = buttons.indexOf(event.target as HTMLButtonElement);
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1
        : (index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length;
      event.preventDefault(); buttons[next]?.focus();
    }
  }

  async function duplicateChat(id: string): Promise<void> {
    menu = null;
    duplicateError = '';
    try { await onDuplicate(id); }
    catch (error) { duplicateError = error instanceof Error ? error.message : 'Could not duplicate chat'; }
  }

  function dropChat(event: DragEvent, folderId: string): void {
    if (!draggedChatId) return;
    event.stopPropagation();
    event.preventDefault();
    if (pins.chats.includes(draggedChatId)) {
      pins = toggleChatPin(pins, 'chats', draggedChatId);
    }
    moveChat(draggedChatId, folderId);
    draggedChatId = null;
    dropFolderId = null;
    dropUnfiledList = false;
    dropPinnedList = false;
  }

  function dragOverFolder(event: DragEvent, folderId: string): void {
    if (!draggedChatId) return;
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer!.dropEffect = 'move';
    dropFolderId = folderId;
    dropUnfiledList = false;
    dropPinnedList = false;
  }

  function dragLeaveFolder(event?: DragEvent | string, folderId?: string): void {
    if (typeof event === 'string') {
      if (dropFolderId === event) dropFolderId = null;
      return;
    }
    if (event?.currentTarget && event.relatedTarget && (event.currentTarget as HTMLElement).contains(event.relatedTarget as Node)) return;
    if (!folderId || dropFolderId === folderId) dropFolderId = null;
  }

  function dragOverUnfiledList(event: DragEvent): void {
    if (!draggedChatId) return;
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer!.dropEffect = 'move';
    dropFolderId = null;
    dropUnfiledList = true;
    dropPinnedList = false;
  }

  function dragLeaveUnfiledList(event?: DragEvent): void {
    if (event?.currentTarget && event.relatedTarget && (event.currentTarget as HTMLElement).contains(event.relatedTarget as Node)) return;
    dropUnfiledList = false;
  }

  function dropUnfiledChat(event: DragEvent): void {
    if (!draggedChatId) return;
    event.preventDefault();
    event.stopPropagation();
    if (pins.chats.includes(draggedChatId)) {
      pins = toggleChatPin(pins, 'chats', draggedChatId);
    }
    moveChat(draggedChatId, null);
    draggedChatId = null;
    dropFolderId = null;
    dropUnfiledList = false;
    dropPinnedList = false;
  }

  function dragOverPinnedList(event: DragEvent): void {
    if (!draggedChatId) return;
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer!.dropEffect = 'move';
    dropFolderId = null;
    dropUnfiledList = false;
    dropPinnedList = true;
  }

  function dragLeavePinnedList(event?: DragEvent): void {
    if (event?.currentTarget && event.relatedTarget && (event.currentTarget as HTMLElement).contains(event.relatedTarget as Node)) return;
    dropPinnedList = false;
  }

  function dropPinnedChat(event: DragEvent): void {
    if (!draggedChatId) return;
    event.preventDefault();
    event.stopPropagation();
    if (!pins.chats.includes(draggedChatId)) {
      pins = toggleChatPin(pins, 'chats', draggedChatId);
    }
    draggedChatId = null;
    dropFolderId = null;
    dropUnfiledList = false;
    dropPinnedList = false;
  }

  let editingId: string | null = null;
  let draft = '';
  let renameInput: HTMLInputElement;
  let creatingFolder = false;
  let editingFolderId: string | null = null;
  let folderDraft = '';
  let folderInput: HTMLInputElement;
  let pins = loadChatPins();
  let folders: ChatFolder[] = loadChatFolders();
  let chatListPreferences = loadChatListPreferences();
  let collapsedSections = new Set<string>();
  let sectionsInitialised = false;
  let pointerResizing = false;
  let pendingResizeX: number | null = null;
  let resizeFrame = 0;

  $: folderedChatIds = new Set(folders.flatMap((folder) => folder.chatIds));
  $: visibleChats = filterChatList(chats, chatListPreferences.filter, folderedChatIds);
  $: pinnedChats = sortChatList(visibleChats.filter((chat) => pins.chats.includes(chat.id)), chatListPreferences.sortBy, chatListPreferences.order);
  $: pinnedFolderChatIds = new Set(folders.filter((folder) => pins.folders.includes(folder.id)).flatMap((folder) => folder.chatIds));
  $: regularChats = visibleChats.filter((chat) => !pins.chats.includes(chat.id) && !pinnedFolderChatIds.has(chat.id));
  $: sortedVisibleChats = sortChatList(regularChats, chatListPreferences.sortBy, chatListPreferences.order);
  $: visibleChatIds = new Set(visibleChats.map((chat) => chat.id));
  $: allFolderViews = folders
      .map((folder) => ({
        folder,
        chats: sortChatList(
          chats.filter((chat) => folder.chatIds.includes(chat.id) && visibleChatIds.has(chat.id) && !pins.chats.includes(chat.id)),
          chatListPreferences.sortBy,
          chatListPreferences.order,
        ),
      }))
      .filter((view) => chatListPreferences.filter === 'all' || view.chats.length);
  $: pinnedFolderViews = allFolderViews.filter((view) => pins.folders.includes(view.folder.id));
  $: folderViews = chatListPreferences.groupBy === 'folder' ? allFolderViews.filter((view) => !pins.folders.includes(view.folder.id)) : [];
  $: dateSource = chatListPreferences.groupBy === 'date' ? regularChats : [];
  $: dateSections = (() => {
    const sections = groupChatsByRecency(dateSource).map((group) => ({
      id: `date:${group.label}`,
      label: $t(groupLabels[group.label]),
      chats: sortChatList(group.chats, chatListPreferences.sortBy, chatListPreferences.order),
    }));
    return chatListPreferences.sortBy === 'activity' && chatListPreferences.order === 'ascending'
      ? sections.reverse()
      : sections;
  })();
  $: chatSections = chatListPreferences.groupBy === 'date'
    ? dateSections
    : chatListPreferences.groupBy === 'folder'
      ? [{id: 'folder:unfiled', label: $t('titlebar.chats'), chats: sortedVisibleChats.filter((chat) => !folderedChatIds.has(chat.id))}]
      : chatListPreferences.groupBy === 'state'
        ? [
          {id: 'state:running', label: $t('chats.stateRunning'), chats: sortedVisibleChats.filter((chat) => chat.running)},
          {id: 'state:idle', label: $t('chats.stateIdle'), chats: sortedVisibleChats.filter((chat) => !chat.running)},
        ].filter((section) => section.chats.length)
        : [];
  $: flatChats = chatListPreferences.groupBy === 'none' ? sortedVisibleChats : [];
  // Folder grouping always keeps the Folders and Chats section titles on
  // screen, even when either list is empty. New folder rides Folders; new
  // chat rides Chats. Pinned still appears only when something is pinned.
  $: folderMode = chatListPreferences.groupBy === 'folder';
  // The drawer opens showing only the most recent group, so the newest chats
  // are readable without scrolling. This runs once per launch, as soon as the
  // chat list has loaded; from then on the sections keep whatever the user set.
  $: if (!sectionsInitialised && dateSections.length) {
    collapsedSections = new Set(dateSections.slice(1).map((section) => section.id));
    sectionsInitialised = true;
  }

  onMount(() => {
    const syncFolders = (event: StorageEvent) => {
      if (event.key === chatPinsStorageKey) pins = loadChatPins();
      if (event.key === chatFoldersStorageKey) folders = loadChatFolders();
      if (event.key === chatListPreferencesStorageKey) chatListPreferences = loadChatListPreferences();
    };
    window.addEventListener('storage', syncFolders);
    return () => window.removeEventListener('storage', syncFolders);
  });

  function toggleSection(id: string): void {
    const next = new Set(collapsedSections);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    collapsedSections = next;
    menu = null;
  }

  function setChatListPreferences(preferences: ChatListPreferences): void {
    if (preferences.groupBy !== 'folder') {
      creatingFolder = false;
      editingFolderId = null;
    }
    chatListPreferences = saveChatListPreferences(preferences);
    menu = null;
  }

  function resizeFromPointer(clientX: number): void {
    onResize(clampPanelWidth(clientX, chatDrawerResizeBounds(window.innerWidth, reservedWidth)));
  }

  /** Pointer events can arrive several times between paints. Keep the newest
   * sample and perform one layout update per frame so the divider follows the
   * pointer without making the rest of the shell repeatedly reflow. */
  function latestClientX(event: PointerEvent): number {
    const samples = event.getCoalescedEvents?.();
    return samples?.length ? samples[samples.length - 1].clientX : event.clientX;
  }

  function queuePointerResize(clientX: number): void {
    pendingResizeX = clientX;
    if (resizeFrame) return;
    resizeFrame = requestAnimationFrame(() => {
      resizeFrame = 0;
      if (!pointerResizing || pendingResizeX === null) return;
      const nextX = pendingResizeX;
      pendingResizeX = null;
      resizeFromPointer(nextX);
    });
  }

  function flushPointerResize(clientX: number): void {
    if (resizeFrame) cancelAnimationFrame(resizeFrame);
    resizeFrame = 0;
    pendingResizeX = null;
    resizeFromPointer(clientX);
  }

  function crossedOvershootThreshold(clientX: number): boolean {
    return chatDrawerOvershootMinimisesWorkspace(
      clientX,
      window.innerWidth,
      workspaceExpanded,
      reservedWidth,
    );
  }

  function finishPointerResize(event: PointerEvent, minimise: boolean): void {
    if (resizeFrame) cancelAnimationFrame(resizeFrame);
    resizeFrame = 0;
    pendingResizeX = null;
    pointerResizing = false;
    onResizeState(false);
    (event.currentTarget as HTMLElement).releasePointerCapture?.(event.pointerId);
    if (minimise) {
      onResize(MIN_CHAT_DRAWER_WIDTH);
      onMinimiseWorkspace();
    }
  }

  function startResize(event: PointerEvent): void {
    if (window.innerWidth < SPLIT_LAYOUT_MIN_WIDTH) return;
    pointerResizing = true;
    onResizeState(true);
    (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
    flushPointerResize(latestClientX(event));
    event.preventDefault();
  }

  function dragResize(event: PointerEvent): void {
    if (!pointerResizing) return;
    const clientX = latestClientX(event);
    if (crossedOvershootThreshold(clientX)) finishPointerResize(event, true);
    else queuePointerResize(clientX);
    event.preventDefault();
  }

  function stopResize(event: PointerEvent): void {
    if (!pointerResizing) return;
    const clientX = latestClientX(event);
    const minimise = event.type !== 'pointercancel' && crossedOvershootThreshold(clientX);
    if (!minimise) flushPointerResize(clientX);
    finishPointerResize(event, minimise);
  }

  onDestroy(() => {
    if (resizeFrame) cancelAnimationFrame(resizeFrame);
  });

  function resizeWithKeyboard(event: KeyboardEvent): void {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    const delta = event.key === 'ArrowRight' ? 16 : -16;
    onResize(clampPanelWidth(drawer.getBoundingClientRect().width + delta, chatDrawerResizeBounds(window.innerWidth, reservedWidth)));
    event.preventDefault();
  }

  /** Aligns the menu's top-left corner below its trigger, allowing it to
   * extend beyond the drawer while keeping its measured size on screen. */
  function placeRowMenu(): void {
    if (!menu || !menuElement || !menuAnchor) return;
    const drawerRect = drawer.getBoundingClientRect();
    const {width, height} = menuElement.getBoundingClientRect();
    if (menuAnchor.kind === 'trigger') {
      menuLeft = clampToMenuEdge(menuAnchor.left, width, window.innerWidth) - drawerRect.left;
      menuTop = Math.max(8, Math.min(
        menuAnchor.bottom - drawerRect.top + 4,
        drawerRect.height - height - 10,
      ));
      menuPlaced = true;
      return;
    }
    const pointerX = menuAnchor.x - drawerRect.left;
    const pointerY = menuAnchor.y - drawerRect.top;
    const preferredLeft = menuAnchor.x + width <= window.innerWidth - MENU_EDGE_MARGIN
      ? pointerX
      : pointerX - width;
    const preferredTop = menuAnchor.y + height <= window.innerHeight - MENU_EDGE_MARGIN
      ? pointerY
      : pointerY - height;
    menuLeft = clampToMenuEdge(preferredLeft + drawerRect.left, width, window.innerWidth) - drawerRect.left;
    menuTop = clampToMenuEdge(preferredTop + drawerRect.top, height, window.innerHeight) - drawerRect.top;
    menuPlaced = true;
  }

  function toggleMenu(event: MouseEvent, kind: 'chat' | 'folder' | 'team' | 'team-group', id: string): void {
    event.stopPropagation();
    moveMenuOpen = false;
    if (menu?.kind === kind && menu.id === id) {
      menu = null;
      return;
    }
    const triggerRect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    menuAnchor = {
      kind: 'trigger',
      left: triggerRect.left,
      right: triggerRect.right,
      bottom: triggerRect.bottom,
    };
    menuPlaced = false;
    menu = {kind, id};
    void tick().then(placeRowMenu);
  }

  function openContextMenu(event: MouseEvent, kind: 'chat' | 'folder' | 'team' | 'team-group', id: string): void {
    event.preventDefault();
    event.stopPropagation();
    moveMenuOpen = false;
    menuAnchor = {kind: 'point', x: event.clientX, y: event.clientY};
    menuPlaced = false;
    menu = {kind, id};
    void tick().then(placeRowMenu);
  }

  async function startRename(chat: ChatEntry): Promise<void> {
    menu = null;
    editingId = chat.id;
    draft = chat.title;
    await tick();
    renameInput?.focus();
    renameInput?.select();
  }

  function saveRename(id: string): void {
    if (editingId !== id) return;
    const title = draft.trim();
    editingId = null;
    if (title) onRename(id, title);
  }

  function renameKeydown(event: KeyboardEvent, id: string): void {
    if (event.key === 'Enter') {
      event.preventDefault();
      saveRename(id);
    } else if (event.key === 'Escape') {
      event.preventDefault();
      editingId = null;
    }
  }

  function archiveChat(id: string): void {
    menu = null;
    onArchive(id);
  }

  async function startCreateFolder(): Promise<void> {
    menu = null;
    editingFolderId = null;
    creatingFolder = true;
    folderDraft = uniqueChatFolderName(folders, $t('chats.newFolder'));
    if (collapsedSections.has(foldersSectionId)) {
      const next = new Set(collapsedSections);
      next.delete(foldersSectionId);
      collapsedSections = next;
    }
    await tick();
    folderInput?.focus();
    folderInput?.select();
  }

  async function startRenameFolder(folder: ChatFolder): Promise<void> {
    menu = null;
    creatingFolder = false;
    editingFolderId = folder.id;
    folderDraft = folder.name;
    await tick();
    folderInput?.focus();
    folderInput?.select();
  }

  function saveFolder(): void {
    if (!creatingFolder) return;
    const name = folderDraft.trim();
    creatingFolder = false;
    if (name) folders = createChatFolder(folders, crypto.randomUUID(), name);
  }

  function saveFolderRename(id: string): void {
    if (editingFolderId !== id) return;
    const name = folderDraft.trim();
    editingFolderId = null;
    if (name) folders = renameChatFolder(folders, id, name);
  }

  function folderKeydown(event: KeyboardEvent, id: string | null): void {
    if (event.key === 'Enter') {
      event.preventDefault();
      if (id) saveFolderRename(id); else saveFolder();
    } else if (event.key === 'Escape') {
      event.preventDefault();
      creatingFolder = false;
      editingFolderId = null;
    }
  }

  function moveChat(chatId: string, folderId: string | null): void {
    folders = moveChatToFolder(folders, chatId, folderId);
    menu = null;
  }

  function removeFolder(id: string): void {
    folders = deleteChatFolder(folders, id);
    menu = null;
  }

  function dismiss(event: MouseEvent | KeyboardEvent): void {
    if (event instanceof KeyboardEvent && event.key !== 'Escape') return;
    if (event instanceof MouseEvent && (event.target as Element).closest('.chat-drawer-more,.chat-drawer-row-menu')) return;
    menu = null;
  }

  /** A team row's stamp, in the interface language and on the shared 12-hour
   * clock: the time for today, the date beyond it. */
  function teamTime(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    if (date.toDateString() === new Date().toDateString()) return clockTime(date);
    return new Intl.DateTimeFormat(activeLocale(), {month: 'short', day: 'numeric'}).format(date);
  }

  function teamStatus(member: BotDto): string {
    if (member.status === 'working') return translate('team.statusWorking');
    if (member.status === 'waiting-for-device') return translate('team.statusWaitingForAccess');
    if (member.status === 'computer-offline') return translate('team.statusOffline');
    if (member.status === 'error') return member.computer.detail || translate('team.statusNeedsAttention');
    return '';
  }

  function teamPreview(member: BotDto): string {
    const preview = member.preview.trim();
    return preview && preview !== member.role ? preview : translate('team.noMessagesYet');
  }

  function groupMembers(group: TeamGroupDto): BotDto[] {
    return group.memberIds.flatMap((id) => bots.find((member) => member.id === id) ?? []);
  }

  function groupStatus(group: TeamGroupDto): string {
    const working = groupMembers(group).filter((member) => member.status === 'working').length;
    return working
      ? plural('team.workingCount', working)
      : plural('team.agentCount', group.memberIds.length);
  }
</script>

<svelte:window onclick={dismiss} onkeydown={dismiss} onresize={() => {menu = null; moveMenuOpen = false;}}/>

<!-- The drawer keeps its resting width while closed and slides in as a finished
     sheet, so its contents never relayout mid-animation. `inert` while closed
     keeps it out of the tab order without needing a separate mount. -->
<aside
  bind:this={drawer}
  class:open
  class:resizing
  class="chat-drawer"
  style:--chat-drawer-panel-width={`${width}px`}
  aria-label="Assistant and Team"
  aria-hidden={!open}
  inert={!open}
>
  <button
    type="button"
    class="chat-drawer-resize-handle"
    aria-label={$t('chats.resize')}
    tabindex={open ? 0 : -1}
    data-tooltip="none"
    onpointerdown={startResize}
    onpointermove={dragResize}
    onpointerup={stopResize}
    onpointercancel={stopResize}
    onkeydown={resizeWithKeyboard}
  ></button>
  <div class="chat-drawer-heading">
    <nav class="chat-drawer-mode-switch" aria-label="Conversation type">
      <button type="button" class:active={mode === 'assistant'} aria-current={mode === 'assistant' ? 'page' : undefined} onclick={() => onModeChange('assistant')}><span>Assistant</span><span class="mode-width" aria-hidden="true">Assistant</span></button>
      <i aria-hidden="true"></i>
      <button type="button" class:active={mode === 'team'} aria-current={mode === 'team' ? 'page' : undefined} onclick={() => onModeChange('team')}><span>Team</span><span class="mode-width" aria-hidden="true">Team</span></button>
    </nav>
    <span class="chat-drawer-heading-actions">
      <button
        type="button"
        class="chat-drawer-new-folder"
        aria-label={$t('titlebar.searchChats')}
        data-tooltip-label={$t('titlebar.searchChats')}
        onclick={onSearchChats}
      ><Icon name="search" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/></button>
      {#if mode === 'team'}
        <button
          type="button"
          class="chat-drawer-new-folder"
          aria-label="New chat"
          data-tooltip-label="New chat"
          onclick={onAddTeam}
        ><Icon name="plus" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/></button>
      {:else}
        <ChatListMenu preferences={chatListPreferences} onChange={setChatListPreferences}/>
      {/if}
    </span>
  </div>

  <div class="chat-drawer-body">
  {#if mode === 'team'}
    {#if teamGroups.length || bots.length}
      <div class="chat-drawer-team-list" use:scrollFade={bots.length + teamGroups.length} onscroll={() => menu = null}>
        {#if teamGroups.length}
          <h2 class="chat-drawer-team-section">Groups</h2>
          <ul aria-label="Team groups">
            {#each teamGroups as group (group.id)}
              {@const members = groupMembers(group)}
              <li>
                <div
                  class:active={group.conversationId === activeId}
                  class:menu-open={menu?.kind === 'team-group' && menu.id === group.id}
                  class="chat-drawer-team-group-row"
                  role="group"
                  oncontextmenu={(event) => openContextMenu(event, 'team-group', group.id)}
                >
                  <button class="chat-drawer-team-avatar-trigger" type="button" aria-label={`Options for ${group.name}`} aria-haspopup="menu" aria-expanded={menu?.kind === 'team-group' && menu.id === group.id} onclick={(event) => toggleMenu(event, 'team-group', group.id)}>
                    <span class="chat-drawer-team-avatar"><GroupAvatar {members} size={39} label={`${group.name} group avatar`}/></span>
                  </button>
                  <button class="chat-drawer-open-team" type="button" aria-label={`Open group ${group.name}`} aria-current={group.conversationId === activeId ? 'page' : undefined} onclick={() => onOpenTeamGroup(group.id)}>
                    <span class="chat-drawer-team-copy">
                      <span class="chat-drawer-team-top"><strong>{group.name}</strong><small>{groupStatus(group)}</small><time datetime={group.updatedAt}>{teamTime(group.updatedAt)}</time></span>
                      <span class="chat-drawer-team-bottom"><span>{group.preview}</span>{#if group.unread}<i class="chat-drawer-team-unread" aria-label={`${group.unreadCount} unread`}></i>{/if}</span>
                    </span>
                  </button>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
        {#if bots.length}
          <h2 class="chat-drawer-static-section">{$t('titlebar.chats')}</h2>
        {/if}
        <ul aria-label="Team chats">
          {#each bots as member (member.id)}
            <li>
              <div
                class:active={member.conversationId === activeId}
                class:menu-open={menu?.kind === 'team' && menu.id === member.id}
                class:working={member.status === 'working'}
                class:attention={member.status === 'waiting-for-device' || member.status === 'computer-offline'}
                class:error={member.status === 'error'}
                class="chat-drawer-team-row"
                role="group"
                oncontextmenu={(event) => openContextMenu(event, 'team', member.id)}
              >
                <button
                  class="chat-drawer-team-avatar-trigger"
                  type="button"
                  aria-label={`Options for ${member.name}`}
                  aria-haspopup="menu"
                  aria-expanded={menu?.kind === 'team' && menu.id === member.id}
                  onclick={(event) => toggleMenu(event, 'team', member.id)}
                >
                  <span class="chat-drawer-team-avatar">
                    <BloubAvatar avatar={member.avatar} expression={bloubExpressionForTeamStatus(member.status, member.preview)} activity={bloubActivityForTeamStatus(member.status)} size={39} label={`${member.name} avatar`}/>
                    <i class:working={member.status === 'working'} class:waiting={member.status === 'waiting-for-device'} class:offline={member.status === 'computer-offline'} class:error={member.status === 'error'} aria-hidden="true"></i>
                  </span>
                </button>
                <button
                  class="chat-drawer-open-team"
                  type="button"
                  aria-label={`Open ${member.name}, ${member.role}`}
                  aria-current={member.conversationId === activeId ? 'page' : undefined}
                  onclick={() => onOpenTeam(member.id)}
                >
                  <span class="chat-drawer-team-copy">
                    <span class="chat-drawer-team-top"><strong>{member.name}</strong><TeamRoleLabel role={member.role} raised={member.conversationId === activeId || (menu?.kind === 'team' && menu.id === member.id)}/><time datetime={member.updatedAt}>{teamTime(member.updatedAt)}</time></span>
                    <span class="chat-drawer-team-bottom">
                      {#if teamStatus(member)}<em>{teamStatus(member)}</em><i class="chat-drawer-team-separator" aria-hidden="true"></i>{/if}
                      <span>{teamPreview(member)}</span>
                      {#if member.unread}<i class="chat-drawer-team-unread" aria-label="Unread"></i>{/if}
                    </span>
                  </span>
                </button>
              </div>
            </li>
          {/each}
        </ul>
      </div>
    {:else}
      <div class="chat-drawer-team-empty">
        <span class="chat-drawer-team-empty-avatar"><BloubAvatar avatar={{shape: 'pebble', color: '#0a0a0c'}} expression="curious" size={46} monochrome/></span>
        <strong>No bots yet</strong>
        <p>Add a bot with its own role, agent and computer.</p>
        <button type="button" onclick={onNewBot}>Add bot</button>
      </div>
    {/if}
  {:else}
    <div class="chat-drawer-groups" role="presentation" use:scrollFade={chats.length} onwheel={() => menu = null} ontouchmove={() => menu = null}>
      {#if pinnedChats.length || pinnedFolderViews.length}
        <section
          class="chat-drawer-group"
          role="group"
          class:chat-drawer-chat-list-drop-target={dropPinnedList}
          ondragover={dragOverPinnedList}
          ondragleave={dragLeavePinnedList}
          ondrop={dropPinnedChat}
        >
          <button class="chat-drawer-group-toggle" class:collapsed={collapsedSections.has('pinned')} type="button" aria-expanded={!collapsedSections.has('pinned')} onclick={() => toggleSection('pinned')}>
            <span>{$t('hub.pinned')}</span><Icon name="chevron" size={SECTION_CHEVRON_SIZE} strokeWidth={SECTION_CHEVRON_STROKE_WIDTH}/>
          </button>
          {#if !collapsedSections.has('pinned')}
            <ul
              aria-label={$t('hub.pinned')}
              ondragover={dragOverPinnedList}
              ondragleave={dragLeavePinnedList}
              ondrop={dropPinnedChat}
            >
              {#each pinnedFolderViews as view (view.folder.id)}{@render folderRow(view)}{/each}
              {#each pinnedChats as chat (chat.id)}<li>{@render chatRow(chat)}</li>{/each}
            </ul>
          {/if}
        </section>
      {/if}
      {#if folderMode}
        <section class="chat-drawer-group" role="group">
          <div class="chat-drawer-section-head">
            <button
              class:collapsed={collapsedSections.has(foldersSectionId)}
              class="chat-drawer-group-toggle"
              type="button"
              aria-expanded={!collapsedSections.has(foldersSectionId)}
              onclick={() => toggleSection(foldersSectionId)}
            >
              <span>{$t('chats.folders')}</span>
              <Icon name="chevron" size={SECTION_CHEVRON_SIZE} strokeWidth={SECTION_CHEVRON_STROKE_WIDTH}/>
            </button>
            <button
              type="button"
              class="chat-drawer-new-folder"
              aria-label={$t('chats.newFolder')}
              data-tooltip-label={$t('chats.newFolder')}
              onclick={startCreateFolder}
            ><Icon name="folder-plus" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/></button>
          </div>
          {#if !collapsedSections.has(foldersSectionId) && creatingFolder}
            <div class="chat-drawer-folder-edit">
              <Icon name="folder" size={17}/>
              <input
                bind:this={folderInput}
                bind:value={folderDraft}
                aria-label={$t('chats.folderName')}
                placeholder={$t('chats.folderName')}
                onkeydown={(event) => folderKeydown(event, null)}
                onblur={saveFolder}
              />
            </div>
          {/if}
          {#if !collapsedSections.has(foldersSectionId) && folderViews.length}
            <ul class="chat-drawer-folders" aria-label={$t('chats.folders')}>
              {#each folderViews as view (view.folder.id)}
                {@render folderRow(view)}
              {/each}
            </ul>
          {:else if !collapsedSections.has(foldersSectionId) && !creatingFolder && folders.length === 0}
            <p class="chat-drawer-section-empty">{$t('chats.noFolders')}</p>
          {/if}
        </section>
      {/if}

      {#if flatChats.length}
        <ul
          class="chat-drawer-flat-list"
          class:chat-drawer-chat-list-drop-target={dropUnfiledList}
          aria-label={$t('titlebar.chats')}
          ondragover={dragOverUnfiledList}
          ondragleave={dragLeaveUnfiledList}
          ondrop={dropUnfiledChat}
        >
          {#each flatChats as chat (chat.id)}
            <li>{@render chatRow(chat)}</li>
          {/each}
        </ul>
        {:else}
          {#each chatSections as section (section.id)}
            <section
              class="chat-drawer-group"
              role="group"
              class:chat-drawer-chat-list-drop-target={section.id === 'folder:unfiled' && dropUnfiledList}
              ondragover={(event) => { if (section.id === 'folder:unfiled') dragOverUnfiledList(event); }}
              ondragleave={(event) => { if (section.id === 'folder:unfiled') dragLeaveUnfiledList(event); }}
              ondrop={(event) => { if (section.id === 'folder:unfiled') dropUnfiledChat(event); }}
            >
            <div class="chat-drawer-section-head">
              <button
                class:collapsed={collapsedSections.has(section.id)}
                class="chat-drawer-group-toggle"
                type="button"
                aria-expanded={!collapsedSections.has(section.id)}
                onclick={() => toggleSection(section.id)}
              >
                <span>{section.label}</span>
                <Icon name="chevron" size={SECTION_CHEVRON_SIZE} strokeWidth={SECTION_CHEVRON_STROKE_WIDTH}/>
              </button>
              {#if section.id === 'folder:unfiled'}
                <button
                  type="button"
                  class="chat-drawer-new-folder"
                  aria-label={$t('titlebar.newChat')}
                  data-tooltip-label={$t('titlebar.newChat')}
                  onclick={onNewChat}
                ><Icon name="new-chat" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/></button>
              {/if}
            </div>
            {#if !collapsedSections.has(section.id)}
              {#if section.chats.length}
                <ul
                  aria-label={section.label}
                  ondragover={(event) => { if (section.id === 'folder:unfiled') dragOverUnfiledList(event); }}
                  ondragleave={(event) => { if (section.id === 'folder:unfiled') dragLeaveUnfiledList(event); }}
                  ondrop={(event) => { if (section.id === 'folder:unfiled') dropUnfiledChat(event); }}
                >
                  {#each section.chats as chat (chat.id)}
                    <li>{@render chatRow(chat)}</li>
                  {/each}
                </ul>
              {:else if section.id === 'folder:unfiled' && chatListPreferences.filter !== 'all'}
                <p class="chat-drawer-section-empty">{$t('chats.noFilterMatches')}</p>
              {:else if section.id === 'folder:unfiled' && chats.length === 0}
                <p class="chat-drawer-section-empty">{$t('chats.noChats')}</p>
              {/if}
            {/if}
          </section>
        {:else}
          {#if chatListPreferences.filter !== 'all' && chats.length}
            <p class="chat-drawer-section-empty">{$t('chats.noFilterMatches')}</p>
          {/if}
        {/each}
      {/if}
    </div>
  {/if}
  </div>

  <!-- Connections sits above the account divider. Chat rows fade into it
       the same way a settings rail fades at its scrolled edge. -->
  <button
    type="button"
    class="chat-drawer-connections"
    class:active={connectionsExpanded}
    aria-current={connectionsExpanded ? 'page' : undefined}
    onclick={onOpenConnections}
  >
    <span class="chat-drawer-connections-icon">
      <Icon name="connections" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/>
    </span>
    <span>{$t('workspace.connections')}</span>
  </button>
  <div class="chat-drawer-footer">
    <AccountMenu status={account} {onSignIn} {onSignOut} {onDocumentation}/>
    <div class="chat-drawer-footer-actions">
      <button
        type="button"
        class="chat-drawer-footer-button"
        aria-label={$t('titlebar.devices')}
        data-tooltip-label={$t('titlebar.devices')}
        data-tooltip-align="end"
        aria-haspopup="dialog"
        onclick={(event) => onOpenHost(event.currentTarget)}
      >
        <Icon name="devices" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/>
      </button>
      <button
        type="button"
        class="chat-drawer-footer-button"
        class:active={settingsExpanded}
        aria-label={$t('titlebar.settings')}
        data-tooltip-label={$t('titlebar.settings')}
        data-tooltip-align="end"
        aria-pressed={settingsExpanded}
        onclick={onToggleSettings}
      >
        <Icon name="settings" size={SETTINGS_ICON_SIZE} strokeWidth={SETTINGS_ICON_STROKE_WIDTH}/>
      </button>
    </div>
  </div>

  {#if menu?.kind === 'team'}
    {@const menuMember = bots.find((member) => member.id === menu?.id)}
    {#if menuMember}
      <div bind:this={menuElement} class="polymux-dropdown-menu chat-drawer-row-menu" role="menu" aria-label={`Options for ${menuMember.name}`} style:left={`${menuLeft}px`} style:top={`${menuTop}px`} style:visibility={menuPlaced ? 'visible' : 'hidden'}>
        <button class="polymux-dropdown-item" role="menuitem" onclick={() => { const id = menuMember.id; menu = null; onEditTeam(id); }}><Icon name="edit" size={14}/><span>Edit</span></button>
        <button class="polymux-dropdown-item destructive" role="menuitem" onclick={() => { const id = menuMember.id; menu = null; onDeleteTeam(id); }}><Icon name="trash" size={14}/><span>Delete</span></button>
      </div>
    {/if}
  {:else if menu?.kind === 'team-group'}
    {@const menuGroup = teamGroups.find((group) => group.id === menu?.id)}
    {#if menuGroup}
      <div bind:this={menuElement} class="polymux-dropdown-menu chat-drawer-row-menu" role="menu" aria-label={`Options for ${menuGroup.name}`} style:left={`${menuLeft}px`} style:top={`${menuTop}px`} style:visibility={menuPlaced ? 'visible' : 'hidden'}>
        <button class="polymux-dropdown-item" role="menuitem" onclick={() => { onEditTeamGroup(menuGroup.id); menu = null; }}><Icon name="edit" size={14}/><span>Edit group</span></button>
        <button class="polymux-dropdown-item destructive" role="menuitem" onclick={() => { onDeleteTeamGroup(menuGroup.id); menu = null; }}><Icon name="trash" size={14}/><span>Delete group</span></button>
      </div>
    {/if}
  {:else if menu?.kind === 'chat'}
    {@const menuChat = chats.find((chat) => chat.id === menu?.id)}
    {@const menuFolder = menuChat ? folderForChat(folders, menuChat.id) : undefined}
    {#if menuChat}
      <div bind:this={menuElement} class="polymux-dropdown-menu chat-drawer-row-menu" role="menu" style:left={`${menuLeft}px`} style:top={`${menuTop}px`} style:visibility={menuPlaced ? 'visible' : 'hidden'}>
        <button class="polymux-dropdown-item" role="menuitem" onclick={() => { pins = toggleChatPin(pins, 'chats', menuChat.id); menu = null; }}><Icon name="pin" size={14}/><span>{$t(pins.chats.includes(menuChat.id) ? 'hub.unpinChat' : 'chats.pinChat')}</span></button>
        <button class="polymux-dropdown-item" role="menuitem" onclick={() => startRename(menuChat)}><Icon name="edit" size={14}/><span>{$t('common.rename')}</span></button>
        <button class="polymux-dropdown-item" role="menuitem" onclick={() => { onShare(menuChat.id); menu = null; }}><Icon name="share" size={14}/><span>Share</span></button>
        <button class="polymux-dropdown-item" role="menuitem" onclick={() => duplicateChat(menuChat.id)}><Icon name="copy-plus" size={14}/><span>{$t('drive.duplicate')}</span></button>
        {#if folders.some((folder) => folder.id !== menuFolder?.id)}
          <button bind:this={moveTrigger} class="polymux-dropdown-item" role="menuitem" aria-haspopup="menu" aria-expanded={moveMenuOpen}
            onmouseenter={() => showMoveMenu()} onclick={() => showMoveMenu()} onkeydown={moveKeys}>
            <Icon name="folder-move" size={14}/><span>{$t('chats.moveToFolder')}</span><Icon name="forward" size={12}/>
          </button>
        {/if}
        {#if menuFolder}
          <button class="polymux-dropdown-item" role="menuitem" onclick={() => moveChat(menuChat.id, null)}><Icon name="folder-move" size={14}/><span>{$t('chats.removeFromFolder')}</span></button>
        {/if}
        <button class="polymux-dropdown-item" role="menuitem" onclick={() => archiveChat(menuChat.id)}><Icon name="archive" size={14}/><span>{$t('chats.archive')}</span></button>
      </div>
      {#if moveMenuOpen}
        <div bind:this={moveMenu} use:scrollFade class="polymux-dropdown-menu chat-drawer-row-menu chat-drawer-move-menu" role="menu" tabindex="-1" aria-label={$t('chats.moveToFolder')}
          style:left={`${moveLeft}px`} style:top={`${moveTop}px`} style:visibility={movePlaced ? 'visible' : 'hidden'} onkeydown={moveKeys}>
          {#each folders.filter(folder => folder.id !== menuFolder?.id) as folder (folder.id)}
            <button class="polymux-dropdown-item" role="menuitem" onclick={() => moveChat(menuChat.id, folder.id)}><Icon name="folder" size={14}/><span>{folder.name}</span></button>
          {/each}
        </div>
      {/if}
    {/if}
  {:else if menu?.kind === 'folder'}
    {@const menuFolder = folders.find((folder) => folder.id === menu?.id)}
    {#if menuFolder}
      <div bind:this={menuElement} class="polymux-dropdown-menu chat-drawer-row-menu" role="menu" style:left={`${menuLeft}px`} style:top={`${menuTop}px`} style:visibility={menuPlaced ? 'visible' : 'hidden'}>
        <button class="polymux-dropdown-item" role="menuitem" onclick={() => { pins = toggleChatPin(pins, 'folders', menuFolder.id); menu = null; }}><Icon name="pin" size={14}/><span>{$t(pins.folders.includes(menuFolder.id) ? 'hub.unpinChat' : 'chats.pinFolder')}</span></button>
        <button class="polymux-dropdown-item" role="menuitem" onclick={() => startRenameFolder(menuFolder)}><Icon name="edit" size={14}/><span>{$t('common.rename')}</span></button>
        <button class="polymux-dropdown-item destructive" role="menuitem" onclick={() => removeFolder(menuFolder.id)}><Icon name="trash" size={14}/><span>{$t('chats.deleteFolder')}</span></button>
      </div>
    {/if}
  {/if}
  {#if duplicateError}<p class="chat-drawer-empty" role="alert">{duplicateError}</p>{/if}
</aside>

{#snippet chatRow(chat: ChatEntry, nested = false)}
  {#if editingId === chat.id}
    <div class:nested class="chat-drawer-edit">
      <input
        bind:this={renameInput}
        bind:value={draft}
        aria-label={$t('chats.rename', {title: chat.title})}
        onkeydown={(event) => renameKeydown(event, chat.id)}
        onblur={() => saveRename(chat.id)}
      />
    </div>
  {:else}
    <div
      class:active={chat.id === activeId}
      class:menu-open={menu?.kind === 'chat' && menu.id === chat.id}
      class:running={chat.running}
      class:nested
      class="chat-drawer-row"
      draggable={true}
       ondragstart={(event) => {
         draggedChatId = chat.id;
         menu = null;
         dropFolderId = null;
         dropUnfiledList = false;
         dropPinnedList = false;
         event.dataTransfer!.effectAllowed = 'move';
         event.dataTransfer!.setData('text/plain', chat.id);
       }}
       ondragend={() => {draggedChatId = null; dropFolderId = null; dropUnfiledList = false; dropPinnedList = false;}}
       role="group"
       oncontextmenu={(event) => openContextMenu(event, 'chat', chat.id)}
      >
      <button
        class="chat-drawer-open-chat"
        type="button"
        aria-label={$t('chats.openChat', {title: chat.title})}
        aria-current={chat.id === activeId ? 'page' : undefined}
        onclick={() => onOpen(chat.id)}
        ondblclick={(event) => { event.preventDefault(); event.stopPropagation(); void startRename(chat); }}
      ><span>{chat.title}</span></button>
      <span class="chat-drawer-row-action">
        {#if chat.running}<i class="chat-drawer-running-ring" role="status" aria-label={$t('chats.running', {title: chat.title})}></i>{/if}
        <button
          class="chat-drawer-more"
          type="button"
          aria-label={$t('chats.moreActions', {title: chat.title})}
          data-tooltip="none"
          aria-haspopup="menu"
          aria-expanded={menu?.kind === 'chat' && menu.id === chat.id}
          onclick={(event) => toggleMenu(event, 'chat', chat.id)}
        ><Icon name="ellipsis" size={16}/></button>
      </span>
    </div>
  {/if}
{/snippet}

{#snippet folderRow(view: {folder: ChatFolder; chats: ChatEntry[]})}
            <li class="chat-drawer-folder">
              {#if editingFolderId === view.folder.id}
                <div class="chat-drawer-folder-edit">
                  <Icon name="folder" size={17}/>
                  <input
                    bind:this={folderInput}
                    bind:value={folderDraft}
                    aria-label={$t('chats.renameFolder', {name: view.folder.name})}
                    onkeydown={(event) => folderKeydown(event, view.folder.id)}
                    onblur={() => saveFolderRename(view.folder.id)}
                  />
                </div>
              {:else}
                <div
                  class:menu-open={menu?.kind === 'folder' && menu.id === view.folder.id}
                  class="chat-drawer-folder-row"
                  class:drop-target={dropFolderId === view.folder.id}
                  ondragover={(event) => dragOverFolder(event, view.folder.id)}
                  ondragleave={() => dragLeaveFolder(view.folder.id)}
                  ondrop={(event) => dropChat(event, view.folder.id)}
                  role="group"
                  oncontextmenu={(event) => openContextMenu(event, 'folder', view.folder.id)}
                >
                  <button
                    class="chat-drawer-folder-toggle"
                    type="button"
                    aria-expanded={!view.folder.collapsed}
                    aria-label={$t(view.folder.collapsed ? 'chats.expandFolder' : 'chats.collapseFolder', {name: view.folder.name})}
                    onclick={() => folders = toggleChatFolder(folders, view.folder.id)}
                    ondblclick={(event) => { event.preventDefault(); event.stopPropagation(); void startRenameFolder(view.folder); }}
                  >
                    <Icon name={view.folder.collapsed ? 'folder' : 'folder-open'} size={17}/>
                    <span>{view.folder.name}</span>
                  </button>
                  <button
                    class="chat-drawer-new-folder chat-drawer-folder-new-chat"
                    type="button"
                    aria-label={`${$t('titlebar.newChat')}: ${view.folder.name}`}
                    data-tooltip-label={$t('titlebar.newChat')}
                    onclick={() => { menu = null; onNewFolderChat(view.folder.id); }}
                  ><Icon name="new-chat" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/></button>
                  <button
                    class="chat-drawer-more"
                    type="button"
                    aria-label={$t('chats.folderActions', {name: view.folder.name})}
                    data-tooltip="none"
                    aria-haspopup="menu"
                    aria-expanded={menu?.kind === 'folder' && menu.id === view.folder.id}
                    onclick={(event) => toggleMenu(event, 'folder', view.folder.id)}
                  ><Icon name="ellipsis" size={16}/></button>
                </div>
              {/if}
              {#if !view.folder.collapsed && view.chats.length}
                <ul
                  class="chat-drawer-folder-chats"
                  aria-label={view.folder.name}
                  ondragover={(event) => dragOverFolder(event, view.folder.id)}
                  ondragleave={(event) => dragLeaveFolder(event, view.folder.id)}
                  ondrop={(event) => dropChat(event, view.folder.id)}
                >
                  {#each view.chats as chat (chat.id)}
                    <li>{@render chatRow(chat, true)}</li>
                  {/each}
                </ul>
              {/if}
            </li>
{/snippet}
