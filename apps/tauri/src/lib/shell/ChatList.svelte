<script lang="ts">
  import type { Session } from '../session.svelte';
  import type { Section, Thread } from '../api/contract';
  import ChatRow from './ChatRow.svelte';
  import AvatarStack from '../avatar/AvatarStack.svelte';
  import OpenMenu from '../shared/components/OpenMenu.svelte';
  import { tick } from 'svelte';

  let {
    session,
    onMenu,
    onNewBot,
  }: {
    session: Session;
    onMenu?: (thread: Thread, point: { x: number; y: number }) => void;
    onNewBot: (sectionID?: string) => void;
  } = $props();

  const pins = $derived(session.pinned);
  const sections = $derived(session.visibleSections);
  let menu = $state<{ section?: Section; point: { x: number; y: number } } | undefined>();
  let editing = $state<string>();
  let draft = $state('');
  let moveAfter = $state<string>();
  let input = $state<HTMLInputElement>();
  let issue = $state('');
  let deleting = $state<Section>();
  let busy = $state(false);
  let began = 0;
  let dragging = $state<string>();
  let dropTarget = $state<{ id: string; after: boolean }>();
  let dragChat = $state<string>();
  let dragReady = $state(false);
  let chatTarget = $state<{ kind: 'pins'; before?: string } | { kind: 'section'; id: string }>();
  let pinFrames: { id: string; frame: DOMRect }[] = [];
  let list = $state<HTMLElement>();
  const draftID = '__new_section__';
  const dragSections = $derived.by(() => {
    if (!dragReady || sections.some(section => section.id === '__unassigned__')) return sections;
    const unassigned = session.sections.find(section => section.id === '__unassigned__')
      ?? { id: '__unassigned__', name: 'Unassigned', collapsed: false, order: 0 } as Section;
    return [...sections, unassigned].sort((left, right) => left.order - right.order);
  });
  const shownSections = $derived(editing === draftID
    ? [...dragSections, { id: draftID, name: draft, collapsed: false, order: dragSections.length } as Section]
    : dragSections);
  const shownPins = $derived.by(() => {
    if (!dragChat || chatTarget?.kind !== 'pins' || chatTarget.before === dragChat) return pins;
    const moving = session.threads.find(thread => thread.id === dragChat);
    if (!moving) return pins;
    const result = pins.filter(thread => thread.id !== dragChat);
    const before = chatTarget.before;
    const index = result.findIndex(thread => thread.id === before);
    result.splice(index < 0 ? result.length : index, 0, moving);
    return result;
  });

  export function createSection(thread?: Thread) {
    edit(undefined, thread);
  }

  function edit(section?: Section, thread?: Thread) {
    if (busy) return;
    const names = new Set(session.sections.map(item => item.name.toLocaleLowerCase()));
    let name = 'New section';
    for (let index = 2; names.has(name.toLocaleLowerCase()); index++) name = `New section ${index}`;
    draft = section?.name ?? name;
    editing = section?.id ?? draftID;
    moveAfter = thread?.id;
    menu = undefined;
    void tick().then(() => { began = performance.now(); input?.focus(); input?.select(); });
  }

  function blur(event: FocusEvent) {
    if (event.relatedTarget === null && performance.now() - began < 600) {
      void tick().then(() => { if (editing) input?.focus(); });
      return;
    }
    if (editing === draftID) { editing = undefined; issue = ''; }
    else void finish();
  }

  function notify(message: string) {
    issue = message;
    setTimeout(() => { if (issue === message) issue = ''; }, 8000);
  }

  async function finish() {
    const id = editing;
    if (!id) return;
    const name = draft.trim();
    if ([...name].length > 40) { issue = 'Section names are limited to 40 characters.'; return; }
    editing = undefined;
    issue = '';
    if (!name) return;
    if (busy) return;
    busy = true;
    session.beginSidebarChange();
    let saved = false;
    if (id === draftID) {
      const local = { id: `local-${crypto.randomUUID()}`, name, collapsed: false, order: sections.length } as Section;
      session.sections = [...session.sections, local];
      const target = moveAfter;
      moveAfter = undefined;
      try {
        const created = await session.client.createSection(name);
        session.sections = session.sections.map(item => item.id === local.id ? created : item);
        saved = true;
        if (target) void session.placeChat(target, { sectionID: created.id });
      } catch { session.sections = session.sections.filter(item => item.id !== local.id); notify('Couldn’t create this section.'); }
    } else {
      const section = session.sections.find(item => item.id === id);
      if (section && section.name !== name) {
        const previousName = section.name;
        session.sections = session.sections.map(item => item.id === id ? { ...item, name } : item);
        try { await session.client.updateSection(id, name); saved = true; }
        catch {
          session.sections = session.sections.map(item => item.id === id && item.name === name ? { ...item, name: previousName } : item);
          notify('Couldn’t rename this section.');
        }
      }
    }
    session.endSidebarChange();
    busy = false;
    if (saved) void session.refresh().catch(() => {});
  }

  async function removeSection(section: Section, chats: boolean) {
    if (busy) return;
    busy = true;
    session.beginSidebarChange();
    deleting = undefined;
    const previous = { ...section };
    const threads = session.threads.filter(item => item.sectionID === section.id).map(item => ({ ...item }));
    session.sections = session.sections.filter(item => item.id !== section.id);
    session.threads = chats ? session.threads.filter(item => item.sectionID !== section.id)
      : session.threads.map(item => item.sectionID === section.id ? { ...item, sectionID: undefined, sectionOrder: undefined } : item);
    let saved = false;
    try { await session.client.deleteSection(section.id, chats); saved = true; }
    catch {
      if (!session.sections.some(item => item.id === section.id)) session.sections = [...session.sections, previous];
      for (const thread of threads) {
        const current = session.threads.find(item => item.id === thread.id);
        if (chats && !current) session.threads = [...session.threads, thread];
        else if (!chats && current && !current.sectionID) {
          session.threads = session.threads.map(item => item.id === thread.id ? { ...item, sectionID: thread.sectionID, sectionOrder: thread.sectionOrder } : item);
        }
      }
      notify('Couldn’t delete this section.');
    } finally {
      session.endSidebarChange();
      busy = false;
      if (saved) void session.refresh().catch(() => {});
    }
  }

  function title(thread: Thread): string {
    if (thread.id !== thread.agentID || thread.agentIDs.length > 1) return thread.title;
    return session.agentsFor(thread)[0]?.name ?? thread.title;
  }

  function select(thread: Thread) {
    session.selection = thread.id;
    void session.open(thread);
  }

  async function toggle(section: Section) {
    if (busy) return;
    busy = true;
    session.beginSidebarChange();
    const collapsed = !section.collapsed;
    const previous = session.sections.find(item => item.id === section.id);
    if (session.sections.some(item => item.id === section.id)) {
      session.sections = session.sections.map(item => item.id === section.id ? { ...item, collapsed } : item);
    } else {
      session.sections = [...session.sections, { ...section, collapsed }];
    }
    try { await session.client.updateSection(section.id, undefined, collapsed); }
    catch {
      session.sections = previous
        ? session.sections.map(item => item.id === section.id && item.collapsed === collapsed ? { ...item, collapsed: previous.collapsed } : item)
        : session.sections.filter(item => item.id !== section.id || item.collapsed !== collapsed);
      notify('Couldn’t update this section.');
    } finally { session.endSidebarChange(); busy = false; }
  }

  async function move(section: Section, direction: -1 | 1) {
    if (busy) return;
    const index = sections.findIndex(item => item.id === section.id);
    const neighbor = sections[index + direction];
    if (index < 0 || !neighbor) return;
    const source = direction < 0 ? section.id : neighbor.id;
    const target = direction < 0 ? neighbor.id : section.id;
    await reorder(source, target);
  }

  async function reorder(source: string, target?: string) {
    if (busy || source === target) return;
    const all = [...session.sections];
    const actual = new Set(all.map(item => item.id));
    if (!all.some(item => item.id === '__unassigned__')) {
      const unassigned = sections.find(item => item.id === '__unassigned__');
      if (unassigned) all.push(unassigned);
    }
    all.sort((left, right) => left.order - right.order || left.id.localeCompare(right.id));
    const previous = new Map(all.map(item => [item.id, item.order]));
    const from = all.findIndex(item => item.id === source);
    if (from < 0) return;
    const [item] = all.splice(from, 1);
    const to = target === undefined ? all.length : all.findIndex(item => item.id === target);
    if (to < 0) return;
    all.splice(to, 0, item);
    const ordered = all.map((value, order) => ({ ...value, order }));
    busy = true;
    session.beginSidebarChange();
    session.sections = ordered;
    let saved = false;
    try { await session.client.reorderSection(source, target); saved = true; }
    catch {
      const changed = new Map(ordered.map(value => [value.id, value.order]));
      session.sections = session.sections.map(value => previous.has(value.id) && value.order === changed.get(value.id)
        ? { ...value, order: previous.get(value.id)! } : value)
        .filter(value => actual.has(value.id) || value.id !== '__unassigned__' || value.order !== previous.get(value.id));
      notify('Couldn’t move this section.');
    } finally {
      session.endSidebarChange();
      busy = false;
      if (saved) void session.refresh().catch(() => {});
    }
  }

  function startDrag(event: DragEvent, section: Section) {
    if (busy || editing || !event.dataTransfer) { event.preventDefault(); return; }
    dragging = section.id;
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('application/x-polymux-section', section.id);
  }

  function startChat(event: DragEvent, thread: Thread) {
    if (busy || editing || session.sidebarMoves[thread.id] || !event.dataTransfer) { event.preventDefault(); return; }
    pinFrames = Array.from(list?.querySelectorAll<HTMLElement>('[data-pin]') ?? []).map(element => ({ id: element.dataset.pin!, frame: element.getBoundingClientRect() }));
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('application/x-polymux-chat', thread.id);
    dragChat = thread.id;
  }

  function pinBefore(x: number, y: number): string | undefined {
    const rows: typeof pinFrames[] = [];
    for (const pin of pinFrames) {
      const row = rows.at(-1);
      if (row && row[0].frame.top === pin.frame.top) row.push(pin);
      else rows.push([pin]);
    }
    for (let index = 0; index < rows.length; index++) {
      const row = rows[index];
      const bottom = Math.max(...row.map(pin => pin.frame.bottom));
      const next = rows[index + 1]?.[0].frame.top ?? bottom;
      if (y <= (bottom + next) / 2) {
        return row.find(pin => x <= pin.frame.left + pin.frame.width / 2)?.id ?? rows[index + 1]?.[0].id;
      }
    }
    return undefined;
  }

  function chatOver(event: DragEvent) {
    if (!list || !dragChat || busy) return;
    const contains = (frame: DOMRect) => event.clientX >= frame.left && event.clientX <= frame.right && event.clientY >= frame.top && event.clientY <= frame.bottom;
    const grid = list.querySelector<HTMLElement>('.pins');
    if (grid && contains(grid.getBoundingClientRect())) {
      chatTarget = { kind: 'pins', before: pinBefore(event.clientX, event.clientY) };
    } else {
      const area = Array.from(list.querySelectorAll<HTMLElement>('[data-section]')).find(element => contains(element.getBoundingClientRect()));
      chatTarget = area?.dataset.section ? { kind: 'section', id: area.dataset.section } : undefined;
    }
    if (chatTarget) {
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
    }
  }

  function dragOver(event: DragEvent) {
    if (dragChat) { dragReady = true; chatOver(event); return; }
    if (!dragging || !list || busy) return;
    const source = sections.findIndex(item => item.id === dragging);
    if (source < 0) return;
    let nearest: { id: string; distance: number } | undefined;
    for (const element of list.querySelectorAll<HTMLElement>('[data-section]')) {
      const id = element.dataset.section;
      if (!id || !sections.some(section => section.id === id)) continue;
      const frame = element.getBoundingClientRect();
      const distance = (event.clientX - (frame.left + frame.width / 2)) ** 2 + (event.clientY - (frame.top + frame.height / 2)) ** 2;
      if (!nearest || distance < nearest.distance) nearest = { id, distance };
    }
    const target = nearest && sections.findIndex(item => item.id === nearest.id);
    dropTarget = nearest && target !== undefined && target >= 0 && target !== source
      ? { id: nearest.id, after: source < target } : undefined;
    if (dropTarget) {
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
    }
  }

  function drop(event: DragEvent) {
    if (dragChat && chatTarget) {
      event.preventDefault();
      const thread = session.threads.find(item => item.id === dragChat);
      const target = chatTarget;
      endDrag();
      if (!thread) return;
      if (target.kind === 'pins' && target.before !== thread.id) void session.placeChat(thread.id, { pinned: true, before: target.before });
      else if (target.kind === 'section') {
        const id = target.id === '__unassigned__' ? undefined : target.id;
        if (thread.pinned || (thread.sectionID || undefined) !== id) void session.placeChat(thread.id, { sectionID: id });
      }
      return;
    }
    if (!dragging || !dropTarget) return;
    event.preventDefault();
    const source = dragging;
    const index = sections.findIndex(section => section.id === dropTarget?.id);
    const before = dropTarget.after ? sections[index + 1]?.id : dropTarget.id;
    dragging = undefined;
    dropTarget = undefined;
    if (index >= 0) void reorder(source, before);
  }

  function endDrag() { dragging = undefined; dropTarget = undefined; dragChat = undefined; dragReady = false; chatTarget = undefined; pinFrames = []; }
