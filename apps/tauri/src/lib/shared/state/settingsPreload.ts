import type {
  AcpRegistryEntryDto,
  AgentRuntimeDto,
  AppVersionDto,
  BrowserDownloadDto,
  BrowserExtensionDto,
  BrowserSettingsDto,
  CommsStatusDto,
  ComputerHistoryEntryDto,
  ComputerHistoryStatusDto,
  DriveStatusDto,
  GeneralSettingsDto,
  McpServerDto,
  MemoryEntryDto,
  MemoryStatusDto,
  ModelDto,
  ModelMetadataDto,
  ModelRolesDto,
  PluginDto,
  PolymuxApi,
  ProfilesDto,
  ProviderDto,
  SavedLoginDto,
  SitePermissionDto,
  SkillDto,
  WorkspaceAppsDto,
} from '@polymux/protocol';

/**
 * Window-lived Settings data.
 *
 * Settings is a modal and its individual tabs are destroyed when they are not
 * visible. Keeping their read-only opening data here lets the app prepare it
 * after the main screen has painted, then lets each tab refresh that copy in
 * place instead of making its first open a loading screen.
 */
export const settingsSnapshot: {
  loaded: boolean;
  loadedAt: number;
  mcpServers: McpServerDto[];
  skills: SkillDto[];
  plugins: PluginDto[];
  workspaceApps: WorkspaceAppsDto;
  models: ModelDto[];
  providers: ProviderDto[];
  memory: MemoryStatusDto | null;
  computerHistory: ComputerHistoryStatusDto | null;
  general: GeneralSettingsDto | null;
  extensionStatus: BrowserExtensionDto | null;
  agentRuntime: AgentRuntimeDto | null;
  profiles: ProfilesDto | null;
  acpRegistry: AcpRegistryEntryDto[];
  modelMetadata: Record<string, ModelMetadataDto>;
  modelRoles: ModelRolesDto | null;
  appVersion: AppVersionDto | null;
  computerHistoryEntries: ComputerHistoryEntryDto[];
  memoryEntries: MemoryEntryDto[];
  historyLoaded: boolean;
  historyLoadedAt: number;
  detailsLoaded: boolean;
  detailsLoadedAt: number;
} = {
  loaded: false,
  loadedAt: 0,
  mcpServers: [],
  skills: [],
  plugins: [],
  workspaceApps: {apps: [], pinnedIds: []},
  models: [],
  providers: [],
  memory: null,
  computerHistory: null,
  general: null,
  extensionStatus: null,
  agentRuntime: null,
  profiles: null,
  acpRegistry: [],
  modelMetadata: {},
  modelRoles: null,
  appVersion: null,
  computerHistoryEntries: [],
  memoryEntries: [],
  historyLoaded: false,
  historyLoadedAt: 0,
  detailsLoaded: false,
  detailsLoadedAt: 0,
};

export const settingsHubSnapshot: {status: CommsStatusDto | null; loadedAt: number} = {
  status: null,
  loadedAt: 0,
};

export const settingsDriveSnapshot: {status: DriveStatusDto | null; loadedAt: number} = {
  status: null,
  loadedAt: 0,
};

export const settingsBrowserSnapshot: {
  settings: BrowserSettingsDto | null;
  logins: SavedLoginDto[];
  downloads: BrowserDownloadDto[];
  permissions: SitePermissionDto[];
  loadedAt: number;
} = {
  settings: null,
  logins: [],
  downloads: [],
  permissions: [],
  loadedAt: 0,
};

let settingsRequest: Promise<typeof settingsSnapshot> | null = null;
let historyRequest: Promise<typeof settingsSnapshot> | null = null;
let detailsRequest: Promise<typeof settingsSnapshot> | null = null;
let hubRequest: Promise<CommsStatusDto> | null = null;
let driveRequest: Promise<DriveStatusDto> | null = null;
let browserRequest: Promise<typeof settingsBrowserSnapshot> | null = null;

function isFresh(loadedAt: number, maxAgeMs: number): boolean {
  return loadedAt > 0 && Date.now() - loadedAt < maxAgeMs;
}

/** Reads every row-producing Settings source together. Overlapping callers
 * share one request, so opening Settings while its launch warm is still in
 * flight joins that work rather than duplicating it. */
export function loadSettingsSnapshot(
  api: PolymuxApi,
  maxAgeMs = Number.POSITIVE_INFINITY,
): Promise<typeof settingsSnapshot> {
  if (settingsRequest) return settingsRequest;
  if (settingsSnapshot.loaded && isFresh(settingsSnapshot.loadedAt, maxAgeMs))
    return Promise.resolve(settingsSnapshot);

  settingsRequest = (async () => {
    const [
      mcpServers,
      skills,
      plugins,
      workspaceApps,
      models,
      providers,
      memory,
      computerHistory,
      general,
      extensionStatus,
      agentRuntime,
      profiles,
      acpRegistry,
    ] = await Promise.all([
      api.mcp.list(),
      api.skills.list(),
      api.plugins.list(),
      api.apps.list(),
      api.models.list(),
      api.providers.list(),
      api.memory.status(),
      api.computerHistory.status(),
      api.general.get(),
      api.extension.status(),
      api.agentRuntime.get(),
      api.profiles.list(),
      api.agentRuntime.registry().catch(() => []),
    ]);
    Object.assign(settingsSnapshot, {
      loaded: true,
      loadedAt: Date.now(),
      mcpServers,
      skills,
      plugins,
      workspaceApps,
      models,
      providers,
      memory,
      computerHistory,
      general,
      extensionStatus,
      agentRuntime,
      profiles,
      acpRegistry,
    });
    return settingsSnapshot;
  })().finally(() => {
    settingsRequest = null;
  });
  return settingsRequest;
}

