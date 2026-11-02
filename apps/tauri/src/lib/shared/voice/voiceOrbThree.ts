import { PerspectiveCamera } from 'three/src/cameras/PerspectiveCamera.js'
import { NormalBlending } from 'three/src/constants.js'
import { BufferAttribute } from 'three/src/core/BufferAttribute.js'
import { BufferGeometry } from 'three/src/core/BufferGeometry.js'
import type { Material } from 'three/src/materials/Material.js'
import { ShaderMaterial } from 'three/src/materials/ShaderMaterial.js'
import { Color } from 'three/src/math/Color.js'
import { Points } from 'three/src/objects/Points.js'
import { WebGLRenderer } from 'three/src/renderers/WebGLRenderer.js'
import { Scene } from 'three/src/scenes/Scene.js'
import {
  approachVisual,
  orbStateVisual,
  smoothLevel,
  type AudioBands,
  type RealtimeVoiceState,
  type VoiceOrbHandle,
} from './voiceOrb'

/**
 * A 3D audio visualiser: a sphere of individual points pushed outward by 3D
 * simplex noise, so the form reads as a particle cloud rather than a solid
 * shell. The noise is rectified to push outward only, which concentrates energy
 * into plumes erupting from an otherwise calm sphere; bass drives how far they
 * reach and treble the fine shimmer between them.
 *
 * The orb keeps the surface it sits on, which is near-white, so the particles
 * behave like ink in water rather than like light: they are alpha-blended, and
 * overlapping dots deepen toward full pigment while sparse ones stay pale. That
 * rules out the dark end of the palette for anything but the densest cores —
 * navy and ink composited over white at partial coverage read as grey, not as
 * blue — so brand blue carries the energy and the dispersing tips fade out.
 */
const FLARE_LIGHT = new Color('#6fd6ff')
const FLARE_BLUE = new Color('#2384cb')
/**
 * The deep end of the ramp. Kept a legible blue rather than the near-black it
 * started as: these points are alpha-blended, so wherever the cloud is dense
 * the darkest colour composites darker still, and an ink that is almost black
 * to begin with lands as black specks scattered through the form.
 */
const FLARE_INK = new Color('#1e4778')
/**
 * Two colours from outside the brand blues. Between navy and the light blue
 * there is only about 17 degrees of hue, which is far too little to read as a
 * gradient at all — every mixture of them looks like one blue lit unevenly.
 * Electric violet carries most of the hue travel, while magenta remains a
 * restrained accent. That keeps the form recognisably blue without flattening
 * it into one hue shown at several brightnesses.
 */
const FLARE_VIOLET = new Color('#7b3ff2')
const FLARE_MAGENTA = new Color('#ee3ba4')

const POINT_COUNT = 14_000

/**
 * Fibonacci lattice: points spaced evenly over the unit sphere with no poles or
 * seams, which a subdivided polyhedron cannot give and which shows as banding
 * once the surface is drawn as discrete dots.
 */
export function spherePoints(count: number): Float32Array {
  const positions = new Float32Array(count * 3)
  const increment = Math.PI * (3 - Math.sqrt(5))
  for (let index = 0; index < count; index += 1) {
    const y = count === 1 ? 0 : 1 - (index / (count - 1)) * 2
    const ring = Math.sqrt(Math.max(0, 1 - y * y))
    const theta = increment * index
    positions[index * 3] = Math.cos(theta) * ring
    positions[index * 3 + 1] = y
    positions[index * 3 + 2] = Math.sin(theta) * ring
  }
  return positions
}

/** Stable per-point jitter so dot size and opacity vary without a random source. */
export function pointSeeds(count: number): Float32Array {
  const seeds = new Float32Array(count)
  for (let index = 0; index < count; index += 1) {
    const noise = Math.sin(index * 12.9898) * 43758.5453
    seeds[index] = noise - Math.floor(noise)
  }
  return seeds
}