</script>

<div class="list" bind:this={list} role="presentation" ondragover={dragOver} ondrop={drop} ondragleave={(event) => {
  if (!event.currentTarget.contains(event.relatedTarget as Node | null)) { dropTarget = undefined; chatTarget = undefined; }
}} oncontextmenu={(event) => {
  if (event.defaultPrevented || busy) return;
  event.preventDefault();
  menu = { point: { x: event.clientX, y: event.clientY } };
}}>
  {#if pins.length || dragReady}
    <div class="pins" class:chat-drop={chatTarget?.kind === 'pins'}>
      {#each shownPins as thread (thread.id)}
        <button class="pin" class:selected={thread.id === session.selectedChat?.id} onclick={() => select(thread)}
          class:dragging={thread.id === dragChat} data-pin={thread.id} draggable="true" ondragstart={(event) => startChat(event, thread)} ondragend={endDrag}
          oncontextmenu={(event) => { event.preventDefault(); event.stopPropagation(); onMenu?.(thread, { x: event.clientX, y: event.clientY }); }}>
          <div class="tile-avatar">
            <AvatarStack agents={session.agentsFor(thread)} size={76} />
            {#if thread.unreadCount > 0}<span class="pin-dot"></span>{/if}
          </div>
          <span class="pin-title">{title(thread)}</span>
        </button>
      {:else}<span class="empty-pins">Pinned</span>{/each}
    </div>
  {/if}

  {#each shownSections as section (section.id)}
    {@const chats = session.chatsOf(section)}
    <div class="section" data-section={section.id} class:dragging={dragging === section.id}
      class:chat-drop={chatTarget?.kind === 'section' && chatTarget.id === section.id}
      class:drop-before={dropTarget?.id === section.id && !dropTarget.after}
      class:drop-after={dropTarget?.id === section.id && dropTarget.after}>
      {#if editing === section.id}
        <div class="head editor">
          <input bind:this={input} bind:value={draft} aria-label="Section name"
            onkeydown={(event) => { if (event.key === 'Enter') { event.preventDefault(); void finish(); } else if (event.key === 'Escape') { editing = undefined; issue = ''; } }}
            onblur={blur} />
        </div>
      {:else}
      <button class="head" aria-label="{section.collapsed ? 'Expand' : 'Collapse'} {section.name}" disabled={busy} onclick={() => toggle(section)}
        draggable="true" aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown"
        ondragstart={(event) => startDrag(event, section)} ondragend={endDrag}
        onkeydown={(event) => { if (event.altKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) { event.preventDefault(); void move(section, event.key === 'ArrowUp' ? -1 : 1); } }}
        oncontextmenu={(event) => { event.preventDefault(); event.stopPropagation(); if (!busy) menu = { section, point: { x: event.clientX, y: event.clientY } }; }}>
        <span class="head-name">{section.name}</span>
        {#if section.collapsed}
          <span class="count">{chats.length}</span>
        {:else}
          <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2">
            <path d="m6 9 6 6 6-6"></path>
          </svg>
        {/if}
      </button>
      {/if}
      {#if !section.collapsed}
        {#each chats as thread (thread.id)}
          <ChatRow
            {session}
            {thread}
            selected={thread.id === session.selectedChat?.id}
            chooseRow={() => select(thread)}
            openMenu={(point) => onMenu?.(thread, point)}
            dragging={thread.id === dragChat}
            onDragStart={(event) => startChat(event, thread)}
            onDragEnd={endDrag}
          />
        {:else}
          <div class="empty">No chats yet.</div>
        {/each}
      {/if}
    </div>
  {/each}
  {#if issue}<div class="issue" role="status">{issue}</div>{/if}
  {#if session.sidebarIssue}<div class="issue" role="status">{session.sidebarIssue}</div>{/if}
</div>

{#if menu}
  <OpenMenu anchor={{ point: menu.point }} ariaLabel="Section actions"
    choices={[
      ...(menu.section ? [{ value: 'bot', label: 'New Bot', icon: 'smile' as const }] : []),
      ...(menu.section && menu.section.id !== '__unassigned__' ? [{ value: 'rename', label: 'Rename', icon: 'edit' as const, separatorBefore: true }] : []),
      ...(menu.section ? [
        { value: 'up', label: 'Move up', icon: 'arrow-up' as const, separatorBefore: true, disabled: sections[0]?.id === menu.section.id },
        { value: 'down', label: 'Move down', icon: 'arrow-down' as const, disabled: sections.at(-1)?.id === menu.section.id },
      ] : []),
      { value: 'create', label: 'Create section', icon: 'folder-plus' as const, separatorBefore: !!menu.section },
      ...(menu.section && menu.section.id !== '__unassigned__' ? [{ value: 'delete', label: 'Delete section', icon: 'trash' as const, separatorBefore: true, danger: true }] : []),
    ]}
    onChoose={(value) => {
      const section = menu?.section;
      menu = undefined;
      if (value === 'rename' && section) edit(section);
      else if (value === 'bot' && section) onNewBot(section.id === '__unassigned__' ? undefined : section.id);
      else if (value === 'up' && section) void move(section, -1);
      else if (value === 'down' && section) void move(section, 1);
      else if (value === 'create') edit();
      else if (value === 'delete') deleting = section;
    }}
    onClose={() => menu = undefined} />
{/if}

{#if deleting}
  {@const section = deleting}
  {@const count = session.chatsOf(section).length}
  <div class="confirm-shade">
    <div class="confirm" role="alertdialog" aria-modal="true" aria-label="Delete {section.name}?" tabindex="-1"
      onkeydown={(event) => { if (event.key === 'Escape') deleting = undefined; }}>
      <strong>Delete “{section.name}”?</strong>
      <p>{count ? `Deleting just the section moves its ${count === 1 ? 'chat' : `${count} chats`} to the main list. Deleting its chats also permanently removes them, and each bot whose own chat is in it.` : 'This section has no chats.'}</p>
      <div class="confirm-actions">
        <button onclick={() => deleting = undefined}>Cancel</button>
        <button onclick={() => void removeSection(section, false)}>Delete section</button>
        {#if count}<button class="danger" onclick={() => void removeSection(section, true)}>Delete section and chats</button>{/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .list {
    padding: 0 0 12px;
    min-height: 100%;
    box-sizing: border-box;
  }
  .pins {
    display: grid;
    grid-template-columns: repeat(auto-fit, 95px);
    gap: 12px 4px;
    justify-content: center;
    padding: 12px 4px 38px;
  }
  .pin {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 6px 8px 4px;
    border-radius: 10px;
    min-width: 0;
  }
  .pin:hover {
    background: var(--row-hover);
  }
  .pin.selected {
    background: var(--row-selected);
  }
  .pin.dragging { opacity: 0; }
  .empty-pins { grid-column: 1 / -1; min-height: 44px; display: grid; place-items: center; color: var(--text-muted); }
  .pins.chat-drop, .section.chat-drop { border-radius: 8px; background: color-mix(in srgb, var(--text) 6%, transparent); }
  .tile-avatar {
    position: relative;
  }
  .pin-dot {
    position: absolute;
    right: -4px;
    bottom: 8px;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--unread-dot);
    box-shadow: 0 0 0 2px var(--sidebar-bg);
  }
  .pin-title {
    font-size: 12px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .section {
    margin-top: 2px;
    position: relative;
  }
  .section.dragging { opacity: .4; }
  .section.drop-before::before, .section.drop-after::after { content: ''; position: absolute; left: 0; right: 0; height: 1px; background: var(--text-secondary); pointer-events: none; }
  .section.drop-before::before { top: 0; }
  .section.drop-after::after { bottom: 0; }
  .head {
    display: flex;
    align-items: center;
    width: 100%;
    padding: 6px 20px;
    margin-bottom: 2px;
    font-size: 13px;
    color: var(--text);
    opacity: 0.7;
  }
  .head:hover {
    opacity: 1;
  }
  .head-name {
    flex: 1;
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .editor input { width: 100%; min-width: 0; border: 0; outline: 0; background: transparent; color: var(--text); font: inherit; }
  .issue { padding: 6px 20px; color: var(--text-muted); font-size: 11px; }
  .confirm-shade { position: fixed; inset: 0; z-index: 300; display: grid; place-items: center; background: rgba(0,0,0,.25); }
  .confirm { width: min(380px, calc(100vw - 48px)); box-sizing: border-box; padding: 20px; border-radius: 16px; background: var(--panel-bg); box-shadow: 0 12px 40px rgba(0,0,0,.24); }
  .confirm p { color: var(--text-muted); line-height: 1.4; }
  .confirm-actions { display: flex; gap: 8px; justify-content: flex-end; flex-wrap: wrap; }
  .confirm-actions button { padding: 7px 12px; border-radius: 8px; background: var(--glass-field); }
  .confirm-actions .danger { color: var(--danger); }
  .count {
    font-size: 13px;
  }
  .empty {
    padding: 10px 20px;
    font-size: 13px;
    color: var(--text);
    opacity: 0.7;
  }
</style>
