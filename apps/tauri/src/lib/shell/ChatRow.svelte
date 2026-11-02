<script lang="ts">
  import type { Session } from '../session.svelte';
  import type { Thread } from '../api/contract';
  import AvatarStack from '../avatar/AvatarStack.svelte';
  import { progressiveTime } from '../time';

  /** One chat row: 34pt avatar column, name, role, time, preview and unread dot. */
  let {
    session,
    thread,
    selected,
    chooseRow,
    openMenu,
    dragging = false,
    onDragStart,
    onDragEnd,
  }: {
    session: Session;
    thread: Thread;
    selected: boolean;
    chooseRow: () => void;
    openMenu?: (point: { x: number; y: number }) => void;
    dragging?: boolean;
    onDragStart?: (event: DragEvent) => void;
    onDragEnd?: () => void;
  } = $props();

  const isGroup = $derived(thread.id !== thread.agentID || thread.agentIDs.length > 1);
  const agents = $derived(session.agentsFor(thread));
  const agent = $derived(agents[0]);
  const title = $derived(isGroup ? thread.title : (agent?.name ?? thread.title));
  const role = $derived(isGroup ? '' : (agent?.role ?? '').trim());
  const duplicate = $derived(!isGroup && session.agents.filter((item) => item.name === agent?.name).length > 1);
  const roleLabel = $derived([role, duplicate ? agent?.id.slice(0, 6) : ''].filter(Boolean).join(' · '));
  const state = $derived([thread.unreadCount > 0 ? 'Unread' : '', session.online ? 'Ready' : ''].filter(Boolean).join(', '));
  const accessible = $derived(isGroup
    ? `${title}, ${agents.map((item) => item.name).join(', ')}`
    : [state, title, role, roleLabel].filter(Boolean).join(', '));
  const last = $derived(thread.lastMessage);
  const view = $derived(session.view(thread));

  const preview = $derived.by(() => {
    if (!last) return '';
    const question = !isGroup && last.parts.find((part) => part.kind === 'choice')?.question;
    if (question) return question;
    const text = last.parts.map((part) => part.text ?? '').join('');
    if (text) return text.replace(/\n/g, ' ');
    return last.parts.find((part) => part.attachment)?.attachment?.name ?? '';
  });

  const stamp = $derived(last ? progressiveTime(new Date(last.createdAt)) : '');
</script>

<button class="row" class:selected class:dragging draggable="true" aria-label={accessible} ondragstart={onDragStart} ondragend={onDragEnd} onclick={() => chooseRow()} oncontextmenu={(event) => {
  event.preventDefault();
  openMenu?.({ x: event.clientX, y: event.clientY });
}}>
  <div class="avatar">
    <AvatarStack {agents} size={52} runs={Object.values(session.runs)} messages={session.messages[thread.id] ?? []} conversationID={thread.id} connected={session.online} />
  </div>
  <div class="details">
    <div class="line">
      <span class="name">{title}</span>
      {#if roleLabel}<span class="role">{roleLabel}</span>{/if}
      <span class="spacer"></span>
      {#if thread.pinned}
        <svg class="pin" viewBox="0 0 24 24" width="10" height="10" fill="currentColor">
          <path
            d="M16 12a4.06 4.06 0 0 0-2-3.45V4a1 1 0 0 0-2 0v4.55A4.06 4.06 0 0 0 10 12a1 1 0 0 0 0 2h2.5v6a1 1 0 0 0 2 0v-6H15a1 1 0 0 0 1-2Z"
          ></path>
        </svg>
      {/if}
      {#if stamp}<span class="stamp">{stamp}</span>{/if}
    </div>
    {#if preview}<div class="preview">{preview}</div>{/if}
  </div>
  {#if view.unread && thread.unreadCount > 0}<span class="dot"></span>{/if}
</button>

<style>
  .row {
    position: relative;
    display: flex;
    outline: none;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 4px 10px 4px 18px;
    text-align: left;
    border-radius: 10px;
    isolation: isolate;
  }
  .row::before {
    content: '';
    position: absolute;
    inset: 0 10px;
    border-radius: 10px;
    z-index: -1;
  }
  .row:hover::before {
    background: var(--row-hover);
  }
  .row.dragging { opacity: .4; }
  .row.selected::before {
    background: var(--row-selected);
  }
  .avatar {
    width: 34px;
    height: 52px;
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .details {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 4px 0;
  }
  .line {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }
  .name {
    font-size: 13px;
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex-shrink: 1;
  }
  .role {
    font-size: 10px;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex-shrink: 2;
  }
  .spacer {
    flex: 1;
    min-width: 4px;
  }
  .pin {
    color: var(--text-muted);
    flex: none;
  }
  .stamp {
    font-size: 10px;
    color: var(--text-muted);
    white-space: nowrap;
    flex: none;
  }
  .preview {
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--unread-dot);
    flex: none;
    margin-left: 2px;
  }
</style>
