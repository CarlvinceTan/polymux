<script lang="ts">
  import {onMount} from 'svelte';
  import logo from '../assets/app-icon.webp';
  import {agents, type Agent} from '../lib/agents';
  import AppIcon, {type AppId} from './AppIcon.svelte';
  import WorkWindow from './WorkWindow.svelte';

  let {active}: {active?: Agent['id']} = $props();

  // A 1100×640 stage; every length is multiplied by --u, so the island scales with its container.
  const OX = 550;
  const OY = 300;
  const HALF = 36;
  const STEP = 18;
  const CUBE = 30;
  const speed = 190;

  type Cell = [number, number];
  type Point = {x: number; y: number};
  type App = {
    id: AppId;
    label: string;
    tint: string;
    hue: number;
    cell: Cell;
    dock: Cell;
    win: [number, number];
    tx: string;
    ty: string;
    tasks: string[];
  };

  const apps: App[] = [
    {id: 'chrome', label: 'Chrome', tint: '#4285f4', hue: 217, cell: [5, -2], dock: [6, 0], win: [846, 190], tx: '59px', ty: '27px',
      tasks: ['Found a cake recipe', 'Asked about the tab', 'Listed the ingredients']},
    {id: 'slack', label: 'Slack', tint: '#36c5f0', hue: 197, cell: [4, 3], dock: [4, 5], win: [648, 392], tx: '76px', ty: '87px',
      tasks: ['Replied in the thread', 'Read the check-ins', 'Posted the update']},
    {id: 'figma', label: 'Figma', tint: '#a259ff', hue: 271, cell: [-1, 5], dock: [1, 6], win: [18, 338], tx: '99px', ty: '16px',
      tasks: ['Tidied the map screen', 'Nudged 6 frames', 'Checked the prototype']},
    {id: 'notion', label: 'Notion', tint: '#d9d3cc', hue: 38, cell: [-5, 2], dock: [-3, 4], win: [18, 18], tx: '54px', ty: '61px',
      tasks: ['Added Sprint 27 notes', 'Tagged the meetings', 'Wrote the summary']},
    {id: 'mail', label: 'Mail', tint: '#5ac8fa', hue: 205, cell: [-5, -3], dock: [-3, -1], win: [18, 178], tx: '50px', ty: '40px',
      tasks: ['Replied to Liz', 'Sorted the inbox', 'Drafted 2 replies']},
    {id: 'calendar', label: 'Calendar', tint: '#ff5a6e', hue: 354, cell: [1, -5], dock: [3, -3], win: [838, 16], tx: '133px', ty: '77px',
      tasks: ['Booked a 16:30 walk', 'Found a free hour', 'Added the walk']},
  ];

  const key = (i: number, j: number) => `${i},${j}`;
  const hash = (i: number, j: number) => {
    const n = Math.sin(i * 127.1 + j * 311.7) * 43758.5453;
    return n - Math.floor(n);
  };

  // A floor with a chipped shelf and an underside that tapers to a point.
  const heights = new Map<string, number>();
  for (let i = -9; i <= 9; i += 1) {
    for (let j = -9; j <= 9; j += 1) {
      const d = Math.hypot(i, j * 1.04);
      if (d <= 6.7) heights.set(key(i, j), 1);
      else if (d <= 7.8) heights.set(key(i, j), hash(i, j) > .84 ? 1 : 0);
    }
  }

  const platformAt = new Map<string, App>();
  for (const app of apps) {
    const [ai, aj] = app.cell;
    for (const [di, dj] of [[0, 0], [1, 0], [0, 1], [1, 1]]) {
      heights.set(key(ai + di, aj + dj), 2);
      platformAt.set(key(ai + di, aj + dj), app);
    }
  }

  const hubCells: Cell[] = [[0, 0], [1, 0], [0, 1], [1, 1]];
  const hubSet = new Set(hubCells.map(([i, j]) => key(i, j)));
  for (const [i, j] of hubCells) heights.set(key(i, j), 5);

  const cellPoint = (i: number, j: number, h = heights.get(key(i, j)) ?? 1) => ({
    x: OX + (i - j) * HALF,
    y: OY + (i + j) * STEP - h * CUBE,
  });

  // Routes run around platforms, not through them.
  const blocked = new Set([...platformAt.keys(), ...hubSet]);
  function findRoute(goal: Cell): Cell[] {
    const start: Cell = [2, 2];
    const seen = new Map<string, Cell | null>([[key(...start), null]]);
    const queue: Cell[] = [start];
    const dirs: Cell[] = [[1, 0], [0, 1], [-1, 0], [0, -1]];
    for (let head = 0; head < queue.length && !seen.has(key(...goal)); head += 1) {
      const cell = queue[head];
      for (const [di, dj] of dirs) {
        const next: Cell = [cell[0] + di, cell[1] + dj];
        const id = key(...next);
        if (seen.has(id) || blocked.has(id) || heights.get(id) !== 1) continue;
        seen.set(id, cell);
        queue.push(next);
      }
    }
    if (!seen.has(key(...goal))) return [start, goal];
    const route: Cell[] = [];
    let cell: Cell | null = goal;
    while (cell) {
      route.unshift(cell);
      cell = seen.get(key(...cell)) ?? null;
    }
    return route;
  }

  const routes = new Map<AppId, Cell[]>(apps.map((app) => [app.id, findRoute(app.dock)]));
  const pathCells = new Set<string>();
  for (const route of routes.values()) for (const [i, j] of route) pathCells.add(key(i, j));

  const homes: Cell[] = [[2, 2], [3, 2], [4, 2], [2, 3], [2, 4], [3, 3], [3, 4], [1, 3], [1, 4], [0, 4], [2, 5], [4, 1]];
  const kept = new Set([...homes.map(([i, j]) => key(i, j)), ...apps.map((app) => key(...app.dock))]);
  const isGrass = (id: string) => !platformAt.has(id) && !hubSet.has(id) && !pathCells.has(id);

  const poly = (x: number, y: number) => `${x.toFixed(1)},${y.toFixed(1)}`;
  const diamond = (x: number, y: number, w: number, h: number) => `M${poly(x, y - h)}L${poly(x + w, y)}L${poly(x, y + h)}L${poly(x - w, y)}Z`;

  // A horizontal band of a cube's side, from a to b below its top edge.
  const band = (x: number, y: number, side: 'left' | 'right', a: number, b: number) => side === 'left'
    ? `M${poly(x - HALF, y + a)}L${poly(x, y + STEP + a)}L${poly(x, y + STEP + b)}L${poly(x - HALF, y + b)}Z`
    : `M${poly(x, y + STEP + a)}L${poly(x + HALF, y + a)}L${poly(x + HALF, y + b)}L${poly(x, y + STEP + b)}Z`;

  // Columns reach further down towards the middle, like an upturned cone.
  const underside = (i: number, j: number) => {
    const r = Math.hypot(i, j * 1.04);
    return 12 + 260 * Math.max(0, 1 - r / 7.9) ** 1.6;
  };

  type Face = {d: string; fill: string};
  type Cube = {id: string; d: number; faces: Face[]; top: string; topFill: string; edge: string; inner: string; lit: boolean};

  function look(i: number, j: number, h: number) {
    const id = key(i, j);
    const app = platformAt.get(id);
    const shade = hash(i + 9, j - 4);
    if (app) return {top: `hsl(${app.hue} 62% 82%)`, edge: app.tint, walls: [`hsl(${app.hue} 24% 97%)`, `hsl(${app.hue} 14% 80%)`]};
    if (hubSet.has(id)) return {top: 'hsl(262 54% 80%)', edge: '#f4efff', walls: ['hsl(256 30% 95%)', 'hsl(258 20% 77%)']};
    if (pathCells.has(id)) return {top: 'hsl(250 46% 96%)', edge: '#ffffff'};
    const light = (h === 0 ? 70 : 81) + shade * 4;
    return {top: `hsl(250 ${16 + shade * 6}% ${light}%)`, edge: '#ffffff66'};
  }

  const cubes: Cube[] = [...heights.entries()].map(([id, h]) => {
    const [i, j] = id.split(',').map(Number);
    const {x, y} = cellPoint(i, j, h);
    const deep = h * CUBE + underside(i, j);
    const cap = Math.max(0, (h - 1) * CUBE);
    const m = look(i, j, h);
    const faces: Face[] = [];
    (['left', 'right'] as const).forEach((side, s) => {
      faces.push({d: band(x, y, side, cap, deep), fill: `url(#aw-stone-${side})`});
      if (m.walls) faces.push({d: band(x, y, side, 0, cap), fill: m.walls[s]});
    });
    return {id, d: i + j, faces, top: diamond(x, y, HALF, STEP), topFill: m.top, edge: m.edge, inner: diamond(x, y, HALF * .3, STEP * .3), lit: pathCells.has(id)};
  });

  // A few soft voxel trees: a box of half-width s and height h standing on (x, y).
  const block = (x: number, y: number, s: number, h: number) => ({
    top: diamond(x, y - h, s, s / 2),
    left: `M${poly(x - s, y - h)}L${poly(x, y - h + s / 2)}L${poly(x, y + s / 2)}L${poly(x - s, y)}Z`,
    right: `M${poly(x, y - h + s / 2)}L${poly(x + s, y - h)}L${poly(x + s, y)}L${poly(x, y + s / 2)}Z`,
  });
  type Box = ReturnType<typeof block> & {fill: [string, string, string]};
  type Tree = {id: string; d: number; x: number; y: number; boxes: Box[]};

  const trunk: [string, string, string] = ['hsl(28 22% 70%)', 'hsl(28 18% 56%)', 'hsl(28 16% 46%)'];
  const kinds: [string, string, string][][] = [
    [['hsl(152 28% 74%)', 'hsl(154 22% 58%)', 'hsl(156 20% 47%)'], ['hsl(150 32% 84%)', 'hsl(152 24% 69%)', 'hsl(154 22% 57%)']],
    [['hsl(268 42% 84%)', 'hsl(268 26% 69%)', 'hsl(270 24% 58%)'], ['hsl(268 48% 91%)', 'hsl(268 30% 77%)', 'hsl(270 26% 66%)']],
  ];

  const trees: Tree[] = [...heights.entries()]
    .filter(([id, h]) => h === 1 && isGrass(id) && !kept.has(id))
    .map(([id]) => id.split(',').map(Number) as Cell)
    .filter(([i, j]) => hash(i - 5, j + 8) > .8)
    .map(([i, j]) => {
      const {x, y} = cellPoint(i, j);
      const [crown, cap] = kinds[Math.floor(hash(i + 2, j + 6) * kinds.length)];
      return {id: key(i, j), d: i + j, x, y, boxes: [
        {...block(x, y + 2, 4, 12), fill: trunk},
        {...block(x, y - 8, 15, 19), fill: crown},
        {...block(x + 2, y - 27, 8, 9), fill: cap},
      ]};
    });

  const clouds = [
    {x: 250, y: 580, w: 260, delay: '0s'},
    {x: 860, y: 590, w: 300, delay: '-14s'},
    {x: 560, y: 630, w: 340, delay: '-7s'},
  ];

  const layers = [...new Set(cubes.map((cube) => cube.d))]
    .sort((a, b) => a - b)
    .map((d) => ({
      d,
      cubes: cubes.filter((cube) => cube.d === d),
      trees: trees.filter((tree) => tree.d === d),
      docks: apps.filter((app) => app.dock[0] + app.dock[1] === d),
      z: Math.round((OY + d * STEP) * 4) + 2,
    }));

  const hubTop = OY + STEP - 5 * CUBE;
  const appX = (app: App) => cellPoint(app.cell[0], app.cell[1]).x;
  const appTop = (app: App) => OY + (app.cell[0] + app.cell[1] + 1) * STEP - 2 * CUBE;
  const dockPoint = (app: App) => cellPoint(app.dock[0], app.dock[1]);

  const place = (x: number, y: number) => `calc(${x.toFixed(1)} * var(--u)) calc(${y.toFixed(1)} * var(--u))`;
  const depth = (y: number) => Math.round(y * 4) + 2;

  const home = (index: number): Point => {
    const [i, j] = homes[index % homes.length];
    const point = cellPoint(i, j);
    const [dx, dy] = [[-14, 4], [14, 4], [0, 14]][index % 3];
    return {x: point.x + dx, y: point.y + dy};
  };
  const slots = [[-18, 1], [18, 1], [0, 13]];
  const stand = (index: number, app: App): Point => {
    const point = dockPoint(app);
    const [dx, dy] = slots[index % slots.length];
    return {x: point.x + dx, y: point.y + dy};
  };

  const distance = (points: Point[]) => points.slice(1).reduce((sum, point, index) => sum + Math.hypot(point.x - points[index].x, point.y - points[index].y), 0);
  const dedupe = (points: Point[]) => points.filter((point, index) => !index || Math.hypot(point.x - points[index - 1].x, point.y - points[index - 1].y) > .5);

  function trip(index: number, app: App) {
    const route = routes.get(app.id)!;
    const start = home(index);
    const end = stand(index, app);
    const out = dedupe([start, ...route.map(([i, j]) => cellPoint(i, j, 1)), end]);
    const back = [...out].reverse();
    return {out, back, outLength: distance(out), backLength: distance(back)};
  }

  type Phase = 'rest' | 'out' | 'work' | 'back';
  type Motion = {t: number; wait: number; task: string; path: Point[]; length: number};

  const roles = $state(agents.map((_, index) => ({phase: 'rest' as Phase, app: index % apps.length, started: 0})));
  const motion: Motion[] = agents.map((_, index) => ({t: 0, wait: .4 + index * .35, task: '', path: [home(index)], length: 0}));
  const walkers: HTMLElement[] = [];

  let done = $state(0);
  let animate = $state(false);
  let still = $state(false);
  let world: HTMLElement;

  const busy = $derived(roles.filter((role) => role.phase !== 'rest').length);
  const working = $derived(roles
    .map((role, index) => ({...role, index}))
    .filter((role) => role.phase === 'work')
    .sort((a, b) => b.started - a.started)
    .slice(0, 3)
    .map((role) => ({index: role.index, app: apps[role.app], task: motion[role.index].task})));

  function pointAt(points: Point[], travelled: number) {
    let left = travelled;
    for (let index = 0; index < points.length - 1; index += 1) {
      const from = points[index];
      const to = points[index + 1];
      const length = Math.hypot(to.x - from.x, to.y - from.y);
      if (left <= length || index === points.length - 2) {
        const t = length ? Math.min(1, left / length) : 0;
        const hop = Math.min(6, 2 + length * .02);
        return {
          x: from.x + (to.x - from.x) * t,
          y: from.y + (to.y - from.y) * t,
          z: Math.sin(Math.PI * t) * hop,
        };
      }
      left -= length;
    }
    return {...points[points.length - 1], z: 0};
  }

  function position(index: number, now: number) {
    const role = roles[index];
    if (role.phase === 'rest') return {...home(index), z: 0};
    if (role.phase === 'work') {
      const at = stand(index, apps[role.app]);
      return {...at, z: Math.abs(Math.sin(now / 420 + index)) * 2};
    }
    return pointAt(motion[index].path, motion[index].t);
  }

  function draw(now: number) {
    walkers.forEach((element, index) => {
      if (!element) return;
      const {x, y, z} = position(index, now);
      element.style.translate = place(x, y);
      element.style.setProperty('--z', z.toFixed(1));
      element.style.zIndex = String(depth(y + CUBE));
    });
  }

  function assign(index: number, appIndex: number) {
    const app = apps[appIndex];
    const {out, outLength} = trip(index, app);
    motion[index] = {
      t: 0,
      wait: 0,
      task: app.tasks[Math.floor(Math.random() * app.tasks.length)],
      path: out,
      length: outLength,
    };
    roles[index] = {phase: 'out', app: appIndex, started: 0};
  }

  function advance(index: number, dt: number) {
    const role = roles[index];
    const state = motion[index];
    if (role.phase === 'rest' || role.phase === 'work') {
      state.wait -= dt;
      if (state.wait > 0) return;
      if (role.phase === 'rest') {
        const taken = new Set(roles.filter((other, at) => at !== index && other.phase !== 'rest').map((other) => other.app));
        const free = apps.map((_, at) => at).filter((at) => !taken.has(at) && at !== role.app);
        const pool = free.length ? free : apps.map((_, at) => at);
        assign(index, pool[Math.floor(Math.random() * pool.length)]);
      } else {
        const {back, backLength} = trip(index, apps[role.app]);
        motion[index] = {...state, t: 0, wait: 0, path: back, length: backLength};
        roles[index] = {phase: 'back', app: role.app, started: 0};
      }
      return;
    }
    state.t = Math.min(state.length, state.t + dt * speed);
    if (state.t < state.length) return;
    if (role.phase === 'out') {
      const app = apps[role.app];
      state.wait = 3.3 + Math.random() * .6;
      roles[index] = {phase: 'work', app: role.app, started: Date.now()};
    } else {
      done += 1;
      state.wait = .6 + Math.random() * 1.8;
      roles[index] = {phase: 'rest', app: role.app, started: 0};
    }
  }

  function tilt(event: PointerEvent) {
    if (event.pointerType !== 'mouse') return;
    const box = world.getBoundingClientRect();
    world.style.setProperty('--px', (((event.clientX - box.left) / box.width - .5) * 2).toFixed(3));
    world.style.setProperty('--py', (((event.clientY - box.top) / box.height - .5) * 2).toFixed(3));
  }

  function stillFrame() {
    const phases: Phase[] = ['rest', 'out', 'work', 'back', 'work', 'out', 'rest', 'work', 'back', 'out', 'work', 'rest'];
    agents.forEach((_, index) => {
      const app = apps[index % apps.length];
      const {out, back, outLength, backLength} = trip(index, app);
      const phase = phases[index % phases.length];
      motion[index] = {
        t: phase === 'back' ? backLength * .55 : phase === 'out' ? outLength * .45 : 0,
        wait: 0,
        task: app.tasks[0],
        path: phase === 'back' ? back : out,
        length: phase === 'back' ? backLength : outLength,
      };
      roles[index] = {phase, app: index % apps.length, started: Date.now() - index * 100};
    });
    requestAnimationFrame?.(() => draw(0));
  }

  onMount(() => {
    if (!window.matchMedia || matchMedia('(prefers-reduced-motion: reduce)').matches) {
      still = true;
      stillFrame();
      return;
    }
    animate = true;
    let visible = true;
    const observer = 'IntersectionObserver' in window ? new IntersectionObserver(([entry]) => visible = entry.isIntersecting) : null;
    observer?.observe(world);
    let last = performance.now();
    let frame = requestAnimationFrame(function tick(now) {
      const dt = Math.min(.05, (now - last) / 1000);
      last = now;
      if (visible) {
        roles.forEach((_, index) => advance(index, dt));
        draw(now);
      }
      frame = requestAnimationFrame(tick);
    });
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
    };
  });

  const pathD = (app: App, reverse = false) => {
    const points = routes.get(app.id)!.map(([i, j]) => {
      const point = cellPoint(i, j, 1);
      return {x: point.x, y: point.y - 16};
    });
    if (reverse) points.reverse();
    return `M${points[0].x.toFixed(1)},${points[0].y.toFixed(1)} ${points.slice(1).map((point) => `L${point.x.toFixed(1)},${point.y.toFixed(1)}`).join(' ')}`;
  };

