<script lang="ts">
  import { tick } from 'svelte';

  export type MenuEntry = {
    id: string;
    label: string;
    shortcut?: string;
    submenu?: MenuEntry[];
    danger?: boolean;
    disabled?: boolean;
  };

  let {
    label = 'Open menu',
    items,
    onSelect,
  }: {
    label?: string;
    items: MenuEntry[];
    onSelect?: (entry: MenuEntry) => void;
  } = $props();

  let trigger = $state<HTMLButtonElement>();
  let menu = $state<HTMLDivElement>();
  let open = $state(false);
  let point = $state({ x: 0, y: 0 });
  let origin = $state('top left');
  let active = $state<string>();
  let submenu = $state<MenuEntry[]>();
  let submenuPoint = $state({ x: 0, y: 0 });
  let parentItem = $state<HTMLElement>();
  let submenuEl = $state<HTMLDivElement>();
  let safe = $state<{ x: number; y: number }>();
  let closeTimer: ReturnType<typeof setTimeout> | undefined;
  let openTimer: ReturnType<typeof setTimeout> | undefined;

  function show() {
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const width = 224;
    const height = Math.min(320, items.length * 34 + 10);
    const below = rect.bottom + 6;
    point = {
      x: Math.min(Math.max(8, rect.left), window.innerWidth - width - 8),
      y: below + height > window.innerHeight - 8 ? Math.max(8, rect.top - height - 6) : below,
    };
    origin = point.y < rect.top ? 'bottom left' : 'top left';
    open = true;
    active = undefined;
    submenu = undefined;
    void tick().then(() => menu?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus({ preventScroll: true }));
  }

  function close() {
    open = false;
    submenu = undefined;
    active = undefined;
    clearTimeout(closeTimer);
    clearTimeout(openTimer);
    trigger?.focus({ preventScroll: true });
  }

  function choose(entry: MenuEntry) {
    if (entry.disabled || entry.submenu) return;
    onSelect?.(entry);
    close();
  }

  function enter(entry: MenuEntry, event: PointerEvent, element: HTMLElement) {
    active = entry.id;
    safe = { x: event.clientX, y: event.clientY };
    parentItem = element;
    clearTimeout(closeTimer);
    clearTimeout(openTimer);
    if (!entry.submenu?.length) { submenu = undefined; return; }
    const rect = element.getBoundingClientRect();
    const width = 196;
    const height = Math.min(280, entry.submenu.length * 32 + 8);
    submenuPoint = {
      x: rect.right + 4 + width > window.innerWidth - 8 ? Math.max(8, rect.left - width - 4) : rect.right + 4,
      y: Math.min(Math.max(8, rect.top - 5), window.innerHeight - height - 8),
    };
    openTimer = setTimeout(() => { submenu = entry.submenu; }, 70);
  }

  function insideTriangle(x: number, y: number, a: { x: number; y: number }, b: { x: number; y: number }, c: { x: number; y: number }) {
    const sign = (p: { x: number; y: number }, q: { x: number; y: number }, r: { x: number; y: number }) =>
      (p.x - r.x) * (q.y - r.y) - (q.x - r.x) * (p.y - r.y);
    const d1 = sign({ x, y }, a, b);
    const d2 = sign({ x, y }, b, c);
    const d3 = sign({ x, y }, c, a);
    const hasNegative = d1 < 0 || d2 < 0 || d3 < 0;
    const hasPositive = d1 > 0 || d2 > 0 || d3 > 0;
    return !(hasNegative && hasPositive);
  }

  function leave(event: PointerEvent) {
    if (!submenu || !safe || !submenuEl) { submenu = undefined; active = undefined; return; }
    const rect = submenuEl.getBoundingClientRect();
    const top = { x: rect.left, y: rect.top + 6 };
    const bottom = { x: rect.left, y: rect.bottom - 6 };
    if (insideTriangle(event.clientX, event.clientY, safe, top, bottom)) return;
    clearTimeout(closeTimer);
    closeTimer = setTimeout(() => { submenu = undefined; active = undefined; }, 120);
  }

  function submenuEnter() {
    clearTimeout(closeTimer);
  }

  function keys(event: KeyboardEvent) {
    if (event.key === 'Escape') { event.preventDefault(); close(); return; }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const buttons = [...(menu?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])];
      const index = buttons.indexOf(event.target as HTMLButtonElement);
      buttons[event.key === 'ArrowDown' ? Math.min(index + 1, buttons.length - 1) : Math.max(index - 1, 0)]?.focus();
    }
  }

  $effect(() => {
    if (!open) return;
    const dismiss = (event: Event) => {
      const target = event.target as Node;
      if (menu?.contains(target) || submenuEl?.contains(target) || trigger?.contains(target)) return;
      open = false;
      submenu = undefined;
    };
    document.addEventListener('pointerdown', dismiss, true);
    window.addEventListener('resize', dismiss);
    return () => { document.removeEventListener('pointerdown', dismiss, true); window.removeEventListener('resize', dismiss); };
  });
