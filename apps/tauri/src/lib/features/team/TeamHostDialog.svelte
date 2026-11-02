<script lang="ts">
  import {formatTeamHostSetupCode, HOST_PAIRING_CODE_LENGTH, type DevicePairingRequest, type DevicePairingState, type TeamHostDto} from '@polymux/protocol';
  import {onDestroy, onMount, tick} from 'svelte';
  import DeviceCodeInput from './DeviceCodeInput.svelte';
  import DeviceQrScanner from './DeviceQrScanner.svelte';
  import Icon from '../../shared/components/Icon.svelte';
  import {copyText} from '../../shared/clipboard';
  import {deviceTypeIconName} from '../../shared/deviceTypeIcon';
  import {fade} from 'svelte/transition';
  import HostPlatformIcon from './HostPlatformIcon.svelte';
  import {polymuxApi} from '../../api/polymux';
  const api = polymuxApi();
  import {scrollFade} from '../../shared/scrollFade';
  import {qrSvgPath} from '../../shared/qr';

  let HOST_INSTALL_COMMAND = '';
  let deviceState: DevicePairingState = {approvals: [], connectedDevices: [], outgoing: null};
  let deviceError = '';
  let deviceBusy = false;
  let devicePolling = false;
  let scanDevice = false;
  let alive = true;
  async function deviceRequest(request: DevicePairingRequest): Promise<void> {
    if (request.action !== 'state') deviceBusy = true;
    try {
      const next = await api.devices.request(request);
      if (!alive) return;
      deviceState = next; deviceError = next.error ?? '';
      if (next.connected || request.action === 'approve') void onRefreshPairing();
    } catch (error) { if (alive) deviceError = error instanceof Error ? error.message : String(error); }
    finally { deviceBusy = false; }
  }
  async function pollDevices(): Promise<void> {
    if (devicePolling || !alive) return;
    devicePolling = true;
    try { await deviceRequest({action: 'state'}); } finally { devicePolling = false; }
  }
  onMount(() => { void pollDevices(); const timer = setInterval(() => void pollDevices(), 1000); return () => { alive = false; clearInterval(timer); }; });

  export let host: TeamHostDto;
  export let anchor: HTMLButtonElement | null = null;
  export let hosts: TeamHostDto[] = [];
  export let busy = false;
  export let error = '';
  export let onRefreshPairing: () => void | Promise<void> = () => {};
  export let onResetPairing: () => void = () => {};
  export let onRemove: (hostId: string) => void = () => {};
  export let onClose: () => void = () => {};

  let method: 'qr' | 'code' | 'connect' = 'qr';
  let left = 0;
  let top = 0;
  let maxHeight = 0;
  let placed = false;
  const EDGE = 8;
  const GAP = 8;
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const RELEASES_URL = 'https://polymux.com/releases/';
  const platforms = [{id: 'macos', label: 'MacOS'}, {id: 'windows', label: 'Windows'}, {id: 'linux', label: 'Linux'}] as const;
  let code = '';
  let dialog: HTMLDivElement;
  let copying = false;
  let installCopy: 'idle' | 'copied' | 'failed' = 'idle';
  let installCopyTimer: ReturnType<typeof setTimeout> | undefined;
  let codeCopy: 'idle' | 'copied' | 'failed' = 'idle';
  let codeCopyTimer: ReturnType<typeof setTimeout> | undefined;
  let pairingNow = Date.now();
  let pairingRefreshInFlight = false;
  let refreshedPairingExpiry = '';
  $: configuredHosts = hosts.length ? hosts : [host];
  $: localHost = configuredHosts.find((candidate) => candidate.mode === 'local');
  $: mobilePairingQr = localHost?.listeningEndpoint && localHost.pairingCode
    ? qrSvgPath(formatTeamHostSetupCode(localHost.listeningEndpoint, localHost.pairingCode))
    : null;
  $: pairingExpiry = localHost?.pairingExpiresAt ? Date.parse(localHost.pairingExpiresAt) : Number.NaN;
  $: pairingSeconds = Number.isFinite(pairingExpiry)
    ? Math.max(0, Math.ceil((pairingExpiry - pairingNow) / 1000))
    : null;
  $: pairingCountdown = pairingSeconds === null
    ? ''
    : `${Math.floor(pairingSeconds / 60)}:${String(pairingSeconds % 60).padStart(2, '0')}`;

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return {destroy: () => node.remove()};
  }

  function place(): void {
    if (!anchor?.isConnected) { onClose(); return; }
    if (!dialog) return;
    const rect = anchor.getBoundingClientRect();
    const measured = dialog.getBoundingClientRect();
    const width = measured.width || Math.min(310, window.innerWidth - EDGE * 2);
    const height = measured.height;
    const spaceBelow = window.innerHeight - rect.bottom - GAP - EDGE;
    const spaceAbove = rect.top - GAP - EDGE;
    const openBelow = height <= spaceBelow || spaceBelow >= spaceAbove;
    maxHeight = Math.max(0, openBelow ? spaceBelow : spaceAbove);
    const usedHeight = Math.min(height || maxHeight, maxHeight);
    left = Math.max(EDGE, Math.min(
      rect.left + rect.width / 2 - width / 2,
      window.innerWidth - width - EDGE,
    ));
    top = openBelow
      ? rect.bottom + GAP
      : Math.max(EDGE, rect.top - GAP - usedHeight);
    placed = true;
  }

  onMount(() => {
    place();
    // Placement removes visibility:hidden on the next Svelte update.
    void tick().then(() => { if (alive && dialog.isConnected) dialog.focus({preventScroll: true}); });
    const observer = new ResizeObserver(place);
    observer.observe(dialog);
    return () => { observer.disconnect(); };
  });

  function outside(event: PointerEvent): void {
    if (event.target instanceof Node && !dialog.contains(event.target) && !anchor?.contains(event.target)) onClose();
  }

  function focusout(event: FocusEvent): void {
    if (!copying && event.relatedTarget instanceof Node && !dialog.contains(event.relatedTarget) && !anchor?.contains(event.relatedTarget)) onClose();
  }

  function openReleases(): void {
    void api.browser.openExternal(RELEASES_URL);
  }

  onMount(() => {
    const timer = setInterval(() => {
      pairingNow = Date.now();
      const expiry = localHost?.pairingExpiresAt ?? '';
      if (!deviceState.outgoing && !deviceState.approvals.length &&
        expiry && Number.isFinite(pairingExpiry) && pairingNow >= pairingExpiry
        && refreshedPairingExpiry !== expiry && !pairingRefreshInFlight
      ) {
        refreshedPairingExpiry = expiry;
        void refreshPairing();
      }
    }, 250);
    return () => clearInterval(timer);
  });
  onDestroy(() => {
    if (installCopyTimer) clearTimeout(installCopyTimer);
    if (codeCopyTimer) clearTimeout(codeCopyTimer);
  });

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      anchor?.focus({preventScroll: true});
      onClose();
    }
  }

  async function copyPairingCode(): Promise<void> {
    const pairingCode = localHost?.pairingCode;
    if (!pairingCode) return;
    if (codeCopyTimer) clearTimeout(codeCopyTimer);
    copying = true;
    try {
      codeCopy = await copyText(pairingCode) ? 'copied' : 'failed';
    } finally {
      copying = false;
      dialog.focus({preventScroll: true});
    }
    codeCopyTimer = setTimeout(() => codeCopy = 'idle', 1800);
  }

  async function copyHostInstall(): Promise<void> {
    if (installCopyTimer) clearTimeout(installCopyTimer);
    copying = true;
    try {
      const invitation = await api.devices.request({action: 'invitation'});
      HOST_INSTALL_COMMAND = invitation.installCommand ?? '';
      installCopy = await copyText(HOST_INSTALL_COMMAND) ? 'copied' : 'failed';
    } catch { installCopy = 'failed'; }
    finally { copying = false; dialog.focus({preventScroll: true}); }
    installCopyTimer = setTimeout(() => installCopy = 'idle', 2200);
  }

  async function refreshPairing(): Promise<void> {
    if (pairingRefreshInFlight) return;
    pairingRefreshInFlight = true;
    try {
      await onRefreshPairing();
    } finally {
      pairingRefreshInFlight = false;
      pairingNow = Date.now();
    }
  }

