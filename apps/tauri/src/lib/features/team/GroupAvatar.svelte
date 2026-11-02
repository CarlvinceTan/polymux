<script lang="ts">
  import type {BotDto} from '@polymux/protocol';
  import BloubAvatar from './BloubAvatar.svelte';
  import {bloubActivityForTeamStatus, bloubExpressionForTeamStatus} from './bloub/expression';
  import {groupAvatarLayout} from './groupAvatarLayout';

  export let members: BotDto[] = [];
  export let size = 38;
  export let label = '';
  export let animated = true;
  export let showActivity = true;

  $: layout = groupAvatarLayout(members.length);
  $: visible = members.slice(0, layout.visibleCount);
  $: tile = Math.round(size * layout.tileScale);
  $: overflowCount = Math.max(0, members.length - visible.length);

</script>

<span
  class:single={layout.kind === 'single'}
  class:pair={layout.kind === 'pair'}
  class:triple={layout.kind === 'triple'}
  class:quad={layout.kind === 'quad'}
  class="team-group-avatar"
  style:width={`${size}px`}
  style:height={`${size}px`}
  role={label ? 'img' : undefined}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : 'true'}
>
  {#each visible as member, index (member.id)}
    <span class="team-group-avatar-person" style:--group-avatar-index={index} style:--group-avatar-tile={`${tile}px`}>
      <BloubAvatar
        avatar={member.avatar}
        expression={bloubExpressionForTeamStatus(member.status, member.preview)}
        activity={showActivity ? bloubActivityForTeamStatus(member.status) : 'idle'}
        size={tile}
        {animated}
        paper="var(--team-group-surface,var(--app-bg))"
      />
    </span>
  {/each}
  {#if overflowCount}<i>+{overflowCount}</i>{/if}
</span>

<style>
  .team-group-avatar{position:relative;display:block;flex:none;isolation:isolate}.team-group-avatar-person{position:absolute;width:var(--group-avatar-tile);height:var(--group-avatar-tile);display:grid;place-items:center;border:0;background:transparent}
  .single .team-group-avatar-person{inset:0}.pair .team-group-avatar-person:nth-child(1){top:0;left:0;z-index:2}.pair .team-group-avatar-person:nth-child(2){right:0;bottom:0;z-index:1}
  .triple .team-group-avatar-person:nth-child(1){top:0;left:50%;z-index:3;transform:translateX(-50%)}.triple .team-group-avatar-person:nth-child(2){bottom:0;left:0;z-index:2}.triple .team-group-avatar-person:nth-child(3){right:0;bottom:0;z-index:1}
  .quad .team-group-avatar-person:nth-child(1){top:0;left:0;z-index:4}.quad .team-group-avatar-person:nth-child(2){top:0;right:0;z-index:3}.quad .team-group-avatar-person:nth-child(3){bottom:0;left:0;z-index:2}.quad .team-group-avatar-person:nth-child(4){right:0;bottom:0;z-index:1}
  i{position:absolute;z-index:5;right:-3px;bottom:-3px;min-width:18px;height:18px;display:grid;place-items:center;border:2px solid var(--team-group-surface,var(--app-bg));border-radius:10px;background:var(--neutral-800);color:var(--app-bg);font-size:10px;font-style:normal;font-weight:700;line-height:1}
</style>
