<script lang="ts">
  import { untrack } from 'svelte';
  import type { Routine, Run, Thread } from '../api/contract';
  import { isOffline, reason } from '../api/errors';
  import { Cache } from '../cache';
  import type { Session } from '../session.svelte';

  let { session, agentID, thread, visible }: { session: Session; agentID: string; thread: Thread; visible: boolean } = $props();
  const cache = untrack(() => new Cache(session.address));
  let routines = $state<Routine[] | undefined>();
  let issue = $state('');
  let pending = $state<string[]>([]);
  let revision = 0;
  const active = (run: Run) => ['queued', 'running', 'waiting_peer', 'resuming'].includes(run.status);
  const tasks = $derived((thread.runs ?? []).filter((run) => run.agentID === agentID && run.trigger === 'child')
    .toSorted((a, b) => Number(active(b)) - Number(active(a)) || b.createdAt.localeCompare(a.createdAt)));

  $effect(() => {
    const id = agentID;
    let live = true;
    let timer: ReturnType<typeof setTimeout>;
    routines = undefined;
    issue = '';
    pending = [];
    void (async () => {
      const saved = await cache.routines(id);
      if (!live) return;
      if (Array.isArray(saved)) routines = saved;
      const refresh = async () => {
        const start = revision;
        try {
          const fresh = await session.client.routines(id);
          if (live && start === revision) {
            routines = fresh.filter((entry) => !pending.includes(entry.id));
            void cache.saveRoutines(id, $state.snapshot(routines));
          }
        } catch { /* Keep cached routines when the server is unavailable. */ }
        if (live) timer = setTimeout(refresh, document.hidden ? 30000 : 5000);
      };
      await refresh();
    })();
    return () => { live = false; clearTimeout(timer); };
  });

  async function cancel(routine: Routine) {
    if (!routines || pending.includes(routine.id)) return;
    const id = agentID;
    const position = routines.findIndex((item) => item.id === routine.id);
    if (position < 0) return;
    revision++;
    pending = [...pending, routine.id];
    routines = routines.filter((item) => item.id !== routine.id);
    issue = '';
    void cache.saveRoutines(id, $state.snapshot(routines));
    try {
      await session.client.deleteRoutine(id, routine.id);
    } catch (error) {
      if (agentID === id && routines && !routines.some((item) => item.id === routine.id)) {
        routines = [...routines.slice(0, position), routine, ...routines.slice(position)];
        issue = isOffline(error) ? 'Not cancelled yet.' : reason(error, 'Couldn’t cancel that routine.');
      }
    } finally {
      revision++;
      pending = pending.filter((item) => item !== routine.id);
      if (agentID === id && routines) void cache.saveRoutines(id, $state.snapshot(routines));
    }
  }

  function nextFire(routine: Routine): string {
    if (routine.status === 'pending') return 'Waiting for your approval';
    const date = routine.nextAt ? new Date(routine.nextAt) : undefined;
    return date && !Number.isNaN(date.valueOf()) ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date) : 'Scheduled';
  }
</script>

<div class="work" hidden={!visible}>
  {#if issue}<div class="issue" role="status">{issue}</div>{/if}
  <section class="grow">
    <div class="heading"><span>Schedule</span>{#if routines}<span class="count">{routines.length}</span>{/if}</div>
    <div class="card grow" class:empty={!routines?.length}>
      {#if !routines?.length}<span class="none">{routines ? 'No scheduled work' : 'Loading'}</span>{/if}
      {#each routines ?? [] as routine (routine.id)}
        <div class="row"><div class="details"><span>{routine.prompt}</span><small>{nextFire(routine)}</small></div><button aria-label="Cancel {routine.prompt}" onclick={() => void cancel(routine)}>Cancel</button></div>
      {/each}
    </div>
  </section>
  <section class="grow">
    <div class="heading"><span>Tasks</span><span class="count">{tasks.length}</span></div>
    <div class="card grow" class:empty={!tasks.length}>
      {#if !tasks.length}<span class="none">No tasks yet</span>{/if}
      {#each tasks as run (run.id)}
        <div class="row">{#if active(run)}<span aria-label="Active">◌</span>{/if}<span>{active(run) && run.activity ? run.activity : run.summary}</span></div>
      {/each}
    </div>
  </section>
</div>

<style>
  .work { display:flex; flex-direction:column; gap:22px; flex:1; min-height:0; }
  .work[hidden] { display:none; }
  section { display:flex; flex-direction:column; min-height:0; }
  section.grow, .card.grow { flex:1; }
  .heading { display:flex; align-items:baseline; justify-content:space-between; font-size:13px; font-weight:600; padding:0 4px 10px; }
  .count { font-size:11px; font-weight:400; color:var(--text-muted); }
  .card { background:var(--window-bg); border-radius:var(--radius-card); padding:6px 8px; min-height:44px; display:flex; flex-direction:column; }
  .card.empty { min-height:113px; align-items:center; justify-content:center; }
  .none, .issue { color:var(--text-muted); font-size:13px; text-align:center; }
  .row { display:flex; align-items:center; gap:8px; padding:6px; font-size:13px; }
  .details { flex:1; min-width:0; display:flex; flex-direction:column; gap:2px; }
  .details span { display:-webkit-box; -webkit-line-clamp:2; line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; }
  small, button { color:var(--text-muted); font-size:11px; }
</style>
