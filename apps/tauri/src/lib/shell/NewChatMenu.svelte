<script lang="ts">
  import { untrack } from 'svelte';
  import OpenMenu from '../shared/components/OpenMenu.svelte';
  import CreatePanel from './CreatePanel.svelte';
  import type { Session } from '../session.svelte';
  import type { Thread } from '../api/contract';

  let { session, point, initial, sectionID, onClose, onCreated }: {
    session: Session;
    point: { x: number; y: number };
    initial?: 'bot' | 'group';
    sectionID?: string;
    onClose: () => void;
    onCreated: (thread: Thread) => void;
  } = $props();

  let creation = $state<'bot' | 'group' | undefined>(untrack(() => initial));
</script>

{#if creation}
  <CreatePanel {session} kind={creation} {sectionID} {onClose} {onCreated} />
{:else}
  <OpenMenu
    choices={[
      { value: 'bot', label: 'New Bot' },
      { value: 'group', label: 'New Group Chat' },
    ]}
    anchor={{ point }}
    onChoose={(value) => { creation = value === 'bot' ? 'bot' : 'group'; }}
    {onClose}
    ariaLabel="New chat"
  />
{/if}