</script>

<svelte:window onpointerdown={outside} onresize={onClose} onkeydown={keydown}/>

<div use:portal bind:this={dialog} id="team-host-panel" class="team-host-panel polymux-dropdown-menu" class:placed
  style:left="{left}px" style:top="{top}px" style:max-height={placed ? `${maxHeight}px` : undefined}
  role="dialog" aria-labelledby="team-host-title" tabindex="-1" onfocusout={focusout}
  transition:fade={{duration: reducedMotion ? 0 : 120}}>
  <header><h2 id="team-host-title">Devices</h2><button type="button" aria-label="Close Devices" data-tooltip="none" onclick={() => { anchor?.focus({preventScroll: true}); onClose(); }}><Icon name="close" size={16}/></button></header>
  <div class="pair-stage">
    {#if deviceState.approvals.length}
      {@const approval = deviceState.approvals[0]}
      <strong>Connect {approval.deviceName}?</strong>
      <small>Select the number shown on your other device.</small>
      <div class="confirmation-choices">{#each approval.choices as number}<button type="button" disabled={deviceBusy} onclick={() => void deviceRequest({action: 'approve', id: approval.id, number})}>{number}</button>{/each}</div>
      <small>Allow this device to access your Polymux workspace.</small>
      <button type="button" disabled={deviceBusy} onclick={() => void deviceRequest({action: 'approve', id: approval.id, number: null})}>Decline</button>
    {:else if deviceState.outgoing}
      <strong>Confirm on {deviceState.outgoing.deviceName}</strong>
      <code class="pair-code">{deviceState.outgoing.number}</code>
      <small>Select this number on your other device.</small>
      <button type="button" onclick={() => void deviceRequest({action: 'cancel'})}>Cancel connection</button>
    {:else if method === 'connect'}
      {#if scanDevice}
        <DeviceQrScanner onCode={(endpoint, code) => { scanDevice = false; void deviceRequest({action: 'start', endpoint, code}); }}/>
        <button type="button" onclick={() => scanDevice = false}>Enter code instead</button>
      {:else}
        <form onsubmit={(event) => { event.preventDefault(); void deviceRequest({action: 'start', code}); }}>
          <p class="code-prompt">Enter the code from your other device</p>
          <DeviceCodeInput bind:value={code} disabled={deviceBusy}/>
          <button type="submit" class="host-primary" disabled={code.length !== HOST_PAIRING_CODE_LENGTH || deviceBusy}>{deviceBusy ? 'Connecting…' : 'Connect Device'}</button>
          <button type="button" onclick={() => scanDevice = true}>Scan QR code</button>
        </form>
      {/if}
    {:else if localHost?.listeningEndpoint && localHost.pairingCode}
      {#if pairingSeconds === 0}
        <Icon name="clock" size={22}/>
        <strong>{pairingRefreshInFlight ? 'Refreshing pairing…' : 'Pairing code expired'}</strong>
        {#if !pairingRefreshInFlight}<button type="button" onclick={() => void refreshPairing()}>Refresh code</button>{/if}
      {:else}
        {#if method === 'qr' && mobilePairingQr}
          <svg class="pair-qr" viewBox="-4 -4 {mobilePairingQr.size + 8} {mobilePairingQr.size + 8}" role="img" aria-label="Polymux pairing QR code">
            <rect x="-4" y="-4" width={mobilePairingQr.size + 8} height={mobilePairingQr.size + 8} fill="#fff"/>
            <path d={mobilePairingQr.path} fill="#000"/>
          </svg>
        {:else}
          <button type="button" class="pair-code" aria-label="Copy pairing code" onclick={() => void copyPairingCode()}>{localHost.pairingCode}</button>
        {/if}
        <small aria-live="polite">{codeCopy === 'copied' ? 'Copied' : codeCopy === 'failed' ? 'Couldn’t copy' : method === 'qr' ? 'Scan with Polymux on your other device' : 'Enter this code to pair'}</small>
        {#if pairingCountdown}<small class="pair-refresh" role="timer">Refreshes in {pairingCountdown}</small>{/if}
      {/if}
    {:else if localHost?.pairedDesktopName}
      <Icon name="check" size={24}/><strong>{localHost.pairedDesktopName}</strong><small>Paired with this computer</small>
      <button type="button" disabled={busy} onclick={onResetPairing}>Reset pairing</button>
    {:else}
      <Icon name="devices" size={24}/><strong>Pairing unavailable</strong>
      <button type="button" disabled={busy || pairingRefreshInFlight} onclick={() => void refreshPairing()}>Try again</button>
    {/if}
  </div>
  <div class="method-switch" role="group" aria-label="Connection method">
    {#each [{id: 'qr', label: 'QR code'}, {id: 'code', label: 'Code'}, {id: 'connect', label: 'Connect'}] as choice}
      <button type="button" aria-pressed={method === choice.id} class:selected={method === choice.id} onclick={() => { method = choice.id as typeof method; scanDevice = false; codeCopy = 'idle'; }}>{choice.label}</button>
    {/each}
  </div>
  {#if deviceError}<p class="host-error" role="alert">{deviceError}</p>{/if}
  {#if error}<p class="host-error" role="alert">{error}</p>{/if}
  <section class="host-list" aria-labelledby="configured-hosts-title">
    <h3 id="configured-hosts-title">Connected Devices</h3>
    <div class="host-list-rows" use:scrollFade={configuredHosts.length}>
      {#each configuredHosts as configured (configured.hostId)}
        {@const online = configured.mode === 'local' || configured.state === 'connected'}
        <div class="host-row" class:disconnected={!online}>
          <Icon name={deviceTypeIconName(configured.deviceType)} size={15} strokeWidth={1.4}/>
          <strong class="host-identity">{configured.deviceName}</strong>
          <span class="device-status"><span class="status-dot" class:online aria-hidden="true"></span>{online ? 'Online' : 'Offline'}</span>
          {#if configured.mode === 'remote'}
            <div class="host-row-actions">
              <button type="button" aria-label={`Remove ${configured.deviceName}`} data-tooltip="none" disabled={busy} onclick={() => onRemove(configured.hostId)}><Icon name="close" size={13} strokeWidth={1.4}/></button>
            </div>
          {/if}
        </div>
      {/each}
      {#each deviceState.connectedDevices.filter(device => !configuredHosts.some(host => host.hostId === device.deviceId)) as device}
        <div class="host-row" class:disconnected={!device.online}>
          <Icon name={deviceTypeIconName(device.deviceType)} size={15} strokeWidth={1.4}/>
          <strong class="host-identity">{device.deviceName}</strong>
          <span class="device-status"><span class="status-dot" class:online={device.online} aria-hidden="true"></span>{device.online ? 'Online' : 'Offline'}</span>
          <div class="host-row-actions"><button type="button" aria-label={`Disconnect ${device.deviceName}`} data-tooltip="none" onclick={() => void deviceRequest({action: 'revoke', deviceId: device.deviceId})}><Icon name="close" size={13} strokeWidth={1.4}/></button></div>
        </div>
      {/each}
    </div>
  </section>
  <section class="host-install" aria-labelledby="host-install-title">
    <div class="install-slider" class:feedback={installCopy !== 'idle'}>
      <div class="install-options" inert={installCopy !== 'idle'}>
        <h3 id="host-install-title">Installations:</h3>
        <div class="platforms">
          {#each platforms as item (item.id)}<button type="button" aria-label={item.label} data-tooltip-label={item.label} onclick={openReleases}><HostPlatformIcon platform={item.id} size={15}/></button>{/each}
          <button type="button" aria-label="Copy Command" onclick={() => void copyHostInstall()}><Icon name="terminal" size={15} strokeWidth={1.4}/></button>
        </div>
      </div>
      <div class="install-feedback" aria-live="polite" aria-atomic="true">
        {#if installCopy === 'copied'}<span class="install-copied">Copied<Icon name="check" size={16}/></span><code aria-label="Command copied">{HOST_INSTALL_COMMAND}</code>{:else if installCopy === 'failed'}<Icon name="warning" size={16}/><span>Couldn’t copy the command</span>{/if}
      </div>
    </div>
    {#if installCopy === 'failed'}<code class="install-command">{HOST_INSTALL_COMMAND}</code>{/if}
  </section>
</div>

<style>
  .code-prompt{font-size:11px;color:var(--neutral-600);margin:0}
  .confirmation-choices{display:flex;gap:12px;margin:8px 0}.confirmation-choices button{padding:10px 14px;border:1px solid var(--neutral-250,var(--neutral-300));border-radius:8px;font-size:22px;font-variant-numeric:tabular-nums}
  .team-host-panel{position:fixed;z-index:181;box-sizing:border-box;width:min(310px,calc(100vw - 16px));padding:14px;overflow:auto;scrollbar-width:none;visibility:hidden;background:var(--app-surface);color:var(--neutral-900);border-radius:14px;outline:none}
  .team-host-panel.placed{visibility:visible}.team-host-panel::-webkit-scrollbar,.host-list-rows::-webkit-scrollbar{display:none}
  header,header>button,.platforms,.host-row,.host-row-actions{display:flex;align-items:center;gap:8px}
  header{justify-content:space-between}h2,h3,p{margin:0}h2{font-size:14px;font-weight:600}h3{font-size:12px;font-weight:560}
  button{border:0;padding:4px;background:transparent;color:var(--neutral-600);font:inherit;font-size:12px;cursor:pointer}button:hover{color:var(--neutral-950)}button:disabled{opacity:.45;cursor:not-allowed}button:focus-visible{outline:2px solid var(--focus-ring);outline-offset:2px;border-radius:4px}small{font-size:11px;color:var(--neutral-600);line-height:1.5}
  .pair-stage{box-sizing:border-box;height:212px;flex:none;overflow:auto;scrollbar-width:none;display:flex;flex-direction:column;align-items:center;justify-content:safe center;gap:6px;padding:8px 0}.pair-stage::-webkit-scrollbar{display:none}.pair-stage>:global(*){flex-shrink:0}.pair-stage>strong{font-size:13px}.pair-qr{width:140px;height:140px;shape-rendering:crispEdges;border-radius:8px}.pair-code{font-size:32px;font-weight:600;letter-spacing:.12em;font-variant-numeric:tabular-nums;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;white-space:nowrap}.pair-refresh{font-variant-numeric:tabular-nums}
  button.pair-code{padding:0;color:var(--neutral-900);cursor:pointer;user-select:none}button.pair-code:hover{color:var(--neutral-950)}
  .method-switch{display:flex;border-radius:8px;background:var(--neutral-100);padding:3px;gap:3px}.method-switch button{flex:1;padding:6px 3px;border-radius:6px;white-space:nowrap;font-size:11px}.method-switch button.selected{background:var(--app-surface);color:var(--neutral-950)}
  .host-list{margin-top:16px}.host-list-rows{margin-top:8px;max-height:156px;overflow:auto;scrollbar-width:none}.host-row{width:100%;min-width:0;padding:4px 0;min-height:26px;color:var(--neutral-600);flex-wrap:nowrap}.host-row>:global(svg){flex:none}.host-identity{min-width:0;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px;font-weight:500;line-height:18px;color:var(--neutral-900)}.device-status{display:inline-flex;align-items:center;gap:5px;flex:none;margin-left:auto;font-size:11px;line-height:18px;white-space:nowrap}.status-dot{width:4px;height:4px;border-radius:50%;background:var(--neutral-400)}.status-dot.online{background:var(--success-text)}.host-row-actions{flex:none}.host-row-actions button{display:flex;align-items:center;justify-content:center;padding:0}

  .host-install{margin-top:10px;padding-top:10px;border-top:1px solid var(--neutral-200)}.install-slider{position:relative;overflow:clip;min-height:34px}.install-options{min-height:34px;display:flex;align-items:center;justify-content:space-between;gap:8px;transition:transform .28s cubic-bezier(.16,1,.3,1),opacity .2s}.platforms{gap:8px}.platforms button{display:grid;place-items:center;min-width:24px;min-height:30px}.install-feedback{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;gap:8px;transform:translateX(100%);opacity:0;transition:transform .28s cubic-bezier(.16,1,.3,1),opacity .2s;font-size:12px}.install-copied{display:inline-flex;align-items:center;gap:4px;flex:none;color:var(--success-text)}.install-feedback code{min-width:0;font-size:10.5px;line-height:1.45;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;text-align:left}.install-feedback span{flex:none}.install-feedback :global(svg){flex:none}.feedback .install-options{transform:translateX(-100%);opacity:0}.feedback .install-feedback{transform:translateX(0);opacity:1}.install-command{display:block;margin-top:8px;font-size:11px;overflow-wrap:anywhere;user-select:text}
  form{width:100%;display:flex;flex-direction:column;gap:10px}.host-primary{padding:8px;border-radius:7px;background:var(--neutral-900);color:var(--app-bg)}.host-primary:hover{color:var(--app-bg)}.host-error{margin-top:8px;color:var(--danger,#b42318);font-size:11px;overflow-wrap:anywhere}
  @media(prefers-reduced-motion:reduce){.install-options,.install-feedback{transition:none}}
</style>
