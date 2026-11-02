<script module lang="ts">
  import type {Platform} from '../../shared/components/PlatformLogo.svelte';
  import type {ActivityPreviewRequestDto} from '@polymux/protocol';

  export type AgentActivityKind =
    | 'thinking'
    | 'compacting'
    | 'reading'
    | 'searching'
    | 'running'
    | 'task'
    | 'memory'
    | 'skill'
    | 'tool'
    | 'resource'
    | 'editing'
    | 'messaging'
    | 'mail'
    | 'plugin'
    | 'commentary';
  export type AgentActivityStatus = 'pending' | 'active' | 'completed' | 'failed';

  /** One reported step inside a tool invocation (from tool.progress events),
   * shown as an indented sub-row when the activity row is opened. */
  export type AgentActivityStep = {
    id: string;
    label: string;
    status: AgentActivityStatus;
    /** The step's own outcome, shown under its label. Set when a counted row
     * ("Ran 2 commands") folds each call it stands for in as one of its steps. */
    result?: string;
  };

  export type AgentActivityItem = {
    id: string;
    kind: AgentActivityKind;
    status: AgentActivityStatus;
    label: string;
    /** Overrides the kind's glyph when the activity has one of its own. Set
     * when the row is built, because the label it used to be inferred from is
     * translated and so is no longer a stable key. */
    icon?: 'globe' | 'computer';
    /** A platform's own mark, drawn in place of the kind's glyph. Set when the
     * row is for one of the hub's accounts, so a WhatsApp read reads as
     * WhatsApp rather than as a generic tool. */
    logo?: Platform;
    target?: string;
    result?: string;
    steps?: AgentActivityStep[];
    /** How many calls this row stands for. A collapsed stretch of identical
     * calls stays counted so the settled trail can still say "Ran 2 commands". */
    count?: number;
    /** A passive frame for Browser or exact-window activity. */
    preview?: ActivityPreviewRequestDto;
  };
</script>

