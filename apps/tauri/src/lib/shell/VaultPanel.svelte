<script lang="ts">
  import { onMount } from 'svelte';
  import type { Session } from '../session.svelte';
  import type { VaultItem, VaultItemKind, VaultStatus, VaultSummary } from '../api/contract';
  import { isOffline, reason } from '../api/errors';

  let { session }: { session: Session } = $props();
  const categories: [string, string][] = [['all', 'All'], ['login', 'Logins'], ['card', 'Cards'], ['api', 'Secrets'], ['totp', 'TOTP'], ['note', 'Notes'], ['recovery', 'Recovery codes'], ['wifi', 'Wi-Fi'], ['passkey', 'Passkeys']];
  const kinds: [VaultItemKind, string][] = [['login', 'Login'], ['card', 'Card'], ['api', 'Secret'], ['totp', 'TOTP'], ['note', 'Note'], ['recovery', 'Recovery codes'], ['wifi', 'Wi-Fi'], ['passkey', 'Passkey']];
  const fieldMap: Record<string, [string, string][]> = {
    login: [['username', 'Username'], ['password', 'Password'], ['totp', 'TOTP seed or URI'], ['notes', 'Notes']],
    api: [['token', 'Secret'], ['notes', 'Notes']],
    card: [['holder', 'Cardholder'], ['number', 'Card number'], ['expiry', 'Expiry'], ['cvc', 'Security code'], ['pin', 'PIN'], ['notes', 'Notes']],
    totp: [['username', 'Username'], ['totp', 'TOTP seed or URI'], ['notes', 'Notes']],
    note: [['notes', 'Notes']],
    recovery: [['username', 'Username'], ['notes', 'Notes']],
    wifi: [['ssid', 'Network'], ['security', 'Security'], ['username', 'Username'], ['password', 'Password'], ['notes', 'Notes']],
    passkey: [['username', 'Username'], ['rpID', 'Relying party'], ['credentialID', 'Credential ID'], ['userHandle', 'User handle'], ['privateKey', 'Private key'], ['notes', 'Notes']],
  };

  let status = $state<VaultStatus>();
  let items = $state<VaultSummary[]>([]);
  let query = $state('');
  let category = $state('all');
  let draft = $state<VaultItem>();
  let saved = $state<Set<string>>(new Set());
  let savedCodes = $state<Set<string>>(new Set());
  let busy = $state(false);
  let issue = $state('');
  let imported = $state<{ added: number; skipped: number }>();
  let importing = $state(false);
  let file = $state<File>();
  let keyFile = $state<File>();
  let password = $state('');
  let fileInput = $state<HTMLInputElement>();
  let keyInput = $state<HTMLInputElement>();
  let addOpen = $state(false);

  const results = $derived(items.filter((item) => (category === 'all' || item.kind === category)
    && (!query.trim() || `${item.title} ${item.origin}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()))));
  const summary = $derived(`${results.length} ${query.trim() ? (results.length === 1 ? 'result' : 'results') : (results.length === 1 ? 'item' : 'items')}`);
  const fields = $derived(draft ? fieldMap[draft.kind] ?? [] : []);
  const showCodes = $derived(!!draft && (draft.codes.length > 0 || ['login', 'recovery'].includes(draft.kind)));
  const kdbx = $derived(!!file && (/\.kdbx$/i.test(file.name)));

  onMount(() => { void load(); });

  async function load() {
    try {
      const [nextStatus, nextItems] = await Promise.all([session.client.vaultStatus(), session.client.vaultItems()]);
      status = nextStatus;
      items = nextItems;
      issue = '';
    } catch (error) {
      status = undefined;
      issue = isOffline(error) ? 'Connect to open the vault.' : reason(error, 'Couldn’t open the vault.');
    }
  }

  function add(kind: VaultItemKind) {
    addOpen = false;
    query = '';
    category = 'all';
    draft = { id: crypto.randomUUID(), kind, title: '', url: '', fields: {}, codes: [], revision: 0, removed: [] };
    saved = new Set();
    savedCodes = new Set();
    issue = '';
  }

  async function open(id: string) {
    if (busy) return;
    busy = true;
    issue = '';
    try {
      const item = await session.client.vaultItem(id);
      draft = item;
      saved = new Set(Object.keys(item.fields));
      savedCodes = new Set(item.codes.map((code) => code.id));
    } catch (error) { issue = reason(error, 'Couldn’t open this vault entry.'); }
    finally { busy = false; }
  }

  function close() {
    draft = undefined;
    issue = '';
    importing = false;
    file = undefined;
    keyFile = undefined;
    password = '';
  }

  async function save() {
    if (!draft || busy) return;
    if (!draft.title.trim()) { issue = 'Enter a title for this entry.'; return; }
    busy = true;
    issue = '';
    try {
      const value = await session.client.vaultSaveItem($state.snapshot(draft));
      items = [...items.filter((item) => item.id !== value.id), { id: value.id, kind: value.kind, title: value.title, origin: value.url, fields: Object.keys(value.fields), revision: value.revision }];
      close();
    } catch (error) { issue = isOffline(error) ? 'You’re offline, so this wasn’t saved.' : reason(error, 'Couldn’t save this vault entry.'); }
    finally { busy = false; }
  }

  async function remove() {
    if (!draft || busy || draft.revision < 1) return;
    busy = true;
    issue = '';
    try {
      await session.client.vaultDeleteItem(draft.id, draft.revision);
      items = items.filter((item) => item.id !== draft?.id);
      close();
    } catch (error) { issue = reason(error, 'Couldn’t delete this vault entry.'); }
    finally { busy = false; }
  }

  function field(key: string) { return draft?.fields[key] ?? ''; }
  function editField(key: string, value: string) {
    if (!draft) return;
    draft.fields[key] = value;
    if (value) draft.removed = draft.removed.filter((name) => name !== key);
  }
  function clearField(key: string) {
    if (!draft) return;
    draft.fields[key] = '';
    if (!draft.removed.includes(key)) draft.removed.push(key);
  }
  function addCode() { if (draft) draft.codes = [...draft.codes, { id: crypto.randomUUID(), value: '', used: false }]; }
  function removeCode(id: string) { if (draft) draft.codes = draft.codes.filter((code) => code.id !== id); }

  function startImport() { importing = true; draft = undefined; issue = ''; }
  function choose(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    file = input.files?.[0];
    input.value = '';
  }
  function chooseKey(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    keyFile = input.files?.[0];
    input.value = '';
  }

  async function runImport() {
    if (busy) return;
    if (!file) { issue = 'Choose a file to import.'; return; }
    if (kdbx && !password && !keyFile) { issue = 'Enter the KDBX master password or choose a key file.'; return; }
    busy = true;
    issue = '';
    try {
      let body: Blob | FormData;
      if (kdbx) {
        const form = new FormData();
        form.append('file', file, file.name);
        if (password) form.append('password', password);
        if (keyFile) form.append('keyfile', keyFile, keyFile.name);
        body = form;
      } else body = file;
      imported = await session.client.vaultImport(body);
      await load();
      file = undefined;
      keyFile = undefined;
      password = '';
      importing = false;
    } catch (error) { issue = isOffline(error) ? 'You’re offline, so nothing was imported.' : reason(error, 'Couldn’t import this file.'); }
    finally { busy = false; }
  }

  function icon(kind: string) {
    return kind === 'card' ? '▣' : kind === 'api' ? '{}' : kind === 'note' ? '≡' : kind === 'wifi' ? '◔' : kind === 'totp' ? '◷' : '⌘';
  }
</script>

<div class="vault">
  {#if status && !status.available}
    <div class="empty" role="status">The vault is unavailable. Check the server’s secure key storage.</div>
  {:else if draft}
    <div class="editor">
      <div class="toolbar">
        <button class="back" aria-label="Back to vault" onclick={close}>‹</button>
        <span class="editor-title">{draft.revision > 0 ? 'Edit entry' : 'New entry'}</span>
        <span class="spacer"></span>
        {#if draft.revision > 0}<button class="danger" disabled={busy} onclick={() => void remove()}>Delete</button>{/if}
        <button class="primary" disabled={busy || !draft.title.trim()} onclick={() => void save()}>{busy ? 'Saving…' : 'Save'}</button>
      </div>
      <div class="form">
        <label><span>Title</span><input aria-label="Vault title" bind:value={draft.title} disabled={busy} /></label>
        <label><span>Website</span><input aria-label="Vault website" bind:value={draft.url} disabled={busy} /></label>
        {#each fields as [key, label] (key)}
          <label><span>{label}</span><div class="field"><input aria-label={label} type="password" autocomplete="off" spellcheck="false" disabled={busy} value={field(key)} placeholder={saved.has(key) && !draft.removed.includes(key) ? 'Saved' : 'Not set'} oninput={(event) => editField(key, event.currentTarget.value)} />{#if saved.has(key) && !draft.removed.includes(key)}<button class="clear" aria-label="Clear {label}" disabled={busy} onclick={() => clearField(key)}>−</button>{/if}</div></label>
        {/each}
        {#if showCodes}
          <div class="codes-head"><span>Recovery codes</span><button disabled={busy} onclick={addCode}>Add code</button></div>
          {#each draft.codes as code, index (code.id)}
            <div class="code"><input aria-label="Recovery code" type="password" autocomplete="off" placeholder={savedCodes.has(code.id) ? 'Saved' : 'Recovery code'} value={code.value} disabled={busy} oninput={(event) => { if (draft) draft.codes[index] = { ...code, value: event.currentTarget.value }; }} /><label class="used"><input type="checkbox" bind:checked={code.used} disabled={busy} /> Used</label><button aria-label="Remove recovery code" disabled={busy} onclick={() => removeCode(code.id)}>−</button></div>
          {/each}
        {/if}
      </div>
    </div>
  {:else if importing}
    <div class="editor">
      <div class="toolbar"><button class="back" aria-label="Back to vault" onclick={close}>‹</button><span class="editor-title">Import</span><span class="spacer"></span><button class="primary" disabled={busy || !file} onclick={() => void runImport()}>{busy ? 'Importing…' : 'Import'}</button></div>
      <div class="form">
        <input class="hidden" bind:this={fileInput} type="file" accept=".csv,.json,.kdbx,text/csv,application/json" aria-label="Choose vault import" onchange={choose} />
        <input class="hidden" bind:this={keyInput} type="file" aria-label="Choose KDBX key file" onchange={chooseKey} />
        <div class="import-row"><button disabled={busy} onclick={() => fileInput?.click()}>{file ? 'Change file' : 'Choose file'}</button>{#if file}<span>{file.name}</span>{/if}</div>
        {#if kdbx}<input aria-label="KDBX password" type="password" placeholder="KDBX master password" autocomplete="off" bind:value={password} disabled={busy} /><div class="import-row"><button disabled={busy} onclick={() => keyInput?.click()}>Choose key file</button>{#if keyFile}<span>{keyFile.name}</span>{/if}</div>{/if}
        {#if imported}<p class="note" role="status">{imported.added} imported · {imported.skipped} already present</p>{/if}
      </div>
    </div>
  {:else}
    <div class="toolbar">
      <div class="search"><span aria-hidden="true">⌕</span><input bind:value={query} aria-label="Search vault" placeholder="Search vault" /></div>
      <button class="add" aria-label="Add vault item" aria-expanded={addOpen} disabled={busy || !status} onclick={() => (addOpen = !addOpen)}>+</button>
    </div>
    {#if addOpen}
      <div class="add-menu" role="menu" aria-label="Add vault item">
        {#each kinds as [kind, label] (kind)}<button role="menuitem" onclick={() => add(kind)}>{label}</button>{/each}
        <button role="menuitem" onclick={startImport}>Import</button>
      </div>
    {/if}
    <div class="categories" role="group" aria-label="Vault category">
      {#each categories as [id, label] (id)}<button aria-pressed={category === id} class:on={category === id} onclick={() => (category = id)}>{label}</button>{/each}
    </div>
    <p class="summary" aria-label="Vault item count">{summary}</p>
    {#if !results.length}<div class="empty">{query.trim() ? 'No results found.' : 'No items yet.'}</div>
    {:else}
      <div class="list">
        {#each results as item (item.id)}
          <button class="item" aria-label={item.title} disabled={busy} onclick={() => void open(item.id)}>
            <span class="glyph" aria-hidden="true">{icon(item.kind)}</span>
            <span class="details"><span>{item.title}</span>{#if item.origin}<small>{item.origin}</small>{/if}</span>
            <span class="chevron" aria-hidden="true">›</span>
          </button>
        {/each}
      </div>
    {/if}
    {#if imported}<p class="note" role="status">{imported.added} imported · {imported.skipped} already present</p>{/if}
  {/if}
  {#if issue}<p class="issue" role="status">{issue}</p>{/if}
</div>

<style>
  .vault { flex: 1; min-height: 0; display: flex; flex-direction: column; gap: 10px; position: relative; }
  .toolbar { display: flex; align-items: center; gap: 8px; }
  .search { flex: 1; min-width: 0; display: flex; align-items: center; gap: 6px; height: 34px; padding: 0 11px; border-radius: 17px; background: var(--glass-field); border: 0.5px solid var(--glass-border); color: var(--text-muted); }
  .search input { flex: 1; min-width: 0; border: 0; outline: 0; background: none; color: var(--text); font: inherit; font-size: 12px; }
  .add { width: 34px; height: 34px; flex: none; border-radius: 50%; background: var(--glass-field); border: 0.5px solid var(--glass-border); font-size: 19px; }
  .add-menu { position: absolute; right: 0; top: 42px; z-index: 5; width: 180px; padding: 4px; border: 1px solid var(--hairline); border-radius: 11px; background: var(--window-bg); box-shadow: 0 8px 24px rgba(0,0,0,.18); }
  .add-menu button { display: flex; width: 100%; min-height: 27px; padding: 0 8px; border-radius: 6px; text-align: left; font-size: 12px; }
  .add-menu button:hover, .add-menu button:focus-visible { background: var(--row-hover); }
  .categories { display: flex; gap: 4px; overflow-x: auto; scrollbar-width: none; padding-bottom: 1px; }
  .categories::-webkit-scrollbar { display: none; }
  .categories button { flex: none; padding: 6px 9px; border-radius: 14px; font-size: 11px; color: var(--text-muted); }
  .categories button.on { background: var(--row-selected); color: var(--text); }
  .summary { margin: 0 3px; color: var(--text-muted); font-size: 11px; }
  .list { flex: 1; min-height: 0; overflow: auto; border-radius: 12px; background: var(--window-bg); padding: 4px; }
  .item { display: flex; align-items: center; gap: 9px; width: 100%; min-height: 46px; padding: 6px 8px; border-radius: 8px; text-align: left; }
  .item:hover { background: var(--row-hover); }
  .glyph { flex: none; width: 30px; height: 30px; display: grid; place-items: center; border-radius: 8px; background: var(--row-hover); color: var(--text-muted); font-size: 13px; }
  .details { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .details span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
  small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-muted); font-size: 10px; }
  .chevron { color: var(--text-muted); font-size: 16px; }
  .empty { flex: 1; display: grid; place-items: center; color: var(--text-muted); font-size: 12px; text-align: center; }
  .editor { flex: 1; min-height: 0; display: flex; flex-direction: column; gap: 10px; }
  .editor-title { font-size: 13px; font-weight: 600; }
  .back { width: 28px; height: 28px; border-radius: 7px; font-size: 20px; color: var(--text-muted); }
  .spacer { flex: 1; }
  .form { flex: 1; min-height: 0; overflow: auto; display: flex; flex-direction: column; gap: 8px; padding: 10px; border-radius: 12px; background: var(--window-bg); }
  .form label { display: grid; grid-template-columns: 90px minmax(0, 1fr); align-items: center; gap: 8px; font-size: 11px; color: var(--text-muted); }
  input:not([type='checkbox']) { box-sizing: border-box; width: 100%; border: 1px solid var(--hairline); border-radius: 6px; padding: 6px 8px; background: var(--window-bg); color: var(--text); font: inherit; font-size: 12px; outline: none; }
  input:focus-visible { border-color: var(--focus-ring); box-shadow: 0 0 0 1px var(--focus-ring); }
  .field { position: relative; display: flex; }
  .field input { padding-right: 25px; }
  .clear { position: absolute; right: 2px; top: 2px; width: 22px; height: 22px; border-radius: 5px; color: var(--text-muted); }
  .clear:hover { background: var(--row-hover); }
  .codes-head { display: flex; align-items: center; justify-content: space-between; font-size: 11px; color: var(--text-muted); }
  .codes-head button, .code button, .import-row button, .toolbar button:not(.back) { min-height: 27px; padding: 0 9px; border-radius: 6px; border: 1px solid var(--hairline); background: var(--window-bg); font-size: 11px; }
  .code { display: flex; align-items: center; gap: 5px; }
  .used { display: flex; align-items: center; gap: 3px; white-space: nowrap; font-size: 10px; color: var(--text-muted); }
  .primary { background: var(--text) !important; color: var(--window-bg); border-color: transparent !important; }
  .danger { color: var(--danger); }
  .import-row { display: flex; align-items: center; gap: 8px; min-width: 0; font-size: 11px; color: var(--text-muted); }
  .import-row span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .hidden { display: none; }
  .note, .issue { margin: 0 2px; color: var(--text-muted); font-size: 11px; }
  button:disabled { opacity: 0.5; }
</style>
