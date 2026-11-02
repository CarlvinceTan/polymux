import {conversationCopyTitle} from '../../../shared/conversation-copy';
import type {
  AccountStatusDto,
  ArtifactDto,
  AgentRuntimeDto,
  AgentSettingsDto,
  AppUpdateDto,
  CalendarEventDto,
  CalendarListDto,
  ChatDto,
  ChatMessageDto,
  ContactLinkDto,
  ContactLinkMemberDto,
  ComputerHistoryActivityDto,
  ComputerHistoryEntryDto,
  ComputerHistoryStatusDto,
  CommsEmailAccountDto,
  CommsPlatform,
  CommsStatusDto,
  DiscoveredMcpGroupDto,
  DiscoveredSkillGroupDto,
  BrowserDownloadDto,
  BrowserEventDto,
  BrowserHistoryEntryDto,
  BrowserSettingsDto,
  BrowserSiteDto,
  BrowserSourceDto,
  BroadcastDto,
  BroadcastMessageDto,
  ChatActivityDto,
  ChatStickerDto,
  DriveEntryDto,
  DriveProviderId,
  DriveStatusDto,
  MailEnvelopeDto,
  MailFolderDto,
  WorkspaceRevealDto,
  WorkspaceSnapshotDto,
  WorkspaceAppsDto,
  ConversationDto,
  GoalDto,
  GeneralSettingsDto,
  JsonValue,
  McpServerDto,
  SavedLoginDto,
  SitePermissionDto,
  MessageDto,
  PolymuxApi,
  ModelDto,
  ModelMetadataDto,
  ModelRole,
  ModelRoleAssignmentDto,
  ModelRolesDto,
  ProviderDto,
  ProfileDto,
  ReasoningEffort,
  ReferenceDto,
  RunEventDto,
  ScheduleDto,
  MarketplaceAppDto,
  MarketplacePluginDto,
  PluginDto,
  PluginMarketplaceDto,
  PhoneIosSigningStatusDto,
  PhoneStatusDto,
  SkillDto,
  SendMailRequest,
  StartRunRequest,
  TeamGroupDto,
  TeamHostDto,
  TerminalEventDto,
  UsageStatsDto,
  IdeEntryDto,
  IdeFileDto,
  BotDto,
} from '@polymux/protocol';
import {LOCAL_RUNTIMES, parseDriveSourceId} from '@polymux/protocol';
import {isBinaryFileName, languageForName} from '../../../main/ide/language';
import {EXTENSION_INSTALL_URL} from '../../../shared/extension';

let browserApi: PolymuxApi | undefined;
let demoDevicePairing: import('@polymux/protocol').DevicePairingState = {approvals: typeof location !== 'undefined' && new URLSearchParams(location.search).has('deviceApproval') ? [{id: 'incoming', deviceName: 'Test Phone', choices: ['17', '42', '68'], expiresAt: new Date(Date.now() + 120000).toISOString()}] : [], connectedDevices: [], outgoing: null};

export function polymuxApi(): PolymuxApi {
  if (typeof window !== 'undefined' && window.polymux) return window.polymux;
  if (import.meta.env.DEV || import.meta.env.VITE_POLYMUX_BROWSER_DEMO === 'true')
    return browserApi ??= createBrowserDemoApi();
  throw new Error('The Polymux desktop bridge is unavailable. Open this build through the desktop app.');
}

/** Demo mode follows the desktop contact-link contract: extending an
 * overlapping identity retains every route that was already linked. */
export function mergeDemoContactLinkMembers(
  overlapping: readonly ContactLinkDto[],
  requested: readonly ContactLinkMemberDto[],
): ContactLinkMemberDto[] {
  const members: ContactLinkMemberDto[] = [];
  for (const member of [
    ...overlapping.flatMap((link) => link.members),
    ...requested,
  ]) {
    const existing = members.findIndex((candidate) =>
      sameDemoContactMember(candidate, member),
    );
    if (existing < 0) members.push(member);
    else members[existing] = member;
  }
  return members;
}

function sameDemoContactMember(
  left: ContactLinkMemberDto,
  right: ContactLinkMemberDto,
): boolean {
  if (left.platform !== right.platform) return false;
  const leftRemote = left.remoteId?.trim().normalize('NFKC').toLowerCase();
  const rightRemote = right.remoteId?.trim().normalize('NFKC').toLowerCase();
  return leftRemote && rightRemote
    ? leftRemote === rightRemote
    : left.chatId.trim() === right.chatId.trim();
}

/** A development-only adapter keeps browser-based component tests useful. The
 * packaged desktop never selects it because preload supplies `window.polymux`. */
