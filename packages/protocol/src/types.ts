export type JsonValue =
  null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };
export type ConversationId = string;
export type RunId = string;

export interface ConversationDto {
  id: ConversationId;
  title: string;
  createdAt: string;
  updatedAt: string;
  archivedAt: string | null;
}
export interface AttachmentDto {
  id: string;
  messageId: string;
  name: string;
  path: string;
  mimeType: string | null;
  size: number | null;
  sha256: string | null;
  createdAt: string;
}

/** Content the desktop host can place on the system clipboard. File and
 * attachment variants stay out of the renderer because browser clipboard APIs
 * can only copy their labels, not an OS-pasteable file. */
export type ClipboardContentDto =
  | {kind: "text"; text: string; title?: string}
  | {kind: "file"; path: string}
  | {
      kind: "attachment";
      url: string;
      name: string;
      mimeType: string | null;
      copyAs: "image" | "file";
    };

export interface MessageDto {
  id: string;
  conversationId: ConversationId;
  runId: RunId | null;
  role: "system" | "user" | "assistant" | "tool";
  content: JsonValue;
  createdAt: string;
  sequence: number;
  attachments: AttachmentDto[];
  metadata: JsonValue;
}
export interface GoalDto {
  id: string;
  conversationId: ConversationId;
  objective: string;
  status: "active" | "paused" | "completed" | "blocked";
  createdAt: string;
  updatedAt: string;
  completedAt: string | null;
}
export interface MemoryStatusDto {
  enabled: boolean;
  directory: string;
  storedBytes: number;
  registryPath: string;
  summaryPath: string;
  memories: number;
  userMemories: number;
  conversationMemories: number;
  latestMemoryAt: string | null;
  consolidatedAt: string | null;
  consolidationError: string | null;
  consolidationRetryAfter: string | null;
  pendingMemories: number;
}
export interface MemoryEntryDto {
  id: string;
  scope: "user" | "conversation";
  kind: string;
  content: string;
  createdAt: string;
  updatedAt: string;
}
export interface ComputerHistoryStatusDto {
  enabled: boolean;
  running: boolean;
  directory: string;
  lastCapturedAt: string | null;
  lastError: string | null;
  storedFrames: number;
  storedBytes: number;
  storedEvents: number;
  /** Apps and sites ComputerHistory leaves alone; everything else is recorded. */
  excludeApps: string[];
  excludeSites: string[];
  recordPrivateBrowsing: boolean;
  interactionEvents: boolean;
}
export interface ComputerHistorySettingsPatchDto {
  excludeApps?: string[];
  excludeSites?: string[];
  recordPrivateBrowsing?: boolean;
  interactionEvents?: boolean;
}
export interface ComputerHistoryEntryDto {
  id: string;
  capturedAt: string;
  sourceId: string;
  sourceName: string;
  displayId: string | null;
  width: number;
  height: number;
  path: string;
  change: number;
  reason: "change" | "heartbeat" | "initial";
  bytes: number;
  kind?: "image" | "text";
  app?: string;
  bundleId?: string;
  url?: string;
}
export interface ComputerHistoryActivityDto {
  id: string;
  startedAt: string;
  endedAt: string;
  title: string;
  summary: string;
  apps: string[];
  /** Raw accessibility captures retained as evidence behind this activity. */
  entryIds: string[];
  captures: number;
  events: number;
  summarized: boolean;
}
/** Built-in workspace surfaces that can be pinned to the title bar. */
export type PinnableWorkspaceView =
  | "drive"
  | "calendar"
  | "hub"
  | "tasks"
  | "phone"
  | "locker"
  | "media"
  | "terminal"
  | "ide"
  | "usage"
  | "finance";
export interface GeneralSettingsDto {
  theme: "light" | "dark" | "system";
  /** BCP 47 tag the interface is drawn in, or "system" to follow the host
   * locale. It steers the UI only — what language the agent replies in stays
   * the user's business, decided by what they write to it. */
  language: string;
  currency: "USD" | "AUD" | "EUR" | "GBP" | "SGD" | "JPY" | null;
  speechModeEnabled: boolean;
  /** Seconds of silence after which dictation stops listening on its own, or
   * null to keep listening until it is switched off by hand. */
  dictationAutoStopSeconds: number | null;
  timeEnabled: boolean;
  locationEnabled: boolean;
  /** Read Hub conversations without sending read receipts or clearing their
   * unread state. */
  hubIncognitoMode: boolean;
  reasoningLevel: ReasoningEffort;
  /** False until the first-run setup has been finished or dismissed. */
  onboardingCompleted: boolean;
  /**
   * Whether the app may *use* each OS permission, independently of whether
   * macOS has granted it. Switching one off stops the app reaching for the
   * capability without touching the grant, so turning it back on needs no
   * second trip through System Settings.
   */
  permissions: Record<SystemPermissionKind, boolean>;
  /**
   * Whether the app may post system notifications at all, and which events
   * earn one. The master switch is kept separate from the per-event map so
   * turning it off silences everything without forgetting which events the
   * user had chosen — flipping it back on restores that choice rather than
   * every event at once.
   */
  notificationsEnabled: boolean;
  notifications: Record<NotificationKind, boolean>;
  /** Workspace views pinned to the title bar, in display order. */
  pinnedViews: PinnableWorkspaceView[];
  location: {
    latitude: number;
    longitude: number;
    accuracy: number;
    updatedAt: string;
  } | null;
}
export interface GeneralSettingsUpdate {
  theme?: GeneralSettingsDto["theme"];
  language?: GeneralSettingsDto["language"];
  currency?: GeneralSettingsDto["currency"];
  speechModeEnabled?: boolean;
  dictationAutoStopSeconds?: GeneralSettingsDto["dictationAutoStopSeconds"];
  timeEnabled?: boolean;
  locationEnabled?: boolean;
  hubIncognitoMode?: boolean;
  reasoningLevel?: ReasoningEffort;
  onboardingCompleted?: boolean;
  permissions?: Partial<Record<SystemPermissionKind, boolean>>;
  notificationsEnabled?: boolean;
  notifications?: Partial<Record<NotificationKind, boolean>>;
  pinnedViews?: GeneralSettingsDto['pinnedViews'];
  location?: GeneralSettingsDto["location"];
}
export interface ExternalProfileSourceDto {
  kind: "external";
  agentId: string;
  agentName: string;
  /** Absolute directory read and written by the external agent. */
  directory: string;
}
export interface ProfileAgentDto {
  kind: "polymux" | "acp";
  /** Stable ACP registry/family id, or `polymux` for the built-in agent. */
  id: string;
  name: string;
}
export interface ProfileDto {
  id: string;
  name: string;
  isDefault: boolean;
  /** Present only while this profile follows another agent's live files. */
  source: ExternalProfileSourceDto | null;
  /** Agent implementation configured for this profile. Older Hosts may omit it. */
  agent?: ProfileAgentDto;
  /** Team only accepts runtimes whose execution boundary is known to be safe. */
  teamEligible?: boolean;
  teamBlockedReason?: string;
}
export interface ProfilesDto { activeId: string; profiles: ProfileDto[]; }

/**
 * The durable identity choices exposed by Bloub's customizer. Geometry and
 * labels live in the renderer, while these stable ids travel between Desktop
 * and Host. Expressions are deliberately presentation-only: the renderer
 * selects one from live UI context without storing it in agent state.
 */
export const TEAM_AVATAR_SHAPES = [
  "circle",
  "pebble",
  "squircle",
  "capsule",
  "triangle",
  "hexagon",
  "cube",
  "cloud",
  "droplet",
] as const;
export type TeamAvatarShape = typeof TEAM_AVATAR_SHAPES[number];

/**
 * Metadata marker for the host-authored first-run cue that opens a newly
 * created Team bot's conversation. The stored row is a real user turn so the
 * model acts on it, but it is Polymux's wording rather than the user's, so
 * every surface that lists messages hides it. Desktop and Host share this one
 * definition rather than repeating the key.
 */
export const TEAM_BOT_SETUP_KEY = "setupCue";

export function isTeamBotSetupCue(metadata: JsonValue | null | undefined): boolean {
  return Boolean(
    metadata &&
    typeof metadata === "object" &&
    !Array.isArray(metadata) &&
    (metadata as Record<string, JsonValue>)[TEAM_BOT_SETUP_KEY] === true,
  );
}

export const TEAM_AVATAR_EXPRESSIONS = [
  "neutral",
  "attentive",
  "surprised",
  "excited",
  "happy",
  "laughing",
  "angry",
  "sad",
  "frightened",
  "suspicious",
  "confused",
  "curious",
  "proud",
  "shy",
  "unimpressed",
  "sleepy",
] as const;
export type TeamAvatarExpression = typeof TEAM_AVATAR_EXPRESSIONS[number];
export interface TeamAvatarDto {
  shape: TeamAvatarShape;
  /** Six-digit CSS colour, including the leading #. */
  color: string;
  /** Optional paired colours for avatars that adapt to the applied theme. */
  colorPair?: {
    light: string;
    dark: string;
  };
}

export type BotStatus =
  | "idle"
  | "working"
  | "waiting-for-device"
  | "computer-offline"
  | "error";
export type TeamComputerProvider = "podman" | "docker" | "remote" | "unavailable";
export interface TeamComputerDto {
  provider: TeamComputerProvider;
  state: "stopped" | "starting" | "running" | "unavailable" | "error";
  detail: string | null;
  persistent: true;
  /** Team computers start without network access; a later capability may open it. */
  network: "none" | "restricted";
}

/** One row in Team is one persistent agent and one private conversation. */
export interface BotDto {
  id: string;
  conversationId: ConversationId;
  name: string;
  role: string;
  profileId: string;
  profileName: string;
  /** Computer that owns this bot's conversation, runtime, and workspace. */
  hostId: string;
  hostName: string;
  /** Device kind of that computer, for the glyph shown beside its name. */
  deviceType?: import('./device-pairing.js').DeviceType;
  avatar: TeamAvatarDto;
  /** Peer tools may contact other members without asking the user each time. */
  /** Device-side tools remain behind a short-lived lease even when this is on. */
  laptopAccess: "off" | "ask";
  status: BotStatus;
  preview: string;
  updatedAt: string;
  unread: boolean;
  /** Number of unseen Team messages. Older Hosts may expose only `unread`. */
  unreadCount?: number;
  computer: TeamComputerDto;
  /** Skills enabled for this bot from the connections pool. */
  skills?: string[];
  /** MCP server ids connected to this bot from the connections pool. */
  mcpServers?: string[];
  /** Plugin ids enabled for this bot from the connections pool. */
  plugins?: string[];
}

/** A Desktop-owned conversation that fans a message out to named Team agents. */
export interface TeamGroupDto {
  id: string;
  conversationId: ConversationId;
  name: string;
  memberIds: string[];
  preview: string;
  updatedAt: string;
  unread: boolean;
  unreadCount: number;
}

export interface CreateTeamGroupRequest {
  name: string;
  memberIds: string[];
}

export interface UpdateTeamGroupRequest {
  name?: string;
  memberIds?: string[];
}

export interface SendTeamGroupMessageRequest {
  id: string;
  text: string;
}

export interface CreateBotRequest {
  name: string;
  role: string;
  profileId: string;
  avatar: TeamAvatarDto;
  /** Defaults to the Desktop's preferred Host. */
  hostId?: string;
  laptopAccess?: "off" | "ask";
  skills?: string[];
  mcpServers?: string[];
  plugins?: string[];
}
export interface UpdateBotRequest {
  name?: string;
  role?: string;
  profileId?: string;
  avatar?: TeamAvatarDto;
  /** Moving this value transfers the bot and conversation to that Host. */
  hostId?: string;
  laptopAccess?: "off" | "ask";
  skills?: string[];
  mcpServers?: string[];
  plugins?: string[];
}

/** Durable provenance for a message sent by another Polymux agent. */
export interface AgentMessageOriginDto {
  kind: "team" | "assistant";
  memberId: string | null;
  conversationId: ConversationId;
  name: string;
  role: string | null;
  avatar: TeamAvatarDto | null;
  traceId: string;
  hop: number;
  automatic: boolean;
}
export interface SendAgentMessageRequest {
  to: string;
  text: string;
  fromConversationId?: ConversationId;
  fromMemberId?: string;
  attachments?: string[];
  automatic?: boolean;
}

export interface LaptopCapabilityLeaseDto {
  id: string;
  memberId: string;
  capabilities: Array<"browser" | "computer" | "files">;
  createdAt: string;
  expiresAt: string;
}

/**
 * Polymux Host is deliberately personal: exactly one Desktop may be paired to
 * one Host. There are no organisations, shared admin panels, or second users.
 */
export interface TeamHostDto {
  deviceType?: import('./device-pairing.js').DeviceType;
  mode: "local" | "remote";
  state: "local" | "connecting" | "connected" | "disconnected" | "error";
  endpoint: string | null;
  hostId: string;
  desktopId: string;
  deviceName: string;
  fingerprint: string;
  pairedAt: string | null;
  detail: string | null;
  /** New bots use this Host unless another one is selected. */
  isDefault?: boolean;
  /** Present on the computer currently acting as Host. */
  listeningEndpoint?: string | null;
  /** Short-lived and shown only before this Host is paired. */
  pairingCode?: string | null;
  /** Exact expiry of the current pairing window. */
  pairingExpiresAt?: string | null;
  /** The sole Desktop identity paired to this Host. */
  pairedDesktopName?: string | null;
}
export interface PairTeamHostRequest {
  endpoint: string;
  code: string;
}

export type ExternalConfigurationSection =
  | "settings"
  | "skills"
  | "plugins"
  | "mcp"
  | "memory";

export interface ExternalConfigurationSummaryDto {
  kind: ExternalConfigurationSection;
  count: number;
  /** Human-readable names only. Secrets and file contents never cross IPC. */
  items: string[];
  /** False when Polymux can report an agent-native asset but cannot merge it safely. */
  importable: boolean;
  detail: string | null;
}

/** One user configuration directory an installed ACP agent can run against. */
export interface ExternalAgentProfileDto {
  id: string;
  agentId: string;
  agentName: string;
  name: string;
  directory: string;
  exists: boolean;
  supportsImport: boolean;
  supportsSync: boolean;
  importUnavailableReason: string | null;
  syncUnavailableReason: string | null;
  summaries: ExternalConfigurationSummaryDto[];
}

export type ExternalProfileConnectionMode = "clean" | "merge" | "import" | "sync";

export interface ConnectExternalProfileRequest {
  runtime: Extract<UpdateAgentRuntimeRequest, {kind: "acp"}>;
  mode: ExternalProfileConnectionMode;
  /** Existing configuration to copy from or keep following. */
  sourceDirectory?: string;
  sections?: ExternalConfigurationSection[];
  /** New-profile name for import/sync, or an edited existing profile name. */
  profileName?: string;
  /** Updating an existing external profile keeps its identity. */
  profileId?: string;
}

/** The agent implementation attached to the active configuration profile. */
export type AgentRuntimeDto =
  | {kind: "polymux"; name: "Polymux Agent"}
  | {
      kind: "acp";
      name: string;
      command: string;
      args: string[];
      cwd: string | null;
      /** Session options remembered for new ACP sessions. */
      config: Record<string, string | boolean>;
      /** Stable family used to choose its isolated configuration directory. */
      agentId: string;
      /** Profile-local slot holding imported or clean external configuration. */
      configId: string;
      /** Sanitized, non-secret launch defaults remembered from the registry. */
      registryEnvironment?: Record<string, string>;
    };

export type UpdateAgentRuntimeRequest =
  | {kind: "polymux"}
  | {
      kind: "acp";
      name: string;
      command: string;
      args?: string[];
      cwd?: string | null;
      config?: Record<string, string | boolean>;
      agentId?: string;
      configId?: string;
      /** Non-secret launch defaults supplied by the official ACP registry. */
      registryEnvironment?: Record<string, string>;
    };

export interface AgentConfigValueDto {
  value: string;
  name: string;
  description: string | null;
}

export interface AgentConfigValueGroupDto {
  id: string;
  name: string;
  options: AgentConfigValueDto[];
}

/** A control advertised by the active agent through ACP session config. */
export type AgentConfigOptionDto =
  | {
      id: string;
      name: string;
      description: string | null;
      category: string | null;
      type: "select";
      currentValue: string;
      options: AgentConfigValueDto[];
      groups: AgentConfigValueGroupDto[];
    }
  | {
      id: string;
      name: string;
      description: string | null;
      category: string | null;
      type: "boolean";
      currentValue: boolean;
    };

/** Non-secret routing state advertised by an ACP agent. */
export interface AgentProviderDto {
  id: string;
  supported: string[];
  required: boolean;
  apiType: string | null;
  baseUrl: string | null;
}

