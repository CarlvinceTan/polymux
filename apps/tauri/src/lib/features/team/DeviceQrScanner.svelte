<script lang="ts">
  import {onMount, onDestroy} from 'svelte';
  import jsQR from 'jsqr';
  import {parseTeamHostSetupCode} from '@polymux/protocol';
  export let onCode: (endpoint: string, code: string) => void;
  let video: HTMLVideoElement;
  let stream: MediaStream | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let alive = true;
  let error = '';
  onMount(() => {
    void navigator.mediaDevices.getUserMedia({video: {facingMode: 'environment'}, audio: false}).then(async result => {
      if (!alive) { result.getTracks().forEach(track => track.stop()); return; }
      stream = result; video.srcObject = result; await video.play(); read();
    }).catch(() => error = 'Camera unavailable. Enter the code or choose a QR image.');
  });
  onDestroy(() => { alive = false; clearTimeout(timer); stream?.getTracks().forEach(track => track.stop()); });
  function decode(source: CanvasImageSource, width: number, height: number): boolean {
    const canvas = document.createElement('canvas');
    const scale = Math.min(1, 960 / Math.max(width, height));
    canvas.width = Math.round(width * scale); canvas.height = Math.round(height * scale);
    const context = canvas.getContext('2d', {willReadFrequently: true});
    if (!context || !width || !height) return false;
    context.drawImage(source, 0, 0, canvas.width, canvas.height);
    const result = jsQR(context.getImageData(0, 0, canvas.width, canvas.height).data, canvas.width, canvas.height);
    if (!result) return false;
    const setup = parseTeamHostSetupCode(result.data);
    if (!setup) { error = 'This is not a Polymux device QR code.'; return false; }
    onCode(setup.endpoint, setup.code); return true;
  }
  function read(): void {
    if (!alive) return;
    if (video.readyState >= 2 && decode(video, video.videoWidth, video.videoHeight)) return;
    timer = setTimeout(read, 200);
  }
  async function image(event: Event): Promise<void> {
    const file = (event.currentTarget as HTMLInputElement).files?.[0];
    if (!file) return;
    try {
      const bitmap = await createImageBitmap(file);
      try { if (alive && !decode(bitmap, bitmap.width, bitmap.height)) error = 'No Polymux QR code found in this image.'; }
      finally { bitmap.close(); }
    } catch { error = 'Couldn’t read this image.'; }
  }
</script>
<video bind:this={video} muted playsinline aria-label="Scan a device QR code"></video>
{#if error}<small role="status">{error}</small>{/if}
<label class="qr-image">Choose QR image<input type="file" accept="image/*" onchange={(event) => void image(event)}/></label>
<style>video{width:100%;max-height:150px;border-radius:8px;object-fit:cover}.qr-image{position:relative;font-size:12px;color:var(--neutral-700);cursor:pointer}.qr-image input{position:absolute;inset:0;opacity:0;width:100%;cursor:pointer}small{color:var(--neutral-600);font-size:11px}</style>
