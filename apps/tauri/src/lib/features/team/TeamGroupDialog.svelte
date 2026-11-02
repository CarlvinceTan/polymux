<script lang="ts">
  import type {CreateTeamGroupRequest, TeamGroupDto, BotDto} from '@polymux/protocol';
  import {onMount, tick} from 'svelte';
  import {fade} from 'svelte/transition';
  import {scrollFade} from '../../shared/scrollFade';
  import Icon from '../../shared/components/Icon.svelte';
  import {activateModalDialog, trapModalFocus} from '../../shared/dialogFocus';
  import BloubAvatar from './BloubAvatar.svelte';
  import GroupAvatar from './GroupAvatar.svelte';

  export let group: TeamGroupDto | null = null;
  export let members: BotDto[] = [];
  export let busy = false;
  export let error = '';
  export let onSave: (request: CreateTeamGroupRequest) => void = () => {};
  export let onDelete: (() => void) | null = null;
  export let onClose: () => void = () => {};

  let dialog: HTMLDivElement;
  let nameInput: HTMLInputElement;
  let name = group?.name ?? '';
  let memberQuery = '';
  let memberIds = group?.memberIds ?? members.slice(0, Math.min(3, members.length)).map((member) => member.id);
  const dialogFade = {duration: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 140};

  $: selected = members.filter((member) => memberIds.includes(member.id));
  $: memberNeedle = memberQuery.trim().toLowerCase();
  $: visibleMembers = memberNeedle
    ? members.filter((member) => `${member.name} ${member.role}`.toLowerCase().includes(memberNeedle))
    : members;
  $: valid = Boolean(name.trim()) && memberIds.length >= 2;

  onMount(() => {
    const restoreFocus = activateModalDialog(dialog);
    void tick().then(() => {
      nameInput?.focus();
      nameInput?.select();
    });

    return restoreFocus;
  });

  function toggleMember(id: string): void {
    memberIds = memberIds.includes(id) ? memberIds.filter((memberId) => memberId !== id) : [...memberIds, id];
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
      return;
    }
    trapModalFocus(event, dialog);
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey) && valid && !busy)
      onSave({name: name.trim(), memberIds});
  }
</script>

