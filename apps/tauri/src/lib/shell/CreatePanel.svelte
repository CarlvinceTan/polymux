<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import type { Agent, AvatarDesign, AvatarDesignColor, AvatarDesignShape, Thread } from '../api/contract';
  import type { Session } from '../session.svelte';
  import AgentAvatar from '../avatar/AgentAvatar.svelte';
  import CreatePanel from './CreatePanel.svelte';
  import AvatarPicker from '../avatar/AvatarPicker.svelte';
  import { categories, colors } from '../avatar/choices';

  let { session, kind, sectionID, onClose, onCreated }: {
    session: Session; kind: 'bot' | 'group'; sectionID?: string; onClose: () => void; onCreated: (thread: Thread) => void;
  } = $props();

  const initialShape = categories.Shapes[Math.floor(Math.random() * categories.Shapes.length)][0];
  const initialColor = colors[Math.floor(Math.random() * colors.length)][0];
  const draftKey = untrack(() => `polymux.create.${session.address}.${kind}`);
  const saved = untrack(() => {
    try {
      const value = JSON.parse(localStorage.getItem(draftKey) ?? 'null') as { name?: string; shape?: AvatarDesignShape; color?: AvatarDesignColor; selected?: string[] } | null;
      return value ?? {};
    } catch { return {}; }
  });
  const shapes = new Set<AvatarDesignShape>(Object.values(categories).flat().map((item) => item[0]));
  const palette = new Set<AvatarDesignColor>(colors.map((item) => item[0]));
  let shape = $state<AvatarDesignShape>(saved.shape && shapes.has(saved.shape) ? saved.shape : initialShape);
  let color = $state<AvatarDesignColor>(saved.color && palette.has(saved.color) ? saved.color : initialColor);
  let name = $state(saved.name ?? '');
  let query = $state('');
  let selected = $state<string[]>((saved.selected ?? []).filter((id) => session.agents.some((agent) => agent.id === id)).slice(0, 16));
  let created = false;
  let creating = $state(false);
  let error = $state('');
  let adding = $state(false);
  let model = $state('Server default');
  const cleanName = $derived(name.trim());
  const validName = $derived(new TextEncoder().encode(cleanName).length <= 128);
  const validMembers = $derived(selected.length >= 2 && selected.length <= 16 && selected.every(id => session.agents.some(agent => agent.id === id)));
  const choices = $derived(session.agents.filter(agent => !selected.includes(agent.id) && `${agent.name} ${agent.role}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())));
  const design = $derived<AvatarDesign>({ shape, color, expression: 'neutre', followCursor: true, animates: true, playSequence: false, blocks: [] });
  const preview = $derived({ id: 'preview', name, avatar: design } as Agent);
  const message = $derived(!validName ? (kind === 'bot' ? 'Choose a shorter name.' : 'Choose a shorter group name.') : kind === 'group' && selected.length < 2 ? 'Choose at least two bots.' : '');

  onMount(() => {
    void session.client.settings().then(settings => { model = settings.defaultModel || 'Server default'; }).catch(() => {});
  });

  $effect(() => {
    if (created) return;
    try { localStorage.setItem(draftKey, JSON.stringify({ name, shape, color, selected })); } catch { /* Keep the draft in memory. */ }
  });

  async function create() {
    if (creating || !validName || (kind === 'group' && !validMembers)) return;
    creating = true;
    error = '';
    try {
      if (kind === 'bot') {
        const agent = await session.client.createAgent({ name: cleanName, role: '', prompt: '', avatar: design, sectionID });
        const thread: Thread = { id: agent.id, agentID: agent.id, agentIDs: [agent.id], title: agent.name, runs: [], lastReadSeq: 0, lastSeq: 0, unreadCount: 0, sectionID };
        session.agents = [...session.agents, agent];
        session.threads = [...session.threads, thread];
        created = true;
        try { localStorage.removeItem(draftKey); } catch { /* The created chat is already open. */ }
        onCreated(thread);
        void session.load().catch(() => {});
      } else {
        const thread = await session.client.createThread(selected, cleanName || undefined);
        session.threads = [...session.threads, thread];
        created = true;
        try { localStorage.removeItem(draftKey); } catch { /* The created chat is already open. */ }
        onCreated(thread);
      }
    } catch {
      error = kind === 'bot' ? "Couldn't create this bot." : "Couldn't create this chat.";
    } finally { creating = false; }
  }
  function choose(id: string) { if (selected.length < 16) { selected = [...selected, id]; query = ''; } }
</script>

{#if adding}
  <CreatePanel {session} kind="bot" onClose={() => adding = false} onCreated={() => adding = false} />
{:else}
  <div class="shade" role="presentation">
    <div class:bot={kind === 'bot'} class="panel" role="dialog" aria-modal="true" aria-label={kind === 'bot' ? 'New Bot' : 'New Group Chat'} tabindex="-1" onkeydown={(event) => { if (event.key === 'Escape' && !creating) onClose(); }}>
      <header><strong>{kind === 'bot' ? 'New Bot' : 'New Group Chat'}</strong><button class="close glass" aria-label={kind === 'bot' ? 'Close New Bot' : 'Close New Group Chat'} disabled={creating} onclick={onClose}>×</button></header>
      {#if kind === 'bot'}
        <div class="body bot-body">
          <div class="preview"><AgentAvatar agent={preview} size={80} /><input aria-label="Name" placeholder="Name (optional)" bind:value={name} disabled={creating} /></div>
          <AvatarPicker {design} agent={preview} onChange={(next) => { shape = next.shape; color = next.color; }} />
          <div class="model"><span>Model</span><span>{model}</span></div>
        </div>
      {:else}
        <div class="group-header"><div class="recipients"><span>To:</span>{#each selected as id (id)}<button onclick={() => selected = selected.filter(value => value !== id)} aria-label="Remove {session.agent(id)?.name}">{session.agent(id)?.name} ×</button>{/each}<input aria-label="To" placeholder={selected.length ? '' : 'Add bot'} bind:value={query} onkeydown={(event) => { if (event.key === 'Enter' && choices[0]) { event.preventDefault(); choose(choices[0].id); } else if (event.key === 'Backspace' && !query) selected = selected.slice(0, -1); }} /></div>{#if selected.length > 1}<input class="group-name" aria-label="Chat name (optional)" placeholder="Chat name (optional)" bind:value={name} />{/if}</div>
        <div class="body members">{#each choices as agent (agent.id)}<button class="member" aria-label="Select {agent.name}" disabled={selected.length >= 16} onclick={() => choose(agent.id)}><AgentAvatar {agent} size={52}/><span>{agent.name}{#if agent.role}<small>{agent.role}</small>{/if}</span></button>{:else}<div class="empty">{query.trim() ? 'No matching bots.' : session.agents.length ? 'No available bots.' : 'No bots yet.'}</div>{/each}</div>
      {/if}
      <footer>{#if kind === 'group' && session.agents.length < 2}<button class="action" onclick={() => adding = true} disabled={creating}>New Bot</button>{/if}<span>{error || message}</span><button class="action glass" disabled={creating || !validName || (kind === 'group' && !validMembers)} onclick={create}>{creating ? 'Creating' : 'Create'}</button></footer>
    </div>
  </div>
{/if}

<style>
  .shade{position:fixed;inset:0;z-index:150;display:grid;place-items:center;background:rgba(0,0,0,.25)}
  .panel{width:min(620px,calc(100vw - 48px));height:min(600px,calc(100vh - 48px));border-radius:24px;background:var(--window-bg);display:flex;flex-direction:column;overflow:hidden;box-shadow:0 12px 40px rgba(0,0,0,.24)}
  .panel.bot{width:min(460px,calc(100vw - 48px))}header{height:76px;flex:none;display:grid;place-items:center;position:relative}header strong{font-size:13px;font-weight:600}.close{position:absolute;right:16px;top:20px;width:36px;height:36px;border-radius:50%;font-size:19px}.body{overflow:auto;flex:1;min-height:0}.bot-body{padding:8px 24px;display:flex;flex-direction:column;gap:12px;align-items:center}.preview{display:flex;flex-direction:column;align-items:center;gap:12px;width:100%}.preview input{width:min(260px,100%);box-sizing:border-box;border:1px solid var(--hairline);border-radius:6px;background:var(--glass-field);padding:5px 8px}.model{display:flex;justify-content:space-between;align-self:stretch;padding-top:4px}.model span:last-child{color:var(--text-muted)}footer{display:flex;gap:10px;align-items:center;padding:16px;flex:none}footer span{flex:1;font-size:11px;color:var(--text-muted)}.action{min-height:32px;padding:0 14px;border-radius:16px;background:var(--glass-field)}.action:disabled{opacity:.45}.group-header{display:flex;flex-direction:column;gap:12px;padding:0 16px}.recipients,.group-name{box-sizing:border-box;min-height:44px;border-radius:22px;background:var(--glass-field);padding:8px 14px;border:0}.recipients{display:flex;flex-wrap:wrap;align-items:center;gap:8px}.recipients span{color:var(--text-muted)}.recipients button{border-radius:14px;background:var(--row-selected);padding:5px 10px}.recipients input{flex:1;min-width:80px;background:transparent;border:0;outline:0}.members{padding:8px 16px}.member{display:flex;align-items:center;gap:16px;width:100%;text-align:left;padding:4px 8px}.member>span{display:flex;flex-direction:column;gap:3px;overflow:hidden}.member small{color:var(--text-muted)}.empty{height:320px;display:grid;place-items:center;color:var(--text-muted)}
</style>
