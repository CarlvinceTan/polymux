<script lang="ts">
  import { untrack } from 'svelte';
  import type { CredentialRequest, Message } from '../api/contract';
  import type { Session } from '../session.svelte';
  import { isOffline, reason } from '../api/errors';

  let { request, message, session }: { request: CredentialRequest; message: Message; session: Session } = $props();
  let value = $state('');
  let status = $state(untrack(() => request.status));
  let saving = $state(false);
  let issue = $state('');

  const isKey = $derived(request.kind === 'ssh');
  const pending = $derived(status === 'pending');
  const lifetime = $derived.by(() => {
    const lines: string[] = [];
    if (request.ephemeral) lines.push('Kept in memory only.');
    if (request.expiresAt) {
      const date = new Date(request.expiresAt);
      if (Number.isFinite(date.getTime())) lines.push(`Expires ${date.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })}.`);
    }
    return lines.join(' ');
  });

  async function submit(skip = false) {
    if (saving || !pending) return;
    if (!skip && !value.trim()) { issue = 'Enter a credential, or choose Skip.'; return; }
    saving = true;
    issue = '';
    const previous = status;
    status = skip ? 'skipped' : 'saved';
    try {
      const saved = await session.client.answerCredential(message.threadID, message.id, skip ? undefined : value, skip);
      status = saved.parts.find((part) => part.kind === 'credential')?.credential?.status ?? status;
      value = '';
    } catch (error) {
      status = previous;
      issue = isOffline(error) ? 'You’re offline, so this wasn’t saved.' : reason(error, 'Couldn’t save this credential.');
    } finally {
      saving = false;
    }
  }
</script>

<div class="credential" role="group" aria-label="Credential request: {request.label}">
  <div class="title">
    <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">
      {#if isKey}<path d="M4 12h10M8 8v8M14 12a4 4 0 1 0 4-4 4 4 0 0 0-4 4Z"></path>
      {:else}<circle cx="8" cy="12" r="4"></circle><path d="M12 12h9M17 12v3M20 12v2"></path>{/if}
    </svg>
    <span>{request.label}</span>
  </div>
  <div class="origins">{#each [request.origin, ...(request.origins ?? [])].filter((origin, index, all) => origin && all.indexOf(origin) === index) as origin (origin)}<span>{origin}</span>{/each}</div>
  {#if lifetime}<p class="lifetime">{lifetime}</p>{/if}

  {#if pending}
    {#if isKey}
      <textarea bind:value aria-label="SSH private key" placeholder="Private key" disabled={saving} spellcheck="false"></textarea>
    {:else}
      <input bind:value type="password" aria-label="Credential value" placeholder="Credential" autocomplete="off" autocapitalize="off" spellcheck="false" disabled={saving}
        onkeydown={(event) => { if (event.key === 'Enter') void submit(); }} />
    {/if}
    <div class="actions">
      <button class="primary" disabled={saving} onclick={() => void submit()}>{saving ? 'Saving…' : 'Save'}</button>
      <button disabled={saving} onclick={() => void submit(true)}>Skip</button>
    </div>
  {:else}
    <div class="done" role="status">
      <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
        {#if status === 'saved'}<path d="m5 12 4 4 10-10"></path>{:else}<path d="M5 5v6h6M5 11a8 8 0 1 0 3-6"></path>{/if}
      </svg>
      {status === 'saved' ? 'Credential saved' : 'Skipped'}
    </div>
  {/if}
  {#if issue}<p class="issue" role="status">{issue}</p>{/if}
</div>

<style>
  .credential { display: flex; flex-direction: column; gap: 8px; min-width: 230px; max-width: 340px; padding: 2px 0; }
  .title { display: flex; align-items: center; gap: 7px; font-size: 13px; font-weight: 550; }
  .origins { display: flex; flex-wrap: wrap; gap: 4px 8px; color: var(--text-muted); font-size: 10px; }
  .origins span { overflow-wrap: anywhere; }
  .lifetime { margin: 0; color: var(--text-muted); font-size: 10px; }
  input, textarea { box-sizing: border-box; width: 100%; border: 1px solid var(--hairline); border-radius: 7px; padding: 6px 8px; background: var(--window-bg); color: var(--text); font: inherit; font-size: 12px; outline: none; }
  input:focus-visible, textarea:focus-visible { border-color: var(--focus-ring); box-shadow: 0 0 0 1px var(--focus-ring); }
  textarea { min-height: 76px; max-height: 140px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .actions { display: flex; gap: 8px; }
  button { min-height: 28px; padding: 0 10px; border-radius: 7px; border: 1px solid var(--hairline); background: var(--window-bg); font-size: 12px; }
  button:hover:not(:disabled) { background: var(--row-hover); }
  button:disabled { opacity: 0.5; }
  .primary { border-color: transparent; background: var(--text); color: var(--window-bg); }
  .primary:hover:not(:disabled) { background: color-mix(in srgb, var(--text) 85%, transparent); }
  .done { display: flex; align-items: center; gap: 5px; color: var(--text-muted); font-size: 11px; }
  .issue { margin: 0; color: var(--text-muted); font-size: 11px; }
</style>
