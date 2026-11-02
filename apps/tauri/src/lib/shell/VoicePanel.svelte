<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import type { Session } from '../session.svelte';
  import type { Thread } from '../api/contract';
  import { isOffline, reason } from '../api/errors';
  import { createDictation, nativeSpeech } from './dictation';

  let { session, thread, onClose }: { session: Session; thread: Thread; onClose: () => void } = $props();
  type Phase = 'idle' | 'listening' | 'thinking' | 'speaking' | 'error';
  let phase = $state<Phase>('idle');
  let draft = $state('');
  let partial = $state('');
  let issue = $state('');
  let muted = $state(false);
  let history = $state<{ role: 'user' | 'assistant'; text: string }[]>([]);
  let input = $state<HTMLInputElement>();
  let busy = $state(false);
  let controller: AbortController | undefined;
  let disposed = false;

  const dictation = createDictation(nativeSpeech, {
    state(next, _level, message) {
      if (disposed) return;
      issue = message ?? '';
      if (next === 'preparing' || next === 'recording') phase = 'listening';
      else if (next === 'stopping') phase = 'thinking';
      else if (next === 'idle' && !busy && phase === 'listening') phase = 'idle';
    },
    transcript(text, _start, final) {
      if (disposed) return;
      partial = text;
      draft = text;
      if (final && text.trim()) void ask(text.trim());
    },
  });

  const status = $derived(phase === 'listening' ? 'Listening' : phase === 'thinking' ? 'Thinking' : phase === 'speaking' ? 'Speaking' : phase === 'error' ? 'Voice unavailable' : 'Tap the microphone to talk');

  onMount(() => {
    void tick().then(() => input?.focus());
  });

  onDestroy(() => {
    disposed = true;
    controller?.abort();
    dictation.stop(false);
    if (typeof speechSynthesis !== 'undefined') speechSynthesis.cancel();
  });

  async function ask(text: string) {
    if (busy || !text.trim()) return;
    busy = true;
    phase = 'thinking';
    issue = '';
    partial = '';
    draft = '';
    history = [...history, { role: 'user' as const, text }].slice(-6);
    controller?.abort();
    controller = new AbortController();
    let reply = '';
    try {
      for await (const fragment of session.client.voiceStream(thread.id, { id: crypto.randomUUID(), text }, controller.signal)) {
        if (!fragment.text) continue;
        reply += fragment.text;
        const last = history.at(-1);
        history = last?.role === 'assistant' ? [...history.slice(0, -1), { ...last, text: reply }] : [...history, { role: 'assistant' as const, text: reply }];
      }
      if (!reply.trim()) { phase = 'idle'; return; }
      phase = 'speaking';
      speak(reply, () => { if (!disposed) phase = 'idle'; });
    } catch (error) {
      if (controller.signal.aborted) return;
      phase = 'error';
      issue = isOffline(error) ? 'You’re offline, so this reply didn’t finish.' : reason(error, 'Couldn’t finish this voice reply.');
    } finally {
      busy = false;
    }
  }

  function speak(text: string, done: () => void) {
    if (muted || typeof speechSynthesis === 'undefined') { done(); return; }
    speechSynthesis.cancel();
    const utterance = new SpeechSynthesisUtterance(text);
    utterance.rate = 1.02;
    utterance.onend = done;
    utterance.onerror = done;
    speechSynthesis.speak(utterance);
  }

  function toggleMic() {
    if (phase === 'listening') dictation.stop(true);
    else if (!busy) { draft = ''; partial = ''; issue = ''; void dictation.start(); }
  }

  function toggleMute() {
    muted = !muted;
    if (muted && typeof speechSynthesis !== 'undefined') speechSynthesis.cancel();
  }

  function submit(event: SubmitEvent) {
    event.preventDefault();
    const text = draft.trim();
    if (text) void ask(text);
  }

  function close() {
    controller?.abort();
    dictation.stop(false);
    if (typeof speechSynthesis !== 'undefined') speechSynthesis.cancel();
    onClose();
  }
</script>