/** A login route advertised by the selected ACP agent during initialization. */
export interface AgentAuthMethodDto {
  id: string;
  name: string;
  description: string | null;
  type: "agent" | "terminal";
  /** Terminal methods need an interactive terminal, which Polymux does not yet expose. */
  available: boolean;
}

export interface AgentSettingsDto {
  authMethods: AgentAuthMethodDto[];
  authRequired: boolean;
  supportsLogout: boolean;
  configOptions: AgentConfigOptionDto[];
  providers: AgentProviderDto[];
  /** Provider routing is a draft ACP capability and is absent on most agents. */
  supportsProviders: boolean;
}

export interface SetAgentProviderRequest {
  id: string;
  apiType: string;
  baseUrl: string;
  headers?: Record<string, string>;
}
/**
 * An event worth interrupting the user for. Each one is a row in Settings and
 * a key in the map above, so adding a kind here is all it takes for the row
 * and its stored switch to follow.
 */
export type NotificationKind =
  | "schedule-completed"
  | "schedule-failed"
  | "agent-completed"
  | "agent-attention"
  | "message-received";

export const NOTIFICATION_KINDS: NotificationKind[] = [
  "schedule-completed",
  "schedule-failed",
  "agent-completed",
  "agent-attention",
  "message-received",
];

/** Where a system notification lands after the user clicks it. */
export type NotificationTargetDto =
  | {kind: "conversation"; conversationId: string}
  | {kind: "workspace"; request: WorkspaceRevealDto}
  | {kind: "browser"; tabId: string};

/** How much the model is asked to reason before answering. Mirrors the
 * inference package's effort levels so the renderer can persist the choice
 * without importing the inference package directly. */
export type ReasoningEffort =
  | "off"
  | "minimal"
  | "low"
  | "medium"
  | "high"
  | "xhigh"
  | "max";
/**
 * The grants the app reaches for itself, for capabilities it ships with. Every
 * one of them is Electron's to ask for, and each is a row in Settings whether
 * or not anything is currently using it.
 */
export type BuiltInPermissionKind =
  | "microphone"
  | "screen-recording"
  | "accessibility"
  // The only one macOS will not prompt for: it is switched on by hand in
  // System Settings, so it is never "not-determined" — either this process can
  // read what the grant covers or it cannot.
  | "full-disk-access";

/**
 * The grants a *skill* needs, because it drives one of the user's own apps.
 * Electron can neither read nor prompt for these, so they go through the
 * native helper; and unlike the built-in ones they are asked for when a skill
 * that declares one is installed, rather than at first run.
 */
export type AppPermissionKind =
  | "reminders"
  | "calendars"
  | "contacts"
  | "photos"
  // Driving another application, which macOS records per (Polymux, target app)
  // pair rather than as one switch. It is presented as one row all the same:
  // the pane it opens is the one place any of those pairs can be changed.
  | "automation";

export type SystemPermissionKind = BuiltInPermissionKind | AppPermissionKind;

export const BUILT_IN_PERMISSION_KINDS: BuiltInPermissionKind[] = [
  "microphone",
  "screen-recording",
  "accessibility",
  "full-disk-access",
];

export const APP_PERMISSION_KINDS: AppPermissionKind[] = [
  "reminders",
  "calendars",
  "contacts",
  "photos",
  "automation",
];

export function isAppPermissionKind(
  value: unknown,
): value is AppPermissionKind {
  return APP_PERMISSION_KINDS.includes(value as AppPermissionKind);
}
export type SystemPermissionStatus =
  | "not-determined"
  | "granted"
  | "denied"
  | "restricted"
  | "unknown";
export interface FirstRunPermissionDto {
  firstRun: boolean;
  microphone: SystemPermissionStatus;
  screenRecording: SystemPermissionStatus;
}
export interface McpServerDto {
  id: string;
  name: string;
  description?: string;
  /** "official" marks a server bundled with the app, like official skills. */
  source: "official" | "polymux" | "codex";
  editable: boolean;
  enabled: boolean;
  transport: "stdio" | "streamable-http";
  status: "disconnected" | "connecting" | "connected" | "error";
  error?: string;
  toolNames: string[];
  resourceUris: string[];
  promptNames: string[];
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  cwd?: string;
  url?: string;
  headers?: Record<string, string>;
}
export interface SaveCustomMcpRequest {
  id: string;
  name: string;
  description?: string;
  transport: "stdio" | "streamable-http";
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  cwd?: string;
  url?: string;
  headers?: Record<string, string>;
}
export interface DiscoveredMcpDto {
  id: string;
  name: string;
  description?: string;
  transport: "stdio" | "streamable-http";
  /** The command or url the server runs on, shown as its one-line detail. */
  target: string;
  /** The configuration it was found in, e.g. "pi". */
  source: string;
  /** The scanned file, with the home directory shortened to "~". */
  path: string;
  /**
   * "loaded" — Polymux already runs a server with this id.
   * "available" — it can be copied into ~/.polymux/mcp.json.
   */
  state: "loaded" | "available";
}
/** Servers found in an external MCP configuration file. */
export interface DiscoveredMcpGroupDto {
  id: string;
  /** The configuration group the file belongs to, e.g. "Pi". */
  label: string;
  path: string;
  servers: DiscoveredMcpDto[];
}
export interface McpChangeDto {
  servers: McpServerDto[];
  error: string | null;
}
export interface McpRegistryEntryDto {
  id: string;
  name: string;
  description: string;
  url: string;
  repository?: string;
  requiredHeaders: string[];
}
export interface AcpRegistryEntryDto {
  id: string;
  name: string;
  description: string;
  version: string;
  icon: string;
  /** Whether this registry launch target is already available locally. */
  installed: boolean;
  /** Empty only when the registry has no distribution for this platform. */
  command: string;
  args: string[];
  /** Non-secret launch defaults declared by the official registry distribution. */
  environment?: Record<string, string>;
}
/** One page of registry results. `nextCursor` is empty once the registry has
 * nothing further; entries can be empty while it is not, because remote-less
 * servers are dropped after the page arrives. */
export interface McpRegistryPageDto {
  entries: McpRegistryEntryDto[];
  nextCursor: string;
}
export interface SkillDto {
  name: string;
  description: string;
  source: "official" | "codex" | "polymux" | "agents" | "bundled" | "configured";
  filePath: string;
  disableModelInvocation: boolean;
  allowedTools: string[];
  /**
   * The app grants this skill declared in its own frontmatter, e.g.
   * `permissions: reminders`. Read from the SKILL.md rather than a list kept
   * here, so a skill installed from anywhere can say what it needs — and so
   * installing it can ask for that grant instead of leaving the agent to hit
   * the refusal mid-run.
   */
  permissions: AppPermissionKind[];
  enabled: boolean;
  editable: boolean;
  instructions?: string;
  displayName?: string;
  author?: string;
  category?: string;
  /** ISO timestamp of the SKILL.md's last modification. */
  updatedAt?: string;
}
export interface SaveCustomSkillRequest {
  originalName?: string;
  name: string;
  description: string;
  instructions: string;
}
export interface SkillUploadFile {
  path: string;
  relativePath: string;
}
/** One entry of the skills.sh directory (GitHub-backed skill registry). */
export interface SkillRegistryEntryDto {
  /** Installable package spec, e.g. "vercel-labs/skills/find-skills". */
  id: string;
  name: string;
  /** Repository the skill ships from, e.g. "vercel-labs/skills". */
  source: string;
  installs: number;
}
/** One skill found sitting in another agent's directory on this machine. */
export interface DiscoveredSkillDto {
  name: string;
  description: string;
  /** The skill's own folder, shown so the user can see where it came from. */
  path: string;
  /**
   * "loaded" — Polymux already reads this skill, either because the directory
   * is one it sources or because a skill of that name is installed already.
   * "available" — it can be copied into ~/.polymux/skills.
   */
  state: "loaded" | "available";
}
/** Skills found under one agent's home, e.g. every skill in ~/.codex/skills. */
export interface DiscoveredSkillGroupDto {
  id: string;
  /** The agent the directory belongs to, e.g. "Codex". */
  label: string;
  /** The scanned directory, with the home directory shortened to "~". */
  directory: string;
  skills: DiscoveredSkillDto[];
}
/**
 * What a plugin brings with it. A plugin is installed and removed whole, so
 * these are reported rather than listed as separate rows: the Skills and MCP
 * tabs stay a view of what the user added directly, and everything a plugin
 * contributed is read on its own card.
 */
export interface PluginContributionsDto {
  /** Skill names, loaded by the agent but deliberately absent from Skills. */
  skills: string[];
  /** MCP server ids, connected but deliberately absent from the MCP tab. */
  mcpServers: string[];
  /** Workspace tab views bundled under `views/<id>/view.json`. */
  views: string[];
  /** Counted rather than named: Polymux has no surface for these yet. */
  commands: number;
  agents: number;
  hooks: number;
}
/**
 * A name a plugin contributes that the user already has standalone. The
 * plugin's copy is what runs, so the clash is surfaced on the plugin's card
 * rather than silently resolved.
 */
export interface PluginConflictDto {
  kind: "skill" | "mcp";
  name: string;
  /** Where the standalone copy came from, e.g. "polymux" or "official". */
  existingSource: string;
}
export interface PluginDto {
  /** "<marketplace>/<plugin>", unique across every marketplace added. */
  id: string;
  name: string;
  description: string;
  version?: string;
  author?: string;
  homepage?: string;
  /** The marketplace it was installed from, by id. */
  marketplace: string;
  marketplaceName: string;
  /** The installed folder, with the home directory shortened to "~". */
  directory: string;
  enabled: boolean;
  contributions: PluginContributionsDto;
  conflicts: PluginConflictDto[];
  /** Set when the plugin is installed but could not be read. */
  error?: string;
}
/** One entry of a marketplace's catalog, installed or not. */
export interface MarketplacePluginDto {
  id: string;
  name: string;
  description: string;
  version?: string;
  author?: string;
  homepage?: string;
  /** True when a plugin of this id is already installed. */
  installed: boolean;
}
export interface PluginMarketplaceDto {
  id: string;
  name: string;
  /** The `owner/repo` (or URL) the catalog was added from. */
  source: string;
  /** How many plugins its catalog lists, or 0 when it could not be read. */
  pluginCount: number;
  /** True for the marketplace Polymux ships with, which cannot be removed. */
  builtin: boolean;
  error?: string;
}
/** A workspace tab supplied by an enabled plugin. */
export interface PluginViewDto {
  /** `<plugin id>/<view id>`, unique across installed plugins. */
  id: string;
  pluginId: string;
  name: string;
  description: string;
  /** The local HTML entry point. The renderer exchanges this for a preview URL. */
  entry: string;
}

/**
 * A workspace surface installed for the active profile. Browser deliberately
 * does not appear here: it is the one core workspace surface and cannot be
 * disabled. Official apps ship with Polymux; marketplace Apps are installed
 * and managed independently from plugins.
 */
export interface WorkspaceAppDto {
  id: string;
  name: string;
  description: string;
  official: boolean;
  enabled: boolean;
  /** Built-in component to open, `view` for a marketplace HTML app, or null
   * when the app only contributes a Settings surface. */
  workspaceKind: PinnableWorkspaceView | "view" | null;
  settingsKind: "hub" | "drive" | null;
  /** Local entry point for a marketplace App. Never present for official apps. */
  entry: string | null;
  pinnable: boolean;
}

/** One entry in the dedicated App marketplace, installed or not. */
export interface MarketplaceAppDto {
  id: string;
  name: string;
  description: string;
  version?: string;
  author?: string;
  official: boolean;
  installed: boolean;
}

export interface WorkspaceAppsDto {
  apps: WorkspaceAppDto[];
  /** New Tab pins in display order. The backend caps this to four. */
  pinnedIds: string[];
}

/** Who the Usage app is about: signed-in account, else this machine. */
export interface UsageIdentityDto {
  name: string;
  handle: string | null;
  avatarUrl: string | null;
  badge: string | null;
}

export interface UsageDayDto {
  date: string;
  tokens: number;
  costUsd: number;
  runs: number;
}

export interface UsageNamedCountDto {
  name: string;
  count: number;
}

export interface UsageModelSpendDto {
  model: string;
  tokens: number;
  costUsd: number;
  runs: number;
}

export interface UsageAgentDto {
  id: string;
  name: string;
  kind: "polymux" | "acp";
  tokens: number;
  costUsd: number;
  runs: number;
  chats: number;
}

export type UsageScope = "all" | "assistant" | "team";
export interface UsageFilterDto {
  scope?: UsageScope;
  agentId?: string | null;
}

/**
 * Lifetime activity for the Usage app. Token spend is API-equivalent USD from
 * each run's stored model rates — the same mapping CodeBurn uses on local logs.
 * Totals are this installation's SQLite run log, including parent and child
 * runs. Scope separates Assistant and Team; agentId selects the runtime used
 * for those runs, independently of team member or current profile settings.
 */
export interface UsageStatsDto {
  identity: UsageIdentityDto;
  lifetimeTokens: number;
  peakTokens: number;
  costUsd: number;
  longestChatMs: number;
  currentStreakDays: number;
  longestStreakDays: number;
  days: UsageDayDto[];
  fastModePercent: number | null;
  reasoningPercent: number | null;
  skillsExplored: number;
  skillsUsed: number;
  totalChats: number;
  plugins: UsageNamedCountDto[];
  connections: UsageNamedCountDto[];
  models: UsageModelSpendDto[];
  agents: UsageAgentDto[];
  agentId: string | null;
  scope: UsageScope;
  spendIncomplete: boolean;
}

/** A newly allocated PTY. Attach to start painting it. */
export interface TerminalCreateDto {
  id: string;
}

/** Bytes the Terminal view missed while it was unmounted, plus a sequence so
 * events that race the attach reply are not painted twice. */
export interface TerminalAttachDto {
  id: string;
  seq: number;
  /** PTY output since the shell started, or the retained tail, as base64. */
  replay: string;
}

/** One name in the IDE project tree. `path` is relative to the chosen root. */
export interface IdeEntryDto {
  name: string;
  path: string;
  kind: "folder" | "file";
}

/** A file opened in the IDE. `content` is omitted when the bytes are not text. */
export interface IdeFileDto {
  name: string;
  path: string;
  language: string;
  binary: boolean;
  content: string | null;
}

export type TerminalEventDto =
  | {type: "data"; id: string; seq: number; data: string}
  /** The PTY process ended, including a shell `exit`. The session is gone;
   * further `close` calls are a no-op. */
  | {type: "exit"; id: string; seq: number; code: number | null};

export type LockerSyncState = "offline" | "local" | "syncing" | "synced" | "pending" | "error";

/** Where this device keeps the locker. Account is the default. */
export type LockerStorageMode = "local" | "account";

/** Which copy to keep when linking a local locker to an existing account vault. */
export type LockerStorageResolve = "keep-local" | "keep-cloud";

/** Cloud sync of the encrypted vault blob. Never includes secrets. */
export interface LockerSyncDto {
  signedIn: boolean;
  available: boolean;
  state: LockerSyncState;
  storage: LockerStorageMode;
  revision: number;
  lastSyncedAt: string | null;
  conflict?: "cloud-exists";
  error?: string;
}

export interface LockerStatusDto {
  exists: boolean;
  unlocked: boolean;
  itemCount: number;
  idleLockSeconds: number;
  sync: LockerSyncDto;
}

/** Username, password, and current TOTP for one fill. Secrets only after a user click. */
export interface LockerFillFieldsDto {
  username: string;
  password: string;
  totp: string | null;
}

export interface LockerGroupDto {
  id: string;
  name: string;
  parentId: string | null;
}

export interface LockerItemDto {
  id: string;
  title: string;
  username: string;
  url: string;
  notes: string;
  groupId: string;
  groupName: string;
  hasPassword: boolean;
  hasTotp: boolean;
  hasRecoveryCodes: boolean;
  hasPasskey: boolean;
  pinned?: boolean;
  sortIndex?: number;
  updatedAt: string | null;
}

export interface LockerListDto {
  groups: LockerGroupDto[];
  items: LockerItemDto[];
  trash: LockerItemDto[];
}

export interface LockerTotpDto {
  code: string;
  next: string;
  period: number;
  remaining: number;
  issuer: string;
  account: string;
}

export interface LockerCodesDto {
  id: string;
  code: string;
  next: string;
  period: number;
  remaining: number;
}

export interface LockerPasskeyDto {
  relyingParty: string;
  username: string;
  credentialId: string;
  userHandle: string;
}

