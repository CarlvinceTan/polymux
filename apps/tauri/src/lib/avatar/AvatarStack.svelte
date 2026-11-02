<script lang="ts">
  import type { Agent, Message, Run } from '../api/contract';
  import AgentAvatar from './AgentAvatar.svelte';
  import { thinking } from './activity';

  /** Grouped avatars, ported from AvatarUI's AvatarStack layout. */
  let {
    agents,
    size = 52,
    height,
    runs = [],
    messages = [],
    conversationID = '',
    connected = true,
  }: {
    agents: Agent[];
    size?: number;
    height?: number;
    runs?: Run[];
    messages?: Message[];
    conversationID?: string;
    connected?: boolean;
  } = $props();

  const count = $derived(Math.min(4, agents.length));
  const gap = $derived(size > 60 ? 14 : 10);
  const span = $derived(count === 2 ? 1.6 : count === 3 ? 1.5 : count === 4 ? 1.8 : 1);
  const faceSize = $derived(
    height !== undefined ? (height / span) * (316 / 200) : count > 1 ? (size - gap) * (count === 4 ? 1.6 / 1.8 : 1) : size,
  );
  const boxHeight = $derived(height ?? size);

  function offset(index: number): { x: number; y: number } {
    const diameter = (faceSize * 200) / 316;
    switch (count) {
      case 2:
        return { x: (index - 0.5) * diameter * 0.6, y: (index - 0.5) * diameter * 0.6 };
      case 3:
        return {
          x: index === 0 ? 0 : index === 1 ? -diameter * 0.3 : diameter * 0.3,
          y: index === 0 ? -diameter * 0.25 : diameter * 0.25,
        };
      case 4:
        return {
          x: (index % 2 === 0 ? -1 : 1) * diameter * 0.4,
          y: (index < 2 ? -1 : 1) * diameter * 0.4,
        };
      default:
        return { x: 0, y: 0 };
    }
  }
</script>

<div class="stack" style:width="{size}px" style:height="{boxHeight}px" aria-hidden="true">
  {#each agents.slice(0, 4) as member, index (member.id)}
    <div
      class="face"
      style:width="{faceSize}px"
      style:height="{faceSize}px"
      style:left="{size / 2 - faceSize / 2 + offset(index).x}px"
      style:top="{boxHeight / 2 - faceSize / 2 + offset(index).y}px"
      style:z-index={count - index}
    >
      <AgentAvatar agent={member} size={faceSize} thinking={connected && thinking(runs, messages, conversationID, member.id)} />
    </div>
  {/each}
</div>

<style>
  .stack {
    position: relative;
    flex: none;
  }
  .face {
    position: absolute;
  }
</style>