// Ashima's simplex noise, the standard GLSL implementation used for this effect.
const SIMPLEX_3D = `
vec3 mod289(vec3 x){return x-floor(x*(1.0/289.0))*289.0;}
vec4 mod289(vec4 x){return x-floor(x*(1.0/289.0))*289.0;}
vec4 permute(vec4 x){return mod289(((x*34.0)+1.0)*x);}
vec4 taylorInvSqrt(vec4 r){return 1.79284291400159-0.85373472095314*r;}
float snoise(vec3 v){
  const vec2 C=vec2(1.0/6.0,1.0/3.0); const vec4 D=vec4(0.0,0.5,1.0,2.0);
  vec3 i=floor(v+dot(v,C.yyy)); vec3 x0=v-i+dot(i,C.xxx);
  vec3 g=step(x0.yzx,x0.xyz); vec3 l=1.0-g; vec3 i1=min(g.xyz,l.zxy); vec3 i2=max(g.xyz,l.zxy);
  vec3 x1=x0-i1+C.xxx; vec3 x2=x0-i2+C.yyy; vec3 x3=x0-D.yyy;
  i=mod289(i);
  vec4 p=permute(permute(permute(i.z+vec4(0.0,i1.z,i2.z,1.0))+i.y+vec4(0.0,i1.y,i2.y,1.0))+i.x+vec4(0.0,i1.x,i2.x,1.0));
  float n_=0.142857142857; vec3 ns=n_*D.wyz-D.xzx;
  vec4 j=p-49.0*floor(p*ns.z*ns.z);
  vec4 x_=floor(j*ns.z); vec4 y_=floor(j-7.0*x_);
  vec4 x=x_*ns.x+ns.yyyy; vec4 y=y_*ns.x+ns.yyyy; vec4 h=1.0-abs(x)-abs(y);
  vec4 b0=vec4(x.xy,y.xy); vec4 b1=vec4(x.zw,y.zw);
  vec4 s0=floor(b0)*2.0+1.0; vec4 s1=floor(b1)*2.0+1.0; vec4 sh=-step(h,vec4(0.0));
  vec4 a0=b0.xzyw+s0.xzyw*sh.xxyy; vec4 a1=b1.xzyw+s1.xzyw*sh.zzww;
  vec3 p0=vec3(a0.xy,h.x); vec3 p1=vec3(a0.zw,h.y); vec3 p2=vec3(a1.xy,h.z); vec3 p3=vec3(a1.zw,h.w);
  vec4 norm=taylorInvSqrt(vec4(dot(p0,p0),dot(p1,p1),dot(p2,p2),dot(p3,p3)));
  p0*=norm.x; p1*=norm.y; p2*=norm.z; p3*=norm.w;
  vec4 m=max(0.6-vec4(dot(x0,x0),dot(x1,x1),dot(x2,x2),dot(x3,x3)),0.0); m=m*m;
  return 42.0*dot(m*m,vec4(dot(p0,x0),dot(p1,x1),dot(p2,x2),dot(p3,x3)));
}`

