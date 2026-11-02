export type RealtimeVoiceState = 'connecting' | 'listening' | 'thinking' | 'speaking' | 'error' | 'closed'

/** Per-band audio energy, used to let different frequencies deform the orb. */
export interface AudioBands {
  low: number
  mid: number
  high: number
}

/**
 * The orb is drawn on a canvas rather than composed from clipped CSS layers so
 * its fluid lobes can spill across the whole surface and react to live audio.
 * This is the 2D fallback; its brand palette desaturates toward grey by state.
 */

export interface OrbStateVisual {
  /** Resting radius as a fraction of the available half-size. */
  scale: number
  /** How strongly audio deforms the silhouette. */
  reactivity: number
  /** Idle breathing speed multiplier. */
  speed: number
  /** Outer aura strength. */
  glow: number
  saturation: number
}

export interface OrbStateFlags {
  muted?: boolean
  paused?: boolean
}

export function orbStateVisual(state: RealtimeVoiceState, flags: OrbStateFlags = {}): OrbStateVisual {
  const { muted = false, paused = false } = flags
  if (paused) return { scale: 0.82, reactivity: 0, speed: 0.15, glow: 0.28, saturation: 0.5 }
  if (muted) return { scale: 0.86, reactivity: 0.08, speed: 0.4, glow: 0.3, saturation: 0.35 }
  switch (state) {
    case 'connecting':
      return { scale: 0.84, reactivity: 0.05, speed: 0.75, glow: 0.42, saturation: 0.8 }
    case 'listening':
      return { scale: 0.9, reactivity: 1, speed: 0.85, glow: 0.66, saturation: 1 }
    case 'thinking':
      return { scale: 0.87, reactivity: 0.18, speed: 1.9, glow: 0.58, saturation: 0.95 }
    case 'speaking':
      return { scale: 0.94, reactivity: 1.25, speed: 1.35, glow: 0.92, saturation: 1 }
    case 'error':
      return { scale: 0.8, reactivity: 0, speed: 0.2, glow: 0.2, saturation: 0.2 }
    case 'closed':
      return { scale: 0.86, reactivity: 0.4, speed: 0.6, glow: 0.4, saturation: 0.85 }
    default:
      state satisfies never
      return { scale: 0.86, reactivity: 0.4, speed: 0.6, glow: 0.4, saturation: 0.85 }
  }
}

/**
 * Smooth deterministic displacement: summed sines at incommensurate
 * frequencies, which reads as organic motion without a noise dependency.
 */
export function blobRadius(theta: number, time: number, amplitude: number): number {
  const wobble =
    Math.sin(theta * 3 + time * 0.9) * 0.5 +
    Math.sin(theta * 5 - time * 0.61) * 0.28 +
    Math.sin(theta * 2 + time * 1.37) * 0.34 +
    Math.sin(theta * 7 + time * 0.44) * 0.14
  return 1 + wobble * amplitude
}

/**
 * Eases one visual toward another. A state change otherwise steps scale, speed,
 * glow, reactivity and saturation all on the same frame, so a reply beginning
 * reads as a cut rather than as a transition.
 */
export function approachVisual(current: OrbStateVisual, target: OrbStateVisual, rate: number): OrbStateVisual {
  const step = (from: number, to: number) => from + (to - from) * rate
  return {
    scale: step(current.scale, target.scale),
    reactivity: step(current.reactivity, target.reactivity),
    speed: step(current.speed, target.speed),
    glow: step(current.glow, target.glow),
    saturation: step(current.saturation, target.saturation),
  }
}

/** Smooths jittery frame-to-frame audio so the orb swells instead of flickering. */
export function smoothLevel(previous: number, next: number, attack = 0.35, release = 0.08): number {
  const rate = next > previous ? attack : release
  return previous + (next - previous) * rate
}

export interface VoiceOrbHandle {
  setState: (state: RealtimeVoiceState, flags?: OrbStateFlags) => void
  setLevel: (level: number, bands?: AudioBands) => void
  destroy: () => void
}

