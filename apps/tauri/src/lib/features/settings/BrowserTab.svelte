<script lang="ts">
  import {onMount, type ComponentProps} from 'svelte';
  import type {
    BrowserDownloadDto,
    BrowserHistoryEntryDto,
    BrowserImportResultDto,
    BrowserPermissionDto,
    BrowserSettingsDto,
    BrowserSiteDto,
    BrowserSourceDto,
    PolymuxApi,
    PermissionDecisionDto,
    SavedLoginDto,
    SitePermissionDto,
  } from '@polymux/protocol';
  import {readableError} from '../../shared/errors';
  import {loadSettingsBrowserSnapshot, settingsBrowserSnapshot} from '../../shared/state/settingsPreload';
  import {scrollFade} from '../../shared/scrollFade';
  import Icon from '../../shared/components/Icon.svelte';
  import {activeLocale, plural, t, type MessageKey} from '../../../i18n';
  import {clockTime} from '../../shared/displayTime';

  type IconName = ComponentProps<Icon>['name'];

  export let api: PolymuxApi;

  type Section = 'passwords' | 'downloads' | 'history' | 'permissions' | 'data' | 'import';

  let section: Section = 'passwords';
  let settings: BrowserSettingsDto | null = settingsBrowserSnapshot.settings;
  let logins: SavedLoginDto[] = settingsBrowserSnapshot.logins;
  let downloads: BrowserDownloadDto[] = settingsBrowserSnapshot.downloads;
  let permissions: SitePermissionDto[] = settingsBrowserSnapshot.permissions;
  let history: BrowserHistoryEntryDto[] = [];
  let historyQuery = '';
  /** Whether history has been asked for, as distinct from being empty. */
  let historyLoaded = false;
  let sites: BrowserSiteDto[] = [];
  /** Whether the site list has been asked for, as distinct from being empty. */
  let sitesLoaded = false;
  let sources: BrowserSourceDto[] = [];
  /** Whether a scan has run, as distinct from finding nothing. */
  let scanned = false;
  let error = '';
  let busy = '';
  /** The one password currently shown, and nothing else. Revealing is one at a
   * time on purpose: the whole list in the clear is not a thing to put on
   * screen. */
  let revealed: {id: string; password: string} | null = null;
  let copied = '';
  /** Which destructive action is awaiting confirmation, in the inline-takeover
   * style the drive toolbar uses rather than a dialog. */
  let confirming = '';
  let clearChoices = {
    cookies: true,
    cache: true,
    downloads: false,
    permissions: false,
    logins: false,
  };
  let importChoice: Record<string, {cookies: boolean; passwords: boolean; history: boolean}> = {};
  let importSummary = '';
  let importProblems: string[] = [];

  onMount(() => {
    void load();
    return api.browser.subscribe((event) => {
      if (event.type === 'downloads') downloads = settingsBrowserSnapshot.downloads = event.downloads;
      // A password captured in a browser tab lands here without the tab being
      // reopened.
      if (event.type === 'logins') void refreshLogins();
    });
  });

  async function load(): Promise<void> {
    error = '';
    try {
      const next = await loadSettingsBrowserSnapshot(api, 5_000);
      settings = next.settings;
      logins = next.logins;
      downloads = next.downloads;
      permissions = next.permissions;
    } catch (cause) {
      error = readableError(cause);
    }
  }

  async function refreshLogins(): Promise<void> {
    logins = settingsBrowserSnapshot.logins = await api.browser.logins();
  }

  async function guard(key: string, work: () => Promise<void>): Promise<void> {
    busy = key;
    error = '';
    try {
      await work();
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  async function patchSettings(patch: Parameters<PolymuxApi['browser']['updateSettings']>[0]): Promise<void> {
    await guard('settings', async () => {
      settings = settingsBrowserSnapshot.settings = await api.browser.updateSettings(patch);
    });
  }

  async function reveal(login: SavedLoginDto): Promise<void> {
    if (revealed?.id === login.id) {
      revealed = null;
      return;
    }
    await guard(`reveal:${login.id}`, async () => {
      const password = await api.browser.revealLogin(login.id);
      revealed = password === null ? null : {id: login.id, password};
    });
  }

  async function copyPassword(login: SavedLoginDto): Promise<void> {
    await guard(`copy:${login.id}`, async () => {
      const password = await api.browser.revealLogin(login.id);
      if (password === null) return;
      await navigator.clipboard.writeText(password);
      copied = login.id;
      setTimeout(() => {
        if (copied === login.id) copied = '';
      }, 1600);
    });
  }

  async function deleteLogin(login: SavedLoginDto): Promise<void> {
    await guard(`delete:${login.id}`, async () => {
      logins = settingsBrowserSnapshot.logins = await api.browser.deleteLogin(login.id);
      if (revealed?.id === login.id) revealed = null;
    });
  }

  async function setPermission(
    row: SitePermissionDto,
    decision: PermissionDecisionDto,
  ): Promise<void> {
    await guard(`permission:${row.origin}:${row.permission}`, async () => {
      permissions = settingsBrowserSnapshot.permissions = await api.browser.setPermission(
        row.origin,
        row.permission,
        decision,
      );
    });
  }

  async function loadHistory(): Promise<void> {
    historyLoaded = true;
    await guard('history', async () => {
      history = await api.browser.browsingHistory({query: historyQuery, limit: 300});
    });
  }

  async function forgetPage(url: string): Promise<void> {
    await guard(`forget:${url}`, async () => {
      history = await api.browser.forgetHistoryEntry(url);
    });
  }

  async function clearAllHistory(): Promise<void> {
    await guard('clearHistory', async () => {
      history = await api.browser.clearHistory();
      confirming = '';
    });
  }

  async function loadSites(): Promise<void> {
    // Set before the read, not after: the reactive trigger below watches this,
    // and a site list that is legitimately empty would otherwise ask again the
    // moment `busy` cleared, forever.
    sitesLoaded = true;
    await guard('sites', async () => {
      sites = await api.browser.sites();
    });
  }

  async function clearSite(origin: string): Promise<void> {
    await guard(`site:${origin}`, async () => {
      sites = await api.browser.clearSiteData(origin);
      permissions = settingsBrowserSnapshot.permissions = await api.browser.permissions();
      confirming = '';
    });
  }

  async function clearBrowsingData(): Promise<void> {
    await guard('clear', async () => {
      await api.browser.clearBrowsingData(clearChoices);
      confirming = '';
      await load();
      if (section === 'data') await loadSites();
    });
  }

  /** The counts, grouped so a history of eighty thousand pages reads as a
   * number rather than a run of digits. Both import routes say it the same
   * way, which is why it is here rather than at each call site. */
  function summarise(result: BrowserImportResultDto): string {
    return $t('browser.importDone', {
      cookies: result.cookiesImported.toLocaleString(),
      passwords: result.passwordsImported.toLocaleString(),
      pages: result.historyImported.toLocaleString(),
    });
  }

  async function scanSources(): Promise<void> {
    await guard('scan', async () => {
      sources = await api.browser.importSources();
      scanned = true;
      for (const source of sources)
        for (const profile of source.profiles)
          importChoice[`${source.id}:${profile.id}`] ??= {
            cookies: true,
            passwords: !source.fileImportOnly,
            history: true,
          };
      importChoice = importChoice;
    });
  }

  async function runImport(source: BrowserSourceDto, profileId: string): Promise<void> {
    const key = `${source.id}:${profileId}`;
    const choice = importChoice[key] ?? {cookies: true, passwords: false, history: true};
    await guard(`import:${key}`, async () => {
      const result = await api.browser.importFrom({
        sourceId: source.id,
        profileId,
        cookies: choice.cookies,
        passwords: choice.passwords,
        history: choice.history,
      });
      importSummary = summarise(result);
      importProblems = result.problems;
      await refreshLogins();
    });
  }

  async function importFromFile(): Promise<void> {
    await guard('import-file', async () => {
      const result = await api.browser.importFile();
      importSummary = summarise(result);
      importProblems = result.problems;
      await refreshLogins();
    });
  }

  const PERMISSION_LABELS: Record<BrowserPermissionDto, MessageKey> = {
    geolocation: 'browser.permissionGeolocation',
    media: 'browser.permissionMedia',
    notifications: 'browser.permissionNotifications',
    'clipboard-read': 'browser.permissionClipboardRead',
    pointerLock: 'browser.permissionPointerLock',
    fullscreen: 'browser.permissionFullscreen',
    openExternal: 'browser.permissionOpenExternal',
  };

  const STATE_LABELS: Record<BrowserDownloadDto['state'], MessageKey> = {
    progressing: 'browser.stateProgressing',
    paused: 'browser.statePaused',
    completed: 'browser.stateCompleted',
    cancelled: 'browser.stateCancelled',
    interrupted: 'browser.stateInterrupted',
  };

  /** The host, which is what a person recognises. The scheme is kept only when
   * it is not the ordinary one, because `http://` on a site is worth seeing. */
  function siteLabel(origin: string): string {
    try {
      const url = new URL(origin);
      return url.protocol === 'https:' ? url.host : `${url.protocol}//${url.host}`;
    } catch {
      return origin;
    }
  }

  /* Binary units and the same rounding rule the drive tab uses — one decimal
     until three figures, none after — so a size reads the same wherever the
     app shows one. */
  /** A visit's date in the shortest form that is still unambiguous: a time for
   * today, a weekday inside the week, a date beyond it. The clock is the shared
   * 12-hour one, so history reads like every other stamp in the app. */
  function formatVisited(iso: string): string {
    const when = new Date(iso);
    if (Number.isNaN(when.getTime())) return '';
    const now = new Date();
    if (when.toDateString() === now.toDateString()) return clockTime(when);
    const days = (now.getTime() - when.getTime()) / 86_400_000;
    if (days < 7) {
      return when.toLocaleDateString(activeLocale(), {weekday: 'short', hour: 'numeric', minute: '2-digit', hour12: true});
    }
    return when.toLocaleDateString(activeLocale(), {month: 'short', day: 'numeric', year: when.getFullYear() === now.getFullYear() ? undefined : 'numeric'});
  }

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    const units = ['KB', 'MB', 'GB'];
    let value = bytes / 1024;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit += 1;
    }
    const digits = value >= 100 ? 0 : 1;
    return `${value.toFixed(digits)} ${units[unit]}`;
  }

  $: sections = [
    {id: 'passwords', icon: 'key', label: $t('browser.groupPasswords')},
    {id: 'downloads', icon: 'download', label: $t('browser.groupDownloads')},
    {id: 'history', icon: 'history', label: $t('browser.groupHistory')},
    {id: 'permissions', icon: 'shield', label: $t('browser.groupPermissions')},
    {id: 'data', icon: 'cookie', label: $t('browser.groupData')},
    {id: 'import', icon: 'import', label: $t('browser.groupImport')},
  ] satisfies {id: Section; icon: IconName; label: string}[];
  $: if (section === 'data' && !sitesLoaded) void loadSites();
  $: if (section === 'history' && !historyLoaded) void loadHistory();
