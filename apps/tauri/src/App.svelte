<script lang="ts">
  import { onMount, type Component } from 'svelte';
  import Shell from './lib/shell/Shell.svelte';
  import { APIClient } from './lib/api/client';
  import { Session } from './lib/session.svelte';
  import { invoke, isTauri } from './lib/api/bridge';
  import { parseAddress } from './lib/api/pairing';
  import { rememberServer } from './lib/servers';

  const gallery = new URLSearchParams(location.search).has('gallery');
  let Gallery = $state<Component>();
  if (gallery) {
    void import('./lib/gallery/Gallery.svelte').then(({ default: component }) => { Gallery = component; });
  }
  let session = $state<Session | undefined>(undefined);
  let address = $state(localStorage.getItem('polymux.server') ?? 'http://127.0.0.1:8787');
  let connecting = $state(false);
  let issue = $state('');
  let addressInput = $state<HTMLInputElement>();
  let connectionVersion = 0;

  async function announce(next: Session, target: string) {
    if (!isTauri) return;
    try {
      const network = await next.client.network();
      rememberServer(target, network.server?.name);
      const device = network.devices.find((entry) => entry.id === network.current);
      if (!device || device.label) return;
      const name = await invoke<string | null>('machine_name');
      if (name) await next.client.labelDevice(device.id, name);
    } catch { /* The device keeps its server-assigned name. */ }
  }

  async function connect(value: string, useCache = false) {
    const version = ++connectionVersion;
    const target = value.trim().replace(/\/$/, '');
    if (!parseAddress(target)) {
      issue = 'The server address is invalid.';
      throw new Error(issue);
    }
    issue = '';
    connecting = true;
    const client = new APIClient(target);
    const next = new Session(target, client);
    try {
      const restored = useCache && await next.restore();
      if (!restored) await next.load();
      if (version !== connectionVersion) return;
      void announce(next, target);
      localStorage.setItem('polymux.server', target);
      rememberServer(target);
      session?.dispose();
      session = next;
      next.start(restored);
    } catch {
      if (version === connectionVersion) issue = "Can't reach the server.";
      throw new Error("Can't reach the server.");
    } finally {
      if (version === connectionVersion) connecting = false;
    }
  }

  onMount(() => {
    void (async () => {
      const target = (isTauri ? await invoke<string | null>('launch_address') : null) ?? localStorage.getItem('polymux.server');
      if (target) await connect(target, true).catch(() => undefined);
      else addressInput?.focus();
    })();
    return () => session?.dispose();
  });
</script>

{#if gallery}
  {#if Gallery}<Gallery />{:else}<div class="gate">Loading gallery…</div>{/if}
{:else if session}
  {#key session}
    <Shell {session} onConnect={(value) => connect(value)} />
  {/key}
{:else}
  <div class="gate">
    <div class="card">
      <h1>Polymux</h1>
      <form
        onsubmit={(event) => { event.preventDefault(); void connect(address).catch(() => undefined); }}
      >
        <input
          type="url"
          aria-label="Server address"
          placeholder="http://127.0.0.1:8787"
          bind:this={addressInput}
          bind:value={address}
        />
        <button type="submit" disabled={connecting}>
          {connecting ? 'Connecting' : 'Connect'}
        </button>
      </form>
      {#if issue}<p class="issue">{issue}</p>{/if}
      {#if import.meta.env.DEV}<a class="gallery-link" href="?gallery=1">Component gallery</a>{/if}
    </div>
  </div>
{/if}

<style>
  .gate {
    height: 100vh;
    display: grid;
    place-items: center;
    background: var(--surface);
  }
  .card {
    width: 320px;
    padding: 28px;
    border-radius: 24px;
    background: var(--panel-bg);
    box-shadow: 0 12px 24px rgba(0, 0, 0, 0.25);
    border: 0.5px solid rgba(0, 0, 0, 0.12);
    text-align: center;
  }
  h1 {
    font-size: 22px;
    font-weight: 600;
    margin: 0 0 18px;
  }
  form {
    display: flex;
    gap: 8px;
  }
  input {
    flex: 1;
    padding: 8px 12px;
    border-radius: 10px;
    border: 0.5px solid rgba(0, 0, 0, 0.2);
    background: var(--field-bg);
    color: inherit;
    font: inherit;
  }
  button {
    padding: 8px 14px;
    border-radius: 10px;
    border: none;
    background: var(--accent);
    color: white;
    font: inherit;
    cursor: pointer;
  }
  .issue {
    color: var(--danger);
    font-size: 12px;
  }
  .gallery-link {
    display: inline-block;
    margin-top: 16px;
    color: var(--text-muted);
    font-size: 11px;
    text-decoration: none;
  }
  .gallery-link:hover { color: var(--text); text-decoration: underline; }
</style>
