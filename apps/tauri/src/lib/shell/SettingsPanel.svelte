<script lang="ts">
  import type { Session } from '../session.svelte';
  import { untrack } from 'svelte';
  import type { NetworkDevice, NetworkInfo } from '../api/models';
  import { applyAppearance } from '../appearance';
  import AddDevice from './AddDevice.svelte';
  import ModelSettings from './ModelSettings.svelte';
  import ProviderEditor from './ProviderEditor.svelte';
  import VaultPanel from './VaultPanel.svelte';
  import { ProviderStore } from './providers.svelte';
  import { savedServers } from '../servers';
  import { isTauri } from '../api/bridge';
  import type { Update } from '@tauri-apps/plugin-updater';

  /** Settings, as the reference settings view draws it: a tab column and titled rows. */
  let {
    session,
    onClose,
    onConnect,
    initialTab = 'general',
  }: {
    session: Session;
    onClose: () => void;
    onConnect: (address: string) => Promise<void>;
    initialTab?: string;
  } = $props();

  const tabs = [
    { id: 'general', name: 'General', icon: 'gear' },
    { id: 'models', name: 'Models', icon: 'cpu' },
    { id: 'vault', name: 'Vault', icon: 'lock' },
    { id: 'updates', name: 'Updates', icon: 'arrow-down' },
  ];
  let tab = $state(untrack(() => initialTab));
  let network = $state<NetworkInfo | undefined>(undefined);
  let renaming = $state<NetworkDevice | undefined>();
  let adding = $state(false);
  let providerTarget = $state<{ type?: string }>();
  const providerStore = untrack(() => new ProviderStore(session));
  let deviceName = $state('');
  let deviceIssue = $state('');
  let pending = $state<Record<string, boolean>>({});
  let networkRevision = 0;
  let servers = $state(savedServers());
  let switching = $state(false);
  let serverIssue = $state('');
  let update = $state<{ current: string; latest: string; available: boolean; notes?: string }>();
  let updateChecking = $state(false);
  let updateIssue = $state('');
  let updateChecked = $state(false);
  let updateInstalling = $state(false);
  let updateProgress = $state(0);
  let pendingUpdate: Update | undefined;
  const networkKey = untrack(() => `polymux.network.${session.address}`);
  $effect(() => { if (tab === 'updates' && !updateChecked) void checkUpdates(); });

  try {
    const cached = localStorage.getItem(networkKey);
    if (cached) network = JSON.parse(cached) as NetworkInfo;
  } catch { /* Keep settings usable if the cache is unavailable. */ }

  $effect(() => {
    const revision = networkRevision;
    void session.client
      .network()
      .then((info) => {
        if (networkRevision !== revision) return;
        network = info;
        if (info.server?.name) servers = servers.map((server) => server.address === session.address ? { ...server, name: info.server.name } : server);
        cacheNetwork();
      })
      .catch(() => undefined);
  });

  const devices = $derived([...(network?.devices ?? [])].sort((a, b) => Number(b.id === network?.current) - Number(a.id === network?.current)));
  const current = $derived(tabs.find((entry) => entry.id === tab)!);
  let theme = $state(localStorage.getItem('polymux.appearance') ?? 'system');
  let locationBusy = $state(false);
  let locationIssue = $state('');
  let notificationsBusy = $state(false);

  function setTheme(value: string) {
    theme = value;
    applyAppearance(value);
    void session.client.updateSettings({ appearance: value }).catch(() => undefined);
  }

  async function toggleNotifications() {
    if (notificationsBusy) return;
    notificationsBusy = true;
    try { await session.notifications.setEnabled(!session.notifications.enabled); }
    finally { notificationsBusy = false; }
  }

  async function toggleLocation() {
    if (locationBusy) return;
    locationBusy = true;
    locationIssue = '';
    try { await session.setLocation(!session.locationEnabled); }
    catch { locationIssue = 'Allow Polymux to use your location in System Settings.'; }
    finally { locationBusy = false; }
  }

  async function switchServer(address: string) {
    if (switching || address === session.address) return;
    switching = true;
    serverIssue = '';
    try {
      await onConnect(address);
      onClose();
    } catch {
      serverIssue = 'Couldn’t connect to that server.';
    } finally {
      switching = false;
    }
  }

  function cacheNetwork() {
    try { if (network) localStorage.setItem(networkKey, JSON.stringify(network)); } catch { /* Keep the state in memory. */ }
  }

  async function checkUpdates() {
    if (updateChecking || updateInstalling) return;
    updateChecking = true;
    updateIssue = '';
    try {
      if (!isTauri) {
        updateIssue = 'Update checks are available in the desktop app.';
        return;
      }
      const [{ check }, { getVersion }] = await Promise.all([
        import('@tauri-apps/plugin-updater'),
        import('@tauri-apps/api/app'),
      ]);
      const current = await getVersion();
      const found = await check();
      if (!found) {
        update = { current, latest: current, available: false };
        pendingUpdate = undefined;
        return;
      }
      update = { current, latest: found.version, available: true, notes: found.body ?? '' };
      pendingUpdate = found;
    } catch {
      updateIssue = 'Couldn’t check for updates.';
    } finally {
      updateChecking = false;
      updateChecked = true;
    }
  }

  async function installUpdate() {
    if (!pendingUpdate || updateInstalling) return;
    updateInstalling = true;
    updateIssue = '';
    updateProgress = 0;
    let downloaded = 0;
    let total = 0;
    try {
      await pendingUpdate.downloadAndInstall((event) => {
        if (event.event === 'Started') {
          downloaded = 0;
          total = event.data.contentLength ?? 0;
          updateProgress = 0;
        } else if (event.event === 'Progress') {
          downloaded += event.data.chunkLength;
          if (total) updateProgress = Math.min(99, Math.round((downloaded / total) * 100));
        } else if (event.event === 'Finished') {
          updateProgress = 100;
        }
      });
      const { relaunch } = await import('@tauri-apps/plugin-process');
      await relaunch();
    } catch {
      updateIssue = 'Couldn’t install this update.';
      updateInstalling = false;
    }
  }

  function editDevice(device: NetworkDevice) {
    renaming = device;
    deviceName = device.label ?? '';
  }

  async function renameDevice() {
    const device = renaming;
    if (!device || !network || pending[device.id]) return;
    const label = deviceName.trim();
    renaming = undefined;
    if (label === (device.label ?? '')) return;
    pending[device.id] = true;
    networkRevision++;
    deviceIssue = '';
    network.devices = network.devices.map((item) => item.id === device.id ? { ...item, label } : item);
    cacheNetwork();
    try { await session.client.labelDevice(device.id, label); }
    catch {
      if (network) network.devices = network.devices.map((item) => item.id === device.id ? { ...item, label: device.label } : item);
      deviceIssue = `Couldn't rename ${device.label || device.name}.`;
    } finally { delete pending[device.id]; networkRevision++; cacheNetwork(); }
  }

  async function removeDevice(device: NetworkDevice) {
    if (!network || device.id === network.current || device.id === network.host || pending[device.id]) return;
    const index = network.devices.findIndex((item) => item.id === device.id);
    if (index < 0) return;
    pending[device.id] = true;
    networkRevision++;
    deviceIssue = '';
    network.devices = network.devices.filter((item) => item.id !== device.id);
    cacheNetwork();
    try { await session.client.removeDevice(device.id); }
    catch {
      if (network) network.devices.splice(Math.min(index, network.devices.length), 0, device);
      deviceIssue = `Couldn't remove ${device.label || device.name}.`;
    } finally { delete pending[device.id]; networkRevision++; cacheNetwork(); }
  }
