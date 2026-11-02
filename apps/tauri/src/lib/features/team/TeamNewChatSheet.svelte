<script lang="ts">
  import type {BotDto, TeamGroupDto} from '@polymux/protocol';
  import {onMount, tick} from 'svelte';
  import {fade} from 'svelte/transition';
  import {scrollFade} from '../../shared/scrollFade';
  import Icon from '../../shared/components/Icon.svelte';
  import {activateModalDialog, trapModalFocus} from '../../shared/dialogFocus';
  import {clockTime} from '../../shared/displayTime';
  import {activeLocale, plural, t} from '../../../i18n';
  import BloubAvatar from './BloubAvatar.svelte';
  import TeamRoleLabel from './TeamRoleLabel.svelte';
  import GroupAvatar from './GroupAvatar.svelte';
  import {bloubActivityForTeamStatus, bloubExpressionForTeamStatus} from './bloub/expression';

  export let bots: BotDto[] = [];
  export let teamGroups: TeamGroupDto[] = [];
  export let onOpenTeam: (id: string) => void = () => {};
  export let onOpenTeamGroup: (id: string) => void = () => {};
  export let onNewBot: (initialName: string) => void = () => {};
  export let onNewGroup: () => void = () => {};
  export let onClose: () => void = () => {};
  /** True while a bot or group editor is stacked on top of this chooser, so
   * the chooser stays mounted — the editor's opener lives here, and closing the
   * editor has to return focus to it — but yields as the active dialog. */
  export let nested = false;

  let dialog: HTMLDivElement;
  let input: HTMLInputElement;
  let query = '';
  const dialogFade = {duration: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 140};

  $: needle = query.trim().toLowerCase();
  $: matchedBots = (needle
    ? bots.filter((member) => `${member.name} ${member.role}`.toLowerCase().includes(needle))
    : bots
  ).slice().sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
  $: matchedGroups = (needle
    ? teamGroups.filter((group) => group.name.toLowerCase().includes(needle))
    : teamGroups
  ).slice().sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
  $: exactName = needle && bots.some((member) => member.name.toLowerCase() === needle);
  $: showCreateRow = Boolean(needle) && !exactName;

  onMount(() => {
    const restoreFocus = activateModalDialog(dialog);
    void tick().then(() => input?.focus());
    return restoreFocus;
  });

  function membersOf(group: TeamGroupDto): BotDto[] {
    return group.memberIds.flatMap((id) => bots.find((member) => member.id === id) ?? []);
  }

  function groupStatus(group: TeamGroupDto): string {
    const working = membersOf(group).filter((member) => member.status === 'working').length;
    return working
      ? plural('team.workingCount', working)
      : plural('team.agentCount', group.memberIds.length);
  }

  /** A row's stamp, in the interface language and on the shared 12-hour clock:
   * the time for today, the date beyond it. */
  function teamTime(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    if (date.toDateString() === new Date().toDateString()) return clockTime(date);
    return new Intl.DateTimeFormat(activeLocale(), {month: 'short', day: 'numeric'}).format(date);
  }

  function teamStatus(member: BotDto): string {
    if (member.status === 'working') return $t('team.statusWorking');
    if (member.status === 'waiting-for-device') return $t('team.statusWaitingForAccess');
    if (member.status === 'error') return member.computer.detail || $t('team.statusNeedsAttention');
    return '';
  }

  function teamPreview(member: BotDto): string {
    const preview = member.preview.trim();
    return preview && preview !== member.role ? preview : $t('team.noMessagesYet');
  }

  function keydown(event: KeyboardEvent): void {
    if (nested) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
      return;
    }
    trapModalFocus(event, dialog);
  }
</script>