const VERTEX = `
uniform float uTime;
uniform float uAmplitude;
uniform float uFrequency;
uniform float uBass;
uniform float uTreble;
uniform float uSize;
uniform float uCameraZ;
attribute float aSeed;
varying float vEnergy;
varying float vSeed;
varying float vHeight;
varying float vDepth;
varying float vFlow;
varying vec3 vNormal;
${SIMPLEX_3D}
void main() {
  // Two octaves at unrelated speeds so the plumes drift and recombine instead
  // of pulsing in a fixed pattern.
  float swell = snoise(position * uFrequency + uTime * 0.32);
  float fold = snoise(position * (uFrequency * 1.9) - uTime * 0.21);
  float ripple = snoise(position * (uFrequency * 4.1) + uTime * 0.55);

  // Rectified: the noise only ever pushes outward, so the sphere stays intact
  // and the energy erupts from it rather than denting it.
  float crest = max(swell + fold * 0.45, 0.0) * (0.62 + uBass * 0.95);
  float detail = ripple * 0.12 * (0.3 + uTreble * 1.1);
  float energy = clamp(crest * 0.85, 0.0, 1.0);

  // Particles inside a plume scatter along its length rather than riding a
  // smooth displaced surface, which is what makes it read as a cloud.
  float displacement = crest * (0.72 + aSeed * 0.52) + detail;

  vEnergy = energy;
  vSeed = aSeed;

  // Colour follows a band wound around the axis rather than stacked in flat
  // layers, so it sweeps around the form the way pigment does in water. The
  // slow noise field breaks the regularity: without it the helix reads as a
  // barber pole. Both are in object space, so the pattern turns with the cloud.
  float azimuth = atan(position.z, position.x);
  // Taken through sin() rather than used directly: atan jumps from +PI to -PI,
  // and feeding that straight into the band draws a hard seam down one side.
  // Adding the height inside the sine is what twists the band into a helix.
  float wind = sin(azimuth + position.y * 2.6);
  float helix = position.y * 1.2 + wind * 0.55 + snoise(position * 0.8 + uTime * 0.09) * 0.85;
  vFlow = clamp(helix * 0.42 + 0.5, 0.0, 1.0);
  // The undisplaced direction is the shell's normal, which is what lets a
  // highlight travel across the whole form rather than only within each dot.
  vNormal = normalize(normalMatrix * position);

  vec4 mvPosition = modelViewMatrix * vec4(position * (1.0 + displacement * uAmplitude), 1.0);
  // Measured in view space, so the shading gradient stays anchored to the
  // screen while the cloud itself rotates.
  vHeight = clamp(mvPosition.y / 2.8 + 0.5, 0.0, 1.0);
  // How near this particle is to the viewer, 0 at the back of the cloud and 1
  // at the front. Without it every layer prints at the same weight and the
  // cloud flattens into a disc.
  vDepth = clamp((mvPosition.z + uCameraZ) / 3.2 + 0.5, 0.0, 1.0);
  // Plume particles are slightly smaller, not larger: growing them turns the
  // crests into solid studs instead of dispersing dust.
  gl_PointSize = uSize * (0.95 - energy * 0.3) * (0.7 + aSeed * 0.6) * (6.0 / -mvPosition.z);
  gl_Position = projectionMatrix * mvPosition;
}`