function createBrowserDemoApi(): PolymuxApi {
  // The dev branch always represents an already-configured profile.
  const onboardingPreview = false;
  const permissionPreview = new URLSearchParams(window.location.search).get('permissions') === 'preview';
  const releaseNotesPreview = new URLSearchParams(window.location.search).get('releaseNotesPreview') === '1';
  const weChatMissingPreview = new URLSearchParams(window.location.search).get('wechat') === 'missing';
  const emptyTeamPreview = new URLSearchParams(window.location.search).get('team') === 'empty';
  const emptyChatsPreview = new URLSearchParams(window.location.search).get('chats') === 'empty';
  const teamGroupSendPreview = new URLSearchParams(window.location.search).get('teamGroupSend');
  const now = Date.now();
  let conversations: ConversationDto[] = emptyChatsPreview ? [] : [
    conversation('welcome', 'Planning a product launch', now - 86_400_000),
    conversation('research', 'Research notes', now - 3 * 86_400_000),
  ];
  const messages = new Map<string, MessageDto[]>(emptyChatsPreview ? [] : [
    ['welcome', [
      message('m1', 'welcome', 'user', 'Help me outline a simple launch plan.', now - 6000),
      message('m2', 'welcome', 'assistant', [{type: 'text', text: 'I can turn that into a concise plan with milestones, owners, and launch-day checks.'}], now - 2000),
    ]],
  ]);
  if (new URLSearchParams(location.search).has('memoryCitations')) {
    messages.set('welcome', [
      message('m1', 'welcome', 'user', 'Help me outline a simple launch plan.', now - 6000),
      message('m2', 'welcome', 'assistant', [{type: 'text', text: 'Here is your concise launch plan.\n\n<polymux-memories>["Maintained personal pi presentation preferences", "Prefers concise project updates"]</polymux-memories>'}], now - 2000),
    ]);
  }
  if (new URLSearchParams(location.search).has('forkChat')) {
    messages.set('welcome', [
      message('m1', 'welcome', 'user', 'First question', now - 6000),
      message('m2', 'welcome', 'assistant', [{type: 'text', text: 'First answer'}], now - 5000),
      message('m3', 'welcome', 'user', 'Later question', now - 4000),
      message('m4', 'welcome', 'assistant', [{type: 'text', text: 'Later answer'}], now - 3000),
    ]);
  }
  const goals = new Map<string, GoalDto>();
  const addedReferences = new Map<string, ReferenceDto[]>();
  const listeners = new Set<(event: RunEventDto) => void>();
  const demoBrowserListeners = new Set<(event: BrowserEventDto) => void>();
  let demoWebAuthnAnswer: {id: string; credentialId?: string} | null = null;
  (window as unknown as {
    polymuxDemoRequestPasskey?: (tabId: string) => void;
    polymuxDemoPasskeyAnswer?: () => {id: string; credentialId?: string} | null;
  }).polymuxDemoRequestPasskey = (tabId) => {
    const event: BrowserEventDto = {
      type: 'webauthn',
      prompt: {
        id: 'demo-passkey-prompt',
        tabId,
        relyingPartyId: 'github.com',
        accounts: [
          {credentialId: 'personal-passkey', displayName: 'Carlvince', name: 'carlvince@example.com'},
          {credentialId: 'work-passkey', displayName: 'Work', name: 'carlvince@work.example'},
        ],
      },
    };
    for (const listener of demoBrowserListeners) listener(event);
  };
  (window as unknown as {
    polymuxDemoPasskeyAnswer?: () => {id: string; credentialId?: string} | null;
  }).polymuxDemoPasskeyAnswer = () => demoWebAuthnAnswer;
  const timers = new Map<string, ReturnType<typeof setTimeout>>();
  const runConversations = new Map<string, string>();
  let demoProfiles: ProfileDto[] = [{id: 'default', name: 'Default Profile', isDefault: true, source: null}];
  let demoActiveProfile = 'default';
  let demoHosts: TeamHostDto[] = [
    {
      mode: 'local', state: 'local', endpoint: null, hostId: 'demo-host', desktopId: 'demo-desktop',
      deviceName: 'This Mac', deviceType: 'laptop', fingerprint: 'a22f 91bc 3780 552d', pairedAt: null, detail: null, isDefault: true,
      listeningEndpoint: 'https://connect.polymux.com/h/86c92dd5-5042-4aa4-a33f-b656bf641e28', pairingCode: 'K7M2P9X4Q',
      pairingExpiresAt: new Date(now + 5 * 60_000).toISOString(), pairedDesktopName: null,
    },
    {
      mode: 'remote', state: 'connected', endpoint: 'https://connect.polymux.com/h/3cb460aa-2ce8-49c7-a53d-fe33a1e827f4', hostId: 'demo-studio-host', desktopId: 'demo-desktop',
      deviceName: 'Studio Linux', deviceType: 'server', fingerprint: '71ad 309c 14fe a992', pairedAt: new Date(now - 86_400_000).toISOString(), detail: null, isDefault: false,
    },
  ];
  const demoTeamListeners = new Set<(members: BotDto[]) => void>();
  const demoTeamGroupListeners = new Set<(groups: TeamGroupDto[]) => void>();
  let demoBots: BotDto[] = emptyTeamPreview ? [] : [
    demoBot('maya', 'team-maya', 'Maya', 'Product researcher', '#8b5cf6', 'working', 'Comparing the latest primary sources.', now - 42_000),
    demoBot('linus', 'team-linus', 'Linus', 'Software engineer', '#3ecf8e', 'idle', 'Ready to work', now - 3_400_000),
    demoBot('sol', 'team-sol', 'Sol', 'Operations coordinator', '#f08a24', 'idle', 'Launch checklist updated.', now - 86_400_000),
  ];
  (window as unknown as {
    polymuxDemoSetTeamStatus?: (id: string, status: BotDto['status']) => void;
  }).polymuxDemoSetTeamStatus = (id, status) => {
    demoBots = demoBots.map((member) => member.id === id ? {...member, status} : member);
    demoTeamListeners.forEach((listener) => listener(structuredClone(demoBots)));
  };
  let demoTeamGroups: TeamGroupDto[] = emptyTeamPreview ? [] : [{
    id: 'launch-room',
    conversationId: 'team-group-launch',
    name: 'Launch room',
    memberIds: ['maya', 'linus', 'sol'],
    preview: 'Two claims still need primary sources.',
    updatedAt: new Date(now - 24_000).toISOString(),
    unread: true,
    unreadCount: 2,
  }];
  messages.set('team-maya', [
    message('team-peer-demo', 'team-maya', 'tool', 'Please compare the onboarding findings with the current product brief.', now - 90_000, null, {
      agentRelay: {
        source: {kind: 'assistant', memberId: null, conversationId: 'research', name: 'Research notes', role: null, avatar: null, traceId: 'demo-trace', hop: 0, automatic: false},
        destination: {kind: 'team', memberId: 'maya', conversationId: 'team-maya', name: 'Maya'},
        deliveredAt: new Date(now - 90_000).toISOString(),
      },
    } as unknown as JsonValue),
    message('team-maya-reply', 'team-maya', 'assistant', [{type: 'text', text: 'I’m checking the claims against primary sources and will flag anything the brief overstates.'}], now - 42_000),
  ]);
  messages.set('team-group-launch', [
    message('team-group-question', 'team-group-launch', 'user', 'What is still blocking launch?', now - 120_000),
    message('team-group-maya', 'team-group-launch', 'tool', 'Two claims still need primary sources. I’m checking both now.', now - 55_000, null, {
      agentRelay: {
        source: {kind: 'team', memberId: 'maya', conversationId: 'team-maya', name: 'Maya', role: 'Product researcher', avatar: demoBots.find((member) => member.id === 'maya')?.avatar ?? null, traceId: 'demo-group-maya', hop: 1, automatic: true},
        destination: {kind: 'assistant', memberId: null, conversationId: 'team-group-launch', name: 'Launch room'},
        deliveredAt: new Date(now - 55_000).toISOString(),
      },
    } as unknown as JsonValue),
    message('team-group-linus', 'team-group-launch', 'tool', 'The release build is green. I’m waiting on Maya’s source check before I tag it ready.', now - 24_000, null, {
      agentRelay: {
        source: {kind: 'team', memberId: 'linus', conversationId: 'team-linus', name: 'Linus', role: 'Software engineer', avatar: demoBots.find((member) => member.id === 'linus')?.avatar ?? null, traceId: 'demo-group-linus', hop: 1, automatic: true},
        destination: {kind: 'assistant', memberId: null, conversationId: 'team-group-launch', name: 'Launch room'},
        deliveredAt: new Date(now - 24_000).toISOString(),
      },
    } as unknown as JsonValue),
  ]);
  let demoAgentRuntime: AgentRuntimeDto = {kind: 'polymux', name: 'Polymux Agent'};
  const demoCompactAgentSettings: AgentSettingsDto = {
    authMethods: [{id: 'account', name: 'Sign in with agent account', description: 'Continue with the account managed by this agent.', type: 'agent', available: true}],
    authRequired: false,
    supportsLogout: true,
    configOptions: [
      {id: 'model', name: 'Model', description: 'AI model used by this agent', category: 'model', type: 'select', currentValue: 'default', options: [
        {value: 'default', name: 'Default', description: 'Use the agent recommendation'},
        {value: 'opus', name: 'Claude Opus', description: 'Most capable'},
        {value: 'sonnet', name: 'Claude Sonnet', description: 'Balanced'},
        {value: 'haiku', name: 'Claude Haiku', description: 'Fastest'},
      ], groups: []},
      {id: 'effort', name: 'Reasoning', description: 'How deeply the agent should reason', category: 'thought_level', type: 'select', currentValue: 'high', options: [
        {value: 'low', name: 'Low', description: null},
        {value: 'medium', name: 'Medium', description: null},
        {value: 'high', name: 'High', description: null},
      ], groups: []},
    ],
    providers: [],
    supportsProviders: false,
  };
  const demoPiAgentSettings: AgentSettingsDto = {
    authMethods: [{id: 'provider', name: 'Sign in with provider', description: 'Let pi complete authentication with the selected provider.', type: 'agent', available: true}],
    authRequired: false,
    supportsLogout: true,
    configOptions: [
      {id: 'model', name: 'Model', description: 'AI model used by pi', category: 'model', type: 'select', currentValue: 'anthropic/claude-sonnet-4', options: [
        {value: 'openai/gpt-5.2', name: 'GPT-5.2', description: 'OpenAI flagship model'},
        {value: 'openai/gpt-5.1-codex', name: 'GPT-5.1 Codex', description: 'Optimized for coding'},
        {value: 'openai/gpt-4.1', name: 'GPT-4.1', description: 'Fast general-purpose model'},
        {value: 'anthropic/claude-opus-4', name: 'Claude Opus 4', description: 'Most capable Anthropic model'},
        {value: 'anthropic/claude-sonnet-4', name: 'Claude Sonnet 4', description: 'Balanced Anthropic model'},
        {value: 'anthropic/claude-haiku-3.5', name: 'Claude Haiku 3.5', description: 'Fast Anthropic model'},
        {value: 'google/gemini-2.5-pro', name: 'Gemini 2.5 Pro', description: 'Google flagship model'},
        {value: 'google/gemini-2.5-flash', name: 'Gemini 2.5 Flash', description: 'Fast Google model'},
      ], groups: [
        {id: 'openai', name: 'OpenAI', options: [
          {value: 'openai/gpt-5.2', name: 'GPT-5.2', description: 'OpenAI flagship model'},
          {value: 'openai/gpt-5.1-codex', name: 'GPT-5.1 Codex', description: 'Optimized for coding'},
          {value: 'openai/gpt-4.1', name: 'GPT-4.1', description: 'Fast general-purpose model'},
        ]},
        {id: 'anthropic', name: 'Anthropic', options: [
          {value: 'anthropic/claude-opus-4', name: 'Claude Opus 4', description: 'Most capable Anthropic model'},
          {value: 'anthropic/claude-sonnet-4', name: 'Claude Sonnet 4', description: 'Balanced Anthropic model'},
          {value: 'anthropic/claude-haiku-3.5', name: 'Claude Haiku 3.5', description: 'Fast Anthropic model'},
        ]},
        {id: 'google', name: 'Google', options: [
          {value: 'google/gemini-2.5-pro', name: 'Gemini 2.5 Pro', description: 'Google flagship model'},
          {value: 'google/gemini-2.5-flash', name: 'Gemini 2.5 Flash', description: 'Fast Google model'},
        ]},
      ]},
      {id: 'effort', name: 'Reasoning', description: 'How deeply the agent should reason', category: 'thought_level', type: 'select', currentValue: 'high', options: [
        {value: 'low', name: 'Low', description: null},
        {value: 'medium', name: 'Medium', description: null},
        {value: 'high', name: 'High', description: null},
      ], groups: []},
    ],
    providers: [],
    supportsProviders: false,
  };
  let demoAgentSettings = structuredClone(demoCompactAgentSettings);
  const demoAcpRegistry = [
    {id: 'codex-acp', name: 'Codex', description: "OpenAI's coding assistant", version: '1.6.2', icon: '', installed: true, command: 'npx', args: ['-y', '@agentclientprotocol/codex-acp@1.6.2']},
    {id: 'claude-acp', name: 'Claude Agent', description: "Anthropic's Claude agent", version: '0.70.0', icon: '', installed: true, command: 'npx', args: ['-y', '@agentclientprotocol/claude-agent-acp@0.70.0']},
    {id: 'pi-acp', name: 'pi ACP', description: 'Run pi through Agent Client Protocol', version: '0.0.33', icon: '', installed: false, command: 'npx', args: ['-y', 'pi-acp@0.0.33']},
    {id: 'opencode', name: 'OpenCode', description: 'Open source coding agent', version: '1.0.0', icon: '', installed: false, command: 'npx', args: ['-y', 'opencode-ai@1.0.0', 'acp']},
    {id: 'junie', name: 'Junie', description: 'AI Coding Agent by JetBrains', version: '3032.2.0', icon: '', installed: false, command: 'junie', args: ['--acp=true']},
    {id: 'poolside', name: 'Poolside', description: "Poolside's coding agent", version: '1.0.16', icon: '', installed: false, command: 'pool', args: ['acp']},
  ];
  const demoTeamProfileOptions = (): ProfileDto[] => [
    ...demoProfiles.map((profile) => ({
      ...profile,
      agent: {kind: 'polymux' as const, id: 'polymux', name: 'Polymux'},
      teamEligible: true,
    })),
    {
      id: 'demo-claude-team',
      name: 'Claude Research',
      isDefault: false,
      source: {kind: 'external', agentId: 'claude', agentName: 'Claude Code', directory: '/Users/demo/.claude'},
      agent: {kind: 'acp', id: 'claude', name: 'Claude Code'},
      teamEligible: true,
    },
  ];
  let demoRoleOverrides: Partial<Record<ModelRole, {provider: string; id: string; reasoning?: ReasoningEffort}>> = {};
  const demoRoles = (): ModelRolesDto => {
    const assignment = (ref?: {provider: string; id: string; reasoning?: ReasoningEffort}): ModelRoleAssignmentDto | null => {
      const model = ref && demoModels.find((item) => item.provider === ref.provider && item.id === ref.id);
      if (!model) return null;
      const reasoning = model.reasoning ? ref?.reasoning : undefined;
      return {provider: model.provider, id: model.id, name: model.name, ...(reasoning ? {reasoning} : {})};
    };
    const main = demoModels.find((item) => item.selected);
    // Main's level lives in the general settings, the same place the composer
    // reads it from, so the two can never disagree.
    return {
      main: assignment(main && {provider: main.provider, id: main.id, reasoning: demoGeneral.reasoningLevel}),
      subagent: assignment(demoRoleOverrides.subagent),
      judge: assignment(demoRoleOverrides.judge),
      compaction: assignment(demoRoleOverrides.compaction),
      speech: assignment(demoRoleOverrides.speech),
      image: assignment(demoRoleOverrides.image),
      video: assignment(demoRoleOverrides.video),
    };
  };
  let demoModels: ModelDto[] = [
    {provider: 'openai', id: '~openai/gpt-5.6-terra', name: 'GPT-5.6 Terra', contextWindow: 200_000, maxOutputTokens: 32_000, reasoning: true, input: ['text', 'image'], cost: {input: 2.5, output: 15, cacheRead: .25, cacheWrite: 3.125}, selected: true, custom: false},
    {provider: 'openai', id: '~openai/gpt-5.6-sol', name: 'GPT-5.6 Sol', contextWindow: 200_000, maxOutputTokens: 32_000, reasoning: true, input: ['text', 'image'], cost: {input: 5, output: 30, cacheRead: .5, cacheWrite: 6.25}, selected: false, custom: false},
    {provider: 'anthropic', id: '~anthropic/claude-sonnet-4-5', name: 'Claude Sonnet 4.5', contextWindow: 200_000, maxOutputTokens: 16_000, reasoning: true, input: ['text', 'image'], cost: {input: 3, output: 15, cacheRead: .3, cacheWrite: 3.75}, selected: false, custom: false},
    {provider: 'anthropic', id: '~anthropic/claude-haiku-4-5', name: 'Claude Haiku 4.5', contextWindow: 200_000, maxOutputTokens: 16_000, reasoning: true, input: ['text', 'image'], cost: {input: 1, output: 5, cacheRead: .1, cacheWrite: 1.25}, selected: false, custom: false},
    {provider: 'openrouter', id: 'google/gemini-3.1-pro-preview', name: 'Google: Gemini 3.1 Pro Preview', contextWindow: 1_000_000, maxOutputTokens: 65_536, reasoning: true, input: ['text', 'image'], cost: {input: 2, output: 12, cacheRead: .2, cacheWrite: null}, selected: false, custom: false},
  ];
  /** Stands in for the models.dev catalogue, keyed the same way the main
   * process keys it: `<provider>:<id>`. */
  const demoModelMetadata: Record<string, ModelMetadataDto> = {
    'openai:~openai/gpt-5.6-terra': {description: 'Flagship reasoning model for broad agentic work.', family: 'gpt-5.6', lab: 'openai', knowledgeCutoff: '2025-11-30', releaseDate: '2026-04-02', openWeights: false, toolCall: true, structuredOutput: true, temperature: true, attachment: true, contextLimit: 200_000, outputLimit: 32_000},
    'anthropic:~anthropic/claude-sonnet-4-5': {description: 'Balanced model for coding agents and careful analysis.', family: 'claude-sonnet', lab: 'anthropic', knowledgeCutoff: '2025-08-31', releaseDate: '2026-02-17', openWeights: false, toolCall: true, structuredOutput: true, temperature: true, attachment: true, contextLimit: 200_000, outputLimit: 16_000},
    'openrouter:google/gemini-3.1-pro-preview': {description: 'Long-context multimodal preview model.', family: 'gemini-3.1', lab: 'google', releaseDate: '2026-03-05', openWeights: false, toolCall: true, contextLimit: 1_000_000, outputLimit: 65_536},
  };
  const demoKeys = new Map<string, ProviderDto['apiKeys']>(
    onboardingPreview
      ? []
      : [['openai', [{id: 'openai-key-1', label: 'sk-p••••demo', active: true, status: 'ready'}]]],
  );
  const demoProviders: ProviderDto[] = [
    {id: 'openai', name: 'OpenAI', apiKeyLabel: 'OpenAI API key', supportsOAuth: false, storedCredential: true, configured: true, source: '1 saved API key', modelCount: 2, custom: false, apiKeys: []},
    {id: 'anthropic', name: 'Anthropic', apiKeyLabel: 'Anthropic API key', supportsOAuth: true, storedCredential: false, configured: false, source: null, modelCount: 2, custom: false, apiKeys: []},
    {id: 'openrouter', name: 'OpenRouter', apiKeyLabel: 'OpenRouter API key', supportsOAuth: false, storedCredential: false, configured: false, source: null, modelCount: 1, custom: false, apiKeys: []},
    ...LOCAL_RUNTIMES.map((runtime) => ({
      id: runtime.id, name: runtime.name, baseUrl: runtime.baseUrl, apiKeyLabel: null,
      supportsOAuth: false, storedCredential: false, configured: false, source: null,
      modelCount: 0, custom: true, localRuntime: true, apiKeys: [],
    })),
  ];
  const demoSkills: SkillDto[] = [
    {name: 'documents', description: 'Create and edit document files.', source: 'polymux', filePath: '~/.polymux/skills/documents/SKILL.md', disableModelInvocation: false, allowedTools: ['read', 'write'], permissions: [], enabled: true, editable: true, instructions: 'Create and edit document files.', updatedAt: '2026-07-02T09:30:00.000Z'},
    {name: 'personal-research', description: 'Personal research workflow.', source: 'polymux', filePath: '~/.polymux/skills/personal-research/SKILL.md', disableModelInvocation: false, allowedTools: ['read'], permissions: [], enabled: true, editable: true, instructions: 'Personal research workflow.', updatedAt: '2026-05-18T14:00:00.000Z'},
    {name: 'pdf', description: 'Read, create, and edit PDF files.', source: 'official', filePath: '/skills/official/pdf/SKILL.md', disableModelInvocation: false, allowedTools: ['read', 'write', 'bash'], permissions: [], enabled: true, editable: false, displayName: 'PDF', author: 'Polymux', category: 'Documents', updatedAt: '2026-08-01T08:00:00.000Z'},
    // No core integration here: browser/GUI control and the Hub's email and
    // messaging skills are first-class surfaces and never list as add-ons.
    {name: 'spreadsheets', description: 'Create, analyze, and edit spreadsheets.', source: 'official', filePath: '/skills/official/spreadsheets/SKILL.md', disableModelInvocation: false, allowedTools: ['read', 'write', 'bash'], permissions: [], enabled: true, editable: false, displayName: 'Spreadsheets', author: 'Polymux', category: 'Documents', updatedAt: '2026-08-01T08:00:00.000Z'},
  ];
  const demoPlugins: PluginDto[] = [
    {
      id: 'claude-code/code-review',
      name: 'code-review',
      description: 'Automated code review for pull requests using several specialised agents.',
      version: '1.0.0',
      author: 'Anthropic',
      marketplace: 'claude-code',
      marketplaceName: 'claude-code-plugins',
      directory: '~/.polymux/plugins/claude-code/code-review',
      enabled: true,
      contributions: {skills: ['review-diff'], mcpServers: [], views: [], commands: 1, agents: 3, hooks: 0},
      conflicts: [],
    },
    {
      id: 'claude-code/pdf-tools',
      name: 'pdf-tools',
      description: 'Read and rewrite PDFs from a chat.',
      version: '0.4.2',
      author: 'Anthropic',
      marketplace: 'claude-code',
      marketplaceName: 'claude-code-plugins',
      directory: '~/.polymux/plugins/claude-code/pdf-tools',
      enabled: false,
      contributions: {skills: ['pdf'], mcpServers: ['pdf-server'], views: [], commands: 0, agents: 0, hooks: 2},
      // A name the demo's own skills already carry, so the warning has
      // something true to point at.
      conflicts: [{kind: 'skill', name: 'pdf', existingSource: 'official'}],
    },
  ];
  let demoWorkspaceApps: WorkspaceAppsDto = {
    apps: [
      {id: 'hub', name: 'Hub', description: 'Messages and email across connected accounts.', official: true, enabled: true, workspaceKind: 'hub', settingsKind: 'hub', entry: null, pinnable: true},
      {id: 'drive', name: 'Drive', description: 'Files from this computer and connected storage.', official: true, enabled: true, workspaceKind: 'drive', settingsKind: 'drive', entry: null, pinnable: true},
      {id: 'media', name: 'Media', description: 'Photos and videos.', official: true, enabled: true, workspaceKind: 'media', settingsKind: null, entry: null, pinnable: true},
      {id: 'tasks', name: 'Tasks', description: 'Tasks created and managed by you and your agent.', official: true, enabled: true, workspaceKind: 'tasks', settingsKind: null, entry: null, pinnable: true},
      {id: 'calendar', name: 'Calendar', description: 'Events and availability from connected calendars.', official: true, enabled: true, workspaceKind: 'calendar', settingsKind: null, entry: null, pinnable: true},
      {id: 'phone', name: 'Phone', description: 'Your connected Android or iPhone screen.', official: true, enabled: true, workspaceKind: 'phone', settingsKind: null, entry: null, pinnable: true},
      {id: 'locker', name: 'Locker', description: 'Passwords, authenticator codes, recovery codes and passkeys.', official: true, enabled: true, workspaceKind: 'locker', settingsKind: null, entry: null, pinnable: true},
      {id: 'terminal', name: 'Terminal', description: 'A command line on this computer.', official: true, enabled: true, workspaceKind: 'terminal', settingsKind: null, entry: null, pinnable: true},
      {id: 'ide', name: 'IDE', description: 'A project folder, the file in front of you, and a terminal.', official: true, enabled: true, workspaceKind: 'ide', settingsKind: null, entry: null, pinnable: true},
      {id: 'finance', name: 'Finance', description: 'Bank accounts and agent payments.', official: true, enabled: true, workspaceKind: 'finance', settingsKind: null, entry: null, pinnable: true},
      {id: 'usage', name: 'Usage', description: 'Tokens, API-equivalent spend, and activity over time.', official: true, enabled: true, workspaceKind: 'usage', settingsKind: null, entry: null, pinnable: true},
    ],
    pinnedIds: ['drive', 'calendar', 'hub', 'tasks'],
  };
  type DemoLockerEntry = {
    id: string;
    title: string;
    username: string;
    url: string;
    notes: string;
    groupName: string;
    password: string;
    totpSecret: string;
    recoveryCodes: string[];
    passkey: {
      relyingParty: string;
      username: string;
      credentialId: string;
      userHandle: string;
      privateKeyPem: string;
    } | null;
    pinned: boolean;
    sortIndex: number;
    trashed: boolean;
    updatedAt: string;
  };
  let demoLockerExists = false;
  let demoLockerUnlocked = false;
  let demoLockerMaster = '';
  let demoLockerStorage: import('@polymux/protocol').LockerStorageMode = 'account';
  const demoLockerItems: DemoLockerEntry[] = [];
  const demoLockerListeners = new Set<(status: import('@polymux/protocol').LockerStatusDto) => void>();
  const demoLockerStatus = (): import('@polymux/protocol').LockerStatusDto => ({
    exists: demoLockerExists,
    unlocked: demoLockerUnlocked,
    itemCount: demoLockerItems.filter((item) => !item.trashed).length,
    idleLockSeconds: 300,
    sync: {
      signedIn: false,
      available: false,
      state: demoLockerStorage === 'local' ? 'local' : 'offline',
      storage: demoLockerStorage,
      revision: 0,
      lastSyncedAt: null,
    },
  });
  const notifyDemoLocker = (): void => {
    const status = demoLockerStatus();
    for (const listener of demoLockerListeners) listener(status);
  };
  const demoUsageStats = (): UsageStatsDto => {
    const origin = new Date();
    origin.setHours(0, 0, 0, 0);
    const start = new Date(origin);
    start.setDate(start.getDate() - 52 * 7);
    start.setDate(start.getDate() - start.getDay());
    const days: UsageStatsDto['days'] = [];
    let lifetimeTokens = 0;
    let peakTokens = 0;
    let costUsd = 0;
    let runs = 0;
    for (const cursor = new Date(start); cursor.getTime() <= origin.getTime(); cursor.setDate(cursor.getDate() + 1)) {
      const daysAgo = Math.round((origin.getTime() - cursor.getTime()) / 86_400_000);
      const seed = cursor.getFullYear() * 10_000 + (cursor.getMonth() + 1) * 100 + cursor.getDate();
      const pulse = seed % 10;
      const recent = daysAgo < 78;
      const quiet = (!recent && pulse < 7) || (cursor.getDay() === 0 && pulse < 5);
      const tokens = quiet ? 0 : (recent ? 90_000 : 12_000) + pulse * (recent ? 55_000 : 4_000);
      const dayRuns = tokens ? 1 + (pulse % 4) : 0;
      const spend = tokens * 0.0000024;
      lifetimeTokens += tokens;
      costUsd += spend;
      runs += dayRuns;
      if (tokens > peakTokens) peakTokens = tokens;
      const year = cursor.getFullYear();
      const month = String(cursor.getMonth() + 1).padStart(2, '0');
      const day = String(cursor.getDate()).padStart(2, '0');
      days.push({date: `${year}-${month}-${day}`, tokens, costUsd: spend, runs: dayRuns});
    }
    return {
      identity: {name: 'Polymux', handle: '@polymux', avatarUrl: null, badge: null},
      lifetimeTokens,
      peakTokens,
      costUsd,
      longestChatMs: 7 * 3_600_000 + 26 * 60_000,
      currentStreakDays: 18,
      longestStreakDays: 49,
      days,
      fastModePercent: 16,
      reasoningPercent: 41,
      skillsExplored: 141,
      skillsUsed: 18_941,
      totalChats: Math.max(runs, 128),
      plugins: [
        {name: 'background-gui', count: 2187},
        {name: 'window-control', count: 1577},
        {name: 'communication', count: 1430},
        {name: 'email-use', count: 1254},
        {name: 'control', count: 1163},
      ],
      connections: [
        {name: 'GitHub', count: 842},
        {name: 'Linear', count: 311},
        {name: 'Notion', count: 188},
      ],
      models: [
        {model: 'anthropic/claude-sonnet-4', tokens: Math.round(lifetimeTokens * 0.62), costUsd: costUsd * 0.7, runs: Math.round(runs * 0.6)},
        {model: 'openai/gpt-5', tokens: Math.round(lifetimeTokens * 0.38), costUsd: costUsd * 0.3, runs: Math.round(runs * 0.4)},
      ],
      agents: [
        {id: 'polymux', kind: 'polymux', name: 'Polymux', tokens: lifetimeTokens * .6, costUsd: costUsd * .6, runs: runs * .6, chats: 60},
        {id: 'acp:claude', kind: 'acp', name: 'Claude Code', tokens: lifetimeTokens * .2, costUsd: costUsd * .2, runs: runs * .2, chats: 20},
        {id: 'acp:codex', kind: 'acp', name: 'Codex', tokens: lifetimeTokens * .2, costUsd: costUsd * .2, runs: runs * .2, chats: 20},
      ],
      agentId: null,
      scope: 'all',
      spendIncomplete: false,
    };
  };
  const demoLockerList = (): import('@polymux/protocol').LockerListDto => ({
    groups: [{id: 'general', name: 'Locker', parentId: null}],
    items: demoLockerItems
      .filter((item) => !item.trashed)
      .sort((a, b) => Number(b.pinned) - Number(a.pinned) || a.sortIndex - b.sortIndex || a.title.localeCompare(b.title))
      .map(demoLockerSummary),
    trash: demoLockerItems.filter((item) => item.trashed).map(demoLockerSummary),
  });
  const demoLockerSummary = (item: DemoLockerEntry): import('@polymux/protocol').LockerItemDto => ({
    id: item.id,
    title: item.title,
    username: item.username,
    url: item.url,
    notes: item.notes,
    groupId: 'general',
    groupName: item.groupName || 'Locker',
    hasPassword: item.password.length > 0,
    hasTotp: item.totpSecret.length > 0,
    hasRecoveryCodes: item.recoveryCodes.length > 0,
    hasPasskey: Boolean(item.passkey),
    pinned: item.pinned,
    sortIndex: item.sortIndex,
    updatedAt: item.updatedAt,
  });
  const demoTotp = (secret: string): import('@polymux/protocol').LockerTotpDto => {
    const period = 30;
    const remaining = period - (Math.floor(Date.now() / 1000) % period);
    const digits = String(Math.floor(Date.now() / 1000 / period) % 1_000_000).padStart(6, '0');
    const next = String((Math.floor(Date.now() / 1000 / period) + 1) % 1_000_000).padStart(6, '0');
    return {code: digits, next, period, remaining, issuer: '', account: secret ? 'demo' : ''};
  };
  const demoMarketplaces: PluginMarketplaceDto[] = [
    {id: 'claude-code', name: 'claude-code-plugins', source: 'anthropics/claude-code', pluginCount: 3, builtin: true},
  ];
  const demoCatalog: MarketplacePluginDto[] = [
    {id: 'claude-code/code-review', name: 'code-review', description: 'Automated code review for pull requests.', version: '1.0.0', author: 'Anthropic', installed: true},
    {id: 'claude-code/pdf-tools', name: 'pdf-tools', description: 'Read and rewrite PDFs from a chat.', version: '0.4.2', author: 'Anthropic', installed: true},
    {id: 'claude-code/commit-commands', name: 'commit-commands', description: 'Commit workflows, written as commands.', version: '1.1.0', author: 'Anthropic', installed: false},
  ];
  const demoDiscoveredSkills: DiscoveredSkillGroupDto[] = [
    {id: 'claude', label: 'Claude', directory: '~/.claude/skills', skills: [
      {name: 'commit-writer', description: 'Write commit messages from a diff.', path: '~/.claude/skills/commit-writer', state: 'available'},
      {name: 'pdf', description: 'Read, create, and edit PDF files.', path: '~/.claude/skills/pdf', state: 'loaded'},
    ]},
    {id: 'codex', label: 'Codex', directory: '~/.codex/skills', skills: [
      {name: 'repo-map', description: 'Summarize an unfamiliar repository.', path: '~/.codex/skills/repo-map', state: 'available'},
    ]},
    {id: 'agents', label: 'Shared skills', directory: '~/.agents/skills', skills: [
      {name: 'find-skills', description: 'Search the skills directory.', path: '~/.agents/skills/find-skills', state: 'loaded'},
    ]},
  ];
  const demoDiscoveredMcp: DiscoveredMcpGroupDto[] = [
    {id: 'pi:mcp.json', label: 'Pi', path: '~/.pi/agent/mcp.json', servers: [
      {id: 'filesystem', name: 'Filesystem', description: 'Access local files and directories.', transport: 'stdio', target: 'node', source: 'pi', path: '~/.pi/agent/mcp.json', state: 'loaded'},
      {id: 'linear', name: 'Linear', transport: 'streamable-http', target: 'https://mcp.linear.app/sse', source: 'pi', path: '~/.pi/agent/mcp.json', state: 'available'},
    ]},
  ];
  const demoMcpServers: McpServerDto[] = [
    {id: 'filesystem', name: 'Filesystem', description: 'Access local files and directories.', source: 'polymux', editable: true, enabled: true, transport: 'stdio', status: 'connected', toolNames: ['list_files'], resourceUris: ['filesystem://documents'], promptNames: [], command: 'node', args: ['server.mjs']},
    {id: 'github', name: 'GitHub', description: 'Read repositories, issues, and pull requests.', source: 'official', editable: false, enabled: true, transport: 'stdio', status: 'connected', toolNames: ['list_issues', 'get_pull_request'], resourceUris: [], promptNames: [], command: 'node', args: ['github.mjs']},
  ];
  const demoCommsStatus: CommsStatusDto = {
    hub: {
      baseUrl: 'http://127.0.0.1:18080',
      homeserverUrl: 'http://127.0.0.1:8008',
      canAutoConnect: onboardingPreview,
      directory: '~/Library/Application Support/matrix-hub',
      status: onboardingPreview ? 'reachable' : 'signed-in',
      userId: onboardingPreview ? null : '@demo:localhost',
      homeserverName: 'localhost',
      error: null,
    },
    bridges: [
      {platform: 'whatsapp', name: 'WhatsApp', api: 'bridgev2', state: onboardingPreview ? 'logged-out' : 'connected', accounts: onboardingPreview ? [] : [{id: 'wa1', name: '+61 400 000 000', state: 'connected', error: null}], flows: onboardingPreview ? [{id: 'qr', name: 'QR Code', description: 'Scan a QR code to pair the bridge to your WhatsApp account'}, {id: 'phone', name: 'Pairing code', description: 'Enter your phone number and type the code WhatsApp shows into your phone'}] : [], setup: null, managementRoomHint: null, error: null},
      {platform: 'telegram', name: 'Telegram', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'phone', name: 'Phone Number', description: 'Login using your Telegram phone number'}, {id: 'qr', name: 'QR Code', description: 'Login by scanning a QR code from your phone'}, {id: 'bot', name: 'Bot token', description: 'Bots only · Uses a token from BotFather and does not act as your personal account'}, {id: 'manual', name: 'Manual', description: 'Advanced · Existing session credentials; the bridge recommends not using this method'}], setup: {fields: [{id: 'api_id', name: 'API ID', description: 'The numeric ID of your Telegram application.', helpUrl: 'https://my.telegram.org/apps', secret: false}, {id: 'api_hash', name: 'API hash', description: 'The hash shown next to it.', helpUrl: 'https://my.telegram.org/apps', secret: true}], configured: false}, managementRoomHint: null, error: null},
      {platform: 'signal', name: 'Signal', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'qr', name: 'QR Code', description: 'Link this Mac as a Signal device by scanning a QR code'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'slack', name: 'Slack', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'token', name: 'Auth token & cookie', description: 'Personal account · Browser tokens require the matching cookie'}, {id: 'app', name: 'Slack app', description: 'Workspace app · Limited to channels and permissions granted to the app'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'googlechat', name: 'Google Chat', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'cookies', name: 'Google login', description: 'Sign in to your Google account'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'gmessages', name: 'Google Messages', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'google', name: 'Google Account', description: 'Pair with your Google account by matching the emoji shown on your phone'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'twitter', name: 'X', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'cookies', name: 'x.com', description: 'Login using cookies from x.com'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'bluesky', name: 'Bluesky', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'password', name: 'App password', description: 'Sign in with a Bluesky app password'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'gvoice', name: 'Google Voice', api: 'bridgev2', state: 'unreachable', accounts: [], flows: [], setup: null, managementRoomHint: null, error: 'The Google Voice bridge can’t be installed.'},
      {platform: 'zulip', name: 'Zulip', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'apitoken', name: 'API token', description: 'Login with your Zulip email and API token'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'messenger', name: 'Messenger', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'messenger', name: 'messenger.com', description: 'Login using cookies from messenger.com'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'instagram', name: 'Instagram', api: 'bridgev2', state: 'connected', accounts: [{id: 'ig1', name: '@carl.builds', avatarUrl: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==', state: 'connected', error: null}, {id: 'ig2', name: '@polymux', avatarUrl: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7', state: 'connected', error: null}], flows: [{id: 'instagram', name: 'instagram.com', description: 'Login using cookies from instagram.com'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'linkedin', name: 'LinkedIn', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'cookies', name: 'Cookies', description: 'Log in with your LinkedIn account using your cookies'}], setup: null, managementRoomHint: null, error: null},
      {platform: 'imessage', name: 'iMessage', api: 'bridgev2', state: 'logged-out', accounts: [], flows: [{id: 'local', name: 'This Mac', description: 'Read the Messages database on this Mac'}], setup: null, managementRoomHint: null, error: null},
      // No bridge to log in to: a relay against the WeChat app on this Mac,
      // reporting whichever account that app is already signed in as — and
      // only once it is delivering into Polymux's own hub.
      {platform: 'wechat', name: 'WeChat', api: 'none', state: 'unavailable', accounts: [], flows: [], setup: null, managementRoomHint: null, error: weChatMissingPreview ? 'WeChat for Mac is not installed. Install it and sign in.' : 'The WeChat relay on this Mac is running, but it is not connected to Polymux’s own hub, so nothing it carries reaches here.', installUrl: weChatMissingPreview ? 'https://mac.weixin.qq.com/en' : null},
    ],
    email: {
      // The demo shows the buttons: it stands in for a build with clients
      // registered, which is what the Hub tab looks like once they are.
      signInProviders: ['google', 'microsoft'],
      accounts: onboardingPreview
        ? []
        : [
            {id: 'personal', displayName: 'Demo User', email: 'demo@example.com', incoming: {kind: 'imap', host: 'imap.gmail.com', port: 993, encryption: 'tls', login: 'demo@example.com', auth: 'command'}, outgoing: {kind: 'smtp', host: 'smtp.gmail.com', port: 587, encryption: 'start-tls', login: 'demo@example.com', auth: 'command'}, secretStored: true, signatures: [{id: 'personal-default', name: 'Personal', body: 'Kind regards,\nDemo User', html: '<b>Kind regards,</b><br>Demo User'}], defaultSignatureId: 'personal-default', status: 'ok', error: null},
            {id: 'work', displayName: 'Demo At Work', email: 'demo@work.example', incoming: {kind: 'imap', host: 'outlook.office365.com', port: 993, encryption: 'tls', login: 'demo@work.example', auth: 'oauth2'}, outgoing: {kind: 'smtp', host: 'smtp.office365.com', port: 587, encryption: 'start-tls', login: 'demo@work.example', auth: 'oauth2'}, secretStored: false, signatures: [{id: 'work-default', name: 'Work', body: 'Best,\nDemo User\nProduct Engineering', html: null}], defaultSignatureId: 'work-default', status: 'unknown', error: null},
            {id: 'team', displayName: null, email: 'team@example.co', incoming: {kind: 'imap', host: 'imap.larksuite.com', port: 993, encryption: 'tls', login: 'team@example.co', auth: 'command'}, outgoing: {kind: 'smtp', host: 'smtp.larksuite.com', port: 465, encryption: 'tls', login: 'team@example.co', auth: 'command'}, secretStored: true, signatures: [], defaultSignatureId: null, status: 'error', error: 'authentication failed'},
          ],
    },
  };
  /** Logins the user backed out of; their pending waits must go nowhere. */
  let demoLoginCancelled = 0;
  /** A finished login has to show up in the ring, or the demo ends on a lie. */
  const demoMarkLinked = (platform: string): void => {
    demoCommsStatus.bridges = demoCommsStatus.bridges.map((bridge) =>
      bridge.platform === platform
        ? {
            ...bridge,
            state: 'connected',
            accounts: [
              ...bridge.accounts,
              {id: `demo-${platform}`, name: 'Demo account', state: 'connected', error: null},
            ],
          }
        : bridge,
    );
  };
  const demoCommsListeners = new Set<(status: CommsStatusDto) => void>();
  let demoWeChatLogin: import('@polymux/protocol').WeChatLoginDto = {
    state: 'unavailable', qrDataUrl: null, expiresAt: null, optionsReady: false,
  };
  (window as unknown as {polymuxDemoWeChatLogin: (value: import('@polymux/protocol').WeChatLoginDto) => void})
    .polymuxDemoWeChatLogin = value => { demoWeChatLogin = value; };
  (window as unknown as {
    polymuxDemoWeChatAttention: (attention: {title: string; detail: string; installUrl?: string; retry?: boolean} | null, connected?: boolean) => void;
  }).polymuxDemoWeChatAttention = (attention, connected = true) => {
    demoCommsStatus.bridges = demoCommsStatus.bridges.map(bridge => bridge.platform === 'wechat'
      ? {...bridge, attention, state: connected ? 'connected' : 'unavailable'} : bridge);
    for (const listener of demoCommsListeners) listener(structuredClone(demoCommsStatus));
  };
  const demoWakeCalls: CommsPlatform[] = [];
  let demoChatReads = 0;
  let demoChatPickGate: Promise<void> | null = null;
  /** Test-only sticker catalog. Real accounts observe this from the bridge. */
  let demoStickers: ChatStickerDto[] | null = null;
  let demoStickerGate: Promise<void> | null = null;
  let releaseDemoChatPick: (() => void) | null = null;
  let demoWeChatWakeGate: Promise<void> | null = null;
  let releaseDemoWeChatWake: (() => void) | null = null;
  let demoWeChatWakeReady = true;
  (window as unknown as {
    polymuxDemoWakeCalls?: () => CommsPlatform[];
  }).polymuxDemoWakeCalls = () => [...demoWakeCalls];
  (window as unknown as {
    polymuxDemoChatReads?: () => number;
  }).polymuxDemoChatReads = () => demoChatReads;
  (window as unknown as {
    polymuxDemoHoldChatPick?: () => void;
    polymuxDemoReleaseChatPick?: () => void;
  }).polymuxDemoHoldChatPick = () => {
    if (demoChatPickGate) return;
    demoChatPickGate = new Promise<void>((resolve) => {
      releaseDemoChatPick = resolve;
    });
  };
  (window as unknown as {
    polymuxDemoReleaseChatPick?: () => void;
  }).polymuxDemoReleaseChatPick = () => {
    releaseDemoChatPick?.();
    releaseDemoChatPick = null;
    demoChatPickGate = null;
  };
  (window as unknown as {
    polymuxDemoHoldWeChatWake?: () => void;
    polymuxDemoReleaseWeChatWake?: () => void;
  }).polymuxDemoHoldWeChatWake = () => {
    if (demoWeChatWakeGate) return;
    demoWeChatWakeGate = new Promise<void>((resolve) => {
      releaseDemoWeChatWake = resolve;
    });
  };
  (window as unknown as {
    polymuxDemoReleaseWeChatWake?: () => void;
  }).polymuxDemoReleaseWeChatWake = () => {
    releaseDemoWeChatWake?.();
    releaseDemoWeChatWake = null;
    demoWeChatWakeGate = null;
  };
  (window as unknown as {
    polymuxDemoSetWeChatWakeReady?: (ready: boolean) => void;
  }).polymuxDemoSetWeChatWakeReady = (ready) => {
    demoWeChatWakeReady = ready;
  };
  /** Test-only status push: it follows the same subscription seam used when a
   * second Polymux window changes an account in Settings. */
  (window as unknown as {
    polymuxDemoSetPlatformLinked?: (platform: CommsPlatform, linked: boolean) => void;
  }).polymuxDemoSetPlatformLinked = (platform, linked) => {
    demoCommsStatus.bridges = demoCommsStatus.bridges.map((bridge) => {
      if (bridge.platform !== platform) return bridge;
      return linked
        ? {
            ...bridge,
            state: 'connected',
            accounts: bridge.accounts.length > 0
              ? bridge.accounts
              : [{id: `demo-${platform}`, name: 'Demo account', state: 'connected', error: null}],
          }
        : {...bridge, state: 'logged-out', accounts: []};
    });
    for (const listener of demoCommsListeners) listener(structuredClone(demoCommsStatus));
  };
  // Newest first with unread counts, the way the hub now returns them.
  const demoRevealListeners = new Set<(request: WorkspaceRevealDto) => void>();
  (window as unknown as {polymuxDemoReveal?: (request: WorkspaceRevealDto) => void}).polymuxDemoReveal =
    (request) => demoRevealListeners.forEach((listener) => listener(request));
  let demoChats: ChatDto[] = [
    {id: '!wx-filehelper:local', name: 'File Transfer', platform: 'wechat', unread: 0, lastActivity: new Date(now - 1_800_000).toISOString(), preview: 'Project notes.pdf', group: false, avatarUrl: null},
    {id: '!wa-default-space:local', name: 'WhatsApp (+61426982339)', platform: 'whatsapp', accountIds: ['wa1'], unread: 0, lastActivity: null, preview: null, group: true, space: true, defaultSpace: true, parentIds: [], avatarUrl: null},
    {id: '!wa-jules:local', name: 'Jules Tan', platform: 'whatsapp', accountIds: ['wa1'], unreadByAccount: {wa1: 0}, unread: 0, lastActivity: new Date(now - 3_500_000).toISOString(), preview: 'Yes — 2pm works.', group: false, parentIds: ['!wa-default-space:local'], avatarUrl: null},
    {id: '!wa-phone:local', name: '+12262184662', platform: 'whatsapp', accountIds: ['wa1'], unreadByAccount: {wa1: 0}, unread: 0, lastActivity: new Date(now - 3_700_000).toISOString(), preview: null, group: false, parentIds: ['!wa-default-space:local'], avatarUrl: null},
    {id: '!ig-carl:local', name: 'Carl’s chat', platform: 'instagram', accountIds: ['ig1'], unread: 0, lastActivity: new Date(now - 4_000_000).toISOString(), preview: 'Personal account', group: false, avatarUrl: null},
    {id: '!ig-polymux:local', name: 'Polymux chat', platform: 'instagram', accountIds: ['ig2'], unread: 0, lastActivity: new Date(now - 4_100_000).toISOString(), preview: 'Project account', group: false, avatarUrl: null},
    {id: '!ig-project-space:local', name: 'Polymux community', platform: 'instagram', accountIds: ['ig2'], unread: 0, lastActivity: null, preview: null, group: true, space: true, parentIds: [], avatarUrl: null},
    {id: '!ig-project-news:local', name: 'Project updates', platform: 'instagram', accountIds: ['ig2'], unread: 0, lastActivity: new Date(now - 4_200_000).toISOString(), preview: 'Latest build notes', group: true, parentIds: ['!ig-project-space:local'], avatarUrl: null},
    {id: '!tg-devs:local', name: 'Dev Chat', platform: 'telegram', unread: 0, lastActivity: new Date(now - 7_200_000).toISOString(), preview: 'Shipped the build, logs look clean.', group: true, avatarUrl: null},
    {id: '!wa-family:local', name: 'Family', platform: 'whatsapp', accountIds: ['wa1'], unreadByAccount: {wa1: 2}, unread: 2, lastActivity: new Date(now - 86_400_000).toISOString(), preview: 'Dinner Sunday?', group: true, parentIds: ['!wa-default-space:local'], avatarUrl: null},
    {id: '!wa-nus-space:local', name: 'NUS exchange students AY26/27', platform: 'whatsapp', accountIds: ['wa1'], unread: 0, lastActivity: null, preview: null, group: true, space: true, parentIds: ['!wa-default-space:local'], avatarUrl: null},
    {id: '!wa-nus-social:local', name: 'Social 💃', platform: 'whatsapp', accountIds: ['wa1'], unreadByAccount: {wa1: 1}, unread: 1, lastActivity: new Date(now - 5_400_000).toISOString(), preview: 'Dinner after class?', group: true, parentIds: ['!wa-default-space:local', '!wa-nus-space:local'], avatarUrl: null},
    {id: '!wa-nus-soc:local', name: 'School of Computing', platform: 'whatsapp', accountIds: ['wa1'], unreadByAccount: {wa1: 0}, unread: 0, lastActivity: new Date(now - 10_800_000).toISOString(), preview: 'Tutorial group list', group: true, parentIds: ['!wa-default-space:local', '!wa-nus-space:local'], avatarUrl: null},
    {id: '!wa-nus-running:local', name: 'Running 👟', platform: 'whatsapp', accountIds: ['wa1'], unreadByAccount: {wa1: 0}, unread: 0, lastActivity: new Date(now - 14_400_000).toISOString(), preview: 'Saturday, 8am at UTown', group: true, parentIds: ['!wa-default-space:local', '!wa-nus-space:local'], avatarUrl: null},
  ];
  let demoWeChatGroup = {name: 'Study group', isMember: true, renameError: '', renameDelayMs: 0, readDelayMs: 0};
  (window as unknown as {
    polymuxDemoSetStickers?: (stickers: ChatStickerDto[] | null) => void;
    polymuxDemoSetStickerGate?: (gate: Promise<void> | null) => void;
  }).polymuxDemoSetStickers = stickers => {
    demoStickers = stickers;
  };
  (window as unknown as {
    polymuxDemoSetStickerGate?: (gate: Promise<void> | null) => void;
  }).polymuxDemoSetStickerGate = gate => {
    demoStickerGate = gate;
  };
  (window as unknown as {
    polymuxDemoSetWeChatGroup?: (settings: Partial<typeof demoWeChatGroup>) => void;
  }).polymuxDemoSetWeChatGroup = settings => {
    demoWeChatGroup = {...demoWeChatGroup, ...settings};
    const existing = demoChats.find(chat => chat.id === '!wx-group:local');
    if (existing) existing.name = demoWeChatGroup.name;
    else demoChats.push({id: '!wx-group:local', name: demoWeChatGroup.name, platform: 'wechat',
      group: true, avatarUrl: null, unread: 0, lastActivity: new Date(now).toISOString(), preview: ''});
  };
  /** Test-only stand-in for a read marker changed by a native platform. It
   * deliberately emits no Hub activity: remote reads have no new message to
   * push, so the ordinary focused refresh has to discover them. */
  (window as unknown as {
    polymuxDemoSetChatUnread?: (chatId: string, unread: number) => void;
  }).polymuxDemoSetChatUnread = (chatId, unread) => {
    demoChats = demoChats.map((chat) => {
      if (chat.id !== chatId) return chat;
      const unreadByAccount = chat.unreadByAccount
        ? Object.fromEntries(Object.keys(chat.unreadByAccount).map((account) => [account, unread]))
        : undefined;
      return {...chat, unread, ...(unreadByAccount ? {unreadByAccount} : {})};
    });
  };
  // A real, short vertical WebM behind an .mp4 name. That mismatch reproduces
  // the generic-file route used by some reel shares while still letting the
  // headless browser prove the inline player receives playable bytes.
  const demoReelUrl = 'data:video/webm;base64,GkXfo59ChoEBQveBAULygQRC84EIQoKEd2VibUKHgQJChYECGFOAZwEAAAAAAAJeEU2bdLpNu4tTq4QVSalmU6yBoU27i1OrhBZUrmtTrIHYTbuMU6uEElTDZ1OsggElTbuMU6uEHFO7a1OsggJI7AEAAAAAAABZAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAVSalmsirXsYMPQkBNgI1MYXZmNjIuMTIuMTAyV0GNTGF2ZjYyLjEyLjEwMkSJiEBpAAAAAAAAFlSua8iuAQAAAAAAAD/XgQFzxYisFaVyoEewLpyBACK1nIN1bmSIgQCGhVZfVlA5g4EBI+ODhAJiWgDgkLCBELqBHJqBAlWwhFW5gQESVMNnQIBzc6BjwIBnyJpFo4dFTkNPREVSRIeNTGF2ZjYyLjEyLjEwMnNz2mPAi2PFiKwVpXKgR7AuZ8ilRaOHRU5DT0RFUkSHmExhdmM2Mi4yOC4xMDIgbGlidnB4LXZwOWfIoUWjiERVUkFUSU9ORIeTMDA6MDA6MDAuMjAwMDAwMDAwAB9DtnVAl+eBAKO+gQAAgIJJg0IAAPABtgY4JBwYSgAAIEAAMV///5V29t/0rJIV6+83T8qAkchIzbj8ppDSUIBEwUeNuAQOsACjk4EAKACGAECSnEhQAAADcAAAUuKjk4EAUACGAECSnEBO4AADcAAAUuKjk4EAeACGAECSnEhQAAADcAAAUuKjk4EAoACGAECSnDhNQAADcAAAUuIcU7trkbuPs4EAt4r3gQHxggGr8IED';
  // A real, short Opus voice note. Keeping playable bytes in the browser demo
  // lets the custom message control prove playback rather than only its paint.
  const demoVoiceUrl = 'data:audio/webm;base64,GkXfo59ChoEBQveBAULygQRC84EIQoKEd2VibUKHgQRChYECGFOAZwEAAAAAAALsEU2bdLpNu4tTq4QVSalmU6yBoU27i1OrhBZUrmtTrIHYTbuMU6uEElTDZ1OsggFCTbuMU6uEHFO7a1OsggLW7AEAAAAAAABZAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAVSalmsirXsYMPQkBNgI1MYXZmNjIuMTIuMTAyV0GNTGF2ZjYyLjEyLjEwMkSJiEB5gAAAAAAAFlSua+WuAQAAAAAAAFzXgQFzxYhJcb6Va8TQLJyBACK1nIN1bmSIgQCGhkFfT1BVU1aqg2MuoFa7hATEtACDgQLhkZ+BAbWIQL9AAAAAAABiZIEQY6KTT3B1c0hlYWQBATgBQB8AAAAAABJUw2f9c3OgY8CAZ8iaRaOHRU5DT0RFUkSHjUxhdmY2Mi4xMi4xMDJzc9djwItjxYhJcb6Va8TQLGfIokWjh0VOQ09ERVJEh5VMYXZjNjIuMjguMTAyIGxpYm9wdXNnyKFFo4hEVVJBVElPTkSHkzAwOjAwOjAwLjQwODAwMDAwMAAfQ7Z1QQzngQCji4EAAIAIC+Y7I6tgo4qBABWACAissw7Go4qBACmACAissw7Go4qBAD2ACAissw7Go4qBAFGACAissw7Go4qBAGWACAissw7Go4qBAHmACAissw7Go4qBAI2ACAissw7Go4qBAKGACAissw7Go4qBALWACAissw7Go4qBAMmACAissw7Go4qBAN2ACAissw7Go4qBAPGACAissw7Go4qBAQWACAissw7Go4qBARmACAissw7Go4qBAS2ACAissw7Go4qBAUGACAissw7Go4qBAVWACAissw7Go4qBAWmACAissw7Go4qBAX2ACAissw7GoJahioEBkQAICKyzDsabgQd1ooQAzf5gHFO7a5G7j7OBALeK94EB8YIBxPCBAw==';
  let demoChatMessages: ChatMessageDto[] = [
    {id: 'wx1', chatId: '!wx-filehelper:local', sender: 'You', body: '', sentAt: new Date(now - 2_100_000).toISOString(), mine: true, attachments: [{kind: 'file', url: null, name: 'Project notes.pdf', mimeType: null, size: 1_572_864}], viewIn: {app: 'WeChat', url: 'weixin://'}},
    {id: 'wx2', chatId: '!wx-filehelper:local', sender: 'You', body: '', sentAt: new Date(now - 2_000_000).toISOString(), mine: true, attachments: [{kind: 'audio', url: demoVoiceUrl, name: 'Voice message', mimeType: 'audio/webm', size: 796, duration: .4}], viewIn: {app: 'WeChat', url: 'weixin://'}},
    {id: 'wx3', chatId: '!wx-filehelper:local', sender: 'WeChat', body: 'A message was recalled', notice: true, sentAt: new Date(now - 1_900_000).toISOString(), mine: false},
    {id: 'wx4', chatId: '!wx-filehelper:local', sender: 'You', body: 'My answer\n↳ Alice: Earlier text', sentAt: new Date(now - 1_800_000).toISOString(), mine: true, viewIn: {app: 'WeChat', url: 'weixin://'}},
    {id: 'wx5', chatId: '!wx-filehelper:local', sender: 'You', body: '', sentAt: new Date(now - 1_700_000).toISOString(), mine: true, linkPreview: {title: 'Useful article', description: 'A short description', url: 'https://example.test/article', source: 'example.test'}, viewIn: {app: 'WeChat', url: 'weixin://'}},
    {id: 'wx6', chatId: '!wx-filehelper:local', sender: 'You', body: '', sentAt: new Date(now - 1_600_000).toISOString(), mine: true, attachments: [{kind: 'file', url: demoReelUrl, name: 'AQO35LDKTG5E80mb8IC1UxBCatqRtz5e1UfSQbW_6TuswMo_IDXhnFdRLTK0IsjSS6YM4A.mp4', mimeType: null, size: 654, width: 16, height: 28, duration: .2}]},
    {id: 'c1', chatId: '!wa-jules:local', sender: '@whatsapp_jules:local', senderName: 'Jules Tan (WA)', body: 'Are we still on for Thursday?', sentAt: new Date(now - 3_600_000).toISOString(), mine: false, reactions: [
      {key: '👍', count: 3, reactors: [
        {id: '@whatsapp_amy:local', name: 'Amy', avatarUrl: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'},
        {id: '@whatsapp_ben:local', name: 'Ben', avatarUrl: null},
        {id: '@whatsapp_chen:local', name: 'Chen', avatarUrl: null},
      ]},
      {key: '❤️', count: 4, reactors: [
        {id: '@whatsapp_amy:local', name: 'Amy', avatarUrl: null},
        {id: '@whatsapp_ben:local', name: 'Ben', avatarUrl: null},
        {id: '@whatsapp_chen:local', name: 'Chen', avatarUrl: null},
        {id: '@whatsapp_dee:local', name: 'Dee', avatarUrl: null},
      ]},
    ]},
    {id: 'c2', chatId: '!wa-jules:local', sender: '@meta_demo-account:local', senderName: 'Unknown user', body: 'Yes — 2pm works.', sentAt: new Date(now - 3_500_000).toISOString(), mine: true},
    {id: 'c3', chatId: '!wa-family:local', sender: 'Mum', senderAvatarUrl: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7', body: 'Dinner Sunday?', sentAt: new Date(now - 86_400_000).toISOString(), mine: false},
    {id: 'wa-notice', chatId: '!wa-family:local', sender: '@whatsapp_aaron:local', body: 'Áron joined the group', notice: true, sentAt: new Date(now - 86_370_000).toISOString(), mine: false},
    {id: 'c4', chatId: '!tg-devs:local', sender: 'Priya', body: 'Shipped the build, logs look clean.', sentAt: new Date(now - 7_200_000).toISOString(), mine: false, reactions: [
      ...['👍', '❤️', '😂', '🔥', '🎉', '👏', '💯', '👀', '🤯', '🙏'].map((key, index) => ({
        key,
        count: 10,
        reactors: Array.from({length: 10}, (_, reactor) => ({
          id: `@telegram_reactor_${index}_${reactor}:local`,
          name: `Reactor ${index + 1}-${reactor + 1}`,
          avatarUrl: null,
        })),
      })),
    ]},
    {id: 'tg-notice', chatId: '!tg-devs:local', sender: '@telegrambot:local', body: 'Manny Asbanu joined the group', notice: true, sentAt: new Date(now - 7_260_000).toISOString(), mine: false},
    {id: 'tg-link', chatId: '!tg-devs:local', sender: 'Pp Ll', body: 'https://docs.google.com/presentation/d/tutorial/edit?usp=sharing', sentAt: new Date(now - 7_230_000).toISOString(), mine: false, linkPreview: {title: 'CS3210 Tutorial 1', description: 'Instrumentation, Profiling, Slurm, and Report Writing', url: 'https://docs.google.com/presentation/d/tutorial/edit?usp=sharing', source: 'docs.google.com', imageUrl: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7', imageMimeType: 'image/gif', imageWidth: 1200, imageHeight: 630}},
    {id: 'nus1', chatId: '!wa-nus-social:local', sender: 'Amelia', body: 'Dinner after class?', sentAt: new Date(now - 5_400_000).toISOString(), mine: false},
    {id: 'nus2', chatId: '!wa-nus-soc:local', sender: 'Kai', body: 'Tutorial group list', sentAt: new Date(now - 10_800_000).toISOString(), mine: false},
    {id: 'nus3', chatId: '!wa-nus-running:local', sender: 'Sam', body: 'Saturday, 8am at UTown', sentAt: new Date(now - 14_400_000).toISOString(), mine: false},
    // A second message from the same person, close behind: a group names the
    // first of someone's run and lets the rest follow it.
    {id: 'c6', chatId: '!wa-family:local', sender: 'Mum', senderAvatarUrl: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7', body: 'Roast if you can make it.', sentAt: new Date(now - 86_340_000).toISOString(), mine: false},
    {id: 'c7', chatId: '!wa-family:local', sender: 'Dad', body: 'I can bring dessert.', sentAt: new Date(now - 86_280_000).toISOString(), mine: false},
    {id: 'c8', chatId: '!wa-family:local', sender: 'WeChat', body: 'Peter6C invited Percival to the group chat', notice: true, sentAt: new Date(now - 86_220_000).toISOString(), mine: false},
    // A sticker, which is carried as an image but drawn at a sticker's size.
    // A real 1x1 GIF, terminator and all. The bytes have to decode: the thread
    // now swaps an image the homeserver would not serve for a named chip, and
    // a truncated fixture is indistinguishable from one, so a placeholder that
    // merely looked like a GIF made the sticker vanish from the demo.
    {id: 'c5', chatId: '!wa-jules:local', sender: '@whatsapp_jules:local', senderName: 'Unknown user', body: '', sentAt: new Date(now - 3_400_000).toISOString(), mine: false, replyTo: 'c2', attachments: [{kind: 'image', url: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7', name: 'Sticker', mimeType: 'image/gif', size: 42, width: 240, height: 240, sticker: true}]},
  ];
  type DemoChatAction =
    | {kind: 'text'; chatId: string; text: string; replyTo: string | null}
    | {kind: 'files'; chatId: string; files: string[]}
    | {kind: 'audio'; chatId: string; mimetype: string; size: number}
    | {kind: 'sticker'; chatId: string; stickerId: string}
    | {kind: 'recall'; chatId: string; messageId: string};
  const demoChatActions: DemoChatAction[] = [];
  (window as unknown as {
    polymuxDemoChatActions?: () => DemoChatAction[];
  }).polymuxDemoChatActions = () => structuredClone(demoChatActions);
  let demoChatSendGate: Promise<void> | null = null;
  let releaseDemoChatSend: (() => void) | null = null;
  let demoNextDeliveryUnconfirmed = false;
  (window as unknown as {polymuxDemoUnconfirmNextChatSend: () => void}).polymuxDemoUnconfirmNextChatSend = () => {
    demoNextDeliveryUnconfirmed = true;
  };
  (window as unknown as {polymuxDemoConfirmChatMessage: (id: string) => void}).polymuxDemoConfirmChatMessage = id => {
    demoChatMessages = demoChatMessages.map(message => message.id === id ? {...message, deliveryStatus: undefined} : message);
    const message = demoChatMessages.find(message => message.id === id);
    if (message) demoActivityListeners.forEach(listener => listener({chatId: message.chatId, sender: message.sender}));
  };

  (window as unknown as {
    polymuxDemoHoldChatSends?: () => void;
    polymuxDemoReleaseChatSends?: () => void;
  }).polymuxDemoHoldChatSends = () => {
    if (demoChatSendGate) return;
    demoChatSendGate = new Promise<void>((resolve) => {
      releaseDemoChatSend = resolve;
    });
  };
  (window as unknown as {
    polymuxDemoReleaseChatSends?: () => void;
  }).polymuxDemoReleaseChatSends = () => {
    releaseDemoChatSend?.();
    releaseDemoChatSend = null;
    demoChatSendGate = null;
  };
  const demoActivityListeners = new Set<(activity: ChatActivityDto) => void>();
  (window as unknown as {
    polymuxDemoIncomingChatMessage?: (chatId: string, body: string, details?: Pick<ChatMessageDto, 'forwarded' | 'viewIn'>) => void;
  }).polymuxDemoIncomingChatMessage = (chatId, body, details) => {
    const sentAt = new Date().toISOString();
    demoChatMessages = [{
      id: crypto.randomUUID(),
      chatId,
      sender: 'A contact',
      body,
      sentAt,
      mine: false,
      reactions: [],
      forwarded: details?.forwarded,
      viewIn: details?.viewIn,
    }, ...demoChatMessages];
    demoChats = demoChats.map((chat) => chat.id === chatId
      ? {...chat, preview: body, lastActivity: sentAt}
      : chat);
    queueMicrotask(() => demoActivityListeners.forEach((listener) => listener({
      chatId,
      sender: '@polymux_demo_contact:local',
    })));
  };
  const demoAddReaction = (
    chatId: string,
    messageId: string,
    key: string,
    reactor: NonNullable<NonNullable<ChatMessageDto['reactions']>[number]['reactors']>[number],
    mineEventId: string | null,
  ) => {
    demoChatMessages = demoChatMessages.map((item) => {
      if (item.chatId !== chatId || item.id !== messageId) return item;
      const reactions = item.reactions ?? [];
      const existing = reactions.find((reaction) => reaction.key === key);
      return {
        ...item,
        reactions: existing
          ? reactions.map((reaction) => reaction.key === key
            ? {
                ...reaction,
                count: reaction.count + 1,
                reactors: [...(reaction.reactors ?? []), reactor],
                mineEventId: mineEventId ?? reaction.mineEventId,
              }
            : reaction)
          : [...reactions, {key, count: 1, reactors: [reactor], mineEventId}],
      };
    });
    queueMicrotask(() => demoActivityListeners.forEach((listener) => listener({chatId, sender: reactor.id})));
  };
  (window as unknown as {
    polymuxDemoIncomingReaction?: (chatId: string, messageId: string, key: string) => void;
  }).polymuxDemoIncomingReaction = (chatId, messageId, key) => demoAddReaction(
    chatId,
    messageId,
    key,
    {id: '@whatsapp_late-reactor:local', name: 'Late reactor', avatarUrl: null},
    null,
  );
  let demoBroadcasts: BroadcastDto[] = [];
  let demoContactLinks: ContactLinkDto[] = [];
  const demoBroadcastMessages = new Map<string, BroadcastMessageDto[]>();
  const demoMailSends: SendMailRequest[] = [];
  (window as unknown as {
    polymuxDemoMailSends?: () => SendMailRequest[];
  }).polymuxDemoMailSends = () => demoMailSends.map((request) => structuredClone(request));
  const demoPdfContent = Uint8Array.from(
    atob('JVBERi0xLjQKMSAwIG9iago8PCAvVHlwZSAvQ2F0YWxvZyAvUGFnZXMgMiAwIFIgPj4KZW5kb2JqCjIgMCBvYmoKPDwgL1R5cGUgL1BhZ2VzIC9LaWRzIFszIDAgUl0gL0NvdW50IDEgPj4KZW5kb2JqCjMgMCBvYmoKPDwgL1R5cGUgL1BhZ2UgL1BhcmVudCAyIDAgUiAvTWVkaWFCb3ggWzAgMCA2MTIgNzkyXSAvUmVzb3VyY2VzIDw8IC9Gb250IDw8IC9GMSA1IDAgUiA+PiA+PiAvQ29udGVudHMgNCAwIFIgPj4KZW5kb2JqCjQgMCBvYmoKPDwgL0xlbmd0aCA0MSA+PgpzdHJlYW0KQlQgL0YxIDI0IFRmIDcyIDcyMCBUZCAoUTMgcmVwb3J0KSBUaiBFVAplbmRzdHJlYW0KZW5kb2JqCjUgMCBvYmoKPDwgL1R5cGUgL0ZvbnQgL1N1YnR5cGUgL1R5cGUxIC9CYXNlRm9udCAvSGVsdmV0aWNhID4+CmVuZG9iagp4cmVmCjAgNgowMDAwMDAwMDAwIDY1NTM1IGYgCjAwMDAwMDAwMDkgMDAwMDAgbiAKMDAwMDAwMDA1OCAwMDAwMCBuIAowMDAwMDAwMTE1IDAwMDAwIG4gCjAwMDAwMDAyNDEgMDAwMDAgbiAKMDAwMDAwMDMzMSAwMDAwMCBuIAp0cmFpbGVyCjw8IC9TaXplIDYgL1Jvb3QgMSAwIFIgPj4Kc3RhcnR4cmVmCjQwMQolJUVPRgo='),
    (character) => character.charCodeAt(0),
  ).buffer;
  const demoMailFolders: MailFolderDto[] = [
    {name: 'INBOX', label: 'Inbox', role: 'inbox'},
    {name: '[Gmail]/Drafts', label: 'Drafts', role: 'drafts'},
    {name: '[Gmail]/Sent Mail', label: 'Sent Mail', role: 'sent'},
    {name: '[Gmail]/All Mail', label: 'All Mail', role: 'archive'},
    {name: '[Gmail]/Spam', label: 'Spam', role: 'junk'},
    {name: '[Gmail]/Trash', label: 'Trash', role: 'trash'},
  ];
  let demoEnvelopes: Array<{folder: string; body: string; html?: string; envelope: MailEnvelopeDto}> = [
    {folder: 'INBOX', body: 'The quarterly numbers are attached. Let me know if you want the breakdown by region before Thursday.', html: '<p>The quarterly numbers are attached.</p><a href="cid:q3-report">Q3 report</a><p>Let me know if you want the breakdown by region before Thursday.</p>', envelope: {id: '1', subject: 'Q3 numbers', from: {name: 'Priya Raman', address: 'priya@example.com'}, to: {name: null, address: 'demo@example.com'}, date: new Date(now - 5_400_000).toISOString(), seen: false, flagged: true, answered: false, draft: false, hasAttachment: true, importance: 'high'}},
    {folder: 'INBOX', body: 'Reminder that the office will be closed on Monday.', envelope: {id: '2', subject: 'Closed Monday', from: {name: 'Office', address: 'office@example.com'}, to: {name: null, address: 'demo@example.com'}, date: new Date(now - 90_000_000).toISOString(), seen: true, flagged: true, answered: false, draft: false, hasAttachment: false}},
    {folder: 'INBOX', body: 'Your invoice for August is ready to view.', html: '<br><div style="height:40px"></div><div style="margin-top:36px;font-family:system-ui"><img src="cid:logo@example" alt="Billing"><img src="https://example.com/seal.png" alt="Paid"><h2 style="margin:0 0 8px">Invoice #1042</h2><p>Your invoice for August is <b>ready to view</b>.</p><table cellpadding="6" style="border-collapse:collapse"><tr><th align="left" style="border-bottom:1px solid #ddd">Item</th><th align="right" style="border-bottom:1px solid #ddd">Amount</th></tr><tr><td>Subscription</td><td align="right">$42.00</td></tr></table><p><a href="https://example.com/invoice/1042">View invoice</a></p></div>', envelope: {id: '3', subject: 'Invoice ready', from: {name: 'Billing', address: 'billing@example.com'}, to: {name: null, address: 'demo@example.com'}, date: new Date(now - 172_800_000).toISOString(), seen: true, flagged: false, answered: true, draft: false, hasAttachment: false}},
    {folder: '[Gmail]/Spam', body: 'You have definitely won a prize.', envelope: {id: '4', subject: 'YOU WON', from: {name: null, address: 'noreply@spam.example'}, to: null, date: new Date(now - 200_000_000).toISOString(), seen: false, flagged: false, answered: false, draft: false, hasAttachment: false}},
  ];
  const demoWorkspaceSnapshots = new Map<string, WorkspaceSnapshotDto>();
  let demoDictationPass = 0;
  let demoComputerHistoryEnabled = true;
  const demoHistoryPreview =
    typeof location !== 'undefined' && new URLSearchParams(location.search).get('history') === 'summary';
  const demoActivityStart = Math.floor((now - 20 * 60_000) / (10 * 60_000)) * (10 * 60_000);
  const demoComputerHistoryEntries: ComputerHistoryEntryDto[] = demoHistoryPreview ? [
    {id: 'history-chatgpt', capturedAt: new Date(demoActivityStart + 60_000).toISOString(), sourceId: 'ax-chatgpt', sourceName: 'ChatGPT — Computer History comparison', displayId: null, width: 0, height: 0, path: '/demo/computer-history/chatgpt.md', change: 1, reason: 'initial', bytes: 540, kind: 'text', app: 'ChatGPT'},
    {id: 'history-polymux', capturedAt: new Date(demoActivityStart + 4 * 60_000).toISOString(), sourceId: 'ax-polymux', sourceName: 'Polymux — Memory settings', displayId: null, width: 0, height: 0, path: '/demo/computer-history/polymux.md', change: .18, reason: 'change', bytes: 860, kind: 'text', app: 'Polymux'},
    {id: 'history-zed', capturedAt: new Date(demoActivityStart + 7 * 60_000).toISOString(), sourceId: 'ax-zed', sourceName: 'Zed — SettingsPage.svelte', displayId: null, width: 0, height: 0, path: '/demo/computer-history/zed.md', change: .12, reason: 'change', bytes: 720, kind: 'text', app: 'Zed'},
  ] : [];
  const demoComputerHistoryActivities: ComputerHistoryActivityDto[] = demoHistoryPreview ? [{
    id: new Date(demoActivityStart).toISOString(),
    startedAt: new Date(demoActivityStart).toISOString(),
    endedAt: new Date(demoActivityStart + 10 * 60_000).toISOString(),
    title: 'Computer History timeline redesign',
    summary: 'You compared Polymux with ChatGPT’s activity timeline, then reworked Computer History around concise semantic summaries with raw captures kept as supporting evidence.',
    apps: ['ChatGPT', 'Polymux', 'Zed'],
    entryIds: demoComputerHistoryEntries.map((entry) => entry.id),
    captures: demoComputerHistoryEntries.length,
    events: 14,
    summarized: true,
  }] : [];
  let demoComputerHistorySettings: Pick<
    ComputerHistoryStatusDto,
    'excludeApps' | 'excludeSites' | 'recordPrivateBrowsing' | 'interactionEvents'
  > = {
    excludeApps: [],
    excludeSites: [],
    recordPrivateBrowsing: true,
    interactionEvents: true,
  };
  const demoComputerHistoryStatus = (): ComputerHistoryStatusDto => ({
    ...demoComputerHistorySettings,
    enabled: demoComputerHistoryEnabled,
    running: demoComputerHistoryEnabled,
    directory: '/demo/computer-history',
    lastCapturedAt: demoComputerHistoryEntries[0]?.capturedAt ?? null,
    lastError: null,
    storedFrames: demoComputerHistoryEntries.length,
    storedBytes: demoComputerHistoryEntries.reduce((total, entry) => total + entry.bytes, 0),
    storedEvents: demoComputerHistoryActivities.reduce((total, activity) => total + activity.events, 0),
  });
  let demoMemoryEnabled = true;
  let demoGeneral: GeneralSettingsDto = {
    theme: 'light',
    language: 'system',
    // Pinned rather than null: a null here falls through to the locale
    // default, which makes the demo — and every UI test — depend on the
    // machine's timezone (this machine resolves to SGD).
    currency: 'USD',
    speechModeEnabled: true,
    dictationAutoStopSeconds: 6,
    timeEnabled: true,
    locationEnabled: true,
    hubIncognitoMode: false,
    reasoningLevel: 'medium',
    onboardingCompleted: !onboardingPreview,
    permissions: {microphone: true, 'screen-recording': true, accessibility: true, 'full-disk-access': true, reminders: true, calendars: true, contacts: true, photos: true, automation: true},
    notificationsEnabled: true,
    notifications: {'schedule-completed': true, 'schedule-failed': true, 'agent-completed': true, 'agent-attention': true, 'message-received': true},
    pinnedViews: [],
    location: null,
  };

  const demoUpdateReady =
    typeof location !== 'undefined' &&
    new URLSearchParams(location.search).get('update') === 'ready';
  const demoAgentConnectionFailure =
    typeof location !== 'undefined' &&
    new URLSearchParams(location.search).get('agentConnection') === 'fail';
  const demoUpdate: AppUpdateDto = {
    status: demoUpdateReady ? 'ready' : 'unsupported',
    version: '0.1.0',
    latest: demoUpdateReady ? '0.1.1' : null,
    checkedAt: '2026-08-14T00:00:00.000Z',
    message: demoUpdateReady ? null : 'Updates are managed by the desktop app.',
  };

  // The demo has no way to observe a real extension, so it reports installed
  // rather than fabricating a missing one — a false "not installed" would put
  // the install chip into the title bar of every screenshot and layout test.
  // `?extension=missing` opts into the prompting state for the tests that
  // exercise the chip itself.
  const demoExtensionMissing =
    typeof location !== 'undefined' &&
    new URLSearchParams(location.search).get('extension') === 'missing';
  let demoExtensionDismissed = false;
  const demoExtensionStatus = () => ({
    installed: !demoExtensionMissing,
    lastReportedAt: demoExtensionMissing ? null : new Date().toISOString(),
    version: demoExtensionMissing ? null : '0.2.2',
    protocolVersion: demoExtensionMissing ? null : 1,
    compatible: demoExtensionMissing ? null : true,
    capabilities: demoExtensionMissing ? [] : ['surface-commands-v1', 'tab-snapshots-v1'],
    promptToInstall: demoExtensionMissing && !demoExtensionDismissed,
  });

  let demoPhoneConnected = false;
  const demoPhoneMode = typeof location !== 'undefined'
    ? new URLSearchParams(location.search).get('phone')
    : null;
  let demoPhonePlatform: 'ios' | 'android' =
    demoPhoneMode === 'android-pair' ? 'android' : 'ios';
  let demoIosSigningStage: PhoneIosSigningStatusDto['stage'] =
    demoPhoneMode === 'ios-signing' ? 'signed-out' : 'authenticated';
  const demoIosProfileAvailable = () =>
    demoPhoneMode !== 'ios-signing' || demoPhoneConnected;
  const demoPhoneStatus = (): PhoneStatusDto => ({
    supported: true,
    stage: demoPhoneConnected
      ? 'connected'
      : demoPhoneMode === 'android-pair'
        ? 'disconnected'
        : demoIosProfileAvailable()
          ? 'ready'
          : 'needs-signing',
    device: demoPhoneMode === 'android-pair' && !demoPhoneConnected
      ? null
      : demoPhonePlatform === 'android'
        ? {platform: 'android', id: 'demo-phone', udid: 'demo', name: 'Pixel 9', model: 'Pixel 9', osVersion: '16', transport: 'wireless', pairingState: 'paired', developerMode: true, tunnelAddress: null}
        : {platform: 'ios', id: 'demo-phone', udid: 'demo', name: 'Demo iPhone', model: 'iPhone 16 Pro', osVersion: '26.6', transport: 'wireless', pairingState: 'paired', developerMode: true, tunnelAddress: null},
    signing: {
      available: demoPhonePlatform === 'ios' && demoIosProfileAvailable(),
      source: demoPhonePlatform === 'ios' && demoIosProfileAvailable()
        ? 'existing-profile'
        : 'none',
      expiresAt: demoPhonePlatform === 'ios' && demoIosProfileAvailable()
        ? new Date(Date.now() + 6 * 86_400_000).toISOString()
        : null,
      teamId: null,
      message: null,
    },
    wda: {
      available: demoPhonePlatform === 'ios',
      installed: demoPhonePlatform === 'ios',
      running: demoPhoneConnected && demoPhonePlatform === 'ios',
      bundleId: demoPhonePlatform === 'ios' ? 'com.polymux.phone.wda' : null,
    },
    controller: {
      kind: demoPhonePlatform === 'ios' ? 'wda' : 'adb',
      available: true,
      installed: true,
      running: demoPhoneConnected,
    },
    message: demoPhoneConnected
      ? null
      : demoPhoneMode === 'android-pair'
        ? 'Connect with USB, or pair Android wirelessly.'
        : 'Ready to connect.',
  });
  const demoIosSigningStatus = (): PhoneIosSigningStatusDto => ({
    supported: true,
    stage: demoIosSigningStage,
    email: demoIosSigningStage === 'signed-out' ? null : 'owner@example.com',
    teamId: null,
    verificationMethod:
      demoIosSigningStage === 'verification-required' ? 'trusted-device' : null,
    message: null,
  });

  const api: PolymuxApi = {
    phone: {
      status: async () => demoPhoneStatus(),
      connect: async () => {
        demoPhoneConnected = true;
        return demoPhoneStatus();
      },
      pairAndroid: async () => {
        demoPhonePlatform = 'android';
        demoPhoneConnected = true;
        return demoPhoneStatus();
      },
      iosSigningStatus: async () => demoIosSigningStatus(),
      iosSigningBegin: async () => {
        demoIosSigningStage = 'verification-required';
        return demoIosSigningStatus();
      },
      iosSigningComplete: async () => {
        demoIosSigningStage = 'authenticated';
        return demoIosSigningStatus();
      },
      iosSigningLogout: async () => {
        demoIosSigningStage = 'signed-out';
        return demoIosSigningStatus();
      },
      stop: async () => {
        demoPhoneConnected = false;
        return demoPhoneStatus();
      },
      frame: async () => ({
        deviceId: 'demo-phone',
        dataUrl: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
        width: 402,
        height: 874,
        capturedAt: new Date().toISOString(),
      }),
      tap: async () => {},
      swipe: async () => {},
      type: async () => {},
      home: async () => {},
    },
    terminal: (() => {
      const listeners = new Set<(event: TerminalEventDto) => void>();
      const sessions = new Map<string, {seq: number; replay: string}>();
      const prompt = btoa('$ ');
      const session = (id: string) => {
        const existing = sessions.get(id);
        if (!existing) throw new Error('Unknown terminal session');
        return existing;
      };
      return {
        create: async (_cwd?: string) => {
          const id = crypto.randomUUID();
          sessions.set(id, {seq: 0, replay: prompt});
          return {id};
        },
        attach: async (id: string) => {
          const current = session(id);
          return {id, seq: current.seq, replay: current.replay};
        },
        write: async (id: string, data: string) => {
          const current = session(id);
          current.seq += 1;
          const event: TerminalEventDto = {type: 'data', id, seq: current.seq, data: btoa(data)};
          for (const listener of listeners) listener(event);
        },
        resize: async () => {},
        close: async (id: string) => {
          const current = sessions.get(id);
          if (!current) return;
          sessions.delete(id);
          current.seq += 1;
          const event: TerminalEventDto = {type: 'exit', id, seq: current.seq, code: 0};
          for (const listener of listeners) listener(event);
        },
        subscribe(listener: (event: TerminalEventDto) => void) {
          listeners.add(listener);
          return () => listeners.delete(listener);
        },
      };
    })(),
    ide: (() => {
      const root = '/Users/demo/project';
      const files = new Map<string, IdeFileDto>([
        ['README.md', {name: 'README.md', path: 'README.md', language: 'Markdown', binary: false, content: '# Demo\n\nA small project for the IDE.\n'}],
        ['package.json', {name: 'package.json', path: 'package.json', language: 'JSON', binary: false, content: '{\n  "name": "demo"\n}\n'}],
        ['src/main.go', {name: 'main.go', path: 'src/main.go', language: 'Go', binary: false, content: 'package main\n\nfunc main() {}\n'}],
        ['src/icon.png', {name: 'icon.png', path: 'src/icon.png', language: 'Binary', binary: true, content: null}],
        ['src/App.svelte', {name: 'App.svelte', path: 'src/App.svelte', language: 'Svelte', binary: false, content: '<script lang="ts">\n  let name = $state("demo");\n</script>\n\n<p>Hello {name}</p>\n'}],
        ['src/app.css', {name: 'app.css', path: 'src/app.css', language: 'CSS', binary: false, content: ':root {\n  color: inherit;\n}\n'}],
        ['src/main.ts', {name: 'main.ts', path: 'src/main.ts', language: 'TypeScript', binary: false, content: 'console.log("demo");\n'}],
      ]);
      const tree: Record<string, IdeEntryDto[]> = {
        '': [
          {name: 'src', path: 'src', kind: 'folder'},
          {name: 'README.md', path: 'README.md', kind: 'file'},
          {name: 'package.json', path: 'package.json', kind: 'file'},
        ],
        src: [
          {name: 'App.svelte', path: 'src/App.svelte', kind: 'file'},
          {name: 'app.css', path: 'src/app.css', kind: 'file'},
          {name: 'icon.png', path: 'src/icon.png', kind: 'file'},
          {name: 'main.go', path: 'src/main.go', kind: 'file'},
          {name: 'main.ts', path: 'src/main.ts', kind: 'file'},
        ],
      };
      const parentOf = (relative: string) => {
        const index = relative.lastIndexOf('/');
        return index < 0 ? '' : relative.slice(0, index);
      };
      const baseOf = (relative: string) => {
        const index = relative.lastIndexOf('/');
        return index < 0 ? relative : relative.slice(index + 1);
      };
      const sortTree = (entries: IdeEntryDto[]) =>
        [...entries].sort((a, b) => {
          if (a.kind !== b.kind) return a.kind === 'folder' ? -1 : 1;
          return a.name.localeCompare(b.name, undefined, {sensitivity: 'base'});
        });
      const addToTree = (relative: string, kind: IdeEntryDto['kind']) => {
        const parent = parentOf(relative);
        const siblings = tree[parent] ?? [];
        if (kind === 'folder' && !tree[relative]) tree[relative] = [];
        if (siblings.some((entry) => entry.path === relative)) return;
        tree[parent] = sortTree([...siblings, {name: baseOf(relative), path: relative, kind}]);
      };
      const removeFromTree = (relative: string) => {
        const parent = parentOf(relative);
        tree[parent] = (tree[parent] ?? []).filter((entry) => entry.path !== relative);
      };
      const assertName = (relative: string) => {
        const name = baseOf(relative);
        if (!name || name === '.' || name === '..' || name.includes('\\')) throw new Error('Invalid name');
      };
      // Feed real file-reader results into the browser-only IDE fixture.
      (window as unknown as {polymuxDemoAddIdeFiles: (values: IdeFileDto[]) => void})
        .polymuxDemoAddIdeFiles = (values) => {
          for (const file of values) {
            files.set(file.path, structuredClone(file));
            addToTree(file.path, 'file');
          }
        };
      return {
        pickFolder: async () => root,
        list: async (_project: string, path = '') => structuredClone(tree[path] ?? []),
        read: async (_project: string, path: string) => {
          const file = files.get(path);
          if (!file) throw new Error('File not found');
          return structuredClone(file);
        },
        write: async (_project: string, path: string, content: string) => {
          const file = files.get(path);
          if (!file || file.binary) throw new Error('File not found');
          files.set(path, {...file, content});
        },
        create: async (_project: string, path: string, content = '') => {
          assertName(path);
          if (files.has(path) || (tree[parentOf(path)] ?? []).some((entry) => entry.path === path)) {
            throw new Error('Already exists');
          }
          const parent = parentOf(path);
          if (parent && !tree[parent] && !tree[''].some((entry) => entry.path === parent && entry.kind === 'folder')) {
            throw new Error('Folder not found');
          }
          const name = baseOf(path);
          const binary = isBinaryFileName(name);
          const file: IdeFileDto = {
            name,
            path,
            language: languageForName(name),
            binary,
            content: binary ? null : content,
          };
          files.set(path, file);
          addToTree(path, 'file');
          return structuredClone(file);
        },
        move: async (_project: string, from: string, to: string) => {
          assertName(to);
          const file = files.get(from);
          if (!file) throw new Error('File not found');
          if (from === to) return structuredClone(file);
          if (files.has(to) || (tree[parentOf(to)] ?? []).some((entry) => entry.path === to)) {
            throw new Error('Already exists');
          }
          const parent = parentOf(to);
          if (parent && !tree[parent] && !tree[''].some((entry) => entry.path === parent && entry.kind === 'folder')) {
            throw new Error('Folder not found');
          }
          const name = baseOf(to);
          const binary = isBinaryFileName(name);
          const next: IdeFileDto = {
            ...file,
            name,
            path: to,
            language: languageForName(name),
            binary,
            content: binary ? null : file.binary ? '' : file.content,
          };
          files.delete(from);
          files.set(to, next);
          removeFromTree(from);
          addToTree(to, 'file');
          return structuredClone(next);
        },
      };
    })(),
    agentRuntime: {
      get: async () => structuredClone(demoAgentRuntime),
      registry: async () => demoAcpRegistry,
      inspectConfiguration: async (request, sourceDirectory) => {
        const requestedId = request.agentId ?? request.name.toLowerCase().replace(/[^a-z0-9]+/g, '-');
        const family = ({'claude-acp': 'claude', 'codex-acp': 'codex', 'pi-acp': 'pi', 'qwen-code': 'qwen', 'github-copilot-cli': 'github-copilot'} as Record<string, string>)[requestedId] ?? requestedId;
        const defaultDirectory = ({
          claude: '/Users/demo/.claude', codex: '/Users/demo/.codex', pi: '/Users/demo/.pi/agent',
          opencode: '/Users/demo/.config/opencode', junie: '/Users/demo/.junie', poolside: '/Users/demo/.config/poolside',
          gemini: '/Users/demo/.gemini', qwen: '/Users/demo/.qwen', 'github-copilot': '/Users/demo/.copilot',
          'mistral-vibe': '/Users/demo/.vibe',
        } as Record<string, string>)[family];
        const verified = !!defaultDirectory;
        const directory = sourceDirectory?.trim() || defaultDirectory || '';
        const exists = family === 'claude' || !!sourceDirectory?.trim();
        const syncReason = family === 'poolside'
          ? 'Poolside follows a shared XDG configuration root. Import a private copy instead.'
          : exists ? null : 'No configuration folder exists at this location yet.';
        return [{
          id: directory || `unsupported:${requestedId}`,
          agentId: family,
          agentName: request.name,
          name: directory.endsWith('-work') ? `${request.name} Work` : `${request.name} Default`,
          directory,
          exists,
          supportsImport: verified,
          supportsSync: verified && family !== 'poolside' && exists,
          importUnavailableReason: verified ? null : 'Polymux does not yet have a verified configuration adapter for this agent. Start clean to avoid exposing unrelated files.',
          syncUnavailableReason: verified ? syncReason : 'The official ACP registry does not publish this agent\'s configuration layout.',
          summaries: family === 'claude' ? [
            {kind: 'settings' as const, count: 3, items: ['settings.json', 'agents/explore.md', 'CLAUDE.md'], importable: true, detail: null},
            {kind: 'skills' as const, count: 4, items: ['ce-code-review', 'frontend-design', 'ui-tester', 'run'], importable: true, detail: null},
            {kind: 'plugins' as const, count: 2, items: ['context-mode', 'frontend-design'], importable: true, detail: null},
            {kind: 'mcp' as const, count: 2, items: ['Google Drive', 'Notion'], importable: true, detail: null},
            {kind: 'memory' as const, count: 1, items: ['MEMORY.md'], importable: true, detail: null},
          ] : (['settings', 'skills', 'plugins', 'mcp', 'memory'] as const).map((kind) => ({kind, count: 0, items: [], importable: verified, detail: null})),
        }];
      },
      update: async (request) => {
        demoAgentRuntime = request.kind === 'polymux'
          ? {kind: 'polymux', name: 'Polymux Agent'}
          : {kind: 'acp', name: request.name, command: request.command, args: request.args ?? [], cwd: request.cwd ?? null, config: request.config ?? {}, agentId: request.agentId ?? request.name.toLowerCase().replace(/[^a-z0-9]+/g, '-'), configId: request.configId ?? crypto.randomUUID()};
        if (request.kind === 'acp') demoAgentSettings = structuredClone(request.name === 'pi ACP' ? demoPiAgentSettings : demoCompactAgentSettings);
        demoWorkspaceApps.apps = demoWorkspaceApps.apps.map((app) => app.official ? {...app, enabled: request.kind === 'polymux'} : app);
        demoWorkspaceApps.pinnedIds = request.kind === 'polymux' ? ['drive', 'calendar', 'hub', 'tasks'] : [];
        return structuredClone(demoAgentRuntime);
      },
      settings: async () => {
        if (demoAgentConnectionFailure) throw new Error('External agent connection failed');
        return structuredClone(demoAgentSettings);
      },
      authenticate: async (methodId) => {
        if (!demoAgentSettings.authMethods.some((method) => method.id === methodId && method.available)) throw new Error(`Unsupported authentication method: ${methodId}`);
        demoAgentSettings = {...demoAgentSettings, authRequired: false};
        return structuredClone(demoAgentSettings);
      },
      logout: async () => {
        demoAgentSettings = {...demoAgentSettings, authRequired: true};
        return structuredClone(demoAgentSettings);
      },
      setConfigOption: async (id, value) => {
        demoAgentSettings = {...demoAgentSettings, configOptions: demoAgentSettings.configOptions.map((option) => option.id === id ? {...option, currentValue: value} as typeof option : option)};
        if (demoAgentRuntime.kind === 'acp') demoAgentRuntime.config[id] = value;
        return structuredClone(demoAgentSettings);
      },
      setProvider: async (request) => {
        demoAgentSettings = {...demoAgentSettings, providers: demoAgentSettings.providers.map((provider) => provider.id === request.id ? {...provider, apiType: request.apiType, baseUrl: request.baseUrl} : provider)};
        return structuredClone(demoAgentSettings);
      },
      disableProvider: async (id) => {
        demoAgentSettings = {...demoAgentSettings, providers: demoAgentSettings.providers.map((provider) => provider.id === id ? {...provider, apiType: null, baseUrl: null} : provider)};
        return structuredClone(demoAgentSettings);
      },
    },
    profiles: {
      list: async () => ({activeId: demoActiveProfile, profiles: structuredClone(demoProfiles)}),
      create: async (name) => { demoProfiles.push({id: crypto.randomUUID(), name: name.trim() || 'New profile', isDefault: false, source: null}); return {activeId: demoActiveProfile, profiles: structuredClone(demoProfiles)}; },
      select: async (id) => { demoActiveProfile = id; return {activeId: id, profiles: structuredClone(demoProfiles)}; },
      rename: async (id, name) => { demoProfiles = demoProfiles.map(profile => profile.id === id ? {...profile, name} : profile); return {activeId: demoActiveProfile, profiles: structuredClone(demoProfiles)}; },
      setDefault: async (id) => { demoProfiles = demoProfiles.map(profile => ({...profile, isDefault: profile.id === id})); return {activeId: demoActiveProfile, profiles: structuredClone(demoProfiles)}; },
      duplicate: async (id) => { const source = demoProfiles.find(profile => profile.id === id)!; demoProfiles.push({...source, id: crypto.randomUUID(), name: `${source.name} copy`, isDefault: false}); return {activeId: demoActiveProfile, profiles: structuredClone(demoProfiles)}; },
      remove: async (id) => { demoProfiles = demoProfiles.filter(profile => profile.id !== id); if (demoActiveProfile === id) demoActiveProfile = 'default'; return {activeId: demoActiveProfile, profiles: structuredClone(demoProfiles)}; },
      connectExternal: async (request) => {
        let target = request.profileId ? demoProfiles.find((profile) => profile.id === request.profileId) : undefined;
        if (!target && (request.mode === 'import' || request.mode === 'sync')) {
          target = {id: crypto.randomUUID(), name: request.profileName?.trim() || `${request.runtime.name} Profile`, isDefault: false, source: null};
          demoProfiles.push(target);
          demoActiveProfile = target.id;
        }
        target ??= demoProfiles.find((profile) => profile.id === demoActiveProfile)!;
        target.name = request.profileName?.trim() || target.name;
        target.source = request.mode === 'sync'
          ? {kind: 'external', agentId: request.runtime.agentId ?? 'claude', agentName: request.runtime.name, directory: request.sourceDirectory ?? '/Users/demo/.claude'}
          : null;
        demoAgentRuntime = {
          kind: 'acp',
          name: request.runtime.name,
          command: request.runtime.command,
          args: request.runtime.args ?? [],
          cwd: request.runtime.cwd ?? null,
          config: request.runtime.config ?? {},
          agentId: request.runtime.agentId ?? 'claude',
          configId: request.runtime.configId ?? crypto.randomUUID(),
        };
        demoAgentSettings = structuredClone(request.runtime.name === 'pi ACP' ? demoPiAgentSettings : demoCompactAgentSettings);
        demoWorkspaceApps.apps = demoWorkspaceApps.apps.map((app) => app.official ? {...app, enabled: false} : app);
        demoWorkspaceApps.pinnedIds = [];
        return {activeId: demoActiveProfile, profiles: structuredClone(demoProfiles)};
      },
      openFolder: async () => {},
      subscribe: () => () => {},
    },
    extension: {
      status: async () => demoExtensionStatus(),
      dismiss: async () => {
        demoExtensionDismissed = true;
        return demoExtensionStatus();
      },
      openInstall: async () => { window.open(EXTENSION_INSTALL_URL, '_blank'); },
    },
    general: {
      get: async () => structuredClone(demoGeneral),
      update: async (settings) => {
        demoGeneral = {
          ...demoGeneral,
          ...settings,
          // A partial patch, matching the real backend: a row sends only the
          // switch it moved.
          permissions: {...demoGeneral.permissions, ...settings.permissions},
          notifications: {...demoGeneral.notifications, ...settings.notifications},
          location:
            settings.locationEnabled === false
              ? null
              : settings.location === undefined
                ? demoGeneral.location
                : settings.location,
        };
        return structuredClone(demoGeneral);
      },
      locate: async () => ({latitude: -33.8688, longitude: 151.2093, accuracy: 25_000, updatedAt: '2026-08-14T00:00:00.000Z'}),
      version: async () => ({
        version: releaseNotesPreview ? __POLYMUX_VERSION__ : '0.1.0',
        electron: '',
        platform: 'browser',
        packaged: releaseNotesPreview,
      }),
      checkForUpdates: async () => demoUpdate,
      installUpdate: async () => demoUpdate,
      // The browser demo has no OS notification centre behind it, so it says
      // so rather than claiming a notification the user will never see.
      testNotification: async () => 'unsupported' as const,
    },
    clipboard: {
      write: async (content) => {
        if (content.kind !== 'text') return false;
        try {
          await navigator.clipboard.writeText(content.text);
          return true;
        } catch {
          return false;
        }
      },
    },
    locker: {
      status: async () => demoLockerStatus(),
      create: async (password) => {
        if (password.length < 8) throw new Error('Use at least 8 characters');
        demoLockerExists = true;
        demoLockerUnlocked = true;
        demoLockerMaster = password;
        notifyDemoLocker();
        return demoLockerStatus();
      },
      unlock: async (password) => {
        if (!demoLockerExists) throw new Error('Create a locker first');
        if (password !== demoLockerMaster) throw new Error('Wrong master password');
        demoLockerUnlocked = true;
        notifyDemoLocker();
        return demoLockerStatus();
      },
      lock: async () => {
        demoLockerUnlocked = false;
        notifyDemoLocker();
        return demoLockerStatus();
      },
      touch: async () => {},
      list: async () => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        return demoLockerList();
      },
      reveal: async (id) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        const item = demoLockerItems.find((entry) => entry.id === id);
        if (!item) throw new Error('That item is not in the locker');
        return {
          password: item.password,
          totp: item.totpSecret ? demoTotp(item.totpSecret) : null,
          recoveryCodes: item.recoveryCodes,
          passkey: item.passkey
            ? {
                relyingParty: item.passkey.relyingParty,
                username: item.passkey.username,
                credentialId: item.passkey.credentialId,
                userHandle: item.passkey.userHandle,
              }
            : null,
        };
      },
      totp: async (id) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        const item = demoLockerItems.find((entry) => entry.id === id && !entry.trashed);
        return item?.totpSecret ? demoTotp(item.totpSecret) : null;
      },
      codes: async () => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        return demoLockerItems
          .filter((entry) => !entry.trashed && entry.totpSecret)
          .map((entry) => {
            const totp = demoTotp(entry.totpSecret);
            return {id: entry.id, code: totp.code, next: totp.next, period: totp.period, remaining: totp.remaining};
          });
      },
      otpauth: async (id) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        const item = demoLockerItems.find((entry) => entry.id === id);
        if (!item?.totpSecret) return null;
        const issuer = encodeURIComponent(item.title || 'Locker');
        const account = encodeURIComponent(item.username || item.title || 'Locker');
        return `otpauth://totp/${issuer}:${account}?secret=${item.totpSecret.replace(/\s+/g, '')}&issuer=${issuer}`;
      },
      save: async (input) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        const existing = input.id ? demoLockerItems.find((entry) => entry.id === input.id) : undefined;
        const entry: DemoLockerEntry = existing ?? {
          id: crypto.randomUUID(),
          title: input.title,
          username: '',
          url: '',
          notes: '',
          groupName: 'Locker',
          password: '',
          totpSecret: '',
          recoveryCodes: [],
          passkey: null,
          pinned: false,
          sortIndex: demoLockerItems.length,
          trashed: false,
          updatedAt: new Date().toISOString(),
        };
        entry.title = input.title;
        if (input.username !== undefined) entry.username = input.username;
        if (input.url !== undefined) entry.url = input.url;
        if (input.notes !== undefined) entry.notes = input.notes;
        if (input.groupName !== undefined) entry.groupName = input.groupName;
        if (input.password !== undefined) entry.password = input.password;
        if (input.totpSecret !== undefined) entry.totpSecret = input.totpSecret;
        if (input.recoveryCodes !== undefined) entry.recoveryCodes = input.recoveryCodes;
        if (input.passkey !== undefined)
          entry.passkey = input.passkey
            ? {
                relyingParty: input.passkey.relyingParty,
                username: input.passkey.username,
                credentialId: input.passkey.credentialId,
                userHandle: input.passkey.userHandle ?? '',
                privateKeyPem: input.passkey.privateKeyPem ?? existing?.passkey?.privateKeyPem ?? '',
              }
            : null;
        entry.updatedAt = new Date().toISOString();
        if (!existing) demoLockerItems.push(entry);
        notifyDemoLocker();
        return demoLockerList().items.find((item) => item.id === entry.id)!;
      },
      remove: async (id) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        const item = demoLockerItems.find((entry) => entry.id === id && !entry.trashed);
        if (item) item.trashed = true;
        notifyDemoLocker();
        return demoLockerList();
      },
      restore: async (ids) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        for (const id of ids) {
          const item = demoLockerItems.find((entry) => entry.id === id);
          if (item) item.trashed = false;
        }
        notifyDemoLocker();
        return demoLockerList();
      },
      purge: async (ids) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        for (const id of ids) {
          const index = demoLockerItems.findIndex((entry) => entry.id === id && entry.trashed);
          if (index >= 0) demoLockerItems.splice(index, 1);
        }
        notifyDemoLocker();
        return demoLockerList();
      },
      emptyTrash: async () => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        for (let index = demoLockerItems.length - 1; index >= 0; index -= 1) {
          if (demoLockerItems[index]?.trashed) demoLockerItems.splice(index, 1);
        }
        notifyDemoLocker();
        return demoLockerList();
      },
      pin: async (ids, pinned) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        for (const id of ids) {
          const item = demoLockerItems.find((entry) => entry.id === id && !entry.trashed);
          if (item) item.pinned = pinned;
        }
        notifyDemoLocker();
        return demoLockerList();
      },
      reorder: async (ids) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        ids.forEach((id, index) => {
          const item = demoLockerItems.find((entry) => entry.id === id);
          if (item) item.sortIndex = index;
        });
        notifyDemoLocker();
        return demoLockerList();
      },
      changePassword: async (current, next) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        if (current !== demoLockerMaster) throw new Error('Wrong master password');
        if (next.length < 8) throw new Error('Use at least 8 characters');
        demoLockerMaster = next;
        notifyDemoLocker();
        return demoLockerStatus();
      },
      copy: async (id, field, recoveryIndex) => {
        if (!demoLockerUnlocked) throw new Error('Locker is locked');
        const item = demoLockerItems.find((entry) => entry.id === id);
        if (!item) throw new Error('That item is not in the locker');
        const text =
          field === 'password' ? item.password
          : field === 'username' ? item.username
          : field === 'url' ? item.url
          : field === 'notes' ? item.notes
          : field === 'totp' ? demoTotp(item.totpSecret).code
          : item.recoveryCodes[recoveryIndex ?? -1] ?? '';
        try {
          await navigator.clipboard.writeText(text);
          return true;
        } catch {
          return false;
        }
      },
      importBegin: async () => ({status: 'cancelled' as const}),
      importConfirm: async () => ({imported: 0, skipped: 0, problems: ['Import is available in the desktop app']}),
      sync: async () => demoLockerStatus(),
      setStorage: async (mode) => {
        demoLockerStorage = mode;
        notifyDemoLocker();
        return demoLockerStatus();
      },
      subscribe(listener) {
        demoLockerListeners.add(listener);
        return () => demoLockerListeners.delete(listener);
      },
    },
    finance: {
      read: async () => { throw new Error("Connect BankMCP in the desktop app to read your accounts."); },
    },
    usage: {
      get: async (filter = {}) => {
        const stats = demoUsageStats();
        const scope = filter.scope ?? 'all';
        const shares: Record<string, number> = scope === 'assistant'
          ? {polymux: 5 / 6, 'acp:claude': 1}
          : scope === 'team' ? {polymux: 1 / 6, 'acp:codex': 1} : {polymux: 1, 'acp:claude': 1, 'acp:codex': 1};
        const agents = stats.agents.filter(agent => shares[agent.id]).map(agent => ({...agent,
          tokens: Math.round(agent.tokens * shares[agent.id]), costUsd: agent.costUsd * shares[agent.id],
          runs: Math.round(agent.runs * shares[agent.id]), chats: Math.round(agent.chats * shares[agent.id]),
        }));
        const selected = filter.agentId ? agents.filter(agent => agent.id === filter.agentId) : agents;
        const tokens = selected.reduce((sum, agent) => sum + agent.tokens, 0);
        const spend = selected.reduce((sum, agent) => sum + agent.costUsd, 0);
        const ratio = stats.lifetimeTokens ? tokens / stats.lifetimeTokens : 0;
        return {
          ...stats, scope, agents, agentId: filter.agentId ?? null,
          lifetimeTokens: tokens, peakTokens: Math.round(stats.peakTokens * ratio), costUsd: spend,
          totalChats: selected.reduce((sum, agent) => sum + agent.chats, 0),
          days: stats.days.map(day => ({...day, tokens: Math.round(day.tokens * ratio), costUsd: day.costUsd * ratio, runs: Math.round(day.runs * ratio)})),
          plugins: scope === 'team' || filter.agentId === 'acp:codex'
            ? [{name: 'window-control', count: 412}, {name: 'communication', count: 208}] : stats.plugins,
          connections: scope === 'team' || filter.agentId === 'acp:codex' ? [{name: 'Linear', count: 180}] : stats.connections,
          models: stats.models.map(model => ({...model, tokens: Math.round(model.tokens * ratio), costUsd: model.costUsd * ratio, runs: Math.round(model.runs * ratio)})),
        };
      },
    },
    // A browser tab has no traffic lights to move out of, so the state never
    // changes and the subscription has nothing to tear down.
    window: {
      openWorkspaceView: async () => {},
      subscribeNotificationTarget: () => () => {},
      subscribeFullscreen: () => () => {},
    },
    permissions: {
      ensureFirstRun: async () => ({firstRun: false, microphone: 'granted', screenRecording: 'granted'}),
      status: async (kind) => {
        if (permissionPreview) {
          const value = localStorage.getItem(`polymux.demo.permission.${kind}`);
          return value === 'granted' || value === 'unknown' || value === 'restricted' ? value : 'denied';
        }
        return onboardingPreview ? 'not-determined' : 'granted';
      },
      request: async () => {
        await new Promise((resolve) => setTimeout(resolve, 600));
        return 'granted';
      },
      openSettings: async (kind) => {
        if (permissionPreview) localStorage.setItem('polymux.demo.permission.opened', kind);
      },
    },
    dictation: {
      // Nothing to fetch: the demo has no model behind it.
      prepare: async () => {},
      // whisper.cpp lives in the desktop app, so the demo stands in for it —
      // revealing the canned sentence a few words at a time, the way real
      // passes over a growing recording do. Without this the composer's voice
      // button is the one control a browser test cannot exercise.
      transcribe: async () => {
        const words = 'this is dictated text from the browser demo'.split(' ');
        demoDictationPass = Math.min(demoDictationPass + 1, words.length);
        return words.slice(0, demoDictationPass).join(' ');
      },
    },
    conversations: {
      duplicate: async (id, throughMessageId) => {
        const source = conversations.find(item => item.id === id);
        if (!source) throw new Error('Conversation not found');
        const history = messages.get(id) ?? [];
        const boundary = throughMessageId === undefined ? history.length - 1 : history.findIndex(item => item.id === throughMessageId);
        if (throughMessageId !== undefined && boundary < 0) throw new Error('Fork message not found in conversation');
        const copy = conversation(crypto.randomUUID(), conversationCopyTitle(source.title, conversations.map(item => item.title)), Date.now());
        conversations = [copy, ...conversations];
        messages.set(copy.id, structuredClone(history.slice(0, boundary + 1)).map(message => ({...message,
          id: crypto.randomUUID(), conversationId: copy.id, runId: null})));
        return copy;
      },
      list: async () => [...conversations].filter((item) => !item.archivedAt).sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)),
      listArchived: async () => [...conversations].filter((item) => item.archivedAt).sort((a, b) => (b.archivedAt ?? b.updatedAt).localeCompare(a.archivedAt ?? a.updatedAt)),
      create: async (title = 'New chat') => {
        const created = conversation(crypto.randomUUID(), title, Date.now());
        conversations = [created, ...conversations];
        messages.set(created.id, []);
        return created;
      },
      rename: async (id, title) => {
        const current = conversations.find((item) => item.id === id);
        if (!current) return null;
        const updated = {...current, title, updatedAt: new Date().toISOString()};
        conversations = conversations.map((item) => item.id === id ? updated : item);
        return updated;
      },
      archive: async (id) => {
        const current = conversations.find((item) => item.id === id);
        if (!current) return null;
        const now = new Date().toISOString();
        const updated = {...current, archivedAt: current.archivedAt ?? now, updatedAt: now};
        conversations = conversations.map((item) => item.id === id ? updated : item);
        return updated;
      },
      unarchive: async (id) => {
        const current = conversations.find((item) => item.id === id);
        if (!current) return null;
        const updated = {...current, archivedAt: null, updatedAt: new Date().toISOString()};
        conversations = conversations.map((item) => item.id === id ? updated : item);
        return updated;
      },
      remove: async (id) => {
        const before = conversations.length;
        conversations = conversations.filter((item) => item.id !== id);
        messages.delete(id);
        goals.delete(id);
        addedReferences.delete(id);
        return conversations.length !== before;
      },
      messages: async (id) => structuredClone(messages.get(id) ?? []),
      updateMessage: async (id, patch) => {
        for (const [conversationId, items] of messages) {
          const index = items.findIndex((item) => item.id === id);
          if (index < 0) continue;
          const current = items[index]!;
          const attachments = patch.attachments?.map((attachmentPath) => ({
            id: crypto.randomUUID(), messageId: id, name: attachmentPath.split(/[\\/]/).pop() ?? attachmentPath,
            path: attachmentPath, mimeType: null, size: null, sha256: null, createdAt: new Date().toISOString(),
          })) ?? [];
          const updated = {
            ...current,
            ...(patch.content === undefined ? {} : {content: patch.content}),
            ...(patch.metadata === undefined ? {} : {metadata: patch.metadata}),
            attachments: [...current.attachments, ...attachments],
          };
          messages.set(conversationId, items.map((item) => item.id === id ? updated : item));
          return structuredClone(updated);
        }
        return null;
      },
    },
    devices: {
      request: async (request) => {
        if (request.action === 'start') {
          demoDevicePairing = {approvals: [], connectedDevices: [], outgoing: {id: 'demo-pair', number: '42', deviceName: 'Home Mac mini', expiresAt: new Date(Date.now() + 120000).toISOString()}};
        }
        if (request.action === 'approve') demoDevicePairing = {approvals: [], outgoing: null, connectedDevices: request.number === '42' ? [{deviceId: 'test-phone', deviceName: 'Test Phone', deviceType: 'phone', online: true, pairedAt: new Date().toISOString()}] : []};
        if (request.action === 'cancel') demoDevicePairing = {approvals: [], connectedDevices: [], outgoing: null};
        if (request.action === 'invitation') return {...demoDevicePairing, installCommand: 'curl -fsSL https://polymux.com/install.sh | sh -s -- connect demo-invitation'};
        return structuredClone(demoDevicePairing);
      },
    },
    account: (() => {
      let status: AccountStatusDto = {signedIn: false, available: true, profile: null, accounts: []};
      const listeners = new Set<(status: AccountStatusDto) => void>();
      const publish = (): AccountStatusDto => { listeners.forEach((listener) => listener(structuredClone(status))); return structuredClone(status); };
      const applyProfile = (next: NonNullable<AccountStatusDto['profile']>): AccountStatusDto => {
        if (status.signedIn && status.profile && status.profile.userId !== next.userId) {
          const rest = status.accounts.filter((entry) => entry.userId !== next.userId && entry.userId !== status.profile!.userId);
          status.accounts = [status.profile, ...rest];
        }
        status = {
          signedIn: true,
          available: true,
          profile: next,
          accounts: status.accounts.filter((entry) => entry.userId !== next.userId),
        };
        return publish();
      };
      return {
        get: async () => structuredClone(status),
        signInWithPassword: async (email) => applyProfile({
          userId: `demo:${email.toLowerCase()}`,
          email,
          name: email.split('@')[0] ?? 'Demo',
          avatarUrl: '',
        }),
        signUp: async (email) => applyProfile({
          userId: `demo:${email.toLowerCase()}`,
          email,
          name: email.split('@')[0] ?? 'Demo',
          avatarUrl: '',
        }),
        resendConfirmation: async () => ({ok: true}),
        requestPasswordReset: async () => ({ok: true}),
        updatePassword: async () => publish(),
        signInWithOAuth: async (provider) => {
          void provider;
          return applyProfile({userId: 'demo-user', email: 'owner@example.com', name: 'Demo', avatarUrl: ''});
        },
        switchTo: async (userId) => {
          const found = status.accounts.find((entry) => entry.userId === userId);
          if (!found) return structuredClone(status);
          return applyProfile(found);
        },
        signOut: async () => {
          status = {signedIn: false, available: true, profile: null, accounts: status.accounts};
          return publish();
        },
        subscribe: (listener) => {
          listeners.add(listener);
          return () => { listeners.delete(listener); };
        },
      };
    })(),
    team: {
      list: async () => structuredClone(demoBots),
      groups: async () => structuredClone(demoTeamGroups),
      createGroup: async (request) => {
        const group: TeamGroupDto = {
          id: crypto.randomUUID(),
          conversationId: crypto.randomUUID(),
          name: request.name,
          memberIds: [...new Set(request.memberIds)],
          preview: 'No messages yet',
          updatedAt: new Date().toISOString(),
          unread: false,
          unreadCount: 0,
        };
        demoTeamGroups = [group, ...demoTeamGroups];
        messages.set(group.conversationId, []);
        demoTeamGroupListeners.forEach((listener) => listener(structuredClone(demoTeamGroups)));
        return structuredClone(group);
      },
      updateGroup: async (id, request) => {
        const current = demoTeamGroups.find((group) => group.id === id);
        if (!current) throw new Error('Unknown Team group');
        const updated = {...current, ...request, updatedAt: new Date().toISOString()};
        demoTeamGroups = demoTeamGroups.map((group) => group.id === id ? updated : group);
        demoTeamGroupListeners.forEach((listener) => listener(structuredClone(demoTeamGroups)));
        return structuredClone(updated);
      },
      markGroupRead: async (id) => {
        const current = demoTeamGroups.find((group) => group.id === id);
        if (!current) throw new Error('Unknown Team group');
        current.unread = false;
        current.unreadCount = 0;
        demoTeamGroupListeners.forEach((listener) => listener(structuredClone(demoTeamGroups)));
        return structuredClone(current);
      },
      removeGroup: async (id) => {
        const current = demoTeamGroups.find((group) => group.id === id);
        if (!current) return false;
        demoTeamGroups = demoTeamGroups.filter((group) => group.id !== id);
        messages.delete(current.conversationId);
        demoTeamGroupListeners.forEach((listener) => listener(structuredClone(demoTeamGroups)));
        return true;
      },
      sendGroup: async (request) => {
        const group = demoTeamGroups.find((candidate) => candidate.id === request.id);
        if (!group) throw new Error('Unknown Team group');
        if (teamGroupSendPreview === 'slow' || teamGroupSendPreview === 'fail')
          await new Promise((resolve) => setTimeout(resolve, 240));
        if (teamGroupSendPreview === 'fail') throw new Error('Demo group delivery failed');
        const created = message(crypto.randomUUID(), group.conversationId, 'user', request.text, Date.now(), null, {
          teamGroupMessage: {memberIds: group.memberIds, deliveredMemberIds: group.memberIds, failedMemberIds: []},
        });
        messages.set(group.conversationId, [...(messages.get(group.conversationId) ?? []), created]);
        group.preview = request.text;
        group.updatedAt = created.createdAt;
        demoTeamGroupListeners.forEach((listener) => listener(structuredClone(demoTeamGroups)));
        return structuredClone(created);
      },
      profiles: async () => structuredClone(demoTeamProfileOptions()),
      create: async (request) => {
        const host = demoHosts.find((candidate) => candidate.hostId === (request.hostId ?? demoHosts.find((candidate) => candidate.isDefault)?.hostId)) ?? demoHosts[0];
        const member = demoBot(
          crypto.randomUUID(), crypto.randomUUID(), request.name, request.role,
          request.avatar.color, 'idle', request.role,
          Date.now(), request.avatar.shape,
        );
        member.avatar = structuredClone(request.avatar);
        member.profileId = request.profileId;
        member.profileName = demoTeamProfileOptions().find((profile) => profile.id === request.profileId)?.name ?? 'Missing profile';
        member.hostId = host.hostId;
        member.hostName = host.deviceName;
        member.deviceType = host.deviceType;
        member.laptopAccess = request.laptopAccess === 'ask' ? 'ask' : 'off';
        member.skills = request.skills;
        member.mcpServers = request.mcpServers;
        member.plugins = request.plugins;
        demoBots = [member, ...demoBots];
        messages.set(member.conversationId, []);
        demoTeamListeners.forEach((listener) => listener(structuredClone(demoBots)));
        return structuredClone(member);
      },
      update: async (id, request) => {
        const current = demoBots.find((member) => member.id === id);
        if (!current) throw new Error('Unknown Team member');
        const updated: BotDto = {
          ...current,
          ...request,
          hostName: request.hostId
            ? demoHosts.find((host) => host.hostId === request.hostId)?.deviceName ?? current.hostName
            : current.hostName,
          deviceType: request.hostId
            ? demoHosts.find((host) => host.hostId === request.hostId)?.deviceType ?? current.deviceType
            : current.deviceType,
          profileName: request.profileId
            ? demoTeamProfileOptions().find((profile) => profile.id === request.profileId)?.name ?? 'Missing profile'
            : current.profileName,
          updatedAt: new Date().toISOString(),
        };
        demoBots = demoBots.map((member) => member.id === id ? updated : member);
        demoTeamListeners.forEach((listener) => listener(structuredClone(demoBots)));
        return structuredClone(updated);
      },
      markRead: async (id) => {
        const current = demoBots.find((member) => member.id === id);
        if (!current) throw new Error('Unknown Team member');
        current.unread = false;
        demoTeamListeners.forEach((listener) => listener(structuredClone(demoBots)));
        return structuredClone(current);
      },
      remove: async (id) => {
        const member = demoBots.find((candidate) => candidate.id === id);
        if (!member) return false;
        demoBots = demoBots.filter((candidate) => candidate.id !== id);
        demoTeamGroups = demoTeamGroups.map((group) => ({...group, memberIds: group.memberIds.filter((memberId) => memberId !== id)}));
        messages.delete(member.conversationId);
        demoTeamListeners.forEach((listener) => listener(structuredClone(demoBots)));
        demoTeamGroupListeners.forEach((listener) => listener(structuredClone(demoTeamGroups)));
        return true;
      },
      send: async (request) => {
        const target = demoBots.find((member) => member.id === request.to || member.name.toLowerCase() === request.to.toLowerCase());
        if (!target) throw new Error('Unknown Team member');
        const sourceConversation = request.fromConversationId ?? 'welcome';
        const source = demoBots.find((member) => member.id === request.fromMemberId);
        const created = message(crypto.randomUUID(), target.conversationId, 'tool', request.text, Date.now(), null, {
          agentRelay: {source: {
            kind: source ? 'team' : 'assistant', memberId: source?.id ?? null,
            conversationId: source?.conversationId ?? sourceConversation,
            name: source?.name ?? conversations.find((item) => item.id === sourceConversation)?.title ?? 'Assistant',
            role: source?.role ?? null, avatar: source?.avatar ?? null,
            traceId: crypto.randomUUID(), hop: 0, automatic: request.automatic === true,
          }},
        } as unknown as JsonValue);
        messages.set(target.conversationId, [...(messages.get(target.conversationId) ?? []), created]);
        target.preview = request.text;
        target.updatedAt = created.createdAt;
        target.unread = true;
        demoTeamListeners.forEach((listener) => listener(structuredClone(demoBots)));
        return structuredClone(created);
      },
      startComputer: async (id) => {
        const member = demoBots.find((candidate) => candidate.id === id);
        if (!member) throw new Error('Unknown Team member');
        member.computer = {...member.computer, state: 'running', detail: null};
        return structuredClone(member);
      },
      stopComputer: async (id) => {
        const member = demoBots.find((candidate) => candidate.id === id);
        if (!member) throw new Error('Unknown Team member');
        member.computer = {...member.computer, state: 'stopped', detail: null};
        return structuredClone(member);
      },
      leases: async () => [],
      grantLease: async (id, capabilities, minutes = 15) => ({
        id: crypto.randomUUID(), memberId: id, capabilities,
        createdAt: new Date().toISOString(), expiresAt: new Date(Date.now() + minutes * 60_000).toISOString(),
      }),
      revokeLease: async () => true,
      host: async () => structuredClone(demoHosts.find((host) => host.isDefault) ?? demoHosts[0]),
      hosts: async () => structuredClone(demoHosts),
      beginHostPairing: async (_preserveFailures) => {
        demoHosts = demoHosts.map((host) => host.mode === 'local'
          ? {...host, pairingExpiresAt: new Date(Date.now() + 5 * 60_000).toISOString()}
          : host);
        return structuredClone(demoHosts.find((host) => host.mode === 'local')!);
      },
      pairHost: async (request) => {
        demoHosts = demoHosts.map((host) => ({...host, isDefault: false}));
        const remote: TeamHostDto = {
          mode: 'remote', state: 'connected', endpoint: request.endpoint, hostId: 'demo-remote-host', desktopId: 'demo-desktop',
          deviceName: 'Home Mac mini', fingerprint: '7fe2 a901 35bc 101d', pairedAt: new Date().toISOString(), detail: null, isDefault: true,
        };
        demoHosts = [...demoHosts.filter((host) => host.hostId !== remote.hostId), remote];
        return structuredClone(remote);
      },
      useLocalHost: async () => {
        demoHosts = demoHosts.map((host) => ({...host, isDefault: host.mode === 'local'}));
        return structuredClone(demoHosts.find((host) => host.isDefault)!);
      },
      setDefaultHost: async (hostId) => {
        demoHosts = demoHosts.map((host) => ({...host, isDefault: host.hostId === hostId}));
        const selected = demoHosts.find((host) => host.isDefault);
        if (!selected) throw new Error('Unknown Host');
        return structuredClone(selected);
      },
      removeHost: async (hostId) => {
        if (demoBots.some((member) => member.hostId === hostId)) throw new Error("Move this Host's bots elsewhere before removing it.");
        demoHosts = demoHosts.filter((host) => host.hostId !== hostId);
        if (!demoHosts.some((host) => host.isDefault)) demoHosts[0].isDefault = true;
        return structuredClone(demoHosts);
      },
      resetHostPairing: async () => ({
        mode: 'local', state: 'local', endpoint: null, hostId: 'demo-host', desktopId: 'demo-desktop',
        deviceName: 'This Mac', deviceType: 'laptop', fingerprint: 'a22f 91bc 3780 552d', pairedAt: null, detail: null,
        listeningEndpoint: 'https://connect.polymux.com/h/86c92dd5-5042-4aa4-a33f-b656bf641e28', pairingCode: 'N8W3H6Y2K',
        pairingExpiresAt: new Date(Date.now() + 5 * 60_000).toISOString(), pairedDesktopName: null,
      }),
      subscribeHost() {
        return () => {};
      },
      subscribeHosts() {
        return () => {};
      },
      subscribe(listener) {
        demoTeamListeners.add(listener);
        return () => demoTeamListeners.delete(listener);
      },
      subscribeGroups(listener) {
        demoTeamGroupListeners.add(listener);
        return () => demoTeamGroupListeners.delete(listener);
      },
    },
    runs: {
      start: async (request) => startDemoRun(request),
      cancel: async (runId) => finishDemoRun(runId, 'run.cancelled'),
      steer: async (runId, text, messageId) => {
        const conversationId = runConversations.get(runId);
        if (!conversationId) throw new Error(`Run is not active: ${runId}`);
        const items = messages.get(conversationId) ?? [];
        items.push(message(messageId ?? crypto.randomUUID(), conversationId, 'user', text, Date.now(), runId));
        messages.set(conversationId, items);
        emit(runId, conversationId, 'steer.accepted', {message: {role: 'user', content: text}});
      },
      events: async () => [],
      subscribe(listener) { listeners.add(listener); return () => listeners.delete(listener); },
    },
    activity: {
      preview: async (request) => {
        const browser = request.kind === 'browser';
        const label = browser ? 'Browser' : 'Computer';
        const accent = browser ? '#b9d4ff' : '#d7c7ff';
        const frame = `<svg xmlns="http://www.w3.org/2000/svg" width="720" height="450" viewBox="0 0 720 450"><rect width="720" height="450" fill="#fff"/><rect x="34" y="32" width="194" height="20" rx="5" fill="${accent}"/><rect x="34" y="72" width="590" height="12" rx="5" fill="#dededb"/><rect x="34" y="96" width="498" height="12" rx="5" fill="#e8e8e5"/><rect x="34" y="146" width="292" height="204" rx="10" fill="#f0f0ed"/><rect x="354" y="146" width="270" height="17" rx="5" fill="#dededb"/><rect x="354" y="178" width="218" height="12" rx="5" fill="#e8e8e5"/><rect x="354" y="212" width="246" height="12" rx="5" fill="#e8e8e5"/><text x="34" y="408" font-family="-apple-system, BlinkMacSystemFont, sans-serif" font-size="13" fill="#777">${label} live content</text></svg>`;
        return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(frame)}`;
      },
    },
    manager: {
      snapshot: async () => ({enabled: false, jobs: []}),
      enqueue: async () => { throw new Error('Manager scheduler is unavailable in the demo'); },
      cancel: async () => { throw new Error('Manager scheduler is unavailable in the demo'); },
      reprioritize: async () => { throw new Error('Manager scheduler is unavailable in the demo'); },
      reorder: async () => { throw new Error('Manager scheduler is unavailable in the demo'); },
      subscribe: () => () => {},
    },
    workspace: {
      snapshot: async (conversationId) => demoWorkspaceSnapshots.get(conversationId) ?? null,
      saveSnapshot: async (conversationId, snapshot) => {
        demoWorkspaceSnapshots.set(conversationId, structuredClone(snapshot));
      },
      // The demo has no host to grant a file with, and a browser tab could not
      // read one anyway: whatever it is handed is already loadable, or nothing.
      preview: async (path) => path,
      saveAs: async (url) => {
        const name = decodeURIComponent(url.split('/').pop() ?? 'download');
        const link = document.createElement('a');
        link.href = url;
        link.download = name;
        link.click();
        return name;
      },
      pick: async () => {
        const file = await new Promise<File | null>((resolve) => {
          const input = document.createElement('input');
          input.type = 'file';
          input.accept = 'image/*,video/*,.avif,.bmp,.gif,.heic,.heif,.jpeg,.jpg,.png,.svg,.webp,.m4v,.mov,.mp4,.ogv,.webm';
          input.addEventListener('change', () => resolve(input.files?.[0] ?? null), {once: true});
          input.addEventListener('cancel', () => resolve(null), {once: true});
          input.click();
        });
        if (!file) return null;
        return {url: URL.createObjectURL(file), name: file.name};
      },
      // Nothing drives the agent in the demo, so nothing ever asks to be shown.
      // The demo has no agent to ask for a surface, so the reveal channel is
      // driven from the page instead: `window.polymuxDemoReveal(request)` fans
      // out to whoever subscribed, which is how the workspace's reveal and
      // draft-prefill behaviour is reachable in the browser build.
      subscribeReveal: (listener) => {
        demoRevealListeners.add(listener);
        return () => demoRevealListeners.delete(listener);
      },
    },
    goals: {
      execute: async (request) => {
        if (request.action === 'clear') { goals.delete(request.conversationId); return null; }
        const current = goals.get(request.conversationId);
        if (request.action === 'view') return current ?? null;
        if (request.action === 'create') {
          const created = goal(request.conversationId, request.objective!);
          goals.set(request.conversationId, created);
          return created;
        }
        if (!current) throw new Error('No goal exists for this conversation');
        const updated = request.action === 'update'
          ? {...current, objective: request.objective!, updatedAt: new Date().toISOString()}
          : {...current, status: request.action === 'pause' ? 'paused' as const : 'active' as const, updatedAt: new Date().toISOString()};
        goals.set(request.conversationId, updated);
        return updated;
      },
      get: async (conversationId) => goals.get(conversationId) ?? null,
    },
    /**
     * The demo keeps schedules in memory and never fires one: the clock and
     * the agent both live in the main process. It seeds a row per state so the
     * view — greyed-out finished rows, the unread dot, the detail panel — can
     * be worked on in a browser.
     */
    schedules: (() => {
      const hour = 3_600_000;
      let items: ScheduleDto[] = demoSchedules(now, hour);
      const listeners = new Set<(items: ScheduleDto[]) => void>();
      const publish = () => { for (const listener of listeners) listener(items); };
      const patch = (id: string, change: (item: ScheduleDto) => ScheduleDto) => {
        items = items.map((item) => item.id === id ? change(item) : item);
        publish();
        const found = items.find((item) => item.id === id);
        if (!found) throw new Error(`Schedule not found: ${id}`);
        return found;
      };
      return {
        list: async () => items,
        create: async (input) => {
          const created: ScheduleDto = {
            id: crypto.randomUUID(),
            title: input.title,
            prompt: input.prompt,
            frequency: input.frequency,
            status: 'active',
            createdAt: Date.now(),
            nextRunAt: Date.now() + hour,
            history: [],
            unread: false,
          };
          items = [...items, created];
          publish();
          return created;
        },
        update: async (id, change) => patch(id, (item) => ({
          ...item,
          ...change,
          status: change.status ?? item.status,
        })),
        remove: async (id) => { items = items.filter((item) => item.id !== id); publish(); },
        runNow: async (id) => patch(id, (item) => ({...item, status: 'running', lastRunAt: Date.now()})),
        markRead: async (id) => patch(id, (item) => ({...item, unread: false})),
        subscribe(listener) {
          listeners.add(listener);
          return () => listeners.delete(listener);
        },
      };
    })(),
    calendar: (() => {
      const day = 86_400_000;
      const today = new Date();
      today.setHours(0, 0, 0, 0);
      const calendars: CalendarListDto[] = [
        {id: 'demo-personal', title: 'Personal', color: '#e05a4f', editable: true, subscribed: false, source: {id: 'demo-icloud', title: 'iCloud', kind: 'icloud'}},
        {id: 'demo-exchange', title: 'University', color: '#4b82d0', editable: true, subscribed: false, source: {id: 'demo-google', title: 'Google', kind: 'google'}},
        {id: 'demo-birthdays', title: 'Birthdays', color: '#8d6ac8', editable: false, subscribed: true, source: {id: 'demo-other', title: 'Other', kind: 'birthdays'}},
      ];
      let events: CalendarEventDto[] = [
        {id: 'demo-event-1', calendarId: 'demo-exchange', title: 'CS4234 Lecture', start: new Date(today.getTime() + day + 10 * 3_600_000).toISOString(), end: new Date(today.getTime() + day + 12 * 3_600_000).toISOString(), allDay: false, location: 'COM3-01-20', notes: 'Approximation algorithms', availability: 'busy', attendees: [], editable: true},
        {id: 'demo-event-2', calendarId: 'demo-personal', title: 'Polymux calendar review', start: new Date(today.getTime() + 2 * day + 14 * 3_600_000).toISOString(), end: new Date(today.getTime() + 2 * day + 15.5 * 3_600_000).toISOString(), allDay: false, alarmMinutes: 15, availability: 'busy', attendees: [], editable: true},
        {id: 'demo-event-3', calendarId: 'demo-personal', title: 'Exchange planning', start: new Date(today.getTime() + 4 * day).toISOString(), end: new Date(today.getTime() + 5 * day).toISOString(), allDay: true, availability: 'free', attendees: [], editable: true},
        {id: 'demo-event-4', calendarId: 'demo-birthdays', title: 'Percival’s Birthday', start: new Date(today.getTime() + 7 * day).toISOString(), end: new Date(today.getTime() + 8 * day).toISOString(), allDay: true, recurrence: {frequency: 'yearly', interval: 1}, availability: 'free', attendees: [], editable: false},
      ];
      const listeners = new Set<() => void>();
      const publish = () => { for (const listener of listeners) listener(); };
      const eventsInRange = (start: string, end: string, ids?: string[]) => {
        const from = Date.parse(start);
        const until = Date.parse(end);
        return events.filter((event) => Date.parse(event.start) < until && Date.parse(event.end) > from && (!ids || ids.includes(event.calendarId)));
      };
      return {
        snapshot: async (start, end) => ({
          calendars: structuredClone(calendars),
          events: structuredClone(eventsInRange(start, end)),
          fetchedAt: new Date().toISOString(),
        }),
        calendars: async () => structuredClone(calendars),
        events: async (start, end, ids) => structuredClone(eventsInRange(start, end, ids)),
        create: async (input) => {
          const created: CalendarEventDto = {...input, id: crypto.randomUUID(), recurrence: input.recurrence ?? undefined, alarmMinutes: input.alarmMinutes ?? undefined, availability: input.availability ?? 'busy', attendees: [], editable: true};
          events = [...events, created];
          publish();
          return structuredClone(created);
        },
        update: async (id, change) => {
          events = events.map((event) => event.id === id ? {
            ...event,
            ...change,
            location: change.location === null ? undefined : change.location ?? event.location,
            notes: change.notes === null ? undefined : change.notes ?? event.notes,
            url: change.url === null ? undefined : change.url ?? event.url,
            recurrence: change.recurrence === null ? undefined : change.recurrence ?? event.recurrence,
            alarmMinutes: change.alarmMinutes === null ? undefined : change.alarmMinutes ?? event.alarmMinutes,
          } : event);
          const found = events.find((event) => event.id === id);
          if (!found) throw new Error(`Event not found: ${id}`);
          publish();
          return structuredClone(found);
        },
        remove: async (id) => { events = events.filter((event) => event.id !== id); publish(); },
        importFile: async () => ({imported: 0, skipped: 0, fileName: null}),
        exportFile: async () => null,
        openAccounts: async () => {},
        subscribe(listener) {
          listeners.add(listener);
          return () => listeners.delete(listener);
        },
      };
    })(),
    tasks: (() => {
      let cards: import('@polymux/protocol').TaskCardDto[] = [
        {id: crypto.randomUUID(), chatId: 'demo', title: 'Research competitor features', status: 'todo', reviewed: false, order: 0, createdAt: Date.now(), updatedAt: Date.now()},
        {id: crypto.randomUUID(), chatId: 'demo', title: 'Draft marketing copy', status: 'in_progress', owner: 'agent-1', reviewed: false, order: 0, createdAt: Date.now(), updatedAt: Date.now()},
        {id: crypto.randomUUID(), chatId: 'demo', title: 'Update landing page', status: 'done', reviewed: false, order: 0, createdAt: Date.now(), updatedAt: Date.now()},
        {id: crypto.randomUUID(), chatId: 'demo', title: 'Fix login bug', status: 'done', reviewed: true, order: 1, createdAt: Date.now(), updatedAt: Date.now()},
      ];
      const listeners = new Set<(items: typeof cards) => void>();
      const publish = () => { for (const l of listeners) l(cards); };
      return {
        list: async (chatId: string) => cards.filter((card) => card.chatId === chatId),
        create: async (input: import('@polymux/protocol').TaskCardInput) => {
          const card: import('@polymux/protocol').TaskCardDto = {
            id: crypto.randomUUID(), chatId: input.chatId, title: input.title, detail: input.detail,
            status: 'todo', reviewed: false, order: cards.length,
            createdAt: Date.now(), updatedAt: Date.now(),
          };
          cards = [...cards, card]; publish(); return card;
        },
        update: async (id: string, patch: import('@polymux/protocol').TaskCardPatch) => {
          cards = cards.map((c) => c.id === id ? {...c, ...patch, updatedAt: Date.now()} : c);
          publish();
          const found = cards.find((c) => c.id === id);
          if (!found) throw new Error(`Card not found: ${id}`);
          return found;
        },
        remove: async (id: string) => { cards = cards.filter((c) => c.id !== id); publish(); },
        markRead: async (id: string) => {
          cards = cards.map((c) => c.id === id ? {...c, reviewed: true, updatedAt: Date.now()} : c);
          publish();
          const found = cards.find((c) => c.id === id);
          if (!found) throw new Error(`Card not found: ${id}`);
          return found;
        },
        subscribe(listener: (items: typeof cards) => void) {
          listeners.add(listener);
          return () => listeners.delete(listener);
        },
      };
    })(),
    files: {paths: async (files) => files.map((file) => file.name)},
    resources: {
      artifacts: async (conversationId) => demoArtifacts(conversationId),
      references: async (conversationId) => [...demoReferences(conversationId), ...(addedReferences.get(conversationId) ?? [])],
      addFiles: async (conversationId, files) => {
        const created = files.map((file): ReferenceDto => ({
          id: crypto.randomUUID(),
          conversationId,
          runId: null,
          kind: 'file',
          title: file.name,
          uri: file.name,
          createdAt: new Date().toISOString(),
          metadata: {mimeType: file.type || null, size: file.size},
        }));
        addedReferences.set(conversationId, [...(addedReferences.get(conversationId) ?? []), ...created]);
        return created;
      },
      subscribe: () => () => {},
    },
    memory: {
      status: async () => ({
        enabled: demoMemoryEnabled,
        directory: '/demo/memories',
        storedBytes: 18_240,
        registryPath: '/demo/memories/MEMORY.md',
        summaryPath: '/demo/memories/memory_summary.md',
        memories: 2,
        userMemories: 2,
        conversationMemories: 0,
        latestMemoryAt: new Date().toISOString(),
        consolidatedAt: new Date().toISOString(),
        consolidationError: null,
        consolidationRetryAfter: null,
        pendingMemories: 0,
      }),
      setEnabled: async (enabled) => {
        demoMemoryEnabled = enabled;
        return {...await api.memory.status(), enabled};
      },
      entries: async () => [
        {id: 'memory-demo-1', scope: 'user', kind: 'preference', content: 'Prefers concise, cohesive explanations with room to ask deeper questions.', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString()},
        {id: 'memory-demo-2', scope: 'user', kind: 'project', content: 'Polymux is a context-aware desktop assistant with manager-style delegation.', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString()},
      ],
    },
    computerHistory: {
      status: async () => demoComputerHistoryStatus(),
      setEnabled: async (enabled) => {
        demoComputerHistoryEnabled = enabled;
        return demoComputerHistoryStatus();
      },
      update: async (patch) => {
        demoComputerHistorySettings = {...demoComputerHistorySettings, ...patch};
        return demoComputerHistoryStatus();
      },
      forget: async () => demoComputerHistoryStatus(),
      removeEntry: async () => demoComputerHistoryStatus(),
      revealEntry: async () => {},
      entries: async (options) => demoComputerHistoryEntries.filter((entry) => {
        const at = Date.parse(entry.capturedAt);
        return (!options?.since || at >= Date.parse(options.since)) &&
          (!options?.until || at <= Date.parse(options.until));
      }).slice(0, options?.limit ?? demoComputerHistoryEntries.length),
      activities: async (options) => demoComputerHistoryActivities.filter((activity) => {
        const at = Date.parse(activity.startedAt);
        return (!options?.since || at >= Date.parse(options.since)) &&
          (!options?.until || at <= Date.parse(options.until));
      }).slice(0, options?.limit ?? demoComputerHistoryActivities.length),
      pickApp: async () => null,
      appIcon: async () => null,
    },
    comms: {
      status: async () => demoCommsStatus,
      weChatLogin: async () => structuredClone(demoWeChatLogin),
      weChatOpen: async () => {},
      // The demo's data is already in memory, so there is nothing on disk for
      // a snapshot to be: it hands back an empty one and the demo hub fetches
      // as it always did.
      snapshot: async () => ({status: null, chats: [], mailboxes: [], mail: [], messages: []}),
      refresh: async () => demoCommsStatus,
      // Nothing to start in a browser tab; the demo's bridges are always up.
      wake: async (platform) => {
        demoWakeCalls.push(platform);
        if (platform === 'wechat' && demoWeChatWakeGate) await demoWeChatWakeGate;
        return {
          platform,
          ready: platform !== 'wechat' || demoWeChatWakeReady,
          status: demoCommsStatus,
        };
      },
      setHubUrl: async (baseUrl) => {
        demoCommsStatus.hub.baseUrl = baseUrl;
        return demoCommsStatus;
      },
      connect: async () => {
        demoCommsStatus.hub = {...demoCommsStatus.hub, status: 'signed-in', userId: '@polymux-demo:localhost', canAutoConnect: false};
        return demoCommsStatus;
      },
      signIn: async (userId) => {
        demoCommsStatus.hub = {...demoCommsStatus.hub, status: 'signed-in', userId, error: null};
        return demoCommsStatus;
      },
      signOut: async () => {
        demoCommsStatus.hub = {...demoCommsStatus.hub, status: 'reachable', userId: null, canAutoConnect: true};
        return demoCommsStatus;
      },
      // Each flow id opens with the step shape its real bridge uses, so the
      // preview walks the same screens the packaged app does.
      loginStart: async (platform, flowId) => {
        // A real bridge takes a beat to produce its first step; without that
        // beat here the preview never shows the waiting state that stands in
        // for it.
        await new Promise((resolve) => setTimeout(resolve, 500));
        if (flowId === 'qr')
          return {type: 'display_and_wait', loginId: 'demo', stepId: 'qr', instructions: 'Scan this from your phone.', display: 'qr', data: `https://example.com/pair/${platform}`, imageUrl: null};
        if (flowId === 'phone')
          return {type: 'user_input', loginId: 'demo', stepId: `phone:${platform}`, instructions: null, fields: [{id: 'phone', type: 'phone_number', name: 'Phone number', description: 'With country code, e.g. +61 400 000 000.', pattern: '^\\+?[0-9 ]{6,}$'}]};
        if (flowId === 'token' || flowId === 'bot')
          return {type: 'user_input', loginId: 'demo', stepId: 'token', instructions: null, fields: [{id: 'token', type: 'token', name: 'Token', description: null, pattern: null}]};
        if (flowId === 'password')
          return {type: 'user_input', loginId: 'demo', stepId: 'password', instructions: null, fields: [{id: 'username', type: 'username', name: 'Handle', description: 'e.g. you.bsky.social', pattern: null}, {id: 'password', type: 'password', name: 'App password', description: null, pattern: null}]};
        if (flowId === 'local')
          return {type: 'display_and_wait', loginId: 'demo', stepId: 'local', instructions: 'Reading Messages on this Mac…', display: 'nothing', data: null, imageUrl: null};
        // Everything else in the fleet signs in on the network's own site.
        return {type: 'cookies', loginId: 'demo', stepId: 'cookies', instructions: null, url: 'https://example.com/login', waitForUrl: null, userAgent: null, fields: []};
      },
      loginSubmit: async (platform, _loginId, stepId) => {
        // WhatsApp answers a phone number with a code to type on the phone;
        // Telegram answers it by sending a login code to the app.
        if (stepId === 'phone:whatsapp')
          return {type: 'display_and_wait', loginId: 'demo', stepId: 'pairing', instructions: 'Type this code into WhatsApp on your phone.', display: 'code', data: 'GRWM-K2FH', imageUrl: null};
        if (stepId.startsWith('phone:'))
          return {type: 'user_input', loginId: 'demo', stepId: 'code', instructions: 'Telegram sent a login code to your other devices.', fields: [{id: 'code', type: '2fa_code', name: 'Login code', description: null, pattern: '^[0-9]{5,6}$'}]};
        demoMarkLinked(platform);
        return {type: 'complete', loginId: 'demo', accountId: 'demo', accountName: 'Demo account'};
      },
      // The real endpoint blocks until the remote side scans, and the QR stays
      // on screen for as long as it does; resolving instantly would make the
      // demo flash past the step it is meant to show.
      loginWait: async (platform, _loginId, stepId) => {
        const before = demoLoginCancelled;
        await new Promise((resolve) => setTimeout(resolve, stepId === 'local' ? 3000 : 20_000));
        if (demoLoginCancelled !== before) throw new Error('The login was cancelled.');
        demoMarkLinked(platform);
        return {type: 'complete', loginId: 'demo', accountId: 'demo', accountName: 'Demo account'};
      },
      loginCookies: async (platform) => {
        demoMarkLinked(platform);
        return {type: 'complete', loginId: 'demo', accountId: 'demo', accountName: 'Demo account'};
      },
      loginCancel: async () => {
        demoLoginCancelled += 1;
        return demoCommsStatus;
      },
      bridgeLogout: async (platform, accountId) => {
        demoCommsStatus.bridges = demoCommsStatus.bridges.map((bridge) => {
          if (bridge.platform !== platform) return bridge;
          const accounts = bridge.accounts.filter((account) => account.id !== accountId);
          return {...bridge, accounts, state: accounts.length > 0 ? 'connected' : 'logged-out'};
        });
        return demoCommsStatus;
      },
      bridgeSetup: async (platform, values) => {
        demoCommsStatus.bridges = demoCommsStatus.bridges.map((bridge) =>
          bridge.platform === platform && bridge.setup
            ? {
                ...bridge,
                setup: {
                  ...bridge.setup,
                  configured: bridge.setup.fields.every((field) => Boolean(values[field.id])),
                },
              }
            : bridge,
        );
        return demoCommsStatus;
      },
      chats: async () => {
        demoChatReads += 1;
        return demoChats;
      },
      chatContacts: async () => demoChats
        .filter((chat) => !chat.group && !chat.space)
        .map((chat) => ({
          id: `${chat.platform}:demo:${chat.id}`,
          remoteId: chat.id,
          name: chat.name,
          platform: chat.platform as import('@polymux/protocol').CommsPlatform,
          accountId: 'demo',
          accountName: 'Demo account',
          avatarUrl: chat.avatarUrl ?? null,
          identifiers: [],
          chatId: chat.id,
          accounts: [{
            accountId: 'demo',
            accountName: 'Demo account',
            remoteId: chat.id,
            chatId: chat.id,
          }],
        })),
      chatMembers: async (chatId) => chatId === '!tg-devs:local'
        ? [
            {userId: '@telegram_priya:local', name: 'Priya', avatarUrl: null},
            {userId: '@telegram_manny:local', name: 'Manny Asbanu', avatarUrl: null},
            {userId: '@telegram_pp_ll:local', name: 'Pp Ll', avatarUrl: null},
          ]
        : chatId === '!wa-family:local'
          ? [
              {userId: '@whatsapp_mum:local', name: 'Mum', avatarUrl: null},
              {userId: '@whatsapp_dad:local', name: 'Dad', avatarUrl: null},
            ]
          : [],
      contactLinks: async () => structuredClone(demoContactLinks),
      chatGroupInfo: async (chatId) => {
        if (demoWeChatGroup.readDelayMs) await new Promise(resolve => setTimeout(resolve, demoWeChatGroup.readDelayMs));
        const chat = demoChats.find(chat => chat.id === chatId && chat.platform === 'wechat' && chat.group);
        if (!chat) throw new Error('This WeChat group is unavailable');
        return {name: chat.name, isMember: demoWeChatGroup.isMember};
      },
      chatRenameGroup: async (chatId, name, expectedName) => {
        if (demoWeChatGroup.renameDelayMs) await new Promise(resolve => setTimeout(resolve, demoWeChatGroup.renameDelayMs));
        if (demoWeChatGroup.renameError) throw new Error(demoWeChatGroup.renameError);
        const chat = demoChats.find(chat => chat.id === chatId && chat.platform === 'wechat' && chat.group);
        if (!chat) throw new Error('This WeChat group is unavailable');
        if (!name.trim()) throw new Error('Enter a group name');
        if (chat.name !== expectedName) throw new Error('The group name changed. Reload it and try again.');
        chat.name = name.trim();
        demoWeChatGroup.name = chat.name;
        return {name: chat.name, isMember: true};
      },
      contactLinkMerge: async (request) => {
        const overlapping = demoContactLinks.filter((link) => link.members.some((member) =>
          request.members.some((candidate) => candidate.platform === member.platform &&
            ((candidate.remoteId && member.remoteId && candidate.remoteId === member.remoteId) ||
              candidate.chatId === member.chatId))));
        const at = new Date().toISOString();
        const link: ContactLinkDto = {
          id: `contact-${crypto.randomUUID()}`,
          name: request.name,
          members: mergeDemoContactLinkMembers(overlapping, request.members),
          createdAt: overlapping[0]?.createdAt ?? at,
          updatedAt: at,
        };
        const removed = new Set(overlapping.map((item) => item.id));
        demoContactLinks = [link, ...demoContactLinks.filter((item) => !removed.has(item.id))];
        return structuredClone(link);
      },
      contactRename: async (request) => {
        const overlapping = demoContactLinks.filter((link) =>
          link.members.some((member) => sameDemoContactMember(member, request.member)));
        const at = new Date().toISOString();
        const link: ContactLinkDto = {
          id: `contact-${crypto.randomUUID()}`,
          name: request.name.trim(),
          members: mergeDemoContactLinkMembers(overlapping, [request.member]),
          createdAt: overlapping[0]?.createdAt ?? at,
          updatedAt: at,
        };
        const removed = new Set(overlapping.map((item) => item.id));
        demoContactLinks = [link, ...demoContactLinks.filter((item) => !removed.has(item.id))];
        return structuredClone(link);
      },
      contactLinkRemove: async (id) => {
        demoContactLinks = demoContactLinks.filter((link) => link.id !== id);
      },
      chatCreate: async (request) => {
        const existing = request.participantIds.length === 1
          ? demoChats.find((chat) => chat.id === request.participantIds[0])
          : undefined;
        if (existing) return existing.id;
        const id = `demo-${crypto.randomUUID()}`;
        demoChats = [{
          id,
          name: request.name ?? 'New conversation',
          platform: request.platform,
          accountIds: [request.accountId],
          unread: 0,
          lastActivity: null,
          preview: null,
          group: request.participantIds.length > 1,
        }, ...demoChats];
        return id;
      },
      broadcasts: async () => demoBroadcasts,
      broadcastCreate: async (request) => {
        const at = new Date().toISOString();
        const broadcast: BroadcastDto = {
          id: `broadcast-${crypto.randomUUID()}`,
          name: request.name.trim(),
          recipients: structuredClone(request.recipients),
          createdAt: at,
          updatedAt: at,
          lastActivity: null,
          preview: null,
        };
        demoBroadcasts = [broadcast, ...demoBroadcasts];
        demoBroadcastMessages.set(broadcast.id, []);
        return broadcast;
      },
      broadcastMessages: async (broadcastId) => demoBroadcastMessages.get(broadcastId) ?? [],
      broadcastSend: async (broadcastId, text) => {
        const broadcast = demoBroadcasts.find((item) => item.id === broadcastId);
        if (!broadcast) throw new Error('This broadcast no longer exists.');
        const sentAt = new Date().toISOString();
        const recipients = broadcast.recipients.map((recipient) => ({
          ...recipient,
          chatId: recipient.chatId ?? `demo-direct-${recipient.id}`,
        }));
        const message: BroadcastMessageDto = {
          id: `broadcast-message-${crypto.randomUUID()}`,
          broadcastId,
          body: text,
          sentAt,
          deliveries: recipients.map((recipient) => ({
            recipientId: recipient.id,
            recipientName: recipient.name,
            platform: recipient.platform,
            chatId: recipient.chatId,
            status: 'sent',
          })),
        };
        const updated = {...broadcast, recipients, updatedAt: sentAt, lastActivity: sentAt, preview: text};
        demoBroadcasts = demoBroadcasts.map((item) => item.id === broadcastId ? updated : item);
        demoBroadcastMessages.set(broadcastId, [message, ...(demoBroadcastMessages.get(broadcastId) ?? [])]);
        return {broadcast: updated, message};
      },
      chatMarkRead: async (chatId) => {
        if (demoGeneral.hubIncognitoMode) return false;
        demoChats = demoChats.map((chat) => (chat.id === chatId ? {...chat, unread: 0} : chat));
        return true;
      },
      chatMessages: async (chatId) => ({
        // Newest first, the way the real hub answers — the thread is drawn in
        // that order, and a demo that hands them back the other way round
        // exercises a view nobody ever sees.
        messages: demoChatMessages
          .filter((item) => item.chatId === chatId)
          .sort((a, b) => Date.parse(b.sentAt) - Date.parse(a.sentAt)),
        // The demo holds one page; there is never an older one to walk to.
        nextBefore: null,
      }),
      chatSend: async (chatId, text, replyTo) => {
        const gate = demoChatSendGate;
        if (gate) await gate;
        demoChatActions.push({kind: 'text', chatId, text, replyTo: replyTo ?? null});
        const sent: ChatMessageDto = {id: crypto.randomUUID(), chatId, sender: 'You', body: text, sentAt: new Date().toISOString(), mine: true, replyTo: replyTo ?? null, reactions: []};
        if (demoNextDeliveryUnconfirmed) {
          sent.deliveryStatus = 'unconfirmed';
          demoNextDeliveryUnconfirmed = false;
        }
        demoChatMessages = [sent, ...demoChatMessages];
        return sent;
      },
      // The demo has no homeserver to upload to and no microphone to open, so
      // file and voice payloads are recorded for renderer interaction tests.
      chatSendFiles: async (chatId, files) => {
        demoChatActions.push({kind: 'files', chatId, files: [...files]});
      },
      chatPickFiles: async () => {
        const gate = demoChatPickGate;
        if (gate) await gate;
        return [];
      },
      chatSendAudio: async (chatId, bytes, mimetype) => {
        demoChatActions.push({kind: 'audio', chatId, mimetype, size: bytes.byteLength});
      },
      chatStickers: async () => {
        if (demoStickerGate) await demoStickerGate;
        return demoStickers ?? [{
          id: 'demo-native-sticker',
          url: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7',
          mimeType: 'image/gif',
          size: 42,
          width: 240,
          height: 240,
        }];
      },
      chatSendSticker: async (chatId, stickerId) => {
        demoChatActions.push({kind: 'sticker', chatId, stickerId});
      },
      chatRecall: async (chatId, messageId) => {
        demoChatActions.push({kind: 'recall', chatId, messageId});
        demoChatMessages = demoChatMessages.filter(
          (item) => item.chatId !== chatId || item.id !== messageId,
        );
      },
      chatReact: async (chatId, messageId, key) => {
        const eventId = `demo-${key}`;
        demoAddReaction(
          chatId,
          messageId,
          key,
          {id: '@polymux-demo:local', name: 'You', avatarUrl: null, mine: true},
          eventId,
        );
        return eventId;
      },
      chatUnreact: async (_chatId, reactionId) => {
        demoChatMessages = demoChatMessages.map((item) => ({
          ...item,
          reactions: (item.reactions ?? []).flatMap((entry) => {
            if (entry.mineEventId !== reactionId) return [entry];
            if (entry.count <= 1) return [];
            return [{
              ...entry,
              count: entry.count - 1,
              reactors: entry.reactors?.filter((reactor) => !reactor.mine),
              mineEventId: null,
            }];
          }),
        }));
      },
      mailFolders: async () => demoMailFolders,
      mailEnvelopes: async (request) => {
        const folder = request.folder ?? 'INBOX';
        const needle = request.query?.trim().toLowerCase() ?? '';
        return demoEnvelopes
          .filter((item) => item.folder === folder)
          .filter((item) => !needle || item.envelope.subject.toLowerCase().includes(needle) || item.envelope.from.address.toLowerCase().includes(needle))
          // The demo's bodies stand in for the peek the real listing does.
          .map((item) => ({...item.envelope, preview: item.body.slice(0, 200)}));
      },
      mailMessage: async (id) => {
        const found = demoEnvelopes.find((item) => item.envelope.id === id);
        if (!found) throw new Error(`No message ${id}`);
        const attachments = !found.envelope.hasAttachment
          ? []
          : id === '1'
            ? [
                {id: '2', name: 'q3-report.pdf', mime: 'application/pdf', contentId: 'q3-report', disposition: 'inline' as const, size: demoPdfContent.byteLength},
                {id: '3', name: 'regional-breakdown.csv', mime: 'text/csv', contentId: null, disposition: 'attachment' as const, size: 24},
              ]
            : [{id: '2', name: 'q3-report.pdf', mime: 'application/pdf', contentId: null, disposition: 'attachment' as const, size: demoPdfContent.byteLength}];
        return {id, subject: found.envelope.subject, from: found.envelope.from, to: found.envelope.to ? [found.envelope.to] : [], cc: [], bcc: [], date: found.envelope.date, body: found.body, html: found.html ?? null, attachments, importance: found.envelope.importance, messageId: `<demo-${id}@example.com>`, references: []};
      },
      mailSend: async (request) => {
        demoMailSends.push(structuredClone(request));
        return {};
      },
      mailDelete: async (ids) => {
        demoEnvelopes = demoEnvelopes.filter((item) => !ids.includes(item.envelope.id));
      },
      mailAttachment: async (_id, part) => part === '3'
        ? {id: part, name: 'regional-breakdown.csv', mime: 'text/csv', content: new TextEncoder().encode('region,total\nAPAC,42').buffer}
        : {id: part, name: 'q3-report.pdf', mime: 'application/pdf', content: demoPdfContent},
      mailDownload: async () => ['/tmp/q3-report.pdf', '/tmp/regional-breakdown.csv'],
      mailOpenFile: async () => {},
      mailPickFiles: async () => ['/tmp/demo-attachment.pdf'],
      mailMove: async (ids, target) => {
        demoEnvelopes = demoEnvelopes.map((item) => (ids.includes(item.envelope.id) ? {...item, folder: target} : item));
      },
      mailFlag: async (ids, flag, on) => {
        demoEnvelopes = demoEnvelopes.map((item) =>
          ids.includes(item.envelope.id) ? {...item, envelope: {...item.envelope, [flag === 'seen' ? 'seen' : 'flagged']: on}} : item,
        );
      },
      // Real enough to exercise the UI: several mailboxes can share a
      // provider, and each is edited or removed on its own.
      emailSave: async (request) => {
        const existing = request.originalId ?? request.id;
        const previous = demoCommsStatus.email.accounts.find((item) => item.id === existing);
        const account: CommsEmailAccountDto = {
          id: request.id,
          displayName: request.displayName ?? null,
          email: request.email,
          incoming: {kind: 'imap', host: request.imapHost, port: request.imapPort, encryption: request.imapEncryption, login: request.email, auth: 'password'},
          outgoing: {kind: 'smtp', host: request.smtpHost, port: request.smtpPort, encryption: request.smtpEncryption, login: request.email, auth: 'password'},
          secretStored: true,
          signatures: previous?.signatures ?? [],
          defaultSignatureId: previous?.defaultSignatureId ?? null,
          status: 'ok',
          error: null,
        };
        const accounts = demoCommsStatus.email.accounts.some((item) => item.id === existing)
          ? demoCommsStatus.email.accounts.map((item) => (item.id === existing ? account : item))
          : [...demoCommsStatus.email.accounts, account];
        demoCommsStatus.email = {...demoCommsStatus.email, accounts};
        return demoCommsStatus;
      },
      emailSignaturesSave: async (request) => {
        demoCommsStatus.email = {
          ...demoCommsStatus.email,
          accounts: demoCommsStatus.email.accounts.map((account) =>
            account.id === request.account
              ? {
                  ...account,
                  signatures: request.signatures.map((signature) => ({...signature})),
                  defaultSignatureId: request.defaultSignatureId,
                }
              : account,
          ),
        };
        return demoCommsStatus;
      },
      emailRemove: async (id) => {
        demoCommsStatus.email = {
          ...demoCommsStatus.email,
          accounts: demoCommsStatus.email.accounts.filter((item) => item.id !== id),
        };
        return demoCommsStatus;
      },
      // Nothing to sign in to in a browser: the demo answers with the status
      // it already has, so the button is visible without pretending to work.
      emailSignIn: async () => demoCommsStatus,
      emailTest: async (id) => {
        const account = demoCommsStatus.email.accounts.find((item) => item.id === id);
        if (!account) throw new Error(`No email account named ${id}`);
        return {...account, status: 'ok', error: null};
      },
      subscribe(listener) {
        demoCommsListeners.add(listener);
        return () => demoCommsListeners.delete(listener);
      },
      subscribeActivity: (listener) => {
        demoActivityListeners.add(listener);
        return () => demoActivityListeners.delete(listener);
      },
    },
    mcp: {
      list: async () => demoMcpServers,
      reload: async () => demoMcpServers,
      setEnabled: async (id, enabled) => {
        const item = demoMcpServers.find((candidate) => candidate.id === id);
        if (item) { item.enabled = enabled; item.status = enabled ? 'connected' : 'disconnected'; }
        return demoMcpServers;
      },
      saveCustom: async (request) => {
        const item = demoMcpServers.find((candidate) => candidate.id === request.id);
        const next: McpServerDto = {...request, source: 'polymux', editable: true, enabled: item?.enabled ?? true, status: item?.status ?? 'disconnected', toolNames: item?.toolNames ?? [], resourceUris: [], promptNames: []};
        if (item) Object.assign(item, next); else demoMcpServers.push(next);
        return demoMcpServers;
      },
      removeCustom: async (id) => {
        const index = demoMcpServers.findIndex((candidate) => candidate.id === id && candidate.editable);
        if (index < 0) throw new Error(`MCP server is not removable: ${id}`);
        demoMcpServers.splice(index, 1);
        return demoMcpServers;
      },
      discover: async () => demoDiscoveredMcp.map((group) => ({
        ...group,
        servers: group.servers.map((entry) => ({
          ...entry,
          state: demoMcpServers.some((item) => item.id === entry.id) ? 'loaded' as const : 'available' as const,
        })),
      })),
      adopt: async (groupId, serverId) => {
        const entry = demoDiscoveredMcp.find((group) => group.id === groupId)?.servers.find((item) => item.id === serverId);
        if (!entry) throw new Error(`That MCP server is no longer in ${groupId}`);
        demoMcpServers.push({id: entry.id, name: entry.name, description: entry.description, source: 'polymux', editable: true, enabled: true, transport: entry.transport, status: 'connected', toolNames: [], resourceUris: [], promptNames: [], ...(entry.transport === 'stdio' ? {command: entry.target} : {url: entry.target})});
        return demoMcpServers;
      },
      // One page, and no second one: the demo directory is two rows long, so
      // its cursor is always empty and the list never asks for more.
      searchRegistry: async (query) => ({
        entries: [
          {id: 'io.github/example/files', name: 'Files', description: 'Browse and manage files.', url: 'https://example.com/files/mcp', requiredHeaders: []},
          {id: 'io.github/example/issues', name: 'Issues', description: `Search ${query || 'project'} issues.`, url: 'https://example.com/issues/mcp', repository: 'https://github.com/example/issues', requiredHeaders: ['Authorization']},
        ],
        nextCursor: '',
      }),
      subscribe: () => () => {},
    },
    drive: {
      status: async () => demoDriveStatus,
      refresh: async () => demoDriveStatus,
      // The demo has no OAuth to run, so connecting just flips the provider on
      // — enough to exercise every state the settings tab renders.
      connect: async (provider) => demoDriveConnect(provider, true),
      disconnect: async (provider) => demoDriveConnect(provider, false),
      setSaveOrder: async (order) => {
        demoDriveStatus.saveOrder = order;
        return demoDriveStatus;
      },
      revealEntry: async () => {},
      openEntry: async () => {},
      addShare: async (sharePath, label) => {
        const target = sharePath || '/Volumes/Studio';
        const id = target;
        demoDriveStatus.sources = [
          ...demoDriveStatus.sources.filter((source) => source.id !== `network#${id}`),
          {
            id: `network#${id}`,
            provider: 'network',
            accountId: id,
            name: 'Network',
            accountLabel: label || target.split('/').pop() || target,
            state: 'connected',
            usage: {used: 2_100_000_000_000, total: 8_000_000_000_000, appUsed: 84_000_000},
            root: target,
            error: null,
          },
        ];
        return demoDriveStatus;
      },
      removeShare: async (id) => {
        demoDriveStatus.sources = demoDriveStatus.sources.filter(
          (source) => source.id !== `network#${id}` && source.id !== id,
        );
        return demoDriveStatus;
      },
      setLocalRoot: async (path) => {
        const local = demoDriveStatus.providers.find((entry) => entry.id === 'local');
        if (local) local.root = path ?? '/demo/Polymux';
        return demoDriveStatus;
      },
      saveS3: async (config) => {
        const s3 = demoDriveStatus.providers.find((entry) => entry.id === 's3');
        if (s3) {
          s3.state = 'connected';
          s3.root = config.prefix ? `${config.bucket}/${config.prefix}` : config.bucket;
          s3.accounts = [{id: config.bucket, name: config.bucket, email: null}];
        }
        return demoDriveStatus;
      },
      list: async (source, path) => demoDriveFolder(source, path ?? ''),
      createFolder: async (source, parentPath, name) => {
        const folder = demoDriveFolder(source, parentPath);
        // The same collision the real adapters raise, so the drive's error
        // path is reachable in the demo rather than only in the packaged app.
        if (folder.some((entry) => entry.name === name))
          throw new Error(`A folder named ${name} already exists here.`);
        const entry: DriveEntryDto = {
          id: `${parentPath}/${name}`, name, kind: 'folder', size: null,
          modifiedAt: new Date().toISOString(), provider: parseDriveSourceId(source).provider,
          path: `${parentPath}/${name}`, mimeType: null,
        };
        folder.push(entry);
        return entry;
      },
      // A browser tab has no file picker to open, so the demo stands in with a
      // file of its own rather than doing nothing and looking broken.
      upload: async (source, parentPath) => {
        const entry: DriveEntryDto = {
          id: `${parentPath}/upload-${demoDriveUploads += 1}.png`,
          name: `Uploaded ${demoDriveUploads}.png`, kind: 'file', size: 128_400,
          modifiedAt: new Date().toISOString(), provider: parseDriveSourceId(source).provider,
          path: `${parentPath}/upload-${demoDriveUploads}.png`, mimeType: 'image/png',
        };
        demoDriveFolder(source, parentPath).push(entry);
        return [entry];
      },
      download: async (_source, path) => `/demo/downloads/${path}`,
      remove: async (source, paths) => {
        for (const [key, entries] of demoDriveFolders) {
          if (!key.startsWith(`${source}:`)) continue;
          demoDriveFolders.set(key, entries.filter((entry) => !paths.includes(entry.path)));
        }
      },
      rename: async (source, path, name) => {
        const entry = demoDriveFind(source, path);
        if (entry) entry.name = name;
        return entry ?? {
          id: path, name, kind: 'file', size: null,
          modifiedAt: new Date().toISOString(),
          provider: parseDriveSourceId(source).provider, path, mimeType: null,
        };
      },
      move: async (source, paths, destinationFolder) => paths.map((path) => {
        const entry = demoDriveFind(source, path);
        const moved: DriveEntryDto = {
          ...(entry ?? {
            id: path, name: path.slice(path.lastIndexOf('/') + 1), kind: 'file' as const,
            size: null, modifiedAt: new Date().toISOString(),
            provider: parseDriveSourceId(source).provider, path, mimeType: null,
          }),
          path: `${destinationFolder}/${path.slice(path.lastIndexOf('/') + 1)}`,
        };
        for (const [key, entries] of demoDriveFolders)
          if (key.startsWith(`${source}:`))
            demoDriveFolders.set(key, entries.filter((item) => item.path !== path));
        const destinationKey = `${source}:${destinationFolder}`;
        demoDriveFolders.set(destinationKey, [...(demoDriveFolders.get(destinationKey) ?? []), moved]);
        return moved;
      }),
      copy: async (source, paths) => paths.map((path) => {
        const entry = demoDriveFind(source, path);
        const parent = path.slice(0, path.lastIndexOf('/'));
        const copied: DriveEntryDto = {
          ...(entry ?? {
            id: path, name: 'Item', kind: 'file' as const, size: null,
            modifiedAt: new Date().toISOString(),
            provider: parseDriveSourceId(source).provider, path, mimeType: null,
          }),
          id: `${path}-copy`,
          name: `${entry?.name ?? 'Item'} copy`,
          path: `${path}-copy`,
        };
        demoDriveFolder(source, parent).push(copied);
        return copied;
      }),
      subscribe: () => () => {},
    },
    skills: {
      list: async () => demoSkills,
      reload: async () => demoSkills,
      subscribe: () => () => {},
      install: async (spec) => {
        const name = spec.trim().replace(/\/+$/, '').split('/').pop() ?? 'installed-skill';
        if (demoSkills.some((item) => item.name === name)) throw new Error(`A skill named ${name} already exists`);
        demoSkills.push({name, description: `Installed from ${spec.trim()}.`, source: 'polymux', filePath: `~/.polymux/skills/${name}/SKILL.md`, disableModelInvocation: false, allowedTools: [], permissions: [], enabled: true, editable: true, instructions: `Installed from ${spec.trim()}.`, updatedAt: '2026-08-14T03:00:00.000Z'});
        return demoSkills;
      },
      searchRegistry: async (query, limit = 15) => {
        const directory = [
          {id: 'vercel-labs/agent-skills/vercel-react-best-practices', name: 'vercel-react-best-practices', source: 'vercel-labs/agent-skills', installs: 630_723},
          {id: 'vercel-labs/skills/find-skills', name: 'find-skills', source: 'vercel-labs/skills', installs: 120_345},
          {id: 'vercel-labs/agent-skills/web-design-guidelines', name: 'web-design-guidelines', source: 'vercel-labs/agent-skills', installs: 84_210},
        ];
        const text = query.trim().toLowerCase();
        if (text.length === 1) return [];
        return directory
          .filter((entry) => !text || entry.name.includes(text) || entry.source.includes(text))
          .slice(0, limit);
      },
      // `?one-agent` narrows the scan to a single agent, which is how the one
      // interesting variation of the discovery pane — nothing to survey, so it
      // opens expanded — is reachable from a test.
      discover: async () => (location.search.includes('one-agent')
        ? demoDiscoveredSkills.filter((group) => group.id === 'codex')
        : demoDiscoveredSkills
      ).map((group) => ({
        ...group,
        skills: group.skills.map((entry) => ({
          ...entry,
          state: entry.state === 'loaded' || demoSkills.some((item) => item.name === entry.name)
            ? 'loaded' as const
            : 'available' as const,
        })),
      })),
      adopt: async (path) => {
        const entry = demoDiscoveredSkills.flatMap((group) => group.skills).find((item) => item.path === path);
        if (!entry) throw new Error('That skill is not in a directory Polymux scans');
        if (demoSkills.some((item) => item.name === entry.name)) throw new Error(`A skill named ${entry.name} already exists`);
        demoSkills.push({name: entry.name, description: entry.description, source: 'polymux', filePath: `~/.polymux/skills/${entry.name}/SKILL.md`, disableModelInvocation: false, allowedTools: [], permissions: [], enabled: true, editable: true, instructions: entry.description, updatedAt: '2026-08-16T09:00:00.000Z'});
        return demoSkills;
      },
      setEnabled: async (name, enabled) => {
        const item = demoSkills.find((candidate) => candidate.name === name);
        if (item) item.enabled = enabled;
        return demoSkills;
      },
      saveCustom: async (request) => {
        const index = demoSkills.findIndex((candidate) => candidate.name === (request.originalName ?? request.name));
        const next: SkillDto = {name: request.name, description: request.description, instructions: request.instructions, source: 'polymux', filePath: `~/.polymux/skills/${request.name}/SKILL.md`, disableModelInvocation: false, allowedTools: [], permissions: [], enabled: index >= 0 ? demoSkills[index]!.enabled : true, editable: true};
        if (index >= 0) demoSkills.splice(index, 1, next); else demoSkills.push(next);
        return demoSkills;
      },
      removeCustom: async (name) => {
        const index = demoSkills.findIndex((candidate) => candidate.name === name && candidate.editable);
        if (index < 0) throw new Error(`Skill is not removable: ${name}`);
        demoSkills.splice(index, 1);
        return demoSkills;
      },
      upload: async () => demoSkills,
    },
    models: {
      list: async () => demoModels,
      select: async (provider, id) => {
        const selected = demoModels.find((model) => model.provider === provider && model.id === id);
        if (!selected) throw new Error(`Unknown model: ${provider}/${id}`);
        const providerState = demoProviders.find((item) => item.id === provider);
        if (providerState && !providerWithKeys(providerState).configured)
          throw new Error(`${providerState.name} is not configured. Add its API key in Settings → Provider, or choose a configured model.`);
        demoModels = demoModels.map((model) => ({...model, selected: model === selected}));
        return demoModels.find((model) => model.selected)!;
      },
      // The browser demo has no main process to reach models.dev through, so
      // it exercises the "catalogue knows nothing" path the real app falls
      // back to when offline.
      metadata: async () => demoModelMetadata,
      roles: async () => demoRoles(),
      assignRole: async (role, provider, id, reasoning) => {
        const model = demoModels.find((item) => item.provider === provider && item.id === id);
        if (!model) throw new Error(`Unknown model: ${provider}/${id}`);
        const providerState = demoProviders.find((item) => item.id === provider);
        if (providerState && !providerWithKeys(providerState).configured)
          throw new Error(`${providerState.name} is not configured. Add its API key in Settings → Provider, or choose a configured model.`);
        if (role === 'main') {
          demoModels = demoModels.map((item) => ({...item, selected: item === model}));
          if (reasoning) demoGeneral = {...demoGeneral, reasoningLevel: reasoning};
          return demoRoles();
        }
        demoRoleOverrides = {...demoRoleOverrides, [role]: {provider, id, reasoning}};
        if (role === 'speech') demoGeneral = {...demoGeneral, speechModeEnabled: true};
        return demoRoles();
      },
      clearRole: async (role) => {
        if (role === 'main') throw new Error('The main model cannot be cleared');
        const {[role]: _removed, ...rest} = demoRoleOverrides;
        demoRoleOverrides = rest;
        if (role === 'speech') demoGeneral = {...demoGeneral, speechModeEnabled: false};
        return demoRoles();
      },
    },
    // The demo runs in a plain browser tab with no main process, so the
    // embedded browser is unavailable and BrowserView falls back to its
    // iframe rendering.
    browser: {
      embedded: false,
      open: async () => ({url: '', title: ''}),
      navigate: async () => {},
      history: async () => {},
      reload: async () => {},
      setBounds: async () => {},
      setVisible: async () => {},
      close: async () => {},
      openExternal: async (url) => { window.open(url, '_blank'); },
      // A browser tab has no host to ask which application owns a link, so the
      // menu falls back to naming one generically.
      defaultApp: async () => null,
      openPath: async () => {},
      find: async () => {},
      stopFind: async () => {},
      print: async () => {},
      preview: async () => null,
      screenshot: async () => null,
      // In a plain browser tab the page's own CSP is what it is, so a link's
      // icon falls back to the globe rather than being fetched for it.
      favicon: async () => null,
      downloads: async () => [...demoBrowserDownloads],
      openDownload: async () => {},
      openDownloadsFolder: async () => {},
      // Outside Electron there is no session, jar or keychain, so the demo
      // keeps its own state and mutates it — enough to exercise every state
      // the Browser tab renders.
      pauseDownload: async (id) => demoBrowserDownloadState(id, 'paused'),
      resumeDownload: async (id) => demoBrowserDownloadState(id, 'progressing'),
      cancelDownload: async (id) => demoBrowserDownloadState(id, 'cancelled'),
      removeDownload: async (id) => {
        const at = demoBrowserDownloads.findIndex((entry) => entry.id === id);
        if (at >= 0) demoBrowserDownloads.splice(at, 1);
        return [...demoBrowserDownloads];
      },
      clearDownloads: async () => {
        demoBrowserDownloads.length = 0;
        return [];
      },
      settings: async () => ({...demoBrowserSettings}),
      updateSettings: async (patch) => {
        if (patch.askWhereToSave !== undefined) demoBrowserSettings.askWhereToSave = patch.askWhereToSave;
        if (patch.autofillEnabled !== undefined) demoBrowserSettings.autofillEnabled = patch.autofillEnabled;
        // null asks the main process for a picker, which the demo stands in for.
        if (patch.downloadDirectory === null) demoBrowserSettings.downloadDirectory = '/demo/Documents/Polymux';
        else if (patch.downloadDirectory !== undefined) demoBrowserSettings.downloadDirectory = patch.downloadDirectory;
        return {...demoBrowserSettings};
      },
      permissions: async () => [...demoBrowserPermissions],
      setPermission: async (site, permission, decision) => {
        const existing = demoBrowserPermissions.find((row) => row.origin === site && row.permission === permission);
        if (existing) existing.decision = decision;
        else demoBrowserPermissions.push({origin: site, permission, decision, updatedAt: new Date().toISOString()});
        return [...demoBrowserPermissions];
      },
      clearPermissions: async (site) => {
        demoBrowserPermissions = site
          ? demoBrowserPermissions.filter((row) => row.origin !== site)
          : [];
        return [...demoBrowserPermissions];
      },
      respondToPermission: async () => {},
      respondToWebAuthn: async (id, credentialId) => {
        demoWebAuthnAnswer = {id, ...(credentialId ? {credentialId} : {})};
      },
      sites: async () => [...demoBrowserSites],
      clearSiteData: async (site) => {
        demoBrowserSites = demoBrowserSites.filter((row) => row.origin !== site);
        demoBrowserPermissions = demoBrowserPermissions.filter((row) => row.origin !== site);
        return [...demoBrowserSites];
      },
      clearBrowsingData: async (options) => {
        if (options.cookies) demoBrowserSites = [];
        if (options.downloads) demoBrowserDownloads.length = 0;
        if (options.permissions) demoBrowserPermissions = [];
        if (options.logins) {
          demoBrowserLogins.length = 0;
          demoBrowserPasswords.clear();
        }
      },
      logins: async () => [...demoBrowserLogins],
      saveLogin: async (site, username, password) => {
        const existing = demoBrowserLogins.find((row) => row.origin === site && row.username === username);
        const id = existing?.id ?? crypto.randomUUID();
        if (!existing)
          demoBrowserLogins.push({id, origin: site, username, source: 'manual', updatedAt: new Date().toISOString(), lastUsedAt: null});
        demoBrowserPasswords.set(id, password);
        return [...demoBrowserLogins];
      },
      revealLogin: async (id) => demoBrowserPasswords.get(id) ?? null,
      fillAutofill: async () => false,
      dismissAutofill: async () => {},
      deleteLogin: async (id) => {
        const at = demoBrowserLogins.findIndex((row) => row.id === id);
        if (at >= 0) demoBrowserLogins.splice(at, 1);
        demoBrowserPasswords.delete(id);
        return [...demoBrowserLogins];
      },
      browsingHistory: async (options) => {
        const query = options?.query?.trim().toLowerCase();
        const rows = query
          ? demoBrowserHistory.filter(
              (row) =>
                row.url.toLowerCase().includes(query) || row.title.toLowerCase().includes(query),
            )
          : [...demoBrowserHistory];
        return rows
          .sort((a, b) => b.visitedAt.localeCompare(a.visitedAt))
          .slice(0, options?.limit ?? 200);
      },
      suggestions: async (query) => {
        const text = query.trim();
        return text ? [text, `${text} login`, `${text} templates`, `${text} app`, `${text} ai`, `${text} download`, `${text} meaning`] : [];
      },
      forgetHistoryEntry: async (url) => {
        const at = demoBrowserHistory.findIndex((row) => row.url === url);
        if (at >= 0) demoBrowserHistory.splice(at, 1);
        return [...demoBrowserHistory];
      },
      clearHistory: async (options) => {
        const keep = options?.source === 'import'
          ? demoBrowserHistory.filter((row) => row.source !== 'import')
          : [];
        demoBrowserHistory.length = 0;
        demoBrowserHistory.push(...keep);
        return [...demoBrowserHistory];
      },
      importSources: async () => demoBrowserSources,
      importFrom: async (request) => ({
        cookiesImported: request.cookies ? 128 : 0,
        cookiesSkipped: request.cookies ? 4 : 0,
        passwordsImported: request.passwords ? 6 : 0,
        passwordsSkipped: 0,
        historyImported: request.history ? 2_140 : 0,
        historySkipped: request.history ? 12 : 0,
        problems: request.cookies ? ['the cookie has already expired (4 items)'] : [],
      }),
      importFile: async () => ({
        cookiesImported: 0,
        cookiesSkipped: 0,
        passwordsImported: 3,
        passwordsSkipped: 1,
        historyImported: 0,
        historySkipped: 0,
        problems: ['the login has no password'],
      }),
      subscribe: (listener) => {
        demoBrowserListeners.add(listener);
        return () => demoBrowserListeners.delete(listener);
      },
    },
    /**
     * The demo world outside Electron. One installed plugin and a small
     * catalogue rather than an empty list, so the tab can be seen — and
     * tested — with something in it.
     */
    plugins: {
      list: async () => demoPlugins,
      setEnabled: async (id, enabled) => {
        const item = demoPlugins.find((candidate) => candidate.id === id);
        if (item) item.enabled = enabled;
        return demoPlugins;
      },
      install: async (id) => {
        const entry = demoCatalog.find((candidate) => candidate.id === id);
        if (!entry) throw new Error(`Unknown plugin: ${id}`);
        entry.installed = true;
        demoPlugins.push({
          id: entry.id,
          name: entry.name,
          description: entry.description,
          version: entry.version,
          author: entry.author,
          marketplace: 'claude-code',
          marketplaceName: 'claude-code-plugins',
          directory: `~/.polymux/plugins/claude-code/${entry.name}`,
          enabled: true,
          contributions: {skills: [], mcpServers: [], views: [], commands: 1, agents: 0, hooks: 0},
          conflicts: [],
        });
        return demoPlugins;
      },
      remove: async (id) => {
        const index = demoPlugins.findIndex((candidate) => candidate.id === id);
        if (index >= 0) demoPlugins.splice(index, 1);
        const entry = demoCatalog.find((candidate) => candidate.id === id);
        if (entry) entry.installed = false;
        return demoPlugins;
      },
      marketplaces: async () => demoMarketplaces,
      addMarketplace: async (source) => {
        const repo = source.trim().split('/').pop() ?? source;
        if (!demoMarketplaces.some((entry) => entry.id === repo))
          demoMarketplaces.push({id: repo, name: repo, source: source.trim(), pluginCount: 0, builtin: false});
        return demoMarketplaces;
      },
      removeMarketplace: async (id) => {
        const index = demoMarketplaces.findIndex((entry) => entry.id === id && !entry.builtin);
        if (index >= 0) demoMarketplaces.splice(index, 1);
        return demoMarketplaces;
      },
      browse: async (query) => {
        const text = (query ?? '').trim().toLowerCase();
        return demoCatalog.filter((entry) =>
          !text || `${entry.name} ${entry.description}`.toLowerCase().includes(text));
      },
      views: async () => [],
      upload: async () => demoPlugins,
    },
    apps: {
      list: async () => structuredClone(demoWorkspaceApps),
      browse: async (query) => {
        const text = (query ?? '').trim().toLowerCase();
        return demoWorkspaceApps.apps
          .filter((app) => !text || `${app.name} ${app.description}`.toLowerCase().includes(text))
          .map((app): MarketplaceAppDto => ({
            id: app.id,
            name: app.name,
            description: app.description,
            author: 'Polymux',
            official: app.official,
            installed: true,
          }));
      },
      install: async (id) => {
        if (!demoWorkspaceApps.apps.some((app) => app.id === id)) throw new Error(`Unknown marketplace app: ${id}`);
        return structuredClone(demoWorkspaceApps);
      },
      setEnabled: async (id, enabled) => {
        const app = demoWorkspaceApps.apps.find((candidate) => candidate.id === id);
        if (!app) throw new Error(`Unknown app: ${id}`);
        app.enabled = enabled;
        if (!enabled) demoWorkspaceApps.pinnedIds = demoWorkspaceApps.pinnedIds.filter((candidate) => candidate !== id);
        return structuredClone(demoWorkspaceApps);
      },
      setPinned: async (ids) => {
        if (ids.length > 4) throw new Error('Only four apps can be pinned to New Tab');
        const available = new Set(demoWorkspaceApps.apps.filter((app) => app.enabled && app.pinnable).map((app) => app.id));
        if (new Set(ids).size !== ids.length || ids.some((id) => !available.has(id)))
          throw new Error('Only enabled workspace apps can be pinned');
        demoWorkspaceApps.pinnedIds = [...ids];
        return structuredClone(demoWorkspaceApps);
      },
      remove: async (id) => {
        const app = demoWorkspaceApps.apps.find((candidate) => candidate.id === id);
        if (!app) throw new Error(`Unknown app: ${id}`);
        if (app.official) throw new Error('Official apps cannot be uninstalled');
        demoWorkspaceApps.apps = demoWorkspaceApps.apps.filter((candidate) => candidate.id !== id);
        demoWorkspaceApps.pinnedIds = demoWorkspaceApps.pinnedIds.filter((candidate) => candidate !== id);
        return structuredClone(demoWorkspaceApps);
      },
    },
    providers: {
      list: async () => demoProviders.map((provider) => providerWithKeys(provider)),
      saveApiKey: async (provider, apiKey) => {
        const item = demoProviders.find((candidate) => candidate.id === provider);
        if (!item) throw new Error(`Unknown provider: ${provider}`);
        if (!apiKey.trim()) throw new Error('API key is required');
        const keys = demoKeys.get(provider) ?? [];
        keys.push({id: crypto.randomUUID(), label: `${apiKey.slice(0, 4)}••••${apiKey.slice(-4)}`, active: keys.length === 0, status: 'ready'});
        demoKeys.set(provider, keys);
        return providerWithKeys(item);
      },
      removeApiKey: async (provider, keyId) => {
        const item = demoProviders.find((candidate) => candidate.id === provider);
        if (!item) throw new Error(`Unknown provider: ${provider}`);
        const keys = (demoKeys.get(provider) ?? []).filter((key) => key.id !== keyId);
        if (keys.length && !keys.some((key) => key.active)) keys[0]!.active = true;
        demoKeys.set(provider, keys);
        return providerWithKeys(item);
      },
      connectOAuth: async (provider) => {
        const item = demoProviders.find((candidate) => candidate.id === provider);
        if (!item) throw new Error(`Unknown provider: ${provider}`);
        item.configured = true;
        item.storedCredential = true;
        item.source = 'OAuth';
        return providerWithKeys(item);
      },
      cancelOAuth: async () => {},
      disconnectOAuth: async (provider) => {
        const item = demoProviders.find((candidate) => candidate.id === provider);
        if (!item) throw new Error(`Unknown provider: ${provider}`);
        item.configured = false;
        item.storedCredential = false;
        item.source = null;
        return providerWithKeys(item);
      },
      subscribeOAuth: () => () => {},
      createCustom: async (request) => {
        const id = request.name.trim().toLocaleLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || crypto.randomUUID();
        if (demoProviders.some((provider) => provider.id === id)) throw new Error(`Provider already exists: ${request.name}`);
        const provider: ProviderDto = {
          id, name: request.name.trim(), apiKeyLabel: `${request.name.trim()} API key`, supportsOAuth: false,
          logoDataUrl: request.logoDataUrl, baseUrl: request.baseUrl,
          storedCredential: Boolean(request.apiKey), configured: true,
          source: request.apiKey ? '1 saved API key' : 'Custom endpoint', modelCount: request.models.length, custom: true, apiKeys: [],
        };
        demoProviders.push(provider);
        demoModels = [...demoModels, ...request.models.map((model) => ({
          provider: id, id: model.id, name: model.name?.trim() || model.id,
          contextWindow: 0, maxOutputTokens: 0, reasoning: false, input: ['text' as const],
          cost: {input: null, output: null, cacheRead: null, cacheWrite: null}, selected: false, custom: true,
        }))];
        if (request.apiKey) demoKeys.set(id, [{id: crypto.randomUUID(), label: `${request.apiKey.slice(0, 4)}••••${request.apiKey.slice(-4)}`, active: true, status: 'ready'}]);
        return providerWithKeys(provider);
      },
      updateCustom: async (request) => {
        const provider = demoProviders.find((candidate) => candidate.id === request.id && candidate.custom);
        if (!provider) throw new Error(`Unknown custom provider: ${request.id}`);
        provider.name = request.name.trim();
        provider.baseUrl = request.baseUrl;
        provider.logoDataUrl = request.logoDataUrl;
        provider.modelCount = request.models.length;
        const selectedId = demoModels.find((model) => model.provider === request.id && model.selected)?.id;
        demoModels = [
          ...demoModels.filter((model) => model.provider !== request.id),
          ...request.models.map((model, index) => ({
            provider: request.id, id: model.id, name: model.name?.trim() || model.id,
            contextWindow: 0, maxOutputTokens: 0, reasoning: false, input: ['text' as const],
            cost: {input: null, output: null, cacheRead: null, cacheWrite: null},
            selected: selectedId ? model.id === selectedId : index === 0, custom: true,
          })),
        ];
        return providerWithKeys(provider);
      },
      discoverModels: async () => [
        {id: 'llama3.1:8b'},
        {id: 'qwen2.5-coder:14b'},
      ],
      setupLocalRuntime: async (request) => {
        const provider = demoProviders.find((candidate) => candidate.id === request.id);
        if (!provider) throw new Error(`Unknown local runtime: ${request.id}`);
        const detected = [{id: 'llama3.1:8b'}, {id: 'qwen2.5-coder:14b'}];
        provider.baseUrl = request.baseUrl ?? provider.baseUrl;
        provider.localRuntime = false;
        provider.configured = true;
        provider.source = 'Local endpoint';
        provider.modelCount = detected.length;
        demoModels = [
          ...demoModels.filter((model) => model.provider !== provider.id),
          ...detected.map((model, index) => ({
            provider: provider.id, id: model.id, name: model.id,
            contextWindow: 0, maxOutputTokens: 0, reasoning: false, input: ['text' as const],
            cost: {input: null, output: null, cacheRead: null, cacheWrite: null},
            selected: index === 0, custom: true,
          })),
        ];
        return providerWithKeys(provider);
      },
    },
  };

  function providerWithKeys(provider: ProviderDto): ProviderDto {
    const apiKeys = demoKeys.get(provider.id) ?? [];
    // A local runtime that has not been connected yet is offered, not configured.
    const custom = provider.custom && !provider.localRuntime;
    return {...provider, apiKeys: structuredClone(apiKeys), storedCredential: apiKeys.length > 0, configured: custom || apiKeys.length > 0, source: apiKeys.length ? `${apiKeys.length} saved API ${apiKeys.length === 1 ? 'key' : 'keys'}` : custom ? 'Custom endpoint' : null};
  }

  function startDemoRun(request: StartRunRequest): {runId: string} {
    const runId = crypto.randomUUID();
    const timestamp = Date.now();
    const items = messages.get(request.conversationId) ?? [];
    if (request.rewind && request.messageId) {
      for (const [activeRunId, chatId] of [...runConversations]) {
        if (chatId === request.conversationId) finishDemoRun(activeRunId, 'run.cancelled');
      }
      const index = items.findIndex((item) => item.id === request.messageId);
      if (index >= 0) {
        const current = items[index]!;
        const attachments = (request.attachments ?? []).map((attachmentPath) => ({
          id: crypto.randomUUID(),
          messageId: current.id,
          name: attachmentPath.split(/[\\/]/).pop() ?? attachmentPath,
          path: attachmentPath,
          mimeType: null,
          size: null,
          sha256: null,
          createdAt: new Date(timestamp).toISOString(),
        }));
        items.splice(index, items.length - index, {
          ...current,
          content: request.text,
          attachments: [...current.attachments, ...attachments.filter((file) => !current.attachments.some((existing) => existing.path === file.path))],
        });
      }
    } else if (!request.reuseUserMessage) {
      items.push(message(request.messageId ?? crypto.randomUUID(), request.conversationId, 'user', request.text, timestamp, null, {asGoal: request.asGoal ?? false}));
    }
    runConversations.set(runId, request.conversationId);
    messages.set(request.conversationId, items);
    if (request.asGoal) goals.set(request.conversationId, goal(request.conversationId, request.text));
    emit(runId, request.conversationId, 'run.started', {});
    emit(runId, request.conversationId, 'run.state', {status: 'running'});
    emit(runId, request.conversationId, 'message.reasoning.delta', {delta: 'Thinking'});
    if (request.text === '__demo_agent_notice__') {
      emit(runId, request.conversationId, 'agent.notice', {
        severity: 'warning',
        message: 'Fast mode turned off: requires extra usage to be enabled for this account.',
      });
    }
    if (request.text === '__demo_external_connection_failure__') {
      timers.set(runId, setTimeout(() => {
        timers.delete(runId);
        runConversations.delete(runId);
        emit(runId, request.conversationId, 'agent.notice', {
          severity: 'error',
          message: 'External agent connection lost',
        });
        emit(runId, request.conversationId, 'run.failed', {
          result: {error: {message: 'External agent connection lost', reportedAsNotice: true}},
        });
        queueMicrotask(() => emit(runId, request.conversationId, 'run.settled', {}));
      }, 50));
      return {runId};
    }
    // Exercises the real failed-run UI without persisting a fake assistant
    // response; production Electron runs never use this browser demo adapter.
    if (request.text === '__demo_provider_failure__') {
      timers.set(runId, setTimeout(() => {
        timers.delete(runId);
        runConversations.delete(runId);
        emit(runId, request.conversationId, 'run.failed', {
          result: {error: {message: 'OpenCode Go is not configured. Add its API key in Settings → Provider, or choose a configured model.'}},
        });
        queueMicrotask(() => emit(runId, request.conversationId, 'run.settled', {}));
      }, 50));
      return {runId};
    }
    if (request.text === '__demo_auth_failure__') {
      timers.set(runId, setTimeout(() => {
        timers.delete(runId);
        runConversations.delete(runId);
        emit(runId, request.conversationId, 'run.failed', {
          result: {error: {message: '401: {"message":"Missing Authentication header","code":401}'}},
        });
        queueMicrotask(() => emit(runId, request.conversationId, 'run.settled', {}));
      }, 50));
      return {runId};
    }
    // `__demo_run_<ms>__` holds the run open for that long, which is how the
    // browser demo exercises anything that only exists mid-run (steering, the
    // queue behind a running agent).
    const held = /^__demo_run_(\d+)__$/.exec(request.text);
    const duration = held ? Number(held[1]) : 900;
    const isActivityDemo = request.text === '__demo_activity__';
    // A delegated task, its subagent's run, and the link between them, which is
    // what lets a task row open the run it started.
    if (request.text === '__demo_task__') {
      const childRunId = 'demo-subagent-run';
      const call = {
        id: 'demo-task-1',
        name: 'subagent',
        arguments: {description: 'Compare the two providers', prompt: 'Compare the two providers and report which is cheaper.'},
      };
      emit(runId, request.conversationId, 'tool.started', {toolCall: call});
      emit(runId, request.conversationId, 'tool.progress', {toolCallId: call.id, message: '', data: {childRunId}});
      // Dispatch returns at once — the delegated run is only just starting, and
      // the parent is free to keep working while it does.
      emit(runId, request.conversationId, 'tool.completed', {toolCall: call, result: {content: JSON.stringify({subagent: 'subagent_1', status: 'running'}), metadata: {subagent: 'subagent_1', status: 'running'}}});
      emit(childRunId, request.conversationId, 'run.started', {}, runId);
      emit(childRunId, request.conversationId, 'message.reasoning.delta', {delta: 'Weighing the two price sheets'}, runId);
      emit(childRunId, request.conversationId, 'tool.started', {toolCall: {id: 'demo-subagent-read', name: 'read', arguments: {path: '/pricing.md'}}}, runId);
      timers.set(runId, setTimeout(() => {
        const answer = 'The second provider is cheaper at this volume.';
        emit(childRunId, request.conversationId, 'tool.completed', {toolCall: {id: 'demo-subagent-read', name: 'read', arguments: {path: '/pricing.md'}}, result: {content: 'Read 40 lines.'}}, runId);
        emit(childRunId, request.conversationId, 'message.text.delta', {delta: answer}, runId);
        emit(childRunId, request.conversationId, 'run.completed', {result: {lastAgentMessage: answer}}, runId);
        const text = 'The subagent reports the second provider is cheaper.';
        emit(runId, request.conversationId, 'message.text.delta', {delta: text});
        items.push(message(crypto.randomUUID(), request.conversationId, 'assistant', [{type: 'text', text}], Date.now(), runId, {phase: 'final'}));
        finishDemoRun(runId, 'run.completed', {hadWorkActivity: true, lastAgentMessage: text, durationMs: 300});
      }, 300));
      return {runId};
    }
    if (isActivityDemo) {
      const args = {path: '/skills/window-control/SKILL.md'};
      const commentary = 'I’ll read the skill files first to see what applies here.';
      emit(runId, request.conversationId, 'message.completed', {message: {role: 'assistant', content: [{type: 'text', text: commentary}]}, phase: 'commentary'});
      emit(runId, request.conversationId, 'tool.started', {toolCall: {id: 'demo-skill-read-1', name: 'read', arguments: args}});
      emit(runId, request.conversationId, 'tool.started', {toolCall: {id: 'demo-skill-read-2', name: 'read', arguments: args}});
      emit(runId, request.conversationId, 'tool.started', {toolCall: {id: 'demo-skill-read-3', name: 'read', arguments: args}});
      emit(runId, request.conversationId, 'tool.progress', {toolCallId: 'demo-skill-read-3', message: 'Scanning the skill manifest'});
      emit(runId, request.conversationId, 'tool.progress', {toolCallId: 'demo-skill-read-3', message: 'Reading workflow steps'});
    }
    timers.set(runId, setTimeout(() => {
      const text = 'This is the assembled Polymux chat surface. Connect the send handler to your agent backend when it is ready.';
      if (isActivityDemo) {
        const args = {path: '/skills/window-control/SKILL.md'};
        emit(runId, request.conversationId, 'tool.completed', {toolCall: {id: 'demo-skill-read-3', name: 'read', arguments: args}, result: {content: 'Read the window-control workflow.'}});
        const browserArgs = {action: 'read', tabId: 'demo-browser-tab', url: 'https://polymux.com/docs'};
        emit(runId, request.conversationId, 'tool.started', {toolCall: {id: 'demo-browser-1', name: 'browser', arguments: browserArgs}});
        emit(runId, request.conversationId, 'tool.completed', {toolCall: {id: 'demo-browser-1', name: 'browser', arguments: browserArgs}, result: {content: JSON.stringify({ok: true, tabId: 'demo-browser-tab', pageUrl: browserArgs.url, pageTitle: 'Polymux Docs'})}});
        items.push(message(crypto.randomUUID(), request.conversationId, 'assistant', [], Date.now() - 2, runId, {phase: 'commentary'}));
        items.push(message(crypto.randomUUID(), request.conversationId, 'assistant', [], Date.now() - 1, runId, {phase: 'commentary'}));
      }
      emit(runId, request.conversationId, 'message.text.delta', {delta: text});
      emit(runId, request.conversationId, 'message.completed', {message: {role: 'assistant', content: [{type: 'text', text}]}, phase: 'final'});
      items.push(message(crypto.randomUUID(), request.conversationId, 'assistant', [{type: 'text', text}], Date.now(), runId, {phase: 'final'}));
      finishDemoRun(runId, 'run.completed', {hadWorkActivity: isActivityDemo, lastAgentMessage: text, durationMs: duration});
    }, duration));
    return {runId};
  }

  function finishDemoRun(runId: string, type: 'run.completed' | 'run.cancelled', result: Record<string, JsonValue> = {}): void {
    const conversationId = runConversations.get(runId);
    if (!conversationId) return;
    const timer = timers.get(runId);
    if (timer) clearTimeout(timer);
    timers.delete(runId);
    runConversations.delete(runId);
    emit(runId, conversationId, type, {result});
    queueMicrotask(() => emit(runId, conversationId, 'run.settled', {}));
  }

  let emitSequence = 0;
  function emit(
    runId: string,
    conversationId: string,
    type: string,
    body: Record<string, JsonValue>,
    parentRunId: string | null = null,
  ): void {
    // A real run's events carry increasing sequences, and the renderer keys
    // per-event UI (like tool sub-steps) off them, so the demo must too.
    const sequence = ++emitSequence;
    const event: RunEventDto = {runId, conversationId, parentRunId, sequence, timestamp: Date.now(), type, payload: {runId, conversationId, sequence, timestamp: Date.now(), type, ...body}};
    for (const listener of listeners) listener(event);
  }

  return api;
}

