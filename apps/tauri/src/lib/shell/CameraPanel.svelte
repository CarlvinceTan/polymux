<script lang="ts">
  import { onMount } from 'svelte';

  let { onClose, onCapture }: { onClose: () => void; onCapture: (file: File) => void } = $props();
  let video: HTMLVideoElement | undefined = $state();
  let ready = $state(false);
  let capturing = $state(false);
  let issue = $state('');
  let closed = false;

  onMount(() => {
    let disposed = false;
    let stream: MediaStream | undefined;
    void (async () => {
      try {
        if (!navigator.mediaDevices?.getUserMedia) throw new Error('unavailable');
        const next = await navigator.mediaDevices.getUserMedia({ video: true, audio: false });
        if (disposed) { next.getTracks().forEach((track) => track.stop()); return; }
        stream = next;
        if (video) {
          video.srcObject = stream;
          await video.play();
          if (!disposed) updateReady();
        }
      } catch (error) {
        if (!disposed) issue = error instanceof DOMException && error.name === 'NotAllowedError'
          ? 'Camera access is off.' : 'Camera unavailable.';
      }
    })();
    return () => { disposed = true; closed = true; stream?.getTracks().forEach((track) => track.stop()); };
  });

  function updateReady() {
    ready = !!video && video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && video.videoWidth > 0 && video.videoHeight > 0;
  }

  function capture() {
    if (!video || !ready || capturing) return;
    const canvas = document.createElement('canvas');
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    const context = canvas.getContext('2d');
    if (!context || !canvas.width || !canvas.height) { issue = 'Couldn’t take the photo.'; return; }
    context.drawImage(video, 0, 0);
    capturing = true;
    canvas.toBlob((blob) => {
      if (closed) return;
      capturing = false;
      if (blob) onCapture(new File([blob], 'Photo.jpg', { type: 'image/jpeg' }));
      else issue = 'Couldn’t take the photo.';
    }, 'image/jpeg', 0.9);
  }
</script>

<div class="shade">
  <div class="panel" role="dialog" aria-modal="true" aria-label="Camera" tabindex="-1" onkeydown={(event) => { if (event.key === 'Escape') onClose(); }}>
    <div class="header">Camera<button class="close glass" aria-label="Close camera" onclick={onClose}>×</button></div>
    <div class="content">
      {#if issue}<div class="issue">{issue}</div>{:else}<video bind:this={video} autoplay playsinline muted aria-label="Camera preview" onloadeddata={updateReady} onresize={updateReady}></video>{#if !ready}<div class="loading">Opening camera…</div>{/if}{/if}
    </div>
    <div class="actions"><button class="action glass" onclick={onClose}>Cancel</button><button class="action glass" disabled={!ready || capturing || !!issue} onclick={capture}>Take photo</button></div>
  </div>
</div>

<style>
  .shade { position: fixed; inset: 0; z-index: 250; display: grid; place-items: center; background: rgba(0, 0, 0, .25); }
  .panel { width: min(620px, calc(100vw - 48px)); height: min(600px, calc(100vh - 48px)); border-radius: var(--radius-panel); background: var(--window-bg); display: flex; flex-direction: column; overflow: hidden; }
  .header { position: relative; height: 76px; display: grid; place-items: center; flex: none; font-weight: 600; }
  .close { position: absolute; right: 16px; top: 20px; width: 36px; height: 36px; border-radius: 50%; font-size: 19px; }
  .content { position: relative; flex: 1; min-height: 0; display: grid; place-items: center; padding: 16px; }
  video { display: block; width: 100%; max-height: 100%; aspect-ratio: 4 / 3; object-fit: cover; border-radius: var(--radius-card); }
  .loading { position: absolute; color: var(--text-muted); }
  .issue { color: var(--text-muted); }
  .actions { display: flex; justify-content: flex-end; gap: 8px; padding: 16px; }
  .action { min-height: 32px; padding: 0 14px; border-radius: 16px; }
  .action:disabled { opacity: .45; }
</style>