const FRAGMENT = `
uniform vec3 uLight;
uniform vec3 uBlue;
uniform vec3 uInk;
uniform vec3 uViolet;
uniform vec3 uMagenta;
uniform float uSaturation;
uniform float uOpacity;
varying float vEnergy;
varying float vSeed;
varying float vHeight;
varying float vDepth;
varying float vFlow;
varying vec3 vNormal;
void main() {
  // Round, softly feathered dots. Deliberately not shaded as individual
  // spheres: giving every dot its own lit side and catchlight needs beads big
  // enough to see, and at that size the cloud stops being a mist and turns
  // into bubbles. The gloss belongs to the form, not to the grains.
  float radius = length(gl_PointCoord - 0.5);
  if (radius > 0.5) discard;
  float mask = smoothstep(0.5, 0.34, radius);

  // Mostly the swirl, with enough screen-anchored height left in it that the
  // cloud still reads as lit from above rather than coloured at random.
  float shade = clamp(vFlow * 0.74 + vHeight * 0.26, 0.0, 1.0);

  // The dark sweep remains predominantly blue. Magenta only tints its endpoint
  // before the ramp travels through violet, brand blue, and cyan. Violet and
  // magenta hold just enough hue travel to keep the form from flattening; brand
  // blue owns the widest band so the orb reads as Polymux's blue.
  vec3 magentaHint = mix(uBlue, uMagenta, 0.10);
  vec3 colour = mix(uInk, magentaHint, smoothstep(0.0, 0.18, shade));
  colour = mix(colour, uViolet, smoothstep(0.16, 0.32, shade));
  colour = mix(colour, uBlue, smoothstep(0.28, 0.70, shade));
  colour = mix(colour, uLight, smoothstep(0.72, 0.98, shade) * 0.85);

  // Energy then works against that gradient rather than replacing it: plume
  // roots deepen from whatever blue they sit in, and the dispersing tips lift
  // toward the light blue as they thin out.
  colour = mix(colour, uViolet, smoothstep(0.35, 0.85, vEnergy) * 0.13);
  colour = mix(colour, uLight, smoothstep(0.70, 1.0, vEnergy) * 0.5);

  // Density rises into the plumes, then falls away again at their very tips:
  // those particles are dispersing, and holding them opaque turns them into
  // dark specks rather than spray.
  float density = (0.92 + vEnergy * 0.45) * (1.0 - smoothstep(0.72, 1.0, vEnergy) * 0.55);

  // Weight follows the swirl rather than the screen: the deep sweeps print
  // heavily and the pale ones thin out, which is what gives the pattern depth
  // instead of leaving it a flat recolouring.
  density *= 1.72 - shade * 1.28;

  vec3 N = normalize(vNormal);
  // Upper left and slightly in front, the same key the rest of the interface
  // is lit from.
  vec3 L = normalize(vec3(-0.35, 0.62, 0.70));
  vec3 H = normalize(L + vec3(0.0, 0.0, 1.0));
  float lit = pow(clamp(dot(N, L), 0.0, 1.0), 0.7);

  // The shaded side prints denser and the lit side thinner. On a near-white
  // surface the shine cannot come from the highlight getting brighter — white
  // on white is a hole — so it has to come from the shadow getting darker.
  density *= 1.16 - lit * 0.42;

  // Aerial perspective, kept deliberately slight. Lightening the far side is
  // the same operation as the top of the vertical gradient, so a strong one
  // silently erases the dark end: at 0.35 it dragged ink to mid-blue and the
  // whole cloud read as a single colour. Thinning the far side by alpha below
  // carries the depth instead.
  colour = mix(mix(colour, uLight, 0.12), colour, vDepth);
  float alpha = mask * uOpacity * density * (0.6 + vSeed * 0.4) * (0.42 + vDepth * 0.58);
  // Shell shading: a fast fall from lit to shaded is what separates a glossy
  // surface from a matte one, which spreads the same light evenly instead. The
  // floor is an ambient term, not a true black — taken all the way down it
  // multiplied the deep end of the ramp into specks with no hue left in them,
  // and the shaded side of the cloud read as scattered black rather than as
  // blue in shadow. Depth here is carried by alpha and by density above.
  colour *= 0.58 + 0.62 * lit;

  // Only the near shell can reflect toward the viewer. Without this the far
  // side paints highlights straight through the form and the reflection smears
  // across the whole silhouette instead of sitting where the light is.
  float facing = smoothstep(0.30, 0.85, vDepth);
  float ndh = clamp(dot(N, H), 0.0, 1.0);

  // A medium-tight lobe rather than a very tight one: in a cloud this fine, a
  // narrow highlight lands on too few particles to survive being averaged with
  // everything drawn over it. Brightness here should read as a reflection,
  // which is why the ramp above stops short of pale — a highlight needs darker
  // material around it or there is nothing for it to be brighter than.
  colour += mix(uLight, vec3(1.0), 0.4) * pow(ndh, 20.0) * 1.05 * facing;
  colour += uLight * pow(ndh, 6.0) * 0.28 * facing;

  // Push every colour away from its own grey before the state saturation is
  // applied. Alpha compositing over a light surface pulls everything toward
  // white, so hues have to leave the shader already over-saturated to arrive
  // on screen as vivid rather than pastel.
  float own = dot(colour, vec3(0.299, 0.587, 0.114));
  colour = clamp(own + (colour - own) * 1.35, 0.0, 1.0);

  float grey = dot(colour, vec3(0.299, 0.587, 0.114));
  gl_FragColor = vec4(mix(vec3(grey), colour, uSaturation), alpha);
  // three converts colour uniforms into its linear working space, so without
  // this the palette is written to an sRGB buffer unconverted and every hue
  // lands far darker than the brand colour it came from.
  #include <colorspace_fragment>
}`