</script>

<div class="scrim" role="presentation" onclick={onClose}>
  {#if adding}
    <AddDevice {session} {onConnect} {onClose} onBack={() => (adding = false)} />
  {:else if providerTarget}
    <ProviderEditor store={providerStore} type={providerTarget.type} {onClose} onBack={() => (providerTarget = undefined)} />
  {:else}
  <div class="panel" class:editing={!!renaming} role="dialog" aria-label={renaming ? 'Rename device' : 'Settings'} tabindex="-1" onclick={(event) => event.stopPropagation()} onkeydown={(event) => event.stopPropagation()}>
    <div class="title">{renaming ? 'Rename device' : current.name}</div>
    {#if renaming}<button class="back glass" aria-label="Back" onclick={() => (renaming = undefined)}>‹</button>{/if}
    <button class="close glass" aria-label="Close settings" onclick={onClose}>
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M18 6 6 18M6 6l12 12"></path>
      </svg>
    </button>

    {#if renaming}
      <div class="device-editor">
        <div class="row"><label for="device-name">Name</label><input id="device-name" aria-label="Name" placeholder={renaming.name} bind:value={deviceName} onkeydown={(event) => { if (event.key === 'Enter') void renameDevice(); }} /></div>
        <div class="row"><span>Hostname</span><span class="note">{renaming.name}</span></div>
        <button class="done glass" onclick={() => void renameDevice()}>Done</button>
      </div>
    {:else}
    <div class="body">
      <nav class="tabs">
        {#each tabs as entry (entry.id)}
          <button class="tab" class:on={tab === entry.id} onclick={() => (tab = entry.id)}>
            <span class="glyph">
              {#if entry.icon === 'gear'}
                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6">
                  <circle cx="12" cy="12" r="3"></circle>
                  <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
                </svg>
              {:else if entry.icon === 'cpu'}
                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6">
                  <rect x="6" y="6" width="12" height="12" rx="2"></rect>
                  <path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"></path>
                </svg>
              {:else if entry.icon === 'lock'}
                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6">
                  <rect x="4" y="10" width="16" height="11" rx="2.5"></rect>
                  <path d="M8 10V7a4 4 0 0 1 8 0v3"></path>
                </svg>
              {:else}
                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6">
                  <path d="M12 4v12M7 11l5 5 5-5"></path>
                </svg>
              {/if}
            </span>
            {entry.name}
          </button>
        {/each}
      </nav>

      <div class="content">
        {#if tab === 'general'}
          <section>
            <h2>Appearance</h2>
            <div class="row">
              <span>Theme</span>
              <select bind:value={theme} onchange={() => setTheme(theme)} aria-label="Theme">
                <option value="system">System</option>
                <option value="light">Light</option>
                <option value="dark">Dark</option>
              </select>
            </div>
          </section>
          <section class="devices">
            <h2 class="with-action">Devices <button class="plus" aria-label="Add device" onclick={() => (adding = true)}>+</button></h2>
            <div class="row server-row">
              <span>Server</span>
              <select value={session.address} disabled={switching} aria-label="Server" onchange={(event) => void switchServer(event.currentTarget.value)}>
                {#each servers as server (server.address)}<option value={server.address}>{server.name}</option>{/each}
              </select>
            </div>
            {#if serverIssue}<p class="caption" role="status">{serverIssue}</p>{/if}
            {#each devices as device (device.id)}
              <div class="row device-row">
                <button class="device" aria-label={device.label || device.name} disabled={pending[device.id]} onclick={() => editDevice(device)}>
                  <span class="online" class:offline={device.paired === false || (device.id !== network?.current && !network?.online?.includes(device.id))}></span>
                  <span class="device-label">{device.label || device.name}{#if device.label && device.label !== device.name}<small>{device.name}</small>{/if}</span>
                  {#if device.id === network?.host}<svg class="server-icon" viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.6" aria-label="Server"><rect x="3" y="4" width="18" height="6" rx="1.5"></rect><rect x="3" y="14" width="18" height="6" rx="1.5"></rect><path d="M7 7h.01M7 17h.01"></path></svg>{/if}
                </button>
                {#if device.id === network?.current}<span class="note">This device</span>
                {:else if device.id !== network?.host}<button class="remove" aria-label="Remove {device.label || device.name}" disabled={pending[device.id]} onclick={() => void removeDevice(device)}>Remove</button>{/if}
              </div>
            {/each}
            {#if deviceIssue}<p class="caption" role="status">{deviceIssue}</p>{/if}
          </section>
          <section>
            <h2>Notifications</h2>
            <div class="row"><span>Allow notifications</span><button class="toggle" class:on={session.notifications.enabled} aria-label="Allow notifications" aria-pressed={session.notifications.enabled} disabled={notificationsBusy} onclick={() => void toggleNotifications()}></button></div>
            {#if session.notifications.issue}<p class="caption" role="status">{session.notifications.issue}</p>{/if}
          </section>
          <section>
            <h2>Connections</h2>
            <div class="row"><span>Location</span><button class="toggle" class:on={session.locationEnabled} aria-label="Location" aria-pressed={session.locationEnabled} disabled={locationBusy} onclick={() => void toggleLocation()}></button></div>
            <p class="caption">Lets bots on this server request this device's current location and address.</p>
            {#if locationIssue}<p class="caption" role="status">{locationIssue}</p>{/if}
          </section>
        {:else if tab === 'models'}
          <ModelSettings {session} store={providerStore} onProvider={(type) => (providerTarget = { type })} />
        {:else if tab === 'vault'}
          <VaultPanel {session} />
        {:else}
          <section>
            <h2>Updates</h2>
            <div class="row"><span>App version</span><span class="note">{update?.current ?? '0.1.0'}</span></div>
            <div class="row">
              <span>Status</span>
              <span class="note">{updateChecking ? 'Checking…' : updateInstalling ? `Installing… ${updateProgress}%` : updateIssue || (update ? (update.available ? `Version ${update.latest} available` : 'Up to date') : 'Not checked')}</span>
            </div>
            <div class="row">
              <span>Check for updates</span>
              <button class="action" disabled={updateChecking || updateInstalling} onclick={() => void checkUpdates()}>{updateChecking ? 'Checking…' : 'Check'}</button>
            </div>
            {#if update?.available}
              <div class="row"><span>Latest version</span><span class="note">{update.latest}</span></div>
              <div class="row">
                <span>{updateInstalling ? 'Installing update' : 'Install update'}</span>
                <button class="action" disabled={updateInstalling || updateChecking} onclick={() => void installUpdate()}>{updateInstalling ? 'Installing…' : 'Install'}</button>
              </div>
              {#if update.notes}<p class="caption notes">{update.notes}</p>{/if}
            {/if}
          </section>
        {/if}
      </div>
    </div>
    {/if}
  </div>
  {/if}
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.24);
    display: grid;
    place-items: center;
    z-index: 50;
  }
  .panel {
    position: relative;
    width: 820px;
    height: 600px;
    max-width: calc(100vw - 40px);
    max-height: calc(100vh - 60px);
    background: var(--window-bg);
    border-radius: var(--radius-panel);
    box-shadow: 0 20px 50px rgba(0, 0, 0, 0.25);
    overflow: hidden;
  }
  .panel.editing { width: 620px; }
  .back { position: absolute; top: 22px; left: 18px; width: 36px; height: 36px; border-radius: 50%; font-size: 24px; z-index: 2; }
  .device-editor { position: absolute; inset: 74px 24px 20px; }
  .device-editor .row + .row { margin-top: 2px; }
  .device-editor input { border: 0; background: transparent; text-align: right; min-width: 0; outline: none; }
  .done { position: absolute; right: 0; bottom: 0; border-radius: 16px; min-height: 32px; padding: 0 14px; }
  .title {
    position: absolute;
    top: 28px;
    left: 0;
    right: 0;
    text-align: center;
    font-weight: 600;
  }
  .close {
    position: absolute;
    top: 22px;
    right: 18px;
    width: 36px;
    height: 36px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    z-index: 2;
  }
  .body {
    display: flex;
    height: 100%;
    padding-top: 80px;
    box-sizing: border-box;
  }
  .tabs {
    width: 200px;
    border-right: 1px solid var(--hairline);
    flex: none;
    margin-top: -6px;
    padding: 0 10px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 28px;
    padding: 0 10px;
    border-radius: 10px;
    font-size: 13px;
    text-align: left;
    color: var(--text);
  }
  .tab.on {
    background: var(--row-selected);
  }
  .tab:hover:not(.on) {
    background: var(--row-hover);
  }
  .glyph {
    width: 16px;
    display: grid;
    place-items: center;
    color: var(--text);
  }
  .content {
    flex: 1;
    overflow-y: auto;
    padding: 0 24px 24px 16px;
    scrollbar-width: none;
  }
  .content::-webkit-scrollbar {
    display: none;
  }
  section {
    margin-bottom: 22px;
  }
  h2 {
    font-size: 13px;
    font-weight: 600;
    margin: 0 0 8px;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  h2.with-action {
    padding-right: 4px;
  }
  .plus {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    font-size: 15px;
    color: var(--text);
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 30px;
    padding: 0 12px;
    border-radius: 8px;
    background: var(--sidebar-bg);
    font-size: 13px;
  }
  .device {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
    text-align: left;
  }
  .devices { padding-top: 4px; margin-bottom: 20px; }
  .devices h2 { margin-bottom: 15px; }
  .server-row { margin-bottom: 7px; }
  .device-row + .device-row { margin-top: 6px; }
  .device-label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .server-icon { flex: none; color: var(--text-muted); }
  .device-label small { display: block; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; }
  .remove { border: 1px solid var(--hairline); border-radius: 6px; padding: 4px 9px; }
  .online {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #24a35b;
  }
  .online.offline { background: var(--text-secondary); }
  .note {
    color: var(--text-muted);
    font-size: 12px;
  }
  .action {
    padding: 4px 10px;
    border: 1px solid var(--glass-border);
    border-radius: 7px;
    background: var(--glass-field);
    color: var(--text);
    font-size: 12px;
  }
  .action:disabled { opacity: 0.5; }
  .notes { max-height: 120px; overflow: auto; white-space: pre-wrap; }
  .toggle {
    width: 32px;
    height: 19px;
    border-radius: 999px;
    background: rgba(0, 0, 0, 0.12);
    position: relative;
  }
  .toggle::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 15px;
    height: 15px;
    border-radius: 50%;
    background: #fff;
    transition: transform 0.15s ease-out;
  }
  .toggle.on { background: #24a35b; }
  .toggle.on::after { transform: translateX(13px); }
  .toggle:disabled { opacity: 0.55; }
  .caption {
    margin: 8px 2px 0;
    font-size: 11px;
    color: var(--text-muted);
  }
  select {
    font: inherit;
    font-size: 12px;
    border: none;
    background: none;
    color: var(--text);
  }
</style>
