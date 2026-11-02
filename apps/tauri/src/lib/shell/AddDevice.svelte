<script lang="ts">
  import { onMount } from 'svelte';
  import type { Session } from '../session.svelte';
  import { finish, inviteLink, parseInvite, redeem } from '../api/pairing';
  import { copyText } from '../shared/clipboard';
  import { qrSvgPath } from '../shared/qr';

  let { session, onBack, onClose, onConnect }: {
    session: Session; onBack: () => void; onClose: () => void; onConnect: (address: string) => Promise<void>;
  } = $props();

  let link = $state('');
  let issue = $state('');
  let pasted = $state('');
  let pasteIssue = $state('');
  let joining = $state(false);
  let copyIssue = $state('');
  let controller: AbortController | undefined;
  let alive = true;
  const qr = $derived(link ? qrSvgPath(link) : undefined);
  const canShare = typeof navigator !== 'undefined' && typeof navigator.share === 'function';

  onMount(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function load() {
      try {
        const invite = await session.client.invite();
        if (!alive) return;
        link = inviteLink(invite);
        issue = '';
      } catch {
        if (!alive) return;
        issue = session.online ? "Couldn't create a pairing code." : 'Offline';
      }
      if (alive) timer = setTimeout(load, link ? 120_000 : 5_000);
    }
    void load();
    return () => { alive = false; controller?.abort(); clearTimeout(timer); };
  });

  async function copy() {
    copyIssue = await copyText(link) ? '' : "Couldn't copy the link.";
  }

  async function share() {
    if (!link || !canShare) return;
    try { await navigator.share({ url: link }); } catch { /* Sharing was cancelled. */ }
  }

  async function join() {
    if (joining) return;
    const invite = parseInvite(pasted);
    if (!invite) { pasteIssue = 'Paste a valid Polymux pairing link.'; return; }
    joining = true;
    pasteIssue = '';
    controller = new AbortController();
    try {
      const token = await redeem(invite, controller.signal);
      if (!alive) return;
      await finish(invite, token);
      await onConnect(invite.url);
      if (alive) onClose();
    } catch (error) {
      if (alive && !controller.signal.aborted) pasteIssue = error instanceof Error && error.message
        ? error.message : "Couldn't pair with this server.";
    } finally { if (alive) joining = false; }
  }
</script>

<div class="panel" role="dialog" aria-modal="true" aria-label="Add device" tabindex="-1" onclick={(event) => event.stopPropagation()} onkeydown={(event) => { if (event.key === 'Escape') onClose(); }}>
  <div class="title">Add device</div>
  <button class="back glass" aria-label="Back" onclick={onBack}>‹</button>
  <button class="close glass" aria-label="Close settings" onclick={onClose}>×</button>
  <div class="body">
    <div class="invite">
      {#if qr}
        <svg class="qr" viewBox="0 0 {qr.size} {qr.size}" role="img" aria-label="Pairing QR code" shape-rendering="crispEdges"><path d={qr.path} fill="black"></path></svg>
        <div class="actions"><button class="action glass" onclick={share} disabled={!canShare}>Share</button><button class="action glass" onclick={copy}>Copy link</button></div>
        <span class="hint">Single-use link. Expires in 3 minutes.</span>
        {#if copyIssue}<span class="hint" role="status">{copyIssue}</span>{/if}
      {:else if issue}<span class="hint" role="status">{issue}</span>
      {:else}<span class="hint">Loading…</span>{/if}
    </div>
    <section>
      <h2>Join with a link</h2>
      <input aria-label="Pairing link" placeholder="Paste a pairing link" bind:value={pasted} disabled={joining} onkeydown={(event) => { if (event.key === 'Enter') void join(); }} />
      <button class="action glass" aria-label="Join server" disabled={joining || !pasted.trim()} onclick={() => void join()}>Join</button>
      {#if pasteIssue}<span class="hint" role="status">{pasteIssue}</span>{:else if joining}<span class="hint">Connecting…</span>{/if}
    </section>
  </div>
</div>

<style>
  .panel { position: relative; width: 620px; height: 600px; max-width: calc(100vw - 40px); max-height: calc(100vh - 60px); background: var(--window-bg); border-radius: var(--radius-panel); box-shadow: 0 20px 50px rgba(0,0,0,.25); overflow: hidden; }
  .title { position: absolute; top: 30px; left: 0; right: 0; text-align: center; font-weight: 600; }
  .back, .close { position: absolute; top: 22px; width: 36px; height: 36px; border-radius: 50%; display: grid; place-items: center; z-index: 1; }
  .back { left: 18px; font-size: 24px; }
  .close { right: 18px; font-size: 19px; }
  .body { position: absolute; inset: 74px 24px 24px; overflow-y: auto; }
  .invite { min-height: 310px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 16px; padding: 8px 0; }
  .qr { box-sizing: border-box; width: 220px; height: 220px; padding: 12px; background: white; border-radius: 12px; flex: none; }
  .actions { display: flex; gap: 10px; }
  .action { min-height: 32px; padding: 0 14px; border-radius: 16px; }
  .action:disabled { opacity: .45; }
  .hint { color: var(--text-muted); font-size: 12px; }
  section { display: flex; flex-direction: column; align-items: flex-start; gap: 10px; }
  h2 { font-size: 13px; margin: 0; }
  input { box-sizing: border-box; width: 100%; min-width: 0; padding: 10px 12px; border: 1px solid var(--hairline); border-radius: 10px; background: var(--glass-field); }
</style>