/** Detail-only decoration follows the rows: model catalogue prose, role
 * assignments, and build identity are useful on first reveal but must not hold
 * the navigation rails back. */
export function loadSettingsDetailSnapshot(
  api: PolymuxApi,
  maxAgeMs = Number.POSITIVE_INFINITY,
): Promise<typeof settingsSnapshot> {
  if (detailsRequest) return detailsRequest;
  if (settingsSnapshot.detailsLoaded && isFresh(settingsSnapshot.detailsLoadedAt, maxAgeMs))
    return Promise.resolve(settingsSnapshot);

  detailsRequest = (async () => {
    const [modelMetadata, modelRoles, appVersion] = await Promise.all([
      api.models.metadata(),
      api.models.roles(),
      api.general.version(),
    ]);
    Object.assign(settingsSnapshot, {
      modelMetadata,
      modelRoles,
      appVersion,
      detailsLoaded: true,
      detailsLoadedAt: Date.now(),
    });
    return settingsSnapshot;
  })().finally(() => {
    detailsRequest = null;
  });
  return detailsRequest;
}

/** Computer History can be much larger than the rails, so it warms beside the
 * core request without delaying the point at which Settings is paint-ready. */
export function loadSettingsHistorySnapshot(
  api: PolymuxApi,
  maxAgeMs = Number.POSITIVE_INFINITY,
): Promise<typeof settingsSnapshot> {
  if (historyRequest) return historyRequest;
  if (settingsSnapshot.historyLoaded && isFresh(settingsSnapshot.historyLoadedAt, maxAgeMs))
    return Promise.resolve(settingsSnapshot);

  historyRequest = (async () => {
    const [computerHistoryEntries, memoryEntries] = await Promise.all([
      api.computerHistory.entries({limit: 1000}),
      api.memory.entries(),
    ]);
    Object.assign(settingsSnapshot, {
      computerHistoryEntries,
      memoryEntries,
      historyLoaded: true,
      historyLoadedAt: Date.now(),
    });
    return settingsSnapshot;
  })().finally(() => {
    historyRequest = null;
  });
  return historyRequest;
}

export function loadSettingsHubSnapshot(
  api: PolymuxApi,
  maxAgeMs = Number.POSITIVE_INFINITY,
): Promise<CommsStatusDto> {
  if (hubRequest) return hubRequest;
  if (settingsHubSnapshot.status && isFresh(settingsHubSnapshot.loadedAt, maxAgeMs))
    return Promise.resolve(settingsHubSnapshot.status);
  hubRequest = api.comms.status().then((status) => {
    settingsHubSnapshot.status = status;
    settingsHubSnapshot.loadedAt = Date.now();
    return status;
  }).finally(() => {
    hubRequest = null;
  });
  return hubRequest;
}

export function loadSettingsDriveSnapshot(
  api: PolymuxApi,
  maxAgeMs = Number.POSITIVE_INFINITY,
): Promise<DriveStatusDto> {
  if (driveRequest) return driveRequest;
  if (settingsDriveSnapshot.status && isFresh(settingsDriveSnapshot.loadedAt, maxAgeMs))
    return Promise.resolve(settingsDriveSnapshot.status);
  driveRequest = api.drive.status().then((status) => {
    settingsDriveSnapshot.status = status;
    settingsDriveSnapshot.loadedAt = Date.now();
    return status;
  }).finally(() => {
    driveRequest = null;
  });
  return driveRequest;
}

export function loadSettingsBrowserSnapshot(
  api: PolymuxApi,
  maxAgeMs = Number.POSITIVE_INFINITY,
): Promise<typeof settingsBrowserSnapshot> {
  if (browserRequest) return browserRequest;
  if (settingsBrowserSnapshot.settings && isFresh(settingsBrowserSnapshot.loadedAt, maxAgeMs))
    return Promise.resolve(settingsBrowserSnapshot);
  browserRequest = (async () => {
    const [settings, logins, downloads, permissions] = await Promise.all([
      api.browser.settings(),
      api.browser.logins(),
      api.browser.downloads(),
      api.browser.permissions(),
    ]);
    Object.assign(settingsBrowserSnapshot, {
      settings,
      logins,
      downloads,
      permissions,
      loadedAt: Date.now(),
    });
    return settingsBrowserSnapshot;
  })().finally(() => {
    browserRequest = null;
  });
  return browserRequest;
}

/** Starts every safe, read-only Settings warm. A failed integration must not
 * stop the other tabs from becoming ready. */
export async function preloadSettings(api: PolymuxApi): Promise<void> {
  await Promise.allSettled([
    loadSettingsSnapshot(api),
    loadSettingsDetailSnapshot(api),
    loadSettingsHistorySnapshot(api),
    loadSettingsHubSnapshot(api),
    loadSettingsDriveSnapshot(api),
    loadSettingsBrowserSnapshot(api),
  ]);
}