export interface LockerSecretsDto {
  password: string;
  totp: LockerTotpDto | null;
  recoveryCodes: string[];
  passkey: LockerPasskeyDto | null;
}

export interface LockerPasskeyInputDto {
  relyingParty: string;
  username: string;
  credentialId: string;
  userHandle?: string;
  privateKeyPem?: string;
}

export interface LockerItemInputDto {
  id?: string;
  title: string;
  username?: string;
  url?: string;
  notes?: string;
  groupName?: string;
  password?: string;
  totpSecret?: string;
  recoveryCodes?: string[];
  passkey?: LockerPasskeyInputDto | null;
}

export interface LockerImportResultDto {
  imported: number;
  skipped: number;
  problems: string[];
}

export type LockerImportStartDto =
  | { status: "cancelled" }
  | ({ status: "imported" } & LockerImportResultDto)
  | { status: "needs-password"; name: string };

export type LockerCopyField = "password" | "username" | "url" | "totp" | "notes" | "recovery";

/** Encrypted kdbx plus revision metadata. Never includes plaintext secrets. */
export interface LockerVaultMetaDto {
  revision: number;
  updatedAt: string;
  checksum: string;
  dirty: boolean;
  lastSyncedAt: string | null;
  storage: LockerStorageMode;
}

export interface LockerVaultBlobDto {
  bytes: string;
  meta: LockerVaultMetaDto;
}

export interface PhoneDeviceDto {
  platform: "ios" | "android";
  id: string;
  udid: string;
  name: string;
  model: string;
  osVersion: string;
  transport: "wired" | "wireless";
  pairingState: "paired" | "unpaired";
  developerMode: boolean;
  tunnelAddress: string | null;
}

export interface PhoneStatusDto {
  supported: boolean;
  stage: "unsupported" | "disconnected" | "needs-signing" | "ready" | "connected" | "error";
  device: PhoneDeviceDto | null;
  signing: {
    available: boolean;
    source: "existing-profile" | "none";
    expiresAt: string | null;
    teamId: string | null;
    message: string | null;
  };
  wda: {
    available: boolean;
    installed: boolean;
    running: boolean;
    bundleId: string | null;
  };
  controller: {
    kind: "wda" | "adb" | "iphone-mirroring" | "coredevice";
    available: boolean;
    installed: boolean;
    running: boolean;
  };
  message: string | null;
}

export interface PhoneIosSigningStatusDto {
  supported: boolean;
  stage: "unavailable" | "signed-out" | "verification-required" | "authenticated";
  email: string | null;
  teamId: string | null;
  verificationMethod: "trusted-device" | "sms" | null;
  message: string | null;
}

export interface PhoneFrameDto {
  deviceId: string;
  dataUrl: string;
  width: number;
  height: number;
  capturedAt: string;
}

export interface PhonePointDto {
  x: number;
  y: number;
}

export interface ModelDto {
  provider: string;
  id: string;
  name: string;
  contextWindow: number;
  maxOutputTokens: number;
  reasoning: boolean;
  input: Array<"text" | "image">;
  cost: {
    input: number | null;
    output: number | null;
    cacheRead: number | null;
    cacheWrite: number | null;
  };
  selected: boolean;
  custom: boolean;
}
/**
 * The jobs a model can be assigned to. `main` is the model the agent answers
 * with; `subagent`, `judge` and `compaction` are overrides that fall back to `main`
 * when unset. `speech`, `image` and `video` are recorded preferences for the
 * generation surfaces; a speech assignment also switches speech mode on.
 */
export type ModelRole =
  | "main"
  | "subagent"
  | "judge"
  | "compaction"
  | "speech"
  | "image"
  | "video";
export interface ModelRoleAssignmentDto {
  provider: string;
  id: string;
  /** Display name of the assigned model, or its id when it is unknown. */
  name: string;
  /** How hard the role's model is asked to think. Absent when the model takes
   * no effort level, in which case the provider's own default applies. */
  reasoning?: ReasoningEffort;
}
/**
 * What each role currently points at. `null` means nothing is assigned: for
 * `subagent` and `judge` that is "follow the main model", and for the generation
 * roles it is "not set". `main` is never null once a model has been chosen.
 */
export type ModelRolesDto = Record<ModelRole, ModelRoleAssignmentDto | null>;
/**
 * Facts about a model that the inference layer does not carry, sourced from
 * the models.dev catalogue. Every field is optional: the catalogue does not
 * know every model, and a missing entry must degrade to showing nothing
 * rather than blocking the model list.
 */
export interface ModelMetadataDto {
  description?: string;
  family?: string;
  /** The lab that built the model, as distinct from the provider serving it. */
  lab?: string;
  knowledgeCutoff?: string;
  releaseDate?: string;
  lastUpdated?: string;
  openWeights?: boolean;
  toolCall?: boolean;
  structuredOutput?: boolean;
  temperature?: boolean;
  attachment?: boolean;
  contextLimit?: number;
  outputLimit?: number;
}
export interface ProviderDto {
  id: string;
  name: string;
  logoDataUrl?: string;
  baseUrl?: string;
  apiKeyLabel: string | null;
  supportsOAuth: boolean;
  storedCredential: boolean;
  configured: boolean;
  source: string | null;
  modelCount: number;
  custom: boolean;
  /** A model server on this machine that Polymux knows how to set up. Offered
   * in the provider list before it exists, so it is found where every other
   * provider is found rather than behind a custom-endpoint form. */
  localRuntime?: boolean;
  apiKeys: Array<{
    id: string;
    label: string;
    active: boolean;
    status: "ready" | "rate_limited" | "invalid";
  }>;
}
export type ProviderOAuthEventDto =
  | {
      providerId: string;
      type: "device_code";
      userCode: string;
      verificationUri: string;
      expiresInSeconds?: number;
    }
  | {
      providerId: string;
      type: "progress";
      message: string;
    };
export interface CreateCustomProviderRequest {
  name: string;
  baseUrl: string;
  logoDataUrl?: string;
  apiKey?: string;
  models: Array<{id: string; name?: string}>;
}
export interface UpdateCustomProviderRequest extends Omit<CreateCustomProviderRequest, "apiKey"> {
  id: string;
}
/** Turn a known local runtime into a configured provider by reading the models
 * off it. `baseUrl` overrides the runtime's default port. */
export interface SetupLocalRuntimeRequest {
  id: string;
  baseUrl?: string;
}
/** Ask a local or remote OpenAI-compatible endpoint what it can serve. */
export interface DiscoverModelsRequest {
  baseUrl: string;
  apiKey?: string;
}

export type RunEventDto = {
  runId: RunId;
  conversationId: ConversationId;
  /** Set when this run is a subagent's: the run that delegated to it. The
   * renderer routes those events to the task's own transcript rather than into
   * the conversation the parent is writing. */
  parentRunId?: RunId | null;
  sequence: number;
  timestamp: number;
  type: string;
  payload: JsonValue;
};

/** The one live surface an activity row can disclose beneath its label. */
export type ActivityPreviewRequestDto =
  | {kind: "browser"; tabId: string}
  | {kind: "computer"; runId: string};

export interface ArtifactDto {
  id: string;
  conversationId: ConversationId | null;
  runId: RunId | null;
  kind: "document" | "slides" | "sheet" | "photo" | "video" | "other";
  name: string;
  path: string;
  mimeType: string | null;
  size: number | null;
  createdAt: string;
  updatedAt: string;
  metadata: JsonValue;
}

export interface ReferenceDto {
  id: string;
  conversationId: ConversationId;
  runId: RunId | null;
  kind: "web" | "file" | "other";
  title: string;
  uri: string;
  createdAt: string;
  metadata: JsonValue;
}

/** State of the Polymux browser extension, which backs `browser_tabs`. */
export interface BrowserExtensionDto {
  /** True while a recent tab snapshot proves the extension is reporting. */
  installed: boolean;
  /** ISO timestamp of the last snapshot, or null if it has never reported. */
  lastReportedAt: string | null;
  /** Chrome Web Store package version, independent from the desktop version. */
  version: string | null;
  /** Highest mutually supported browser-surface protocol. */
  protocolVersion: number | null;
  /** Null before first contact; false prevents unsafe command exchange. */
  compatible: boolean | null;
  /** Negotiated extension features available to this desktop build. */
  capabilities: string[];
  /**
   * True when the title-bar chip should be shown: not installed, and not
   * dismissed since the last time it was seen installed.
   */
  promptToInstall: boolean;
}

export interface StartRunRequest {
  /** Execution device for a new Assistant conversation. */
  deviceId?: string;
  conversationId: ConversationId;
  text: string;
  messageId?: string;
  attachments?: string[];
  asGoal?: boolean;
  reasoning?: ReasoningEffort;
  /**
   * Set while the user is driving the conversation by speech rather than
   * typing. Transcribed speech arrives as ordinary text, so without this the
   * agent cannot tell the two apart and cannot shape replies for listening.
   */
  speechMode?: boolean;
  /**
   * The user prompt is already stored (an edited message, or a durable job
   * recovered after exit). The run must not append a second copy of it.
   */
  reuseUserMessage?: boolean;
  /**
   * Drop every stored turn after `messageId` and resend from there. Requires
   * `messageId`. Implies `reuseUserMessage`.
   */
  rewind?: boolean;
}
export interface StartRunResponse {
  runId: RunId;
}
export type ManagerJobPriorityDto = "background" | "normal" | "urgent" | "attention";
export type ManagerJobStatusDto = "queued" | "running" | "completed" | "cancelled" | "failed" | "blocked";
export interface ManagerJobDto {
  id: string;
  messageId: string;
  chatId: ConversationId;
  text: string;
  attachments: string[];
  asGoal: boolean;
  priority: ManagerJobPriorityDto;
  dependencyIds: string[];
  /** Inclusive durable-history boundary frozen when this job was accepted. */
  contextThroughSequence: number | null;
  /** Stable namespace for run-local routing and retained task state. */
  executionScopeId: string;
  /** User transcript row which owns this job's assistant result. */
  replyToMessageId: string;
  status: ManagerJobStatusDto;
  runId: RunId | null;
  createdAt: string;
  updatedAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  error: string | null;
}
export interface EnqueueManagerJobRequest {
  id?: string;
  chatId: ConversationId;
  text: string;
  attachments?: string[];
  asGoal?: boolean;
  priority?: ManagerJobPriorityDto;
  dependencyIds?: string[];
}
export interface ManagerSnapshotDto {
  enabled: boolean;
  jobs: ManagerJobDto[];
}
export interface GoalCommandRequest {
  conversationId: ConversationId;
  action: "view" | "create" | "update" | "pause" | "resume" | "clear";
  objective?: string;
}


/** Live page state for an embedded workspace-browser tab. */
export interface BrowserPageStateDto {
  tabId: string;
  url: string;
  title: string;
  /** The page's icon as a `data:` url, already fetched by the main process —
   * the renderer's CSP allows no remote images. Null until one arrives, and
   * again from the moment the tab navigates. */
  faviconUrl: string | null;
  canGoBack: boolean;
  canGoForward: boolean;
  loading: boolean;
}
export interface BrowserDownloadDto {
  id: string;
  title: string;
  path: string;
  kind: "document" | "image" | "pdf" | "spreadsheet" | "file";
  completedAt: string;
  url: string;
  state: "progressing" | "paused" | "completed" | "cancelled" | "interrupted";
  receivedBytes: number;
  /** Zero when the server sent no length, which is why progress is reported as
   * bytes rather than a fraction the UI would have to guard. */
  totalBytes: number;
}

export interface BrowserSettingsDto {
  /** Where downloads land when the user is not asked each time. */
  downloadDirectory: string;
  askWhereToSave: boolean;
  autofillEnabled: boolean;
}

export type PermissionDecisionDto = "allow" | "deny" | "ask";

/** The permissions the embedded browser will negotiate on a site's behalf.
 * Deliberately shorter than Electron's own list: anything absent stays denied
 * outright rather than gaining a prompt nobody designed. */
export type BrowserPermissionDto =
  | "geolocation"
  | "media"
  | "notifications"
  | "clipboard-read"
  | "pointerLock"
  | "fullscreen"
  | "openExternal";

export interface SitePermissionDto {
  origin: string;
  permission: BrowserPermissionDto;
  decision: PermissionDecisionDto;
  updatedAt: string;
}

/** A site the browser is holding data for. Cookie counts come from the cookie
 * jar itself; Electron exposes no per-origin storage figure, so none is
 * promised here. */
export interface BrowserSiteDto {
  origin: string;
  cookies: number;
  permissions: number;
  logins: number;
}

/** A saved login as the renderer is allowed to see it. The password is not a
 * field: it is returned only by an explicit, one-at-a-time reveal. */
export interface SavedLoginDto {
  id: string;
  origin: string;
  username: string;
  source: "manual" | "import";
  updatedAt: string;
  lastUsedAt: string | null;
}

export interface BrowserProfileDto {
  id: string;
  name: string;
  path: string;
  /** False when the profile is present but unreadable — Safari without Full
   * Disk Access, or a Firefox profile behind a Primary Password. `reason` says
   * which, so the UI can tell the user what to do instead of failing blankly. */
  readable: boolean;
  reason: string | null;
}

export interface BrowserSourceDto {
  id: string;
  name: string;
  family: "chromium" | "firefox" | "safari";
  profiles: BrowserProfileDto[];
  /** Set when nothing can be read from this browser directly and the only way
   * in is a file the user exports themselves. */
  fileImportOnly: boolean;
}

export interface BrowserImportRequestDto {
  sourceId: string;
  profileId: string;
  cookies: boolean;
  passwords: boolean;
  history: boolean;
}

export interface BrowserImportResultDto {
  cookiesImported: number;
  cookiesSkipped: number;
  passwordsImported: number;
  passwordsSkipped: number;
  historyImported: number;
  historySkipped: number;
  /** Human-readable reasons things were skipped, deduplicated. Empty on a
   * clean run. */
  problems: string[];
}

/** One page in the browsing history. `visitCount` is what survives collapsing
 * repeat visits onto a single url. */
export interface BrowserHistoryEntryDto {
  url: string;
  title: string;
  visitedAt: string;
  visitCount: number;
  source: "local" | "import";
}

/** A live permission request from a page, waiting on the user. */
export interface BrowserPermissionPromptDto {
  id: string;
  tabId: string;
  origin: string;
  permission: BrowserPermissionDto;
}

/** One discoverable passkey account Electron is waiting for the user to
 * choose. Credential ids are opaque selection tokens; user handles stay in
 * the main process because the UI has no reason to receive them. */
export interface BrowserWebAuthnAccountDto {
  credentialId: string;
  displayName?: string;
  name?: string;
}

/** A WebAuthn request with more than one discoverable account. Chromium keeps
 * the underlying request pending until `browser.respondToWebAuthn` answers. */
export interface BrowserWebAuthnPromptDto {
  id: string;
  tabId: string;
  relyingPartyId: string;
  accounts: BrowserWebAuthnAccountDto[];
}
export type BrowserAutofillFocus = "login" | "otp";

export interface BrowserAutofillItemDto {
  id: string;
  source: "locker" | "browser";
  title: string;
  username: string;
  hasPassword: boolean;
  hasTotp: boolean;
}

/** What Browser chrome may show for the current page. No secrets. */
export interface BrowserAutofillOfferDto {
  tabId: string;
  origin: string;
  /** Whether a Locker vault exists on this device. */
  locker: "missing" | "locked" | "unlocked";
  focus: BrowserAutofillFocus | null;
  items: BrowserAutofillItemDto[];
}

export interface BrowserFoundDto {
  tabId: string;
  matches: number;
  activeMatch: number;
}
export type BrowserEventDto =
  /** The agent opened a tab of its own; the renderer surfaces it so the user
   * can watch the page it is working in. `show` is set when the user asked to
   * be shown the page, which is the only case that brings the workspace
   * forward on its own. */
  | { type: "opened"; tab: { tabId: string; url: string; title: string }; show: boolean }
  /** A tab went away in the main process; the renderer drops its workspace tab
   * rather than leaving one pointed at nothing. */
  | { type: "closed"; tabId: string }
  | { type: "state"; state: BrowserPageStateDto }
  | { type: "downloads"; downloads: BrowserDownloadDto[] }
  | { type: "found"; found: BrowserFoundDto }
  /** The page took keyboard focus. Clicks inside the embedded web contents
   * never reach the renderer, so this is how the chrome around it knows to
   * drop its own focus (the address bar's caret, above all). */
  | { type: "focus"; tabId: string }
  /** A page asked for a capability and nothing is stored for it yet. The
   * renderer prompts; `browser.respondToPermission` settles it. */
  | { type: "permission"; prompt: BrowserPermissionPromptDto }
  /** A passkey sign-in needs the person at the keyboard to choose the account.
   * This is separate from a permission: choosing is the authentication act,
   * not a remembered allow/deny decision. */
  | { type: "webauthn"; prompt: BrowserWebAuthnPromptDto }
  /** A login was saved, imported or removed. Sent so an open Settings tab
   * reflects a password captured in a browser tab without being reopened. */
  | { type: "logins" }
  /** A sign-in or OTP form is on the page. Metadata only — secrets stay in
   * the main process until the user picks an item in the browser chrome. */
  | { type: "autofill"; tabId: string; offer: BrowserAutofillOfferDto | null };