/** One row per state the schedule view draws differently. */
function demoSchedules(now: number, hour: number): ScheduleDto[] {
  return [
    {
      id: 'demo-brief',
      title: 'Morning brief',
      prompt: 'Summarise my inbox and calendar for the day.',
      frequency: {kind: 'weekly', days: [1, 2, 3, 4, 5], time: '08:00'},
      status: 'active',
      createdAt: now - 30 * 24 * hour,
      nextRunAt: now + 14 * hour,
      lastRunAt: now - 10 * hour,
      unread: true,
      history: [{
        id: 'demo-brief-run',
        startedAt: now - 10 * hour,
        finishedAt: now - 10 * hour + 42_000,
        outcome: 'succeeded',
        conversationId: 'welcome',
        summary: 'Read 24 new messages, flagged 3 needing a reply today, and listed the four meetings on the calendar with their prep notes.',
      }],
    },
    {
      id: 'demo-inbox',
      title: 'Triage inbox',
      prompt: 'Triage new mail and flag anything that needs a reply.',
      frequency: {kind: 'hourly', interval: 2},
      status: 'running',
      createdAt: now - 20 * 24 * hour,
      nextRunAt: now + 2 * hour,
      lastRunAt: now - hour,
      unread: false,
      history: [{id: 'demo-inbox-run', startedAt: now - 60_000, outcome: 'running'}],
    },
    {
      id: 'demo-report',
      title: 'Weekly spend report',
      prompt: 'Total this week’s spend and compare it to last week.',
      frequency: {kind: 'weekly', days: [5], time: '17:00'},
      status: 'paused',
      createdAt: now - 60 * 24 * hour,
      lastRunAt: now - 96 * hour,
      unread: false,
      history: [],
    },
    {
      id: 'demo-backup',
      title: 'Archive finished work',
      prompt: 'Move finished documents into the archive folder.',
      frequency: {kind: 'weekly', days: [0], time: '02:00'},
      status: 'failed',
      createdAt: now - 90 * 24 * hour,
      nextRunAt: now + 70 * hour,
      lastRunAt: now - 98 * hour,
      unread: true,
      history: [{
        id: 'demo-backup-run',
        startedAt: now - 98 * hour,
        finishedAt: now - 98 * hour + 9_000,
        outcome: 'failed',
        error: 'The archive folder is not connected in Drive.',
      }],
    },
    {
      id: 'demo-invite',
      title: 'Send the launch invite',
      prompt: 'Email the launch invitation to the press list.',
      frequency: {kind: 'once', at: now - 26 * hour},
      status: 'done',
      createdAt: now - 5 * 24 * hour,
      lastRunAt: now - 26 * hour,
      unread: false,
      history: [{
        id: 'demo-invite-run',
        startedAt: now - 26 * hour,
        finishedAt: now - 26 * hour + 18_000,
        outcome: 'succeeded',
        conversationId: 'research',
        summary: 'Sent the invitation to all 38 addresses on the press list and saved the delivery report to Drive.',
      }],
    },
  ];
}

