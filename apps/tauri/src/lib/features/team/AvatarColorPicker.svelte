<script lang="ts">
  import {onMount} from 'svelte';
  import Icon from '../../shared/components/Icon.svelte';
  import {MENU_EDGE_MARGIN, clampToMenuEdge} from '../../shared/layout/menuPlacement';

  export let value: string;
  export let onChange: (color: string) => void;

  let trigger: HTMLButtonElement;
  let panel: HTMLDivElement;
  let hexInput: HTMLInputElement;
  let open = false;
  let hue = 0;
  let saturation = 0;
  let brightness = 0;
  let hex = value;
  let left = 0;
  let top = 0;

  onMount(() => {
    const reposition = () => { if (open) place(); };
    window.addEventListener('scroll', reposition, true);
    return () => window.removeEventListener('scroll', reposition, true);
  });

  function readColor(color: string): void {
    hex = color;
    const [r, g, b] = [1, 3, 5].map((start) => parseInt(color.slice(start, start + 2), 16) / 255);
    const max = Math.max(r, g, b), min = Math.min(r, g, b), delta = max - min;
    brightness = max * 100;
    saturation = max ? delta / max * 100 : 0;
    if (delta) hue = ((max === r ? (g - b) / delta : max === g ? (b - r) / delta + 2 : (r - g) / delta + 4) * 60 + 360) % 360;
  }

  function publish(): void {
    const s = saturation / 100, v = brightness / 100;
    const channel = (n: number) => {
      const k = (n + hue / 60) % 6;
      return Math.round((v - v * s * Math.max(0, Math.min(k, 4 - k, 1))) * 255).toString(16).padStart(2, '0');
    };
    hex = `#${channel(5)}${channel(3)}${channel(1)}`;
    onChange(hex);
  }

  function place(): void {
    const anchor = trigger.getBoundingClientRect();
    const rect = panel.getBoundingClientRect();
    left = clampToMenuEdge(anchor.left + anchor.width / 2 - rect.width / 2, rect.width, window.innerWidth);
    const below = anchor.bottom + 8;
    top = clampToMenuEdge(
      below + rect.height <= window.innerHeight - MENU_EDGE_MARGIN ? below : anchor.top - rect.height - 8,
      rect.height,
      window.innerHeight,
    );
  }

  function toggle(): void {
    if (open) { panel.hidePopover(); return; }
    readColor(value);
    panel.showPopover();
    open = true;
    place();
    hexInput.focus({preventScroll: true});
  }

  function close(): void {
    panel.hidePopover();
    open = false;
    trigger.focus({preventScroll: true});
  }

  function choose(event: PointerEvent): void {
    const field = event.currentTarget as HTMLElement;
    if (event.type === 'pointerdown') field.setPointerCapture(event.pointerId);
    else if (!field.hasPointerCapture(event.pointerId)) return;
    const rect = field.getBoundingClientRect();
    saturation = Math.max(0, Math.min(100, (event.clientX - rect.left) / rect.width * 100));
    brightness = Math.max(0, Math.min(100, (1 - (event.clientY - rect.top) / rect.height) * 100));
    publish();
  }

  function fieldKey(event: KeyboardEvent): void {
    const step = event.shiftKey ? 10 : 1;
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return;
    event.preventDefault();
    saturation = Math.max(0, Math.min(100, saturation + (event.key === 'ArrowRight' ? step : event.key === 'ArrowLeft' ? -step : 0)));
    brightness = Math.max(0, Math.min(100, brightness + (event.key === 'ArrowUp' ? step : event.key === 'ArrowDown' ? -step : 0)));
    publish();
  }
</script>

<svelte:window onresize={() => open && place()}/>

<button bind:this={trigger} class="custom-color" type="button" aria-label="Custom avatar colour" aria-haspopup="dialog" aria-expanded={open} data-tooltip="none" onclick={toggle}>
  <Icon name="plus" size={13}/>