export interface AppVersionDto {
  version: string;
  electron: string;
  platform: string;
  packaged: boolean;
}
export interface AppUpdateDto {
  /** `downloading` means an update exists and Squirrel is fetching it;
   * `ready` means it is staged and only a restart is left. `unsupported`
   * means this build has no update channel to check against. */
  status:
    | "current"
    | "downloading"
    | "ready"
    | "unsupported"
    | "error";
  version: string;
  latest: string | null;
  checkedAt: string;
  message: string | null;
}

/**
 * Platforms the Communications tab can link. Every messaging platform except
 * `matrix` itself reaches the network through a bridge; `matrix` is the hub's
 * own account, which the others are attached to.
 */
export type CommsPlatform =
  | "whatsapp"
  | "telegram"
  | "signal"
  | "slack"
  | "messenger"
  | "instagram"
  | "linkedin"
  | "googlechat"
  | "gmessages"
  | "twitter"
  | "bluesky"
  | "gvoice"
  | "zulip"
  | "imessage"
  | "wechat"
  | "matrix";

/**
 * The local Matrix homeserver plus bridge fleet that carries every messaging
 * platform. Bridges are only reachable through this hub, so its status gates
 * everything else in the tab.
 */
export interface CommsHubDto {
  /** Local proxy that fronts the homeserver and the bridge provisioning routes. */
  baseUrl: string;
  /** The homeserver itself, which the proxy does not expose the admin API of. */
  homeserverUrl: string;
  /**
   * Whether Polymux can create its own account on the hub, which is what lets
   * messaging be set up without the user entering anything.
   */
  canAutoConnect: boolean;
  /** Deployment root, when one was found on disk. */
  directory: string | null;
  status: "unconfigured" | "unreachable" | "reachable" | "signed-in";
  /** Matrix ID the app holds an access token for. */
  userId: string | null;
  homeserverName: string | null;
  error: string | null;
}

/**
 * A value a bridge needs before it can run at all, as distinct from a login.
 * Telegram is the case this exists for: its bridge will not connect without an
 * api_id/api_hash pair, and the pair belongs to whoever registered it, so each
 * user supplies their own rather than inheriting one baked into the app.
 */
export interface CommsSetupFieldDto {
  id: string;
  name: string;
  description: string | null;
  /** Where the user gets the value. */
  helpUrl: string | null;
  secret: boolean;
}

export interface CommsBridgeSetupDto {
  fields: CommsSetupFieldDto[];
  /** Whether every required field already has a value on this machine. */
  configured: boolean;
}

/** One linked remote account on a bridge. Most networks allow exactly one. */
export interface CommsBridgeAccountDto {
  id: string;
  /** Remote-side label: a phone number, handle, or display name. */
  name: string;
  /** Remote profile picture, when the bridge exposes one. */
  avatarUrl?: string | null;
  state: "connected" | "connecting" | "bad-credentials" | "error" | "unknown";
  error: string | null;
  /** Remote account kind when the connector reports it. Telegram bot-token
   * logins use this so the UI does not present a bot as a personal account. */
  kind?: "user" | "bot";
}

/** A way to link an account, as advertised by the bridge itself. */
export interface CommsLoginFlowDto {
  id: string;
  name: string;
  description: string;
}

export interface CommsBridgeDto {
  platform: CommsPlatform;
  name: string;
  /**
   * Which provisioning dialect the bridge speaks. Every bridge speaks the
   * step-based login API; `none` is a platform with no bridge to link.
   */
  api: "bridgev2" | "none";
  state:
    | "unknown"
    | "unavailable"
    | "unreachable"
    /**
     * Installed and able to run, but not running: nothing is linked to it, so
     * it is not started until its platform is opened. Distinct from
     * `unreachable`, which is a bridge that was asked for and did not answer.
     */
    | "dormant"
    | "logged-out"
    | "connecting"
    | "connected"
    | "error";
  accounts: CommsBridgeAccountDto[];
  flows: CommsLoginFlowDto[];
  /** Configuration the bridge needs before login, or null when it needs none. */
  setup: CommsBridgeSetupDto | null;
  /** Management room to fall back to when the flow cannot be driven here. */
  managementRoomHint: string | null;
  error: string | null;
  /** Official installer page when the platform's required desktop app is absent. */
  installUrl?: string | null;
  /** A confirmed condition requiring the user, independent of cached reads. */
  attention?: {title: string; detail: string; installUrl?: string; retry?: boolean} | null;
  /**
   * A macOS grant this bridge is held back by, when `error` describes one. It
   * is the difference between telling someone where the switch is and putting
   * it in front of them, so anything that renders the error should offer this
   * as a button. Absent whenever no grant would change the answer.
   */
  permission?: SystemPermissionKind | null;
}

/** One prompt in a `user_input` login step. */
export interface CommsLoginFieldDto {
  id: string;
  /** Maps to the input type the field should be rendered with. */
  type: "username" | "phone_number" | "email" | "password" | "2fa_code" | "token" | "url" | "unknown";
  name: string;
  description: string | null;
  /** Regex the value must satisfy before it is worth submitting. */
  pattern: string | null;
}

/** A cookie or storage value a `cookies` login step needs collected. */
export interface CommsLoginCookieFieldDto {
  /** Where the value lives in the logged-in page. */
  source: "cookie" | "local_storage" | "request_header" | "request_body" | "special";
  id: string;
  required: boolean;
}

/**
 * One step of a bridge login, mirroring the bridge's own step machine. The UI
 * renders a step, collects what it asks for, and posts back for the next one.
 */
export type CommsLoginStepDto =
  | {
      type: "user_input";
      loginId: string;
      stepId: string;
      instructions: string | null;
      fields: CommsLoginFieldDto[];
    }
  | {
      type: "cookies";
      loginId: string;
      stepId: string;
      instructions: string | null;
      /** Page to sign in on. */
      url: string;
      /** Navigating here means the sign-in finished. */
      waitForUrl: string | null;
      userAgent: string | null;
      fields: CommsLoginCookieFieldDto[];
    }
  | {
      type: "display_and_wait";
      loginId: string;
      stepId: string;
      instructions: string | null;
      display: "qr" | "code" | "emoji" | "nothing";
      /** QR payload to render, or the literal code to read out. */
      data: string | null;
      imageUrl: string | null;
    }
  | {
      type: "complete";
      loginId: string;
      accountId: string | null;
      accountName: string | null;
    };

export interface CommsEmailEndpointDto {
  kind: "imap" | "maildir" | "notmuch" | "smtp" | "sendmail" | "none";
  host: string | null;
  port: number | null;
  encryption: "tls" | "start-tls" | "none" | null;
  login: string | null;
  auth: "password" | "oauth2" | "command" | "keyring" | "none";
}

/** A reusable ending for mail sent from one account. */
export interface MailSignatureDto {
  /** Stable within its account, so an open composer can keep its selection. */
  id: string;
  name: string;
  /** Plain-text alternative, used by text-only recipients and search. */
  body: string;
  /** Sanitised email-safe formatting; null for a plain-text signature. */
  html: string | null;
}

export interface CommsEmailAccountDto {
  /** Account key, unique across the user's mailboxes. */
  id: string;
  displayName: string | null;
  email: string;
  incoming: CommsEmailEndpointDto;
  outgoing: CommsEmailEndpointDto;
  /** Whether Polymux holds the password for this account in encrypted storage. */
  secretStored: boolean;
  signatures: MailSignatureDto[];
  /** Null means new mail from this account starts without a signature. */
  defaultSignatureId: string | null;
  status: "unknown" | "ok" | "error";
  error: string | null;
}

/** Known provider whose server settings the UI can fill in for the user. */
/** A mail provider Polymux can sign in to on the user's behalf. */
export type CommsMailProvider = "google" | "microsoft";

export type CommsEmailPreset = "gmail" | "outlook" | "icloud" | "lark" | "fastmail" | "custom";

export interface SaveEmailAccountRequest {
  /** Existing account key when editing, absent when adding. */
  originalId?: string;
  id: string;
  displayName?: string;
  email: string;
  preset: CommsEmailPreset;
  imapHost: string;
  imapPort: number;
  imapEncryption: "tls" | "start-tls" | "none";
  imapLogin?: string;
  smtpHost: string;
  smtpPort: number;
  smtpEncryption: "tls" | "start-tls" | "none";
  smtpLogin?: string;
  /**
   * Plaintext only in transit from the settings form; it is written to
   * OS-encrypted storage and never returned to the renderer.
  */
  password?: string;
}

/** Replaces the reusable signatures belonging to one mailbox. */
export interface SaveMailSignaturesRequest {
  account: string;
  signatures: MailSignatureDto[];
  defaultSignatureId: string | null;
}

/** A mailbox folder, classified by the IMAP special-use flags it advertises. */
export interface MailFolderDto {
  /** Full IMAP path, e.g. "[Gmail]/Spam". */
  name: string;
  /** Leaf name for display, e.g. "Spam". */
  label: string;
  role: "inbox" | "drafts" | "sent" | "junk" | "trash" | "archive" | "flagged" | "other";
}

export interface MailAddressDto {
  name: string | null;
  address: string;
}

/** A message header row, as listed in a folder. */
export interface MailEnvelopeDto {
  /** Folder-relative id: only meaningful together with its folder. */
  id: string;
  subject: string;
  from: MailAddressDto;
  to: MailAddressDto | null;
  date: string;
  seen: boolean;
  flagged: boolean;
  answered: boolean;
  draft: boolean;
  hasAttachment: boolean;
  /** Sender-defined priority, separate from the recipient's mailbox flag. */
  importance?: MailImportance;
  /**
   * The first line or so of the message, for the list row to show under the
   * subject. Empty when the body could not be peeked at cheaply.
   */
  preview?: string;
}

/** A file carried by a message, as announced by its MIME part. */
export interface MailAttachmentDto {
  /** IMAP body section used to fetch just this part. */
  id: string;
  name: string;
  mime: string | null;
  /** Content-ID referenced by `cid:` URLs in the authored HTML. */
  contentId: string | null;
  disposition: "inline" | "attachment" | null;
  size: number;
}

/** One attachment's bytes, fetched lazily only when the reader displays it. */
export interface MailAttachmentContentDto {
  id: string;
  name: string;
  mime: string | null;
  content: ArrayBuffer;
}

export interface MailMessageDto {
  id: string;
  subject: string;
  from: MailAddressDto | null;
  to: MailAddressDto[];
  cc: MailAddressDto[];
  /** Only ever present on a message we sent: a received one never carries it. */
  bcc: MailAddressDto[];
  date: string;
  /** The message as text, always present — the fallback when there is no
   * HTML part, and what a reader falls back to if the markup is unusable. */
  body: string;
  /** The sender's own HTML, unsanitised, or null when the message has none.
   * Whatever displays it is responsible for sanitising it first. */
  html: string | null;
  attachments: MailAttachmentDto[];
  /** Sender-defined priority, separate from the recipient's mailbox flag. */
  importance?: MailImportance;
  /** RFC 5322 Message-ID, needed so a reply threads in the recipient's client. */
  messageId: string | null;
  /** The chain this message is part of, oldest first, from its References. */
  references: string[];
}

export interface MailListRequest {
  account?: string;
  folder?: string;
  page?: number;
  pageSize?: number;
  /** How the folder is ordered; date, newest first, when unset. */
  sort?: "date-desc" | "date-asc" | "subject" | "from";
  /** Plain words to look for. Turned into an IMAP search by the
   * main process, so callers never have to speak it. */
  query?: string;
}

/** How a message announces its priority to the recipient's mail client. */
export type MailImportance = "high" | "normal" | "low";
export interface SendMailInlineAttachment {
  /** Absolute path also present in `attachments`. */
  path: string;
  /** Stable MIME Content-ID referenced by the outgoing HTML's `cid:` link. */
  contentId: string;
}
export interface SendMailRequest {
  account?: string;
  to: string[];
  cc?: string[];
  bcc?: string[];
  subject: string;
  body: string;
  /** Optional formatted alternative for clients that render HTML mail. */
  html?: string;
  /** Saves to the drafts folder instead of sending. */
  draft?: boolean;
  /** Absolute paths to files to attach. */
  attachments?: string[];
  /** Files placed at authored nodes in the HTML alternative. Files omitted
   * here remain ordinary trailing attachments. */
  inlineAttachments?: SendMailInlineAttachment[];
  /** Marks the message urgent or low priority for the recipient's client.
   * "normal" is the default and writes no header. */
  importance?: MailImportance;
  /** Message-ID being answered, so the reply threads for the recipient. */
  inReplyTo?: string;
  /** The chain so far, which the reply extends. */
  references?: string[];
  /** A draft this replaces: saving an edited draft deletes the old copy
   * rather than leaving two versions in the folder. */
  replacesDraft?: {id: string; folder: string} | null;
}

export interface SendMailResult {
  /** The mailbox copy created by a draft save, so the next autosave can
   * replace it instead of adding another version. */
  draft?: {id: string; folder: string};
}

/** One conversation on a linked messaging platform. */
export interface ChatDto {
  id: string;
  name: string;
  platform: string;
  /** Stable remote conversation identity shared by duplicate Matrix portals. */
  remoteId?: string;
  /** This is the portal the bridge currently routes outbound traffic through. */
  currentPortal?: boolean;
  /** A Matrix Space: a navigational container whose child rooms are chats. */
  space?: boolean;
  /** A bridge's account-wide container, flattened into the platform rail. */
  defaultSpace?: boolean;
  /** Matrix Spaces this chat belongs to. A room may appear in more than one. */
  parentIds?: string[];
  /** Linked bridge accounts through which this conversation is available. */
  accountIds?: string[];
  /** Account-specific unread counts when a bridge exposes remote read state. */
  unreadByAccount?: Record<string, number>;
  /** Room avatar as an http(s) url the renderer can show, when it has one. */
  avatarUrl?: string | null;
  /** Unread messages the account has not acknowledged. */
  unread?: number;
  /**
   * When the newest message landed, as an ISO string. Drives the ordering of
   * the list: a chat list sorted by anything but recency is a list nobody can
   * find their last conversation in.
   */
  lastActivity?: string | null;
  /** One line of the newest message, for the row under the name. */
  preview?: string | null;
  /** Whether this is a group, which is drawn and named differently. */
  group?: boolean;
  /** The source platform attests that this is an official or verified account.
   * Absent is deliberately different from false: many bridges do not expose
   * the platform's trust metadata, and Polymux must not guess from a name. */
  official?: boolean;
}

/** A person who can be mentioned in one messaging conversation. */
export interface ChatMemberDto {
  /** Matrix identity used by bridges to turn a pill into a native mention. */
  userId: string;
  name: string;
  avatarUrl: string | null;
}

/** Shared native group name, distinct from this account's private label. */
export interface ChatGroupInfoDto {
  name: string;
  isMember: boolean;
}

/** The structured identity behind visible mention text in an outbound message. */
export interface ChatMentionDto {
  userId: string;
  /** Exact text inserted into the composer, including the leading `@`. */
  label: string;
}

export interface ChatMentionsDto {
  users: ChatMentionDto[];
  /** Encodes the composer's `@everyone` as Matrix's room-wide mention. */
  everyone?: boolean;
}

/** A person a linked messaging account can start a conversation with. */
export interface CommsContactDto {
  /** Stable across refreshes and unique across platform accounts. */
  id: string;
  /** The bridge's remote user id, passed back when starting a chat. */
  remoteId: string | null;
  name: string;
  platform: CommsPlatform;
  accountId: string;
  accountName: string;
  avatarUrl: string | null;
  /** Phone numbers, usernames, and other identifiers the platform exposes. */
  identifiers: string[];
  /** An already-open DM for this person, when one exists. */
  chatId: string | null;
  /** Every linked account that can reach this person. Keeping these routes on
   * one row avoids showing the same address-book entry once per login, and
   * lets a multi-selection choose an account shared by every participant. */
  accounts: Array<{
    accountId: string;
    accountName: string;
    remoteId: string | null;
    chatId: string | null;
  }>;
}

/** One stable route to a person inside a linked cross-platform contact. The
 * remote id survives a bridge reconnect that replaces the Matrix room; the
 * chat id keeps older bridges without one useful. */