</script>

<div class="browser-tab">
  {#if error}
    <p class="browser-error">{error}</p>
  {/if}

  <div class="browser-body">
    <ul class="browser-rail" use:scrollFade={sections.length}>
      {#each sections as item (item.id)}
        <li>
          <button
            type="button"
            class:active={section === item.id}
            onclick={() => {
              section = item.id;
              confirming = '';
            }}
          >
            <Icon name={item.icon} size={16} strokeWidth={1.7} />
            <span>{item.label}</span>
          </button>
        </li>
      {/each}
    </ul>

    <div class="browser-detail" class:history-detail={section === 'history'} use:scrollFade={section}>
      {#if section === 'passwords'}
        <section class="browser-block first">
          <div class="browser-row">
            <span class="browser-mark"><Icon name="key" size={16} /></span>
            <span class="browser-copy">
              <h4>{$t('browser.autofill')}</h4>
              <small>{$t('browser.autofillHint')}</small>
            </span>
            <button
              type="button"
              role="switch"
              class="browser-toggle"
              class:enabled={settings?.autofillEnabled ?? false}
              aria-checked={settings?.autofillEnabled ?? false}
              aria-label={$t('browser.autofill')}
              disabled={!settings || busy === 'settings'}
              onclick={() => void patchSettings({autofillEnabled: !settings?.autofillEnabled})}
            >
              <span></span>
            </button>
          </div>
        </section>

        <section class="browser-block">
          <h4>{$t('browser.savedPasswords')}</h4>
          {#if logins.length === 0}
            <p class="browser-empty">{$t('browser.noPasswords')}</p>
          {:else}
            <ul class="browser-list">
              {#each logins as login (login.id)}
                <li>
                  <span class="browser-item">
                    <strong>{siteLabel(login.origin)}</strong>
                    <small>{login.username}</small>
                  </span>
                  {#if revealed?.id === login.id}
                    <code class="browser-secret">{revealed.password}</code>
                  {/if}
                  <span class="browser-item-actions">
                    <button
                      type="button"
                      aria-label={revealed?.id === login.id
                        ? $t('browser.hidePassword')
                        : $t('browser.revealPassword')}
                      onclick={() => void reveal(login)}
                    >
                      <Icon name={revealed?.id === login.id ? 'eye-off' : 'eye'} size={13} />
                    </button>
                    <button
                      type="button"
                      aria-label={$t('browser.copyPassword')}
                      onclick={() => void copyPassword(login)}
                    >
                      <Icon name={copied === login.id ? 'check' : 'copy'} size={13} />
                    </button>
                    <button
                      type="button"
                      class="destructive"
                      aria-label={$t('browser.deletePassword')}
                      onclick={() => void deleteLogin(login)}
                    >
                      <Icon name="trash" size={13} />
                    </button>
                  </span>
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      {:else if section === 'downloads'}
        <section class="browser-block first">
          <h4>{$t('browser.downloadLocation')}</h4>
          <p class="browser-value">
            <code>{settings?.downloadDirectory ?? '—'}</code>
            <button
              type="button"
              disabled={busy === 'settings'}
              onclick={() => void patchSettings({downloadDirectory: null})}
            >
              {$t('browser.changeLocation')}
            </button>
          </p>
          <div class="browser-row">
            <span class="browser-mark"><Icon name="download" size={16} /></span>
            <span class="browser-copy">
              <h4>{$t('browser.askWhereToSave')}</h4>
            </span>
            <button
              type="button"
              role="switch"
              class="browser-toggle"
              class:enabled={settings?.askWhereToSave ?? false}
              aria-checked={settings?.askWhereToSave ?? false}
              aria-label={$t('browser.askWhereToSave')}
              disabled={!settings || busy === 'settings'}
              onclick={() => void patchSettings({askWhereToSave: !settings?.askWhereToSave})}
            >
              <span></span>
            </button>
          </div>
        </section>

        <section class="browser-block">
          <h4>{$t('browser.downloadHistory')}</h4>
          {#if downloads.length === 0}
            <p class="browser-empty">{$t('browser.noDownloads')}</p>
          {:else}
            <ul class="browser-list">
              {#each downloads as download (download.id)}
                <li>
                  <span class="browser-item">
                    <strong>{download.title}</strong>
                    <small>
                      {$t(STATE_LABELS[download.state])}
                      {#if download.state === 'progressing' || download.state === 'paused'}
                        · {formatBytes(download.receivedBytes)}{#if download.totalBytes > 0}
                          / {formatBytes(download.totalBytes)}{/if}
                      {:else}· {download.completedAt}{/if}
                    </small>
                  </span>
                  <span class="browser-item-actions">
                    {#if download.state === 'progressing'}
                      <button
                        type="button"
                        aria-label={$t('browser.pauseDownload')}
                        onclick={() =>
                          void guard(`pause:${download.id}`, async () => {
                            downloads = settingsBrowserSnapshot.downloads =
                              await api.browser.pauseDownload(download.id);
                          })}
                      >
                        <Icon name="pause" size={13} />
                      </button>
                      <button
                        type="button"
                        aria-label={$t('browser.cancelDownload')}
                        onclick={() =>
                          void guard(`cancel:${download.id}`, async () => {
                            downloads = settingsBrowserSnapshot.downloads =
                              await api.browser.cancelDownload(download.id);
                          })}
                      >
                        <Icon name="close" size={13} />
                      </button>
                    {:else if download.state === 'paused'}
                      <button
                        type="button"
                        aria-label={$t('browser.resumeDownload')}
                        onclick={() =>
                          void guard(`resume:${download.id}`, async () => {
                            downloads = settingsBrowserSnapshot.downloads =
                              await api.browser.resumeDownload(download.id);
                          })}
                      >
                        <Icon name="play" size={13} />
                      </button>
                    {:else if download.state === 'completed'}
                      <button
                        type="button"
                        aria-label={$t('browser.openDownload')}
                        onclick={() => void api.browser.openDownload(download.id)}
                      >
                        <Icon name="link" size={13} />
                      </button>
                    {/if}
                    <button
                      type="button"
                      class="destructive"
                      aria-label={$t('browser.removeDownload')}
                      onclick={() =>
                        void guard(`remove:${download.id}`, async () => {
                          downloads = settingsBrowserSnapshot.downloads =
                            await api.browser.removeDownload(download.id);
                        })}
                    >
                      <Icon name="trash" size={13} />
                    </button>
                  </span>
                </li>
              {/each}
            </ul>
            <footer class="browser-block-actions">
              <button
                type="button"
                class="destructive"
                onclick={() =>
                  void guard('clear-downloads', async () => {
                    downloads = settingsBrowserSnapshot.downloads = await api.browser.clearDownloads();
                  })}
              >
                {$t('browser.clearDownloads')}
              </button>
            </footer>
          {/if}
        </section>
      {:else if section === 'history'}
        <section class="browser-block first browser-history-block">
          <header class="browser-history-head">
            <h4>{$t('browser.groupHistory')}</h4>
            <input
              class="browser-search"
              type="search"
              spellcheck="false"
              placeholder={$t('browser.historySearch')}
              bind:value={historyQuery}
              oninput={() => void loadHistory()}
            />
          </header>
          {#if history.length === 0}
            <p class="browser-empty">
              {historyQuery ? $t('browser.historyNoMatches') : $t('browser.historyEmpty')}
            </p>
          {:else}
            <ul class="browser-list browser-history-list" use:scrollFade={history.length}>
              {#each history as page (page.url)}
                <li>
                  <span class="browser-item">
                    <strong>{page.title || page.url}</strong>
                    <small>{siteLabel(page.url)} · {formatVisited(page.visitedAt)}{#if page.visitCount > 1} · {$t('browser.historyVisits', {count: page.visitCount})}{/if}</small>
                  </span>
                  <span class="browser-item-actions">
                    <button
                      type="button"
                      aria-label={$t('browser.forgetPage')}
                      disabled={busy === `forget:${page.url}`}
                      onclick={() => void forgetPage(page.url)}
                    >
                      <Icon name="trash" size={13} />
                    </button>
                  </span>
                </li>
              {/each}
            </ul>
            <footer class="browser-block-actions">
              {#if confirming === 'history'}
                <span class="browser-confirm">{$t('browser.historyClearConfirm')}</span>
                <button type="button" onclick={() => (confirming = '')}>{$t('browser.cancel')}</button>
                <button type="button" class="danger" onclick={() => void clearAllHistory()}>
                  {$t('browser.clear')}
                </button>
              {:else}
                <button type="button" onclick={() => (confirming = 'history')}>
                  {$t('browser.historyClear')}
                </button>
              {/if}
            </footer>
          {/if}
        </section>
      {:else if section === 'permissions'}
        <section class="browser-block first">
          {#if permissions.length === 0}
            <p class="browser-empty">{$t('browser.noPermissions')}</p>
          {:else}
            <table class="browser-columns">
              <thead>
                <tr>
                  <th>{$t('browser.permissionSite')}</th>
                  <th>{$t('browser.permissionName')}</th>
                  <th>{$t('browser.permissionDecision')}</th>
                </tr>
              </thead>
            </table>
            <div class="browser-table-wrap" use:scrollFade={permissions.length}>
              <table class="browser-table">
                <tbody>
                  {#each permissions as row (`${row.origin}:${row.permission}`)}
                    <tr>
                      <td><span class="browser-site">{siteLabel(row.origin)}</span></td>
                      <td>{$t(PERMISSION_LABELS[row.permission])}</td>
                      <td>
                        <select
                          value={row.decision}
                          aria-label={$t('browser.permissionDecision')}
                          onchange={(event) =>
                            void setPermission(
                              row,
                              event.currentTarget.value as PermissionDecisionDto,
                            )}
                        >
                          <option value="allow">{$t('browser.allow')}</option>
                          <option value="deny">{$t('browser.deny')}</option>
                          <option value="ask">{$t('browser.ask')}</option>
                        </select>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
            <footer class="browser-block-actions">
              <button
                type="button"
                class="destructive"
                onclick={() =>
                  void guard('clear-permissions', async () => {
                    permissions = settingsBrowserSnapshot.permissions =
                      await api.browser.clearPermissions();
                  })}
              >
                {$t('browser.clearPermissions')}
              </button>
            </footer>
          {/if}
        </section>
      {:else if section === 'data'}
        <section class="browser-block first">
          <h4>{$t('browser.storedData')}</h4>
          {#if sites.length === 0}
            <p class="browser-empty">{$t('browser.noSites')}</p>
          {:else}
            <ul class="browser-list">
              {#each sites as site (site.origin)}
                <li>
                  <span class="browser-item">
                    <strong>{siteLabel(site.origin)}</strong>
                    <small>{plural('browser.cookieCount', site.cookies)}</small>
                  </span>
                  <span class="browser-item-actions">
                    {#if confirming === `site:${site.origin}`}
                      <span class="browser-confirm">
                        <em>{$t('browser.clearSiteConfirm', {site: siteLabel(site.origin)})}</em>
                        <button type="button" onclick={() => (confirming = '')}>
                          {$t('browser.cancel')}
                        </button>
                        <button
                          type="button"
                          class="destructive"
                          onclick={() => void clearSite(site.origin)}
                        >
                          {$t('browser.confirm')}
                        </button>
                      </span>
                    {:else}
                      <button
                        type="button"
                        class="destructive text"
                        onclick={() => (confirming = `site:${site.origin}`)}
                      >
                        {$t('browser.clearSite')}
                      </button>
                    {/if}
                  </span>
                </li>
              {/each}
            </ul>
          {/if}
        </section>

        <section class="browser-block">
          <h4>{$t('browser.clearBrowsingData')}</h4>
          <ul class="browser-checks">
            {#each [['cookies', 'browser.clearCookies'], ['cache', 'browser.clearCache'], ['downloads', 'browser.clearDownloadHistory'], ['permissions', 'browser.clearPermissions'], ['logins', 'browser.clearLogins']] as [key, label] (key)}
              <li>
                <label>
                  <input
                    type="checkbox"
                    checked={clearChoices[key as keyof typeof clearChoices]}
                    onchange={(event) => {
                      clearChoices = {
                        ...clearChoices,
                        [key]: event.currentTarget.checked,
                      };
                    }}
                  />
                  <span>{$t(label as MessageKey)}</span>
                </label>
              </li>
            {/each}
          </ul>
          <footer class="browser-block-actions">
            {#if confirming === 'clear-all'}
              <span class="browser-confirm">
                <em>{$t('browser.clearConfirm')}</em>
                <button type="button" onclick={() => (confirming = '')}>
                  {$t('browser.cancel')}
                </button>
                <button
                  type="button"
                  class="destructive"
                  disabled={busy === 'clear'}
                  onclick={() => void clearBrowsingData()}
                >
                  {$t('browser.confirm')}
                </button>
              </span>
            {:else}
              <button
                type="button"
                class="destructive"
                onclick={() => (confirming = 'clear-all')}
              >
                {$t('browser.clearBrowsingData')}
              </button>
            {/if}
          </footer>
        </section>
      {:else}
        <section class="browser-block first">
          <h4>{$t('browser.importFrom')}</h4>
          <p class="browser-hint">{$t('browser.importKeychainNote')}</p>
          {#if sources.length === 0}
            <footer class="browser-block-actions">
              <button type="button" disabled={busy === 'scan'} onclick={() => void scanSources()}>
                {$t('browser.importScan')}
              </button>
            </footer>
            {#if scanned && busy !== 'scan'}
              <p class="browser-empty small">{$t('browser.importNone')}</p>
            {/if}
          {:else}
            <div class="browser-sources" use:scrollFade={sources.length}>
            {#each sources as source (source.id)}
              <div class="browser-source">
                <h5>{source.name}</h5>
                {#each source.profiles as profile (profile.id)}
                  <div class="browser-profile">
                    <span class="browser-item">
                      <strong>{profile.name}</strong>
                      {#if !profile.readable}<small class="warn">{profile.reason}</small>{/if}
                    </span>
                    {#if profile.readable}
                      <span class="browser-profile-choices">
                        <label>
                          <input
                            type="checkbox"
                            checked={importChoice[`${source.id}:${profile.id}`]?.cookies ?? true}
                            onchange={(event) => {
                              const key = `${source.id}:${profile.id}`;
                              importChoice = {
                                ...importChoice,
                                [key]: {
                                  cookies: event.currentTarget.checked,
                                  passwords: importChoice[key]?.passwords ?? false,
                                  history: importChoice[key]?.history ?? true,
                                },
                              };
                            }}
                          />
                          <span>{$t('browser.importCookies')}</span>
                        </label>
                        <label class:disabled={source.fileImportOnly}>
                          <input
                            type="checkbox"
                            disabled={source.fileImportOnly}
                            checked={importChoice[`${source.id}:${profile.id}`]?.passwords ?? false}
                            onchange={(event) => {
                              const key = `${source.id}:${profile.id}`;
                              importChoice = {
                                ...importChoice,
                                [key]: {
                                  cookies: importChoice[key]?.cookies ?? true,
                                  passwords: event.currentTarget.checked,
                                  history: importChoice[key]?.history ?? true,
                                },
                              };
                            }}
                          />
                          <span>{$t('browser.importPasswords')}</span>
                        </label>
                        <label>
                          <input
                            type="checkbox"
                            checked={importChoice[`${source.id}:${profile.id}`]?.history ?? true}
                            onchange={(event) => {
                              const key = `${source.id}:${profile.id}`;
                              importChoice = {
                                ...importChoice,
                                [key]: {
                                  cookies: importChoice[key]?.cookies ?? true,
                                  passwords: importChoice[key]?.passwords ?? false,
                                  history: event.currentTarget.checked,
                                },
                              };
                            }}
                          />
                          <span>{$t('browser.importHistory')}</span>
                        </label>
                        <button
                          type="button"
                          disabled={busy === `import:${source.id}:${profile.id}`}
                          onclick={() => void runImport(source, profile.id)}
                        >
                          {busy === `import:${source.id}:${profile.id}`
                            ? $t('browser.importing')
                            : $t('browser.import')}
                        </button>
                      </span>
                    {/if}
                  </div>
                {/each}
                {#if source.fileImportOnly}
                  <p class="browser-hint">{$t('browser.importPasswordsUnavailable')}</p>
                {/if}
                {#if source.family === 'safari'}
                  <p class="browser-hint">{$t('browser.importFullDiskNote')}</p>
                {/if}
              </div>
            {/each}
            </div>
          {/if}
        </section>

        <section class="browser-block">
          <h4>{$t('browser.importFile')}</h4>
          <p class="browser-hint">{$t('browser.importFileHint')}</p>
          <footer class="browser-block-actions">
            <button
              type="button"
              disabled={busy === 'import-file'}
              onclick={() => void importFromFile()}
            >
              {busy === 'import-file' ? $t('browser.importing') : $t('browser.importFile')}
            </button>
          </footer>
          {#if importSummary}
            <p class="browser-hint">{importSummary}</p>
          {/if}
          {#if importProblems.length}
            <p class="browser-hint warn">{$t('browser.importProblems')}</p>
            <ul class="browser-problems">
              {#each importProblems as problem, index (index)}
                <li>{problem}</li>
              {/each}
            </ul>
          {/if}
        </section>
      {/if}
    </div>
  </div>
</div>

<style>
  /* Metrics are the drive tab's, deliberately: the two are the same kind of
     page and reading as one surface matters more than either being novel. */
  /* The drive tab's own frame: the same edges every settings pane sits inside,
     so the section rail and the detail beside it line up with the rest of
     Settings rather than running to the window edge. */
  .browser-tab{flex:1;min-height:0;display:flex;flex-direction:column;overflow:hidden;padding:2px var(--options-detail-edge) 16px 0}
  .browser-error{margin:0 0 10px;padding:7px 10px;border-radius:8px;background:#fbeaea;color:#8f3a3a;font-size:11px}
  :global(:root[data-theme="dark"]) .browser-error{background:#321f1f;color:#eea7a7}

  .browser-body{min-width:0;min-height:0;flex:1;display:grid;grid-template-columns:186px minmax(0,1fr);gap:var(--options-divider-gap)}
  .browser-rail{min-width:0;min-height:0;display:flex;flex-direction:column;gap:4px;margin:0;padding:6px 12px;overflow-y:auto;list-style:none;border-right:1px solid var(--neutral-200)}
  .browser-rail>li{margin:0}
  .browser-rail button{width:100%;height:32px;display:flex;align-items:center;gap:10px;border:0;border-radius:10px;padding:0 9px;background:transparent;color:var(--neutral-600);cursor:pointer;font-family:inherit;text-align:left;font-size:13px;transition:color .15s,background .15s}
  .browser-rail button:hover,.browser-rail button:focus-visible{outline:0;background:var(--neutral-100);color:var(--neutral-950)}
  .browser-rail button.active{background:var(--neutral-200);color:var(--neutral-950);font-weight:540}
  .browser-rail button>span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .browser-rail button :global(svg){flex:none}

  .browser-detail{min-width:0;min-height:0;overflow-y:auto;padding-right:2px}
  .browser-block{margin-bottom:16px;padding-top:12px;border-top:1px solid var(--neutral-200)}
  .browser-block.first{padding-top:0;border-top:0}
  .browser-block h4{margin:0 0 7px;color:var(--neutral-900);font-size:11.5px;font-weight:570}

  /* The icon is centred on the text's optical centre, not the row box's. */
  .browser-row{display:flex;align-items:center;gap:11px;min-height:52px}
  .browser-mark{flex:none;display:grid;place-items:center;color:var(--neutral-600)}
  .browser-copy{min-width:0;flex:1;display:flex;flex-direction:column;gap:1px}
  .browser-copy h4{margin:0;font-size:11.5px;font-weight:545}
  .browser-copy small{color:var(--neutral-500);font-size:10px}

  .browser-toggle{width:36px;height:20px;flex:none;position:relative;border:0;border-radius:10px;padding:0;background:var(--neutral-300);cursor:pointer;transition:background .15s ease}
  .browser-toggle.enabled{background:var(--neutral-950)}
  .browser-toggle>span{position:absolute;top:2px;left:2px;width:16px;height:16px;border-radius:50%;background:var(--app-surface);transition:transform .15s ease}
  .browser-toggle.enabled>span{transform:translateX(16px)}
  .browser-toggle:disabled{cursor:default;opacity:.5}

  .browser-value{display:flex;align-items:center;gap:9px;margin:0 0 4px;font-size:11px}
  .browser-value code{overflow:hidden;padding:2px 6px;border-radius:5px;background:var(--neutral-100);color:var(--neutral-800);text-overflow:ellipsis;white-space:nowrap;font-size:10.5px}
  .browser-value button{height:26px;flex:none;border:1px solid var(--neutral-200);border-radius:7px;padding:0 10px;background:var(--app-surface);color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  .browser-value button:hover{background:var(--neutral-100);color:var(--neutral-950)}
  .browser-value button:disabled{cursor:default;opacity:.5}

  .browser-list{margin:8px 0 0;padding:0;list-style:none;display:flex;flex-direction:column;gap:6px}
  .browser-list li{display:flex;align-items:center;gap:10px;padding:8px 11px;border:1px solid var(--neutral-200);border-radius:9px}
  .browser-item{min-width:0;flex:1;display:flex;flex-direction:column;gap:1px}
  .browser-item strong{overflow:hidden;color:var(--neutral-900);text-overflow:ellipsis;white-space:nowrap;font-size:11.5px;font-weight:545}
  .browser-item small{overflow:hidden;color:var(--neutral-500);text-overflow:ellipsis;white-space:nowrap;font-size:10px}
  .browser-item small.warn{color:var(--danger-500)}
  .browser-secret{flex:none;max-width:180px;overflow:hidden;padding:2px 6px;border-radius:5px;background:var(--neutral-100);color:var(--neutral-800);text-overflow:ellipsis;white-space:nowrap;font-size:10.5px}

  .browser-item-actions{flex:none;display:flex;align-items:center;gap:2px}
  .browser-item-actions button{width:24px;height:24px;display:grid;place-items:center;border:0;border-radius:6px;background:transparent;color:var(--neutral-500);cursor:pointer}
  /* The icon darkens on hover; no pill, no circle. */
  .browser-item-actions button:hover{color:var(--neutral-950)}
  .browser-item-actions button.destructive:hover{color:var(--danger-500)}
  .browser-item-actions button.text{width:auto;padding:0 4px;font-family:inherit;font-size:10.5px;font-weight:550}
  .browser-item-actions button.destructive.text{color:var(--danger-500)}

  .browser-empty{display:grid;place-items:center;min-height:120px;margin:0;color:var(--neutral-400);font-size:11px}
  .browser-empty.small{min-height:60px}

  .browser-hint{max-width:520px;margin:5px 0 0;color:var(--neutral-500);font-size:10.5px;line-height:1.5}
  .browser-hint.warn{color:var(--danger-500)}

  .browser-block-actions{display:flex;justify-content:flex-end;gap:7px;margin-top:12px}
  .browser-block-actions button{height:29px;flex:none;white-space:nowrap;border:1px solid var(--neutral-200);border-radius:8px;padding:0 12px;background:var(--app-surface);color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:11px;font-weight:550}
  .browser-block-actions button:hover{background:var(--neutral-100);color:var(--neutral-950)}
  .browser-block-actions button.destructive{color:var(--danger-500)}
  .browser-block-actions button:disabled{cursor:default;opacity:.5}

  /* The confirmation takes the row over rather than opening a dialog: what is
     about to be cleared is already on screen behind it. */
  .browser-confirm{display:flex;align-items:center;gap:7px}
  .browser-confirm em{color:var(--neutral-600);font-style:normal;font-size:10.5px}
  .browser-confirm button{height:24px;border:1px solid var(--neutral-200);border-radius:7px;padding:0 9px;background:var(--app-surface);color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  .browser-confirm button.destructive{border-color:var(--danger-500);color:var(--danger-500)}

  .browser-columns,.browser-table{width:100%;table-layout:fixed;border-collapse:collapse}
  .browser-columns th{padding:0 8px 6px;color:var(--neutral-500);text-align:left;font-size:10px;font-weight:540}
  .browser-columns th:nth-child(1),.browser-table td:nth-child(1){width:44%}
  .browser-columns th:nth-child(2),.browser-table td:nth-child(2){width:34%}
  .browser-table-wrap{max-height:280px;overflow-y:auto}
  .browser-table td{padding:6px 8px;border-top:1px solid var(--neutral-200);color:var(--neutral-800);font-size:11px}
  .browser-site{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .browser-table select{width:100%;height:24px;border:1px solid var(--neutral-200);border-radius:6px;background:var(--app-surface);color:var(--neutral-800);font-family:inherit;font-size:10.5px}

  .browser-checks{margin:8px 0 0;padding:0;list-style:none;display:flex;flex-direction:column;gap:6px}
  .browser-checks label{display:flex;align-items:center;gap:7px;color:var(--neutral-800);font-size:11px}
  .browser-checks input{width:14px;height:14px;flex:none}

  /* A fixed window onto the results rather than a list that grows with however
     many browsers the machine happens to have: eight profiles pushed the file
     import below the fold. Five rows is the height; the rest scrolls, and the
     edges fade so a cut-off row reads as more-below rather than the end. */
  .browser-sources{max-height:252px;overflow-y:auto}
  .browser-history-head{display:flex;align-items:center;justify-content:space-between;gap:12px}
  .browser-history-head h4{margin:0}
  .browser-search{width:190px;height:26px;flex:none;border:1px solid var(--neutral-200);border-radius:8px;padding:0 9px;background:var(--neutral-50);color:var(--neutral-950);outline:0;font-family:inherit;font-size:11px}
  .browser-search:focus{border-color:var(--neutral-400);background:var(--app-surface)}
  /* History owns the available detail height. Its controls stay in place while
     the results grow and shrink with the Settings window. */
  .browser-detail.history-detail{display:flex;flex-direction:column;overflow:hidden}
  .browser-history-block{min-height:0;flex:1;display:flex;flex-direction:column;margin-bottom:0}
  .browser-history-block>.browser-empty{min-height:0;flex:1}
  .browser-history-list{min-height:0;flex:1;overflow-y:auto}
  .browser-source{margin-top:10px}
  .browser-source:first-child{margin-top:0}
  .browser-source h5{margin:0 0 5px;color:var(--neutral-800);font-size:11px;font-weight:560}
  .browser-profile{display:flex;align-items:center;gap:10px;padding:8px 11px;border:1px solid var(--neutral-200);border-radius:9px;margin-bottom:6px}
  .browser-profile-choices{flex:none;display:flex;align-items:center;gap:9px}
  .browser-profile-choices label{display:flex;align-items:center;gap:5px;color:var(--neutral-700);font-size:10.5px}
  .browser-profile-choices label.disabled{opacity:.45}
  .browser-profile-choices input{width:13px;height:13px;flex:none}
  .browser-profile-choices button{height:25px;border:1px solid var(--neutral-200);border-radius:7px;padding:0 10px;background:var(--app-surface);color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  .browser-profile-choices button:hover{background:var(--neutral-100);color:var(--neutral-950)}
  .browser-profile-choices button:disabled{cursor:default;opacity:.5}

  .browser-problems{margin:5px 0 0;padding-left:16px;color:var(--neutral-500);font-size:10px;line-height:1.6}
</style>