function mix(from: number[], to: number[], amount: number, alpha = 1): string {
  const channel = (index: number) => Math.round(from[index] + (to[index] - from[index]) * amount)
  return `rgba(${channel(0)},${channel(1)},${channel(2)},${alpha})`
}

const LIGHT_RGB = [111, 214, 255]
const BLUE_RGB = [35, 132, 203]
const NAVY_RGB = [22, 48, 79]
const INK_RGB = [8, 21, 37]
const GREY_RGB = [126, 134, 142]

export function startVoiceOrb(canvas: HTMLCanvasElement, initialState: RealtimeVoiceState = 'connecting'): VoiceOrbHandle {
  const context = canvas.getContext?.('2d') ?? null
  // Without a 2D context there is nothing to animate; the handle stays inert
  // rather than spinning an empty render loop.
  if (!context) return { setState() {}, setLevel() {}, destroy() {} }
  const reduceMotion = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
  let state = initialState
  let muted = false
  let paused = false
  let level = 0
  let smoothed = 0
  let frame = 0
  let phase = 0
  let lastNow = 0
  let visual = orbStateVisual(initialState)

  const resize = () => {
    const ratio = Math.min(window.devicePixelRatio || 1, 2)
    const rect = canvas.getBoundingClientRect()
    const size = Math.max(1, Math.min(rect.width, rect.height))
    canvas.width = Math.round(size * ratio)
    canvas.height = Math.round(size * ratio)
  }

  const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(resize)
  observer?.observe(canvas)
  resize()

  const draw = (now: number) => {
    const target = orbStateVisual(state, { muted, paused })
    visual = reduceMotion ? target : approachVisual(visual, target, 0.055)
    // Integrated, for the same reason as the 3D renderer: drift computed as
    // elapsed x speed rescales the whole timeline when the speed changes.
    const delta = lastNow ? Math.min(0.05, (now - lastNow) / 1000) : 0
    lastNow = now
    if (!reduceMotion) phase += delta * visual.speed
    const time = reduceMotion ? 0 : phase
    smoothed = reduceMotion ? 0 : smoothLevel(smoothed, level)
    const energy = Math.min(1, smoothed * visual.reactivity)

    const width = canvas.width
    const height = canvas.height
    const centreX = width / 2
    const centreY = height / 2
    const half = Math.min(width, height) / 2
    // The body is kept to roughly half the canvas so its halo has room to fall
    // off inside the bitmap; otherwise the glow is clipped into a square.
    const base = half * 0.56 * visual.scale * (1 + energy * 0.07)
    const amplitude = (0.04 + energy * 0.1) * (reduceMotion ? 0.2 : 1)
    const drift = time

    context.clearRect(0, 0, width, height)

    // Aura: a soft coloured halo that breathes with the audio.
    const auraRadius = Math.min(half, base * (1.45 + energy * 0.22))
    const aura = context.createRadialGradient(centreX, centreY, base * 0.6, centreX, centreY, auraRadius)
    aura.addColorStop(0, mix(GREY_RGB, BLUE_RGB, visual.saturation, 0.42 * visual.glow))
    aura.addColorStop(0.55, mix(GREY_RGB, BLUE_RGB, visual.saturation, 0.16 * visual.glow))
    aura.addColorStop(1, mix(GREY_RGB, BLUE_RGB, visual.saturation, 0))
    context.fillStyle = aura
    context.beginPath()
    context.arc(centreX, centreY, auraRadius, 0, Math.PI * 2)
    context.fill()

    // Silhouette: a closed morphing path, not a clipped circle, so the shape
    // itself deforms with speech.
    context.beginPath()
    const steps = 180
    for (let index = 0; index <= steps; index += 1) {
      const theta = (index / steps) * Math.PI * 2
      const radius = base * blobRadius(theta, drift, amplitude)
      const x = centreX + Math.cos(theta) * radius
      const y = centreY + Math.sin(theta) * radius
      if (index === 0) context.moveTo(x, y)
      else context.lineTo(x, y)
    }
    context.closePath()
    context.save()
    context.clip()

    const body = context.createRadialGradient(
      centreX - base * 0.32, centreY - base * 0.38, base * 0.05,
      centreX, centreY, base * 1.25,
    )
    body.addColorStop(0, mix(GREY_RGB, LIGHT_RGB, visual.saturation))
    body.addColorStop(0.34, mix(GREY_RGB, BLUE_RGB, visual.saturation))
    body.addColorStop(0.72, mix(GREY_RGB, NAVY_RGB, visual.saturation))
    body.addColorStop(1, mix(GREY_RGB, INK_RGB, visual.saturation))
    context.fillStyle = body
    context.fillRect(centreX - base * 2, centreY - base * 2, base * 4, base * 4)

    // Drifting lobes read as fluid moving inside the orb.
    context.globalCompositeOperation = 'lighter'
    const lobes = [
      { colour: LIGHT_RGB, radius: 0.58, speed: 0.42, phase: 0, alpha: 0.72 },
      { colour: BLUE_RGB, radius: 0.74, speed: -0.31, phase: 2.1, alpha: 0.6 },
      { colour: LIGHT_RGB, radius: 0.42, speed: 0.57, phase: 4.2, alpha: 0.5 },
    ]
    for (const lobe of lobes) {
      const angle = drift * lobe.speed + lobe.phase
      const distance = base * (0.34 + energy * 0.2)
      const x = centreX + Math.cos(angle) * distance
      const y = centreY + Math.sin(angle * 1.23) * distance
      const size = base * lobe.radius * (1 + energy * 0.22)
      const gradient = context.createRadialGradient(x, y, 0, x, y, size)
      const alpha = lobe.alpha * (0.55 + energy * 0.5) * visual.saturation
      gradient.addColorStop(0, mix(GREY_RGB, lobe.colour, visual.saturation, alpha))
      gradient.addColorStop(1, mix(GREY_RGB, lobe.colour, visual.saturation, 0))
      context.fillStyle = gradient
      context.beginPath()
      context.arc(x, y, size, 0, Math.PI * 2)
      context.fill()
    }

    // A deep pool keeps the lower body anchored in brand navy.
    context.globalCompositeOperation = 'multiply'
    const depthX = centreX + base * 0.24
    const depthY = centreY + base * 0.42
    const depth = context.createRadialGradient(depthX, depthY, 0, depthX, depthY, base * 1.05)
    depth.addColorStop(0, `rgba(${NAVY_RGB.join(',')},${0.62 * visual.saturation})`)
    depth.addColorStop(1, 'rgba(255,255,255,0)')
    context.fillStyle = depth
    context.beginPath()
    context.arc(depthX, depthY, base * 1.05, 0, Math.PI * 2)
    context.fill()

    // Specular sheen.
    context.globalCompositeOperation = 'screen'
    const sheenX = centreX - base * 0.34
    const sheenY = centreY - base * 0.44
    const sheen = context.createRadialGradient(sheenX, sheenY, 0, sheenX, sheenY, base * 0.6)
    sheen.addColorStop(0, `rgba(255,255,255,${0.42 * visual.saturation + 0.06})`)
    sheen.addColorStop(1, 'rgba(255,255,255,0)')
    context.fillStyle = sheen
    context.beginPath()
    context.arc(sheenX, sheenY, base * 0.6, 0, Math.PI * 2)
    context.fill()

    context.restore()
    context.globalCompositeOperation = 'source-over'

    if (!reduceMotion) frame = requestAnimationFrame(draw)
  }

  frame = requestAnimationFrame(draw)

  return {
    setState(nextState, flags = {}) {
      state = nextState
      muted = flags.muted ?? false
      paused = flags.paused ?? false
      if (reduceMotion) frame = requestAnimationFrame(draw)
    },
    setLevel(next) {
      level = Number.isFinite(next) ? Math.max(0, Math.min(1, next)) : 0
    },
    destroy() {
      cancelAnimationFrame(frame)
      observer?.disconnect()
    },
  }
}
