<script module lang="ts">
  export type ActiveGoal = {
    id: string;
    text: string;
    startedAt: string;
    status: 'active' | 'paused';
  };
</script>

<script lang="ts">
  import {onDestroy, onMount, tick} from 'svelte';
  import Icon from '../../shared/components/Icon.svelte';
  import {t, translate} from '../../../i18n';

  export let goal: ActiveGoal;
  export let onEdit: (text: string) => void = () => {};
  export let onTogglePaused: () => void = () => {};
  export let onDelete: () => void = () => {};

  let now = Date.now();
  let timer: ReturnType<typeof setInterval> | undefined;
  let editing = false;
  let draft = '';
  let input: HTMLInputElement;

  $: elapsed = formatElapsed(Math.max(0, now - new Date(goal.startedAt).getTime()));

  onMount(() => {
    timer = setInterval(() => now = Date.now(), 1000);
  });

  onDestroy(() => clearInterval(timer));

  async function startEdit(): Promise<void> {
    draft = goal.text;
    editing = true;
    await tick();
    input?.focus();
    input?.select();
  }

  function save(): void {
    const value = draft.trim();
    if (!value) return;
    editing = false;
    onEdit(value);
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') editing = false;
    if (event.key === 'Enter') save();
  }

  function formatElapsed(milliseconds: number): string {
    const seconds = Math.floor(milliseconds / 1000);
    if (seconds < 60) return translate('time.seconds', {seconds});
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return translate('time.minutes', {minutes});
    return translate('time.hoursMinutes', {hours: Math.floor(minutes / 60), minutes: minutes % 60});
  }
</script>

<section class:paused={goal.status === 'paused'} class="stacked-bar goal-bar" aria-label={$t('goal.current')}>
  <div class="stacked-row goal-row">
    <span class="goal-state" aria-hidden="true"><Icon name="goal" size={15}/></span>
    {#if editing}
      <input bind:this={input} bind:value={draft} aria-label={$t('goal.edit')} onkeydown={keydown} onblur={save}/>
    {:else}
      <span class="goal-copy">
        <strong>{goal.status === 'paused' ? $t('goal.paused') : $t('goal.pursuing')}</strong>
        <span>{goal.text}</span>
        <small>· {elapsed}</small>
      </span>
    {/if}
    <div class="goal-actions">
      <button type="button" aria-label={$t('goal.edit')} data-tooltip-label={$t('common.edit')} onclick={startEdit}><Icon name="edit" size={14}/></button>
      <button type="button" aria-label={goal.status === 'paused' ? $t('goal.resume') : $t('goal.pause')} onclick={onTogglePaused}><Icon name={goal.status === 'paused' ? 'play' : 'pause'} size={14}/></button>
      <button type="button" aria-label={$t('goal.delete')} data-tooltip-label={$t('common.delete')} onclick={onDelete}><Icon name="trash" size={14}/></button>
    </div>
  </div>
</section>
