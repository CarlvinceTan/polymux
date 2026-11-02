<script lang="ts">
  import {onDestroy, tick} from 'svelte';
  import Icon from './Icon.svelte';
  import {createDictation} from './dictation';
  import {polymuxApi} from '../../api/polymux';
  import {readableError} from '../errors';

  export let value = '';
  export let field: HTMLTextAreaElement | null = null;
  export let placeholder = 'Message';
  export let hasAttachments = false;
  export let dictationAutoStopSeconds: number | null = 6;
  export let onSend: () => void = () => {};
  export let onkeydown: (event: KeyboardEvent) => void = () => {};
  export let oninput: (event: Event) => void = () => {};
  export let ondictation: () => void = () => {};
  export let oncursor: (event: Event) => void = () => {};
  export let autocompleteId: string | undefined = undefined;
  export let activeDescendant: string | undefined = undefined;
  export let onSendRecording: (file: File) => void = () => {};
  let listening = false;
  let error = '';
  let disposed = false;
  let capture: MediaRecorder | null = null;
  let stream: MediaStream | null = null;
  let recording = false;
  let starting = false;
  let audio: Blob | null = null;
  let preview = '';
  let captureGeneration = 0;
  const dictation = createDictation({
    getText: () => value,
    caret: () => field?.selectionStart ?? value.length,
    setText: (text, caret) => {
      value = text;
      void tick().then(() => {
        if (!field || disposed) return;
        field.setSelectionRange(caret, caret);
        ondictation();
      });
    },
    autoStopSeconds: () => dictationAutoStopSeconds,
    onState: (active, message) => { listening = active; error = message; },
  });

  export function cancelDictation() { dictation.cancel(); }

  export async function startRecording() {
    if (starting || recording || audio) return;
    dictation.cancel();
    error = '';
    starting = true;
    const generation = ++captureGeneration;
    try {
      const permission = await polymuxApi().permissions.request('microphone');
      if (disposed || generation !== captureGeneration) return;
      if (permission !== 'granted') throw new Error('Microphone access is required.');
      const nextStream = await navigator.mediaDevices.getUserMedia({audio: true});
      if (disposed || generation !== captureGeneration) { nextStream.getTracks().forEach(track => track.stop()); return; }
      stream = nextStream;
      const next = new MediaRecorder(stream);
      const chunks: Blob[] = [];
      next.ondataavailable = event => { if (event.data.size) chunks.push(event.data); };
      next.onstop = () => {
        nextStream.getTracks().forEach(track => track.stop());
        if (disposed || generation !== captureGeneration) return;
        capture = null;
        stream = null;
        recording = false;
        audio = new Blob(chunks, {type: next.mimeType || 'audio/webm'});
        if (audio.size) preview = URL.createObjectURL(audio);
        else { audio = null; error = 'No audio was recorded. Try again.'; }
      };
      next.onerror = () => { discardRecording(); error = 'Recording failed. Try again.'; };
      capture = next;
      next.start();
      recording = true;
    } catch (cause) { discardRecording(); error = readableError(cause); }
    finally { if (generation === captureGeneration) starting = false; }
  }

  function discardRecording() {
    captureGeneration += 1;
    if (capture && capture.state !== 'inactive') capture.stop();
    stream?.getTracks().forEach(track => track.stop());
    capture = null;
    stream = null;
    recording = false;
    starting = false;
    audio = null;
    if (preview) URL.revokeObjectURL(preview);
    preview = '';
  }

  function sendRecording() {
    if (!audio?.size) return;
    const extension = audio.type.includes('mp4') ? 'm4a' : audio.type.includes('ogg') ? 'ogg' : 'webm';
    const file = new File([audio], `Voice recording.${extension}`, {type: audio.type});
    onSendRecording(file);
    discardRecording();
  }

  function send() { dictation.cancel(); onSend(); }
  function keydown(event: KeyboardEvent) {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) dictation.cancel();
    onkeydown(event);
  }
  onDestroy(() => { disposed = true; dictation.cancel(); discardRecording(); });
</script>

<div class="message-input">
  {#if recording || starting || audio}
    <button type="button" class="message-input-discard" aria-label="Discard recording" onclick={discardRecording}><Icon name="trash" size={15}/></button>
    {#if preview}<audio controls src={preview} aria-label="Voice recording preview"></audio>
    {:else}<span class="message-input-status" role="status">{starting ? 'Starting recording…' : 'Recording…'}</span>{/if}
    <button type="button" class="message-input-primary" data-tooltip="none" disabled={starting} aria-label={audio ? 'Send recording' : 'Stop recording'} onclick={() => audio ? sendRecording() : capture?.stop()}><Icon name={audio ? 'send' : 'stop'} size={16}/></button>
  {:else}
    <textarea bind:this={field} bind:value rows="1" {placeholder} aria-label={placeholder} aria-autocomplete={autocompleteId ? 'list' : undefined} aria-controls={autocompleteId} aria-activedescendant={activeDescendant} oninput={oninput} onclick={oncursor} onkeyup={oncursor} onkeydown={keydown}></textarea>
    <button type="button" class="message-input-primary" data-tooltip="none" aria-label={listening ? 'Stop dictation' : value.trim() || hasAttachments ? 'Send message' : 'Dictate message'} aria-pressed={listening} onclick={() => listening ? dictation.stop() : value.trim() || hasAttachments ? send() : dictation.toggle()}><Icon name={listening ? 'stop' : value.trim() || hasAttachments ? 'send' : 'mic'} size={16}/></button>
  {/if}
  {#if error}<span class="message-input-error" role="alert">{error}</span>{/if}
</div>

<style>
  .message-input{position:relative;display:flex;align-items:flex-end;gap:4px;flex:1;min-width:0;width:100%}
  textarea{z-index:1;min-width:0;width:0;min-height:32px;max-height:90px;box-sizing:border-box;flex:1;field-sizing:content;overflow-y:auto;resize:none;border:0;padding:7.5px 0;background:transparent;color:var(--neutral-950);font:inherit;font-size:12.5px;line-height:17px;overflow-wrap:anywhere;scrollbar-width:none}textarea::-webkit-scrollbar{display:none}.message-input textarea::placeholder{color:var(--secondary)}textarea:placeholder-shown{white-space:nowrap;text-overflow:ellipsis;overflow:hidden}textarea:focus-visible{outline:0}
  .message-input .message-input-primary{width:32px;height:32px;display:grid;place-items:center;flex:none;border:0;border-radius:50%;padding:0;cursor:pointer;background:var(--neutral-950);color:var(--app-bg)}.message-input .message-input-primary:hover{opacity:.84}.message-input-primary:disabled{opacity:.38;cursor:default}
  .message-input-discard{height:32px;display:grid;place-items:center;border:0;background:transparent;color:var(--secondary);cursor:pointer}.message-input-status{flex:1;align-self:center;font-size:12px;color:var(--secondary)}audio{flex:1;min-width:0;height:32px}.message-input-error{position:absolute;bottom:100%;left:0;max-width:100%;padding:6px;background:var(--app-bg);color:var(--danger-600);font-size:12px}
</style>
