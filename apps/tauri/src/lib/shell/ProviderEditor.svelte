<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import type { Provider, ProviderLogin, ProviderModel, ProviderOption } from '../api/contract';
  import { isTauri } from '../api/bridge';
  import { copyText } from '../shared/clipboard';
  import type { ProviderStore } from './providers.svelte';

  let { store, type, onBack, onClose }: { store: ProviderStore; type?: string; onBack: () => void; onClose: () => void } = $props();
  type Page = 'catalog' | 'provider' | 'login' | 'form';
  const initial = untrack(() => ({ type, provider: store.providers?.find((provider) => provider.type === type) }));
  let page = $state<Page>(initial.type ? (['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(initial.type) ? 'form' : 'provider') : 'catalog');
  let currentType = $state(initial.type ?? '');
  let query = $state('');
  let category = $state<'all' | 'api_key' | 'oauth'>('all');
  let issue = $state('');
  let working = $state(false);
  let removing = $state<Provider>();
  let login = $state.raw<ProviderLogin>();
  let loginOption = $state<ProviderOption>();
  let answer = $state('');
  let sending = $state(false);
  let name = $state(initial.provider?.name ?? '');
  let baseURL = $state(initial.provider?.baseURL ?? '');
  let key = $state('');
  let enabled = $state(initial.provider?.enabled ?? true);
  let models = $state<ProviderModel[]>(initial.provider?.models.map((model) => ({ ...model })) ?? []);
  let modelID = $state('');
  let original = $state<Provider | undefined>(initial.type && ['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(initial.type) ? initial.provider : undefined);
  let loginGeneration = 0;
  let loginActive = false;
  let disposed = false;

  const group = $derived(store.groups.find((item) => item.id === currentType));
  const connections = $derived(group?.connections ?? []);
  const title = $derived(page === 'catalog' ? 'Add provider' : page === 'login' ? loginOption?.name ?? 'Sign in' : page === 'form' ? (original ? (group?.name ?? original.name) : 'Add connection') : group?.name ?? currentType);
  const matches = $derived((store.options ?? []).filter((option) => (category === 'all' || option.auth === category) && query.trim().toLowerCase().split(/\s+/).filter(Boolean).every((term) => `${option.name} ${option.label}`.toLowerCase().includes(term))));
  const allModels = $derived.by(() => {
    const result: ProviderModel[] = [];
    for (const connection of connections) for (const model of connection.models) {
      const found = result.find((item) => item.id === model.id);
      if (found) found.enabled ||= model.enabled; else result.push({ ...model });
    }
    return result;
  });

  function cancelLogin() {
    loginGeneration++;
    const active = login;
    login = undefined;
    answer = '';
    if (active && !active.done) void store.session.client.cancelLogin(active.id).catch(() => undefined);
    if (loginActive) { loginActive = false; store.end(); }
  }

  onDestroy(() => { disposed = true; cancelLogin(); key = ''; });

  function back() {
    if (working || sending) return;
    if (page === 'login') { cancelLogin(); page = type ? 'provider' : 'catalog'; }
    else if (page === 'form' && original && !['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(original.type)) { key = ''; page = 'provider'; }
    else if (page === 'form' && !type) { key = ''; page = 'catalog'; }
    else onBack();
    issue = ''; query = '';
  }

  function close() { cancelLogin(); key = ''; onClose(); }

  async function connect(option: ProviderOption, provider?: string) {
    cancelLogin();
    page = 'login'; loginOption = option; issue = '';
    const generation = loginGeneration;
    store.begin(); loginActive = true;
    try {
      let state = await store.session.client.startLogin(option.type, option.auth, provider);
      if (generation !== loginGeneration || disposed) { if (!state.done) void store.session.client.cancelLogin(state.id).catch(() => undefined); return; }
      login = state;
      while (!state.done && generation === loginGeneration && !disposed) {
        try {
          state = await store.session.client.providerLogin(state.id, state.revision);
          if (generation !== loginGeneration || disposed) return;
          if (state.prompt?.id !== login?.prompt?.id) answer = '';
          login = state;
          issue = '';
        } catch {
          if (generation !== loginGeneration || disposed) return;
          issue = 'Sign-in is waiting for a connection.';
          await new Promise((resolve) => setTimeout(resolve, 2000));
        }
      }
      if (state.provider && generation === loginGeneration && !disposed) {
        store.merge([state.provider]);
        if (loginActive) { loginActive = false; store.end(); }
        if (type) { currentType = state.provider.type; page = 'provider'; }
        else onBack();
      } else if (state.done && state.error) { if (loginActive) { loginActive = false; store.end(); } issue = 'Couldn’t connect this provider.'; }
    } catch { if (generation === loginGeneration && !disposed) { if (loginActive) { loginActive = false; store.end(); } issue = 'Couldn’t start sign-in.'; } }
  }

  async function submit(value: string) {
    const state = login;
    if (!state?.prompt || sending) return;
    const prompt = state.prompt;
    const generation = loginGeneration;
    answer = ''; sending = true; issue = '';
    login = { ...state, prompt: undefined };
    try { await store.session.client.answerLogin(state.id, prompt.id, value); }
    catch {
      if (generation === loginGeneration && !disposed) {
        if (login?.revision === state.revision) login = { ...login, prompt };
        issue = 'Couldn’t send this answer.';
      }
    } finally { sending = false; }
  }

  async function openLink(value: string) {
    let url: URL;
    try { url = new URL(value); } catch { return; }
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return;
    try {
      if (isTauri) { const { openUrl } = await import('@tauri-apps/plugin-opener'); await openUrl(url.toString()); }
      else window.open(url.toString(), '_blank', 'noopener,noreferrer');
    } catch { issue = 'Couldn’t open this link.'; }
  }

  async function copy(value: string) { if (!await copyText(value)) issue = 'Couldn’t copy.'; }

  async function update(flags: { enabled?: boolean; models?: Record<string, boolean> }) {
    if (working) return;
    const before = store.providers ?? [];
    const changed = connections.map((provider) => ({ ...provider, enabled: flags.enabled ?? provider.enabled,
      models: provider.models.map((model) => ({ ...model, enabled: flags.models?.[model.id] ?? model.enabled })) }));
    working = true; issue = ''; store.begin(); store.merge(changed);
    try { store.merge(await store.session.client.updateProvider(currentType, flags.enabled, flags.models)); }
    catch { store.accept(before); issue = 'Couldn’t save provider settings.'; }
    finally { store.end(); working = false; }
  }

  async function remove(provider: Provider) {
    if (working) return;
    removing = undefined;
    const before = store.providers ?? [];
    working = true; issue = ''; store.begin(); store.remove(provider.id);
    try { await store.session.client.deleteProvider(provider.id); if (page === 'form' || (store.providers ?? []).every((item) => item.type !== currentType)) onBack(); }
    catch { store.accept(before); issue = 'Couldn’t remove this connection.'; }
    finally { store.end(); working = false; }
  }

  async function refreshModels() {
    if (working) return;
    working = true; issue = ''; store.begin();
    const results = await Promise.allSettled(connections.map((provider) => store.session.client.discoverModels(provider.id)));
    store.merge(results.flatMap((result) => result.status === 'fulfilled' ? [result.value] : []));
    if (results.some((result) => result.status === 'rejected')) issue = 'Couldn’t load some models.';
    store.end(); working = false;
  }

  function edit(provider?: Provider) {
    original = provider;
    const preset = provider?.type ?? currentType ?? 'custom';
    const labels: Record<string, string> = { ollama: 'Ollama', lmstudio: 'LM Studio', elevenlabs: 'ElevenLabs', custom: 'Custom' };
    const urls: Record<string, string> = { ollama: 'http://127.0.0.1:11434/v1', lmstudio: 'http://127.0.0.1:1234/v1', elevenlabs: 'https://api.elevenlabs.io/v1' };
    name = provider?.name ?? labels[preset] ?? 'Custom';
    baseURL = provider?.baseURL ?? urls[preset] ?? '';
    enabled = provider?.enabled ?? true;
    models = provider?.models.map((model) => ({ ...model })) ?? [];
    key = ''; query = ''; issue = ''; page = 'form';
  }

  function addModel() {
    const id = modelID.trim();
    if (id && !models.some((model) => model.id === id)) models.push({ id, tags: ['text'], enabled: true });
    modelID = '';
  }

  async function refreshForm() {
    if (!original || working) return;
    working = true; issue = ''; store.begin();
    try {
      const provider = await store.session.client.discoverModels(original.id);
      original = provider;
      models = provider.models.map((model) => ({ ...model }));
      store.merge([provider]);
    } catch { issue = 'Couldn’t load models.'; }
    finally { store.end(); working = false; }
  }

  async function saveForm() {
    if (working || !name.trim()) return;
    if (original && !['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(original.type)) {
      working = true; issue = '';
      try { store.merge([await store.session.client.renameProvider(original.id, name.trim())]); page = 'provider'; }
      catch { issue = 'Couldn’t save this name.'; }
      finally { working = false; }
      return;
    }
    let url: URL;
    try { url = new URL(baseURL.trim()); } catch { issue = 'Enter an HTTP or HTTPS provider URL.'; return; }
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) { issue = 'Enter an HTTP or HTTPS provider URL without credentials.'; return; }
    const preset = original?.type ?? (currentType || 'custom');
    const id = original?.id ?? `${preset}-${crypto.randomUUID()}`;
    const provider: Provider = { id, type: preset, name: name.trim(), baseURL: baseURL.trim(), enabled, order: original?.order ?? 0, models: models.map((model) => ({ ...model })), headers: original?.headers, sessionHeader: original?.sessionHeader };
    const secret = key; key = ''; working = true; issue = ''; store.begin();
    try {
      let saved = await store.session.client.saveProvider(provider, !original);
      original = saved; store.merge([saved]);
      if (secret) { await store.session.client.setProviderKey(id, secret); saved = { ...saved, status: 'api_key', keyHint: '••••••••' }; original = saved; store.merge([saved]); }
      if (!models.length && saved.type !== 'elevenlabs') {
        try { saved = await store.session.client.discoverModels(id); store.merge([saved]); }
        catch { issue = 'Saved, but couldn’t load its models.'; return; }
      }
      onBack();
    } catch { issue = 'Couldn’t finish saving this provider.'; }
    finally { store.end(); working = false; void store.refresh(); }
  }
</script>

<div class="panel" role="dialog" aria-label={title} tabindex="-1" onclick={(event) => event.stopPropagation()} onkeydown={(event) => { if (event.key === 'Escape') { event.stopPropagation(); if (removing) removing = undefined; else back(); } }}>
  <div class="title">{title}</div>
  <button class="back glass" aria-label="Back" disabled={working || sending} onclick={back}>‹</button>
  <button class="close glass" aria-label="Close settings" disabled={working} onclick={close}>×</button>
  <div class="content">
    {#if page === 'catalog'}
      <div class="search"><input aria-label="Search providers" placeholder="Search providers" bind:value={query} /></div>
      <div class="categories" role="group" aria-label="Provider category">{#each [{ id: 'all', label: 'All' }, { id: 'api_key', label: 'API key' }, { id: 'oauth', label: 'Sign in' }] as item (item.id)}<button aria-pressed={category === item.id} onclick={() => { category = item.id as typeof category; query = ''; }}>{item.label}</button>{/each}<button aria-label="Add custom provider" onclick={() => edit()}>+</button></div>
      <div class="rows">{#each matches as option (`${option.type}/${option.auth}`)}<button class="catalog-row" aria-label="{option.name} · {option.label}" onclick={() => void connect(option)}><span>{option.name}<small>{option.label !== option.name ? option.label : ''}</small></span><span aria-hidden="true">›</span></button>{:else}{#if store.options}<div class="empty">No providers found.</div>{/if}{/each}</div>
    {:else if page === 'login'}
      {#each login?.events ?? [] as event, index (index)}
        <div class="rows event">{#if event.message}<div>{event.message}</div>{/if}{#if event.instructions}<div>{event.instructions}</div>{/if}{#if event.userCode}<div class="line"><span>Code</span><code>{event.userCode}</code></div>{/if}{#if event.url || event.verificationUri}<div class="line"><button class="bordered" onclick={() => void openLink((event.url ?? event.verificationUri)!)}>Open</button><button class="bordered" aria-label={event.userCode ? 'Copy code' : 'Copy link'} onclick={() => void copy((event.userCode ?? event.url ?? event.verificationUri)!)}>Copy</button></div>{/if}</div>
      {/each}
      {#if login?.prompt}
        <div class="rows">
          {#if login.prompt.type !== 'secret'}<div class="line">{login.prompt.message}</div>{/if}
          {#if login.prompt.type === 'select'}{#each login.prompt.options ?? [] as option (option.id)}<button class="catalog-row" disabled={sending} onclick={() => void submit(option.id)}><span>{option.label}<small>{option.description ?? ''}</small></span></button>{/each}
          {:else}<div class="line"><label for="login-answer">{login.prompt.type === 'secret' ? 'API key' : (login.prompt.placeholder ?? 'Answer')}</label><input id="login-answer" type={login.prompt.type === 'secret' ? 'password' : 'text'} autocomplete="off" placeholder={login.prompt.type === 'secret' ? 'Enter API key' : (login.prompt.placeholder ?? '')} bind:value={answer} disabled={sending} onkeydown={(event) => { if (event.key === 'Enter' && answer.trim()) void submit(answer); }} /></div>{/if}
        </div>
        {#if login.prompt.type !== 'select'}<button class="action glass" disabled={sending || !answer.trim()} onclick={() => void submit(answer)}>Continue</button>{/if}
      {:else if !login?.done && !issue}<div class="empty" role="status">Connecting provider</div>{/if}
    {:else if page === 'provider'}
      <section><div class="rows"><label class="line"><span>Enabled</span><input type="checkbox" checked={connections.some((provider) => provider.enabled)} disabled={working} onchange={(event) => void update({ enabled: event.currentTarget.checked })} /></label></div></section>
      <section><h2>Models <button aria-label="Refresh models" title="Refresh models" disabled={working} onclick={() => void refreshModels()}>↻</button></h2><div class="rows">{#if allModels.length}<div class="line"><input aria-label="Search models" placeholder="Search models" bind:value={query} /></div><label class="line"><span>All models</span><input type="checkbox" checked={allModels.every((model) => model.enabled)} disabled={working} onchange={(event) => void update({ models: Object.fromEntries(allModels.map((model) => [model.id, event.currentTarget.checked])) })} /></label>{#each allModels.filter((model) => model.id.toLowerCase().includes(query.toLowerCase())) as model (model.id)}<label class="line"><span>{model.id}</span><input type="checkbox" checked={model.enabled} disabled={working} onchange={(event) => void update({ models: { [model.id]: event.currentTarget.checked } })} /></label>{:else}<div class="empty">No models found.</div>{/each}{:else}<div class="empty">No models yet.</div>{/if}</div></section>
      {#each ['api_key', 'oauth'] as auth (auth)}
        {@const items = connections.filter((provider) => auth === 'api_key' ? provider.status === 'api_key' : provider.status !== 'api_key')}
        {@const option = store.options?.find((item) => item.type === currentType && item.auth === auth)}
        {#if items.length || option}<section><h2>{auth === 'api_key' ? 'API keys' : 'Accounts'}{#if option}<button aria-label={auth === 'api_key' ? 'Add API key' : 'Add account'} onclick={() => void connect(option)}>+</button>{/if}</h2><div class="rows">{#each items as provider (provider.id)}<div class="line connection"><span>{provider.name}{#if auth === 'api_key'}<small>{provider.keyHint ?? '••••••••'}</small>{/if}</span><span class="controls"><button class="bordered" aria-label="Edit {provider.name}" disabled={working} onclick={() => edit(provider)}>Edit</button><button class="bordered" aria-label="Remove {provider.name}" disabled={working} onclick={() => (removing = provider)}>Remove</button></span></div>{:else}<div class="empty">{auth === 'api_key' ? 'No API keys yet.' : 'No accounts yet.'}</div>{/each}</div></section>{/if}
      {/each}
    {:else}
      <section><h2>{original && !['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(original.type) ? 'Details' : 'Connection'}{#if original && ['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(original.type)}<button aria-label="Add connection" title="Add connection" onclick={() => edit()}>+</button>{/if}</h2><div class="rows"><div class="line"><label for="provider-name">Name</label><input id="provider-name" placeholder="Enter name" bind:value={name} /></div>{#if !original || ['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(original.type)}<div class="line"><label for="provider-url">Base URL</label><input id="provider-url" placeholder="https://api.example.com/v1" bind:value={baseURL} /></div>{#if original?.status === 'api_key'}<div class="line"><span>API key</span><span class="muted">{original.keyHint ?? '••••••••'}</span></div>{:else}<div class="line"><label for="provider-key">API key</label><input id="provider-key" type="password" autocomplete="off" placeholder="Enter API key" bind:value={key} /></div>{/if}{#if original}<label class="line"><span>Enabled</span><input type="checkbox" bind:checked={enabled} /></label>{/if}{:else if original.status === 'api_key'}<div class="line"><span>API key</span><span class="muted">{original.keyHint ?? '••••••••'}</span></div>{/if}</div></section>
      {#if original && ['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(original.type) && connections.length > 1}<section><h2>Connections</h2><div class="rows">{#each connections as connection (connection.id)}<button class="catalog-row" aria-label="Select {connection.name}" aria-pressed={connection.id === original.id} onclick={() => edit(connection)}><span>{connection.name}</span>{#if connection.id === original.id}<span aria-hidden="true">✓</span>{/if}</button>{/each}</div></section>{/if}
      {#if !original || ['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(original.type)}<section><h2>Models {#if original && original.type !== 'elevenlabs'}<button aria-label="Refresh models" title="Refresh models" disabled={working} onclick={() => void refreshForm()}>↻</button>{/if}</h2><div class="rows">{#if models.length}<div class="line"><input aria-label="Search models" placeholder="Search models" bind:value={query} /></div><label class="line"><span>All models</span><input type="checkbox" checked={models.every((model) => model.enabled)} onchange={(event) => { for (const model of models) model.enabled = event.currentTarget.checked; }} /></label>{/if}{#each models.filter((model) => model.id.toLowerCase().includes(query.toLowerCase())) as model (model.id)}<label class="line"><span>{model.id}</span><input type="checkbox" bind:checked={model.enabled} /></label>{:else}{#if query.trim()}<div class="empty">No models found.</div>{/if}{/each}{#if original?.type !== 'elevenlabs' && currentType !== 'elevenlabs'}<div class="line"><label for="model-id">Model ID</label><input id="model-id" placeholder="Enter model ID" bind:value={modelID} onkeydown={(event) => { if (event.key === 'Enter') addModel(); }} /><button class="bordered" disabled={!modelID.trim()} onclick={addModel}>Add</button></div>{/if}</div></section>{/if}
      {#if original && ['ollama', 'lmstudio', 'elevenlabs', 'custom'].includes(original.type)}<button class="delete" disabled={working} onclick={() => (removing = original)}>Delete</button>{/if}
      <button class="action glass" disabled={working || !name.trim()} onclick={() => void saveForm()}>Save</button>
    {/if}
    {#if issue}<p role="status">{issue}</p>{/if}
  </div>
  {#if removing}<div class="confirm" role="alertdialog" aria-label="Remove connection"><div>Remove {removing.name}?</div><div class="controls"><button class="bordered" onclick={() => (removing = undefined)}>Cancel</button><button class="bordered" onclick={() => void remove(removing!)}>Remove</button></div></div>{/if}
</div>

<style>
  .panel { position: relative; width: 620px; height: 600px; max-width: calc(100vw - 40px); max-height: calc(100vh - 60px); background: var(--window-bg); border-radius: var(--radius-panel); box-shadow: 0 20px 50px #0004; overflow: hidden; }
  .title { position: absolute; top: 30px; left: 60px; right: 60px; text-align: center; font-size: 13px; font-weight: 600; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .back, .close { position: absolute; top: 22px; width: 36px; height: 36px; border-radius: 50%; z-index: 2; }
  .back { left: 18px; font-size: 24px; }.close { right: 18px; font-size: 22px; }
  .content { position: absolute; inset: 74px 24px 20px; overflow-y: auto; font-size: 13px; }
  section { margin-bottom: 22px; } h2 { display: flex; align-items: center; justify-content: space-between; margin: 0 0 10px; font-size: 13px; font-weight: 600; } h2 button { width: 24px; height: 24px; font-size: 16px; }
  .rows { border-radius: 10px; background: var(--sidebar-bg); overflow: hidden; margin-bottom: 12px; }
  .line, .catalog-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 40px; padding: 0 12px; box-sizing: border-box; }
  .line + .line, .catalog-row + .catalog-row { border-top: 1px solid var(--hairline); }
  .catalog-row { width: 100%; text-align: left; }.catalog-row:hover { background: var(--row-hover); }
  .line span:first-child, .catalog-row span:first-child { min-width: 0; overflow-wrap: anywhere; }
  small { display: block; margin-top: 2px; color: var(--text-muted); font-size: 11px; }.muted { color: var(--text-muted); }
  input:not([type="checkbox"]) { min-width: 0; flex: 1; border: 0; background: transparent; color: var(--text); font: inherit; text-align: right; outline: none; } .search input { text-align: left; }
  .search { display: flex; padding: 0 14px; min-height: 36px; border-radius: 18px; background: var(--sidebar-bg); }
  .categories { display: flex; gap: 8px; margin: 12px 0; }.categories button { padding: 6px 12px; border-radius: 14px; }.categories button[aria-pressed="true"] { background: var(--row-selected); }.categories button:last-child { margin-left: auto; }
  .event { padding: 12px; display: grid; gap: 8px; }.event .line { padding: 0; }
  .controls { display: flex; gap: 8px; flex: none; }.bordered { border: 1px solid var(--hairline); border-radius: 6px; padding: 5px 10px; }.bordered:hover { background: var(--row-hover); }
  .action { display: block; margin: 20px 0 0 auto; padding: 6px 14px; border-radius: 16px; }.delete { color: var(--text-muted); }.empty { min-height: 40px; display: grid; place-items: center; }
  .confirm { position: absolute; inset: 0; display: flex; flex-direction: column; justify-content: center; align-items: center; gap: 18px; background: var(--window-bg); }
  p { margin: 8px 2px; color: var(--text-muted); font-size: 11px; } button:disabled { opacity: .4; }
  @media (max-width: 420px) { .connection { align-items: flex-start; flex-direction: column; padding: 10px 12px; } }
</style>
