<script lang="ts">
  import logo from '../../assets/app-icon.webp';
  import AppIcon, {type AppId} from '../../components/AppIcon.svelte';
  import whatsapp from 'simple-icons/icons/whatsapp.svg?url';
  import telegram from 'simple-icons/icons/telegram.svg?url';
  import discord from 'simple-icons/icons/discord.svg?url';

  const tools: {name: string; apps: {id: AppId; name: string}[]}[] = [
    {name: 'Browser', apps: [{id: 'chrome', name: 'Chrome'}, {id: 'notion', name: 'Notion'}, {id: 'figma', name: 'Figma'}]},
    {name: 'Mac apps', apps: [{id: 'mail', name: 'Mail'}, {id: 'calendar', name: 'Calendar'}, {id: 'slack', name: 'Slack'}]},
  ];
  const messages = [
    {name: 'WhatsApp', icon: whatsapp, color: '#65de92'},
    {name: 'Telegram', icon: telegram, color: '#62c9f9'},
    {name: 'Discord', icon: discord, color: '#a1a8ff'},
  ];
</script>

<div class="connection-map">
  <div class="connection-tier">
    {#each tools as group (group.name)}
      <article class="connection-group">
        <h3>{group.name}</h3>
        <ul>
          {#each group.apps as app (app.id)}
            <li><AppIcon id={app.id} size={34} /><span>{app.name}</span></li>
          {/each}
        </ul>
      </article>
    {/each}
  </div>
  <div class="connection-core"><img src={logo} alt="" width="24" height="24" />Polymux</div>
  <article class="connection-group connection-messages">
    <h3>Messaging via Hub</h3>
    <ul>
      {#each messages as app (app.name)}
        <li><span class="brand-icon" aria-hidden="true" style:--brand={`url("${app.icon}")`} style:--brand-color={app.color}></span><span>{app.name}</span></li>
      {/each}
    </ul>
  </article>
</div>

<style>
  .connection-map { --wire: var(--focus); --gap: 24px; width: min(100%, 620px); margin-inline: auto; }
  .connection-tier { position: relative; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; padding-bottom: var(--gap); }
  .connection-tier::after { content: ''; position: absolute; inset: auto calc(25% - 4px) 0; border-top: 1px solid var(--wire); }
  .connection-group { position: relative; min-width: 0; padding: 16px; border: 1px solid var(--line); border-radius: var(--radius); background: var(--surface); }
  .connection-group h3 { margin: 0 0 14px; color: var(--text); font-size: 13px; font-weight: 600; }
  .connection-group ul { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; padding: 0; gap: 4px; list-style: none; }
  .connection-group li { display: flex; min-width: 0; flex-direction: column; align-items: center; gap: 8px; color: var(--muted); font-size: 12px; }
  .brand-icon { width: 34px; height: 34px; background: var(--brand-color); mask: var(--brand) center / 26px no-repeat; }
  .connection-tier article::after, .connection-core::before, .connection-core::after { content: ''; position: absolute; left: 50%; height: calc(var(--gap) + 1px); border-left: 1px solid var(--wire); }
  .connection-tier article::after, .connection-core::after { top: 100%; }
  .connection-core::before { bottom: 100%; }
  .connection-core { position: relative; display: flex; align-items: center; gap: 8px; width: max-content; margin: var(--gap) auto; padding: 8px 12px; border: 1px solid var(--focus); border-radius: var(--radius); background: var(--surface); font-size: 14px; font-weight: 600; }
  .connection-messages { width: min(100%, 302px); margin-inline: auto; }

  @media (max-width: 520px) {
    .connection-tier { grid-template-columns: minmax(0, 1fr); gap: var(--gap); }
    .connection-tier::after { display: none; }
    .connection-tier { padding-bottom: 0; }
    .connection-tier article:last-child::after { display: none; }
  }
</style>
