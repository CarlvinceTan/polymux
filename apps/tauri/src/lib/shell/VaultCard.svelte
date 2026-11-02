<script lang="ts">
  import { untrack } from 'svelte';
  import type { Message, VaultCard, VaultItem, VaultItemKind } from '../api/contract';
  import type { Session } from '../session.svelte';
  import { isOffline, reason } from '../api/errors';

  let { request, message, session }: { request: VaultCard; message: Message; session: Session } = $props();
  let status = $state(untrack(() => request.status));
  let opened = $state(false);
  let busy = $state(false);
  let issue = $state('');
  let item = $state<VaultItem>();
  let imported = $state<{ added: number; skipped: number }>();
  let file = $state<File>();
  let keyFile = $state<File>();
  let password = $state('');
  let fileInput = $state<HTMLInputElement>();
  let keyInput = $state<HTMLInputElement>();
  let saved = $state<Set<string>>(new Set());
  let savedCodes = $state<Set<string>>(new Set());

  const kdbx = $derived(!!file && /\.kdbx$/i.test(file.name) || !!request.path);
  const importName = $derived(file?.name ?? fileName(request.path));
  const keyName = $derived(keyFile?.name ?? fileName(request.keyPath));
  const hasKey = $derived(!!keyFile || !!request.keyPath);
  const done = $derived(status === 'done');

  function fileName(path?: string): string | undefined {
    if (!path) return;
    const name = path.split('/').pop();
    return name || undefined;
  }

  async function open() {
    if (busy) return;
    opened = true;
    issue = '';
    if (request.action !== 'edit') return;
    busy = true;
    try {
      const value = await session.client.vaultItem(request.itemID);
      item = value;
      saved = new Set(Object.keys(value.fields));
      savedCodes = new Set(value.codes.map((code) => code.id));
    } catch {
      item = { id: request.itemID, kind: request.kind as VaultItemKind, title: request.title, url: request.url, fields: {}, codes: [], revision: 0, removed: [] };
      saved = new Set();
      savedCodes = new Set();
    } finally { busy = false; }
  }

  function close() {
    opened = false;
    busy = false;
    issue = '';
    item = undefined;
    file = undefined;
    keyFile = undefined;
    password = '';
  }

  function field(key: string) {
    return item?.fields[key] ?? '';
  }

  function editField(key: string, value: string) {
    if (!item) return;
    item.fields[key] = value;
    if (value) item.removed = item.removed.filter((name) => name !== key);
  }

  function clearField(key: string) {
    if (!item) return;
    item.fields[key] = '';
    if (!item.removed.includes(key)) item.removed.push(key);
  }

  async function save() {
    if (!item || busy) return;
    if (!item.title.trim()) { issue = 'Enter a title for this entry.'; return; }
    busy = true;
    issue = '';
    try {
      const result = await session.client.saveVaultCard(message.id, $state.snapshot(item));
      item = result;
      status = 'done';
      close();
    } catch (error) {
      issue = isOffline(error) ? 'You’re offline, so this wasn’t saved.' : reason(error, 'Couldn’t save this vault entry.');
    } finally { busy = false; }
  }

  async function runImport() {
    if (busy) return;
    if (!kdbx && !file) { issue = 'Choose a file to import.'; return; }
    if (kdbx && !password && !hasKey) { issue = 'Enter the KDBX master password or choose a key file.'; return; }
    busy = true;
    issue = '';
    try {
      let body: Blob | FormData;
      if (kdbx) {
        const form = new FormData();
        if (file) form.append('file', file, file.name);
        if (password) form.append('password', password);
        if (keyFile) form.append('keyfile', keyFile, keyFile.name);
        body = form;
      } else body = file!;
      imported = await session.client.importVaultCard(message.id, body);
      status = 'done';
      password = '';
      close();
    } catch (error) {
      issue = isOffline(error) ? 'You’re offline, so nothing was imported.' : reason(error, 'Couldn’t import this file.');
    } finally { busy = false; }
  }

  function pick(input?: HTMLInputElement) {
    input?.click();
  }

  function choose(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    file = input.files?.[0];
    input.value = '';
    issue = '';
  }

  function chooseKey(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    keyFile = input.files?.[0];
    input.value = '';
  }

  function addCode() {
    if (!item) return;
    item.codes = [...item.codes, { id: crypto.randomUUID(), value: '', used: false }];
  }

  function removeCode(id: string) {
    if (!item) return;
    item.codes = item.codes.filter((code) => code.id !== id);
  }

  const fields = $derived.by(() => {
    const kind = item?.kind ?? request.kind;
    const entries: [string, string][] = kind === 'login' ? [['username', 'Username'], ['password', 'Password'], ['totp', 'TOTP seed or URI'], ['notes', 'Notes']]
      : kind === 'api' ? [['token', 'Secret'], ['notes', 'Notes']]
      : kind === 'card' ? [['holder', 'Cardholder'], ['number', 'Card number'], ['expiry', 'Expiry'], ['cvc', 'Security code'], ['pin', 'PIN'], ['notes', 'Notes']]
      : kind === 'totp' ? [['username', 'Username'], ['totp', 'TOTP seed or URI'], ['notes', 'Notes']]
      : kind === 'note' ? [['notes', 'Notes']]
      : kind === 'recovery' ? [['username', 'Username'], ['notes', 'Notes']]
      : kind === 'wifi' ? [['ssid', 'Network'], ['security', 'Security'], ['username', 'Username'], ['password', 'Password'], ['notes', 'Notes']]
      : kind === 'passkey' ? [['username', 'Username'], ['rpID', 'Relying party'], ['credentialID', 'Credential ID'], ['userHandle', 'User handle'], ['privateKey', 'Private key'], ['notes', 'Notes']]
      : [];
    return entries;
  });
  const showCodes = $derived(!!item && (item.codes.length > 0 || ['login', 'recovery'].includes(item.kind)));