export interface ContactLinkMemberDto {
  platform: CommsPlatform;
  remoteId: string | null;
  chatId: string;
}

/** A local contact identity. A single route stores a local display name;
 * several routes say those platform conversations are the same person. It
 * never sends the name or grouping to any source network. */
export interface ContactLinkDto {
  id: string;
  name: string;
  members: ContactLinkMemberDto[];
  createdAt: string;
  updatedAt: string;
}

export interface MergeContactLinkRequest {
  name: string;
  members: ContactLinkMemberDto[];
}

/** Gives the local identity containing this route a user-chosen display name.
 * If the route is not linked yet, the Hub creates a one-route identity. */
export interface RenameContactRequest {
  name: string;
  member: ContactLinkMemberDto;
}

/** Starts one direct conversation or a remote group on one linked account. */
export interface CreateChatRequest {
  platform: CommsPlatform;
  accountId: string;
  participantIds: string[];
  /** Required by group-capable platforms once more than one person is chosen. */
  name?: string;
}

/** One private destination inside a local broadcast. Broadcasts never create a
 * remote group: each recipient keeps the route to their own direct chat. */
export interface BroadcastRecipientDto {
  /** Stable contact identity, used to keep delivery results attached to rows. */
  id: string;
  name: string;
  platform: CommsPlatform;
  accountId: string;
  accountName: string;
  remoteId: string | null;
  /** Filled immediately for an existing DM, or after the first delivery. */
  chatId: string | null;
  avatarUrl: string | null;
}

/** A named, Polymux-local collection of private direct-message destinations. */
export interface BroadcastDto {
  id: string;
  name: string;
  recipients: BroadcastRecipientDto[];
  createdAt: string;
  updatedAt: string;
  lastActivity: string | null;
  preview: string | null;
}

export interface CreateBroadcastRequest {
  name: string;
  recipients: BroadcastRecipientDto[];
}

/** What happened for one person when a broadcast message fanned out. */
export interface BroadcastDeliveryDto {
  recipientId: string;
  recipientName: string;
  platform: CommsPlatform;
  chatId: string | null;
  status: "sent" | "failed";
  error?: string;
}

/** One authored broadcast, retained locally so its outbound history can be
 * read without merging the unrelated direct conversations it delivered to. */
export interface BroadcastMessageDto {
  id: string;
  broadcastId: string;
  body: string;
  sentAt: string;
  deliveries: BroadcastDeliveryDto[];
}

export interface BroadcastSendResultDto {
  broadcast: BroadcastDto;
  message: BroadcastMessageDto;
}

/** An image, voice note, video, or file carried by a message. */
export interface ChatAttachmentDto {
  kind: "image" | "audio" | "video" | "file";
  /**
   * Resolvable url for the bytes, already authenticated. Null means the
   * source network announced the attachment but could not expose its bytes;
   * the message's `viewIn` route remains the way to open it.
   */
  url: string | null;
  name: string;
  mimeType: string | null;
  size: number | null;
  /** Natural dimensions for an image or video, when the sender gave them. */
  width?: number | null;
  height?: number | null;
  /** Playback length in seconds, for audio and video. */
  duration?: number | null;
  /** A sticker, which is shown at a sticker's size rather than a photo's. */
  sticker?: boolean;
}

/** One account-native sticker Polymux can resend through WeChat. */
export interface ChatStickerDto {
  id: string;
  url: string;
  mimeType: string;
  size: number;
  width: number | null;
  height: number | null;
}

/** Metadata for a web link carried with a chat message. */
export interface ChatLinkPreviewDto {
  title: string;
  description: string | null;
  url: string | null;
  source: string | null;
  /** Authenticated renderer-facing image URL, when the preview supplies one. */
  imageUrl?: string | null;
  imageMimeType?: string | null;
  imageWidth?: number | null;
  imageHeight?: number | null;
}

/** A forwarded transcript. Native media stays in the source app until its
 * bytes have been retrieved; transport URLs and keys never enter this DTO. */
export interface ChatForwardedBundleDto {
  title: string;
  messages: ChatForwardedMessageDto[];
  truncated: boolean;
}

export interface ChatForwardedMessageDto {
  senderName: string | null;
  /** Native display time, kept verbatim when the source omits a time zone. */
  sentAt: string | null;
  kind: "text" | "image" | "audio" | "video" | "file" | "link" | "location" | "record" | "unknown";
  body: string;
  forwarded?: ChatForwardedBundleDto;
}

/** Historical call summary; a missing duration must never become a made-up 0:00. */
export interface ChatCallDto {
  kind: "voice" | "video";
  status: "ended" | "missed" | "declined" | "cancelled" | "incoming" | "started" | "unknown";
  durationSeconds: number | null;
}

export interface ChatMessageDto {
  id: string;
  chatId: string;
  sender: string;
  /** The sender's own name, rather than the `@platform_id:server` puppet. */
  senderName?: string;
  senderAvatarUrl?: string | null;
  body: string;
  /** A conversation event, such as somebody joining, rather than authored text. */
  notice?: boolean;
  sentAt: string;
  /** True when the signed-in account sent it. */
  mine: boolean;
  /** Submitted locally but not confirmed by the source platform. Never auto-retry. */
  deliveryStatus?: "unconfirmed";
  /** Media the message carries. Text messages have none. */
  attachments?: ChatAttachmentDto[];
  /** Structured link/card metadata rendered consistently across bridges. */
  linkPreview?: ChatLinkPreviewDto | null;
  forwarded?: ChatForwardedBundleDto | null;
  call?: ChatCallDto | null;
  /**
   * Set when the message holds something Polymux cannot bring across — a
   * voice note on a network with no media API, a photo whose key the source
   * app never unlocked. Names the app that can show it, and how to open it,
   * so the placeholder is a way through rather than a dead end.
   */
  viewIn?: {app: string; url: string} | null;
  /** One entry per distinct emoji on the message. */
  reactions?: ChatReactionDto[];
  /** The message this one answers, when it is a reply. */
  replyTo?: string | null;
}

export interface ChatReactionDto {
  key: string;
  count: number;
  /** People represented by this reaction, in the order their events arrived. */
  reactors?: ChatReactionActorDto[];
  /** Set when the signed-in account is one of the reactors, so it can undo it. */
  mineEventId?: string | null;
}

export interface ChatReactionActorDto {
  id: string;
  name: string;
  avatarUrl: string | null;
  /** The signed-in person's profile. */
  mine?: boolean;
}

export interface ChatActivityDto {
  /** The conversation that moved. */
  chatId: string;
  /** Who wrote it, so a view can tell its own echo from someone else's. */
  sender: string;
}

/**
 * What the hub knew when the app last quit, handed back in one read.
 *
 * The hub's panes are network-bound, so a launch used to be a skeleton until
 * IMAP and the homeserver answered. This is the paint-first half: the renderer
 * seeds itself from here before its first frame, then fetches and replaces
 * whatever moved. Everything in it is a copy — stale by definition, never
 * authoritative, and safe to be empty.
 */
export interface HubSnapshotDto {
  status: CommsStatusDto | null;
  chats: ChatDto[];
  /** A folder's first page, with the folder list it was read against. */
  mailboxes: Array<{
    account: string;
    folder: string;
    folders: MailFolderDto[];
    envelopes: MailEnvelopeDto[];
  }>;
  /** Message bodies already read, so opening one again is instant. */
  mail: Array<{account: string; folder: string; message: MailMessageDto}>;
  /** The newest page of the conversations most recently looked at. */
  messages: Array<{chatId: string; messages: ChatMessageDto[]; nextBefore: string | null}>;
}

export interface ChatPageDto {
  /** Newest first, the order the thread paints in. */
  messages: ChatMessageDto[];
  /** Passed back as `before` for the older page, or null at the room's start. */
  nextBefore: string | null;
}

export interface CommsStatusDto {
  hub: CommsHubDto;
  bridges: CommsBridgeDto[];
  email: {
    accounts: CommsEmailAccountDto[];
    /**
     * Providers this build can sign a mailbox in to. Empty where no OAuth
     * client is registered, which is what keeps a button that could only fail
     * off the screen.
     */
    signInProviders: CommsMailProvider[];
  };
}

/** Result of an explicit platform wake. `status` remains the passive fleet
 * snapshot, while `ready` answers whether this exact user/agent action made
 * the requested transport safe to use now. Keeping those facts separate lets
 * cached on-demand chats remain visible without pretending a signed-out
 * desktop session can accept an optimistic send. */
export interface CommsWakeDto {
  platform: CommsPlatform;
  ready: boolean;
  status: CommsStatusDto;
}

/** Ephemeral native login surface. Never include this in saved Hub snapshots. */
export interface WeChatLoginDto {
  state: "signed_in" | "signed_out" | "remembered_login" | "interactive_login" | "locked" | "launching" | "unavailable";
  qrDataUrl: string | null;
  expiresAt: number | null;
  optionsReady: boolean;
  issue?: "screen-recording" | "qr-expired" | "background-guard";
}

/**
 * A surface the agent has been asked to show, pushed to the renderer so the
 * workspace opens on it.
 *
 * The agent works in places the user cannot see — a draft saved to a mailbox,
 * a file written to a drive, a schedule it wrote down — and "show me" is then
 * an instruction to *navigate*, not to describe. This is that instruction: a
 * surface, and enough to say where inside it to land.
 */
export interface WorkspaceRevealDto {
  surface: WorkspaceSurface;
  /**
   * Whether this may move what the user is looking at. False lands the request
   * where it belongs — a draft in its composer — without opening the workspace
   * or fronting a tab, which is how a delegated run writes something the user
   * finds waiting rather than being switched to. Defaults to true.
   */
  focus?: boolean;
  /** Hub, mail half: the mailbox, the folder, and the message to open. */
  mail?: {
    account: string;
    folder?: string;
    /** Folder-relative id. Without one the newest message in the folder wins,
     * which is what "the draft you just wrote" means. */
    messageId?: string;
    /** Narrows that fallback to the newest message carrying this subject. */
    subject?: string;
    /**
     * Opens the mail composer already written, instead of landing on a
     * message. Nothing is saved or sent: the user reads it in the pane they
     * would have typed it in, and decides.
     *
     * `mode` other than "new" answers the message `messageId`/`subject` names:
     * the composer opens as a real reply or forward — recipients, Re:/Fwd:
     * subject, quoted body, threading headers — with `body` above the quote.
     */
    compose?: {
      to?: string;
      cc?: string;
      bcc?: string;
      subject?: string;
      body?: string;
      /** Absolute paths, the same as a message sent outright takes. */
      attachments?: string[];
      importance?: MailImportance;
      mode?: "new" | "reply" | "reply-all" | "forward";
    };
  };
  /**
   * Hub, messaging half: the room to open, by id or by name. `draft` fills
   * that chat's message box without sending — the messaging half's equivalent
   * of `mail.compose`.
   */
  chat?: {id?: string; name?: string; draft?: string; replyTo?: string};
  /** Drive: which source to browse, and the folder to land in. */
  drive?: {source?: string; path?: string};
}

/** The workspace surfaces the agent can ask for by name. */
export type WorkspaceSurface = "hub" | "drive" | "tasks" | "calendar" | "summary" | "phone" | "terminal" | "ide" | "locker" | "media" | "usage" | "finance";

/**
 * What the workspace looked like for one conversation: which tabs were open,
 * which was front, and whether the drawer was showing. Restored when the chat
 * is reopened — tabs are re-created, not kept live, so a browser tab reloads
 * its url rather than resuming a session.
 */
export interface WorkspaceSnapshotDto {
  tabs: Array<{
    id: string;
    title: string;
    kind: string;
    url?: string;
    favicon?: string | null;
    section?: string;
  }>;
  activeTabId: string | null;
  open: boolean;
}

/**
 * Backends the drive can read and write. `local` is this Mac's filesystem and
 * is always present; the rest are accounts the user connects. Adding one means
 * adding an adapter in `packages/drive` and a case here — nothing else in the
 * drive is provider-aware.
 *
 * `all` is not a backend at all: it is the virtual drive, the union of every
 * connected source. It holds nothing of its own and cannot be connected or
 * disconnected, which is why it carries the `virtual` kind and stays out of
 * the settings list.
 */
/** The application a protocol opens in: its name, and its icon as a data url
 * because a renderer cannot read one off the disk. */
export interface DefaultAppDto {
  name: string;
  /** `data:image/png;base64,...`, drawn at 32px so a 16px glyph stays sharp. */
  icon: string | null;
}

export type DriveProviderId =
  | "all"
  | "local"
  | "network"
  | "google-drive"
  | "dropbox"
  | "onedrive"
  | "s3";

/**
 * How a provider is connected, which is what decides the shape of its settings
 * panel: a folder picker, an OAuth button, or a credentials form.
 */
export type DriveProviderKind = "virtual" | "local" | "network" | "oauth" | "s3";

/**
 * `unconfigured` is distinct from `logged-out` on purpose: the first means this
 * build has no client credentials for the provider so the connect button cannot
 * work at all, the second means it can and the user simply has not used it.
 */
export type DriveProviderState =
  | "connected"
  | "logged-out"
  | "unconfigured"
  | "unavailable"
  | "error";

export interface DriveAccountDto {
  id: string;
  name: string;
  email: string | null;
}

/** Bytes. Either side is null when the provider does not report it — S3 has no
 * quota to read, and some accounts are uncapped. */
export interface DriveUsageDto {
  used: number | null;
  total: number | null;
  /** Bytes stored inside the root Polymux owns on this provider. */
  appUsed: number | null;
}

export interface DriveProviderDto {
  id: DriveProviderId;
  name: string;
  kind: DriveProviderKind;
  state: DriveProviderState;
  accounts: DriveAccountDto[];
  usage: DriveUsageDto | null;
  /** The local filesystem root, or the S3 bucket. Null for OAuth providers,
   * which keep their own app folder. */
  root: string | null;
  error: string | null;
}

/**
 * One browsable place in the drive: a provider plus the account it is signed in
 * as. Providers can hold several accounts, so the provider id alone no longer
 * says where a file should go — every drive operation addresses a source.
 *
 * The id is `<provider>#<accountId>`, opaque above the drive manager. The local
 * provider spends its two accounts on the folders the app always offers: the
 * output root and this Mac's home folder.
 */
export interface DriveSourceDto {
  id: string;
  provider: DriveProviderId;
  accountId: string;
  /** The provider's own name — "Google Drive", "This Mac". */
  name: string;
  /** Which account, set only when the provider has more than one signed in.
   * The switcher reads as `<name> – <accountLabel>` when this is set, which is
   * what tells two connected Google accounts apart; with a single account
   * there is nothing to tell apart and this is null. */
  accountLabel: string | null;
  state: DriveProviderState;
  usage: DriveUsageDto | null;
  root: string | null;
  error: string | null;
}

export interface DriveStatusDto {
  providers: DriveProviderDto[];
  /** Every place that can be browsed right now, in the order the switcher
   * should show them: the output folder, this Mac, then connected accounts. */
  sources: DriveSourceDto[];
  /**
   * Write preference, most-preferred first. A new file goes to the first
   * provider in this list that is currently connected, which is how the drive
   * picks a destination without asking every time.
   */
  saveOrder: DriveProviderId[];
}

export interface DriveEntryDto {
  /** Provider-scoped and opaque: a Drive file id, an S3 key, an absolute path. */
  id: string;
  name: string;
  kind: "folder" | "file";
  /** Bytes, or null for folders and anything the provider does not size. */
  size: number | null;
  /** ISO 8601, or null when the provider reports no timestamp. */
  modifiedAt: string | null;
  provider: DriveProviderId;
  /** Where the entry lives, in the provider's own addressing. Pass it back to
   * `list` to descend into a folder. */
  path: string;
  mimeType: string | null;
  /**
   * The provider's own token for *this* version of the entry — a Dropbox rev,
   * an S3 or Graph ETag, mtime and size for a local file. Opaque: it is only
   * ever compared for equality, never parsed or ordered.
   *
   * It is what makes a write conditional. A run that read a file holds the
   * version it read, and its write says "only if the file is still that one",
   * so an edit made in between — by another chat, by the user in the provider's
   * own web page, from their phone — fails the write instead of silently
   * replacing what it never saw. Null means the provider offered none, and the
   * write proceeds unconditionally.
   */
  version?: string | null;
  /**
   * Where the provider shows this file on the web, when it has such a page.
   *
   * Only the cloud providers do: a local or network file lives on a volume and
   * is opened in the OS file browser instead, and an S3 object has no
   * user-facing page at all. Null means there is nothing to open.
   */
  webUrl?: string | null;
}

