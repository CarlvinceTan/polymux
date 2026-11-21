<script lang="ts">
  import logo from '../../assets/app-icon.webp';
  import AppIcon, {type AppId} from '../../components/AppIcon.svelte';
  import googledocs from 'simple-icons/icons/googledocs.svg?url';
  import github from 'simple-icons/icons/github.svg?url';
  import linear from 'simple-icons/icons/linear.svg?url';
  import xcode from 'simple-icons/icons/xcode.svg?url';
  import obsidian from 'simple-icons/icons/obsidian.svg?url';
  import spotify from 'simple-icons/icons/spotify.svg?url';
  // The Mac app's own Hub logos, and the mail logos the earlier desktop app used.
  import whatsapp from '../../assets/platforms/whatsapp.svg?url';
  import telegram from '../../assets/platforms/telegram.svg?url';
  import signal from '../../assets/platforms/signal.svg?url';
  import messenger from '../../assets/platforms/messenger.svg?url';
  import instagram from '../../assets/platforms/instagram.svg?url';
  import discord from '../../assets/platforms/discord.svg?url';
  import imessage from '../../assets/platforms/imessage.svg?url';
  import linkedin from '../../assets/platforms/linkedin.svg?url';
  import gmail from '../../assets/platforms/gmail.svg?url';
  import outlook from '../../assets/platforms/outlook.svg?url';
  import icloud from '../../assets/platforms/icloud.svg?url';
  import fastmail from '../../assets/platforms/fastmail.svg?url';

  type Item = {name: string; app?: AppId; src?: string; glyph?: string; color?: string};
  type Group = {name: string; detail: string; items: Item[]};

  // Bridges in plugins/hub/internal/hub/bridges.go, plus WeChat through its relay.
  const hubServices = 13;

  const tools: Group[] = [
    {name: 'Browser', detail: 'Any website', items: [
      {name: 'Chrome', app: 'chrome'}, {name: 'Notion', app: 'notion'}, {name: 'Figma', app: 'figma'},
      {name: 'Google Docs', glyph: googledocs, color: '#4285f4'}, {name: 'GitHub', glyph: github, color: '#f4f2ff'}, {name: 'Linear', glyph: linear, color: '#8f98ff'},
    ]},
    {name: 'Mac apps', detail: 'Apps you choose', items: [
      {name: 'Mail', app: 'mail'}, {name: 'Calendar', app: 'calendar'}, {name: 'Slack', app: 'slack'},
      {name: 'Xcode', glyph: xcode, color: '#3d9bff'}, {name: 'Obsidian', glyph: obsidian, color: '#a88bfa'}, {name: 'Spotify', glyph: spotify, color: '#1ed760'},
    ]},
  ];
  const hub: Group[] = [
    {name: 'Messaging via Hub', detail: `${hubServices} services`, items: [
      {name: 'WhatsApp', src: whatsapp}, {name: 'Telegram', src: telegram}, {name: 'Signal', src: signal}, {name: 'Messenger', src: messenger},
      {name: 'Instagram', src: instagram}, {name: 'Discord', src: discord}, {name: 'iMessage', src: imessage}, {name: 'LinkedIn', src: linkedin},
    ]},
    {name: 'Email via Hub', detail: 'Any IMAP', items: [
      {name: 'Gmail', src: gmail}, {name: 'Outlook', src: outlook}, {name: 'iCloud Mail', src: icloud}, {name: 'Fastmail', src: fastmail},
    ]},
  ];
</script>

