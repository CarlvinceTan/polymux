<script lang="ts">
  import type { Agent } from '../api/contract';
  import { avatarColor, type AvatarDesignLite } from './palette';
  import { restingFrame, type AvatarFrame } from './kit/frame';
  import { FRAME_SIZE } from './kit/math';
  import { shapePaths } from './kit/solid';
  import { characterPaths, drawCharacter, hasCharacter, loadCharacter } from './kit/character';
  import { ThinkingMotion, thinkingDot, thinkingZoom } from './kit/thinking';

  /** One mascot in AvatarKit's 316-unit frame, so the visible face is 200/316 of `size`. */
  let {
    agent,
    size = 52,
    thinking = false,
  }: {
    agent: Agent | undefined;
    size?: number;
    thinking?: boolean;
  } = $props();

  let canvas: HTMLCanvasElement | undefined = $state();
  let dark = $state(document.documentElement.dataset.mode === 'dark');
  let focused = $state(document.hasFocus() && !document.hidden);
  let reduced = $state(matchMedia('(prefers-reduced-motion: reduce)').matches);
  const design = $derived((agent?.avatar ?? undefined) as AvatarDesignLite | undefined);
  const motion = new ThinkingMotion();

  $effect(() => {
    const root = document.documentElement;
    const observer = new MutationObserver(() => { dark = root.dataset.mode === 'dark'; });
    observer.observe(root, { attributes: true, attributeFilter: ['data-mode'] });
    return () => observer.disconnect();
  });

  $effect(() => {
    const media = matchMedia('(prefers-reduced-motion: reduce)');
    const changed = () => { reduced = media.matches; };
    const focus = () => { focused = document.hasFocus() && !document.hidden; };
    media.addEventListener('change', changed);
    window.addEventListener('focus', focus);
    window.addEventListener('blur', focus);
    document.addEventListener('visibilitychange', focus);
    return () => {
      media.removeEventListener('change', changed);
      window.removeEventListener('focus', focus);
      window.removeEventListener('blur', focus);
      document.removeEventListener('visibilitychange', focus);
    };
  });

  let frames = new Map<string, AvatarFrame>();
  function frame(): AvatarFrame {
    if (!frames.has('rest')) frames.set('rest', restingFrame(100));
    return frames.get('rest')!;
  }

  function backing(): string {
    const value = getComputedStyle(document.documentElement).getPropertyValue('--window-bg').trim();
    return value || '#ffffff';
  }

  $effect(() => {
    if (!canvas) return;
    const target = canvas;
    const currentDesign = design;
    const currentDark = dark;
    const currentSize = size;
    const active = thinking && (!currentDesign?.shape || currentDesign.shape === 'cercle');
    const still = reduced || currentDesign?.animates === false;
    const awake = focused;
    const scale = window.devicePixelRatio || 1;
    const context = target.getContext('2d')!;

    function draw() {
      context.setTransform(scale, 0, 0, scale, 0, 0);
      context.clearRect(0, 0, currentSize, currentSize);

      const k = currentSize / FRAME_SIZE;
      context.save();
      context.translate(currentSize / 2, currentSize / 2);
      context.scale(k, k);

      const shape = shapePaths(currentDesign?.shape ?? '');
      const character = characterPaths(currentDesign?.shape ?? '');
      if (character) {
        drawCharacter(context, character);
      } else if (shape) {
        context.fillStyle = avatarColor(currentDesign, currentDark);
        context.fill(shape.body);
        context.fillStyle = backing();
        context.fill(shape.eyes);
      } else {
        const { body, eyes } = frame();
        const amount = motion.amount;
        const core = thinkingDot(1, motion.time, amount, still);
        const zoom = thinkingZoom(currentSize, amount);
        const bodyScale = (1 + (core.radius - 1) * amount) * zoom;
        context.fillStyle = avatarColor(currentDesign, currentDark);
        context.save();
        context.translate(0, core.y * 100 * amount * zoom);
        context.scale(bodyScale, bodyScale);
        context.globalAlpha = 1 - (1 - core.opacity) * amount;
        context.beginPath();
        context.moveTo(body[0].x, body[0].y);
        for (let i = 1; i < body.length; i++) context.lineTo(body[i].x, body[i].y);
        context.closePath();
        context.fill();
        context.restore();
        context.fillStyle = backing();
        if (amount < .5) for (const eye of eyes) {
          if (eye.length < 3) continue;
          context.save();
          context.translate(0, core.y * 100 * amount * zoom);
          context.scale(bodyScale, bodyScale);
          context.beginPath();
          context.moveTo(eye[0].x, eye[0].y);
          for (let i = 1; i < eye.length; i++) context.lineTo(eye[i].x, eye[i].y);
          context.closePath();
          context.fill();
          context.restore();
        }
        for (const index of [0, 2]) {
          const progress = Math.max(0, Math.min(1, (amount - (index === 0 ? 0 : .12)) / (index === 0 ? 1 : .88)));
          if (progress <= .004) continue;
          const visible = 1 - (1 - progress) ** 3;
          const spread = 1 + 2.70158 * (progress - 1) ** 3 + 1.70158 * (progress - 1) ** 2;
          const dot = thinkingDot(index, motion.time, amount, still);
          context.save();
          context.fillStyle = avatarColor(currentDesign, currentDark);
          context.globalAlpha = dot.opacity * visible;
          context.beginPath();
          context.arc(dot.x * spread * 100 * zoom, dot.y * 100 * zoom, dot.radius * visible * 1.02 * 100 * zoom, 0, 2 * Math.PI);
          context.fill();
          context.restore();
        }
      }
      context.restore();
    }

    let request = 0;
    let cancelled = false;
    let previous: number | undefined;
    const tick = (now: number) => {
      const moving = motion.step(active, previous === undefined ? 0 : (now - previous) / 1000, still);
      previous = now;
      draw();
      if (moving && !still && awake) request = requestAnimationFrame(tick);
    };
    const start = () => {
      if (cancelled) return;
      target.width = currentSize * scale;
      target.height = currentSize * scale;
      motion.step(active, 0, still);
      draw();
      if (!still && awake && (active || motion.amount > 0)) request = requestAnimationFrame(tick);
    };
    const id = currentDesign?.shape ?? '';
    if (hasCharacter(id) && !characterPaths(id)) void loadCharacter(id).then(start, () => {});
    else start();
    return () => { cancelled = true; cancelAnimationFrame(request); };
  });
</script>

<canvas bind:this={canvas} style:width="{size}px" style:height="{size}px" aria-hidden="true"></canvas>