</script>

<div
  class="world"
  bind:this={world}
  role="img"
  aria-label="A floating voxel island where Polymux agents leave your Mac, control Chrome, Slack, Figma, Notion, Mail and Calendar, and bring the results back."
  onpointermove={tilt}
  onpointerleave={() => { world.style.setProperty('--px', '0'); world.style.setProperty('--py', '0'); }}
>
  <div class="stage" aria-hidden="true">
    <div class="glow"></div>
    <div class="void"></div>

    <svg class="defs" width="0" height="0" aria-hidden="true">
      <defs>
        <linearGradient id="aw-stone-left" gradientUnits="userSpaceOnUse" x1="0" y1="250" x2="0" y2="620">
          <stop offset="0" stop-color="hsl(250 24% 72%)" /><stop offset=".5" stop-color="hsl(254 22% 46%)" /><stop offset="1" stop-color="hsl(260 26% 24%)" />
        </linearGradient>
        <linearGradient id="aw-stone-right" gradientUnits="userSpaceOnUse" x1="0" y1="250" x2="0" y2="620">
          <stop offset="0" stop-color="hsl(252 20% 58%)" /><stop offset=".5" stop-color="hsl(256 20% 36%)" /><stop offset="1" stop-color="hsl(262 26% 18%)" />
        </linearGradient>
      </defs>
    </svg>

    {#each clouds as cloud, index (index)}
      <i class="cloud" style:left={`calc(${cloud.x} * var(--u))`} style:top={`calc(${cloud.y} * var(--u))`} style:width={`calc(${cloud.w} * var(--u))`} style:--delay={cloud.delay}></i>
    {/each}

    {#each layers as layer (layer.d)}
      <svg class="layer" viewBox="0 0 1100 640" style:z-index={layer.z}>
        {#each layer.cubes as cube (cube.id)}
          {#each cube.faces as face, index (index)}<path d={face.d} fill={face.fill} />{/each}
          <path d={cube.top} style:fill={cube.topFill} style:stroke={cube.edge} class="top" />
          {#if cube.lit}<path d={cube.inner} class="spark" />{/if}
        {/each}

        {#each layer.docks as app (app.id)}
          {@const point = dockPoint(app)}
          <ellipse cx={point.x} cy={point.y} rx="24" ry="12" fill="none" stroke={app.tint} stroke-width="2" opacity=".5" class="dock-ring" />
        {/each}

        {#each layer.trees as tree (tree.id)}
          <ellipse cx={tree.x} cy={tree.y + 3} rx="17" ry="7" fill="#2a2050" opacity=".18" />
          <g>
            {#each tree.boxes as box, index (index)}
              <path d={box.left} fill={box.fill[1]} /><path d={box.right} fill={box.fill[2]} /><path d={box.top} fill={box.fill[0]} />
            {/each}
          </g>
        {/each}

      </svg>
    {/each}

    {#each apps as app (app.id)}
      <div class="sign" style:translate={place(appX(app), appTop(app) - 52)} style:z-index="4000" style:--tint={app.tint}>
        <span class="shaft"></span>
        <span class="icon"><AppIcon id={app.id} /></span>
      </div>
    {/each}

    <div class="hub" style:left={`calc(${OX} * var(--u))`} style:top={`calc(${hubTop} * var(--u))`} style:z-index="4010">
      <span class="hub-glow"></span>
      <img src={logo} alt="" width="86" height="86" decoding="async" />
    </div>

    {#each agents as agent, index (agent.id)}
      {@const role = roles[index]}
      <div class="walker" class:active={active === agent.id} bind:this={walkers[index]} style:--tint={agent.tint}>
        <i class="shadow"></i>
        <div class="body">
          {#if role.phase === 'out'}
            <span class="chip" style:--chip={apps[role.app].tint}><AppIcon id={apps[role.app].id} /></span>
          {:else if role.phase === 'back'}
            <span class="chip" style:--chip={agent.tint}><svg viewBox="0 0 24 24"><path d="M5 12.5l4.5 4.5L19 7.5" /></svg></span>
          {/if}
          <img src={agent.src} alt="" width="160" height="160" decoding="async" />
        </div>
      </div>
    {/each}

    {#each working as item (item.index)}
      <div class="work" style:translate={place(item.app.win[0], item.app.win[1])} style:z-index="6000">
        <WorkWindow app={item.app} agent={{src: agents[item.index].src}} />
      </div>
    {/each}

    {#if animate}
      <svg class="layer packets" viewBox="0 0 1100 640" style:z-index="5000">
        {#each apps as app, index (app.id)}
          <g class="packet">
            <animateMotion path={pathD(app)} dur={`${3.2 + index * .18}s`} begin={`${-index * .7}s`} repeatCount="indefinite" />
            <ellipse cx="0" cy="3" rx="12" ry="6" fill={app.tint} opacity=".2" />
            <path d="M0 -7 6 -3.5 0 0 -6 -3.5Z" fill="#fff" opacity=".92" />
            <path d="M-6 -3.5 0 0 0 7 -6 3.5Z" fill={app.tint} />
            <path d="M6 -3.5 0 0 0 7 6 3.5Z" fill={app.tint} opacity=".55" />
          </g>
          <g class="packet slow">
            <animateMotion path={pathD(app)} dur={`${3.7 + index * .2}s`} begin={`${-index * .7 - 1.55}s`} repeatCount="indefinite" />
            <ellipse cx="0" cy="3" rx="9" ry="5" fill={app.tint} opacity=".16" />
            <path d="M0 -5 4 -2.5 0 0 -4 -2.5Z" fill="#fff" opacity=".8" />
            <path d="M-4 -2.5 0 0 0 5 -4 2.5Z" fill={app.tint} />
            <path d="M4 -2.5 0 0 0 5 4 2.5Z" fill={app.tint} opacity=".5" />
          </g>
          <g class="packet back">
            <animateMotion path={pathD(app, true)} dur={`${4.2 + index * .16}s`} begin={`${-index * .55 - 2}s`} repeatCount="indefinite" />
            <ellipse cx="0" cy="3" rx="9" ry="5" fill={app.tint} opacity=".18" />
            <path d="M0 -5 4 -2.5 0 0 -4 -2.5Z" fill="#eaffff" opacity=".85" />
            <path d="M-4 -2.5 0 0 0 5 -4 2.5Z" fill={app.tint} opacity=".75" />
            <path d="M4 -2.5 0 0 0 5 4 2.5Z" fill={app.tint} opacity=".4" />
          </g>
        {/each}
      </svg>
    {/if}

  </div>

  <div class="hud">
    <span class="live"></span>Your Mac
    <i></i><strong>{busy}</strong>&nbsp;of {agents.length} busy
    <i></i><strong>{done > 99 ? '99+' : done}</strong>&nbsp;{done === 1 ? 'task' : 'tasks'} done
  </div>
</div>

<style>
  .world { position: relative; width: min(760px, 100%); margin: 0 auto; overflow-x: clip; container-type: inline-size; perspective: 1800px; }
  .stage { --u: calc(100cqw / 1100); --s: calc(100cqw / 1100px); position: relative; aspect-ratio: 1100 / 640; transform: rotateX(calc(var(--py, 0) * -3.5deg)) rotateY(calc(var(--px, 0) * 5deg)); transition: transform .9s var(--spring); pointer-events: none; }

  .glow { position: absolute; z-index: 0; left: 50%; top: 44%; width: 100%; aspect-ratio: 2 / 1; translate: -50% -50%; border-radius: 50%; background: radial-gradient(closest-side, #6a4cff55, #2b8dff1c 55%, transparent 78%); filter: blur(calc(26 * var(--u))); }
  .void { position: absolute; z-index: 0; left: 50%; top: 90%; width: 40%; height: 18%; translate: -50% -50%; border-radius: 50%; background: radial-gradient(closest-side, #8f7bff3a, transparent 72%); filter: blur(calc(18 * var(--u))); }
  .defs { position: absolute; }

  .layer { position: absolute; inset: 0; width: 100%; height: 100%; overflow: visible; pointer-events: none; }
  .top { stroke-width: 1; stroke-linejoin: round; }
  .spark { fill: #8f7bff; opacity: .35; }
  .cloud { position: absolute; z-index: 1; aspect-ratio: 3 / 1; translate: -50% -50%; border-radius: 50%; background: radial-gradient(closest-side, #d9d2ff2e, #a99bff12 60%, transparent); filter: blur(calc(8 * var(--u))); animation: cloud 26s ease-in-out infinite alternate; animation-delay: var(--delay); }

  .dock-ring { animation: dock 4s ease-in-out infinite; }

  .sign { position: absolute; top: 0; left: 0; width: 0; height: 0; }
  .sign .icon { position: absolute; bottom: 0; left: 0; width: calc(52 * var(--u)); height: calc(52 * var(--u)); translate: -50% 0; animation: bob 3.6s ease-in-out infinite alternate; filter: drop-shadow(0 calc(8 * var(--u)) calc(14 * var(--u)) #0008) drop-shadow(0 0 calc(18 * var(--u)) color-mix(in srgb, var(--tint) 45%, transparent)); }
  .sign .icon :global(img) { width: 100%; height: 100%; }
  .sign .shaft { position: absolute; top: 0; left: 0; width: calc(28 * var(--u)); height: calc(52 * var(--u)); translate: -50% 0; background: linear-gradient(180deg, var(--tint), transparent 92%); clip-path: polygon(36% 0, 64% 0, 100% 100%, 0 100%); opacity: .33; filter: blur(calc(3 * var(--u))); }

  .hub { position: absolute; width: 0; height: 0; }
  .hub img { position: absolute; bottom: calc(10 * var(--u)); left: 0; max-width: none; width: calc(86 * var(--u)); height: calc(86 * var(--u)); translate: -50% 0; border-radius: 22%; box-shadow: 0 0 0 calc(1 * var(--u)) #ffffff40, 0 calc(14 * var(--u)) calc(38 * var(--u)) #4a2bb899, 0 0 calc(44 * var(--u)) #8f7bff77; animation: bob 4.2s ease-in-out infinite alternate; }
  .hub-glow { position: absolute; bottom: calc(-18 * var(--u)); left: 0; width: calc(150 * var(--u)); height: calc(70 * var(--u)); translate: -50% 0; border-radius: 50%; background: radial-gradient(#b9a8ff88, transparent 68%); filter: blur(calc(8 * var(--u))); }

  .walker { position: absolute; top: 0; left: 0; width: 0; height: 0; }
  .shadow { position: absolute; width: calc(44 * var(--u)); height: calc(15 * var(--u)); translate: -50% -50%; border-radius: 50%; background: radial-gradient(#04021acc, transparent 70%); opacity: calc(1 - var(--z, 0) / 90); }
  .walker.active .shadow { background: radial-gradient(color-mix(in srgb, var(--tint) 70%, transparent), transparent 70%); box-shadow: 0 0 0 calc(2 * var(--u)) var(--tint), 0 0 calc(24 * var(--u)) var(--tint); opacity: 1; }
  .body { position: absolute; bottom: 0; left: 0; width: calc(88 * var(--u)); display: flex; flex-direction: column; align-items: center; translate: -50% calc(var(--z, 0) * var(--u) * -1); }
  .body img { width: 100%; height: auto; filter: drop-shadow(0 calc(4 * var(--u)) calc(6 * var(--u)) #0006); transition: scale 400ms var(--spring); }
  .walker.active .body img { scale: 1.16; }
  .chip { margin-bottom: calc(-2 * var(--u)); width: calc(26 * var(--u)); height: calc(26 * var(--u)); display: grid; place-items: center; border-radius: calc(8 * var(--u)); background: #fff; box-shadow: 0 calc(4 * var(--u)) calc(12 * var(--u)) calc(-3 * var(--u)) #1a10406b; animation: grab 380ms var(--spring); }
  .chip :global(img) { width: 100%; height: 100%; }
  .chip svg { width: 62%; height: 62%; fill: none; stroke: var(--chip); stroke-width: 3; stroke-linecap: round; stroke-linejoin: round; }

  .work { position: absolute; top: 0; left: 0; transform-origin: 0 0; scale: calc(var(--s) * 1.25); animation: arrive 360ms var(--spring); }

  .packet { filter: drop-shadow(0 0 calc(5 * var(--u)) #fff4); }
  .packet.back { opacity: .8; }
  .packet.slow { opacity: .85; }

  .hud { position: relative; left: 50%; translate: -50% 0; margin-top: 4px; display: inline-flex; align-items: center; color: var(--muted); font-size: 12.5px; font-variant-numeric: tabular-nums; white-space: nowrap; }
  .hud strong { color: var(--ink); font-weight: 650; }
  .hud i { width: 1px; height: 12px; margin: 0 10px; background: #ffffff26; }
  .live { width: 7px; height: 7px; margin-right: 8px; border-radius: 50%; background: #2fd38a; box-shadow: 0 0 0 3px #2fd38a33; animation: blink 2s ease-in-out infinite; }

  @container (max-width: 700px) {
    .hud, .work { display: none; }
    .stage { width: 110%; margin-left: -5%; }
    .body { width: calc(104 * var(--u)); }
    .sign .icon { width: calc(64 * var(--u)); height: calc(64 * var(--u)); }
    .hub img { width: calc(92 * var(--u)); height: calc(92 * var(--u)); }
  }

  @media (prefers-reduced-motion: reduce) {
    .stage :global(*) { animation: none !important; }
  }

  @keyframes cloud { from { translate: -56% -50%; } to { translate: -44% -50%; } }
  @keyframes bob { from { transform: translateY(calc(-4 * var(--u))); } to { transform: translateY(calc(4 * var(--u))); } }
  @keyframes dock { 0%, 100% { opacity: .28; scale: .92; } 50% { opacity: .62; scale: 1.06; } }
  @keyframes grab { from { scale: .3; opacity: 0; } }
  @keyframes blink { 50% { opacity: .3; } }
  @keyframes arrive { from { opacity: 0; filter: blur(3px); } }
</style>
