<script lang="ts">
  import type { Agent } from '../api/contract';
  import AgentAvatar from '../avatar/AgentAvatar.svelte';
  import Icon from '../shared/components/Icon.svelte';
  import Menu from '../shared/components/Menu.svelte';
  import SearchField from '../shared/components/SearchField.svelte';
  import Tooltip from '../shared/components/Tooltip.svelte';
  import PlatformLogo from '../shared/components/PlatformLogo.svelte';
  import ProviderLogo from '../shared/components/ProviderLogo.svelte';
  import DriveProviderLogo from '../shared/components/DriveProviderLogo.svelte';
  import TaskGlyph from '../shared/components/TaskGlyph.svelte';
  import WarningBubble from '../shell/WarningBubble.svelte';
  import CursorMenu, { type MenuEntry } from './CursorMenu.svelte';

  let warning = $state(true);
  let choice = $state('None selected');
  let toggle = $state(true);
  let text = $state('A short message');
  let selected = $state('Work');
  let search = $state('');
  let workspace = $state('work');
  const workspaces = [
    { value: 'work', label: 'Work' },
    { value: 'research', label: 'Research' },
    { value: 'personal', label: 'Personal' },
  ];
  const sharedIcons = ['plus', 'search', 'settings', 'chat', 'send', 'mic', 'paper-airplane', 'folder', 'sparkles', 'pin'] as const;
  const taskShapes = ['burst', 'quad', 'star', 'corona', 'pinwheel', 'halo', 'spark'] as const;

  const agents: Agent[] = [
    { id: 'a', name: 'Aster', role: 'Researcher', model: '', active: true, avatar: { shape: 'cercle', color: 'encre', expression: 'neutre', followCursor: true, animates: true, playSequence: false, blocks: [] } },
    { id: 'b', name: 'Milo', role: 'Writer', model: '', active: true, avatar: { shape: 'cat', color: 'ambre', expression: 'neutre', followCursor: true, animates: true, playSequence: false, blocks: [] } },
    { id: 'c', name: 'Nova', role: 'Analyst', model: '', active: true, avatar: { shape: 'heart', color: 'rose', expression: 'neutre', followCursor: true, animates: true, playSequence: false, blocks: [] } },
    { id: 'd', name: 'Fern', role: 'Operator', model: '', active: true, avatar: { shape: 'star', color: 'vert', expression: 'neutre', followCursor: true, animates: true, playSequence: false, blocks: [] } },
  ];

  const menuItems: MenuEntry[] = [
    { id: 'new', label: 'New chat', shortcut: '⌘N' },
    { id: 'bot', label: 'New bot' },
    { id: 'move', label: 'Move to', submenu: [
      { id: 'research', label: 'Research' },
      { id: 'design', label: 'Design' },
      { id: 'personal', label: 'Personal' },
    ] },
    { id: 'pin', label: 'Pin chat', shortcut: '⇧⌘P' },
    { id: 'delete', label: 'Delete', danger: true },
  ];

  const nav = [
    ['foundation', 'Foundation'], ['actions', 'Actions'], ['inputs', 'Inputs'],
    ['shared', 'Shared'], ['menus', 'Menus'], ['avatars', 'Avatars'],
    ['messages', 'Messages'], ['cards', 'Cards'], ['feedback', 'Feedback'],
  ] as const;

  function pick(entry: MenuEntry) { choice = entry.label; }
</script>