{#snippet card(group: Group)}
  <article class="connection-group">
    <header><h3>{group.name}</h3><span>{group.detail}</span></header>
    <ul>
      {#each group.items as item (item.name)}
        <li>
          {#if item.app}<AppIcon id={item.app} size={26} />
          {:else if item.src}<img src={item.src} alt="" width="26" height="26" loading="lazy" />
          {:else}<span class="glyph" aria-hidden="true" style:--glyph={`url("${item.glyph}")`} style:--glyph-color={item.color}></span>{/if}
          <span>{item.name}</span>
        </li>
      {/each}
    </ul>
  </article>
{/snippet}

<div class="connection-map">
  <div class="connection-tier tools">
    {#each tools as group (group.name)}{@render card(group)}{/each}
  </div>
  <div class="connection-core"><img src={logo} alt="" width="26" height="26" />Polymux</div>
  <p class="connection-plugin">Optional Hub plugin</p>
  <div class="connection-tier hub">
    {#each hub as group (group.name)}{@render card(group)}{/each}
  </div>
</div>

<style>
  .connection-map { --wire: color-mix(in srgb, var(--focus) 70%, transparent); --gap: 30px; --col: 14px; position: relative; isolation: isolate; }
  .connection-tier { position: relative; display: grid; gap: var(--col); }
  .tools { grid-template-columns: repeat(2, minmax(0, 1fr)); padding-bottom: var(--gap); }
  .hub { grid-template-columns: minmax(0, 2fr) minmax(0, 1fr); margin-top: var(--gap); padding-top: var(--gap); }

  /* Wires: each card meets a bar, and both bars meet Polymux. Hub's are dashed because it's an optional plugin. */
  .tools::after, .hub::before { content: ''; position: absolute; border-top: 1px solid var(--wire); }
  .tools::after { inset: auto calc((100% - var(--col)) / 4) 0; }
  .hub::before { top: 0; left: calc((100% - var(--col)) / 3); right: calc((100% - var(--col)) / 6); border-top-style: dashed; }
  .tools article::after, .hub article::before, .connection-core::before, .connection-core::after, .connection-plugin::after { content: ''; position: absolute; left: 50%; height: var(--gap); border-left: 1px solid var(--wire); }
  .tools article::after { top: 100%; }
  .hub article::before { bottom: 100%; border-left-style: dashed; }
  .connection-core::before { bottom: 100%; }
  .connection-core::after, .connection-plugin::after { top: 100%; border-left-style: dashed; }

  .connection-group { position: relative; z-index: 1; min-width: 0; padding: 18px; border: 1px solid var(--line); border-radius: var(--radius); background: var(--surface); }
  .connection-group header { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 16px; }
  .connection-group h3 { margin: 0; color: var(--muted); font-size: 11px; font-weight: 600; letter-spacing: .04em; text-transform: uppercase; }
  .connection-group header span { flex: none; color: color-mix(in srgb, var(--muted) 75%, transparent); font-size: 10px; font-weight: 600; }
  .connection-group ul { display: grid; grid-template-columns: repeat(var(--cols, 6), minmax(0, 1fr)); gap: 8px; margin: 0; padding: 0; list-style: none; }
  .hub article:first-child ul { --cols: 8; }
  .hub article:last-child ul { --cols: 4; }
  .connection-group li { display: flex; min-width: 0; flex-direction: column; align-items: center; gap: 7px; padding: 11px 6px 9px; border-radius: var(--radius-small); background: var(--glass); box-shadow: var(--glass-edge); }
  .connection-group li img { width: 26px; height: 26px; object-fit: contain; }
  .connection-group li > span:last-child { max-width: 100%; overflow: hidden; color: var(--muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
  .glyph { width: 26px; height: 26px; background: var(--glyph-color); mask: var(--glyph) center / 22px no-repeat; }

  .connection-core { position: relative; z-index: 1; display: flex; align-items: center; gap: 8px; width: max-content; margin: var(--gap) auto 0; padding: 10px 14px 10px 11px; border: 1px solid var(--focus); border-radius: var(--radius-small); background: var(--surface); box-shadow: 0 0 44px -12px #8f7bffaa; color: var(--ink); font-size: 15px; font-weight: 700; letter-spacing: -.02em; }
  .connection-core img { border-radius: 22%; }
  .connection-plugin { position: relative; z-index: 1; width: max-content; margin: var(--gap) auto 0; padding: 4px 10px; border: 1px dashed var(--wire); border-radius: 999px; background: var(--page); color: var(--muted); font-size: 11px; font-weight: 600; }

  @media (max-width: 760px) {
    .connection-map { --gap: 24px; }
    .tools, .hub { grid-template-columns: minmax(0, 1fr); padding: 0; }
    .hub { margin-top: var(--gap); }
    .tools article::after, .hub article::before { display: none; }
    .tools::after, .hub::before { inset: 0 auto 0 50%; border-top: 0; border-left: 1px solid var(--wire); }
    .hub::before { border-left-style: dashed; }
    .connection-group ul, .hub article:first-child ul, .hub article:last-child ul { --cols: 4; }
  }
  @media (max-width: 440px) {
    .connection-group { padding: 14px; }
    .connection-group ul, .hub article:first-child ul, .hub article:last-child ul { --cols: 3; }
  }
</style>