function conversation(id: string, title: string, timestamp: number): ConversationDto {
  const date = new Date(timestamp).toISOString();
  return {id, title, createdAt: date, updatedAt: date, archivedAt: null};
}

function demoBot(
  id: string,
  conversationId: string,
  name: string,
  role: string,
  color: string,
  status: BotDto['status'],
  preview: string,
  timestamp: number,
  shape: BotDto['avatar']['shape'] = 'circle',
  skills?: string[],
  mcpServers?: string[],
  plugins?: string[],
): BotDto {
  return {
    id, conversationId, name, role,
    profileId: 'default', profileName: 'Default Profile',
    hostId: 'demo-host', hostName: 'This Mac', deviceType: 'laptop',
    avatar: {shape, color},
    laptopAccess: 'ask', status, preview,
    updatedAt: new Date(timestamp).toISOString(), unread: false,
    computer: {provider: 'remote', state: 'running', detail: null, persistent: true, network: 'none'},
    skills, mcpServers, plugins,
  };
}

function message(id: string, conversationId: string, role: MessageDto['role'], content: JsonValue, timestamp: number, runId: string | null = null, metadata: JsonValue = {}): MessageDto {
  return {id, conversationId, runId, role, content, createdAt: new Date(timestamp).toISOString(), sequence: 0, attachments: [], metadata};
}

