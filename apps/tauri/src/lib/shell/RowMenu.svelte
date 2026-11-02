<script lang="ts">
  import OpenMenu, { type OpenChoice } from '../shared/components/OpenMenu.svelte';
  import type { Session } from '../session.svelte';
  import type { Agent, Thread } from '../api/contract';

  /** The chat's own menu: edit, pin, move, avatar and delete. */
  let {
    session,
    thread,
    point,
    onClose,
    onProfile,
    onAvatar,
    onDelete,
    onCreateSection,
  }: {
    session: Session;
    thread: Thread;
    point: { x: number; y: number };
    onClose: () => void;
    onProfile: () => void;
    onAvatar: (agent: Agent) => void;
    onDelete: (thread: Thread) => void;
    onCreateSection: (thread: Thread) => void;
  } = $props();

  const isGroup = $derived(thread.id !== thread.agentID || thread.agentIDs.length > 1);
  const agent = $derived(session.agentsFor(thread)[0]);
  const sections = $derived(session.sections.filter((section) => section.id !== '__unassigned__'));

  const choices = $derived<OpenChoice[]>([
    { value: 'profile', label: isGroup ? 'Edit chat' : 'Edit profile', icon: 'edit' },
    { value: 'pin', label: thread.pinned ? 'Unpin' : 'Pin', icon: 'panel', disabled: !!session.sidebarMoves[thread.id] },
    ...sections.map((section) => ({
      value: 'move:' + section.id,
      label: `Move to ${section.name}`,
      icon: 'collapse' as const,
      disabled: !!session.sidebarMoves[thread.id],
    })),
    { value: 'move:__unassigned__', label: 'Move to Unassigned', icon: 'collapse', disabled: !!session.sidebarMoves[thread.id] },
    { value: 'create-section', label: 'Create section', icon: 'folder-plus', separatorBefore: true, disabled: !!session.sidebarMoves[thread.id] },
    ...(isGroup ? [] : [{ value: 'avatar', label: 'Customise avatar', icon: 'smile' as const, disabled: !agent }]),
    { value: 'delete', label: 'Delete', icon: 'trash', danger: true, disabled: !isGroup && !agent },
  ]);

  async function choose(value: string) {
    try {
      if (value === 'profile') {
        onProfile();
        return;
      }
      if (value === 'pin') {
        void session.placeChat(thread.id, { pinned: !thread.pinned });
      } else if (value.startsWith('move:')) {
        const id = value.slice(5);
        void session.placeChat(thread.id, { sectionID: id === '__unassigned__' ? undefined : id });
      } else if (value === 'create-section') {
        onCreateSection(thread);
      } else if (value === 'avatar' && agent) {
        onAvatar(agent);
      } else if (value === 'delete') {
        onDelete(thread);
      }
    } finally {
      onClose();
    }
  }
</script>

<OpenMenu {choices} anchor={{ point }} onChoose={choose} onClose={onClose} ariaLabel="Chat menu" />
