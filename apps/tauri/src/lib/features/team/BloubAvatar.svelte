<script context="module" lang="ts">
  let nextMaskId = 0;
</script>

<script lang="ts">
  import type {TeamAvatarDto, TeamAvatarExpression} from '@polymux/protocol';
  import {onDestroy, onMount} from 'svelte';
  import {onThemeChange} from '../../shared/theme';
  import {subscribeBloubClock} from './bloub/clock';
  import {bloubColorForTheme, type BloubTheme} from './bloub/colors';
  import type {BloubActivity} from './bloub/expression';
  import {BLOUB_VIEWBOX_RADIUS, bloubVerticalCenter, renderBloubFrame} from './bloub/model';

  export let avatar: TeamAvatarDto;
  /** Visual pose only; it is intentionally separate from durable avatar data. */
  export let expression: TeamAvatarExpression = 'neutral';
  /** Ephemeral activity only. Durable run/status text remains authoritative. */
  export let activity: BloubActivity = 'idle';
  export let size = 32;
  /** Centre the visible silhouette in static chrome, not its radial origin. */
  export let centerSilhouette = false;
  export let label = '';
  export let animated = true;
  export let monochrome = false;
  export let paper = 'var(--app-bg)';
  /** Overrides the app theme for isolated previews only. */
  export let theme: BloubTheme | null = null;

  const maskId = `polymux-bloub-mask-${++nextMaskId}`;
  let time = 0;
  let motionAllowed = false;
  let presentedActivity: BloubActivity = activity;
  let previousActivity: BloubActivity = activity;
  let completionTimer: ReturnType<typeof setTimeout> | null = null;
  let appTheme: BloubTheme = typeof document !== 'undefined'
    && document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light';

  $: frame = renderBloubFrame({...avatar, expression}, time, animated && motionAllowed);
  $: viewBoxY = (centerSilhouette ? bloubVerticalCenter(avatar.shape) : 0) - BLOUB_VIEWBOX_RADIUS;
  $: bodyColor = monochrome ? 'var(--neutral-900)' : bloubColorForTheme(avatar, theme ?? appTheme);
  $: paperColor = monochrome ? 'var(--neutral-100)' : paper;
  $: presentActivity(activity);

  function presentActivity(next: BloubActivity): void {
    if (next === previousActivity) return;
    const prior = previousActivity;
    previousActivity = next;
    if (completionTimer) clearTimeout(completionTimer);
    completionTimer = null;

    // A finished run otherwise jumps straight from Working to Idle. Preserve a
    // brief completion handoff without adding a durable state that could stale.
    if (animated && next === 'idle' && (prior === 'thinking' || prior === 'working')) {
      presentedActivity = 'complete';
      completionTimer = setTimeout(() => {
        presentedActivity = activity;
        completionTimer = null;
      }, 900);
      return;
    }
    presentedActivity = next;
  }

  onMount(() => onThemeChange(() => {
    appTheme = document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light';
  }));

  onMount(() => {
    if (!animated || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    motionAllowed = true;
    return subscribeBloubClock((seconds) => time = seconds);
  });

  onDestroy(() => {
    if (completionTimer) clearTimeout(completionTimer);
  });
</script>

<svg
  class="bloub-avatar"
  width={size}
  height={size}
  viewBox={`${-BLOUB_VIEWBOX_RADIUS} ${viewBoxY} ${BLOUB_VIEWBOX_RADIUS * 2} ${BLOUB_VIEWBOX_RADIUS * 2}`}
  role={label ? 'img' : undefined}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : 'true'}
  data-bloub-expression={expression}
  data-bloub-activity={presentedActivity}
  data-bloub-source-activity={activity}
  data-bloub-animated={animated && motionAllowed ? 'true' : 'false'}
