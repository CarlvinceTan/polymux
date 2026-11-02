<script module lang="ts">
  /** `icon` is optional per option; a list that sets it on none looks exactly
   * as it did before. The glyph name is reached for inline because the
   * instance script below imports `Icon` as a value under the same name. */
  export type MenuOption = {
    value: string;
    label: string;
    /** Keeps unavailable choices visible while preventing selection. */
    disabled?: boolean;
    icon?: import('svelte').ComponentProps<import('./Icon.svelte').default>['name'];
    /**
     * A storage provider's own mark instead of a glyph, so two Google accounts
     * in one list are recognisable as Google before their addresses are read.
     * Takes precedence over `icon` where both are set.
     */
    provider?: import('@polymux/protocol').DriveProviderId;
  };

  /** Escape closes the last menu that opened, so a submenu leaves its parent. */
  type StackEntry = {close: () => void};
  const openStack: StackEntry[] = [];
</script>

<script lang="ts">
  import {tick, type ComponentProps, type Snippet} from 'svelte';
  import Icon from './Icon.svelte';
  import DriveProviderLogo from './DriveProviderLogo.svelte';
  import {MENU_EDGE_MARGIN, clampToMenuEdge, fitInsetMenu} from '../layout/menuPlacement';
  import {scrollFade} from '../scrollFade';

  type IconName = ComponentProps<typeof Icon>['name'];
  type Match = string | HTMLElement | null | undefined;

  /**
   * A choice, in the app's own dropdown rather than the platform's.
   *
   * A native `<select>` renders with the operating system's own chevron and
   * inset, which sits tight against the edge and matches nothing else here.
   * This is the same `polymux-dropdown-menu` shell every other menu uses.
   *
   * Pass `children` (and optionally `trigger`) for an action menu: the list is
   * still that shared shell — border, radius — rather than a second overlay.
   */
  let {
    options = [],
    value = $bindable(''),
    label,
    onChange = () => {},
    wide = false,
    icon = null,
    trailingIcon = null,
    plain = false,
    floating = false,
    keepOpenOnChange = false,
    values = null,
    onTriggerClick = null,
    onToggle = () => {},
    summary = '',
    placement = 'below',
    width = 'auto',
    inset = false,
    open = $bindable(false),
    match = null,
    insetParent = null,
    anchor = null,
    submenuAnchorRow = 0,
    group = '',
    listClass = '',
    opensLeft = $bindable(false),
    onClose = () => {},
    trigger,
    children,
  }: {
    options?: MenuOption[];
    value?: string;
    label: string;
    onChange?: (value: string) => void;
    wide?: boolean;
    icon?: IconName | null;
    trailingIcon?: IconName | null;
    plain?: boolean;
    floating?: boolean;
    keepOpenOnChange?: boolean;
    values?: string[] | null;
    onTriggerClick?: (() => void) | null;
    onToggle?: (value: string) => void;
    summary?: string;
    placement?: 'below' | 'above' | 'end';
    width?: 'auto' | 'full';
    inset?: boolean;
    open?: boolean;
    match?: Match;
    insetParent?: Match;
    anchor?: HTMLElement | null;
    submenuAnchorRow?: number;
    group?: string;
    listClass?: string;
    opensLeft?: boolean;
    onClose?: () => void;
    trigger?: Snippet<[{open: boolean; toggle: () => void}]>;
    children?: Snippet;
  } = $props();

  const isAction = $derived(Boolean(children));
  const portalled = $derived(isAction || floating);
  const multiple = $derived(values !== null);

  let wrap = $state<HTMLElement | undefined>();
  let triggerEl = $state<HTMLButtonElement | undefined>();
  let list = $state<HTMLElement | undefined>();
  /** The height the list is held to so it scrolls instead of running off. */
  let listMaxHeight = $state<number | null>(null);
  let listMinWidth = $state<number | null>(null);
  let listWidth = $state<number | null>(null);
  let listLeft = $state(0);
  let listTop = $state(0);
  let listPlaced = $state(false);
  let stackEntry: StackEntry | null = null;

  /** How close the list may come to the window — room for the shell border. */
  const EDGE_MARGIN = MENU_EDGE_MARGIN;
  /** Air between a select and the control it belongs to. */
  const TRIGGER_GAP = 5;
  /** Old-web action menus sit 4px off their parent; submenus sit 8px off. */
  const ACTION_GAP = 4;
  const SUBMENU_GAP = 8;

  function resolveEl(ref: Match, from: HTMLElement | null | undefined): HTMLElement | null {
    if (!ref) return null;
    if (typeof ref !== 'string') return ref;
    return from?.closest(ref) ?? null;
  }

  function closeMenu(): void {
    if (!open) return;
    open = false;
    onClose();
  }

  function fadeWhenAction(node: HTMLElement, enabled: boolean) {
    if (!enabled) return {};
    return scrollFade(node);
  }

  /** A floating or action list lives at body level so no scrolling panel can clip it. */
  function listLifecycle(node: HTMLElement) {
    if (portalled) document.body.appendChild(node);
    stackEntry = {close: closeMenu};
    openStack.push(stackEntry);
    listMaxHeight = null;
    listMinWidth = floating && !isAction && triggerEl ? Math.ceil(triggerEl.getBoundingClientRect().width) : null;
    listWidth = null;
    listPlaced = !portalled;
    void tick().then(async () => {
      await place();
      if (!isAction) {
        (node.querySelector<HTMLButtonElement>('[aria-checked="true"]:not(:disabled)')
          ?? node.querySelector<HTMLButtonElement>('button:not(:disabled)'))?.focus();
      }
    });
    return {
      destroy() {
        const index = openStack.indexOf(stackEntry!);
        if (index >= 0) openStack.splice(index, 1);
        stackEntry = null;
        if (portalled) node.remove();
      },
    };
  }

  const current = $derived(multiple ? summary : options.find((option) => option.value === value)?.label ?? '');
  /** The trigger wears the chosen option's own glyph where the options carry
   * one, so it names what is selected rather than the menu it came from. */
  const triggerIcon = $derived(options.find((option) => option.value === value)?.icon ?? icon);
  const triggerProvider = $derived(options.find((option) => option.value === value)?.provider);

  async function toggle(): Promise<void> {
    if (onTriggerClick) {
      closeMenu();
      onTriggerClick();
      return;
    }
    if (open) closeMenu();
    else open = true;
  }

  /**
   * Keeps the list on screen. An inline list retains its established downward
   * placement. A floating list prefers below, flips above when that side has
   * more room, and is centred on the trigger before being clamped to the
   * viewport. Action menus honour `placement` (`above` / `end`) and `width`.
   * Both variants cap long lists so their rows scroll internally.
   */
  async function place(): Promise<void> {
    if (!open || !list) return;
    if (isAction) {
      await placeAction();
      return;
    }
    if (!triggerEl) return;
    const rect = triggerEl.getBoundingClientRect();
    // The rendered height, not the content height: a long list is already
    // capped to a few rows by the stylesheet and scrolls within that, so what
    // has to fit is the box, not everything inside it.
    const wanted = list.getBoundingClientRect().height;
    const below = window.innerHeight - rect.bottom - EDGE_MARGIN - TRIGGER_GAP;
    if (!floating) {
      if (wanted <= below) return;
      // Only ever shrinks: an inline max-height outranks the stylesheet's row
      // cap, so handing it a large number would make the list taller than it is
      // ever meant to be rather than shorter.
      listMaxHeight = Math.min(Math.round(wanted), Math.max(120, Math.floor(below)));
      return;
    }

    const above = rect.top - EDGE_MARGIN - TRIGGER_GAP;
    const openBelow = wanted <= below || below >= above;
    const available = Math.max(0, openBelow ? below : above);
    listMaxHeight = Math.min(Math.round(wanted), Math.floor(available));
    await tick();
    if (!open || !triggerEl || !list) return;

    const next = triggerEl.getBoundingClientRect();
    const menu = list.getBoundingClientRect();
    const idealLeft = next.left + next.width / 2 - menu.width / 2;
    listLeft = Math.round(clampToMenuEdge(idealLeft, menu.width, window.innerWidth));
    listTop = Math.round(openBelow
      ? next.bottom + TRIGGER_GAP
      : next.top - TRIGGER_GAP - menu.height);
    listPlaced = true;
  }

  async function placeAction(): Promise<void> {
    if (!open || !list) return;
    if (placement === 'end') {
      await placeSubmenu();
      return;
    }
    const from = wrap ?? triggerEl ?? undefined;
    const matchEl = resolveEl(match, from) ?? triggerEl ?? wrap;
    const insetEl = resolveEl(insetParent, from);
    if (!matchEl) return;
    const matchRect = matchEl.getBoundingClientRect();
    const gap = ACTION_GAP;
    if (width === 'full') {
      if (inset && insetEl) {
        const fitted = fitInsetMenu(matchRect.width, insetEl.getBoundingClientRect());
        listWidth = fitted.width;
        listMinWidth = fitted.width;
        listLeft = fitted.left;
      } else {
        listWidth = matchRect.width;
        listMinWidth = matchRect.width;
        listLeft = matchRect.left;
      }
    } else {
      listWidth = null;
      listMinWidth = null;
      listLeft = matchRect.left;
    }
    await tick();
    if (!open || !list) return;
    const box = list.getBoundingClientRect();
    const maxWidth = Math.max(0, window.innerWidth - 2 * EDGE_MARGIN);
    if (listWidth !== null && listWidth > maxWidth) {
      listWidth = maxWidth;
      listMinWidth = maxWidth;
    }
    listLeft = Math.round(clampToMenuEdge(listLeft, listWidth ?? box.width, window.innerWidth));
    const wanted = box.height;
    const available = placement === 'above'
      ? matchRect.top - gap - EDGE_MARGIN
      : window.innerHeight - matchRect.bottom - gap - EDGE_MARGIN;
    listMaxHeight = Math.min(Math.round(wanted), Math.max(0, Math.floor(available)));
    const height = Math.min(wanted, listMaxHeight ?? wanted);
    listTop = placement === 'above'
      ? Math.max(EDGE_MARGIN, matchRect.top - gap - height)
      : matchRect.bottom + gap;
    listPlaced = true;
  }

  async function placeSubmenu(): Promise<void> {
    if (!open || !list) return;
    const row = anchor ?? triggerEl;
    if (!row) return;
    const main = row.closest('[role="menu"]');
    const from = wrap ?? row;
    const matchEl = resolveEl(match, from);
    const insetEl = resolveEl(insetParent, from) ?? row.closest('.chat-drawer');
    const mainRect = (main ?? row).getBoundingClientRect();
    const anchorRect = row.getBoundingClientRect();
    let panelWidth = width === 'full'
      ? (matchEl?.getBoundingClientRect().width ?? mainRect.width)
      : list.getBoundingClientRect().width;
    if (width === 'full' && insetEl) {
      panelWidth = fitInsetMenu(panelWidth, insetEl.getBoundingClientRect()).width;
    }
    listWidth = width === 'full' ? panelWidth : null;
    const right = mainRect.right + SUBMENU_GAP;
    const left = mainRect.left - SUBMENU_GAP - panelWidth;
    const fitsRight = right + panelWidth <= window.innerWidth - EDGE_MARGIN;
    const fitsLeft = left >= EDGE_MARGIN;
    if (fitsRight) {
      listLeft = right;
      opensLeft = false;
    } else if (fitsLeft) {
      listLeft = left;
      opensLeft = true;
    } else if (insetEl) {
      listLeft = fitInsetMenu(panelWidth, insetEl.getBoundingClientRect()).left;
      opensLeft = false;
    } else {
      listLeft = clampToMenuEdge(right, panelWidth, window.innerWidth);
      opensLeft = false;
    }
    await tick();
    if (!open || !list) return;
    const wanted = list.getBoundingClientRect().height;
    const rowEl = list.querySelector('.polymux-dropdown-item');
    const rowHeight = rowEl?.getBoundingClientRect().height || 28;
    const pad = 4;
    const midY = anchorRect.top + anchorRect.height / 2;
    const anchorOffset = pad + submenuAnchorRow * rowHeight + rowHeight / 2;
    const available = window.innerHeight - 2 * EDGE_MARGIN;
    listMaxHeight = Math.min(Math.round(wanted), Math.max(0, Math.floor(available)));
    const height = Math.min(wanted, listMaxHeight ?? wanted);
    listTop = Math.max(EDGE_MARGIN, Math.min(
      midY - anchorOffset,
      window.innerHeight - height - EDGE_MARGIN,
    ));
    listPlaced = true;
  }

  function observeHost(node: HTMLElement) {
    const host = resolveEl(insetParent, node) ?? resolveEl(match, node) ?? node.closest('.chat-drawer') ?? node;
    const observer = new ResizeObserver(() => {if (open) void place();});
    observer.observe(host);
    return {destroy: () => observer.disconnect()};
  }

  function choose(next: string): void {
    value = next;
    onChange(next);
    if (keepOpenOnChange) {
      void tick().then(place);
      return;
    }
    closeMenu();
    triggerEl?.focus();
  }

  function grouped(target: EventTarget | null): boolean {
    if (!group || !(target instanceof Element)) return false;
    return Boolean(target.closest(`[data-menu-group="${group}"]`));
  }

  /** Padding, the email heading, and section rules — not a choice. */
  function inertChrome(target: EventTarget | null): boolean {
    const el = target instanceof Text ? target.parentElement : target;
    if (!(el instanceof Element)) return false;
    if (el === list || el.getAttribute('role') === 'menu') return true;
    return Boolean(el.closest('[role="separator"], .menu-heading'));
  }

  function keydown(event: KeyboardEvent): void {
    // The menu is the innermost thing open, so it takes the Escape rather than
    // letting the surface underneath close.
    if (open && event.key === 'Escape' && openStack.at(-1) === stackEntry) {
      event.preventDefault();
      event.stopImmediatePropagation();
      closeMenu();
      triggerEl?.focus({preventScroll: true});
      return;
    }
    if (!open || !list || !isAction) return;
    const target = event.target as HTMLElement;
    if (target !== triggerEl && !list.contains(target)) return;
    const buttons = [...list.querySelectorAll<HTMLButtonElement>(
      '[role="menuitem"], [role="menuitemradio"], [role="menuitemcheckbox"]',
    )];
    const currentIndex = buttons.indexOf(target as HTMLButtonElement);
    let next: HTMLButtonElement | undefined;
    if (event.key === 'ArrowDown') next = buttons[(currentIndex + 1) % buttons.length];
    else if (event.key === 'ArrowUp') next = currentIndex < 0 ? buttons.at(-1) : buttons[(currentIndex - 1 + buttons.length) % buttons.length];
    else if (event.key === 'Home') next = buttons[0];
    else if (event.key === 'End') next = buttons.at(-1);
    if (next) {
      event.preventDefault();
      event.stopPropagation();
      next.focus({preventScroll: true});
      next.scrollIntoView({block: 'nearest'});
    }
  }

  function dismiss(event: PointerEvent): void {
    if (!open || event.button !== 0) return;
    const target = event.target;
    if (!(target instanceof Node) || !target.isConnected) return;
    // Press, not click: a resize handle calls preventDefault on pointerdown,
    // which swallows the click that would otherwise dismiss. Capture so the
    // menu hides before that drag starts. Padding, headings, and separators
    // are not choices, so they close the same way as a press outside.
    const inside = Boolean(wrap?.contains(target) || list?.contains(target) || grouped(target));
    if (inside && !inertChrome(target)) return;
    closeMenu();
  }

  function wheel(event: WheelEvent): void {
    if (!open || !portalled) return;
    if (list?.contains(event.target as Node) || grouped(event.target)) return;
    closeMenu();
  }
