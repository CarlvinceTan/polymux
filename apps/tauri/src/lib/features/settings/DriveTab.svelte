<script lang="ts">
  import {onMount} from 'svelte';
  import type {
    DriveSourceDto,
    DriveProviderDto,
    DriveProviderId,
    DriveS3ConfigRequest,
    DriveStatusDto,
    PolymuxApi,
  } from '@polymux/protocol';
  import {readableError} from '../../shared/errors';
  import {loadSettingsDriveSnapshot, settingsDriveSnapshot} from '../../shared/state/settingsPreload';
  import {scrollFade} from '../../shared/scrollFade';
  import Icon from '../../shared/components/Icon.svelte';
  import {activeLocale, locale, plural, t, translate, withLocale, type MessageKey} from '../../../i18n';
  import {driveProviderDescription, driveProviderName} from '../../../i18n/names';

  export let api: PolymuxApi;

  let status: DriveStatusDto | null = settingsDriveSnapshot.status;
  $: if (status && status !== settingsDriveSnapshot.status) {
    settingsDriveSnapshot.status = status;
    settingsDriveSnapshot.loadedAt = Date.now();
  }
  let selected: DriveProviderId | 'configuration' = 'local';
  let loading = !settingsDriveSnapshot.status;
  let error = '';
  let busy = '';

  // S3 credentials form
  let editingS3 = false;
  let s3Form: DriveS3ConfigRequest = blankS3();
  let s3Secret = '';

  onMount(() => {
    void load();
    return api.drive.subscribe((next) => {
      status = next;
    });
  });

  async function load(): Promise<void> {
    loading = !status;
    error = '';
    try {
      status = await loadSettingsDriveSnapshot(api, 5_000);
    } catch (cause) {
      error = readableError(cause);
    } finally {
      loading = false;
    }
  }

  async function refresh(): Promise<void> {
    busy = 'refresh';
    try {
      status = await api.drive.refresh();
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  $: providers = status?.providers ?? [];
  $: active = providers.find((entry) => entry.id === selected) ?? null;
  /** The save order, resolved to the providers that could actually take a
   * file right now — an order listing disconnected accounts is a wish, not a
   * plan, and reordering it would not mean anything. */
  $: writable = (status?.saveOrder ?? [])
    .map((id) => providers.find((entry) => entry.id === id))
    .filter((entry): entry is DriveProviderDto => entry?.state === 'connected');
  /** The backend labels providers in English; both of these put the interface
   * language back over the top. Named against `$locale` so a language change
   * redraws the rail. */
  $: description = withLocale($locale, driveProviderDescription);
  $: providerName = withLocale($locale, (provider: DriveProviderDto) =>
    driveProviderName(provider.id, provider.name));

  /** Who the connection belongs to, which leads the rail's second line the way
   * an MCP server's author does. Nothing to say for a provider that is not
   * connected, so the state word stands alone there. */
  function accountLabel(provider: DriveProviderDto): string {
    if (provider.state !== 'connected') return '';
    // Local and network storage expose folders as drive sources, not signed-in
    // accounts. In particular, Local always has both the Polymux folder and
    // the home folder; counting those made one Mac read as "2 accounts".
    if (provider.kind === 'local' || provider.kind === 'network') return '';
    return provider.accounts.length > 1
      ? plural('drive.accounts', provider.accounts.length)
      : (provider.accounts[0]?.name ?? '');
  }

  function stateLabel(provider: DriveProviderDto): string {
    switch (provider.state) {
      case 'connected':
        return translate('drive.stateConnected');
      case 'logged-out':
        return translate('drive.stateDisconnected');
      case 'unconfigured':
        return translate('drive.stateUnconfigured');
      case 'unavailable':
        return translate('drive.stateUnavailable');
      case 'error':
        return translate('drive.stateError');
      default:
        return translate('hub.unknown');
    }
  }

  async function connect(provider: DriveProviderDto): Promise<void> {
    busy = `connect:${provider.id}`;
    try {
      status = await api.drive.connect(provider.id);
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  /** Drops one account. Omitting it signs the whole provider out, which is what
   * the S3 form's single connection wants. */
  async function disconnect(
    provider: DriveProviderDto,
    accountId?: string,
  ): Promise<void> {
    busy = `disconnect:${provider.id}:${accountId ?? ''}`;
    try {
      status = await api.drive.disconnect(provider.id, accountId);
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  /** Passing no path opens the folder picker in the main process. */
  async function chooseFolder(): Promise<void> {
    busy = 'local-root';
    try {
      status = await api.drive.setLocalRoot(null);
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  /** The shares, which are the network provider's sources rather than its
   * accounts: each is its own place in the drive, mounted or not. */
  $: shares = (status?.sources ?? []).filter((source) => source.provider === 'network');

  /** No path means the main process opens a folder picker, which is how a
   * share is chosen: macOS already puts a mounted one under /Volumes. */
  async function addShare(): Promise<void> {
    busy = 'add-share';
    try {
      status = await api.drive.addShare();
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  async function removeShare(share: DriveSourceDto): Promise<void> {
    busy = `share:${share.id}`;
    try {
      status = await api.drive.removeShare(share.accountId);
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  function blankS3(): DriveS3ConfigRequest {
    return {
      bucket: '',
      region: 'us-east-1',
      endpoint: null,
      accessKeyId: '',
      prefix: null,
      forcePathStyle: false,
    };
  }

  function editS3(): void {
    const s3 = providers.find((entry) => entry.id === 's3');
    s3Form = blankS3();
    // The bucket is the only part of a saved connection that comes back in the
    // status, so an edit starts from it rather than from nothing.
    if (s3?.root) s3Form.bucket = s3.root.split('/')[0];
    s3Secret = '';
    editingS3 = true;
  }

  async function saveS3(): Promise<void> {
    busy = 's3-save';
    try {
      status = await api.drive.saveS3({
        ...s3Form,
        secretAccessKey: s3Secret || undefined,
      });
      editingS3 = false;
      s3Secret = '';
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  async function move(provider: DriveProviderId, delta: -1 | 1): Promise<void> {
    const order = writable.map((entry) => entry.id);
    const index = order.indexOf(provider);
    const target = index + delta;
    if (index === -1 || target < 0 || target >= order.length) return;
    [order[index], order[target]] = [order[target], order[index]];
    busy = 'order';
    try {
      // Only the connected slice is reordered here; the main process folds it
      // back into the full order so a disconnected provider keeps its place.
      status = await api.drive.setSaveOrder([
        ...order,
        ...(status?.saveOrder ?? []).filter((id) => !order.includes(id)),
      ]);
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  const SIZE_UNITS: MessageKey[] = [
    'drive.unitBytes',
    'drive.unitKilobytes',
    'drive.unitMegabytes',
    'drive.unitGigabytes',
    'drive.unitTerabytes',
  ];

  function formatBytes(bytes: number): string {
    let value = bytes;
    let unit = 0;
    while (value >= 1024 && unit < SIZE_UNITS.length - 1) {
      value /= 1024;
      unit += 1;
    }
    const rounded = value >= 100 || unit === 0 ? Math.round(value) : value;
    const digits = value >= 100 || unit === 0 ? 0 : 1;
    return translate(SIZE_UNITS[unit], {
      size: rounded.toLocaleString(activeLocale(), {minimumFractionDigits: digits, maximumFractionDigits: digits}),
    });
  }

  function usageWidth(bytes: number, total: number): number {
    return total > 0 ? Math.min(100, Math.max(0, (bytes / total) * 100)) : 0;
  }
</script>

<div class="drive" role="tabpanel">
  {#if loading}
    <p class="drive-muted">{$t('drive.checking')}</p>
  {:else}
    {#if error}
      <div class="drive-error" role="alert">
        <span>{error}</span>
        <button type="button" onclick={() => void refresh()} disabled={busy === 'refresh'}>{$t('common.retry')}</button>
      </div>
    {/if}

    <div class="drive-body">
      <ul class="drive-rail" use:scrollFade={providers.length}>
        {#each providers as provider (provider.id)}
          <li>
            <button
              type="button"
              class:active={selected === provider.id}
              onclick={() => {
                selected = provider.id;
                // Walking away to another provider abandons the form rather
                // than leaving it up over a provider it has nothing to do with.
                editingS3 = false;
              }}
            >
              <span>
                <strong>{providerName(provider)}</strong>
                <small>
                  <!-- The separator is part of the expression: written as
                       markup, the compiler trims the trailing space and the
                       dot ends up stuck to the word after it. -->
                  {#if accountLabel(provider)}{`${accountLabel(provider)} · `}{/if}<span
                    class="state-text"
                    data-state={provider.state}
                  >{stateLabel(provider)}</span>
                </small>
              </span>
            </button>
          </li>
        {/each}
        <li class="drive-configuration-item">
          <button
            type="button"
            class:active={selected === 'configuration'}
            onclick={() => {
              selected = 'configuration';
              editingS3 = false;
            }}
          >
            <span><strong>{$t('drive.whereNewFilesGo')}</strong></span>
          </button>
        </li>
      </ul>

      <!-- The right column shows either one provider or the shared save
           priority selected from the rail's anchored configuration row. -->
      <div class="drive-column">
        <div class="drive-detail" use:scrollFade={selected}>
        {#if selected === 'configuration'}
          <header class="drive-detail-header">
            <h3>{$t('drive.whereNewFilesGo')}</h3>
            <p>{$t('drive.saveOrderHint')}</p>
          </header>
          {#if writable.length === 0}
            <p class="drive-muted">{$t('drive.nothingConnected')}</p>
          {:else}
            <ol class="drive-order">
              {#each writable as provider, index (provider.id)}
                <li>
                  <span class="drive-rank" class:primary={index === 0}>{index + 1}</span>
                  <span class="drive-order-name">
                    <strong>{providerName(provider)}</strong>
                    {#if index === 0}<em>{$t('drive.primary')}</em>{/if}
                  </span>
                  <span class="drive-order-actions">
                    <button
                      type="button"
                      class="up"
                      aria-label={$t('drive.moveUp', {provider: providerName(provider)})}
                      disabled={index === 0 || busy === 'order'}
                      onclick={() => void move(provider.id, -1)}
                    >
                      <Icon name="chevron" size={12} />
                    </button>
                    <button
                      type="button"
                      aria-label={$t('drive.moveDown', {provider: providerName(provider)})}
                      disabled={index === writable.length - 1 || busy === 'order'}
                      onclick={() => void move(provider.id, 1)}
                    >
                      <Icon name="chevron" size={12} />
                    </button>
                  </span>
                </li>
              {/each}
            </ol>
          {/if}
        {:else if editingS3}
          <header class="drive-detail-header">
            <h3>{$t('drive.s3')}</h3>
            <p>{$t('drive.s3Blurb')}</p>
          </header>
          <div class="drive-form">
            <label>
              <span>{$t('drive.bucket')}</span>
              <input bind:value={s3Form.bucket} spellcheck="false" placeholder="my-bucket" />
            </label>
            <label>
              <span>{$t('drive.region')}</span>
              <input bind:value={s3Form.region} spellcheck="false" placeholder="us-east-1" />
            </label>
            <label>
              <span>{$t('drive.endpoint')}</span>
              <input
                value={s3Form.endpoint ?? ''}
                spellcheck="false"
                placeholder={$t('drive.endpointPlaceholder')}
                oninput={(event) => (s3Form.endpoint = (event.currentTarget as HTMLInputElement).value || null)}
              />
              <small>{$t('drive.endpointHint')}</small>
            </label>
            <label>
              <span>{$t('drive.accessKeyId')}</span>
              <input bind:value={s3Form.accessKeyId} spellcheck="false" />
            </label>
            <label>
              <span>{$t('drive.secretAccessKey')}</span>
              <input bind:value={s3Secret} type="password" placeholder={$t('drive.secretPlaceholder')} />
            </label>
            <label>
              <span>{$t('drive.prefix')}</span>
              <input
                value={s3Form.prefix ?? ''}
                spellcheck="false"
                placeholder={$t('drive.prefixPlaceholder')}
                oninput={(event) => (s3Form.prefix = (event.currentTarget as HTMLInputElement).value || null)}
              />
            </label>
            <label class="drive-check">
              <input type="checkbox" bind:checked={s3Form.forcePathStyle} />
              <span>{$t('drive.pathStyle')}</span>
            </label>
            <footer class="drive-actions">
              <button type="button" onclick={() => (editingS3 = false)}>{$t('common.cancel')}</button>
              <button
                type="button"
                class="primary"
                disabled={busy === 's3-save' || !s3Form.bucket.trim() || !s3Form.accessKeyId.trim()}
                onclick={() => void saveS3()}
              >
                {busy === 's3-save' ? $t('hub.saving') : $t('drive.saveBucket')}
              </button>
            </footer>
          </div>
        {:else if active}
          <header class="drive-detail-header">
            <h3>{providerName(active)}</h3>
            <p>{description(active.id)}</p>
            <span class="drive-detail-actions">
              <button
                type="button"
                aria-label={$t('drive.recheck')}
                data-tooltip-label={$t('drive.recheckShort')}
                class:spinning={busy === 'refresh'}
                disabled={busy === 'refresh'}
                onclick={() => void refresh()}
              >
                <Icon name="reload" size={13} />
              </button>
            </span>
          </header>

          {#if active.error}
            <p class="drive-hint warn">{active.error}</p>
          {/if}

          {#if active.kind !== 'local'}
            <!-- Skipped for this Mac: it has nothing to connect to, and its
                 folder is named by the storage block below instead. -->
          <section class="drive-block">
            {#if active.kind === 'network'}
              <h4>{$t('drive.shares')}</h4>
              {#if shares.length === 0}
                <p class="drive-hint">{$t('drive.sharesHint')}</p>
              {:else}
                <ul class="drive-shares">
                  {#each shares as share (share.id)}
                    <li>
                      <span class="drive-share">
                        <strong>{share.accountLabel ?? share.name}</strong>
                        <!-- A share that is not mounted is not an error: it
                             says so, and stays listed so it can come back. -->
                        <small class:warn={share.state !== 'connected'}>
                          {share.state === 'connected'
                            ? share.root
                            : $t('drive.shareNotMounted')}
                        </small>
                      </span>
                      <button
                        type="button"
                        class="destructive"
                        disabled={busy === `share:${share.id}`}
                        onclick={() => void removeShare(share)}
                      >
                        {$t('drive.forget')}
                      </button>
                    </li>
                  {/each}
                </ul>
              {/if}
              <footer class="drive-actions">
                <button
                  type="button"
                  class="primary"
                  disabled={busy === 'add-share'}
                  onclick={() => void addShare()}
                >
                  {busy === 'add-share' ? $t('drive.adding') : $t('drive.addShare')}
                </button>
              </footer>
            {:else}
              <h4>{$t('drive.connection')}</h4>
              {#if active.state === 'connected'}
              {#each active.accounts as account (account.id)}
                <p class="drive-value">
                  <code>{account.email ?? account.name}</code>
                  {#if active.id !== 'local'}
                    <button
                      type="button"
                      class="destructive"
                      disabled={busy === `disconnect:${active.id}:${account.id}`}
                      onclick={() => void disconnect(active, account.id)}
                    >
                      {busy === `disconnect:${active.id}:${account.id}` ? $t('drive.disconnecting') : $t('drive.disconnect')}
                    </button>
                  {/if}
                </p>
              {/each}
              {#if active.root}
                <p class="drive-hint">{$t('drive.filesLiveIn')} <code>{active.root}</code></p>
              {/if}
              <!-- Several accounts of one provider can be signed in at once,
                   and each becomes its own entry in the drive's switch. -->
              {#if active.kind === 'oauth'}
                <footer class="drive-actions">
                  <button
                    type="button"
                    disabled={busy === `connect:${active.id}`}
                    onclick={() => void connect(active)}
                  >
                    {busy === `connect:${active.id}` ? $t('drive.waitingForSignIn') : $t('drive.addAccount')}
                  </button>
                </footer>
              {/if}
            {:else if active.state === 'unconfigured'}
              <p class="drive-hint warn">
                {$t('drive.unconfiguredHint', {provider: providerName(active)})}
                <code>README.md</code>
              </p>
            {:else if active.kind === 's3'}
              <p class="drive-hint">{$t('drive.addBucketHint')}</p>
              <footer class="drive-actions">
                <button type="button" class="primary" onclick={editS3}>{$t('drive.addBucket')}</button>
              </footer>
            {:else}
              <p class="drive-hint">{$t('drive.signInHint', {provider: providerName(active)})}</p>
              <footer class="drive-actions">
                <button
                  type="button"
                  class="primary"
                  disabled={busy === `connect:${active.id}`}
                  onclick={() => void connect(active)}
                >
                  {busy === `connect:${active.id}` ? $t('drive.waitingForSignIn') : $t('drive.connectProvider', {provider: providerName(active)})}
                </button>
              </footer>
            {/if}
            {/if}
          </section>
          {/if}

          {#if active.id === 'local'}
            <section class="drive-block">
              <h4>{$t('drive.storageLocation')}</h4>
              <p class="drive-value">
                <code>{active.root ?? '—'}</code>
                <button type="button" disabled={busy === 'local-root'} onclick={() => void chooseFolder()}>
                  {$t('common.change')}
                </button>
              </p>
              <p class="drive-hint">{$t('drive.storageLocationHint')}</p>
            </section>
          {/if}

          {#if active.id === 's3' && active.state === 'connected'}
            <section class="drive-block">
              <h4>{$t('drive.bucket')}</h4>
              <footer class="drive-actions">
                <button type="button" onclick={editS3}>{$t('drive.editSettings')}</button>
              </footer>
            </section>
          {/if}

          {#if active.usage}
            <section class="drive-block">
              <h4>{$t('drive.space')}</h4>
              {#if active.usage.total !== null && active.usage.used !== null}
                <div class="drive-meter" role="img" aria-label={`${$t('drive.usedOf', {used: formatBytes(active.usage.used), total: formatBytes(active.usage.total)})}${active.usage.appUsed !== null ? `; Polymux ${formatBytes(active.usage.appUsed)}` : ''}`}>
                  <span
                    class="drive-meter-others"
                    style={`width:${usageWidth(Math.max(0, active.usage.used - (active.usage.appUsed ?? 0)), active.usage.total)}%`}
                  ></span>
                  {#if active.usage.appUsed !== null}
                    <span
                      class="drive-meter-app"
                      style={`width:${usageWidth(Math.min(active.usage.used, active.usage.appUsed), active.usage.total)}%`}
                    ></span>
                  {/if}
                </div>
                <p class="drive-hint drive-usage-line">
                  <span>{$t('drive.usedOf', {used: formatBytes(active.usage.used), total: formatBytes(active.usage.total)})}</span>
                  {#if active.usage.appUsed !== null}
                    <span class="drive-usage-separator">·</span>
                    <span class="drive-app-used">Polymux {formatBytes(active.usage.appUsed)}</span>
                  {/if}
                </p>
              {:else if active.usage.used !== null}
                <!-- An account with no cap reports usage but no limit, which is
                     a real answer rather than a missing one. -->
                <p class="drive-hint">{$t('drive.usedNoQuota', {used: formatBytes(active.usage.used)})}</p>
              {/if}
            </section>
          {/if}
        {/if}

        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .drive{flex:1;min-height:0;display:flex;flex-direction:column;overflow:hidden;padding:2px var(--options-detail-edge) 16px var(--options-content-edge)}
  .drive-muted{color:var(--neutral-400);font-size:11px}
  .drive-error{display:flex;align-items:center;gap:12px;margin-bottom:10px;padding:9px 11px;border-radius:9px;background:#fff5f5;color:#8f3e3e;font-size:11px}
  .drive-error>span{min-width:0;flex:1}
  .drive-error button{height:26px;flex:none;border:1px solid color-mix(in srgb,currentColor 20%,transparent);border-radius:7px;padding:0 9px;background:var(--app-surface);color:inherit;cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  :global(:root[data-theme="dark"]) .drive-error{background:#321f1f;color:#eea7a7}

  .drive-body{min-width:0;min-height:0;flex:1;display:grid;grid-template-columns:calc(186px + var(--options-tab-inline)) minmax(0,1fr);gap:var(--options-divider-gap)}
  .drive-rail{min-width:0;min-height:0;display:flex;flex-direction:column;gap:4px;margin:0;padding:6px var(--options-divider-gap) 6px 0;overflow-y:auto;list-style:none;border-right:1px solid var(--neutral-200)}
  .drive-rail>li{margin:0}
  .drive-rail button{width:100%;display:flex;align-items:center;gap:10px;border:0;border-radius:10px;padding:5px 9px;background:transparent;color:var(--neutral-700);cursor:pointer;font-family:inherit;text-align:left}
  .drive-rail button:hover{background:var(--neutral-100)}
  .drive-rail button.active{background:var(--neutral-100);color:var(--neutral-950)}
  .drive-rail button>span{min-width:0;flex:1;display:flex;flex-direction:column;gap:1px}
  .drive-rail strong{overflow:hidden;color:var(--neutral-900);text-overflow:ellipsis;white-space:nowrap;font-size:11.5px;font-weight:540}
  .drive-rail small{overflow:hidden;color:var(--neutral-400);text-overflow:ellipsis;white-space:nowrap;font-size:9.5px}
  .drive-rail .drive-configuration-item{margin-top:auto;padding-top:8px;transform:translateY(6px)}
  .drive-rail .drive-configuration-item::before{display:block;height:1px;margin:0 0 8px;background:var(--neutral-200);content:"";transform:translateY(-2.75px)}
  .drive-configuration-item button{min-height:31px}

  /* The right column scrolls independently of the provider rail. */
  .drive-column{min-width:0;min-height:0;display:flex;flex-direction:column}
  .drive-detail{min-width:0;min-height:0;flex:1;overflow-y:auto;padding-right:2px}
  .drive-column>:global(.drive-block){flex:none;margin-top:14px;padding-top:14px;border-top:1px solid var(--neutral-200)}
  .drive-detail-header{position:relative;margin-bottom:14px}
  .drive-detail-header h3{margin:0;color:var(--neutral-950);font-size:14px;font-weight:580}
  .drive-detail-header p{max-width:520px;margin:5px 0 0;color:var(--neutral-600);font-size:11px;line-height:1.5}
  .drive-detail-actions{position:absolute;top:0;right:0;display:flex;gap:4px}
  .drive-detail-actions button{width:26px;height:26px;display:grid;place-items:center;border:1px solid var(--neutral-200);border-radius:7px;background:var(--app-surface);color:var(--neutral-600);cursor:pointer}
  .drive-detail-actions button:hover{background:var(--neutral-100);color:var(--neutral-950)}
  .drive-detail-actions button.spinning{animation:drive-spin 1s linear infinite}
  @keyframes drive-spin{to{transform:rotate(360deg)}}

  .drive-block{margin-bottom:16px;padding-top:12px;border-top:1px solid var(--neutral-200)}
  .drive-block h4{margin:0 0 7px;color:var(--neutral-900);font-size:11.5px;font-weight:570}
  .drive-value{display:flex;align-items:center;gap:9px;margin:0 0 4px;font-size:11px}
  .drive-value code{overflow:hidden;padding:2px 6px;border-radius:5px;background:var(--neutral-100);color:var(--neutral-800);text-overflow:ellipsis;white-space:nowrap;font-size:10.5px}
  .drive-value button{height:26px;flex:none;border:1px solid var(--neutral-200);border-radius:7px;padding:0 10px;background:var(--app-surface);color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  .drive-value button:hover{background:var(--neutral-100);color:var(--neutral-950)}
  .drive-value button.destructive{color:var(--danger-500)}
  .drive-value button:disabled{cursor:default;opacity:.5}

  .drive-hint{max-width:520px;margin:5px 0 0;color:var(--neutral-500);font-size:10.5px;line-height:1.5}
  .drive-hint code{padding:1px 4px;border-radius:4px;background:var(--neutral-100);font-size:10px}
  .drive-hint.warn{color:var(--danger-500)}

  .drive-form{display:flex;max-width:440px;flex-direction:column;gap:9px}
  .drive-form label{display:flex;flex-direction:column;gap:3px}
  .drive-form label>span{color:var(--neutral-600);font-size:10.5px;font-weight:530}
  .drive-form label>small{color:var(--neutral-400);font-size:9.5px}
  .drive-form input{height:30px;border:1px solid var(--neutral-200);border-radius:8px;padding:0 9px;background:var(--app-surface);color:var(--neutral-950);font-family:inherit;font-size:11.5px}
  .drive-form input:focus-visible{border-color:var(--neutral-500);outline:0}
  .drive-check{flex-direction:row!important;align-items:center;gap:7px!important}
  .drive-check input{width:14px;height:14px;flex:none}

  /* One row per share, mounted or not. The row is a place rather than an
     account, so it leads with what the user called it and says underneath
     where it is — or that it is not there right now. */
  .drive-shares{margin:8px 0 0;padding:0;list-style:none;display:flex;flex-direction:column;gap:6px}
  .drive-shares li{display:flex;align-items:center;gap:10px;padding:8px 11px;border:1px solid var(--neutral-200);border-radius:9px}
  .drive-share{min-width:0;flex:1;display:flex;flex-direction:column;gap:1px}
  .drive-share strong{font-size:11.5px;font-weight:545;color:var(--neutral-900);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .drive-share small{font-size:10px;color:var(--neutral-500);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .drive-share small.warn{color:var(--neutral-600)}
  .drive-shares button{flex:none}
  .drive-actions{display:flex;justify-content:flex-end;gap:7px;margin-top:12px}
  .drive-actions button{height:29px;border:1px solid var(--neutral-200);border-radius:8px;padding:0 12px;background:var(--app-surface);color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:11px;font-weight:550}
  .drive-actions button:hover{background:var(--neutral-100);color:var(--neutral-950)}
  .drive-actions button.primary{border-color:var(--neutral-950);background:var(--neutral-950);color:var(--app-bg)}
  .drive-actions button.primary:hover{opacity:.88}
  .drive-actions button:disabled{cursor:default;opacity:.5}

  .drive-meter{height:6px;display:flex;overflow:hidden;border-radius:3px;background:var(--neutral-200)}
  .drive-meter>span{height:100%;display:block;flex:none}
  .drive-meter-others{background:var(--neutral-800)}
  .drive-meter-app{background:var(--flare-blue,#2384cb)}
  .drive-usage-line{display:flex;align-items:baseline;gap:5px}
  .drive-app-used{color:var(--flare-blue,#2384cb)}
  .drive-usage-separator{color:var(--neutral-500)}

  .drive-order{margin:8px 0 0;padding:0;list-style:none}
  .drive-order li{min-height:40px;display:flex;align-items:center;gap:10px;padding:0 4px;border-bottom:1px solid var(--neutral-200)}
  .drive-order li:first-child{border-top:1px solid var(--neutral-200)}
  .drive-rank{width:19px;flex:none;color:var(--neutral-400);font-size:9.5px;font-weight:600;text-align:center;font-variant-numeric:tabular-nums}
  .drive-rank.primary{color:var(--neutral-900)}
  .drive-order-name{min-width:0;flex:1;display:flex;align-items:center;gap:7px}
  .drive-order-name strong{overflow:hidden;color:var(--neutral-900);text-overflow:ellipsis;white-space:nowrap;font-size:11.5px;font-weight:545}
  .drive-order-name em{flex:none;color:var(--neutral-500);font-size:9.5px;font-style:normal;font-weight:550}
  .drive-order-actions{flex:none;display:flex;gap:2px}
  .drive-order-actions button{width:24px;height:24px;display:grid;place-items:center;border:0;padding:0;background:transparent;color:var(--neutral-500);cursor:pointer}
  .drive-order-actions button:hover:not(:disabled),.drive-order-actions button:focus-visible{outline:0;color:var(--neutral-950)}
  .drive-order-actions button:disabled{cursor:default;opacity:.3}
  /* The chevron points down, so one glyph serves both directions rather than
     shipping a second icon that is the same shape upside down. */
  .drive-order-actions button.up{transform:rotate(180deg)}
</style>