function goal(conversationId: string, objective: string): GoalDto {
  const date = new Date().toISOString();
  return {id: crypto.randomUUID(), conversationId, objective, status: 'active', createdAt: date, updatedAt: date, completedAt: null};
}

/**
 * One provider of each shape, so the settings tab can be worked on in a browser
 * without credentials: connected, connectable, needing a form, and one this
 * build has no client id for.
 */
const demoDriveStatus: DriveStatusDto = {
  saveOrder: ['google-drive', 'dropbox', 'onedrive', 's3', 'local'],
  providers: [
    {id: 'local', name: 'Local', kind: 'local', state: 'connected', accounts: [{id: 'local', name: 'Polymux', email: null}], usage: {used: 412_000_000_000, total: 994_000_000_000, appUsed: 52_000_000_000}, root: '/demo/Polymux', error: null},
    // A share that is not mounted right now, which is the state worth seeing:
    // it stays listed so it can be reconnected or forgotten.
    {id: 'network', name: 'Network', kind: 'network', state: 'logged-out', accounts: [], usage: null, root: null, error: null},
    {id: 'google-drive', name: 'Google Drive', kind: 'oauth', state: 'connected', accounts: [{id: 'demo@example.com', name: 'Demo User', email: 'demo@example.com'}], usage: {used: 6_200_000_000, total: 15_000_000_000, appUsed: 1_200_000_000}, root: 'Polymux', error: null},
    {id: 'dropbox', name: 'Dropbox', kind: 'oauth', state: 'logged-out', accounts: [], usage: null, root: null, error: null},
    {id: 'onedrive', name: 'OneDrive', kind: 'oauth', state: 'unconfigured', accounts: [], usage: null, root: null, error: 'This build has no OneDrive client credentials.'},
    {id: 's3', name: 'S3 storage', kind: 's3', state: 'logged-out', accounts: [], usage: null, root: null, error: null},
  ],
  // Two local places plus one signed-in account, which is enough for the
  // switcher to show a provider icon next to an account name.
  sources: [
    // The virtual drive the manager now puts at the head of the list: every
    // connected place at once, which is what the workspace opens into.
    {id: 'all#all', provider: 'all', accountId: 'all', name: 'All storage', accountLabel: null, state: 'connected', usage: {used: 418_200_000_000, total: 1_009_000_000_000, appUsed: 53_200_000_000}, root: '', error: null},
    {id: 'local#outputs', provider: 'local', accountId: 'outputs', name: 'Local', accountLabel: null, state: 'connected', usage: {used: 412_000_000_000, total: 994_000_000_000, appUsed: 52_000_000_000}, root: '/demo/Documents/Polymux', error: null},
    {id: 'local#home', provider: 'local', accountId: 'home', name: 'Local', accountLabel: null, state: 'connected', usage: {used: 412_000_000_000, total: 994_000_000_000, appUsed: null}, root: '/demo/home', error: null},
    {id: 'google-drive#default', provider: 'google-drive', accountId: 'default', name: 'Google Drive', accountLabel: 'demo@example.com', state: 'connected', usage: {used: 6_200_000_000, total: 15_000_000_000, appUsed: 1_200_000_000}, root: 'Polymux', error: null},
  ],
};

