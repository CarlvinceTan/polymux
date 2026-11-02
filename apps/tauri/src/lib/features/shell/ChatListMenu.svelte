<script lang="ts">
  import {tick} from 'svelte';
  import {t, type MessageKey} from '../../../i18n';
  import Icon from '../../shared/components/Icon.svelte';
  import {scrollFade} from '../../shared/scrollFade';
  import {MAIN_UI_ICON_SIZE, MAIN_UI_ICON_STROKE_WIDTH} from '../../shared/layout/iconSizing';
  import {MENU_EDGE_MARGIN, clampToMenuEdge} from '../../shared/layout/menuPlacement';
  import {
    defaultChatListPreferences,
    type ChatListFilter,
    type ChatListGroupBy,
    type ChatListPreferences,
    type ChatListSortBy,
    type ChatListSortOrder,
  } from './chatListPreferences';

  export let preferences: ChatListPreferences;
  export let onChange: (preferences: ChatListPreferences) => void = () => {};

  const groupOptions: Array<{value: ChatListGroupBy; label: MessageKey}> = [
    {value: 'date', label: 'chats.groupDate'},
    {value: 'folder', label: 'chats.groupFolder'},
    {value: 'state', label: 'chats.groupState'},
    {value: 'none', label: 'chats.groupNone'},
  ];
  const sortOptions: Array<{value: ChatListSortBy; label: MessageKey}> = [
    {value: 'activity', label: 'chats.sortActivity'},
    {value: 'created', label: 'chats.sortCreated'},
    {value: 'name', label: 'chats.sortName'},
  ];
  const orderOptions: Array<{value: ChatListSortOrder; label: MessageKey}> = [
    {value: 'descending', label: 'chats.orderDescending'},
    {value: 'ascending', label: 'chats.orderAscending'},
  ];
  const filterOptions: Array<{value: ChatListFilter; label: MessageKey}> = [
    {value: 'all', label: 'chats.filterAll'},
    {value: 'running', label: 'chats.filterRunning'},
    {value: 'idle', label: 'chats.filterIdle'},
    {value: 'foldered', label: 'chats.filterFoldered'},
    {value: 'unfiled', label: 'chats.filterUnfiled'},
  ];
  type MenuSection = keyof ChatListPreferences;
  const sectionLabels: Record<MenuSection, MessageKey> = {
    groupBy: 'chats.groupBy',
    sortBy: 'chats.sortBy',
    order: 'chats.order',
    filter: 'chats.filter',
  };

  let open = false;
  let wrap: HTMLDivElement;
  let trigger: HTMLButtonElement;
  let list: HTMLDivElement;
  let submenu: HTMLDivElement | undefined;
  let submenuAnchor: HTMLButtonElement | undefined;
  let activeSection: MenuSection | null = null;
  let menuLeft = 0;
  let menuTop = 0;
  let menuPlaced = false;
  let submenuLeft = 0;
  let submenuTop = 0;
  let submenuPlaced = false;
  let submenuOpensLeft = false;
  const EDGE_MARGIN = MENU_EDGE_MARGIN;
  const MENU_GAP = 5;

  $: active = preferences.groupBy !== defaultChatListPreferences.groupBy
    || preferences.sortBy !== defaultChatListPreferences.sortBy
    || preferences.order !== defaultChatListPreferences.order
    || preferences.filter !== defaultChatListPreferences.filter;
  $: activeSectionLabel = activeSection ? $t(sectionLabels[activeSection]) : '';

  async function toggle(): Promise<void> {
    open = !open;
    activeSection = null;
    menuPlaced = false;
    if (open) {
      await tick();
      await placeMenu();
    }
  }

  async function placeMenu(): Promise<void> {
    if (!open || !trigger || !wrap || !list) return;
    const anchor = trigger.getBoundingClientRect();
    const wrapRect = wrap.getBoundingClientRect();
    const below = window.innerHeight - anchor.bottom - MENU_GAP - EDGE_MARGIN;
    const above = anchor.top - MENU_GAP - EDGE_MARGIN;
    const openBelow = list.scrollHeight <= below || below >= above;
    list.style.maxHeight = `${Math.min(200, Math.max(0, openBelow ? below : above))}px`;
    const {width, height} = list.getBoundingClientRect();
    const idealLeft = anchor.left + anchor.width / 2 - width / 2;
    const left = clampToMenuEdge(idealLeft, width, window.innerWidth);
    menuLeft = left - wrapRect.left;
    menuTop = (openBelow ? anchor.bottom + MENU_GAP : anchor.top - MENU_GAP - height) - wrapRect.top;
    menuPlaced = true;
    await tick();
    if (activeSection) placeSubmenu();
  }

  function observeAnchor(node: HTMLElement) {
    const observer = new ResizeObserver(() => {if (open) void placeMenu();});
    observer.observe(node.closest('.chat-drawer') ?? node);
    return {destroy: () => observer.disconnect()};
  }

  async function showSection(event: Event, section: MenuSection): Promise<void> {
    submenuAnchor = event.currentTarget as HTMLButtonElement;
    if (activeSection === section && submenuPlaced) return;
    activeSection = section;
    submenuPlaced = false;
    await tick();
    placeSubmenu();
  }

  function placeSubmenu(): void {
    if (!open || !activeSection || !wrap || !list || !submenu || !submenuAnchor) return;
    const wrapRect = wrap.getBoundingClientRect();
    const mainRect = list.getBoundingClientRect();
    const anchorRect = submenuAnchor.getBoundingClientRect();
    const rightSpace = window.innerWidth - mainRect.right - MENU_GAP - EDGE_MARGIN;
    const leftSpace = mainRect.left - MENU_GAP - EDGE_MARGIN;
    submenu.style.maxWidth = '';
    const wanted = submenu.getBoundingClientRect().width;
    submenuOpensLeft = wanted > rightSpace && leftSpace > rightSpace;
    // On a narrow window, trim the label column rather than covering the
    // parent menu. The radio's full accessible name remains available.
    submenu.style.maxWidth = `${Math.max(0, submenuOpensLeft ? leftSpace : rightSpace)}px`;
    const {width, height} = submenu.getBoundingClientRect();
    const viewportLeft = submenuOpensLeft ? mainRect.left - width - MENU_GAP : mainRect.right + MENU_GAP;
    const viewportTop = clampToMenuEdge(anchorRect.top - 4, height, window.innerHeight);
    submenuLeft = viewportLeft - wrapRect.left;
    submenuTop = viewportTop - wrapRect.top;
    submenuPlaced = true;
  }

  function choose<K extends keyof ChatListPreferences>(key: K, value: ChatListPreferences[K]): void {
    onChange({...preferences, [key]: value});
    void tick().then(placeMenu);
  }

  function dismiss(event: MouseEvent): void {
    if (!open || wrap?.contains(event.target as Node)) return;
    open = false;
    activeSection = null;
  }

  function keydown(event: KeyboardEvent): void {
    if (!open) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      open = false;
      activeSection = null;
      trigger?.focus({preventScroll: true});
      return;
    }
    const target = event.target as HTMLElement;
    const panel = target.closest('[role="menu"]');
    if (target !== trigger && panel !== list && panel !== submenu) return;
    const buttons = [...(panel ?? list).querySelectorAll<HTMLButtonElement>('button')];
    const current = buttons.indexOf(target as HTMLButtonElement);
    let next: HTMLButtonElement | undefined;
    if (event.key === 'ArrowDown') next = buttons[(current + 1) % buttons.length];
    else if (event.key === 'ArrowUp') next = current < 0 ? buttons.at(-1) : buttons[(current - 1 + buttons.length) % buttons.length];
    else if (event.key === 'Home') next = buttons[0];
    else if (event.key === 'End') next = buttons.at(-1);
    else if (event.key === 'ArrowRight' && panel === list) next = submenu?.querySelector('button') ?? undefined;
    else if (event.key === 'ArrowLeft' && panel === submenu) {
      submenuAnchor?.focus({preventScroll: true});
      activeSection = null;
      event.preventDefault();
      return;
    }
    if (next) {
      event.preventDefault();
      event.stopPropagation();
      next.focus({preventScroll: true});
      next.scrollIntoView({block: 'nearest'});
    }
  }