<script lang="ts">
  import {onMount} from 'svelte';
  import {fade, fly} from 'svelte/transition';
  import {cubicOut} from 'svelte/easing';
  import {activityDuration, collapseActivities, formatElapsedSeconds, nextDurationTickDelay, settledActivities} from './activities';
  import Icon from '../../shared/components/Icon.svelte';
  import LiveActivityPreview from '../../shared/components/LiveActivityPreview.svelte';
  import PlatformLogo from '../../shared/components/PlatformLogo.svelte';
  import {MAIN_UI_ICON_SIZE, MAIN_UI_ICON_STROKE_WIDTH} from '../../shared/layout/iconSizing';
  import {t} from '../../../i18n';

  export let activities: AgentActivityItem[] = [];
  export let startedAt: string | undefined = undefined;
  export let completedAt: string | undefined = undefined;
  export let streaming = false;

  // A live run narrates itself in full, then folds back to its counted summary
  // as soon as it settles. Starting from the incoming prop also covers the
  // component's first paint, before a reactive streaming change can occur.
  let expanded = streaming;
  let previousStreaming = streaming;
  let now = Date.now();
  /** Per-row detail disclosure, keyed by activity id. Mirrors ChatGPT's
   * trail, where a row's extra detail stays hidden until that row is opened. */
  let detailOpen: Record<string, boolean> = {};
  /** Computer and Browser rows share one content-only preview slot. */
  let previewOpenId: string | null = null;

  // Keep a user's manual fold choice for the rest of the current run, but do
  // not let an opened live trail spill into the finished transcript. A future
  // run mounted into the same component opens its narration again.
  $: if (streaming !== previousStreaming) {
    previousStreaming = streaming;
    expanded = streaming;
    detailOpen = {};
    previewOpenId = null;
  }

  $: elapsed = Math.max(1, activityDuration(startedAt, completedAt, now));
  $: collapsed = collapseActivities(activities);
  $: settled = settledActivities(collapsed);
  $: latest = collapsed.at(-1);
  // Settled shows the codex-style summary: one counted row per stretch of the
  // same work ("Ran 2 commands"), plus anything that failed. The full trail
  // waits behind the heading. A manually folded live run keeps only its latest
  // status line; otherwise its chronological narration stays visible.
  $: visibleActivities = expanded ? collapsed : streaming && latest ? [latest] : settled;
  // Collapsed streaming shows exactly one row, so a swap is a handoff: the
  // outgoing row fades out, then the incoming one slides up into its place.
  // Rows are stacked in a single grid cell so the two never push each other.
  $: solo = !expanded && streaming;
  $: if (previewOpenId && !collapsed.some((activity) => activity.id === previewOpenId && activity.preview)) previewOpenId = null;

  function toggleExpanded(): void {
    if (!activities.length) return;
    expanded = !expanded;
    if (!expanded) previewOpenId = null;
  }

  function togglePreview(id: string): void {
    previewOpenId = previewOpenId === id ? null : id;
  }

  /** Reasoning opens as its tokens arrive, but stays a normal disclosure so a
   * user can fold it for the rest of the turn. Other activity detail remains
   * closed until explicitly requested. */
  function detailIsOpen(activity: AgentActivityItem, opened: Record<string, boolean>, isStreaming: boolean): boolean {
    return opened[activity.id] ?? (isStreaming && activity.kind === 'thinking' && Boolean(activity.result));
  }

  function toggleDetail(activity: AgentActivityItem): void {
    detailOpen = {...detailOpen, [activity.id]: !detailIsOpen(activity, detailOpen, streaming)};
  }

  /** Live rows key on the activity's own id; settled ones key under a prefix
   * of their own, so the handoff re-inserts the rows and they cross-fade in
   * rather than the last live label hard-swapping into a summary row. */
  function rowKey(activity: AgentActivityItem): string {
    return streaming ? activity.id : `settled:${activity.id}`;
  }


  const OUT_MS = 140;
  const IN_MS = 240;

  /**
   * A fixed 1s interval drifts and gets coalesced whenever the main thread is
   * busy, which makes the counter stall and then jump. Each tick is instead
   * scheduled to land just after the next true elapsed-second boundary, so the
   * displayed value advances once per second regardless of interval jitter.
   */
  onMount(() => {
    let timer = 0;
    const tick = () => {
      now = Date.now();
      if (completedAt) return;
      timer = window.setTimeout(tick, nextDurationTickDelay(startedAt, now) + 4);
    };
    tick();
    return () => window.clearTimeout(timer);
  });

  /** How far through its current cycle a looping animation is, 0–1. */
  function cycleFraction(animation: Animation): number {
    const duration = Number(animation.effect?.getComputedTiming().duration) || 0;
    if (!duration) return 0;
    return ((Number(animation.currentTime) || 0) % duration) / duration;
  }

  /**
   * The glimmer sweeps at a fixed speed, so a long label takes longer to cross
   * than a short one. CSS cannot read the row's ink width, so the row reports it
   * as `--glint-ink` (unitless px) and the stylesheet derives the travel and the
   * duration from it.
   */
  function glint(node: HTMLElement, _label: string) {
    /**
     * The width feeds the cycle length, so every remeasure — the first one after
     * mount, and each label change after that — hands the running sweep a new
     * duration. The browser keeps the animation's start time across that change,
     * so the same elapsed time lands on a different fraction of the new cycle
     * and the wave snaps backwards mid-pass. Carrying the old fraction over onto
     * the new duration keeps the pass continuous through the change.
     */
    const measure = () => {
      const row = node.getBoundingClientRect();
      const label = node.querySelector('.activity-copy > span, .activity-copy .activity-row-line > span');
      const box = label ? label.getBoundingClientRect() : row;
      const ink = String(Math.max(40, Math.round(box.right - row.left)));
      // Where the label starts within the row, so it can place the wave from the
      // row's head position rather than from its own left edge.
      node.style.setProperty('--glint-label', `${Math.round(box.left - row.left)}px`);
      if (node.style.getPropertyValue('--glint-ink') === ink) return;
      const running = node.getAnimations({subtree: true}).filter((animation) => animation.effect);
      const fractions = running.map(cycleFraction);
      node.style.setProperty('--glint-ink', ink);
      // Reading a custom property flushes the pending style recalc, so the
      // durations read below are the new ones rather than the ones being
      // replaced — without it the carried-over fraction lands on the old cycle
      // and the sweep still jumps.
      void getComputedStyle(node).getPropertyValue('--glint-cycle');
      running.forEach((animation, index) => {
        const duration = Number(animation.effect?.getComputedTiming().duration) || 0;
        if (duration) animation.currentTime = fractions[index] * duration;
      });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return {update: measure, destroy: () => observer.disconnect()};
  }

  const activityIcons: Record<AgentActivityKind, 'brain' | 'compact' | 'book-open' | 'search' | 'terminal' | 'task' | 'sparkles' | 'wrench' | 'link' | 'edit' | 'chat' | 'archive' | 'mail'> = {
    thinking: 'brain',
    compacting: 'compact',
    reading: 'book-open',
    searching: 'search',
    running: 'terminal',
    task: 'task',
    memory: 'archive',
    skill: 'sparkles',
    tool: 'wrench',
    resource: 'link',
    editing: 'edit',
    messaging: 'chat',
    mail: 'mail',
    plugin: 'wrench',
    commentary: 'chat',
  };
</script>

<section class:streaming class:expanded class="agent-activity" aria-label={$t('activity.title')}>
  <button
    type="button"
    class="agent-activity-heading"
    aria-expanded={expanded}
    onclick={toggleExpanded}
  >
    <span>{streaming ? $t('activity.workingFor', {elapsed: formatElapsedSeconds(elapsed)}) : $t('activity.workedFor', {elapsed: formatElapsedSeconds(elapsed)})}</span>
    {#if activities.length}<Icon name="chevron" size={MAIN_UI_ICON_SIZE} strokeWidth={MAIN_UI_ICON_STROKE_WIDTH}/>{/if}
  </button>

  {#if visibleActivities.length}
    <ul class="agent-activity-list" class:solo aria-live="polite">
      {#each visibleActivities as activity (rowKey(activity))}
        <li
          in:fly|local={{y: solo ? 9 : 0, duration: solo ? IN_MS : 0, delay: solo ? OUT_MS : 0, easing: cubicOut}}
          out:fade|local={{duration: solo ? OUT_MS : 0, easing: cubicOut}}
          class:settled={!streaming}
          use:glint={activity.label} class:active={activity.status === 'active'} class:live={streaming && activity.status === 'active'} class:failed={activity.status === 'failed'} class:commentary={activity.kind === 'commentary'}>
          {#if activity.kind !== 'commentary'}
            {#if activity.logo}
              <PlatformLogo platform={activity.logo} size={17}/>
            {:else}
              <Icon name={activity.icon ?? activityIcons[activity.kind]} size={17}/>
            {/if}
          {/if}
          {#if activity.kind === 'commentary'}
            <!-- Narration is the model's own prose. Show it as ordinary wrapping
                 text, not as a truncated disclosure row. -->
            <span class="activity-copy">{activity.label}</span>
          {:else if (expanded || !streaming) && activity.preview}
            <button
              type="button"
              class="activity-copy activity-detail-toggle activity-preview-toggle"
              aria-label={`${activity.label}${activity.target ? ` ${activity.target}` : ''}`}
              aria-expanded={previewOpenId === activity.id}
              onclick={() => togglePreview(activity.id)}
            >
              <span>{activity.label}{#if activity.target} <span class="activity-target">{activity.target}</span>{/if}</span>
              {#if previewOpenId === activity.id}
                <LiveActivityPreview source={activity.preview}/>
              {/if}
            </button>
          {:else if (expanded || !streaming) && (activity.result || activity.steps?.length)}
            <button
              type="button"
              class="activity-copy activity-detail-toggle"
              aria-expanded={detailIsOpen(activity, detailOpen, streaming)}
              onclick={() => toggleDetail(activity)}
            >
              <span class="activity-row-line">
                <span>{activity.label}{#if activity.target} <span class="activity-target">{activity.target}</span>{/if}</span>
                <Icon name="chevron" size={13}/>
              </span>
              {#if detailIsOpen(activity, detailOpen, streaming)}
                {#if activity.steps?.length}
                  <ul class="activity-steps">
                    {#each activity.steps as step (step.id)}
                      <li class:active={step.status === 'active'} class:failed={step.status === 'failed'}>
                        {step.label}{#if step.result}<small>{step.result}</small>{/if}
                      </li>
                    {/each}
                  </ul>
                {/if}
                {#if activity.result}
                  {#if activity.kind === 'thinking'}
                    <!-- Reasoning is streamed provider text, not markdown. Keep
                         whitespace and token boundaries exactly as delivered. -->
                    <span class="activity-thinking">{activity.result}</span>
                  {:else}<small>{activity.result}</small>{/if}
                {/if}
              {/if}
            </button>
          {:else}
            <span class="activity-copy">
              <span>{activity.label}{#if activity.target} <span class="activity-target">{activity.target}</span>{/if}</span>
              {#if streaming && activity.status === 'active' && activity.steps?.length}
                <small class="activity-live-step">{activity.steps.at(-1)?.label}</small>
              {/if}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>