<div class="gallery">
  <aside>
    <div class="brand"><span class="mark"></span><strong>Polymux</strong><small>Components</small></div>
    <nav aria-label="Gallery sections">
      {#each nav as [id, label] (id)}<a href="#{id}">{label}</a>{/each}
    </nav>
    <p class="note">Every surface uses the same tokens, spacing and motion language.</p>
  </aside>

  <main>
    <header class="intro">
      <p class="eyebrow">Design system</p>
      <h1>Quiet tools for working with agents.</h1>
      <p class="lede">A compact reference for the controls, conversation surfaces and feedback states used across Polymux.</p>
    </header>

    <section id="foundation">
      <div class="section-head"><span class="index">01</span><h2>Foundation</h2><p>Colour, type and shape are shared by every component.</p></div>
      <div class="split">
        <div class="panel">
          <h3>Surfaces</h3>
          <div class="swatches">
            <div class="swatch" style:background="var(--window-bg)"><span>Window</span></div>
            <div class="swatch" style:background="var(--sidebar-bg)"><span>Sidebar</span></div>
            <div class="swatch" style:background="var(--glass-field)"><span>Field</span></div>
            <div class="swatch" style:background="var(--text)"><span class="inverse">Ink</span></div>
          </div>
        </div>
        <div class="panel">
          <h3>Type</h3>
          <div class="type"><strong>Conversation</strong><span>Body copy stays at 13 px.</span><small>Metadata at 11 px</small><kbd>Shortcut</kbd></div>
        </div>
      </div>
    </section>

    <section id="actions">
      <div class="section-head"><span class="index">02</span><h2>Actions</h2><p>One primary action, quiet secondaries, and destructive work kept distinct.</p></div>
      <div class="panel row-wrap">
        <button class="primary">Send</button>
        <button class="secondary">Save draft</button>
        <button class="quiet"><Icon name="plus" size={14} /> New bot</button>
        <button class="icon" aria-label="Settings"><Icon name="settings" size={16} /></button>
        <button class="danger">Delete</button>
        <button class="primary" disabled>Disabled</button>
      </div>
    </section>

    <section id="inputs">
      <div class="section-head"><span class="index">03</span><h2>Inputs</h2><p>Fields are calm until they have focus; the composer mirrors the chat.</p></div>
      <div class="split">
        <div class="panel stack">
          <label class="field-label">Name<input bind:value={text} /></label>
          <label class="field-label">Search<div class="search"><Icon name="search" size={14} /><input placeholder="Search components" /></div></label>
          <label class="field-label">Notes<textarea rows="3" placeholder="Add context"></textarea></label>
        </div>
        <div class="panel stack">
          <div class="segmented" role="tablist">{#each ['Work', 'Media', 'Files'] as item (item)}<button role="tab" aria-selected={selected === item} class:on={selected === item} onclick={() => (selected = item)}>{item}</button>{/each}</div>
          <div class="switch-row"><span>Notifications</span><button class="toggle" class:on={toggle} aria-pressed={toggle} aria-label="Notifications" onclick={() => (toggle = !toggle)}></button></div>
          <div class="composer"><button class="icon" aria-label="Add attachment"><Icon name="plus" size={15} /></button><div class="composer-field">Message</div><button class="icon dark" aria-label="Send"><Icon name="arrow-up" size={15} /></button></div>
        </div>
      </div>
    </section>

    <section id="shared">
      <div class="section-head"><span class="index">04</span><h2>Shared</h2><p>The components carried over from the earlier Svelte app, kept on the same tokens and spacing.</p></div>
      <div class="split">
        <div class="panel stack">
          <h3>Search field</h3>
          <SearchField bind:value={search} placeholder="Search shared components" />
          <h3>Select</h3>
          <Menu label="Workspace" bind:value={workspace} options={workspaces} wide width="full" />
          <h3>Tooltip</h3>
          <button class="icon" aria-label="Tooltip demo"><Icon name="info" size={16} /></button>
        </div>
        <div class="panel stack">
          <h3>Icons</h3>
          <div class="icon-grid">
            {#each sharedIcons as name (name)}
              <span class="icon-cell" title={name}><Icon {name} size={18} /><small>{name}</small></span>
            {/each}
          </div>
          <h3>Brands and tasks</h3>
          <div class="brand-row">
            <ProviderLogo provider="openai" />
            <ProviderLogo provider="anthropic" />
            <ProviderLogo provider="google" />
            <PlatformLogo platform="slack" />
            <PlatformLogo platform="telegram" />
            <PlatformLogo platform="whatsapp" />
            <DriveProviderLogo provider="google-drive" />
            <DriveProviderLogo provider="dropbox" />
            <DriveProviderLogo provider="onedrive" />
            {#each taskShapes as shape (shape)}
              <TaskGlyph {shape} status="active" label={shape} />
            {/each}
          </div>
        </div>
      </div>
    </section>

    <section id="menus">
      <div class="section-head"><span class="index">05</span><h2>Menus</h2><p>Menus grow from their trigger. Moving diagonally into a submenu keeps it open through the safe triangle.</p></div>
      <div class="panel menu-demo">
        <CursorMenu items={menuItems} onSelect={pick} />
        <span class="result">Selected: {choice}</span>
      </div>
    </section>

    <section id="avatars">
      <div class="section-head"><span class="index">06</span><h2>Avatars</h2><p>Identity is the visual anchor. Shapes remain legible at row size.</p></div>
      <div class="panel avatar-row">
        {#each agents as agent (agent.id)}<div class="avatar"><AgentAvatar {agent} size={64} /><span>{agent.name}</span><small>{agent.role}</small></div>{/each}
      </div>
    </section>

    <section id="messages">
      <div class="section-head"><span class="index">07</span><h2>Messages</h2><p>Conversation surfaces keep the text in front and controls secondary.</p></div>
      <div class="panel transcript">
        <div class="message-row"><AgentAvatar agent={agents[0]} size={28} /><div class="bubble incoming">Checked the project and edited the transcript.</div></div>
        <div class="message-row outgoing"><div class="bubble outgoing">Can you verify the release before enabling it?</div></div>
        <div class="message-row"><AgentAvatar agent={agents[1]} size={28} /><div class="column"><div class="author">Milo</div><div class="bubble incoming">The release is verified. Two checks remain.<div class="reply"><span>You</span><span>Can you verify the release?</span></div></div><div class="reactions"><span>👍 2</span><span>✓ 1</span></div></div></div>
        <div class="typing"><AgentAvatar agent={agents[0]} size={24} thinking /><span>Thinking it through</span></div>
      </div>
    </section>

    <section id="cards">
      <div class="section-head"><span class="index">08</span><h2>Cards</h2><p>Rows and cards group related settings without adding another visual layer.</p></div>
      <div class="split">
        <div class="panel profile-card">
          <AgentAvatar agent={agents[2]} size={72} />
          <strong>Nova</strong><span>Analyst</span>
          <div class="tabs"><button class="on">Work</button><button>Media</button><button>Files</button><button>Capabilities</button></div>
        </div>
        <div class="panel stack">
          <div class="setting-row"><span>Theme</span><strong>System</strong></div>
          <div class="setting-row"><span>Default model</span><strong>Gpt 5.1</strong></div>
          <div class="setting-row"><span>Reasoning</span><strong>Medium</strong></div>
        </div>
      </div>
    </section>

    <section id="feedback">
      <div class="section-head"><span class="index">09</span><h2>Feedback</h2><p>Warnings say what happened and offer one clear next step. Empty states stay quiet.</p></div>
      <div class="panel stack">
        {#if warning}<WarningBubble text="Couldn’t save that change. It will retry." dismiss={() => (warning = false)} />{:else}<button class="secondary" onclick={() => (warning = true)}>Show warning</button>{/if}
        <div class="empty"><Icon name="chat" size={28} /><strong>No chats yet</strong><span>Start a conversation with a bot.</span><button class="primary">New bot</button></div>
        <div class="loading" role="status"><span class="spinner"></span> Loading conversation</div>
      </div>
    </section>
  </main>
  <Tooltip />
</div>

<style>
  .gallery { min-height: 100vh; display: grid; grid-template-columns: 212px minmax(0, 1fr); background: var(--window-bg); color: var(--text); }
  aside { position: sticky; top: 0; height: 100vh; box-sizing: border-box; padding: 26px 18px; background: var(--sidebar-bg); border-right: 1px solid var(--hairline); display: flex; flex-direction: column; gap: 22px; }
  .brand { display: grid; grid-template-columns: 20px 1fr; align-items: center; column-gap: 8px; }
  .brand strong { font-size: 13px; font-weight: 650; }
  .brand small { grid-column: 2; color: var(--text-muted); font-size: 10px; }
  .mark { width: 20px; height: 20px; border-radius: 50%; background: var(--text); box-shadow: inset 5px -3px 0 var(--sidebar-bg); }
  nav { display: flex; flex-direction: column; gap: 2px; }
  nav a { padding: 7px 9px; border-radius: 7px; color: var(--text-muted); font-size: 12px; text-decoration: none; }
  nav a:hover { background: var(--row-hover); color: var(--text); }
  .note { margin: auto 0 0; color: var(--text-muted); font-size: 10px; line-height: 1.45; }
  main { min-width: 0; max-width: 900px; padding: 46px 46px 90px; box-sizing: border-box; }
  .intro { max-width: 620px; margin-bottom: 48px; }
  .eyebrow { margin: 0 0 10px; color: var(--text-muted); font-size: 10px; font-weight: 600; letter-spacing: .08em; text-transform: uppercase; }
  h1 { margin: 0 0 12px; font-size: 30px; line-height: 1.1; letter-spacing: -0.02em; font-weight: 650; }
  .lede { margin: 0; color: var(--text-muted); font-size: 13px; line-height: 1.55; }
  section { scroll-margin-top: 28px; margin-bottom: 46px; }
  .section-head { display: grid; grid-template-columns: 28px 1fr; column-gap: 10px; align-items: baseline; margin-bottom: 12px; }
  .index { color: var(--text-muted); font-size: 10px; font-variant-numeric: tabular-nums; }
  h2 { margin: 0; font-size: 15px; font-weight: 620; }
  .section-head p { grid-column: 2; margin: 3px 0 0; color: var(--text-muted); font-size: 11px; line-height: 1.45; }
  .panel { border-radius: 14px; background: var(--sidebar-bg); padding: 16px; }
  .split { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .panel h3 { margin: 0 0 12px; font-size: 12px; font-weight: 600; }
  .swatches { display: grid; grid-template-columns: repeat(2, 1fr); gap: 6px; }
  .swatch { height: 56px; border-radius: 9px; border: 1px solid var(--hairline); display: flex; align-items: flex-end; padding: 7px; box-sizing: border-box; color: var(--text); font-size: 9px; }
  .swatch .inverse { color: var(--window-bg); }
  .type { display: flex; flex-direction: column; gap: 7px; }
  .type strong { font-size: 17px; font-weight: 600; }
  .type span { font-size: 13px; }
  .type small { color: var(--text-muted); font-size: 11px; }
  kbd { color: var(--text-muted); font: inherit; font-size: 10px; }
  .row-wrap { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  button { font: inherit; color: var(--text); cursor: default; }
  .primary, .secondary, .quiet, .danger { min-height: 30px; padding: 0 12px; border-radius: 8px; border: 0.5px solid transparent; font-size: 12px; }
  .primary { background: var(--text); color: var(--window-bg); }
  .secondary { background: var(--window-bg); border-color: var(--hairline); }
  .quiet { display: inline-flex; align-items: center; gap: 6px; background: transparent; color: var(--text-muted); }
  .quiet:hover { background: var(--row-hover); color: var(--text); }
  .danger { background: transparent; color: var(--danger); }
  .primary:disabled { opacity: .4; }
  .icon { width: 30px; height: 30px; display: grid; place-items: center; border-radius: 50%; border: 0.5px solid var(--glass-border); background: var(--glass-field); }
  .icon.dark { background: var(--text); color: var(--window-bg); }
  .stack { display: flex; flex-direction: column; gap: 10px; }
  .field-label { display: flex; flex-direction: column; gap: 5px; color: var(--text-muted); font-size: 10px; }
  input, textarea { box-sizing: border-box; width: 100%; border: 1px solid var(--hairline); border-radius: 8px; padding: 7px 9px; background: var(--window-bg); color: var(--text); font: inherit; font-size: 12px; outline: none; }
  input:focus-visible, textarea:focus-visible { border-color: var(--focus-ring); box-shadow: 0 0 0 1px var(--focus-ring); }
  textarea { resize: vertical; }
  .search { display: flex; align-items: center; gap: 7px; padding: 0 9px; border-radius: 8px; border: 1px solid var(--hairline); background: var(--window-bg); color: var(--text-muted); }
  .search input { border: 0; padding: 7px 0; background: transparent; }
  .segmented { display: flex; gap: 2px; padding: 3px; border-radius: 9px; background: var(--window-bg); }
  .segmented button { flex: 1; min-height: 26px; border-radius: 7px; background: transparent; color: var(--text-muted); font-size: 11px; }
  .segmented button.on { background: var(--row-hover); color: var(--text); }
  .switch-row { display: flex; align-items: center; justify-content: space-between; min-height: 34px; padding: 0 2px; font-size: 12px; }
  .toggle { width: 32px; height: 19px; border-radius: 999px; background: rgba(0,0,0,.12); position: relative; border: 0; }
  .toggle::after { content: ''; position: absolute; top: 2px; left: 2px; width: 15px; height: 15px; border-radius: 50%; background: #fff; transition: transform .15s ease-out; }
  .toggle.on { background: #24a35b; }
  .toggle.on::after { transform: translateX(13px); }
  .composer { display: flex; align-items: center; gap: 8px; margin-top: auto; }
  .composer-field { flex: 1; min-height: 30px; display: flex; align-items: center; padding: 0 12px; border-radius: 15px; background: var(--glass-field); border: 0.5px solid var(--glass-border); color: var(--text-muted); font-size: 12px; }
  .menu-demo { display: flex; align-items: center; gap: 14px; }
  .result { color: var(--text-muted); font-size: 11px; }
  .avatar-row { display: flex; flex-wrap: wrap; gap: 20px; }
  .avatar { display: flex; flex-direction: column; align-items: center; gap: 4px; width: 86px; }
  .avatar span { font-size: 12px; font-weight: 550; }
  .avatar small { color: var(--text-muted); font-size: 10px; }
  .transcript { display: flex; flex-direction: column; gap: 12px; }
  .message-row { display: flex; align-items: flex-end; gap: 8px; }
  .message-row.outgoing { justify-content: flex-end; }
  .bubble { max-width: 430px; padding: 9px 11px; border-radius: 14px; font-size: 12px; line-height: 1.4; }
  .bubble.incoming { background: var(--bubble-in); }
  .bubble.outgoing { background: var(--bubble-out); }
  .column { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; }
  .author { color: var(--text-muted); font-size: 10px; font-weight: 500; padding-left: 4px; }
  .reply { display: flex; flex-direction: column; gap: 1px; margin-top: 7px; padding: 6px 8px; border-left: 2px solid var(--text-muted); border-radius: 4px; background: var(--bubble-quote); font-size: 10px; }
  .reply span:first-child { color: var(--text-muted); }
  .reactions { display: flex; gap: 5px; }
  .reactions span { padding: 3px 6px; border-radius: 7px; background: var(--glass-field); border: 0.5px solid var(--glass-border); font-size: 10px; }
  .typing { display: flex; align-items: center; gap: 7px; color: var(--text-muted); font-size: 11px; }
  .profile-card { display: flex; flex-direction: column; align-items: center; gap: 4px; padding: 22px 16px; }
  .profile-card strong { font-size: 15px; font-weight: 620; }
  .profile-card > span { color: var(--text-muted); font-size: 11px; }
  .tabs { display: flex; gap: 4px; margin-top: 10px; }
  .tabs button { padding: 5px 9px; border-radius: 999px; background: transparent; color: var(--text-muted); font-size: 10px; }
  .tabs button.on { background: var(--window-bg); color: var(--text); }
  .setting-row { display: flex; align-items: center; justify-content: space-between; min-height: 34px; padding: 0 10px; border-radius: 8px; background: var(--window-bg); font-size: 11px; }
  .setting-row span { color: var(--text-muted); }
  .setting-row strong { font-weight: 500; }
  .icon-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 6px; }
  .icon-cell { display: flex; flex-direction: column; align-items: center; gap: 4px; padding: 8px 4px; border-radius: 8px; background: var(--window-bg); color: var(--text-muted); font-size: 9px; }
  .icon-cell small { max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .brand-row { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
  .empty { display: flex; flex-direction: column; align-items: center; gap: 6px; padding: 24px; color: var(--text-muted); text-align: center; }
  .empty strong { color: var(--text); font-size: 13px; font-weight: 600; }
  .empty span { font-size: 11px; }
  .empty .primary { margin-top: 5px; }
  .loading { display: flex; align-items: center; gap: 8px; color: var(--text-muted); font-size: 11px; }
  .spinner { width: 12px; height: 12px; border-radius: 50%; border: 1.5px solid var(--hairline); border-top-color: var(--text-muted); animation: spin .8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } .toggle::after { transition: none; } }
  @media (max-width: 760px) {
    .gallery { grid-template-columns: 1fr; }
    aside { position: static; height: auto; flex-direction: row; flex-wrap: wrap; align-items: center; }
    nav { flex-direction: row; flex-wrap: wrap; }
    .note { display: none; }
    main { padding: 28px 20px 60px; }
    .split { grid-template-columns: 1fr; }
  }
</style>
