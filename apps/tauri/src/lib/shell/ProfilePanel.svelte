<script lang="ts">
  import { untrack } from 'svelte';
  import type { Session } from '../session.svelte';
  import type { Agent, MCPConnection, MCPStatus, Thread } from '../api/contract';
  import AvatarStack from '../avatar/AvatarStack.svelte';
  import AgentModel from './AgentModel.svelte';
  import GroupPanel from './GroupPanel.svelte';
  import ProfileWork from './ProfileWork.svelte';
  import ProfileMedia from './ProfileMedia.svelte';
  import { profileEdit, type ProfileEdit } from './profileEdit';

  /** The chat's side panel: profile, work, media, files and capabilities. */
  let {
    session,
    thread,
    onClose,
    onBack,
    onSearch,
    onLocate,
    onAvatar,
  }: {
    session: Session;
    thread: Thread;
    onClose: () => void;
    onBack?: () => void;
    onSearch?: () => void;
    onLocate?: (messageID: string, partIndex: number) => void;
    onAvatar?: (agent: Agent) => void;
  } = $props();

  const agents = $derived(session.agentsFor(thread));
  const agent = $derived(agents[0]);
  const threadID = $derived(thread.id);
  const isGroup = $derived(thread.id !== thread.agentID || thread.agentIDs.length > 1);
  const profileID = $derived(isGroup ? undefined : agent?.id);
  const agentID = $derived(isGroup ? undefined : agent?.id);
  const tabs = $derived(onBack ? ['Work', 'Capabilities'] : ['Work', 'Media', 'Files', 'Capabilities']);
  let tab = $state(untrack(() => isGroup ? 'Capabilities' : 'Work'));

  $effect(() => {
    threadID;
    tab = isGroup ? 'Capabilities' : 'Work';
  });

  let prompt = $state('');
  let name = $state('');
  let role = $state('');
  let promptLoaded = $state(false);
  let promptIssue = $state('');
  let editor: ProfileEdit | undefined;
  let skills = $state<string[]>([]);
  let tools = $state<string[]>([]);
  let connections = $state<MCPConnection[]>([]);
  let statuses = $state<Record<string, MCPStatus>>({});
  let mcpLoading = $state(false);

  $effect(() => {
    const id = profileID;
    if (!id) { editor = undefined; promptLoaded = false; return; }
    editor = profileEdit(session.address, untrack(() => agent!), session.client);
    return editor.connect((values, loaded, issue, accepted) => {
      name = values.name;
      role = values.role;
      prompt = values.prompt;
      promptLoaded = loaded;
      promptIssue = issue;
      if (accepted) session.agents = session.agents.map((entry) => entry.id === accepted.id ? { ...accepted, avatar: entry.avatar } : entry);
    });
  });

  $effect(() => {
    const id = agentID;
    if (!id) return;
    let cancelled = false;
    skills = [];
    tools = [];
    void session.client
      .configuration(id)
      .then((configuration) => {
        if (cancelled) return;
        skills = configuration.skills ?? [];
        tools = configuration.tools ?? [];
      })
      .catch(() => undefined);
    return () => { cancelled = true; };
  });

  $effect(() => {
    const id = agentID;
    if (!id || tab !== 'Capabilities') return;
    let cancelled = false;
    mcpLoading = true;
    connections = [];
    statuses = {};
    void Promise.allSettled([session.client.connections(id), session.client.mcpStatus(id)])
      .then(([list, status]) => {
        if (cancelled) return;
        if (list.status === 'fulfilled') connections = list.value.filter((connection) => connection.enabled && !connection.hidden);
        if (status.status === 'fulfilled') statuses = Object.fromEntries(status.value.map((entry) => [entry.name, entry]));
      })
      .finally(() => { if (!cancelled) mcpLoading = false; });
    return () => { cancelled = true; };
  });

  const mcpTools = $derived(connections.reduce((total, connection) => total + (statuses[connection.name]?.tools.length ?? 0), 0));
  const toolCount = $derived(tools.length + mcpTools);

  function skillName(path: string): string {
    const parts = path.replace(/\/$/, '').split('/');
    const last = parts.at(-1) ?? path;
    const name = last === 'SKILL.md' ? parts.at(-2) ?? last : last.replace(/\.[^.]+$/, '');
    return name.split(/[-_\s]+/).filter(Boolean).map((word) => word.toLowerCase() === 'html' ? 'HTML' : word.charAt(0).toUpperCase() + word.slice(1)).join(' ');
  }

  const messages = $derived(session.messages[thread.id] ?? []);
  const media = $derived(messages.flatMap((message) => message.parts.flatMap((part, partIndex) =>
    part.attachment ? [{ attachment: part.attachment, messageID: message.id, partIndex }] : [],
  )));