</button>
<div bind:this={panel} popover="auto" class="polymux-dropdown-menu avatar-color-picker" role="dialog" aria-label="Custom avatar colour" style:left={`${left}px`} style:top={`${top}px`}
  ontoggle={(event) => open = event.newState === 'open'}
  onkeydown={(event) => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); } }} tabindex="-1">
  <div class="color-field" role="slider" tabindex="0" aria-label="Saturation and brightness" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(saturation)} aria-valuetext={`${Math.round(saturation)}% saturation, ${Math.round(brightness)}% brightness. Left and right adjust saturation; up and down adjust brightness.`}
    style:--hue={`${hue}`} onpointerdown={choose} onpointermove={choose} onkeydown={fieldKey}>
    <span class="color-cursor" style:left={`${saturation}%`} style:top={`${100 - brightness}%`}></span>
  </div>
  <input class="hue" type="range" min="0" max="360" step="1" aria-label="Hue" value={hue} oninput={(event) => { hue = Number(event.currentTarget.value); publish(); }}/>
  <div class="color-value">
    <span class="swatch" style:background={hex}></span>
    <label>Hex<input bind:this={hexInput} aria-label="Hex colour" spellcheck="false" maxlength="7" value={hex}
      oninput={(event) => {
        const text = event.currentTarget.value;
        const color = text.startsWith('#') ? text : `#${text}`;
        if (/^#[0-9a-f]{6}$/i.test(color)) { readColor(color); onChange(color); }
      }} onblur={() => hexInput.value = hex}/></label>
    <button type="button" class="done" onclick={close}>Done</button>
  </div>
</div>

<style>
  .custom-color { display:grid; place-items:center; width:24px; height:24px; padding:0; border:1px dashed var(--neutral-400); border-radius:50%; background:transparent; color:var(--neutral-600); cursor:pointer; }
  .custom-color:hover { color:var(--neutral-900); }
  .avatar-color-picker { position:fixed; inset:auto; margin:0; width:252px; max-width:calc(100vw - 16px); max-height:calc(100vh - 16px); overflow:auto; padding:12px; border:0; color:var(--neutral-900); font:inherit; }
  .color-field { position:relative; height:148px; overflow:hidden; border-radius:8px; background:linear-gradient(to top,#000,transparent),linear-gradient(to right,#fff,hsl(var(--hue) 100% 50%)); touch-action:none; cursor:crosshair; }
  .color-cursor { position:absolute; width:12px; height:12px; box-sizing:border-box; border:2px solid white; border-radius:50%; box-shadow:0 0 0 1px #0006; transform:translate(-50%,-50%); pointer-events:none; }
  .hue { appearance:none; display:block; width:100%; height:12px; margin:16px 0; border:0; border-radius:6px; background:linear-gradient(to right,#f00,#ff0,#0f0,#0ff,#00f,#f0f,#f00); cursor:pointer; }
  .hue::-webkit-slider-thumb { appearance:none; width:17px; height:17px; border:2px solid white; border-radius:50%; background:transparent; box-shadow:0 1px 4px #0006; }
  .color-value { display:flex; align-items:center; gap:10px; }
  .swatch { flex:none; width:24px; height:24px; border-radius:7px; box-shadow:inset 0 0 0 1px #8884; }
  label { display:flex; align-items:center; gap:7px; min-width:0; color:var(--neutral-600); font-size:11px; }
  label input { width:76px; min-width:0; box-sizing:border-box; height:30px; padding:0 7px; border:1px solid var(--neutral-200); border-radius:7px; background:var(--app-bg); color:var(--neutral-900); font:inherit; font-variant-numeric:tabular-nums; }
  .done { margin-left:auto; padding:6px 0; border:0; background:transparent; color:var(--neutral-700); font:inherit; font-size:12px; cursor:pointer; }
  .done:hover { color:var(--neutral-950); }
  :is(button,input,.color-field):focus-visible { outline:2px solid var(--neutral-600); outline-offset:3px; }
</style>