export interface DriveS3ConfigRequest {
  bucket: string;
  region: string;
  /** Set for S3-compatible services (R2, MinIO, Backblaze); null means AWS. */
  endpoint: string | null;
  accessKeyId: string;
  /** Omitted when editing an existing config to keep the stored secret. */
  secretAccessKey?: string;
  /** Confines the drive to one prefix of the bucket rather than its root. */
  prefix: string | null;
  /** Required by most S3-compatible services, which do not do vhost addressing. */
  forcePathStyle: boolean;
}

/** 0 is Sunday, matching `Date#getDay`. */
export type ScheduleWeekday = 0 | 1 | 2 | 3 | 4 | 5 | 6;

/**
 * How often a schedule fires, as data rather than prose. The main process
 * computes every next run from these fields and the view renders the same
 * fields as a sentence, so neither side can drift from the other.
 *
 * `time` is 24-hour "HH:MM" read in `timeZone`, defaulting to this Mac's zone.
 */
export type ScheduleFrequencyDto =
  | {kind: "once"; at: number; timeZone?: string}
  | {kind: "hourly"; interval?: number; minute?: number; timeZone?: string}
  | {kind: "daily"; interval?: number; time: string; timeZone?: string}
  | {kind: "weekly"; interval?: number; days: ScheduleWeekday[]; time: string; timeZone?: string}
  | {kind: "monthly"; interval?: number; dayOfMonth: number; time: string; timeZone?: string}
  | {kind: "yearly"; interval?: number; month: number; dayOfMonth: number; time: string; timeZone?: string}
  /**
   * Anything the pickers cannot say — "every 15 minutes on weekdays between 9
   * and 5". Standard five-field cron, read in `timeZone`.
   */
  | {kind: "cron"; expression: string; timeZone?: string};

/**
 * `done` is a one-off that has already fired: it has no next run, so it is
 * neither active nor paused, and the view greys it out rather than showing a
 * cadence that will never come round again.
 */
export type ScheduleStatusDto = "active" | "paused" | "running" | "failed" | "done";

/** One firing, kept so the detail panel can say what the agent actually did. */
export interface ScheduleRunDto {
  id: string;
  startedAt: number;
  finishedAt?: number;
  outcome: "running" | "succeeded" | "failed";
  /** The agent's own account of the run — its closing message, trimmed. */
  summary?: string;
  error?: string;
  /** Where the run happened, so the detail panel can open the full thread. */
  conversationId?: string;
  runId?: string;
}

export interface ScheduleDto {
  id: string;
  title: string;
  /** The instruction the agent runs each time. */
  prompt: string;
  frequency: ScheduleFrequencyDto;
  status: ScheduleStatusDto;
  createdAt: number;
  nextRunAt?: number;
  lastRunAt?: number;
  /** Newest first, capped — the whole history is not worth keeping forever. */
  history: ScheduleRunDto[];
  /** A finished run the user has not opened yet. Drives the blue dot. */
  unread: boolean;
}

export interface ScheduleInput {
  title: string;
  prompt: string;
  frequency: ScheduleFrequencyDto;
}

export interface SchedulePatch {
  title?: string;
  prompt?: string;
  frequency?: ScheduleFrequencyDto;
  /** Only the two states the user can choose; the rest the scheduler owns. */
  status?: "active" | "paused";
}

export type CalendarSourceKind =
  | "local"
  | "icloud"
  | "google"
  | "exchange"
  | "caldav"
  | "subscription"
  | "birthdays"
  | "other";

export interface CalendarSourceDto {
  id: string;
  title: string;
  kind: CalendarSourceKind;
}

export interface CalendarListDto {
  id: string;
  title: string;
  color: string;
  editable: boolean;
  subscribed: boolean;
  source: CalendarSourceDto;
}

/** One coherent EventKit read for the visible range. Keeping the calendar list
 * and its events together avoids two helper launches and prevents a source
 * change landing between otherwise separate reads. */
export interface CalendarSnapshotDto {
  calendars: CalendarListDto[];
  events: CalendarEventDto[];
  fetchedAt: string;
}

export type CalendarRecurrenceFrequency = "daily" | "weekly" | "monthly" | "yearly";

export interface CalendarRecurrenceDto {
  frequency: CalendarRecurrenceFrequency;
  interval: number;
  count?: number;
  until?: string;
}

export type CalendarAvailability = "busy" | "free" | "tentative" | "unavailable";

export interface CalendarEventDto {
  id: string;
  calendarId: string;
  title: string;
  start: string;
  end: string;
  allDay: boolean;
  location?: string;
  notes?: string;
  url?: string;
  timeZone?: string;
  recurrence?: CalendarRecurrenceDto;
  alarmMinutes?: number;
  availability: CalendarAvailability;
  attendees: string[];
  editable: boolean;
}

export interface CalendarEventInput {
  calendarId: string;
  title: string;
  start: string;
  end: string;
  allDay: boolean;
  location?: string;
  notes?: string;
  url?: string;
  recurrence?: CalendarRecurrenceDto | null;
  alarmMinutes?: number | null;
  availability?: CalendarAvailability;
}

export type CalendarEventPatch = Omit<
  Partial<CalendarEventInput>,
  "location" | "notes" | "url"
> & {
  /** Null clears an existing EventKit value; omission leaves it unchanged. */
  location?: string | null;
  notes?: string | null;
  url?: string | null;
};

export interface CalendarImportResultDto {
  imported: number;
  skipped: number;
  fileName: string | null;
}

export interface CalendarExportRequest {
  start: string;
  end: string;
  calendarIds?: string[];
}

export type TaskCardStatus = "todo" | "in_progress" | "done";

export interface TaskCardDto {
  id: string;
  /** Chat whose context and agents own this task. */
  chatId: string;
  title: string;
  detail?: string;
  status: TaskCardStatus;
  /** The agent run currently owning this card, if in progress. */
  owner?: string;
  /** Whether a completed card has been reviewed by the user. */
  reviewed: boolean;
  /** Position within its column, for user reordering. */
  order: number;
  createdAt: number;
  updatedAt: number;
}

export interface TaskCardInput {
  chatId: string;
  title: string;
  detail?: string;
}

export interface TaskCardPatch {
  title?: string;
  detail?: string;
  status?: TaskCardStatus;
  owner?: string;
  reviewed?: boolean;
  order?: number;
}

