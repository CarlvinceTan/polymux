<script lang="ts">
  import type {AppId} from './AppIcon.svelte';
  import chrome from '../assets/windows/chrome.webp';
  import slack from '../assets/windows/slack.webp';
  import figma from '../assets/windows/figma.webp';
  import notion from '../assets/windows/notion.webp';
  import mail from '../assets/windows/mail.webp';
  import calendar from '../assets/windows/calendar.webp';

  let {app, agent}: {
    app: {id: AppId; tint: string; tx: string; ty: string};
    agent: {src: string};
  } = $props();

  // Real app captures from each vendor's own product and help pages.
  const shots: Record<AppId, string> = {chrome, slack, figma, notion, mail, calendar};
</script>

<div class="work-window" style:--tint={app.tint} style:--tx={app.tx} style:--ty={app.ty}>
  <img class="shot" src={shots[app.id]} alt="" width="480" height="250" decoding="async" />
  <div class="cursor">
    <svg viewBox="0 0 20 20"><path d="M3 2.5 16.5 9l-6 1.8L8 17z" /></svg>
    <b style:background-image={`url(${agent.src})`}></b>
  </div>
  <i class="click"></i>
</div>

<style>
  .work-window { position: relative; width: 200px; height: 104px; border-radius: 9px; overflow: hidden; background: #fff; box-shadow: 0 0 0 1px #ffffff40, 0 18px 40px -10px #000c, 0 0 46px -12px var(--tint); }
  .shot { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }

  .cursor { position: absolute; left: 0; top: 0; display: flex; align-items: flex-start; animation: roam 3.6s cubic-bezier(.45,0,.2,1) infinite; filter: drop-shadow(0 2px 3px #0008); }
  .cursor svg { width: 12px; height: 12px; fill: #fff; stroke: #1a1730; stroke-width: 1.6; stroke-linejoin: round; }
  .cursor b { width: 13px; height: 13px; margin: 9px 0 0 -3px; border-radius: 50%; background: #fff center/cover; box-shadow: 0 0 0 1px #fff; }
  .click { position: absolute; left: var(--tx); top: var(--ty); width: 20px; height: 20px; margin: -10px 0 0 -10px; border-radius: 50%; box-shadow: 0 0 0 2px var(--tint); opacity: 0; animation: tap 3.6s ease-out infinite; }

  @keyframes roam {
    0%, 16% { translate: 118px 24px; }
    38%, 66% { translate: calc(var(--tx) - 2px) calc(var(--ty) - 2px); }
    72% { translate: calc(var(--tx) + 1px) calc(var(--ty) + 1px); }
    100% { translate: calc(var(--tx) + 30px) calc(var(--ty) - 20px); }
  }
  @keyframes tap { 0%, 40% { opacity: 0; scale: .4; } 46% { opacity: .9; scale: 1; } 60% { opacity: 0; scale: 1.6; } 100% { opacity: 0; } }
</style>
