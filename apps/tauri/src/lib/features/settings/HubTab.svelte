<script lang="ts">
  import {onMount, tick} from 'svelte';
  import type {
    CommsBridgeAccountDto,
    CommsBridgeDto,
    CommsEmailAccountDto,
    CommsLoginStepDto,
    CommsPlatform,
    CommsStatusDto,
    CommsMailProvider,
    MailSignatureDto,
    PolymuxApi,
    SaveEmailAccountRequest,
    SystemPermissionKind,
  } from '@polymux/protocol';
  import {COMMS_EMAIL_PRESETS, permissionPrompts, presetForHost} from '@polymux/protocol';
  import {readableError} from '../../shared/errors';
  import {invalidateHubCache} from '../../shared/state/hubCache';
  import {loadSettingsHubSnapshot, settingsHubSnapshot} from '../../shared/state/settingsPreload';
  import {scrollFade} from '../../shared/scrollFade';
  import {qrSvgPath} from '../../shared/qr';
  import {bridgeLogo, mailLogo} from '../../shared/options/platformBrands';
  import {emailPresetHint, qrInstructions} from '../../../i18n/names';
  import {locale, plural, t, translate, withLocale} from '../../../i18n';
  import Icon from '../../shared/components/Icon.svelte';
  import Menu from '../../shared/components/Menu.svelte';
  import RichSignatureEditor from './RichSignatureEditor.svelte';

  export let api: PolymuxApi;

  /** Which providers this build can sign in to; empty draws no buttons. */
  $: mailSignInProviders = status?.email.signInProviders ?? [];
  $: mailPresetOptions = visibleMailPresets(mailSignInProviders).map((item) => ({
    value: item.value,
    label: item.label,
  }));

  /**
   * The providers still worth picking by hand. One that is an app password and
   * a hostname is what "Other" already is; Gmail and Outlook come out only
   * once their sign-in is offered, so a build with no client registered still
   * leaves a way into those mailboxes.
   *
   * A function rather than a reactive statement because the blank form is
   * built during setup, before those have run.
   */
  function visibleMailPresets(providers: readonly CommsMailProvider[]) {
    return COMMS_EMAIL_PRESETS.filter(
      (item) =>
        !item.hidden &&
        !(item.value === 'gmail' && providers.includes('google')) &&
        !(item.value === 'outlook' && providers.includes('microsoft')),
    );
  }

  function mailProviderLabel(provider: CommsMailProvider): string {
    return provider === 'google' ? 'Google' : 'Microsoft';
  }

  /**
   * The provider's own page opens over Polymux; everything after it — the
   * address, the servers, the tokens — comes back from the sign-in, so there
   * is nothing left to fill in and the form closes.
   */
  async function signInMailbox(provider: CommsMailProvider): Promise<void> {
    busy = `email-signin:${provider}`;
    try {
      status = await api.comms.emailSignIn(provider);
      editingEmail = false;
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  type Section = {kind: 'bridge'; platform: CommsPlatform} | {kind: 'mail'};

  let status: CommsStatusDto | null = settingsHubSnapshot.status;
  $: if (status && status !== settingsHubSnapshot.status) {
    settingsHubSnapshot.status = status;
    settingsHubSnapshot.loadedAt = Date.now();
  }
  // Mail heads the rail whatever the fleet turns out to hold, so the opening
  // section is set here rather than waiting on the first status — the pane is
  // filled the moment the tab is opened, not a beat later. `keepSelectionOnRail`
  // still moves it if a filter hides Mail.
  let selected: Section | null = {kind: 'mail'};
  let loading = !settingsHubSnapshot.status;
  let error = '';
  let busy = '';
  let emailHealthChecking = false;

  // Bridge linking
  let step: CommsLoginStepDto | null = null;
  let stepPlatform: CommsPlatform | null = null;
  let stepValues: Record<string, string> = {};
  let stepError = '';
  let waiting = false;
  /**
   * The platform whose login has been confirmed and whose account has not been
   * reported by the bridge yet. Holds the pane on "Signing in…" across that
   * gap, instead of dropping back to the log-in button it started from.
   */
  let settling: CommsPlatform | null = null;

  // Bridge setup — the credentials a bridge needs before it can run at all.
  let setupValues: Record<string, string> = {};
  let setupPlatform: CommsPlatform | null = null;

  // Email form
  let editingEmail = false;
  let emailForm = blankEmail();
  let emailOriginalId = '';
  let emailPassword = '';

  type SignatureAccountDraft = {
    signatures: MailSignatureDto[];
    defaultSignatureId: string | null;
  };
  type SignatureRow = {
    key: string;
    accountId: string;
    accountEmail: string;
    signature: MailSignatureDto;
    isDefault: boolean;
  };
  let editingSignatures = false;
  let signatureAccountId = 'all';
  let signatureSelectedKey = '';
  let signatureDrafts: Record<string, SignatureAccountDraft> = {};
  let dirtySignatureAccounts: string[] = [];

  onMount(() => {
    const unsubscribe = api.comms.subscribe((next) => {
      status = next;
    });
    void load().then(() => checkEmailConnections());
    const healthTimer = window.setInterval(() => void checkEmailConnections(), 60_000);
    const onVisible = () => {
      if (!document.hidden) void checkEmailConnections();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      window.clearInterval(healthTimer);
      document.removeEventListener('visibilitychange', onVisible);
      unsubscribe();
    };
  });

  /** Keep mailbox state current while reconnection remains background work. */
  async function checkEmailConnections(): Promise<void> {
    if (document.hidden || emailHealthChecking || !status?.email.accounts.length) return;
    emailHealthChecking = true;
    try {
      const results = await Promise.allSettled(status.email.accounts.map((account) => api.comms.emailTest(account.id)));
      if (status) {
        const byId = new Map(results.flatMap((result) => result.status === 'fulfilled' ? [[result.value.id, result.value] as const] : []));
        status = {...status, email: {...status.email, accounts: status.email.accounts.map((account) => byId.get(account.id) ?? account)}};
      }
    } finally {
      emailHealthChecking = false;
    }
  }

  async function load(): Promise<void> {
    // Only an empty pane waits — a reopened tab already has rows on screen and
    // corrects them in place rather than blanking back to the loading rail.
    loading = !status;
    error = '';
    try {
      status = await loadSettingsHubSnapshot(api, 5_000);
    } catch (cause) {
      error = readableError(cause);
    } finally {
      loading = false;
    }
  }

  async function refresh(): Promise<void> {
    busy = 'refresh';
    try {
      status = await api.comms.refresh();
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  /**
   * Asks macOS for the grant a bridge is held back by, then re-reads the fleet
   * so a bridge freed by it stops reporting itself as blocked. Full Disk
   * Access opens its pane rather than prompting, so that answer only arrives
   * once the user has been and come back.
   */
  async function grant(permission: SystemPermissionKind): Promise<void> {
    busy = 'grant';
    try {
      await api.permissions.request(permission);
      status = await api.comms.refresh();
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  function blankEmail(): SaveEmailAccountRequest {
    // The first one the picker is actually showing: with Gmail signed in
    // through its own button it is no longer the head of the list.
    const preset =
      visibleMailPresets(status?.email.signInProviders ?? [])[0] ?? COMMS_EMAIL_PRESETS[0];
    return {
      id: '',
      email: '',
      displayName: '',
      preset: preset.value,
      imapHost: preset.imapHost,
      imapPort: preset.imapPort,
      imapEncryption: preset.imapEncryption,
      smtpHost: preset.smtpHost,
      smtpPort: preset.smtpPort,
      smtpEncryption: preset.smtpEncryption,
    };
  }

  /** Filling in a provider's servers is the whole point of picking one. */
  function applyPreset(value: string): void {
    const preset = COMMS_EMAIL_PRESETS.find((item) => item.value === value);
    if (!preset) return;
    emailForm = {
      ...emailForm,
      preset: preset.value,
      imapHost: preset.imapHost,
      imapPort: preset.imapPort,
      imapEncryption: preset.imapEncryption,
      smtpHost: preset.smtpHost,
      smtpPort: preset.smtpPort,
      smtpEncryption: preset.smtpEncryption,
    };
  }

  /**
   * What to tell the user about credentials here.
   *
   * A provider without its own pill is set up as "Other", so its hint has to
   * follow the servers instead — otherwise the one thing that is not guessable
   * about iCloud or Lark, that the account password will not work, goes
   * unsaid.
   */
  $: presetHint = withLocale(
    $locale,
    emailPresetHint(
      (emailForm.preset === 'custom'
        ? presetForHost(emailForm.smtpHost)?.value ?? presetForHost(emailForm.imapHost)?.value
        : undefined) ?? emailForm.preset,
    ),
  );
  $: bridges = status?.bridges ?? [];
  $: emailAccounts = status?.email.accounts ?? [];
  $: signatureRows = signatureRowsFor(signatureAccountId, signatureDrafts, emailAccounts);
  $: selectedSignatureRow = signatureRows.find((row) => row.key === signatureSelectedKey) ?? null;
  $: signatureAccountOptions = [
    {value: 'all', label: $t('hub.allSignatures'), icon: 'signature' as const},
    ...emailAccounts.map((account) => ({
      value: account.id,
      label: account.email,
      icon: 'mail' as const,
    })),
  ];
  $: signatureScopeCount = signatureRows.length;
  $: signaturesSaveable = dirtySignatureAccounts.length > 0 &&
    Object.values(signatureDrafts).every((draft) =>
      draft.signatures.every((signature) => signature.name.trim().length > 0),
    );
  $: signedIn = status?.hub.status === 'signed-in';
  // svelte-check does not narrow `selected` across the ternary, so the tagged
  // branch is pulled into a local first.
  $: activeBridge = pickBridge(selected, bridges);
  $: mailOpen = selected?.kind === 'mail';
  /** A bridge that cannot even start until its own credentials are recorded. */
  $: needsSetup = Boolean(activeBridge?.setup && !activeBridge.setup.configured);
  $: setupReady = Boolean(
    activeBridge?.setup?.fields.every((field) => (setupValues[field.id] ?? '').trim() !== ''),
  );
  // Warm every installed bridge as soon as the Hub status arrives. Login
  // methods belong to the running bridge, so waiting until a row is selected
  // makes quick navigation flash an avoidable intermediate state.
  $: void warmDormantBridges(status);
  // Typed credentials belong to the platform they were typed under, not to
  // whichever one the rail lands on next.
  $: if (activeBridge?.platform !== setupPlatform) {
    setupPlatform = activeBridge?.platform ?? null;
    setupValues = {};
  }

  /** Platforms already asked for this session; a bridge starts once. */
  const wokenPlatforms = new Set<CommsPlatform>();

  async function warmDormantBridges(snapshot: CommsStatusDto | null): Promise<void> {
    const platforms = (snapshot?.bridges ?? [])
      .filter((bridge) => bridge.state === 'dormant' && !wokenPlatforms.has(bridge.platform))
      .map((bridge) => bridge.platform);
    if (platforms.length === 0) return;
    for (const platform of platforms) wokenPlatforms.add(platform);
    await Promise.allSettled(platforms.map((platform) => api.comms.wake(platform)));
    const next = await api.comms.status().catch((): null => null);
    if (next) status = next;
  }

  async function wakeSelected(section: Section | null): Promise<void> {
    if (section?.kind !== 'bridge') return;
    const platform = section.platform;
    if (wokenPlatforms.has(platform)) return;
    wokenPlatforms.add(platform);
    const next = await api.comms.wake(platform).catch((): null => null);
    if (next) status = next.status;
  }

  async function saveSetup(platform: CommsPlatform): Promise<void> {
    busy = 'setup';
    try {
      status = await api.comms.bridgeSetup(platform, setupValues);
      setupValues = {};
      error = '';
      // Recorded credentials only take effect once the bridge is back up, and
      // it is parked until something asks for it.
      wokenPlatforms.delete(platform);
      await wakeSelected({kind: 'bridge', platform});
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }
  $: mailSummary = emailAccounts.length === 0
      ? $t('hub.noMailboxes')
      : emailAccounts.length === 1
        ? emailAccounts[0].email
        : plural('hub.mailboxes', emailAccounts.length);
  $: mailState = emailAccounts.some((account) => account.status === 'error')
      ? 'error'
      : emailAccounts.length > 0
        ? 'connected'
        : 'logged-out';

  /**
   * Mail and the bridges share one rail, so they are flattened into a single
   * list before filtering and sorting — otherwise "Name A–Z" would sort the
   * bridges around a Mail row pinned outside the ordering.
   */
  $: railEntries = [
    {
      key: 'mail',
      section: {kind: 'mail'} as Section,
      name: $t('hub.mail'),
      logo: mailLogo('custom'),
      state: mailState,
      label: mailSummary,
    },
    ...bridges.map((bridge) => ({
      key: bridge.platform,
      section: {kind: 'bridge', platform: bridge.platform} as Section,
      name: bridge.name,
      logo: bridgeLogo(bridge.platform),
      state: bridge.state as string,
      label: bridgeLabel(bridge),
    })),
  ];
  $: visibleRail = sortRail(railEntries);
  $: railEmpty = visibleRail.length === 0;
  // In a function, and handed its inputs, because what a reactive statement
  // re-runs on is what it reads at the top level.
  $: keepSelectionOnRail(visibleRail, railEntries);

  /**
   * A platform that has left the fleet would otherwise leave the detail pane
   * showing something the rail no longer offers a way back to.
   *
   * Deliberately not a reason to move on every status read: signing in changes
   * a platform's state, and a selection that jumped on that would throw you off
   * WhatsApp at the moment logging in to it succeeded — the one screen you are
   * certainly still reading.
   */
  function keepSelectionOnRail(visible: typeof railEntries, all: typeof railEntries): void {
    if (!visible.length) return;
    if (selected !== null && all.some((entry) => isSelected(entry.section, selected))) return;
    selected = visible[0].section;
  }
  // The masks are painted from measurements, so they are re-taken whenever the
  // list's contents change under them.
  $: railContentKey = `${visibleRail.length}`;

  function isSelected(section: Section, current: Section | null): boolean {
    if (!current || current.kind !== section.kind) return false;
    return section.kind !== 'bridge' || (current.kind === 'bridge' && current.platform === section.platform);
  }

  /**
   * What is linked, first. The catalogue order underneath it is a fixed list
   * the user has no part in, so a platform they actually connected can sit at
   * the bottom of it — WeChat does — while a dozen rows they have never linked
   * are what the rail opens on. Sorting is stable, so within each group the
   * catalogue order stands.
   *
   * Mail keeps the top whatever its own state, matching where the tab opens.
   */
  function sortRail<T extends {name: string; key: string; state: string}>(entries: T[]): T[] {
    return [...entries].sort((a, b) => railRank(a) - railRank(b));
  }

  function railRank(entry: {key: string; state: string}): number {
    if (entry.key === 'mail') return 0;
    switch (entry.state) {
      case 'connected':
        return 1;
      // Linked, and saying so: it belongs with the platforms in use rather
      // than among the ones never set up.
      case 'connecting':
      case 'error':
        return 2;
      default:
        return 3;
    }
  }

  function pickBridge(section: Section | null, list: CommsBridgeDto[]): CommsBridgeDto | null {
    if (section?.kind !== 'bridge') return null;
    return list.find((item) => item.platform === section.platform) ?? null;
  }

  function bridgeLabel(bridge: CommsBridgeDto): string {
    switch (bridge.state) {
      case 'connected':
        return bridge.accounts.length > 1
          ? plural('drive.accounts', bridge.accounts.length)
          : (bridge.accounts[0]?.name ?? translate('drive.stateConnected'));
      case 'connecting':
        return translate('hub.connecting');
      case 'logged-out':
        return translate('hub.notLinked');
      case 'error':
        return translate('drive.stateError');
      case 'unavailable':
        return translate('hub.localOnly');
      case 'unreachable':
        return translate('hub.hubOffline');
      case 'dormant':
        return translate('hub.notLinked');
      default:
        return translate('hub.unknown');
    }
  }

  /**
   * The hub sets itself up on its own; this only exists so a bridge held back
   * by a hub that has not provisioned yet is not a dead end.
   */
  async function connect(): Promise<void> {
    busy = 'connect';
    try {
      status = await api.comms.connect();
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  /** Installer links come from the trusted bridge catalogue, but keep the
   * renderer from handing any non-web scheme to the operating system. */
  function openInstall(url: string): void {
    if (!/^https?:\/\//i.test(url)) return;
    void api.browser.openExternal(url);
  }

  // Versions the login flow so a cancelled attempt's pending waits cannot
  // write their stale steps over a newer flow (loginWait blocks for minutes).
  let linkAttempt = 0;

  async function startLink(platform: CommsPlatform, flowId: string): Promise<void> {
    const mine = ++linkAttempt;
    settling = null;
    stepPlatform = platform;
    stepError = '';
    stepValues = {};
    busy = `link:${platform}`;
    try {
      const next = await api.comms.loginStart(platform, flowId);
      if (mine !== linkAttempt) return;
      step = next;
      await advance(mine);
    } catch (cause) {
      if (mine !== linkAttempt) return;
      stepError = readableError(cause);
      step = null;
    } finally {
      if (mine === linkAttempt) busy = '';
    }
  }

  /**
   * Drives whichever step the bridge returned that needs no typing: a QR or
   * code has to be waited on, and a cookie sign-in opens its own window.
   */
  async function advance(mine: number): Promise<void> {
    while (step && stepPlatform) {
      if (step.type === 'display_and_wait') {
        waiting = true;
        try {
          const next = await api.comms.loginWait(stepPlatform, step.loginId, step.stepId);
          if (mine !== linkAttempt) return;
          step = next;
        } catch (cause) {
          if (mine !== linkAttempt) return;
          stepError = readableError(cause);
          waiting = false;
          return;
        } finally {
          if (mine === linkAttempt) waiting = false;
        }
        continue;
      }
      if (step.type === 'cookies') {
        waiting = true;
        const platform = stepPlatform;
        const loginId = step.loginId;
        try {
          const next = await api.comms.loginCookies(stepPlatform, step.loginId, step.stepId);
          if (mine !== linkAttempt) return;
          step = next;
        } catch (cause) {
          if (mine !== linkAttempt) return;
          // The sign-in window is gone — closed, or it failed to open. End the
          // flow so the log-in button comes back; the error stays on screen.
          stepError = readableError(cause);
          step = null;
          stepPlatform = null;
          status = await api.comms.loginCancel(platform, loginId).catch(() => status);
          return;
        } finally {
          if (mine === linkAttempt) waiting = false;
        }
        continue;
      }
      if (step.type === 'complete') {
        const platform = stepPlatform;
        step = null;
        stepPlatform = null;
        await settle(platform, mine);
        return;
      }
      return;
    }
  }

  /**
   * Waits for a just-confirmed login to show up on the fleet, holding the panel
   * on "Signing in…" until it does.
   *
   * `status` rather than `refresh`: refresh retries every blocked bridge, so it
   * pokes the very session that has just been established, and it runs through
   * `busy` — which took the whole detail pane through a loading state for a
   * login that had already succeeded. Polled because the bridge reports the new
   * account whenever it has finished bringing it up, with nothing to announce it.
   */
  const SETTLE_POLL_MS = 700;
  const SETTLE_LIMIT_MS = 20000;

  async function settle(platform: CommsPlatform, mine: number): Promise<void> {
    settling = platform;
    const deadline = Date.now() + SETTLE_LIMIT_MS;
    try {
      for (;;) {
        const next = await api.comms.status().catch(() => null);
        if (mine !== linkAttempt) return;
        if (next) status = next;
        const bridge = next?.bridges.find((item) => item.platform === platform);
        if (bridge?.accounts.length) return;
        if (Date.now() >= deadline) return;
        await new Promise((resolve) => setTimeout(resolve, SETTLE_POLL_MS));
        if (mine !== linkAttempt) return;
      }
    } finally {
      if (settling === platform) settling = null;
    }
  }

  async function submitStep(): Promise<void> {
    if (!step || step.type !== 'user_input' || !stepPlatform) return;
    stepError = '';
    busy = 'step';
    const mine = linkAttempt;
    try {
      const next = await api.comms.loginSubmit(stepPlatform, step.loginId, step.stepId, stepValues);
      if (mine !== linkAttempt) return;
      step = next;
      stepValues = {};
      await advance(mine);
    } catch (cause) {
      if (mine !== linkAttempt) return;
      stepError = readableError(cause);
    } finally {
      if (mine === linkAttempt) busy = '';
    }
  }

  async function cancelLink(): Promise<void> {
    linkAttempt++;
    settling = null;
    if (!step || !stepPlatform) {
      step = null;
      stepPlatform = null;
      return;
    }
    const platform = stepPlatform;
    const loginId = step.loginId;
    step = null;
    stepPlatform = null;
    stepError = '';
    try {
      status = await api.comms.loginCancel(platform, loginId);
    } catch {
      // The dialog is already closed; a failed cancel is not worth reporting.
    }
  }

  async function unlink(bridge: CommsBridgeDto, account: CommsBridgeAccountDto): Promise<void> {
    busy = `unlink:${bridge.platform}:${account.id}`;
    try {
      status = await api.comms.bridgeLogout(bridge.platform, account.id);
      // The hub holds this account's chats and messages in memory; without
      // this it keeps drawing them after the account is gone.
      invalidateHubCache();
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  function editEmail(account: CommsEmailAccountDto): void {
    emailOriginalId = account.id;
    emailPassword = '';
    emailForm = {
      originalId: account.id,
      id: account.id,
      email: account.email,
      displayName: account.displayName ?? '',
      preset: 'custom',
      imapHost: account.incoming.host ?? '',
      imapPort: account.incoming.port ?? 993,
      imapEncryption: account.incoming.encryption ?? 'tls',
      imapLogin: account.incoming.login ?? '',
      smtpHost: account.outgoing.host ?? '',
      smtpPort: account.outgoing.port ?? 587,
      smtpEncryption: account.outgoing.encryption ?? 'start-tls',
      smtpLogin: account.outgoing.login ?? '',
    };
    editingEmail = true;
  }

  function addEmail(): void {
    emailOriginalId = '';
    emailPassword = '';
    emailForm = blankEmail();
    editingEmail = true;
  }

  function signatureKey(accountId: string, signatureId: string): string {
    return `${accountId}:${signatureId}`;
  }

  function signatureRowsFor(
    accountId: string,
    drafts: Record<string, SignatureAccountDraft>,
    accounts: CommsEmailAccountDto[],
  ): SignatureRow[] {
    const visible = accountId === 'all'
      ? accounts
      : accounts.filter((account) => account.id === accountId);
    return visible.flatMap((account) => {
      const draft = drafts[account.id] ?? {
        signatures: account.signatures,
        defaultSignatureId: account.defaultSignatureId,
      };
      return draft.signatures.map((signature) => ({
        key: signatureKey(account.id, signature.id),
        accountId: account.id,
        accountEmail: account.email,
        signature,
        isDefault: draft.defaultSignatureId === signature.id,
      }));
    });
  }

  function openSignatures(accountId = 'all'): void {
    signatureDrafts = Object.fromEntries(
      emailAccounts.map((account) => [
        account.id,
        {
          signatures: account.signatures.map((signature) => ({...signature})),
          defaultSignatureId: account.defaultSignatureId,
        },
      ]),
    );
    dirtySignatureAccounts = [];
    signatureAccountId = accountId;
    const rows = signatureRowsFor(accountId, signatureDrafts, emailAccounts);
    signatureSelectedKey = rows[0]?.key ?? '';
    editingEmail = false;
    editingSignatures = true;
  }

  function chooseSignatureAccount(accountId: string): void {
    signatureAccountId = accountId;
    const rows = signatureRowsFor(accountId, signatureDrafts, emailAccounts);
    signatureSelectedKey = rows[0]?.key ?? '';
  }

  function markSignaturesDirty(accountId: string): void {
    if (!dirtySignatureAccounts.includes(accountId))
      dirtySignatureAccounts = [...dirtySignatureAccounts, accountId];
  }

  function addSignature(): void {
    const accountId =
      (signatureAccountId === 'all' ? selectedSignatureRow?.accountId : signatureAccountId) ??
      emailAccounts[0]?.id;
    if (!accountId) return;
    const current = signatureDrafts[accountId] ?? {signatures: [], defaultSignatureId: null};
    const signature: MailSignatureDto = {
      id: crypto.randomUUID(),
      name: translate('hub.newSignature'),
      body: '',
      html: null,
    };
    signatureDrafts = {
      ...signatureDrafts,
      [accountId]: {
        signatures: [...current.signatures, signature],
        defaultSignatureId: current.defaultSignatureId ?? signature.id,
      },
    };
    signatureAccountId = accountId;
    signatureSelectedKey = signatureKey(accountId, signature.id);
    markSignaturesDirty(accountId);
  }

  function removeSignature(): void {
    if (!selectedSignatureRow) return;
    const {accountId, signature} = selectedSignatureRow;
    const current = signatureDrafts[accountId];
    if (!current) return;
    const signatures = current.signatures.filter((item) => item.id !== signature.id);
    signatureDrafts = {
      ...signatureDrafts,
      [accountId]: {
        signatures,
        defaultSignatureId:
          current.defaultSignatureId === signature.id ? (signatures[0]?.id ?? null) : current.defaultSignatureId,
      },
    };
    markSignaturesDirty(accountId);
    const rows = signatureRowsFor(signatureAccountId, signatureDrafts, emailAccounts);
    signatureSelectedKey = rows[0]?.key ?? '';
  }

  function updateSignature(field: 'name' | 'body', value: string): void {
    if (!selectedSignatureRow) return;
    const {accountId, signature} = selectedSignatureRow;
    const current = signatureDrafts[accountId];
    if (!current) return;
    signatureDrafts = {
      ...signatureDrafts,
      [accountId]: {
        ...current,
        signatures: current.signatures.map((item) =>
          item.id === signature.id ? {...item, [field]: value} : item,
        ),
      },
    };
    markSignaturesDirty(accountId);
  }

  function updateSignatureContent(value: {body: string; html: string | null}): void {
    if (!selectedSignatureRow) return;
    const {accountId, signature} = selectedSignatureRow;
    const current = signatureDrafts[accountId];
    if (!current) return;
    signatureDrafts = {
      ...signatureDrafts,
      [accountId]: {
        ...current,
        signatures: current.signatures.map((item) =>
          item.id === signature.id ? {...item, body: value.body, html: value.html} : item,
        ),
      },
    };
    markSignaturesDirty(accountId);
  }

  function setDefaultSignature(on: boolean): void {
    if (!selectedSignatureRow) return;
    const {accountId, signature} = selectedSignatureRow;
    const current = signatureDrafts[accountId];
    if (!current) return;
    signatureDrafts = {
      ...signatureDrafts,
      [accountId]: {...current, defaultSignatureId: on ? signature.id : null},
    };
    markSignaturesDirty(accountId);
  }

  async function saveSignatures(): Promise<void> {
    if (!signaturesSaveable) return;
    busy = 'signatures-save';
    try {
      for (const account of dirtySignatureAccounts) {
        const draft = signatureDrafts[account];
        if (!draft) continue;
        status = await api.comms.emailSignaturesSave({
          account,
          signatures: draft.signatures.map((signature) => ({
            ...signature,
            name: signature.name.trim(),
          })),
          defaultSignatureId: draft.defaultSignatureId,
        });
      }
      dirtySignatureAccounts = [];
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  async function saveEmail(): Promise<void> {
    busy = 'email-save';
    try {
      status = await api.comms.emailSave({
        ...emailForm,
        originalId: emailOriginalId || undefined,
        password: emailPassword || undefined,
      });
      editingEmail = false;
      emailPassword = '';
      selected = {kind: 'mail'};
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  async function removeEmail(account: CommsEmailAccountDto): Promise<void> {
    busy = `email-remove:${account.id}`;
    try {
      status = await api.comms.emailRemove(account.id);
      error = '';
    } catch (cause) {
      error = readableError(cause);
    } finally {
      busy = '';
    }
  }

  $: qr =
    step?.type === 'display_and_wait' && step.display === 'qr' && step.data
      ? qrSvgPath(step.data)
      : null;

  function fieldType(type: string): string {
    if (type === 'password' || type === 'token') return 'password';
    if (type === 'email') return 'email';
    if (type === 'phone_number') return 'tel';
    if (type === 'url') return 'url';
    return 'text';
  }
</script>

<div class="comms" role="tabpanel">
    {#if error}
      <div class="comms-error" role="alert">
        <span>{error}</span>
        <button type="button" onclick={() => void refresh()} disabled={busy === 'refresh'}>{$t('common.retry')}</button>
      </div>
    {/if}

    <!-- The rail chrome — divider, tool row, list frame — is painted before the
         fleet lands, so the first status fills a pane that is already there
         rather than replacing a bare line of text with the whole layout. -->
    <div class="comms-body">
      <div class="comms-rail-column">
        <ul
          class="comms-rail"
          class:empty-state={railEmpty || loading}
          class:loading
          use:scrollFade={railContentKey}
        >
          {#if loading}
            <li class="comms-rail-loading">{$t('hub.checkingAccounts')}</li>
          {:else}
          {#each visibleRail as entry (entry.key)}
            <li>
              <button
                type="button"
                class:active={isSelected(entry.section, selected)}
                onclick={() => (selected = entry.section)}
              >
                <span class="comms-mark">
                  {#if entry.logo}
                    <img src={entry.logo} data-logo={entry.key} alt="" aria-hidden="true" />
                  {:else}
                    <span class="comms-mark-letter">{entry.name.charAt(0)}</span>
                  {/if}
                  <span class="comms-dot" data-state={entry.state}></span>
                </span>
                <span>
                  <strong>{entry.name}</strong>
                  <small class="state-text" data-state={entry.state}>{entry.label}</small>
                </span>
              </button>
            </li>
          {:else}
            <li class="comms-rail-empty">{$t('hub.railNoMatches')}</li>
          {/each}
          {/if}
        </ul>

      </div>

      <div class="comms-detail" use:scrollFade={selected}>
        {#if editingEmail}
          <header class="comms-detail-header">
            <h3>{emailOriginalId ? $t('hub.editMailbox', {name: emailOriginalId}) : $t('hub.addMailbox')}</h3>
            <p>{$t('hub.mailboxFormBlurb')}</p>
          </header>
          <!-- Signing in answers the address and the servers at once, so it
               stands above the form rather than beside a Provider field: for a
               Google or Microsoft mailbox there is nothing below worth
               filling in. Only offered where a client is registered. -->
          {#if !emailOriginalId && mailSignInProviders.length}
            <div class="comms-signin">
              {#each mailSignInProviders as provider (provider)}
                <button
                  type="button"
                  class="comms-signin-button"
                  disabled={busy === `email-signin:${provider}`}
                  onclick={() => void signInMailbox(provider)}
                >
                  <img src={mailLogo(provider === 'google' ? 'gmail' : 'outlook')} alt="" aria-hidden="true" />
                  {busy === `email-signin:${provider}`
                    ? $t('hub.signingIn')
                    : $t('hub.signInWith', {provider: mailProviderLabel(provider)})}
                </button>
              {/each}
              <p class="comms-signin-note">{$t('hub.signInOrManual')}</p>
            </div>
          {/if}
          <div class="comms-form">
            {#if mailPresetOptions.length > 1}
              <label>
              <span>{$t('hub.provider')}</span>
              <Menu
                value={emailForm.preset}
                label={$t('hub.provider')}
                wide
                options={mailPresetOptions}
                onChange={applyPreset}
              />
            </label>
            {/if}
            {#if presetHint}<p class="comms-hint">{presetHint}</p>{/if}
            <label>
              <span>{$t('hub.accountName')}</span>
              <input bind:value={emailForm.id} placeholder="work" spellcheck="false" />
            </label>
            <label>
              <span>{$t('hub.emailAddress')}</span>
              <input bind:value={emailForm.email} type="email" placeholder="you@example.com" spellcheck="false" />
            </label>
            <label>
              <span>{$t('hub.displayName')}</span>
              <input bind:value={emailForm.displayName} placeholder={$t('hub.displayNamePlaceholder')} />
            </label>
            <label>
              <span>{$t('hub.password')}</span>
              <input
                bind:value={emailPassword}
                type="password"
                placeholder={emailOriginalId ? $t('hub.passwordKeep') : $t('hub.appPassword')}
              />
            </label>
            <div class="comms-form-row">
              <label>
                <span>{$t('hub.imapServer')}</span>
                <input bind:value={emailForm.imapHost} spellcheck="false" />
              </label>
              <label class="comms-port">
                <span>{$t('hub.port')}</span>
                <input bind:value={emailForm.imapPort} type="number" />
              </label>
            </div>
            <div class="comms-form-row">
              <label>
                <span>{$t('hub.smtpServer')}</span>
                <input bind:value={emailForm.smtpHost} spellcheck="false" />
              </label>
              <label class="comms-port">
                <span>{$t('hub.port')}</span>
                <input bind:value={emailForm.smtpPort} type="number" />
              </label>
            </div>
            <footer class="comms-actions">
              <button type="button" onclick={() => (editingEmail = false)}>{$t('common.cancel')}</button>
              <button
                type="button"
                class="primary"
                disabled={busy === 'email-save' || !emailForm.id.trim() || !emailForm.email.trim()}
                onclick={() => void saveEmail()}
              >
                {busy === 'email-save' ? $t('hub.saving') : $t('hub.saveMailbox')}
              </button>
            </footer>
          </div>
        {:else if editingSignatures}
          <div class="comms-signature-page">
            <header class="comms-detail-header comms-signature-header">
              <h3>{$t('hub.signaturesTitle')}</h3>
              {#if selectedSignatureRow}
                <span class="comms-detail-actions">
                  <button type="button" aria-label={$t('hub.addSignature')} onclick={addSignature}>
                    <Icon name="plus" size={14} />
                  </button>
                </span>
              {/if}
            </header>
            <div class="comms-signature-toolbar">
              <Menu
                options={signatureAccountOptions}
                value={signatureAccountId}
                label={$t('hub.signatureAccounts')}
                icon="mail"
                wide
                keepOpenOnChange
                onChange={chooseSignatureAccount}
              />
              <small>{plural('hub.signatures', signatureScopeCount)}</small>
            </div>

            {#if selectedSignatureRow}
              <div class="comms-signature-workspace">
                <section class="comms-signature-list" aria-label={$t('hub.signatureChoices')} use:scrollFade={signatureAccountId}>
                  {#each signatureRows as row (row.key)}
                    <button type="button" class:active={row.key === signatureSelectedKey} onclick={() => (signatureSelectedKey = row.key)}>
                      <span class="comms-signature-row-mark"><Icon name="signature" size={15} /></span>
                      <span class="comms-signature-row-copy">
                        <strong>{row.signature.name}</strong>
                        <small>
                          {signatureAccountId === 'all' ? row.accountEmail : row.isDefault ? $t('hub.default') : row.accountEmail}
                        </small>
                      </span>
                      {#if row.isDefault && signatureAccountId === 'all'}
                        <Icon name="check" size={12} />
                      {/if}
                    </button>
                  {/each}
                </section>

                <section class="comms-signature-editor" aria-label={$t('hub.signatureEditor')}>
                  <label>
                    <span>{$t('hub.signatureName')}</span>
                    <input
                      value={selectedSignatureRow.signature.name}
                      maxlength="80"
                      oninput={(event) => updateSignature('name', event.currentTarget.value)}
                    />
                  </label>
                  <div class="comms-signature-body">
                    <span>{$t('hub.signaturePreview')}</span>
                    {#key selectedSignatureRow.key}
                      <RichSignatureEditor
                        text={selectedSignatureRow.signature.body}
                        html={selectedSignatureRow.signature.html}
                        label={$t('hub.signaturePreview')}
                        placeholder={$t('hub.signaturePlaceholder')}
                        onChange={updateSignatureContent}
                      />
                    {/key}
                  </div>
                  <footer class="comms-signature-editor-footer">
                    <button
                      type="button"
                      class="comms-signature-default"
                      class:checked={selectedSignatureRow.isDefault}
                      role="checkbox"
                      aria-checked={selectedSignatureRow.isDefault}
                      onclick={() => setDefaultSignature(!selectedSignatureRow?.isDefault)}
                    >
                      <span class="comms-signature-checkmark">
                        {#if selectedSignatureRow.isDefault}<Icon name="check" size={11} strokeWidth={2.2} />{/if}
                      </span>
                      <span>{$t('hub.useSignatureByDefault', {email: selectedSignatureRow.accountEmail})}</span>
                    </button>
                    <button type="button" class="destructive" onclick={removeSignature}>
                      <Icon name="trash" size={13} />
                      <span>{$t('hub.removeSignature')}</span>
                    </button>
                  </footer>
                </section>
              </div>
            {:else}
              <section class="comms-signature-empty" aria-label={$t('hub.signatureChoices')}>
                <span><Icon name="signature" size={22} /></span>
                <strong>{$t('hub.noSignatures')}</strong>
                <button type="button" onclick={addSignature}>
                  <Icon name="plus" size={13} />
                  {$t('hub.addSignature')}
                </button>
              </section>
            {/if}
            <footer class="comms-actions comms-signature-actions">
              <button type="button" onclick={() => (editingSignatures = false)}>{$t('common.cancel')}</button>
              <button
                type="button"
                class="primary"
                disabled={busy === 'signatures-save' || !signaturesSaveable}
                onclick={() => void saveSignatures()}
              >
                {busy === 'signatures-save' ? $t('hub.saving') : $t('hub.saveSignatures')}
              </button>
            </footer>
          </div>
        {:else if activeBridge}
          <header class="comms-detail-header">
            <h3>{activeBridge.name}</h3>
            <p>
              {#if activeBridge.state === 'connected'}
                {activeBridge.accounts.length > 1
                  ? $t('hub.linkedMany', {count: activeBridge.accounts.length})
                  : $t('hub.linkedOne', {name: activeBridge.accounts[0]?.name ?? ''})}
              {:else if activeBridge.state === 'unavailable'}
                {activeBridge.error}
              {:else if activeBridge.state === 'unreachable'}
                {$t('hub.unreachable')}
              {:else}
                {$t('hub.linkPrompt', {platform: activeBridge.name})}
              {/if}
            </p>
          </header>

          <!-- The bridge's error is just its first account's (see hub.ts), so
               showing it here as well as on the row would say it twice. The
               row is the more precise of the two: it names the account. -->
          {#if activeBridge.error && activeBridge.state !== 'unavailable' && !activeBridge.accounts.some((account) => account.error)}
            <p class="comms-hint warn">{activeBridge.error}</p>
          {/if}

          <!-- A blocker macOS can lift belongs behind a button rather than in
               directions the reader has to carry to another app. -->
          {#if activeBridge.permission}
            <section class="comms-block">
              <button
                type="button"
                disabled={busy === 'grant'}
                onclick={() => void grant(activeBridge.permission!)}
              >
                {busy === 'grant'
                  ? $t('hub.waiting')
                  : permissionPrompts(activeBridge.permission)
                    ? $t('hub.allowAccess')
                    : $t('hub.openSettings')}
              </button>
            </section>
          {/if}

          {#if activeBridge.installUrl}
            <section class="comms-block">
              <footer class="comms-actions">
                <button
                  type="button"
                  class="primary"
                  onclick={() => openInstall(activeBridge.installUrl!)}
                >
                  {$t('hub.downloadPlatform', {platform: activeBridge.name})}
                </button>
              </footer>
            </section>
          {/if}

          {#if settling === activeBridge.platform}
            <!-- Between the phone confirming and the bridge reporting the
                 account. One line, in place: the pane carries on rather than
                 snapping back to the log-in button it started from. -->
            <section class="comms-block comms-step">
              <p role="status">{$t('hub.signingIn')}</p>
            </section>
          {:else if step && stepPlatform === activeBridge.platform}
            <section class="comms-block comms-step">
              {#if step.type === 'display_and_wait'}
                {#if qr}
                  <div class="comms-qr">
                    <!-- The four-module quiet zone is part of the symbol, not
                         padding: scanners need it to find the finder patterns. -->
                    <svg
                      viewBox="-4 -4 {qr.size + 8} {qr.size + 8}"
                      role="img"
                      aria-label={$t('hub.pairingQr')}
                    >
                      <rect x="-4" y="-4" width={qr.size + 8} height={qr.size + 8} fill="#fff" />
                      <path d={qr.path} fill="#000" />
                    </svg>
                  </div>
                {:else if step.display === 'code' && step.data}
                  <p class="comms-code">{step.data}</p>
                {/if}
                <!-- Our own wording first when the QR is up: it names the menu
                     the scanner hides behind, which "scan this" leaves out. -->
                <p>
                  {(qr && qrInstructions(stepPlatform)) ??
                    step.instructions ??
                    $t('hub.confirmOnPhone')}
                </p>
                {#if waiting}<p class="comms-muted">{$t('hub.waiting')}</p>{/if}
              {:else if step.type === 'user_input'}
                {#if step.instructions}<p>{step.instructions}</p>{/if}
                <div class="comms-form">
                  {#each step.fields as field (field.id)}
                    <label>
                      <span>{field.name}</span>
                      <input
                        type={fieldType(field.type)}
                        value={stepValues[field.id] ?? ''}
                        oninput={(event) =>
                          (stepValues = {
                            ...stepValues,
                            [field.id]: (event.currentTarget as HTMLInputElement).value,
                          })}
                        spellcheck="false"
                      />
                      {#if field.description}<small>{field.description}</small>{/if}
                    </label>
                  {/each}
                </div>
              {:else if step.type === 'cookies'}
                <p>{step.instructions ?? $t('hub.finishInWindow')}</p>
              {/if}
              {#if stepError}<p class="comms-hint warn">{stepError}</p>{/if}
              <footer class="comms-actions">
                <!-- With more than one way in, backing out of a method is a
                     step towards the others rather than giving up on the
                     platform, and the label should say so. -->
                <button type="button" onclick={() => void cancelLink()}>
                  {activeBridge.flows.length > 1 && step.type !== 'cookies'
                    ? $t('hub.otherMethods')
                    : $t('common.cancel')}
                </button>
                {#if step.type === 'user_input'}
                  <button type="button" class="primary" disabled={busy === 'step'} onclick={() => void submitStep()}>
                    {busy === 'step' ? $t('hub.checking') : $t('common.continue')}
                  </button>
                {/if}
              </footer>
            </section>
          {:else}
            {#if activeBridge.accounts.length > 0}
              <section class="comms-block">
                <h4>{activeBridge.accounts.length === 1 ? $t('hub.linkedAccount') : $t('hub.linkedAccounts')}</h4>
                {#each activeBridge.accounts as account (account.id)}
                  <p class="comms-value">
                    <code>{account.name}{account.kind === 'bot' ? ` · ${$t('hub.botAccount')}` : ''}</code>
                    <!-- A relay account belongs to the app on this Mac, so
                         unlinking it stops Polymux carrying its messages
                         rather than signing anything out — but it is still the
                         way off the platform, and belongs on the row. -->
                    <button
                      type="button"
                      class="destructive"
                      disabled={busy === `unlink:${activeBridge.platform}:${account.id}`}
                      onclick={() => void unlink(activeBridge, account)}>{$t('hub.unlink')}</button>
                  </p>
                  {#if account.error}<p class="comms-hint warn">{account.error}</p>{/if}
                {/each}
              </section>
            {/if}
            {#if needsSetup && activeBridge.setup}
            <!-- Telegram and its like will not run on someone else's
                 application, so the pair is asked for here. Without this the
                 pane offered no login methods and no way to get any. -->
            <section class="comms-block">
              <h4>{$t('hub.setupTitle', {platform: activeBridge.name})}</h4>
              <p class="comms-hint">{$t('hub.setupBlurb', {platform: activeBridge.name})}</p>
              <div class="comms-form">
                {#each activeBridge.setup.fields as field (field.id)}
                  <label>
                    <span>{field.name}</span>
                    <input
                      type={field.secret ? 'password' : 'text'}
                      spellcheck="false"
                      autocomplete="off"
                      value={setupValues[field.id] ?? ''}
                      oninput={(event) =>
                        (setupValues = {
                          ...setupValues,
                          [field.id]: (event.currentTarget as HTMLInputElement).value,
                        })}
                    />
                    {#if field.description}<small>{field.description}</small>{/if}
                  </label>
                {/each}
              </div>
              {#if activeBridge.setup.fields[0]?.helpUrl}
                <p class="comms-hint">
                  {$t('hub.setupHelp', {url: activeBridge.setup.fields[0].helpUrl})}
                </p>
              {/if}
              <footer class="comms-actions">
                <button
                  type="button"
                  class="primary"
                  disabled={busy === 'setup' || !setupReady}
                  onclick={() => void saveSetup(activeBridge.platform)}
                >
                  {busy === 'setup' ? $t('hub.saving') : $t('common.save')}
                </button>
              </footer>
            </section>
            {:else if activeBridge.flows.length > 0 && activeBridge.state !== 'unavailable'}
            <section class="comms-block">
              <h4>{activeBridge.accounts.length > 0 ? $t('hub.addAnotherAccount') : $t('hub.howToLink')}</h4>
              <ul class="comms-flows">
                {#each activeBridge.flows as flow (flow.id)}
                  <li>
                    <span>
                      <strong>{flow.name}</strong>
                      <small>{flow.description}</small>
                    </span>
                    <button
                      type="button"
                      disabled={!signedIn || busy === `link:${activeBridge.platform}`}
                      onclick={() => void startLink(activeBridge.platform, flow.id)}
                    >
                      {flow.id === 'qr'
                        ? $t('hub.showQr')
                        : activeBridge.platform === 'telegram' && flow.id === 'bot'
                          ? $t('hub.connectBot')
                          : $t('hub.start')}
                    </button>
                  </li>
                {/each}
              </ul>
              {#if !signedIn}
                {#if status?.hub.canAutoConnect}
                  <p class="comms-hint">{$t('hub.setupFirst')}</p>
                  <footer class="comms-actions">
                    <button type="button" class="primary" disabled={busy === 'connect'} onclick={() => void connect()}>
                      {busy === 'connect' ? $t('hub.settingUp') : $t('hub.setUpMessaging')}
                    </button>
                  </footer>
                {:else}
                  <p class="comms-hint warn">
                    {status?.hub.error ?? $t('hub.notRunning')}
                  </p>
                {/if}
              {/if}
              {#if stepError}<p class="comms-hint warn">{stepError}</p>{/if}
            </section>
            {:else if activeBridge.state !== 'unavailable' &&
            activeBridge.state !== 'dormant' &&
            activeBridge.accounts.length === 0}
            <p class="comms-muted">
              {$t('hub.noLinkMethod')}
              {#if activeBridge.managementRoomHint}
                {$t('hub.useManagementRoom')}
              {/if}
            </p>
            {/if}
          {/if}
        {:else if mailOpen}
          <header class="comms-detail-header">
            <h3>{$t('hub.mail')}</h3>
            <p>{$t('hub.mailBlurb')}</p>
            <span class="comms-detail-actions">
              {#if emailAccounts.length > 0}
                <button type="button" aria-label={$t('hub.manageSignatures')} onclick={() => openSignatures()}>
                  <Icon name="signature" size={15} />
                </button>
              {/if}
              <button type="button" aria-label={$t('hub.addMailboxShort')} onclick={addEmail}>
                <Icon name="plus" size={14} />
              </button>
            </span>
          </header>

          <!-- Held back while the fleet is still on its way: "No mailboxes yet"
               is an answer, and it is the wrong one to flash before we have it. -->
          {#if loading}
            <p class="comms-muted">{$t('hub.checkingAccounts')}</p>
          {:else}
            <ul class="comms-mailboxes">
              {#each emailAccounts as account (account.id)}
                <li>
                  <div class="comms-mailbox-head">
                    <span class="comms-mailbox-name">
                      <strong>{account.email}</strong>
                    </span>
                    <span class="comms-status" data-state={account.status}>
                      {account.status === 'ok' ? $t('hub.reachable') : account.status === 'error' ? $t('drive.stateDisconnected') : $t('hub.connecting')}
                    </span>
                  </div>
                  <p class="comms-mailbox-servers">
                    <span>{account.incoming.host ?? $t('hub.noImapServer')}{account.incoming.port ? `:${account.incoming.port}` : ''}</span>
                    <span aria-hidden="true">·</span>
                    <span>{account.outgoing.host ?? $t('hub.noSmtpServer')}{account.outgoing.port ? `:${account.outgoing.port}` : ''}</span>
                    <span aria-hidden="true">·</span>
                    <span>{account.incoming.auth === 'oauth2' ? 'OAuth' : account.incoming.auth === 'command' ? $t('hub.keychain') : account.incoming.auth}</span>
                  </p>
                  {#if account.error}<p class="comms-hint warn">{account.error}</p>{/if}
                  <div class="comms-mailbox-actions">
                    <button type="button" onclick={() => openSignatures(account.id)}>{$t('hub.signaturesTitle')}</button>
                    <button type="button" onclick={() => editEmail(account)}>{$t('common.edit')}</button>
                    <button
                      type="button"
                      class="destructive"
                      disabled={busy === `email-remove:${account.id}`}
                      onclick={() => void removeEmail(account)}
                    >
                      {$t('hub.remove')}
                    </button>
                  </div>
                </li>
              {:else}
                <li class="comms-mailbox-empty">
                  <p>{$t('hub.noMailboxesYet')}</p>
                  <button type="button" onclick={addEmail}>{$t('hub.addMailbox')}</button>
                </li>
              {/each}
            </ul>
          {/if}
        {/if}
      </div>
    </div>
</div>

<style>
  .comms{flex:1;min-height:0;display:flex;flex-direction:column;overflow:hidden;padding:2px var(--options-detail-edge) 16px var(--options-content-edge)}
  .comms-muted{color:var(--neutral-400);font-size:11px}
  .comms-error{display:flex;align-items:center;gap:12px;margin-bottom:10px;padding:9px 11px;border-radius:9px;background:#fff5f5;color:#8f3e3e;font-size:11px}
  .comms-error>span{min-width:0;flex:1}
  .comms-error button{height:26px;flex:none;border:1px solid color-mix(in srgb,currentColor 20%,transparent);border-radius:7px;padding:0 9px;background:var(--app-surface);color:inherit;cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  :global(:root[data-theme="dark"]) .comms-error{background:#321f1f;color:#eea7a7}

  .comms-body{min-height:0;flex:1;display:grid;grid-template-columns:calc(186px + var(--options-tab-inline)) 1fr;gap:var(--options-divider-gap)}
  /* Flex gap rather than row margins: block margins collapse to half the
     intended spacing, and the MCP and Skills rails read as 4px of clear air
     between adjacent highlights. */
  .comms-rail-column{min-height:0;display:flex;flex-direction:column;gap:6px;padding-right:var(--options-divider-gap);border-right:1px solid var(--neutral-200)}
  /* Same fade as the MCP/Skills rails: the mask only opens at an edge the list
     is actually scrolled away from, so a short list keeps crisp ends. */
  .comms-rail{min-height:0;flex:1;display:flex;flex-direction:column;gap:4px;margin:0;padding:6px 0;overflow-y:auto;list-style:none}
  .comms-rail.empty-state{-webkit-mask-image:none;mask-image:none}
  .comms-rail>li{margin:0}
  .comms-rail-empty{padding:6px 8px;color:var(--neutral-400);font-size:10.5px}
  /* Centred in the rail's whole height rather than sat at the top: while the
     fleet is on its way the rail has no rows for it to head. */
  .comms-rail.loading{justify-content:center}
  .comms-rail-loading{padding:0 8px;color:var(--neutral-400);text-align:center;font-size:10.5px}
  .comms-rail button:not(.comms-add){width:100%;display:flex;align-items:center;gap:9px;border:0;border-radius:8px;padding:7px 8px;background:transparent;color:var(--neutral-700);cursor:pointer;font-family:inherit;text-align:left}
  .comms-rail button:not(.comms-add):hover{background:var(--neutral-100)}
  .comms-rail button.active{background:var(--neutral-100);color:var(--neutral-950)}
  /* Excludes the status dot, which is a sibling span with a fixed size. */
  .comms-rail button>span:not(.comms-dot){min-width:0;flex:1;display:flex;flex-direction:column;gap:1px}
  .comms-rail strong{overflow:hidden;color:var(--neutral-900);text-overflow:ellipsis;white-space:nowrap;font-size:11.5px;font-weight:540}
  .comms-rail small{overflow:hidden;color:var(--neutral-400);text-overflow:ellipsis;white-space:nowrap;font-size:9.5px}

  /* The mark says which platform, the dot says how it is doing. The dot rides
     the corner of the logo rather than sitting beside it, so the row keeps one
     leading element and the names still line up. */
  /* Outranks `.comms-rail button>span:not(.comms-dot)`, which would otherwise
     claim this span as the text column and stretch it. */
  .comms-rail button>span.comms-mark{position:relative;width:20px;height:20px;flex:none;display:grid;place-items:center}
  .comms-mark img{width:100%;height:100%;object-fit:contain}
  .comms-mark-letter{display:grid;place-items:center;width:100%;height:100%;border-radius:50%;background:var(--neutral-200);color:var(--neutral-600);font-size:10px;font-weight:650}
  .comms-mark .comms-dot{position:absolute;right:-2px;bottom:-2px;box-shadow:0 0 0 1.5px var(--app-surface)}
  .comms-rail button.active .comms-mark .comms-dot{box-shadow:0 0 0 1.5px var(--neutral-100)}
  .comms-dot{width:7px;height:7px;flex:none;border-radius:50%;background:var(--neutral-300)}
  .comms-dot[data-state="connected"]{background:#3f9c5a}
  .comms-dot[data-state="connecting"]{background:#c99a3a}
  .comms-dot[data-state="error"]{background:#c05a5a}
  .comms-dot[data-state="logged-out"]{background:var(--neutral-300)}
  .comms-dot[data-state="unreachable"],.comms-dot[data-state="unavailable"]{background:var(--neutral-200);box-shadow:inset 0 0 0 1px var(--neutral-300)}

  .comms-detail{min-height:0;overflow-y:auto;padding-right:2px}
  /* The signature page owns its two internal scrollers. Keeping the outer
     detail pane scrollable would clip and mask the account menu before it can
     float over the workspace below. */
  .comms-detail:has(>.comms-signature-page){overflow:visible;-webkit-mask-image:none;mask-image:none}
  /* The action sits out of the text's way rather than on top of it: the header
     keeps a lane clear on the right wide enough for the button and its gap. */
  .comms-detail-header{position:relative;margin-bottom:14px;padding-right:44px}
  .comms-detail-header h3{margin:0;color:var(--neutral-950);font-size:14px;font-weight:580}
  .comms-detail-header p{max-width:520px;margin:5px 0 0;color:var(--neutral-600);font-size:11px;line-height:1.5}
  .comms-detail-actions{position:absolute;top:-2px;right:0;display:flex;gap:4px}
  /* Borderless — the glyph is the affordance, and a box around it competes with
     the cards below. The hover fill is what confirms it is a target. */
  .comms-detail-actions button{width:28px;height:28px;display:grid;place-items:center;border:0;border-radius:8px;background:transparent;color:var(--neutral-500);cursor:pointer}
  .comms-detail-actions button:hover{background:var(--neutral-100);color:var(--neutral-950)}

  .comms-block{margin-bottom:16px;padding-top:12px;border-top:1px solid var(--neutral-200)}
  .comms-block h4{margin:0 0 7px;color:var(--neutral-900);font-size:11.5px;font-weight:570}
  .comms-block h4:not(:first-child){margin-top:12px}
  .comms-value{display:flex;align-items:center;gap:9px;margin:0 0 4px;font-size:11px}
  .comms-value code{overflow:hidden;padding:2px 6px;border-radius:5px;background:var(--neutral-100);color:var(--neutral-800);text-overflow:ellipsis;white-space:nowrap;font-size:10.5px}
  .comms-value button{height:26px;flex:none;border:1px solid var(--neutral-200);border-radius:7px;padding:0 10px;background:var(--app-surface);color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  .comms-value button:hover{background:var(--neutral-100);color:var(--neutral-950)}
  .comms-value button.destructive{color:var(--danger-500)}
  .comms-value button:disabled{cursor:default;opacity:.5}

  .comms-hint{max-width:520px;margin:5px 0 0;color:var(--neutral-500);font-size:10.5px;line-height:1.5}
  .comms-hint.warn{color:var(--danger-500)}

  /* Above the form and the same width as it, because for a Google or
     Microsoft mailbox there is nothing below worth filling in. */
  .comms-signin{display:flex;max-width:440px;flex-direction:column;gap:7px;margin:0 0 14px}
  .comms-signin-button{display:flex;align-items:center;justify-content:center;gap:8px;border:1px solid var(--neutral-200);border-radius:9px;padding:9px 12px;background:none;color:var(--neutral-900);font-family:inherit;font-size:12.5px;font-weight:560;cursor:pointer;transition:border-color .16s,opacity .16s}
  .comms-signin-button img{width:18px;height:18px;display:block;flex:none;object-fit:contain}
  .comms-signin-button:hover:not(:disabled){border-color:var(--neutral-400)}
  .comms-signin-button:disabled{opacity:.55;cursor:default}
  .comms-signin-button:focus-visible{outline:2px solid var(--neutral-950);outline-offset:2px}
  .comms-signin-note{margin:1px 0 0;color:var(--neutral-500);font-size:11.5px}
  .comms-form{display:flex;max-width:440px;flex-direction:column;gap:9px}
  .comms-form label{display:flex;flex-direction:column;gap:3px}
  .comms-form label>span{color:var(--neutral-600);font-size:10.5px;font-weight:530}
  .comms-form label>small{color:var(--neutral-400);font-size:9.5px}
  .comms-form input{height:30px;border:1px solid var(--neutral-200);border-radius:8px;padding:0 9px;background:var(--app-surface);color:var(--neutral-950);font-family:inherit;font-size:11.5px}
  .comms-form input:focus-visible{border-color:var(--neutral-500);outline:0}
  .comms-form-row{display:flex;max-width:440px;gap:9px}
  .comms-form-row>label{flex:1}
  .comms-form-row .comms-port{max-width:82px;flex:none}

  .comms-actions{display:flex;justify-content:flex-end;gap:7px;margin-top:12px}
  .comms-actions button{height:29px;border:1px solid var(--neutral-200);border-radius:8px;padding:0 12px;background:var(--app-surface);color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:11px;font-weight:550}
  .comms-actions button:hover{background:var(--neutral-100);color:var(--neutral-950)}
  .comms-actions button.primary{border-color:var(--neutral-950);background:var(--neutral-950);color:var(--app-bg)}
  .comms-actions button.primary:hover{opacity:.88}
  .comms-actions button:disabled{cursor:default;opacity:.5}

  .comms-flows{margin:0;padding:0;list-style:none}
  .comms-flows li{position:relative;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:10px 4px}
  .comms-flows li+li::before{position:absolute;top:0;right:4px;left:4px;height:1px;background:var(--neutral-200);content:""}
  .comms-flows span{min-width:0;display:flex;flex-direction:column;gap:2px}
  .comms-flows strong{color:var(--neutral-900);font-size:11.5px;font-weight:545}
  .comms-flows small{color:var(--neutral-500);font-size:10px}
  .comms-flows button{height:27px;flex:none;border:1px solid var(--neutral-200);border-radius:7px;padding:0 11px;background:var(--app-surface);color:var(--neutral-800);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  .comms-flows button:hover:not(:disabled){background:var(--neutral-100)}
  .comms-flows button:disabled{cursor:default;opacity:.45}

  .comms-step p{margin:0 0 8px;color:var(--neutral-700);font-size:11px;line-height:1.5}
  /* The symbol needs a light quiet zone regardless of theme, or scanners fail. */
  .comms-qr{width:200px;padding:10px;margin:0 0 12px;border-radius:10px;background:#fff}
  .comms-qr svg{width:100%;height:auto;display:block;shape-rendering:crispEdges}
  .comms-code{padding:9px 12px;border-radius:9px;background:var(--neutral-100);color:var(--neutral-950);font-size:19px;font-weight:600;letter-spacing:.16em;font-variant-numeric:tabular-nums}


  .comms-mailboxes{margin:0;padding:0;list-style:none}
  .comms-mailboxes>li{position:relative;padding:12px 4px}
  .comms-mailboxes>li+li::before{position:absolute;top:0;right:4px;left:4px;height:1px;background:var(--neutral-200);content:""}
  .comms-mailbox-head{display:flex;align-items:center;justify-content:space-between;gap:12px}
  .comms-mailbox-name{min-width:0;display:flex;align-items:center;gap:7px}
  .comms-mailbox-name strong{overflow:hidden;color:var(--neutral-950);text-overflow:ellipsis;white-space:nowrap;font-size:12px;font-weight:555}
  .comms-mailbox-servers{display:flex;flex-wrap:wrap;gap:6px;margin:5px 0 0;color:var(--neutral-500);font-size:10.5px}
  .comms-mailbox-actions{display:flex;gap:6px;margin-top:9px}
  .comms-mailbox-actions button{height:26px;border:1px solid var(--neutral-200);border-radius:7px;padding:0 9px;background:var(--app-surface);color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  .comms-mailbox-actions button:hover:not(:disabled){background:var(--neutral-100);color:var(--neutral-950)}
  .comms-mailbox-actions button.destructive{color:var(--danger-500)}
  .comms-mailbox-actions button:disabled{color:var(--disabled-text);cursor:default;opacity:1}
  .comms-mailbox-empty{display:flex;align-items:center;justify-content:space-between;gap:12px}
  .comms-mailbox-empty p{margin:0;color:var(--neutral-500);font-size:11px}
  .comms-mailbox-empty button{height:27px;flex:none;border:1px solid var(--neutral-200);border-radius:7px;padding:0 11px;background:var(--app-surface);color:var(--neutral-800);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  .comms-mailbox-empty button:hover{background:var(--neutral-100)}

  /* Signatures use the same compact filter, inset list highlights, form
     fields and bare actions as the rest of Settings. The nested mail-client
     rail from the reference is deliberately flattened: Hub already owns the
     page rail, so another full-height account rail only adds chrome. */
  .comms-signature-page{height:100%;min-height:0;display:flex;flex-direction:column}
  .comms-signature-header{flex:none;margin-bottom:10px}
  .comms-signature-toolbar{display:flex;align-items:center;gap:9px;margin-bottom:12px}
  .comms-signature-toolbar :global(.select-menu){width:160px;min-width:160px;max-width:160px;flex:0 0 160px}
  .comms-signature-toolbar :global(.select-menu-trigger){box-sizing:border-box;width:100%;min-width:0;max-width:100%;height:29px;justify-content:flex-start;gap:6px;padding:0 9px 0 8px;font-size:10.5px}
  .comms-signature-toolbar :global(.select-menu-trigger>span:not(.select-menu-icon)){min-width:0;flex:1;text-align:left}
  .comms-signature-toolbar :global(.select-menu-trigger>[data-icon="chevron"]){flex:none;margin-left:auto;transform:translateY(1px)}
  .comms-signature-toolbar :global(.select-menu-list){right:auto;left:0;width:100%;min-width:100%;max-width:100%}
  .comms-signature-toolbar>small{margin-left:auto;color:var(--neutral-400);font-size:10px;white-space:nowrap}

  .comms-signature-workspace{min-height:0;flex:1;display:grid;grid-template-columns:minmax(176px,.8fr) minmax(260px,1.7fr);gap:18px;animation:comms-signature-in .14s ease-out both}
  .comms-signature-list{position:relative;min-width:0;min-height:0;overflow-y:auto;padding:3px 18px 3px 0;scrollbar-width:none}
  .comms-signature-list::after{position:absolute;top:8px;right:0;bottom:8px;width:1px;background:var(--neutral-200);content:""}
  .comms-signature-list::-webkit-scrollbar,.comms-signature-editor::-webkit-scrollbar{display:none}
  .comms-signature-list>button{width:100%;min-width:0;display:flex;align-items:center;gap:8px;margin:2px 0;border:0;border-radius:8px;padding:7px 8px;background:transparent;color:var(--neutral-600);cursor:pointer;font-family:inherit;text-align:left}
  .comms-signature-list>button:hover,.comms-signature-list>button.active{background:var(--neutral-100);color:var(--neutral-950)}
  .comms-signature-row-mark{width:17px;height:17px;flex:none;display:grid;place-items:center;color:var(--neutral-500)}
  .comms-signature-row-copy{min-width:0;flex:1;display:flex;flex-direction:column;gap:1px}
  .comms-signature-row-copy strong,.comms-signature-row-copy small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .comms-signature-row-copy strong{color:var(--neutral-900);font-size:11.5px;font-weight:545}
  .comms-signature-row-copy small{color:var(--neutral-400);font-size:9.5px}

  .comms-signature-editor{min-width:0;min-height:0;display:flex;flex-direction:column;gap:10px;overflow-y:auto;padding:3px 2px 3px 0;scrollbar-width:none}
  .comms-signature-editor>label,.comms-signature-editor>.comms-signature-body{min-width:0;display:flex;flex-direction:column;gap:4px}
  .comms-signature-editor>label>span,.comms-signature-editor>.comms-signature-body>span{overflow:hidden;color:var(--neutral-500);text-overflow:ellipsis;white-space:nowrap;font-size:10px;font-weight:540}
  .comms-signature-editor input:not([type]){height:29px;border:1px solid var(--neutral-200);border-radius:8px;padding:0 9px;background:var(--app-surface);color:var(--neutral-950);font-family:inherit;font-size:11.5px}
  .comms-signature-body{min-height:0;flex:1}
  .comms-signature-editor input:focus-visible{border-color:var(--neutral-500);outline:0}
  .comms-signature-editor-footer{min-width:0;min-height:24px;display:flex;align-items:center;justify-content:space-between;gap:12px;padding:1px 2px 0}
  .comms-signature-default{min-width:0;min-height:24px;flex:1;display:flex;align-items:center;gap:7px;border:0;padding:0;background:transparent;color:var(--neutral-600);cursor:pointer;font-family:inherit;text-align:left;font-size:10.5px;font-weight:500;line-height:1.2}
  .comms-signature-default>span:last-child{min-width:0;white-space:normal}
  .comms-signature-checkmark{box-sizing:border-box;width:15px;height:15px;flex:none;display:grid;place-items:center;border:1px solid var(--neutral-300);border-radius:5px;background:var(--app-surface);color:transparent;transition:background .12s ease,border-color .12s ease,color .12s ease}
  .comms-signature-default:hover .comms-signature-checkmark{border-color:var(--neutral-500)}
  .comms-signature-default.checked .comms-signature-checkmark{border-color:var(--neutral-900);background:var(--neutral-900);color:var(--app-bg)}
  .comms-signature-editor-footer>button.destructive{min-height:24px;display:flex;align-items:center;gap:5px;flex:none;border:0;padding:0;background:transparent;color:var(--danger-500);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:530;line-height:1.2}
  .comms-signature-editor-footer>button.destructive:hover{color:var(--danger-600)}

  .comms-signature-empty{min-height:0;flex:1;display:flex;align-items:center;justify-content:center;flex-direction:column;gap:7px;color:var(--neutral-400);animation:comms-signature-in .14s ease-out both}
  .comms-signature-empty>span{width:28px;height:28px;display:grid;place-items:center;color:var(--neutral-400)}
  .comms-signature-empty strong{color:var(--neutral-600);font-size:11.5px;font-weight:550}
  .comms-signature-empty button{display:flex;align-items:center;gap:5px;margin-top:3px;border:0;padding:3px;background:transparent;color:var(--neutral-700);cursor:pointer;font-family:inherit;font-size:10.5px;font-weight:550}
  .comms-signature-empty button:hover{color:var(--neutral-950)}
  .comms-signature-actions{flex:none;margin-top:14px}
  @keyframes comms-signature-in{from{opacity:0}to{opacity:1}}

  .comms-status{display:inline-flex;align-items:center;gap:6px;font-size:11px;font-weight:540}
  .comms-status::before{width:6px;height:6px;flex:none;border-radius:50%;background:currentColor;content:""}
  .comms-status[data-state="ok"]{color:var(--status-success-text)}
  .comms-status[data-state="error"]{color:var(--status-error-text)}
  .comms-status[data-state="unknown"]{color:var(--neutral-500)}
</style>