</script>

<svelte:window onclick={dismiss} onkeydown={keydown} onresize={() => open && void placeMenu()}/>

<div bind:this={wrap} use:observeAnchor class="chat-list-options">
  <button
    bind:this={trigger}
    type="button"
    class:active
    class="chat-drawer-heading-action chat-drawer-new-folder"
    aria-label={$t('chats.arrange')}
    data-tooltip-label={$t('chats.arrange')}
    aria-haspopup="menu"
    aria-expanded={open}
    onclick={toggle}
  ><Icon name="filter" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/></button>

  {#if open}
    <div
      bind:this={list}
      use:scrollFade
      class:placed={menuPlaced}
      class:left-opening={submenuOpensLeft}
      class="polymux-dropdown-menu chat-list-options-menu"
      style:left={`${menuLeft}px`}
      style:top={`${menuTop}px`}
      role="menu"
      aria-label={$t('chats.listOptions')}
      onscroll={placeSubmenu}
    >
      {#each Object.entries(sectionLabels) as [section, label] (section)}
        <button
          type="button"
          class:submenu-open={activeSection === section}
          class="polymux-dropdown-item chat-list-options-parent"
          role="menuitem"
          aria-haspopup="menu"
          aria-expanded={activeSection === section}
          onmouseenter={(event) => showSection(event, section as MenuSection)}
          onfocus={(event) => showSection(event, section as MenuSection)}
          onclick={(event) => showSection(event, section as MenuSection)}
        >
          <span>{$t(label)}</span>
          <span class="chat-list-options-chevron"><Icon name="chevron" size={13}/></span>
        </button>
      {/each}
    </div>

    {#if activeSection}
      <div
        bind:this={submenu}
        use:scrollFade={activeSection}
        class:placed={submenuPlaced}
        class="polymux-dropdown-menu chat-list-options-submenu"
        style:left={`${submenuLeft}px`}
        style:top={`${submenuTop}px`}
        role="menu"
        aria-label={activeSectionLabel}
      >
        {#if activeSection === 'groupBy'}
        {#each groupOptions as option (option.value)}
          <button type="button" class="polymux-dropdown-item" role="menuitemradio" aria-checked={preferences.groupBy === option.value} onclick={() => choose('groupBy', option.value)}>
            <span>{$t(option.label)}</span><span class="chat-list-options-check">{#if preferences.groupBy === option.value}<Icon name="check" size={13}/>{/if}</span>
          </button>
        {/each}
        {:else if activeSection === 'sortBy'}
        {#each sortOptions as option (option.value)}
          <button type="button" class="polymux-dropdown-item" role="menuitemradio" aria-checked={preferences.sortBy === option.value} onclick={() => choose('sortBy', option.value)}>
            <span>{$t(option.label)}</span><span class="chat-list-options-check">{#if preferences.sortBy === option.value}<Icon name="check" size={13}/>{/if}</span>
          </button>
        {/each}
        {:else if activeSection === 'order'}
        {#each orderOptions as option (option.value)}
          <button type="button" class="polymux-dropdown-item" role="menuitemradio" aria-checked={preferences.order === option.value} onclick={() => choose('order', option.value)}>
            <span>{$t(option.label)}</span><span class="chat-list-options-check">{#if preferences.order === option.value}<Icon name="check" size={13}/>{/if}</span>
          </button>
        {/each}
        {:else}
        {#each filterOptions as option (option.value)}
          <button type="button" class="polymux-dropdown-item" role="menuitemradio" aria-checked={preferences.filter === option.value} onclick={() => choose('filter', option.value)}>
            <span>{$t(option.label)}</span><span class="chat-list-options-check">{#if preferences.filter === option.value}<Icon name="check" size={13}/>{/if}</span>
          </button>
        {/each}
        {/if}
      </div>
    {/if}
  {/if}
</div>

<style>
  /* Keep the header's 24px footprint independent of either floating panel. */
  .chat-list-options { position: relative; z-index: 91; display: flex; flex: none; align-items: center; gap: 4px; }
  .chat-list-options-menu, .chat-list-options-submenu {
    position: absolute; z-index: 1; max-height: min(200px, calc(100vh - 16px));
    overflow-y: auto; overscroll-behavior: contain; scrollbar-width: none;
  }
  .chat-list-options-menu::-webkit-scrollbar, .chat-list-options-submenu::-webkit-scrollbar { display: none; }
  .chat-list-options-submenu { z-index: 2; }
  /* An unscrolled panel keeps its shell shadow; fades belong only to overflow. */
  .chat-list-options-menu:global(.at-top.at-bottom), .chat-list-options-submenu:global(.at-top.at-bottom) { mask-image: none; -webkit-mask-image: none; }
  .chat-list-options-menu:not(.placed), .chat-list-options-submenu:not(.placed) { visibility: hidden; }
  .polymux-dropdown-item > span:first-child { flex: 1; }
  .chat-list-options-chevron, .chat-list-options-check { display: grid; place-items: center; flex: none; width: 13px; height: 13px; }
  .chat-list-options-chevron { color: var(--neutral-500); transform: rotate(-90deg); }
  .left-opening .chat-list-options-chevron { transform: rotate(90deg); }
  .chat-list-options-parent.submenu-open { color: var(--neutral-950); background: var(--neutral-50); }
  .chat-drawer-heading-action.active { color: var(--neutral-950); }
</style>