</script>

<svelte:window
  onkeydown={keydown}
  onpointerdowncapture={dismiss}
  onresize={() => open && void place()}
  onscroll={() => open && void place()}
  onwheel={wheel}
/>

<div
  bind:this={wrap}
  use:observeHost
  class={[isAction ? 'menu-host' : 'select-menu', {wide, plain}]}
>
  {#if trigger}
    {@render trigger({open, toggle})}
  {:else if !isAction}
    <button
      bind:this={triggerEl}
      type="button"
      class={['select-menu-trigger', {plain}]}
      aria-haspopup={onTriggerClick ? undefined : 'menu'}
      aria-expanded={onTriggerClick ? undefined : open}
      aria-label={label}
      onclick={toggle}
    >
      {#if triggerProvider}
        <span class="select-menu-icon"><DriveProviderLogo provider={triggerProvider} size={16}/></span>
      {:else if triggerIcon}
        <span class="select-menu-icon"><Icon name={triggerIcon} size={16} strokeWidth={1.5}/></span>
      {/if}
      <span>{current}</span>
      {#if trailingIcon}
        <Icon name={trailingIcon} size={13} strokeWidth={1.5}/>
      {:else if !plain}
        <Icon name="chevron" size={11}/>
      {/if}
    </button>
  {/if}
  {#if open}
    <div
      bind:this={list}
      use:listLifecycle
      use:fadeWhenAction={isAction}
      class={[
        'polymux-dropdown-menu',
        isAction ? 'menu-list' : 'select-menu-list',
        listClass,
        {
          floating: portalled,
          'menu-full': isAction && width === 'full',
          inset,
        },
      ]}
      data-menu-group={group || undefined}
      style:left={portalled ? `${listLeft}px` : null}
      style:top={portalled ? `${listTop}px` : null}
      style:right={portalled ? 'auto' : null}
      style:width={listWidth === null ? null : `${listWidth}px`}
      style:min-width={listMinWidth === null ? null : `${listMinWidth}px`}
      style:max-height={listMaxHeight === null ? null : `${listMaxHeight}px`}
      style:visibility={portalled && !listPlaced ? 'hidden' : null}
      role="menu"
      aria-label={label}
    >
      {#if children}
        {@render children()}
      {:else}
        {#each options as option (option.value)}
          {@const checked = multiple ? values!.includes(option.value) : option.value === value}
          <button
            type="button"
            class="polymux-dropdown-item"
            role={multiple ? 'menuitemcheckbox' : 'menuitemradio'}
            aria-checked={checked}
            disabled={option.disabled}
            onclick={() => multiple ? onToggle(option.value) : choose(option.value)}
          >
            {#if option.provider}
              <span class="select-menu-icon"><DriveProviderLogo provider={option.provider} size={16}/></span>
            {:else if option.icon}
              <span class="select-menu-icon"><Icon name={option.icon} size={16} strokeWidth={1.5}/></span>
            {/if}
            <span>{option.label}</span>
            {#if keepOpenOnChange && !multiple}
              <span class="select-menu-check-slot">
                {#if checked}<Icon name="check" size={13}/>{/if}
              </span>
            {:else if checked}
              <Icon name="check" size={13}/>
            {/if}
          </button>
        {/each}
      {/if}
    </div>
  {/if}
</div>
