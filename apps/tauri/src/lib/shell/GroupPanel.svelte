<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import type { Agent, Thread } from '../api/contract';
  import type { Session } from '../session.svelte';
  import AgentAvatar from '../avatar/AgentAvatar.svelte';
  import AvatarStack from '../avatar/AvatarStack.svelte';
  import ProfilePanel from './ProfilePanel.svelte';
  import { groupEdit } from './groupEdit';

  let { session, thread, onClose, onSearch, onAvatar }: { session: Session; thread: Thread; onClose: () => void; onSearch?: () => void; onAvatar?: (agent: Agent) => void } = $props();
  const editor = untrack(() => groupEdit(session, thread));
  let name = $state(untrack(() => thread.title));
  let selected = $state<string[]>(untrack(() => [...thread.agentIDs]));
  let issue = $state('');
  let picking = $state(false);
  let profiling = $state<string>();
  let query = $state('');
  const members = $derived(selected.map((id) => session.agent(id)).filter((entry): entry is Agent => !!entry));
  const available = $derived(session.agents.filter((agent) => !selected.includes(agent.id) && (!query.trim() || `${agent.name} ${agent.role}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()))));
  const validMembers = $derived(selected.length >= 1 && selected.length <= 16 && new Set(selected).size === selected.length && selected.every((id) => !!session.agent(id)));
  const cleanName = $derived(name.trim());
  const validName = $derived(!!cleanName && new TextEncoder().encode(cleanName).length <= 128);
  const hint = $derived(!cleanName ? 'Enter a chat name.' : !validName ? 'Choose a shorter chat name.' : !validMembers ? 'Remove unavailable agents before saving.' : selected.length === 16 ? 'Remove an agent to add another.' : issue);
  const memberThread = $derived.by(() => {
    if (!profiling) return undefined;
    const found = session.threads.find((item) => item.id === profiling);
    const agent = session.agent(profiling);
    return found ?? (agent ? { id: agent.id, agentID: agent.id, agentIDs: [agent.id], title: agent.name, runs: [], lastReadSeq: 0, lastSeq: 0, unreadCount: 0 } : undefined);
  });

  onMount(() => editor.connect((value, message) => { if (document.activeElement?.getAttribute('aria-label') !== 'Chat name') name = value.title; selected = [...value.agentIDs]; issue = message; }));

  function save(delay: number) {
    if (!validMembers) return;
    const title = validName ? cleanName : thread.title;
    if (title !== thread.title || JSON.stringify(selected) !== JSON.stringify(thread.agentIDs)) editor.change({ title, agentIDs: selected }, delay);
  }
  function nameInput(value: string) { name = value; save(550); }
  function choose(id: string) { if (selected.length < 16 && !selected.includes(id)) { selected = [...selected, id]; query = ''; save(0); } }
  function remove(id: string) { if (selected.length > 1) { selected = selected.filter((item) => item !== id); save(0); } }
  function memberName(id: string): string {
    const agent = session.agent(id);
    if (!agent) return `Unavailable agent · ${id.slice(0, 6)}`;
    return session.agents.filter((item) => item.name === agent.name).length > 1 ? `${agent.name} · ${id.slice(0, 6)}` : agent.name;
  }
</script>

{#if profiling && memberThread}
  <ProfilePanel {session} thread={memberThread} {onClose} {onAvatar} onBack={() => (profiling = undefined)} />
{:else}
<div class="panel">
  <div class="bar">
    {#if picking}<button class="tool glass back" aria-label="Back" onclick={() => { picking = false; query = ''; }}><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="m15 5-7 7 7 7" /></svg></button>{/if}
    {#if !picking && onSearch}<button class="tool glass" aria-label="Search this chat" onclick={onSearch}><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.9"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m15.5 15.5 4 4" /></svg></button>{/if}
    <button class="tool glass" aria-label="Close side panel" onclick={onClose}><svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6 6 18M6 6l12 12" /></svg></button>
  </div>
  {#if picking}
    <div class="picker">
      <div class="search"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m15.5 15.5 4 4" /></svg><input aria-label="Search agents to add" placeholder="Search agents" bind:value={query} {@attach (element) => { element.focus(); }} onkeydown={(event) => { if (event.key === 'Enter' && query.trim() && available[0]) choose(available[0].id); }} /></div>
      <div class="choices">
        {#if selected.length === 16}<p>A chat can have up to 16 agents.</p>{/if}
        {#each available as agent (agent.id)}
          <button class="choice" aria-label="Add {memberName(agent.id)} to chat" disabled={selected.length >= 16} onclick={() => choose(agent.id)}><AgentAvatar {agent} size={52} /><span>{memberName(agent.id)}{#if agent.role}<small>{agent.role}</small>{/if}</span></button>
        {:else}<p>{query.trim() ? 'No matching agents.' : 'All available agents are already in this chat.'}</p>{/each}
      </div>
    </div>
  {:else}
    <div class="details">
      <div class="identity"><AvatarStack agents={members} size={100} height={100} /><input aria-label="Chat name" placeholder="Chat name" value={name} oninput={(event) => nameInput(event.currentTarget.value)} />{#if hint}<span class="hint" role="status">{hint}</span>{/if}</div>
      <section><h2>Members</h2><div class="grid">
        {#each selected as id (id)}
          {@const agent = session.agent(id)}
          <div class="tile"><div class="tile-avatar"><button aria-label="Edit {memberName(id)}’s profile" disabled={!agent} onclick={() => (profiling = id)}>{#if agent}<AgentAvatar {agent} size={72} />{/if}</button><button class="remove glass" aria-label="Remove {memberName(id)} from chat" disabled={selected.length === 1} onclick={() => remove(id)}><svg viewBox="0 0 24 24" width="8" height="8" fill="none" stroke="currentColor" stroke-width="3"><path d="M18 6 6 18M6 6l12 12" /></svg></button></div><span>{memberName(id)}</span></div>
        {/each}
        <div class="tile"><button class="add" aria-label="Add agents to chat" disabled={selected.length >= 16} onclick={() => { query = ''; picking = true; }}><span class="glass">+</span></button><span>Add</span></div>
      </div></section>
    </div>
  {/if}
</div>
{/if}

<style>
  .panel{height:100%;display:flex;flex-direction:column;background:var(--side-surface)}.bar{height:var(--titlebar-height);flex:none;display:flex;align-items:center;justify-content:flex-end;gap:8px;padding:0 10px}.tool{width:36px;height:36px;border-radius:50%;display:grid;place-items:center}.back{margin-right:auto}.details{flex:1;overflow:auto;padding:10px 24px 24px;display:flex;flex-direction:column;gap:24px}.identity{display:flex;flex-direction:column;align-items:center;gap:8px}.identity input{box-sizing:border-box;width:100%;border:0;background:transparent;text-align:center;font-size:20px;font-weight:600;outline-color:var(--text-secondary)}.hint{font-size:12px;color:var(--text-muted);text-align:center}h2{font-size:13px;margin:0;font-weight:600}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(88px,120px));justify-content:center;gap:16px;padding-top:8px}.tile{min-width:0;text-align:center;font-size:12px;display:flex;flex-direction:column;align-items:center;gap:4px}.tile>span{height:32px;overflow:hidden;line-height:16px}.tile-avatar{width:72px;height:72px;position:relative}.tile-avatar>button:first-child{width:72px;height:72px}.remove{position:absolute;right:-8px;top:0;width:20px;height:20px;border-radius:50%;display:grid;place-items:center;opacity:0}.tile-avatar:hover .remove,.remove:focus-visible{opacity:1}.add{width:72px;height:72px;display:grid;place-items:center}.add span{width:46px;height:46px;border-radius:50%;display:grid;place-items:center;font-size:22px}.add:disabled,.remove:disabled,.choice:disabled{opacity:.4}.picker{min-height:0;display:flex;flex-direction:column;gap:8px;padding:8px 16px}.search{min-height:44px;border-radius:22px;background:var(--glass-field);display:flex;align-items:center;gap:8px;padding:0 14px;color:var(--text-muted)}.search input{flex:1;min-width:0;border:0;outline:0;background:transparent}.choices{overflow:auto}.choice{display:flex;align-items:center;gap:16px;width:100%;text-align:left;padding:4px 8px}.choice>span{display:flex;flex-direction:column;gap:3px;min-width:0}.choice small,.choices p{color:var(--text-muted)}.choices p{text-align:center}
</style>