<div class="voice" role="dialog" aria-modal="true" aria-label="Voice conversation">
  <div class="top">
    <span class="label">Voice</span>
    <button class="close" aria-label="Close voice" onclick={close}>
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6 6 18M6 6l12 12"></path></svg>
    </button>
  </div>

  <div class="stage">
    <button class="orb" class:listening={phase === 'listening'} class:thinking={phase === 'thinking'} class:speaking={phase === 'speaking'} aria-label={phase === 'listening' ? 'Stop listening' : 'Start listening'} onclick={toggleMic}>
      <span class="core"></span>
      <span class="ring one"></span>
      <span class="ring two"></span>
    </button>
    <strong>{status}</strong>
    {#if partial && phase === 'listening'}<p class="partial">{partial}</p>{/if}
    {#if issue}<p class="issue" role="status">{issue}</p>{/if}
  </div>

  <div class="conversation" aria-live="polite">
    {#each history as line, index (index)}
      <div class="line" class:user={line.role === 'user'}><span>{line.role === 'user' ? 'You' : (session.agentsFor(thread)[0]?.name ?? 'Bot')}</span><p>{line.text}</p></div>
    {/each}
  </div>

  <form class="composer" onsubmit={submit}>
    <input bind:this={input} bind:value={draft} aria-label="Voice message" placeholder="Type a message" disabled={busy} />
    <button class="send" type="submit" aria-label="Send voice message" disabled={busy || !draft.trim()}>
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M12 19V5M6 11l6-6 6 6"></path></svg>
    </button>
    <button class="mute" type="button" aria-label={muted ? 'Unmute voice replies' : 'Mute voice replies'} aria-pressed={muted} onclick={toggleMute}>
      {#if muted}
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7"><path d="M11 5 6 9H3v6h3l5 4zM16 9l5 6M21 9l-5 6"></path></svg>
      {:else}
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7"><path d="M11 5 6 9H3v6h3l5 4zM15 9a4 4 0 0 1 0 6M18 6a8 8 0 0 1 0 12"></path></svg>
      {/if}
    </button>
  </form>
</div>

<style>
  .voice { position: absolute; inset: 0; z-index: 12; display: flex; flex-direction: column; align-items: center; padding: 14px 22px 18px; box-sizing: border-box; background: var(--window-bg); }
  .top { width: 100%; display: flex; align-items: center; justify-content: space-between; }
  .label { font-size: 13px; font-weight: 600; }
  .close { width: 32px; height: 32px; display: grid; place-items: center; border-radius: 50%; color: var(--text-muted); }
  .close:hover { background: var(--row-hover); color: var(--text); }
  .stage { flex: 1; min-height: 0; width: 100%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 14px; }
  .orb { position: relative; width: 132px; height: 132px; display: grid; place-items: center; border-radius: 50%; background: none; }
  .core { width: 72px; height: 72px; border-radius: 50%; background: radial-gradient(circle at 38% 32%, color-mix(in srgb, var(--text) 88%, white), var(--text)); box-shadow: 0 10px 34px color-mix(in srgb, var(--text) 24%, transparent); transition: transform 0.2s ease-out; }
  .ring { position: absolute; inset: 18px; border-radius: 50%; border: 1px solid color-mix(in srgb, var(--text) 22%, transparent); opacity: 0; }
  .orb.listening .ring { animation: pulse 1.8s ease-out infinite; }
  .orb.listening .ring.two { animation-delay: 0.55s; }
  .orb.thinking .core { animation: breathe 1.2s ease-in-out infinite; }
  .orb.speaking .core { animation: speak 0.55s ease-in-out infinite alternate; }
  .orb:hover .core { transform: scale(1.03); }
  strong { font-size: 13px; font-weight: 550; }
  .partial { max-width: 460px; margin: 0; color: var(--text-muted); font-size: 12px; text-align: center; }
  .issue { max-width: 460px; margin: 0; color: var(--text-muted); font-size: 11px; text-align: center; }
  .conversation { width: min(560px, 100%); max-height: 190px; overflow: auto; display: flex; flex-direction: column; gap: 9px; padding: 4px 2px 10px; scrollbar-width: none; }
  .conversation::-webkit-scrollbar { display: none; }
  .line { display: flex; flex-direction: column; gap: 2px; align-items: flex-start; }
  .line.user { align-items: flex-end; }
  .line span { font-size: 10px; color: var(--text-muted); padding: 0 8px; }
  .line p { max-width: 86%; margin: 0; padding: 7px 10px; border-radius: 12px; background: var(--bubble-in); font-size: 12px; line-height: 1.35; }
  .line.user p { background: var(--bubble-out); }
  .composer { width: min(560px, 100%); display: flex; align-items: center; gap: 8px; }
  input { flex: 1; min-width: 0; height: 36px; box-sizing: border-box; border-radius: 18px; border: 0.5px solid var(--glass-border); background: var(--glass-field); color: var(--text); padding: 0 13px; font: inherit; font-size: 12px; outline: none; }
  input:focus-visible { box-shadow: 0 0 0 2px var(--focus-ring); }
  .send, .mute { width: 36px; height: 36px; flex: none; display: grid; place-items: center; border-radius: 50%; border: 0.5px solid var(--glass-border); background: var(--glass-field); color: var(--text); }
  .send:disabled { opacity: 0.4; }
  @keyframes pulse { 0% { opacity: 0.55; transform: scale(0.86); } 100% { opacity: 0; transform: scale(1.28); } }
  @keyframes breathe { 0%, 100% { transform: scale(1); } 50% { transform: scale(0.93); } }
  @keyframes speak { from { transform: scale(0.97); } to { transform: scale(1.04); } }
  @media (prefers-reduced-motion: reduce) {
    .ring, .core { animation: none !important; }
  }
</style>