export function startThreeVoiceOrb(canvas: HTMLCanvasElement, initialState: RealtimeVoiceState = 'connecting'): VoiceOrbHandle {
  const renderer = new WebGLRenderer({ canvas, alpha: true, antialias: true })
  renderer.setClearColor(0x000000, 0)
  const pixelRatio = Math.min(window.devicePixelRatio || 1, 2)
  renderer.setPixelRatio(pixelRatio)

  const scene = new Scene()
  const camera = new PerspectiveCamera(45, 1, 0.1, 100)
  camera.position.set(0, 0, 5.9)

  const uniforms = {
    uTime: { value: 0 },
    uAmplitude: { value: 0.16 },
    uFrequency: { value: 1.4 },
    uBass: { value: 0 },
    uTreble: { value: 0 },
    uSize: { value: 3 },
    uCameraZ: { value: 5.9 },
    uSaturation: { value: 1 },
    uOpacity: { value: 1 },
    uLight: { value: FLARE_LIGHT },
    uBlue: { value: FLARE_BLUE },
    uInk: { value: FLARE_INK },
    uViolet: { value: FLARE_VIOLET },
    uMagenta: { value: FLARE_MAGENTA },
  }

  const geometry = new BufferGeometry()
  geometry.setAttribute('position', new BufferAttribute(spherePoints(POINT_COUNT), 3))
  geometry.setAttribute('aSeed', new BufferAttribute(pointSeeds(POINT_COUNT), 1))

  const points = new Points(
    geometry,
    new ShaderMaterial({
      uniforms,
      vertexShader: VERTEX,
      fragmentShader: FRAGMENT,
      transparent: true,
      // Dots overlap heavily; depth-testing them against each other would punch
      // holes in the cloud rather than letting the pigment accumulate.
      depthWrite: false,
      blending: NormalBlending,
    }),
  )
  scene.add(points)

  const reduceMotion = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
  let state = initialState
  let muted = false
  let paused = false
  let level = 0
  let smoothed = 0
  let bass = 0
  let treble = 0
  let frame = 0
  let disposed = false
  // Elapsed noise phase, integrated rather than derived from a start time. See
  // the draw loop for why multiplying total elapsed time by the current speed
  // cannot work.
  let phase = 0
  let lastNow = 0
  let visual = orbStateVisual(initialState)

  const resize = () => {
    const rect = canvas.getBoundingClientRect()
    const size = Math.max(1, Math.min(rect.width, rect.height))
    renderer.setSize(size, size, false)
    // Dots hold their apparent weight across the full-screen and in-chat sizes.
    uniforms.uSize.value = Math.max(1.6, size / 180) * pixelRatio
    camera.aspect = 1
    camera.updateProjectionMatrix()
  }

  const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(resize)
  observer?.observe(canvas)
  resize()

  const draw = (now: number) => {
    if (disposed) return

    const target = orbStateVisual(state, { muted, paused })
    // Eased, not switched: every parameter would otherwise step together on the
    // frame the state changes.
    visual = reduceMotion ? target : approachVisual(visual, target, 0.055)

    // The phase advances by speed each frame instead of being recomputed as
    // elapsed x speed. That product rescales the whole timeline whenever speed
    // changes, so at thirty seconds in, moving from listening to thinking threw
    // the noise field from 25s to 57s in one frame — the visible jump when a
    // reply starts. Integrating leaves everything already drawn where it is.
    const delta = lastNow ? Math.min(0.05, (now - lastNow) / 1000) : 0
    lastNow = now
    if (!reduceMotion) phase += delta * visual.speed

    smoothed = reduceMotion ? 0 : smoothLevel(smoothed, level)
    const energy = Math.min(1, smoothed * visual.reactivity)

    uniforms.uTime.value = reduceMotion ? 0 : phase
    uniforms.uBass.value += (bass - uniforms.uBass.value) * 0.12
    uniforms.uTreble.value += (treble - uniforms.uTreble.value) * 0.12
    uniforms.uAmplitude.value = (0.13 + energy * 0.26) * visual.scale
    uniforms.uSaturation.value = visual.saturation
    uniforms.uOpacity.value = 0.55 + visual.glow * 0.45

    points.rotation.y += 0.0016 * visual.speed
    points.rotation.x += 0.0007 * visual.speed
    points.scale.setScalar(visual.scale * (1 + energy * 0.06))

    renderer.render(scene, camera)
    if (!reduceMotion) frame = requestAnimationFrame(draw)
  }

  frame = requestAnimationFrame(draw)

  return {
    setState(nextState, flags = {}) {
      state = nextState
      muted = flags.muted ?? false
      paused = flags.paused ?? false
      if (reduceMotion && !disposed) frame = requestAnimationFrame(draw)
    },
    setLevel(next, bands?: AudioBands) {
      level = Number.isFinite(next) ? Math.max(0, Math.min(1, next)) : 0
      if (bands) {
        bass = Math.max(0, Math.min(1, bands.low))
        treble = Math.max(0, Math.min(1, bands.high))
      }
    },
    destroy() {
      disposed = true
      cancelAnimationFrame(frame)
      observer?.disconnect()
      geometry.dispose()
      ;(points.material as Material).dispose()
      renderer.dispose()
    },
  }
}