function demoDriveConnect(provider: DriveProviderId, connected: boolean): DriveStatusDto {
  demoDriveStatus.providers = demoDriveStatus.providers.map((entry) =>
    entry.id === provider
      ? {
          ...entry,
          state: connected ? 'connected' : 'logged-out',
          accounts: connected ? [{id: 'demo@example.com', name: 'Demo User', email: 'demo@example.com'}] : [],
          usage: connected ? {used: 1_200_000_000, total: 10_000_000_000, appUsed: 18_000_000} : null,
        }
      : entry);
  return demoDriveStatus;
}

/**
 * The demo drive's contents, keyed `<provider>:<path>` and mutated in place.
 *
 * Stateful on purpose: a stub that always answered with the same two rows made
 * creating, uploading and deleting look like they did nothing, which is exactly
 * the failure the real drive must not have.
 */
const demoDriveFolders = new Map<string, DriveEntryDto[]>();
let demoDriveUploads = 0;

function demoDriveFolder(source: string, path: string): DriveEntryDto[] {
  const key = `${source}:${path}`;
  const known = demoDriveFolders.get(key);
  if (known) return known;
  const seeded = path ? [] : demoDriveSeed(source);
  demoDriveFolders.set(key, seeded);
  return seeded;
}

/** Finds an entry wherever it currently sits. */
function demoDriveFind(source: string, path: string): DriveEntryDto | undefined {
  for (const [key, entries] of demoDriveFolders) {
    if (!key.startsWith(`${source}:`)) continue;
    const found = entries.find((entry) => entry.path === path);
    if (found) return found;
  }
  return undefined;
}