>
  <defs>
    <mask
      id={maskId}
      maskUnits="userSpaceOnUse"
      x={-BLOUB_VIEWBOX_RADIUS}
      y={-BLOUB_VIEWBOX_RADIUS}
      width={BLOUB_VIEWBOX_RADIUS * 2}
      height={BLOUB_VIEWBOX_RADIUS * 2}
    >
      <path d={frame.bodyPath} fill="#fff"/>
      <g class="bloub-eyes">
        {#each frame.eyes as eye}
          <path d={eye.path} transform={eye.matrix} opacity={eye.opacity} fill="#000"/>
        {/each}
      </g>
    </mask>
  </defs>
  <!-- Bloub's eyes are holes in the body, backed by the current paper colour. -->
  <g class="bloub-state-motion">
    <path class="bloub-paper" d={frame.bodyPath} fill={paperColor}/>
    <path class="bloub-body" d={frame.bodyPath} fill={bodyColor} mask={`url(#${maskId})`}/>
  </g>
  <circle class="bloub-activity-ring" cx="0" cy="0" r={BLOUB_VIEWBOX_RADIUS - 7}/>
</svg>

<style>
  .bloub-avatar{display:block;overflow:visible;flex:none;transform-box:fill-box;transform-origin:center}
  .bloub-state-motion{transform-box:fill-box;transform-origin:center;transition:filter .18s ease}
  .bloub-activity-ring{fill:none;stroke:var(--neutral-500);stroke-width:4;stroke-linecap:round;opacity:0;transform-box:fill-box;transform-origin:center;transition:opacity .16s ease,stroke .16s ease}
  .bloub-avatar[data-bloub-activity="thinking"] .bloub-activity-ring{stroke:currentColor;stroke-dasharray:18 18;opacity:.38}
  .bloub-avatar[data-bloub-activity="working"] .bloub-activity-ring{stroke:currentColor;stroke-dasharray:44 654;opacity:.42}
  .bloub-avatar[data-bloub-activity="waiting"] .bloub-activity-ring{stroke:var(--neutral-500);stroke-dasharray:5 16;opacity:.44}
  .bloub-avatar[data-bloub-activity="attention"] .bloub-activity-ring{stroke:var(--warning-500,#b77716);stroke-dasharray:18 10;opacity:.58}
  .bloub-avatar[data-bloub-activity="complete"] .bloub-activity-ring{stroke:var(--success-500,#2d8062);stroke-dasharray:697 697;opacity:.5}
  .bloub-avatar[data-bloub-activity="failed"] .bloub-activity-ring{stroke:var(--danger-500);stroke-dasharray:28 12;opacity:.58}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="idle"] .bloub-state-motion{animation:bloub-idle-breathe 3.8s ease-in-out infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="thinking"] .bloub-state-motion{animation:bloub-thinking-focus 2.4s ease-in-out infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="thinking"] .bloub-activity-ring{animation:bloub-thinking-orbit 2s linear infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="working"] .bloub-state-motion{animation:bloub-working-float 1.35s cubic-bezier(.4,0,.2,1) infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="working"] .bloub-activity-ring{animation:bloub-working-orbit 1.3s linear infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="waiting"] .bloub-state-motion{animation:bloub-waiting-sway 2.6s ease-in-out infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="waiting"] .bloub-activity-ring{animation:bloub-waiting-pulse 1.8s ease-in-out infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="attention"] .bloub-state-motion{animation:bloub-attention-nudge 2.4s ease-in-out infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="attention"] .bloub-activity-ring{animation:bloub-attention-beacon 1.55s ease-in-out infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="complete"] .bloub-state-motion{animation:bloub-complete-settle .62s cubic-bezier(.16,1,.3,1) both}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="complete"] .bloub-activity-ring{animation:bloub-complete-ring .72s cubic-bezier(.16,1,.3,1) both}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="failed"] .bloub-state-motion{animation:bloub-failed-nudge 2.8s ease-in-out infinite}
  .bloub-avatar[data-bloub-animated="true"][data-bloub-activity="failed"] .bloub-activity-ring{animation:bloub-failed-pulse 1.8s ease-in-out infinite}
  @keyframes bloub-idle-breathe{0%,100%{transform:translateY(0) scale(1)}50%{transform:translateY(-.45px) scale(1.008)}}
  @keyframes bloub-thinking-focus{0%,100%{transform:rotate(-.7deg) translateY(0)}50%{transform:rotate(.7deg) translateY(-.8px)}}
  @keyframes bloub-thinking-orbit{to{rotate:360deg}}
  @keyframes bloub-working-orbit{to{rotate:360deg}}
  @keyframes bloub-working-float{0%,100%{transform:translateY(0)}50%{transform:translateY(-1.5px)}}
  @keyframes bloub-waiting-sway{0%,100%{transform:translateX(-.55px) translateY(.35px)}50%{transform:translateX(.55px) translateY(.35px)}}
  @keyframes bloub-waiting-pulse{0%,100%{opacity:.28}50%{opacity:.54}}
  @keyframes bloub-attention-nudge{0%,72%,100%{transform:translateX(0)}78%{transform:translateX(-1.25px)}84%{transform:translateX(1.25px)}90%{transform:translateX(-.65px)}}
  @keyframes bloub-attention-beacon{0%,100%{opacity:.4;scale:.98}50%{opacity:.66;scale:1.025}}
  @keyframes bloub-complete-settle{0%{transform:scale(.94);filter:saturate(.88)}55%{transform:scale(1.035);filter:saturate(1.12)}100%{transform:scale(1);filter:saturate(1)}}
  @keyframes bloub-complete-ring{0%{opacity:0;stroke-dashoffset:697}100%{opacity:.5;stroke-dashoffset:0}}
  @keyframes bloub-failed-nudge{0%,70%,100%{transform:translateX(0)}76%{transform:translateX(-1.15px)}82%{transform:translateX(1.15px)}88%{transform:translateX(-.55px)}}
  @keyframes bloub-failed-pulse{0%,100%{opacity:.42}50%{opacity:.64}}
  @media (prefers-reduced-motion:reduce){.bloub-state-motion,.bloub-activity-ring{animation:none!important}.bloub-state-motion{transform:none!important;filter:none!important}}
</style>