</script>

<div class="vault-card" role="group" aria-label="Private vault card: {request.title}">
  <div class="title">
    <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><rect x="4" y="10" width="16" height="11" rx="2.5"></rect><path d="M8 10V7a4 4 0 0 1 8 0v3"></path></svg>
    <span>{request.title}</span>
  </div>

  {#if done}
    <div class="done" role="status">
      <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="m5 12 4 4 10-10"></path></svg>
      Completed{#if imported}<span> · {imported.added} imported · {imported.skipped} already present</span>{/if}
    </div>
  {:else if !opened}
    <button class="open" disabled={busy} onclick={() => void open()}>Open</button>
  {:else if request.action === 'edit' && item}
    <div class="section">
      <label><span>Title</span><input aria-label="Vault title" bind:value={item.title} disabled={busy} /></label>
      <label><span>Website</span><input aria-label="Vault website" bind:value={item.url} disabled={busy} /></label>
    </div>
    <div class="section">
      {#each fields as [key, label] (key)}
        <label>
          <span>{label}</span>
          <div class="field">
            <input aria-label={label} type="password" autocomplete="off" spellcheck="false" disabled={busy}
              value={field(key)} placeholder={saved.has(key) && !item.removed.includes(key) ? 'Saved' : 'Not set'}
              oninput={(event) => editField(key, event.currentTarget.value)} />
            {#if saved.has(key) && !item.removed.includes(key)}<button class="clear" aria-label="Clear {label}" disabled={busy} onclick={() => clearField(key)}>−</button>{/if}
          </div>
        </label>
      {/each}
    </div>
    {#if showCodes}
      <div class="section codes">
        <div class="section-head"><span>Recovery codes</span><button disabled={busy} onclick={addCode}>Add code</button></div>
        {#each item.codes as code, index (code.id)}
          <div class="code">
            <input aria-label="Recovery code" type="password" autocomplete="off" placeholder={savedCodes.has(code.id) ? 'Saved' : 'Recovery code'} value={code.value} disabled={busy}
              oninput={(event) => { if (item) item.codes[index] = { ...code, value: event.currentTarget.value }; }} />
            <label class="used"><input type="checkbox" bind:checked={code.used} disabled={busy} /> Used</label>
            <button aria-label="Remove recovery code" disabled={busy} onclick={() => removeCode(code.id)}>−</button>
          </div>
        {/each}
      </div>
    {/if}
    <div class="actions"><button disabled={busy} onclick={close}>Close</button><button class="primary" disabled={busy || !item.title.trim()} onclick={() => void save()}>{busy ? 'Saving…' : 'Save'}</button></div>
  {:else if request.action === 'import'}
    <p class="note">{request.path ? 'Import the server file into the private vault.' : 'Choose a CSV, JSON, or KDBX export.'}</p>
    <input class="hidden" bind:this={fileInput} type="file" accept=".csv,.json,.kdbx,text/csv,application/json" aria-label="Choose vault import" onchange={choose} />
    <input class="hidden" bind:this={keyInput} type="file" aria-label="Choose KDBX key file" onchange={chooseKey} />
    <div class="import-row">
      <button disabled={busy} onclick={() => pick(fileInput)}>{importName ? 'Change file' : 'Choose file'}</button>
      {#if importName}<span class="file-name">{importName}</span>{/if}
    </div>
    {#if kdbx}
      <input aria-label="KDBX password" type="password" placeholder="KDBX master password" autocomplete="off" bind:value={password} disabled={busy} />
      <div class="import-row"><button disabled={busy} onclick={() => pick(keyInput)}>Choose key file</button>{#if keyName}<span class="file-name">{keyName}</span>{/if}</div>
    {/if}
    <div class="actions"><button disabled={busy} onclick={close}>Close</button><button class="primary" disabled={busy || (!kdbx && !file)} onclick={() => void runImport()}>{busy ? 'Importing…' : 'Import'}</button></div>
  {:else}
    <p class="note">This card is unavailable.</p>
  {/if}
  {#if issue}<p class="issue" role="status">{issue}</p>{/if}
</div>

<style>
  .vault-card { display: flex; flex-direction: column; gap: 9px; min-width: 250px; max-width: 360px; padding: 2px 0; }
  .title { display: flex; align-items: center; gap: 7px; font-size: 13px; font-weight: 550; }
  .note { margin: 0; color: var(--text-muted); font-size: 11px; line-height: 1.35; }
  .open { align-self: flex-start; min-height: 28px; padding: 0 12px; border-radius: 7px; border: 1px solid var(--hairline); background: var(--window-bg); font-size: 12px; }
  .section { display: flex; flex-direction: column; gap: 6px; }
  .section label { display: grid; grid-template-columns: 82px minmax(0, 1fr); align-items: center; gap: 8px; font-size: 11px; color: var(--text-muted); }
  .field { position: relative; display: flex; }
  input:not([type='checkbox']) { box-sizing: border-box; width: 100%; border: 1px solid var(--hairline); border-radius: 6px; padding: 5px 7px; background: var(--window-bg); color: var(--text); font: inherit; font-size: 12px; outline: none; }
  input:focus-visible { border-color: var(--focus-ring); box-shadow: 0 0 0 1px var(--focus-ring); }
  .field input { padding-right: 26px; }
  .clear { position: absolute; right: 2px; top: 2px; width: 22px; height: 22px; border-radius: 5px; color: var(--text-muted); }
  .clear:hover { background: var(--row-hover); }
  .section-head { display: flex; align-items: center; justify-content: space-between; color: var(--text-muted); font-size: 11px; }
  .section-head button, .code button, .actions button, .import-row button { min-height: 26px; padding: 0 8px; border-radius: 6px; border: 1px solid var(--hairline); background: var(--window-bg); font-size: 11px; }
  .codes { gap: 5px; }
  .code { display: flex; align-items: center; gap: 5px; }
  .used { display: flex; align-items: center; gap: 3px; font-size: 10px; white-space: nowrap; color: var(--text-muted); }
  .actions { display: flex; justify-content: flex-end; gap: 7px; padding-top: 2px; }
  .primary { background: var(--text); color: var(--window-bg); border-color: transparent; }
  .done { display: flex; align-items: center; gap: 5px; color: var(--text-muted); font-size: 11px; }
  .import-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .file-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-muted); font-size: 11px; }
  .hidden { display: none; }
  .issue { margin: 0; color: var(--text-muted); font-size: 11px; }
  button:disabled { opacity: 0.5; }
</style>