function demoDriveSeed(source: string): DriveEntryDto[] {
  const now = new Date().toISOString();
  const {provider} = parseDriveSourceId(source);
  // The virtual drive is every place at once, so its root mixes providers —
  // which is the only way to see the badge doing its job: an unmarked file is
  // on this Mac, a marked one says where it actually lives.
  if (provider === 'all')
    return [
      {id: 'all#all/Reports', name: 'Reports', kind: 'folder', size: null, modifiedAt: now, provider: 'google-drive', path: 'all#all/Reports', mimeType: null},
      {id: 'all#all/Launch brief.docx', name: 'Launch brief.docx', kind: 'file', size: 48_310, modifiedAt: now, provider: 'google-drive', path: 'all#all/Launch brief.docx', mimeType: null},
      {id: 'all#all/Budget.xlsx', name: 'Budget.xlsx', kind: 'file', size: 21_004, modifiedAt: now, provider: 'dropbox', path: 'all#all/Budget.xlsx', mimeType: null},
      {id: 'all#all/Notes.md', name: 'Notes.md', kind: 'file', size: 1_820, modifiedAt: now, provider: 'local', path: 'all#all/Notes.md', mimeType: null},
    ];
  return [
    // Slash-separated so the parent of an entry can be read off its path, the
    // way every real provider's addressing allows.
    {id: '/Reports', name: 'Reports', kind: 'folder', size: null, modifiedAt: now, provider, path: '/Reports', mimeType: null},
    {id: '/Launch brief.docx', name: 'Launch brief.docx', kind: 'file', size: 48_310, modifiedAt: now, provider, path: '/Launch brief.docx', mimeType: null},
  ];
}