export interface PolymuxApi {
  devices: {request(value: import("./device-pairing.js").DevicePairingRequest): Promise<import("./device-pairing.js").DevicePairingState>};
  account: {
    /** Current optional-account state; safe to call when Supabase is not configured. */
    get(): Promise<import("./account.js").AccountStatusDto>;
    signInWithPassword(email: string, password: string): Promise<import("./account.js").AccountSignInResult>;
    signUp(email: string, password: string): Promise<import("./account.js").AccountSignInResult>;
    /** Re-sends the signup confirmation email after an unconfirmed sign-in. */
    resendConfirmation(email: string): Promise<{ok: boolean; error?: string}>;
    /** Sends a password-reset email; keep the app open so the reset link can return. */
    requestPasswordReset(email: string): Promise<{ok: boolean; error?: string}>;
    /** Sets a new password after the reset email's loopback redirect. */
    updatePassword(password: string): Promise<import("./account.js").AccountSignInResult>;
    /**
     * Opens the provider's consent page in the user's browser and waits for
     * the loopback redirect. Resolves on completion, cancellation, or error
     * with the resulting state.
     */
    signInWithOAuth(provider: import("./account.js").AccountOAuthProvider): Promise<import("./account.js").AccountSignInResult>;
    /** Restores another saved session on this machine. */
    switchTo(userId: string): Promise<import("./account.js").AccountSignInResult>;
    signOut(): Promise<import("./account.js").AccountStatusDto>;
    /** Pushed on every sign-in, sign-out, and profile refresh. */
    subscribe(listener: (status: import("./account.js").AccountStatusDto) => void): () => void;
  };
  phone: {
    status(): Promise<PhoneStatusDto>;
    connect(): Promise<PhoneStatusDto>;
    pairAndroid(pairingAddress: string, pairingCode: string, connectAddress?: string): Promise<PhoneStatusDto>;
    iosSigningStatus(): Promise<PhoneIosSigningStatusDto>;
    iosSigningBegin(email: string, password: string): Promise<PhoneIosSigningStatusDto>;
    iosSigningComplete(code: string): Promise<PhoneIosSigningStatusDto>;
    iosSigningLogout(): Promise<PhoneIosSigningStatusDto>;
    stop(): Promise<PhoneStatusDto>;
    frame(): Promise<PhoneFrameDto>;
    tap(point: PhonePointDto): Promise<void>;
    swipe(from: PhonePointDto, to: PhonePointDto, durationMs?: number): Promise<void>;
    type(text: string): Promise<void>;
    home(): Promise<void>;
  };
  terminal: {
    /** Allocates a PTY. Optional `cwd` is used when the shell first starts. */
    create(cwd?: string): Promise<TerminalCreateDto>;
    /** Starts that session's shell if needed and returns output so far. */
    attach(id: string, cols: number, rows: number): Promise<TerminalAttachDto>;
    write(id: string, data: string): Promise<void>;
    resize(id: string, cols: number, rows: number): Promise<void>;
    /** Kills that session only. */
    close(id: string): Promise<void>;
    subscribe(listener: (event: TerminalEventDto) => void): () => void;
  };
  ide: {
    /** Opens a folder picker. Resolves to the chosen path, or null if cancelled. */
    pickFolder(): Promise<string | null>;
    /** One folder's names. Empty `path` is the project root. */
    list(root: string, path?: string): Promise<IdeEntryDto[]>;
    read(root: string, path: string): Promise<IdeFileDto>;
    write(root: string, path: string, content: string): Promise<void>;
    /** Creates a new text file. `path` is relative to the project root. */
    create(root: string, path: string, content?: string): Promise<IdeFileDto>;
    /** Moves or renames a file. Both paths are relative to the project root. */
    move(root: string, from: string, to: string): Promise<IdeFileDto>;
  };
  agentRuntime: {
    get(): Promise<AgentRuntimeDto>;
    registry(): Promise<AcpRegistryEntryDto[]>;
    /** Reads only safe configuration metadata; no external agent is launched. */
    inspectConfiguration(
      request: Extract<UpdateAgentRuntimeRequest, {kind: "acp"}>,
      sourceDirectory?: string,
    ): Promise<ExternalAgentProfileDto[]>;
    update(request: UpdateAgentRuntimeRequest): Promise<AgentRuntimeDto>;
    settings(): Promise<AgentSettingsDto>;
    authenticate(methodId: string): Promise<AgentSettingsDto>;
    logout(): Promise<AgentSettingsDto>;
    setConfigOption(id: string, value: string | boolean): Promise<AgentSettingsDto>;
    setProvider(request: SetAgentProviderRequest): Promise<AgentSettingsDto>;
    disableProvider(id: string): Promise<AgentSettingsDto>;
  };
  profiles: {
    list(): Promise<ProfilesDto>;
    create(name: string): Promise<ProfilesDto>;
    select(id: string): Promise<ProfilesDto>;
    rename(id: string, name: string): Promise<ProfilesDto>;
    setDefault(id: string): Promise<ProfilesDto>;
    duplicate(id: string): Promise<ProfilesDto>;
    remove(id: string): Promise<ProfilesDto>;
    connectExternal(request: ConnectExternalProfileRequest): Promise<ProfilesDto>;
    openFolder(id: string, target?: "profile" | "source"): Promise<void>;
    subscribe(listener: (profiles: ProfilesDto) => void): () => void;
  };
  extension: {
    /** Whether the browser extension is installed, and whether to prompt. */
    status(): Promise<BrowserExtensionDto>;
    /** Hides the title-bar chip until the extension is seen and lost again. */
    dismiss(): Promise<BrowserExtensionDto>;
    /** Opens the install page in the user's own browser. */
    openInstall(): Promise<void>;
  };
  general: {
    get(): Promise<GeneralSettingsDto>;
    update(settings: GeneralSettingsUpdate): Promise<GeneralSettingsDto>;
    /** Installed build identity, shown in the General settings tab. */
    version(): Promise<AppVersionDto>;
    /** Asks the configured update feed whether a newer build exists. */
    checkForUpdates(): Promise<AppUpdateDto>;
    /** Restarts into a downloaded update. No-op when none is staged. */
    installUpdate(): Promise<AppUpdateDto>;
    /**
     * Posts a sample notification past every switch, so Settings can show
     * whether the OS is letting them through at all. Answers "unsupported"
     * where the platform will never show one.
     */
    testNotification(): Promise<"posted" | "unsupported">;
    /**
     * Network-based approximate location (city-level), used when the
     * platform geolocation service cannot produce a position.
     */
    locate(): Promise<NonNullable<GeneralSettingsDto["location"]>>;
  };
  clipboard: {
    /** Writes real text, image pixels, or a file reference to the OS clipboard. */
    write(content: ClipboardContentDto): Promise<boolean>;
  };
  locker: {
    status(): Promise<LockerStatusDto>;
    create(password: string): Promise<LockerStatusDto>;
    unlock(password: string): Promise<LockerStatusDto>;
    lock(): Promise<LockerStatusDto>;
    /** Resets the idle-lock timer after a user action in Locker. */
    touch(): Promise<void>;
    list(): Promise<LockerListDto>;
    reveal(id: string): Promise<LockerSecretsDto>;
    totp(id: string): Promise<LockerTotpDto | null>;
    /** Current and next authenticator codes for every TOTP item. */
    codes(): Promise<LockerCodesDto[]>;
    /** otpauth URL for one item, used to draw its QR code. */
    otpauth(id: string): Promise<string | null>;
    save(item: LockerItemInputDto): Promise<LockerItemDto>;
    remove(id: string): Promise<LockerListDto>;
    restore(ids: string[]): Promise<LockerListDto>;
    purge(ids: string[]): Promise<LockerListDto>;
    emptyTrash(): Promise<LockerListDto>;
    pin(ids: string[], pinned: boolean): Promise<LockerListDto>;
    reorder(ids: string[]): Promise<LockerListDto>;
    changePassword(current: string, next: string): Promise<LockerStatusDto>;
    copy(id: string, field: LockerCopyField, recoveryIndex?: number): Promise<boolean>;
    importBegin(): Promise<LockerImportStartDto>;
    importConfirm(password: string): Promise<LockerImportResultDto>;
    /** Pulls or pushes the encrypted vault when storage is Account and the user is signed in. */
    sync(): Promise<LockerStatusDto>;
    /** This device vs Account. Account is the default. */
    setStorage(mode: LockerStorageMode, resolve?: LockerStorageResolve): Promise<LockerStatusDto>;
    subscribe(listener: (status: LockerStatusDto) => void): () => void;
  };
  /** Lifetime token, cost, and activity totals for the Usage app. */
  finance: {
    read(request: import("./finance.js").FinanceReadRequest): Promise<import("./finance.js").FinanceReadDto>;
  };
  usage: {
    get(filter?: UsageFilterDto): Promise<UsageStatsDto>;
  };
  window: {
    /** Opens a built-in workspace view in its own app window. */
    openWorkspaceView(
      kind: PinnableWorkspaceView,
      conversationId?: string,
      placement?: {x: number; y: number; width?: number; height?: number},
    ): Promise<void>;
    /** Opens the exact destination attached to a clicked system notification. */
    subscribeNotificationTarget(
      listener: (target: NotificationTargetDto) => void,
    ): () => void;
    /**
     * Full-screen state of the app window, pushed on every change and once on
     * subscribe. The renderer only reserves room for the macOS traffic lights
     * while they are on screen, which they are not in full screen.
     */
    subscribeFullscreen(listener: (fullscreen: boolean) => void): () => void;
  };
  permissions: {
    ensureFirstRun(): Promise<FirstRunPermissionDto>;
    status(permission: SystemPermissionKind): Promise<SystemPermissionStatus>;
    request(permission: SystemPermissionKind): Promise<SystemPermissionStatus>;
    openSettings(permission: SystemPermissionKind | "location"): Promise<void>;
  };
  dictation: {
    /**
     * Downloads the local speech-to-text model if it is not on the machine
     * yet, so the first press of the microphone transcribes straight away
     * instead of waiting on ~148MB. Safe to call at any time and as often as
     * you like: it resolves immediately once the model is in place, and
     * overlapping calls share the one download.
     */
    prepare(): Promise<void>;
    /**
     * Transcribes a mono 16kHz 16-bit WAV recording with the local
     * speech-to-text engine and resolves to the recognised text.
     *
     * @param final Whether the recording is finished. Passing false marks the
     *   pass as a partial, which trades a little accuracy for latency because
     *   another pass is coming right behind it.
     */
    transcribe(audio: ArrayBuffer, final?: boolean): Promise<string>;
  };
  conversations: {
    list(): Promise<ConversationDto[]>;
    listArchived(): Promise<ConversationDto[]>;
    create(title?: string): Promise<ConversationDto>;
    duplicate(id: ConversationId, throughMessageId?: string): Promise<ConversationDto>;
    rename(id: ConversationId, title: string): Promise<ConversationDto | null>;
    archive(id: ConversationId): Promise<ConversationDto | null>;
    unarchive(id: ConversationId): Promise<ConversationDto | null>;
    remove(id: ConversationId): Promise<boolean>;
    messages(id: ConversationId): Promise<MessageDto[]>;
    updateMessage(
      id: string,
      patch: { conversationId?: string; content?: JsonValue; metadata?: JsonValue; attachments?: string[] },
    ): Promise<MessageDto | null>;
  };
  team: {
    list(): Promise<BotDto[]>;
    groups(): Promise<TeamGroupDto[]>;
    createGroup(request: CreateTeamGroupRequest): Promise<TeamGroupDto>;
    updateGroup(id: string, request: UpdateTeamGroupRequest): Promise<TeamGroupDto>;
    markGroupRead(id: string): Promise<TeamGroupDto>;
    removeGroup(id: string): Promise<boolean>;
    sendGroup(request: SendTeamGroupMessageRequest): Promise<MessageDto>;
    /** Profiles installed on the computer currently acting as Team Host. */
    profiles(hostId?: string): Promise<ProfileDto[]>;
    create(request: CreateBotRequest): Promise<BotDto>;
    update(id: string, request: UpdateBotRequest): Promise<BotDto>;
    markRead(id: string): Promise<BotDto>;
    remove(id: string): Promise<boolean>;
    send(request: SendAgentMessageRequest): Promise<MessageDto>;
    startComputer(id: string): Promise<BotDto>;
    stopComputer(id: string): Promise<BotDto>;
    leases(id?: string): Promise<LaptopCapabilityLeaseDto[]>;
    grantLease(
      id: string,
      capabilities: LaptopCapabilityLeaseDto["capabilities"],
      minutes?: number,
    ): Promise<LaptopCapabilityLeaseDto>;
    revokeLease(id: string): Promise<boolean>;
    host(): Promise<TeamHostDto>;
    hosts(): Promise<TeamHostDto[]>;
    beginHostPairing(preserveFailures?: boolean): Promise<TeamHostDto>;
    pairHost(request: PairTeamHostRequest): Promise<TeamHostDto>;
    useLocalHost(): Promise<TeamHostDto>;
    setDefaultHost(hostId: string): Promise<TeamHostDto>;
    removeHost(hostId: string): Promise<TeamHostDto[]>;
    resetHostPairing(): Promise<TeamHostDto>;
    subscribeHost(listener: (host: TeamHostDto) => void): () => void;
    subscribeHosts(listener: (hosts: TeamHostDto[]) => void): () => void;
    subscribe(listener: (members: BotDto[]) => void): () => void;
    subscribeGroups(listener: (groups: TeamGroupDto[]) => void): () => void;
  };
  runs: {
    start(request: StartRunRequest): Promise<StartRunResponse>;
    cancel(runId: RunId): Promise<void>;
    steer(runId: RunId, text: string, messageId?: string): Promise<void>;
    events(runId: RunId, afterSequence?: number): Promise<RunEventDto[]>;
    subscribe(listener: (event: RunEventDto) => void): () => void;
  };
  activity: {
    /** A transient frame only; previews are never written to conversation history. */
    preview(request: ActivityPreviewRequestDto): Promise<string | null>;
  };
  manager: {
    snapshot(): Promise<ManagerSnapshotDto>;
    enqueue(request: EnqueueManagerJobRequest): Promise<ManagerJobDto>;
    cancel(id: string): Promise<ManagerJobDto>;
    reprioritize(id: string, priority: ManagerJobPriorityDto): Promise<ManagerJobDto>;
    reorder(id: string, targetId: string): Promise<ManagerJobDto[]>;
    subscribe(listener: (snapshot: ManagerSnapshotDto) => void): () => void;
  };
  goals: {
    execute(request: GoalCommandRequest): Promise<GoalDto | null>;
    get(conversationId: ConversationId): Promise<GoalDto | null>;
  };
  workspace: {
    snapshot(conversationId: ConversationId): Promise<WorkspaceSnapshotDto | null>;
    saveSnapshot(conversationId: ConversationId, snapshot: WorkspaceSnapshotDto): Promise<void>;
    /**
     * Grants the page read access to one file on disk and answers with the url
     * that serves it. The url carries a token, never the path, so a page can
     * only ever load a file the host handed it — the same boundary the browser
     * tool draws when it refuses a `file://` url.
     */
    preview(path: string): Promise<string>;
    /** Copies a granted preview file to a location the user chooses. */
    saveAs(url: string): Promise<string | null>;
    /** Asks for a photo or video and grants the page a preview url for it. */
    pick(): Promise<{url: string; name: string} | null>;
    /** What the agent asks to be shown; the drawer opens on it. */
    subscribeReveal(listener: (request: WorkspaceRevealDto) => void): () => void;
  };
  files: { paths(files: File[]): Promise<string[]> };
  resources: {
    artifacts(conversationId: ConversationId): Promise<ArtifactDto[]>;
    references(conversationId: ConversationId): Promise<ReferenceDto[]>;
    addFiles(conversationId: ConversationId, files: File[]): Promise<ReferenceDto[]>;
    subscribe(listener: (conversationId: ConversationId) => void): () => void;
  };
  memory: {
    status(): Promise<MemoryStatusDto>;
    setEnabled(enabled: boolean): Promise<MemoryStatusDto>;
    entries(): Promise<MemoryEntryDto[]>;
  };
  computerHistory: {
    status(): Promise<ComputerHistoryStatusDto>;
    setEnabled(enabled: boolean): Promise<ComputerHistoryStatusDto>;
    update(patch: ComputerHistorySettingsPatchDto): Promise<ComputerHistoryStatusDto>;
    /** Deletes every frame and event captured in the window. */
    forget(since: string, until: string): Promise<ComputerHistoryStatusDto>;
    /** Deletes one stored frame without touching neighbouring captures. */
    removeEntry(id: string): Promise<ComputerHistoryStatusDto>;
    /** Reveals one stored frame in the operating system's file browser. */
    revealEntry(id: string): Promise<void>;
    entries(options?: {
      since?: string;
      until?: string;
      limit?: number;
    }): Promise<ComputerHistoryEntryDto[]>;
    /** Human-readable ten-minute activities derived from raw local evidence. */
    activities(options?: {
      since?: string;
      until?: string;
      limit?: number;
    }): Promise<ComputerHistoryActivityDto[]>;
    /** Opens the system file picker at the applications folder. Returns the
     * chosen application's name, or null when the picker was dismissed. */
    pickApp(): Promise<string | null>;
    /** An installed application's own icon as a `data:` url, or null when the
     * application cannot be found under that name. */
    appIcon(name: string): Promise<string | null>;
  };
  mcp: {
    list(): Promise<McpServerDto[]>;
    reload(): Promise<McpServerDto[]>;
    setEnabled(id: string, enabled: boolean): Promise<McpServerDto[]>;
    saveCustom(request: SaveCustomMcpRequest): Promise<McpServerDto[]>;
    removeCustom(id: string): Promise<McpServerDto[]>;
    searchRegistry(query: string, cursor?: string): Promise<McpRegistryPageDto>;
    /**
     * Scans Pi's MCP configuration on this machine.
     */
    discover(): Promise<DiscoveredMcpGroupDto[]>;
    /** Copies a discovered server into ~/.polymux/mcp.json, where it becomes
     * an ordinary Polymux entry: editable, and removable. */
    adopt(groupId: string, serverId: string): Promise<McpServerDto[]>;
    subscribe(listener: (change: McpChangeDto) => void): () => void;
  };
  skills: {
    list(): Promise<SkillDto[]>;
    reload(): Promise<SkillDto[]>;
    /**
     * Fires when the skills directory changes under the tab's feet — most
     * often because the agent just wrote a skill from a recording.
     */
    subscribe(listener: (skills: SkillDto[]) => void): () => void;
    setEnabled(name: string, enabled: boolean): Promise<SkillDto[]>;
    saveCustom(request: SaveCustomSkillRequest): Promise<SkillDto[]>;
    removeCustom(name: string): Promise<SkillDto[]>;
    upload(files: File[]): Promise<SkillDto[]>;
    /**
     * Installs a package from the skills.sh ecosystem (GitHub-backed), e.g.
     * "vercel-labs/skills/find-skills" or a skills.sh / github.com URL.
     */
    install(spec: string): Promise<SkillDto[]>;
    /** Searches the skills.sh directory (minimum two characters). */
    searchRegistry(query: string, limit?: number): Promise<SkillRegistryEntryDto[]>;
    /**
     * Scans the skill directories of the other agents installed on this
     * machine, grouped by which one they belong to.
     */
    discover(): Promise<DiscoveredSkillGroupDto[]>;
    /** Copies a discovered skill's folder into ~/.polymux/skills. */
    adopt(path: string): Promise<SkillDto[]>;
  };
  /**
   * Claude Code plugins: a bundle of skills, MCP servers, commands, agents and
   * hooks installed as one unit from a marketplace. Whatever a plugin brings
   * stays on its own card — the `skills` and `mcp` surfaces above list only
   * what the user added directly.
   */
  plugins: {
    list(): Promise<PluginDto[]>;
    setEnabled(id: string, enabled: boolean): Promise<PluginDto[]>;
    /** Installs `<marketplace>/<plugin>` from an added marketplace. */
    install(id: string): Promise<PluginDto[]>;
    remove(id: string): Promise<PluginDto[]>;
    marketplaces(): Promise<PluginMarketplaceDto[]>;
    /** Adds a marketplace by `owner/repo` or a github.com URL. */
    addMarketplace(source: string): Promise<PluginMarketplaceDto[]>;
    removeMarketplace(id: string): Promise<PluginMarketplaceDto[]>;
    /** Every added marketplace's catalog, filtered by `query` when given. */
    browse(query?: string): Promise<MarketplacePluginDto[]>;
    /** Workspace views contributed by enabled plugins. */
    views(): Promise<PluginViewDto[]>;
    /**
     * Installs a plugin folder chosen on this machine — one holding a
     * `.claude-plugin/plugin.json` — under the local marketplace, which is
     * where anything not from a repository is filed.
     */
    upload(files: File[]): Promise<PluginDto[]>;
  };
  /** Profile-scoped workspace Apps. Browser is core and intentionally absent. */
  apps: {
    list(): Promise<WorkspaceAppsDto>;
    /** App discovery is separate from Claude Code plugin marketplaces. */
    browse(query?: string): Promise<MarketplaceAppDto[]>;
    install(id: string): Promise<WorkspaceAppsDto>;
    setEnabled(id: string, enabled: boolean): Promise<WorkspaceAppsDto>;
    setPinned(ids: string[]): Promise<WorkspaceAppsDto>;
    /** Official Apps reject removal. */
    remove(id: string): Promise<WorkspaceAppsDto>;
  };
  /**
   * Messaging bridges and email accounts. Linking runs entirely here rather
   * than through a bridge's management room, so a QR scan or cookie sign-in is
   * a step in this API rather than a chat command the user has to type.
   */
  comms: {
    status(): Promise<CommsStatusDto>;
    /**
     * What the hub showed last time, read from disk rather than the network.
     * Called before the first paint: it is what lets the hub open on content
     * instead of a skeleton, and every fetch after it is a correction.
     */
    snapshot(): Promise<HubSnapshotDto>;
    /** Re-probes the hub, every bridge, and every mailbox. */
    refresh(): Promise<CommsStatusDto>;
    /**
     * Starts a bridge that is not running yet, because its platform has just
     * been opened. Safe to fire on hover: a bridge already up makes this a
     * plain status read.
     */
    wake(platform: CommsPlatform): Promise<CommsWakeDto>;
    /** Mirrors the current native login QR and enables its login checkboxes. */
    weChatLogin(): Promise<WeChatLoginDto>;
    /** User-requested foreground opening; never used by passive polling. */
    weChatOpen(): Promise<void>;
    setHubUrl(baseUrl: string): Promise<CommsStatusDto>;
    /**
     * Sets messaging up with no input from the user: Polymux creates its own
     * account on the local hub and keeps the token in encrypted storage.
     */
    connect(): Promise<CommsStatusDto>;
    /**
     * Signs in as an existing account instead of provisioning one. Only needed
     * for a hub Polymux cannot provision into, such as a remote homeserver.
     */
    signIn(userId: string, password: string): Promise<CommsStatusDto>;
    signOut(): Promise<CommsStatusDto>;
    /** Begins a link and resolves to the first step to render. */
    loginStart(platform: CommsPlatform, flowId: string): Promise<CommsLoginStepDto>;
    /** Answers a `user_input` step. */
    loginSubmit(
      platform: CommsPlatform,
      loginId: string,
      stepId: string,
      values: Record<string, string>,
    ): Promise<CommsLoginStepDto>;
    /**
     * Waits out a `display_and_wait` step. Resolves when the remote side acts
     * on the QR or code, so callers should treat it as long-lived.
     */
    loginWait(
      platform: CommsPlatform,
      loginId: string,
      stepId: string,
    ): Promise<CommsLoginStepDto>;
    /**
     * Runs a `cookies` step by opening the network's own sign-in page in a
     * dedicated window and harvesting the session once it lands. Resolves when
     * the user finishes signing in, or rejects if they close the window.
     */
    loginCookies(
      platform: CommsPlatform,
      loginId: string,
      stepId: string,
    ): Promise<CommsLoginStepDto>;
    /** Abandons an in-flight login. */
    loginCancel(platform: CommsPlatform, loginId: string): Promise<CommsStatusDto>;
    /** Unlinks a remote account from a bridge. */
    bridgeLogout(platform: CommsPlatform, accountId: string): Promise<CommsStatusDto>;
    /**
     * Records the values a bridge needs before it can run, then restarts it so
     * they take effect. Used for Telegram's api_id/api_hash pair.
     */
    bridgeSetup(
      platform: CommsPlatform,
      values: Record<string, string>,
    ): Promise<CommsStatusDto>;
    /** Conversations across every linked messaging platform. */
    chats(): Promise<ChatDto[]>;
    /** People exposed by linked accounts, including DMs already in the Hub. */
    chatContacts(): Promise<CommsContactDto[]>;
    /** Current participants available to the chat composer's mention menu. */
    chatMembers(chatId: string): Promise<ChatMemberDto[]>;
    chatGroupInfo(chatId: string): Promise<ChatGroupInfoDto>;
    chatRenameGroup(chatId: string, name: string, expectedName: string): Promise<ChatGroupInfoDto>;
    /** User-approved local contact identities and cross-platform links. */
    contactLinks(): Promise<ContactLinkDto[]>;
    /** Creates or extends one cross-platform identity. Overlapping links fold together. */
    contactLinkMerge(request: MergeContactLinkRequest): Promise<ContactLinkDto>;
    /** Renames one contact locally, preserving every route already linked to it. */
    contactRename(request: RenameContactRequest): Promise<ContactLinkDto>;
    /** Removes one local identity, separating linked routes or clearing a one-route name. */
    contactLinkRemove(id: string): Promise<void>;
    /** Opens a DM or creates a real remote group, returning its Matrix room id. */
    chatCreate(request: CreateChatRequest): Promise<string>;
    /** Named local recipient sets whose messages are delivered as private DMs. */
    broadcasts(): Promise<BroadcastDto[]>;
    broadcastCreate(request: CreateBroadcastRequest): Promise<BroadcastDto>;
    /** Outbound-only local history, newest first. */
    broadcastMessages(broadcastId: string): Promise<BroadcastMessageDto[]>;
    /** Delivers the same text separately to every recipient. */
    broadcastSend(broadcastId: string, text: string): Promise<BroadcastSendResultDto>;
    /**
     * One page of a conversation, newest first, with the token that reaches
     * the page before it. Scrolling back up a long history is walking that
     * token until it comes back null.
     */
    chatMessages(chatId: string, limit?: number, before?: string): Promise<ChatPageDto>;
    /** `replyTo` quotes an earlier message; mentions remain structured for bridges. */
    chatSend(
      chatId: string,
      text: string,
      replyTo?: string,
      mentions?: ChatMentionsDto,
    ): Promise<ChatMessageDto>;
    /** Sends files into a conversation, one message each. */
    chatSendFiles(chatId: string, paths: string[]): Promise<void>;
    /** Opens the file picker for the composer's attach button. */
    chatPickFiles(): Promise<string[]>;
    /** Sends a recorded voice note, as bytes rather than a file on disk. */
    chatSendAudio(chatId: string, bytes: Uint8Array, mimetype: string): Promise<void>;
    /** Account-native stickers available in this WeChat session. */
    chatStickers(chatId: string): Promise<ChatStickerDto[]>;
    /** Sends one selected account-native sticker. */
    chatSendSticker(chatId: string, stickerId: string): Promise<void>;
    /** Recalls one of the signed-in account's own messages. */
    chatRecall(chatId: string, messageId: string): Promise<void>;
    /** Puts an emoji on a message. */
    chatReact(chatId: string, messageId: string, key: string): Promise<string>;
    /** Takes a reaction back, given the id `chatReact` returned. */
    chatUnreact(chatId: string, reactionId: string): Promise<void>;
    /** Marks a chat read up to `messageId`, clearing its unread count. */
    /** Returns false when Hub incognito mode deliberately suppresses it. */
    chatMarkRead(chatId: string, messageId: string): Promise<boolean>;
    mailFolders(account?: string): Promise<MailFolderDto[]>;
    mailEnvelopes(request: MailListRequest): Promise<MailEnvelopeDto[]>;
    mailMessage(id: string, account?: string, folder?: string): Promise<MailMessageDto>;
    /** Sends, or saves to drafts when `draft` is set. */
    mailSend(request: SendMailRequest): Promise<SendMailResult>;
    /** Moves messages to another folder — how junk and trash are applied. */
    mailMove(ids: string[], target: string, account?: string, folder?: string): Promise<void>;
    /** Erases messages outright. Emptying trash is this over every id in it. */
    mailDelete(ids: string[], account?: string, folder?: string): Promise<void>;
    /** Reads one MIME part for an inline image or document preview. */
    mailAttachment(
      id: string,
      part: string,
      account?: string,
      folder?: string,
    ): Promise<MailAttachmentContentDto>;
    /**
     * Saves a message's attachments to the downloads directory and returns
     * where they landed, so the caller can open them.
     */
    mailDownload(id: string, account?: string, folder?: string): Promise<string[]>;
    /** Opens a saved file with whatever the OS uses for it. */
    mailOpenFile(path: string): Promise<void>;
    /** Picks files to attach, returning their paths. Empty if cancelled. */
    mailPickFiles(): Promise<string[]>;
    /** Adds or removes an IMAP flag, e.g. marking a message read. */
    mailFlag(
      ids: string[],
      flag: "seen" | "flagged",
      on: boolean,
      account?: string,
      folder?: string,
    ): Promise<void>;
    emailSave(request: SaveEmailAccountRequest): Promise<CommsStatusDto>;
    emailSignaturesSave(request: SaveMailSignaturesRequest): Promise<CommsStatusDto>;
    emailRemove(id: string): Promise<CommsStatusDto>;
    /** Opens IMAP and SMTP connections to prove the account works. */
    emailTest(id: string): Promise<CommsEmailAccountDto>;
    /**
     * Signs a mailbox in with its provider. The address comes back from the
     * provider, so nothing about the account is asked for first — and an
     * address already set up is converted rather than duplicated.
     */
    emailSignIn(provider: CommsMailProvider): Promise<CommsStatusDto>;
    subscribe(listener: (status: CommsStatusDto) => void): () => void;
    /**
     * Fires as a message lands in a conversation, so the open thread updates
     * as it arrives rather than whenever it is next polled.
     */
    subscribeActivity(listener: (activity: ChatActivityDto) => void): () => void;
  };
  models: {
    list(): Promise<ModelDto[]>;
    select(provider: string, id: string): Promise<ModelDto>;
    /** What every role currently points at. */
    roles(): Promise<ModelRolesDto>;
    /** Points `role` at a model, at the reasoning level chosen with it when the
     * model takes one. Assigning `main` also switches the agent. */
    assignRole(
      role: ModelRole,
      provider: string,
      id: string,
      reasoning?: ReasoningEffort,
    ): Promise<ModelRolesDto>;
    /** Clears a role's override so it follows the main model again. */
    clearRole(role: ModelRole): Promise<ModelRolesDto>;
    /** Catalogue detail for the current models, keyed `<provider>:<id>`. */
    metadata(): Promise<Record<string, ModelMetadataDto>>;
  };
  /**
   * The embedded workspace browser: real Chromium web contents hosted by the
   * main process, positioned under a renderer-measured rectangle. `embedded`
   * is false in the browser demo, where BrowserView falls back to an iframe.
   */
  browser: {
    embedded: boolean;
    /** Answers with the tab's live page, so a pane mounting over a tab that
     * already loaded — every tab the agent opens — knows there is a page there
     * without waiting on a state event that is not coming. */
    open(
      tabId: string,
      url?: string,
      viewport?: {width: number; height: number},
    ): Promise<{url: string; title: string}>;
    navigate(tabId: string, url: string): Promise<void>;
    history(tabId: string, delta: -1 | 1): Promise<void>;
    reload(tabId: string): Promise<void>;
    setBounds(tabId: string, bounds: {x: number; y: number; width: number; height: number}): Promise<void>;
    setVisible(tabId: string, visible: boolean): Promise<void>;
    close(tabId: string): Promise<void>;
    openExternal(url: string): Promise<void>;
    /**
     * The application this Mac opens something with, named and drawn the way
     * the user knows it. A menu offering to hand something over says which
     * application it means — "Open in Helium", with Helium's own icon — rather
     * than "open externally", which names nothing.
     *
     * With no `target` this is the browser web links go to. With a file path it
     * is whatever owns that file's type: "Open in Preview" for a PDF.
     */
    defaultApp(target?: string): Promise<DefaultAppDto | null>;
    /**
     * Reveals a local file in its default application. Separate from
     * `openExternal` because the path comes from model-written markdown: the
     * main process resolves and checks it rather than handing the string to
     * the shell.
     */
    openPath(filePath: string): Promise<void>;
    find(tabId: string, text: string, forward: boolean): Promise<void>;
    stopFind(tabId: string): Promise<void>;
    print(tabId: string): Promise<void>;
    /** A transient image of the live page used behind renderer popovers. */
    preview(tabId: string): Promise<string | null>;
    screenshot(tabId: string): Promise<BrowserDownloadDto | null>;
    /**
     * A site icon as a `data:` url, or null when the site has none. Fetched by
     * the main process: the renderer's CSP allows no remote images, so an icon
     * has to arrive as bytes rather than as a url to load.
     */
    favicon(url: string): Promise<string | null>;
    downloads(): Promise<BrowserDownloadDto[]>;
    openDownload(id: string): Promise<void>;
    openDownloadsFolder(): Promise<void>;
    pauseDownload(id: string): Promise<BrowserDownloadDto[]>;
    resumeDownload(id: string): Promise<BrowserDownloadDto[]>;
    cancelDownload(id: string): Promise<BrowserDownloadDto[]>;
    /** Forgets one entry. The file on disk is left where it is — this is the
     * history, not the download. */
    removeDownload(id: string): Promise<BrowserDownloadDto[]>;
    clearDownloads(): Promise<BrowserDownloadDto[]>;
    settings(): Promise<BrowserSettingsDto>;
    /** Passing `downloadDirectory: null` opens the folder picker in the main
     * process, the same way the drive's local root is chosen. */
    updateSettings(
      patch: Partial<Omit<BrowserSettingsDto, "downloadDirectory">> & {
        downloadDirectory?: string | null;
      },
    ): Promise<BrowserSettingsDto>;
    permissions(): Promise<SitePermissionDto[]>;
    setPermission(
      origin: string,
      permission: BrowserPermissionDto,
      decision: PermissionDecisionDto,
    ): Promise<SitePermissionDto[]>;
    /** Drops one site's decisions, or every site's when origin is omitted. */
    clearPermissions(origin?: string): Promise<SitePermissionDto[]>;
    /** Answers a live prompt. `remember` stores the decision for next time. */
    respondToPermission(
      id: string,
      decision: "allow" | "deny",
      remember: boolean,
    ): Promise<void>;
    /** Chooses one account from a live passkey prompt. Omitting the credential
     * id cancels the authentication request. */
    respondToWebAuthn(id: string, credentialId?: string): Promise<void>;
    sites(): Promise<BrowserSiteDto[]>;
    /** Clears one site's cookies, storage and caches. Chromium clears cookies
     * at the registrable domain, so neighbouring subdomains go with it. */
    clearSiteData(origin: string): Promise<BrowserSiteDto[]>;
    /** The whole jar: cookies, storage, caches and history. Saved logins are
     * kept unless `logins` is set. */
    clearBrowsingData(options: {
      cookies: boolean;
      cache: boolean;
      downloads: boolean;
      permissions: boolean;
      logins: boolean;
    }): Promise<void>;
    logins(): Promise<SavedLoginDto[]>;
    saveLogin(
      origin: string,
      username: string,
      password: string,
    ): Promise<SavedLoginDto[]>;
    /** Returns one password in the clear, for a reveal or copy the user asked
     * for. Never called to populate a list. */
    revealLogin(id: string): Promise<string | null>;
    deleteLogin(id: string): Promise<SavedLoginDto[]>;
    /** Fills one offered Locker or saved-login item into the current page. */
    fillAutofill(tabId: string, itemId: string): Promise<boolean>;
    dismissAutofill(tabId: string): Promise<void>;
    /** The browsers found on this machine, with their readable profiles. */
    importSources(): Promise<BrowserSourceDto[]>;
    /** Pages visited, newest first. `query` matches url or title. Named apart
     * from `history` above, which is this tab's back/forward navigation. */
    browsingHistory(options?: {query?: string; limit?: number}): Promise<BrowserHistoryEntryDto[]>;
    /** Search-provider completions for text typed into the address bar. */
    suggestions(query: string): Promise<string[]>;
    forgetHistoryEntry(url: string): Promise<BrowserHistoryEntryDto[]>;
    /** Everything, or just what an import brought in. */
    clearHistory(options?: {source?: "import"}): Promise<BrowserHistoryEntryDto[]>;
    importFrom(request: BrowserImportRequestDto): Promise<BrowserImportResultDto>;
    /** The fallback path: a passwords CSV or a Netscape cookies.txt the user
     * exported themselves. Passing no path opens the file picker. */
    importFile(path?: string): Promise<BrowserImportResultDto>;
    subscribe(listener: (event: BrowserEventDto) => void): () => void;
  };
  /**
   * Files, across every storage backend the user has connected. Browsing is
   * per-provider rather than merged: a path only means something inside the
   * provider that issued it.
   */
  drive: {
    status(): Promise<DriveStatusDto>;
    /** Re-probes every provider's credentials and quota. */
    refresh(): Promise<DriveStatusDto>;
    /**
     * Connects an account. OAuth providers open the provider's own consent page
     * in a dedicated window and resolve once it lands; rejects if the user
     * closes it.
     */
    connect(provider: DriveProviderId): Promise<DriveStatusDto>;
    /** Drops one account's stored credentials. Omitting `accountId`
     * disconnects every account of the provider. */
    disconnect(provider: DriveProviderId, accountId?: string): Promise<DriveStatusDto>;
    setSaveOrder(order: DriveProviderId[]): Promise<DriveStatusDto>;
    /**
     * Points the local provider at the folder agent output is written to.
     * Passing null opens a picker. Defaults to `~/Documents/Polymux`.
     */
    setLocalRoot(path: string | null): Promise<DriveStatusDto>;
    /**
     * Adds a network share by its mount point. Passing no path opens a folder
     * picker. Several can be connected at once, each its own place in the
     * drive; adding one already there renames it rather than duplicating it.
     */
    addShare(path?: string | null, label?: string): Promise<DriveStatusDto>;
    /**
     * Shows an entry where it actually lives.
     *
     * For this Mac and for a network share that is the OS file browser, which
     * is the honest answer: the file is a file on a volume. Cloud providers
     * have no such place, so they are the caller's to open on the web.
     */
    revealEntry(source: string, path: string): Promise<void>;
    /**
     * Opens the file itself, the way double-clicking it in the OS file browser
     * would. Only for entries that are on a volume; a cloud file has no such
     * path and is opened at its `webUrl` instead.
     */
    openEntry(source: string, path: string): Promise<void>;
    /** Forgets a share. The files on the server are untouched. */
    removeShare(id: string): Promise<DriveStatusDto>;
    saveS3(config: DriveS3ConfigRequest): Promise<DriveStatusDto>;
    /** One folder's contents. `path` empty means the source's root. */
    list(source: string, path?: string): Promise<DriveEntryDto[]>;
    createFolder(source: string, parentPath: string, name: string): Promise<DriveEntryDto>;
    /** Uploads files and folders chosen on this Mac. Empty `paths` opens a
     * picker; a folder is recreated with everything under it. */
    upload(source: string, parentPath: string, paths?: string[], onProgress?: (fraction: number) => void): Promise<DriveEntryDto[]>;
    /** Fetches to the downloads folder and resolves to where it landed. */
    download(source: string, path: string): Promise<string>;
    remove(source: string, paths: string[]): Promise<void>;
    rename(source: string, path: string, name: string): Promise<DriveEntryDto>;
    /** Moves entries into another folder of the same source. */
    move(source: string, paths: string[], destinationFolder: string, onProgress?: (fraction: number) => void): Promise<DriveEntryDto[]>;
    /** Duplicates entries alongside themselves. */
    copy(source: string, paths: string[]): Promise<DriveEntryDto[]>;
    subscribe(listener: (status: DriveStatusDto) => void): () => void;
  };
  /**
   * Recurring instructions the agent runs on its own. The main process owns
   * the clock and the run history; the renderer only ever edits and reads.
   */
  schedules: {
    list(): Promise<ScheduleDto[]>;
    create(input: ScheduleInput): Promise<ScheduleDto>;
    update(id: string, patch: SchedulePatch): Promise<ScheduleDto>;
    remove(id: string): Promise<void>;
    /** Fires the schedule now without disturbing its cadence. */
    runNow(id: string): Promise<ScheduleDto>;
    /** Clears the unread mark a finished run left behind. */
    markRead(id: string): Promise<ScheduleDto>;
    subscribe(listener: (items: ScheduleDto[]) => void): () => void;
  };
  /** The Mac's calendar accounts through EventKit. Accounts already connected
   * to iCloud, Google, Exchange or CalDAV stay synced by the system; Polymux
   * reads and writes the same event store Apple Calendar uses. */
  calendar: {
    snapshot(start: string, end: string): Promise<CalendarSnapshotDto>;
    calendars(): Promise<CalendarListDto[]>;
    events(start: string, end: string, calendarIds?: string[]): Promise<CalendarEventDto[]>;
    create(input: CalendarEventInput): Promise<CalendarEventDto>;
    update(id: string, patch: CalendarEventPatch): Promise<CalendarEventDto>;
    remove(id: string): Promise<void>;
    /** Opens an .ics picker and imports its events into one writable calendar. */
    importFile(calendarId: string): Promise<CalendarImportResultDto>;
    /** Saves the selected range as an interoperable .ics file. */
    exportFile(request: CalendarExportRequest): Promise<string | null>;
    /** Opens the system account pane where Google, Exchange and CalDAV accounts are added. */
    openAccounts(): Promise<void>;
    /** EventKit invalidation signal. The next read supplies the changed data. */
    subscribe(listener: () => void): () => void;
  };
  tasks: {
    list(chatId: string): Promise<TaskCardDto[]>;
    create(input: TaskCardInput): Promise<TaskCardDto>;
    update(id: string, patch: TaskCardPatch): Promise<TaskCardDto>;
    remove(id: string): Promise<void>;
    markRead(id: string): Promise<TaskCardDto>;
    subscribe(listener: (items: TaskCardDto[]) => void): () => void;
  };
  providers: {
    list(): Promise<ProviderDto[]>;
    saveApiKey(provider: string, apiKey: string): Promise<ProviderDto>;
    removeApiKey(provider: string, keyId: string): Promise<ProviderDto>;
    connectOAuth(provider: string): Promise<ProviderDto>;
    cancelOAuth(provider: string): Promise<void>;
    disconnectOAuth(provider: string): Promise<ProviderDto>;
    subscribeOAuth(listener: (event: ProviderOAuthEventDto) => void): () => void;
    createCustom(request: CreateCustomProviderRequest): Promise<ProviderDto>;
    updateCustom(request: UpdateCustomProviderRequest): Promise<ProviderDto>;
    discoverModels(
      request: DiscoverModelsRequest,
    ): Promise<Array<{id: string; name?: string}>>;
    setupLocalRuntime(request: SetupLocalRuntimeRequest): Promise<ProviderDto>;
  };
}
