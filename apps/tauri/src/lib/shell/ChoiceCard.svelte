<script lang="ts">
  import type { Message, Part } from '../api/contract';
  import { CHOICE_DISMISSED } from '../api/models';
  import type { Session } from '../session.svelte';

  let { part, message, session, outgoing = false }: {
    part: Part; message: Message; session: Session; outgoing?: boolean;
  } = $props();

  let selected = $state<string>();
  let text = $state('');
  let answering = $state(false);
  let failed = $state(false);
  const answer = $derived(part.answerID ?? selected);

  function letter(index: number): string {
    let number = index + 1;
    let result = '';
    while (number > 0) {
      number--;
      result = String.fromCharCode(65 + number % 26) + result;
      number = Math.floor(number / 26);
    }
    return result;
  }

  function label(id: string): string {
    if (id === CHOICE_DISMISSED) return 'Skipped';
    if (id === '_text') return part.answerText ?? (text.trim() || 'Custom answer');
    return part.options?.find((option) => option.id === id)?.label ?? id;
  }

  async function choose(id: string) {
    const value = text.trim();
    if (answering || answer || (id === '_text' && !value)) return;
    answering = true;
    selected = id;
    failed = false;
    try {
      const accepted = await session.client.answerChoice(message.threadID, message.id, id, id === '_text' ? value : undefined);
      session.acceptChoice(message.threadID, accepted);
    } catch {
      selected = undefined;
      failed = true;
    } finally {
      answering = false;
    }
  }
</script>

<div class={['choice', { outgoing }]}>
  <div class="heading">
    <span>{part.question ?? ''}</span>
    {#if !answer}
      <button class="dismiss" aria-label="Dismiss question" title="Dismiss without answering" disabled={answering} onclick={() => void choose(CHOICE_DISMISSED)}>
        <svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
      </button>
    {/if}
  </div>
  {#if answer}
    <div class="answer"><svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">{#if answer === CHOICE_DISMISSED}<path d="M4 2v7h8m-3-3 3 3-3 3" />{:else}<path d="m3 8 3 3 7-7" />{/if}</svg><span>{label(answer)}</span></div>
  {:else}
    {#each part.options ?? [] as option, index (option.id)}
      <button class="option" aria-label="{letter(index)}. {option.label}" disabled={answering} onclick={() => void choose(option.id)}>
        <span class="letter">{letter(index)}</span><span>{option.label}</span>
      </button>
    {/each}
    {#if part.allowText}
      <form onsubmit={(event) => { event.preventDefault(); void choose('_text'); }}>
        <input aria-label="Another answer" placeholder="Another answer" bind:value={text} disabled={answering} />
        <button class="send" type="submit" disabled={!text.trim() || answering}>Send</button>
      </form>
    {/if}
  {/if}
  {#if failed && !part.answerID}<div class="error" role="status">Couldn’t record that choice.</div>{/if}
</div>

<style>
  .choice { display: flex; flex-direction: column; gap: 12px; box-sizing: border-box; max-width: 550px; padding: 12px; border-radius: var(--radius-bubble); background: var(--bubble-in); color: var(--text); font-size: 13px; line-height: 17px; overflow-wrap: anywhere; }
  .choice.outgoing { background: var(--bubble-out); }
  .heading { display: flex; align-items: baseline; gap: 12px; justify-content: space-between; }
  .dismiss { flex: none; display: grid; place-items: center; align-self: flex-start; width: 24px; height: 24px; margin: -3px 0 -4px; padding: 0; border: 0; border-radius: 6px; color: var(--text-muted); background: none; cursor: pointer; }
  .dismiss:hover { color: var(--text); background: color-mix(in srgb, var(--text) 14%, transparent); }
  .option { display: flex; gap: 10px; align-items: baseline; padding: 0; border: 0; color: inherit; background: none; text-align: left; font: inherit; cursor: pointer; }
  .letter { min-width: 14px; color: var(--text-muted); font-size: 11px; font-weight: 600; }
  .answer { display: flex; gap: 8px; align-items: center; color: var(--text-muted); }
  form { display: flex; gap: 8px; align-items: center; min-width: 0; }
  input { min-width: 0; flex: 1; padding: 4px 6px; border: 1px solid color-mix(in srgb, var(--text) 25%, transparent); border-radius: 5px; background: var(--glass-field); color: inherit; font: inherit; }
  .send { padding: 3px 8px; border: 1px solid color-mix(in srgb, var(--text) 25%, transparent); border-radius: 5px; color: inherit; background: var(--glass-field); font: inherit; cursor: pointer; }
  button:disabled { opacity: .5; cursor: default; }
  .error { color: var(--text-muted); font-size: 11px; }
</style>