<div class="team-group-dialog-backdrop" role="presentation" transition:fade={dialogFade} onclick={(event) => event.target === event.currentTarget && onClose()}>
  <div bind:this={dialog} class="team-group-dialog" role="dialog" aria-modal="true" aria-labelledby="team-group-dialog-title" tabindex="-1" onkeydown={keydown}>
    <header>
      <div class="team-group-dialog-mark"><GroupAvatar members={selected} size={38} animated={false}/></div>
      <div><h2 id="team-group-dialog-title">{group ? `Edit ${group.name}` : 'New group'}</h2><p>Each member receives every group message.</p></div>
      <button type="button" aria-label="Close" onclick={onClose}><Icon name="close" size={17}/></button>
    </header>

    <div class="team-group-dialog-body" use:scrollFade>
      <label class="team-group-name"><span>Group name</span><input bind:this={nameInput} bind:value={name} autocomplete="off" placeholder="Launch room"/></label>
      <fieldset>
        <legend>Members <small>{memberIds.length} selected</small></legend>
        <div class="team-group-search"><Icon name="search" size={13}/><input bind:value={memberQuery} type="text" placeholder="Search members" aria-label="Search members" autocomplete="off" spellcheck="false"/></div>
        <div class="team-group-members">
          {#each visibleMembers as member (member.id)}
            <label class:selected={memberIds.includes(member.id)}>
              <BloubAvatar avatar={member.avatar} expression="neutral" size={30} animated={false} paper="var(--app-surface)"/>
              <span><strong>{member.name}</strong><small>{member.role}</small></span>
              <input type="checkbox" checked={memberIds.includes(member.id)} aria-label={`Include ${member.name}`} onchange={() => toggleMember(member.id)}/>
            </label>
          {:else}
            <p class="team-group-guidance" role="status">{memberQuery.trim() ? 'No matching members' : 'No bots yet'}</p>
          {/each}
        </div>
      </fieldset>
      {#if memberIds.length < 2}<p class="team-group-guidance">Choose at least two bots.</p>{/if}
      {#if error}<p class="team-group-error" role="alert">{error}</p>{/if}
    </div>

    <footer>
      {#if group && onDelete}<button type="button" class="team-group-delete" disabled={busy} onclick={() => onDelete?.()}>Delete group</button>{/if}
      <span></span>
      <button type="button" class="team-group-cancel" disabled={busy} onclick={onClose}>Cancel</button>
      <button type="button" class="team-group-save" disabled={!valid || busy} onclick={() => onSave({name: name.trim(), memberIds})}>{busy ? 'Saving…' : group ? 'Save' : 'Create group'}</button>
    </footer>
  </div>
</div>

<style>
  .team-group-dialog-backdrop{position:fixed;z-index:180;inset:0;display:grid;place-items:center;padding:28px;background:rgba(13,16,24,.34);backdrop-filter:blur(3px)}.team-group-dialog{width:min(480px,calc(100vw - 40px));max-height:min(690px,calc(100vh - 44px));display:flex;flex-direction:column;overflow:hidden;border:1px solid var(--neutral-200);border-radius:18px;outline:none;background:var(--app-surface);color:var(--neutral-900);box-shadow:0 22px 70px rgba(0,0,0,.24)}
  header{display:flex;align-items:center;gap:11px;padding:18px 20px 14px;border-bottom:1px solid var(--neutral-150,var(--neutral-200))}.team-group-dialog-mark{width:40px;height:40px;display:grid;place-items:center;flex:none}header>div:nth-child(2){min-width:0;flex:1}h2{margin:0;color:var(--neutral-950);font-size:16px;font-weight:600;letter-spacing:-.015em}header p{margin:3px 0 0;color:var(--secondary);font-size:11.5px;line-height:1.4}header>button{width:28px;height:28px;display:grid;place-items:center;border:0;padding:0;background:transparent;color:var(--neutral-700);cursor:pointer}header>button:hover{color:var(--neutral-900)}
  .team-group-dialog-body{min-height:0;overflow-y:auto;padding:18px 20px;scrollbar-width:none}.team-group-dialog-body::-webkit-scrollbar{display:none}.team-group-name>span,legend{display:block;margin-bottom:6px;color:var(--secondary);font-size:11px;font-weight:600}.team-group-name input{width:100%;height:36px;box-sizing:border-box;border:1px solid var(--neutral-250,var(--neutral-300));border-radius:9px;padding:0 10px;outline:none;background:var(--app-bg);color:var(--neutral-900);font:inherit;font-size:12px}.team-group-name input:focus-visible{border-color:var(--focus-ring);box-shadow:0 0 0 1px var(--focus-ring)}
  fieldset{min-width:0;margin:17px 0 0;border:0;padding:0}legend{width:100%}legend small{float:right;color:var(--secondary);font-size:11px;font-weight:500}.team-group-search{display:flex;align-items:center;gap:8px;height:30px;margin-bottom:8px;border:1px solid var(--neutral-250,var(--neutral-300));border-radius:8px;padding:0 9px;background:var(--app-bg);color:var(--neutral-500)}.team-group-search input{min-width:0;flex:1;border:0;padding:0;background:transparent;color:var(--neutral-900);font:inherit;font-size:12px;outline:0}.team-group-search input::placeholder{color:var(--neutral-400)}.team-group-members{display:flex;flex-direction:column;gap:2px}.team-group-members>label{min-height:45px;display:flex;align-items:center;gap:9px;border-radius:10px;padding:4px 8px;background:transparent;cursor:pointer}.team-group-members>label:hover,.team-group-members>label.selected{background:var(--neutral-100)}.team-group-members span{min-width:0;display:block;flex:1}.team-group-members strong,.team-group-members small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.team-group-members strong{color:var(--neutral-850,var(--neutral-900));font-size:12px;font-weight:600}.team-group-members small{margin-top:1px;color:var(--secondary);font-size:11px}.team-group-members input{width:16px;height:16px;accent-color:var(--neutral-900)}
  .team-group-guidance,.team-group-error{margin:10px 0 0;font-size:11px}.team-group-guidance{color:var(--secondary)}.team-group-error{color:var(--danger-600,#b42318)}footer{display:flex;align-items:center;gap:8px;padding:13px 20px 16px;border-top:1px solid var(--neutral-150,var(--neutral-200))}footer span{flex:1}footer button{height:34px;border:0;border-radius:9px;padding:0 13px;font:inherit;font-size:11.5px;font-weight:550;cursor:pointer}.team-group-cancel{background:var(--neutral-100);color:var(--neutral-700)}.team-group-save{background:var(--neutral-900);color:var(--app-bg)}.team-group-delete{background:transparent;color:var(--danger-600,#b42318)}footer button:disabled{cursor:not-allowed;opacity:.45}
  h2{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}header>button,.team-group-members input{flex:none}
  .team-group-search:focus-within{border-color:var(--focus-ring)}
  @media(max-width:560px){.team-group-dialog-backdrop{padding:12px}.team-group-dialog{width:100%;max-height:calc(100vh - 24px)}}
</style>