</script>

{#if isGroup}
  {#key thread.id}<GroupPanel {session} {thread} {onClose} {onSearch} {onAvatar} />{/key}
{:else}
<div class="panel">
  <div class="bar">
    {#if onBack}<button class="tool glass back" aria-label="Back" onclick={onBack}><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="m15 5-7 7 7 7"></path></svg></button>{/if}
    <button class="tool glass" aria-label={agent && session.notifications.isMuted(agent.id) ? 'Unmute notifications' : 'Mute notifications'} aria-pressed={agent ? session.notifications.isMuted(agent.id) : false} disabled={!agent} onclick={() => { if (agent) session.notifications.setMuted(agent.id, !session.notifications.isMuted(agent.id)); }}>
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.6">
        {#if agent && session.notifications.isMuted(agent.id)}<path d="M18 8a6 6 0 0 0-10.5-3.8M6 8c0 7-3 7-3 9h12M10 21h4M4 4l16 16"></path>{:else}<path d="M18 8a6 6 0 1 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"></path>{/if}
      </svg>
    </button>
    {#if onSearch}<button class="tool glass" aria-label="Search this chat" onclick={onSearch}>
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.9">
        <circle cx="10.5" cy="10.5" r="6.5"></circle>
        <path d="m15.5 15.5 4 4"></path>
      </svg>
    </button>{/if}
    <button class="tool glass" aria-label="Close side panel" onclick={onClose}>
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M18 6 6 18M6 6l12 12"></path>
      </svg>
    </button>
  </div>

  <div class="body">
    <div class="head">
      <button class="portrait" aria-label="Edit avatar" onclick={() => { if (agent) onAvatar?.(agent); }}>
        <AvatarStack {agents} size={100} />
      </button>
      <label class="inline name">
        <span aria-hidden="true">{name || 'Name'}</span>
        <input aria-label="Name" placeholder="Name" value={name} autocomplete="off" onfocus={(event) => event.currentTarget.select()}
          onkeydown={(event) => { if (event.key === 'Enter') event.currentTarget.blur(); }}
          oninput={(event) => editor?.edit('name', event.currentTarget.value)} />
      </label>
      <label class="inline role">
        <span aria-hidden="true">{role || 'Role'}</span>
        <input aria-label="Role" placeholder="Role" value={role} onfocus={(event) => event.currentTarget.select()}
          onkeydown={(event) => { if (event.key === 'Enter') event.currentTarget.blur(); }}
          oninput={(event) => editor?.edit('role', event.currentTarget.value)} />
      </label>
      {#if promptIssue}<span class="none" role="status">{promptIssue}</span>{/if}
      <div class="tabs" role="tablist">
        {#each tabs as name (name)}
          <button role="tab" class="tab" class:on={tab === name} aria-selected={tab === name} onclick={() => (tab = name)}>
            {name}
          </button>
        {/each}
      </div>
    </div>

    <div class="content">
      {#if agent}{#key agent.id}<ProfileWork {session} agentID={agent.id} {thread} visible={tab === 'Work'} />{/key}{/if}
      {#if tab === 'Media' || tab === 'Files'}
        <ProfileMedia {session} items={media} {tab} onLocate={(messageID, partIndex) => onLocate?.(messageID, partIndex)} />
      {:else if tab === 'Capabilities'}
        {#if agent}<AgentModel {session} {agent} />{/if}
        <section>
          <div class="heading"><span>Instructions</span></div>
          <div class="card">
            <textarea placeholder={promptLoaded ? 'Add instructions' : 'Loading'} aria-label="Instructions"
              value={prompt} disabled={!promptLoaded || isGroup} oninput={(event) => editor?.edit('prompt', event.currentTarget.value)}></textarea>
          </div>
        </section>
        <section>
          <div class="heading"><span>Skills</span><span class="count">{skills.length}</span></div>
          <div class="card">
            {#if skills.length}
              {#each skills as skill (skill)}<div class="row"><span>✦</span><span>{skillName(skill)}</span></div>{/each}
            {:else}
              <span class="none">No skills</span>
            {/if}
          </div>
        </section>
        <section>
          <div class="heading"><span>Tools</span><span class="count">{mcpLoading ? '…' : toolCount}</span></div>
          <div class="card">
            {#each connections as connection (connection.name)}
              <details class="connection">
                <summary>
                  <span class="dot" class:on={statuses[connection.name]?.connected} class:checking={mcpLoading} aria-hidden="true"></span>
                  <span class="connection-name">{connection.name}</span>
                  <span class="count">{statuses[connection.name] ? statuses[connection.name].tools.length : '…'}</span>
                </summary>
                {#if statuses[connection.name]}
                  {#if statuses[connection.name].tools.length}
                    {#each statuses[connection.name].tools as tool (tool)}<div class="row tool"><span>⚒</span><span>{tool}</span></div>{/each}
                  {:else}
                    <span class="none left">{statuses[connection.name].connected ? 'No tools' : 'Unavailable'}</span>
                  {/if}
                {:else}
                  <span class="none left">Checking</span>
                {/if}
              </details>
            {/each}
            {#if tools.length}
              {#each tools as tool (tool)}<div class="row"><span>⚒</span><span>{tool}</span></div>{/each}
            {:else if !connections.length}
              <span class="none">Standard</span>
            {/if}
          </div>
        </section>
      {/if}
    </div>
  </div>
</div>
{/if}

<style>
  .panel {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--side-surface);
  }
  .bar {
    height: var(--titlebar-height);
    flex: none;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    padding: 0 10px;
  }
  .tool {
    width: 36px;
    height: 36px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: var(--text);
  }
  .tool:hover {
    filter: brightness(0.97);
  }
  .bar > .tool:first-child { margin-right: auto; }
  .back { margin-right: auto; }
  .body {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    scrollbar-width: none;
    padding: 4px 16px 22px;
    display: flex;
    flex-direction: column;
    gap: 22px;
  }
  .body::-webkit-scrollbar {
    display: none;
  }
  .head {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
  }
  .portrait {
    width: 100px;
    height: 100px;
    display: grid;
    place-items: center;
  }
  .name {
    font-size: 17px;
    font-weight: 600;
  }
  .role {
    font-size: 13px;
    color: var(--text-muted);
    text-align: center;
  }
  .inline {
    display: grid;
    max-width: 100%;
    min-width: 55px;
    border-radius: 8px;
    padding: 4px 10px;
  }
  .inline:hover:not(:focus-within) { background: var(--hover-bg, rgba(128, 128, 128, 0.12)); }
  .inline:has(input:focus-visible) { outline: 2px solid var(--focus-ring); outline-offset: 1px; }
  .inline span, .inline input { grid-area: 1 / 1; }
  .inline span { visibility: hidden; overflow: hidden; white-space: nowrap; }
  .inline input {
    min-width: 0;
    width: 100%;
    border: 0;
    outline: 0;
    padding: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: center;
    text-overflow: ellipsis;
  }
  .tabs {
    display: flex;
    gap: 8px;
    padding-top: 8px;
    flex-wrap: wrap;
    justify-content: center;
  }
  .tab {
    font-size: 11px;
    padding: 12px 14px;
    border-radius: 999px;
    color: var(--text);
    line-height: 1;
  }
  .tab.on {
    background: var(--window-bg);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
  }
  .tab:not(.on):hover {
    background: rgba(255, 255, 255, 0.5);
  }
  .content {
    display: flex;
    flex-direction: column;
    gap: 22px;
    flex: 1;
    min-height: 0;
  }
  .heading {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    font-size: 13px;
    font-weight: 600;
    padding: 0 4px 10px;
  }
  .count {
    font-size: 11px;
    font-weight: 400;
    color: var(--text-muted);
  }
  .card {
    background: var(--window-bg);
    border-radius: 16px;
    padding: 6px 8px;
    min-height: 44px;
    display: flex;
    flex-direction: column;
    justify-content: center;
  }
  .none {
    color: var(--text-muted);
    font-size: 13px;
    text-align: center;
  }
  .card .none {
    text-align: left;
    padding: 6px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 32px;
    padding: 0 6px;
    font-size: 13px;
  }
  .connection { border-bottom: 1px solid var(--hairline); }
  .connection:last-of-type { border-bottom: 0; }
  .connection summary { display: flex; align-items: center; gap: 8px; min-height: 32px; padding: 0 6px; font-size: 13px; cursor: default; list-style: none; }
  .connection summary::-webkit-details-marker { display: none; }
  .connection summary::after { content: '›'; margin-left: 2px; color: var(--text-muted); transition: transform 0.15s ease-out; }
  .connection[open] summary::after { transform: rotate(90deg); }
  .connection-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .dot { flex: none; width: 7px; height: 7px; border-radius: 50%; background: var(--text-secondary); }
  .dot.on { background: #34c759; }
  .dot.checking { background: var(--attention); }
  .tool { padding-left: 22px; }
  .none.left { text-align: left; padding: 4px 6px 8px 22px; }
  @media (prefers-reduced-motion: reduce) { .connection summary::after { transition: none; } }
  textarea {
    min-height: 90px;
    resize: none;
    border: none;
    outline: none;
    background: none;
    padding: 6px;
    font-size: 13px;
    user-select: text;
  }
  textarea::placeholder {
    color: var(--text-muted);
  }
</style>