function demoArtifacts(conversationId: string): ArtifactDto[] {
  return [{id: 'launch-brief', conversationId, runId: null, kind: 'document', name: 'Launch brief.docx', path: '/tmp/Launch brief.docx', mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', size: null, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(), metadata: {}}];
}

function demoBrowserDownloadState(
  id: string,
  state: BrowserDownloadDto['state'],
): BrowserDownloadDto[] {
  const entry = demoBrowserDownloads.find((row) => row.id === id);
  if (entry) entry.state = state;
  return [...demoBrowserDownloads];
}

function demoReferences(conversationId: string): ReferenceDto[] {
  return [{id: 'polymux-site', conversationId, runId: null, kind: 'web', title: 'Polymux', uri: 'https://polymux.com', createdAt: new Date().toISOString(), metadata: {}}];
}

/**
 * The demo browser's own state, mutated in place.
 *
 * Stateful for the same reason the demo drive is: a stub that always answered
 * with the same rows made saving, revealing and clearing look like they did
 * nothing, which is exactly the failure the real tab must not have. The
 * passwords are obvious fakes — this file ships in the renderer bundle.
 */
const demoBrowserSettings: BrowserSettingsDto = {
  downloadDirectory: '/demo/Downloads',
  askWhereToSave: false,
  autofillEnabled: true,
};

const demoBrowserLogins: SavedLoginDto[] = [
  {id: 'login-github', origin: 'https://github.com', username: 'demo@example.com', source: 'manual', updatedAt: '2026-08-17T09:00:00.000Z', lastUsedAt: '2026-08-17T10:00:00.000Z'},
  {id: 'login-notion', origin: 'https://www.notion.so', username: 'demo@example.com', source: 'import', updatedAt: '2026-08-16T09:00:00.000Z', lastUsedAt: null},
];

const demoBrowserPasswords = new Map<string, string>([
  ['login-github', 'correct-horse-battery'],
  ['login-notion', 'demo-password-2'],
]);

const demoBrowserDownloads: BrowserDownloadDto[] = [
  {id: 'download-1', title: 'report (1).pdf', path: '/demo/Downloads/report (1).pdf', kind: 'pdf', completedAt: '14:20', url: 'https://files.example/report.pdf', state: 'completed', receivedBytes: 248_000, totalBytes: 248_000},
  {id: 'download-2', title: 'dataset.csv', path: '/demo/Downloads/dataset.csv', kind: 'spreadsheet', completedAt: '14:18', url: 'https://files.example/dataset.csv', state: 'progressing', receivedBytes: 4_200_000, totalBytes: 11_800_000},
];

let demoBrowserPermissions: SitePermissionDto[] = [
  {origin: 'https://maps.example.com', permission: 'geolocation', decision: 'allow', updatedAt: '2026-08-17T09:00:00.000Z'},
  {origin: 'https://news.example.com', permission: 'notifications', decision: 'deny', updatedAt: '2026-08-17T09:00:00.000Z'},
];

let demoBrowserSites: BrowserSiteDto[] = [
  {origin: 'https://github.com', cookies: 14, permissions: 0, logins: 1},
  {origin: 'https://maps.example.com', cookies: 3, permissions: 1, logins: 0},
];

const demoBrowserHistory: BrowserHistoryEntryDto[] = [
  {url: 'https://github.com/anthropics', title: 'Anthropic · GitHub', visitedAt: '2026-08-18T09:10:00.000Z', visitCount: 12, source: 'local'},
  {url: 'https://www.notion.so/Roadmap', title: 'Roadmap', visitedAt: '2026-08-18T08:40:00.000Z', visitCount: 3, source: 'local'},
  {url: 'https://news.ycombinator.com/', title: 'Hacker News', visitedAt: '2026-08-17T21:02:00.000Z', visitCount: 48, source: 'import'},
];

const demoBrowserSources: BrowserSourceDto[] = [
  {
    id: 'chromium:chrome',
    name: 'Google Chrome',
    family: 'chromium',
    profiles: [{id: 'Default', name: 'Person 1', path: '/demo/Chrome/Default', readable: true, reason: null}],
    fileImportOnly: false,
  },
  {
    id: 'safari',
    name: 'Safari',
    family: 'safari',
    // The real Safari source reports exactly this when the app has no Full
    // Disk Access, so the demo shows the state the user is most likely to hit.
    profiles: [{id: 'safari', name: 'Safari', path: '/demo/Safari', readable: false, reason: 'Polymux needs Full Disk Access to read Safari’s cookies.'}],
    fileImportOnly: true,
  },
];