</script>

<svelte:document onclick={(event) => {
  const target = event.target as Node;
  if (open && !menu?.contains(target) && !submenuEl?.contains(target) && !trigger?.contains(target)) open = false;
}} />

<button bind:this={trigger} class="trigger" aria-haspopup="menu" aria-expanded={open} onclick={() => (open ? close() : show())}>
  {label}
  <svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><path d="m4 6 4 4 4-4"></path></svg>
</button>

{#if open}
  <div class="menu" bind:this={menu} role="menu" aria-label={label} tabindex="-1" style:left="{point.x}px" style:top="{point.y}px" style:transform-origin={origin}
    onkeydown={keys} onpointerleave={() => { if (!submenu) { active = undefined; } }}>
    {#each items as entry (entry.id)}
      <button class="item" class:danger={entry.danger} class:active={active === entry.id} role="menuitem" disabled={entry.disabled}
        aria-haspopup={entry.submenu?.length ? 'menu' : undefined} aria-expanded={entry.submenu?.length ? submenu === entry.submenu : undefined}
        onpointerenter={(event) => enter(entry, event, event.currentTarget)} onpointerleave={leave} onfocus={(event) => enter(entry, { clientX: 0, clientY: 0 } as PointerEvent, event.currentTarget)} onclick={() => choose(entry)}>
        <span>{entry.label}</span>
        {#if entry.shortcut}<kbd>{entry.shortcut}</kbd>{/if}
        {#if entry.submenu?.length}<svg class="chevron" viewBox="0 0 16 16" width="11" height="11" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true"><path d="m6 3 5 5-5 5"></path></svg>{/if}
      </button>
    {/each}
  </div>
{/if}

{#if submenu}
  <div class="menu submenu" bind:this={submenuEl} role="menu" aria-label="More options" tabindex="-1" style:left="{submenuPoint.x}px" style:top="{submenuPoint.y}px" onpointerenter={submenuEnter} onpointerleave={leave}>
    {#each submenu as entry (entry.id)}
      <button class="item" class:danger={entry.danger} role="menuitem" disabled={entry.disabled} onclick={() => choose(entry)}>{entry.label}</button>
    {/each}
  </div>
{/if}

<style>
  .trigger { display: inline-flex; align-items: center; gap: 7px; min-height: 30px; padding: 0 12px; border-radius: 8px; border: 0.5px solid var(--glass-border); background: var(--glass-field); color: var(--text); font: inherit; font-size: 12px; }
  .trigger:hover, .trigger:focus-visible { background: var(--row-hover); }
  .menu { position: fixed; z-index: 80; width: 224px; padding: 5px; border-radius: 12px; border: 1px solid var(--hairline); background: var(--window-bg); box-shadow: 0 12px 34px rgba(0,0,0,.2), 0 0 0 0.5px rgba(0,0,0,.04); animation: menu-in 0.14s cubic-bezier(.2,.8,.2,1); }
  .submenu { width: 196px; animation: submenu-in 0.12s cubic-bezier(.2,.8,.2,1); }
  .item { display: flex; align-items: center; gap: 8px; width: 100%; min-height: 28px; padding: 0 8px; border-radius: 7px; background: transparent; color: var(--text); text-align: left; font: inherit; font-size: 12px; }
  .item > span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .item:hover, .item.active, .item:focus-visible { background: var(--row-hover); }
  .item.danger { color: var(--danger); }
  .item:disabled { opacity: .42; }
  kbd { color: var(--text-muted); font: inherit; font-size: 10px; }
  .chevron { color: var(--text-muted); }
  @keyframes menu-in { from { opacity: 0; transform: scale(.96); } to { opacity: 1; transform: none; } }
  @keyframes submenu-in { from { opacity: 0; transform: translateX(-3px); } to { opacity: 1; transform: none; } }
  @media (prefers-reduced-motion: reduce) { .menu, .submenu { animation: none; } }
</style>