<div class="team-new-chat-backdrop" role="presentation" transition:fade={dialogFade} onclick={(event) => !nested && event.target === event.currentTarget && onClose()}>
  <div bind:this={dialog} class="team-new-chat" role="dialog" aria-modal={nested ? 'false' : 'true'} aria-label={$t('team.newChat')} tabindex="-1" onkeydown={keydown}>
    <div class="team-new-chat-head">
      <h2>{$t('team.newChat')}</h2>
      <button type="button" aria-label={$t('team.close')} onclick={onClose}><Icon name="close" size={14}/></button>
    </div>
    <div class="team-new-chat-search">
      <Icon name="search" size={16}/>
      <input bind:this={input} bind:value={query} type="text" placeholder={$t('team.searchBotsAndGroups')} aria-label={$t('team.searchBotsAndGroups')} autocomplete="off" spellcheck="false"/>
    </div>
    <div class="team-new-chat-list" use:scrollFade>
      <button type="button" class="team-new-chat-action" onclick={() => onNewBot('')}>
        <Icon name="user-plus" size={18}/><span>{$t('team.newBot')}</span>
      </button>
      <button type="button" class="team-new-chat-action" onclick={onNewGroup}>
        <Icon name="users" size={18}/><span>{$t('team.newGroup')}</span>
      </button>
      <div class="team-new-chat-divider" aria-hidden="true"></div>
      {#if !matchedBots.length && !matchedGroups.length}
        <p class="team-new-chat-empty">{needle ? $t('team.noMatchesFor', {name: query.trim()}) : $t('team.noBotsOrGroups')}</p>
      {:else}
        {#if matchedGroups.length}
          <h3>{$t('team.groups')}</h3>
          {#each matchedGroups as group (group.id)}
            <button type="button" class="team-new-chat-row" aria-label={$t('team.openGroupNamed', {name: group.name})} onclick={() => onOpenTeamGroup(group.id)}>
              <GroupAvatar members={membersOf(group)} size={39} label={$t('team.groupAvatarNamed', {name: group.name})}/>
              <span class="team-new-chat-copy">
                <span class="team-new-chat-top"><strong>{group.name}</strong><small>{groupStatus(group)}</small><time datetime={group.updatedAt}>{teamTime(group.updatedAt)}</time></span>
                <span class="team-new-chat-bottom"><span>{group.preview}</span></span>
              </span>
            </button>
          {/each}
        {/if}
        {#if matchedBots.length}
          <h3>{$t('team.bots')}</h3>
          {#each matchedBots as member (member.id)}
            <button type="button" class="team-new-chat-row" aria-label={$t('team.openMember', {name: member.name, role: member.role})} onclick={() => onOpenTeam(member.id)}>
              <BloubAvatar avatar={member.avatar} expression={bloubExpressionForTeamStatus(member.status, member.preview)} activity={bloubActivityForTeamStatus(member.status)} size={39} label={$t('team.memberAvatarNamed', {name: member.name})}/>
              <span class="team-new-chat-copy">
                <span class="team-new-chat-top"><strong>{member.name}</strong><TeamRoleLabel role={member.role}/><time datetime={member.updatedAt}>{teamTime(member.updatedAt)}</time></span>
                <span class="team-new-chat-bottom">
                  {#if teamStatus(member)}<em>{teamStatus(member)}</em><i class="team-new-chat-separator" aria-hidden="true"></i>{/if}
                  <span>{teamPreview(member)}</span>
                </span>
              </span>
            </button>
          {/each}
        {/if}
      {/if}
      {#if showCreateRow}
        <div class="team-new-chat-divider" aria-hidden="true"></div>
        <button type="button" class="team-new-chat-row team-new-chat-create" onclick={() => onNewBot(query.trim())}>
          <Icon name="user-plus" size={20}/><strong>{$t('team.newBotNamed', {name: query.trim()})}</strong><span>{$t('team.create')}</span>
        </button>
      {/if}
    </div>
  </div>
</div>

<style>
  .team-new-chat-backdrop{position:fixed;z-index:180;inset:0;display:grid;place-items:center;padding:28px;background:rgba(13,16,24,.34);backdrop-filter:blur(3px)}
  .team-new-chat{width:min(432px,calc(100vw - 40px));max-height:min(552px,calc(100vh - 44px));display:flex;flex-direction:column;overflow:hidden;border:1px solid var(--neutral-200);border-radius:16px;background:var(--app-bg);color:var(--neutral-900);box-shadow:0 24px 80px rgba(0,0,0,.3)}
  .team-new-chat-head{display:flex;align-items:center;justify-content:space-between;padding:13px 13px 9px 17px}
  .team-new-chat-head h2{margin:0;color:var(--neutral-950);font-size:14px;font-weight:600;letter-spacing:-.01em}
  .team-new-chat-head button{width:26px;height:26px;display:grid;place-items:center;border:0;border-radius:8px;background:transparent;color:var(--neutral-500);cursor:pointer}
  .team-new-chat-head button:hover{color:var(--neutral-800)}
  .team-new-chat-search{display:flex;align-items:center;gap:9px;padding:9px 16px 11px;border-bottom:1px solid var(--neutral-200);color:var(--neutral-500)}
  .team-new-chat-search input{min-width:0;flex:1;border:0;padding:0;background:transparent;color:var(--neutral-950);font:inherit;font-size:13.5px;outline:0}
  .team-new-chat-search input::placeholder{color:var(--neutral-400)}
  .team-new-chat-list{min-height:0;flex:1;overflow-y:auto;padding:7px 8px 10px;scrollbar-width:none}
  .team-new-chat-list::-webkit-scrollbar{display:none}
  .team-new-chat-action{width:100%;display:flex;align-items:center;gap:10px;min-height:38px;border:0;border-radius:10px;padding:7px 10px;background:transparent;color:var(--neutral-900);font:inherit;font-size:12.5px;font-weight:570;cursor:pointer;text-align:left}
  .team-new-chat-action:hover{background:var(--neutral-100)}
  .team-new-chat-divider{height:1px;margin:6px 10px;background:var(--neutral-200)}
  .team-new-chat-list h3{margin:9px 10px 3px;color:var(--neutral-500);font-size:10.5px;font-weight:620;letter-spacing:.04em;text-transform:uppercase}
  .team-new-chat-row{width:100%;display:flex;align-items:center;gap:10px;border:0;border-radius:10px;padding:6px 10px;background:transparent;color:var(--neutral-900);font:inherit;cursor:pointer;text-align:left}
  .team-new-chat-row:hover{background:var(--neutral-100)}
  .team-new-chat-copy{min-width:0;flex:1}
  .team-new-chat-top{display:flex;align-items:baseline;gap:6px;line-height:17px}
  .team-new-chat-top strong{overflow:hidden;min-width:0;flex:0 1 auto;max-width:170px;text-overflow:ellipsis;white-space:nowrap;color:var(--neutral-950);font-size:12.5px;font-weight:590}
  .team-new-chat-top small{overflow:hidden;min-width:0;flex:1;text-overflow:ellipsis;white-space:nowrap;color:var(--secondary);font-size:11px;font-weight:450}
  .team-new-chat-top time{margin-left:auto;flex:none;color:var(--neutral-500);font-size:10.5px}
  .team-new-chat-bottom{display:flex;align-items:center;gap:6px;margin-top:2px}
  .team-new-chat-bottom em{min-width:0;max-width:60%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;flex:0 1 auto;font-style:normal;color:var(--ok,#3ecf8e);font-size:11px}
  .team-new-chat-bottom span{overflow:hidden;min-width:0;flex:1;text-overflow:ellipsis;white-space:nowrap;color:var(--neutral-500);font-size:11px}
  .team-new-chat-separator{width:3px;height:3px;flex:none;border-radius:50%;background:var(--neutral-400)}
  .team-new-chat-row.team-new-chat-create{align-items:center;color:var(--neutral-600)}
  .team-new-chat-row.team-new-chat-create strong{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:570;color:var(--neutral-900)}
  .team-new-chat-row.team-new-chat-create span{flex:none;color:var(--neutral-500);font-size:11px}
  .team-new-chat-empty{margin:0;padding:26px 16px 10px;text-align:center;color:var(--neutral-400);font-size:12px}
  .team-new-chat button:focus-visible{outline:2px solid var(--focus-ring);outline-offset:-2px}
  @media(max-width:560px){.team-new-chat-backdrop{padding:12px}.team-new-chat{width:100%}}
</style>
