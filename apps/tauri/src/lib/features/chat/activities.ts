import type {AgentActivityItem, AgentActivityKind, AgentActivityStep} from './AgentActivity.svelte';
import {platformForChat} from '../../shared/state/chatPlatforms';
import {plural, translate, type MessageKey, type PluralKey} from '../../../i18n';

const INTERNAL_COMMENTARY_HEADING = /^(?:#{1,6}\s+|\*\*)?(?:searching|identifying|considering|planning|curating|reviewing|checking|thinking|clarifying|analysing|analyzing|filtering|listing|selecting|verifying|refining|summarising|summarizing|assessing|confirming|compiling|highlighting)\b[^\n]*(?:\*\*)?\s*$/i;

/** Provider scratch headings are useful neither as prose nor as activity rows;
 * the concrete tool lifecycle immediately below them already tells the user
 * what Polymux actually did. Keep genuine status narration unchanged. */
export function visibleCommentaryLabel(text: string): string | null {
  const trimmed = text.trim();
  return trimmed && !INTERNAL_COMMENTARY_HEADING.test(trimmed) ? trimmed : null;
}

export function upsertActivity(activities: AgentActivityItem[] = [], activity: AgentActivityItem): AgentActivityItem[] {
  const index = activities.findIndex((item) => item.id === activity.id);
  if (index < 0) return [...activities, activity];
  return activities.map((item, itemIndex) => itemIndex === index ? activity : item);
}

export function collapseActivities(activities: AgentActivityItem[] = []): AgentActivityItem[] {
  const collapsed: AgentActivityItem[] = [];
  for (const activity of activities) {
    const last = collapsed.at(-1);
    if (last && isBrowserActivity(last) && isBrowserActivity(activity)) {
      // A browse is one piece of work, however many snapshots, reads and
      // navigations it needed. Keep those operations as disclosure steps so
      // the default trail stays readable, while a failed detour remains red
      // and inspectable instead of being hidden by the group.
      const steps = [...browserSteps(last), ...browserSteps(activity)];
      collapsed[collapsed.length - 1] = {
        ...last,
        id: last.id,
        target: undefined,
        result: activity.result ?? last.result,
        status: aggregateStatus(last.status, activity.status),
        steps,
        preview: latestPreview(last, activity),
      };
      continue;
    }
    if (
      last
      && last.kind === activity.kind
      && last.label === activity.label
      && last.target === activity.target
      && last.status === activity.status
    ) {
      // Merged rows keep every reported step, so repeated calls to the same
      // tool read as one group with its combined sub-step trail. The calls
      // stay counted even though they share one row, so the settled trail can
      // still say "Ran 2 commands" for two of them.
      const steps = [...(last.steps ?? []), ...(activity.steps ?? [])];
      collapsed[collapsed.length - 1] = {
        ...last,
        ...activity,
        id: last.id,
        count: (last.count ?? 1) + (activity.count ?? 1),
        steps: steps.length ? steps : undefined,
      };
    } else {
      collapsed.push(activity);
    }
  }
  return collapsed;
}

function latestPreview(
  previous: AgentActivityItem,
  next: AgentActivityItem,
): AgentActivityItem['preview'] {
  if (!next.preview) return undefined;
  if (next.preview?.kind === 'computer') return next.preview;
  if (next.preview?.kind === 'browser' && next.preview.tabId) return next.preview;
  return previous.preview ?? next.preview;
}

function isBrowserActivity(activity: AgentActivityItem): boolean {
  return activity.kind === 'searching' && activity.icon === 'globe';
}

function browserSteps(activity: AgentActivityItem): NonNullable<AgentActivityItem['steps']> {
  if (activity.steps?.length) return activity.steps;
  return [{
    id: `${activity.id}:operation`,
    label: activity.target ?? activity.label,
    status: activity.status,
  }];
}

function aggregateStatus(
  left: AgentActivityItem['status'],
  right: AgentActivityItem['status'],
): AgentActivityItem['status'] {
  if (left === 'failed' || right === 'failed') return 'failed';
  if (left === 'active' || right === 'active') return 'active';
  if (left === 'pending' || right === 'pending') return 'pending';
  return 'completed';
}

/** The kinds whose rows condense codex-style into one counted row in the
 * settled trail: "Ran 2 commands" where two commands ran, "Read 3 files"
 * where three reads happened. */
const COUNT_SUMMARIES: Partial<Record<AgentActivityKind, PluralKey>> = {
  running: 'activity.ranCommands',
  reading: 'activity.readFiles',
  editing: 'activity.editedFiles',
};

/** A failed command did not "run" successfully. Give failed stretches their
 * own wording and keep them apart from successful command stretches so neither
 * the label nor its count misstates what happened. */
function countSummaryFamily(activity: AgentActivityItem): PluralKey | undefined {
  if (activity.kind === 'running' && activity.status === 'failed') return 'activity.failedCommands';
  return COUNT_SUMMARIES[activity.kind];
}

/**
 * What the settled trail shows: one row per stretch of the same counted work,
 * each call behind it kept as a step with whatever it came back with. That is
 * the codex handoff — the live line disappears and "Ran 2 commands" takes its
 * place. Narration (thinking, commentary, a successful detour) stays behind
 * the heading; finished compaction keeps its one-line token receipt, and a
 * failure is never dropped, so a failed row that is not one of the counted
 * kinds still shows, in red.
 */
const SETTLED_VISIBLE_KINDS = new Set<AgentActivityKind>(['compacting']);

export function settledActivities(activities: AgentActivityItem[] = []): AgentActivityItem[] {
  const summary: AgentActivityItem[] = [];
  for (const activity of activities) {
    const last = summary.at(-1);
    const family = countSummaryFamily(activity);
    const lastFamily = last ? countSummaryFamily(last) : undefined;
    if (family && last && last.kind === activity.kind && family === lastFamily) {
      const count = (last.count ?? 1) + (activity.count ?? 1);
      summary[summary.length - 1] = {
        ...last,
        label: plural(family, count),
        count,
        status: aggregateStatus(last.status, activity.status),
        steps: [...(last.steps ?? []), ...memberSteps(activity)],
      };
      continue;
    }
    summary.push(family ? countedRow(activity, family) : activity);
  }
  return summary.filter((row) =>
    COUNT_SUMMARIES[row.kind] !== undefined
    || SETTLED_VISIBLE_KINDS.has(row.kind)
    || row.status === 'failed',
  );
}

function countedRow(activity: AgentActivityItem, family: PluralKey): AgentActivityItem {
  const count = activity.count ?? 1;
  return {
    ...activity,
    label: plural(family, count),
    count,
    target: undefined,
    result: undefined,
    steps: memberSteps(activity),
  };
}

/** Each call a row stands for becomes one step: the thing it did (its target,
 * or its label when it has none) with whatever it came back with, and its own
 * sub-steps trailing it so nothing reported is lost in the fold. */
function memberSteps(member: AgentActivityItem): AgentActivityStep[] {
  const step: AgentActivityStep = {
    id: member.id,
    label: member.target ?? member.label,
    status: member.status,
  };
  if (member.result) step.result = member.result;
  return [step, ...(member.steps ?? [])];
}

/** One thinking row belongs to the whole run. The optimistic row exists before
 * the backend has assigned a run id, so later reasoning must reuse it by kind
 * rather than append a second run-addressed row. */
export function runThinkingActivity(
  activities: AgentActivityItem[],
  runId: string,
): AgentActivityItem | undefined {
  return activities.find((item) => item.id === `${runId}:thinking`)
    ?? activities.find((item) => item.kind === 'thinking');
}

/**
 * What a tool call is called in the activity trail, in the language the app is
 * in when the call happens. Labels are written into the message's stored
 * metadata as they are produced, so an old conversation keeps the wording it
 * was narrated with — a record of the run rather than a caption re-rendered
 * against whatever language is current.
 */
export function activityPresentation(
  name: string,
  input: Record<string, unknown> = {},
  runId = '',
): Pick<AgentActivityItem, 'kind' | 'label' | 'icon' | 'logo' | 'target' | 'preview'> {
  const normalized = name.toLowerCase();
  const path = typeof input.path === 'string' ? input.path : '';
  const uri = typeof input.uri === 'string' ? input.uri : '';

  // The hub's own tools are named before anything generic can claim them:
  // `message_search` is a search and `email_read` a read, but what the user
  // needs to see is which of their accounts the agent just reached into.
  const hub = hubActivity(normalized, input);
  if (hub) return hub;

  // Both browser surfaces used to render as a run of indistinguishable
  // "Browser" rows. Keep the familiar globe, but name the operation or host
  // beside it so the trail reveals detours such as an unnecessary external-tab
  // check instead of making every browse look like the same successful step.
  const inAppBrowser = normalized === 'browser' || normalized === 'browser_read' || normalized === 'browser_snapshot_many';
  if (inAppBrowser || normalized === 'browser_tabs' || normalized === 'browser_control') {
    const tabId = browserTabId(input);
    return {
      kind: 'searching',
      label: translate('activity.using', {name: 'Browser'}),
      icon: 'globe',
      target: browserTarget(input, normalized),
      ...(inAppBrowser && (tabId || runId) ? {preview: {kind: 'browser', tabId}} : {}),
    };
  }

  if (normalized.includes('read') && /(?:^|\/)skill\.md$/i.test(path)) {
    return skillActivity(path.split('/').at(-2) ?? '', runId);
  }
  if (normalized === 'skill' || normalized.startsWith('skill_') || normalized.startsWith('skill.')) {
    return skillActivity(typeof input.name === 'string' ? input.name : '', runId);
  }
  // Memory is checked before the generic search and tool rules: recall is a
  // search, but "Recalling memory" is what the user needs to see, and the row
  // is the only place the app tells them their memory was touched at all.
  if (normalized === 'remember' || normalized === 'save_memory') {
    return {kind: 'memory', label: translate('activity.remembering')};
  }
  if (normalized === 'recall' || normalized === 'list_memory' || normalized.includes('search_memory')) {
    return {kind: 'memory', label: translate('activity.recalling')};
  }
  if (normalized === 'forget' || normalized === 'delete_memory') {
    return {kind: 'memory', label: translate('activity.forgetting')};
  }
  if (normalized === 'search_history' || normalized === 'read_conversation') {
    return {kind: 'memory', label: translate('activity.searchingHistory')};
  }
  if (normalized.includes('compact') || normalized.includes('compress') || normalized.includes('optimis') || normalized.includes('optimiz')) {
    return {kind: 'compacting', label: translate('activity.compacting')};
  }
  if (normalized.includes('think') || normalized.includes('reason')) {
    return {kind: 'thinking', label: translate('activity.thinking')};
  }
  if (normalized.includes('resource') || uri.length > 0 || normalized === 'read_resource' || normalized === 'fetch_resource') {
    const named = uri ? uri.split('/').at(-1) : typeof input.name === 'string' ? input.name : '';
    const resource = named ? humanize(named) : translate('activity.resource');
    return {kind: 'resource', label: translate('activity.using', {name: resource})};
  }
  if (normalized === 'read' || normalized.includes('read_file')) {
    const path = typeof input.path === 'string' ? input.path : undefined;
    return {kind: 'reading', label: translate('activity.readingFiles'), ...(path ? {target: path} : {})};
  }
  if (normalized === 'edit' || normalized === 'write' || normalized.includes('apply_patch') || normalized.includes('edit_file') || normalized.includes('write_file')) {
    const path = typeof input.path === 'string' ? input.path : undefined;
    return {kind: 'editing', label: translate('activity.editingFiles'), ...(path ? {target: path} : {})};
  }
  if (normalized === 'bash' || normalized.includes('command') || normalized.includes('terminal') || normalized.includes('exec') || normalized === 'sh') {
    // The row is the command itself, codex-style: while it runs the live line
    // reads as the command, and a settled "Ran 2 commands" row expands to the
    // commands behind it.
    const command = typeof input.command === 'string' ? firstLine(input.command) : '';
    return {kind: 'running', label: command || translate('activity.runningCommand')};
  }
  if (normalized.includes('search') || normalized.includes('web') || normalized === 'glob' || normalized === 'grep') {
    return {kind: 'searching', label: translate('activity.searching')};
  }
  if (normalized === 'task' || normalized.includes('subagent') || normalized.includes('delegate')) {
    return {kind: 'task', label: translate('activity.delegatingTask')};
  }
  if (name.includes('.')) {
    return {kind: 'tool', label: translate('activity.using', {name: humanize(name.split('.')[0]!)})};
  }
  return {kind: 'tool', label: humanize(name)};
}

function browserTarget(input: Record<string, unknown>, name: string): string | undefined {
  const url = typeof input.url === 'string' ? input.url.trim()
    : typeof input.target === 'string' ? input.target.trim()
    : '';
  if (url) {
    try {
      return new URL(url).hostname.replace(/^www\./, '');
    } catch {
      return url.length > 72 ? `${url.slice(0, 71)}…` : url;
    }
  }
  if (name === 'browser_tabs') return 'Tabs';
  const action = typeof input.action === 'string' ? input.action.trim() : '';
  return action ? humanize(action) : undefined;
}

function browserTabId(input: Record<string, unknown>): string {
  if (typeof input.tabId === 'string') return input.tabId.trim();
  if (Array.isArray(input.tabIds))
    return input.tabIds.find((value): value is string => typeof value === 'string' && value.trim().length > 0)?.trim() ?? '';
  return '';
}

/** Pull a browser tab identity from either tool progress data or a result. */
export function activityPreviewTabId(value: unknown): string | null {
  const direct = previewTabIdFromRecord(record(value));
  if (direct) return direct;
  const content = record(value).content;
  const text = typeof content === 'string'
    ? content
    : Array.isArray(content)
      ? content.map((block) => typeof record(block).text === 'string' ? String(record(block).text) : '').find(Boolean) ?? ''
      : '';
  if (!text || text.length > 1_000_000) return null;
  try {
    return previewTabIdFromRecord(record(JSON.parse(text)));
  } catch {
    return null;
  }
}

function previewTabIdFromRecord(value: Record<string, unknown>): string | null {
  const direct = typeof value.browserTabId === 'string' ? value.browserTabId.trim()
    : typeof value.tabId === 'string' ? value.tabId.trim()
    : '';
  if (direct) return direct;
  const pages = Array.isArray(value.pages) ? value.pages : [];
  for (const page of pages) {
    const tabId = typeof record(page).tabId === 'string' ? String(record(page).tabId).trim() : '';
    if (tabId) return tabId;
  }
  return null;
}

/** Some tools complete normally at the transport layer while returning a
 * domain-level error. Their activity row must still be red: this is exactly
 * how a missing browser extension reports failure. */
export function toolResultFailed(value: unknown): boolean {
  const result = record(value);
  const metadata = record(result.metadata);
  return result.isError === true || metadata.status === 'failed';
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

/**
 * A row for one of the hub's messaging or email tools: the platform's own logo
 * where the call names a conversation the hub knows, and wording that says what
 * was done to the account rather than what the tool was called.
 */
function hubActivity(
  normalized: string,
  input: Record<string, unknown>,
): {kind: AgentActivityKind; label: string; logo?: AgentActivityItem['logo']} | undefined {
  if (normalized.startsWith('message_')) {
    const chatId = typeof input.chat_id === 'string' ? input.chat_id : '';
    const named = typeof input.platform === 'string' ? input.platform : '';
    // A call that names one conversation borrows its platform; a call that
    // spans every linked account has none to borrow and keeps the generic
    // glyph rather than picking a logo that would misreport the reach.
    const logo = platformForChat(chatId) ?? (named ? platformForChat(`platform:${named}`) ?? knownPlatform(named) : undefined);
    const label = normalized === 'message_send' ? translate('activity.sendingMessage')
      : normalized === 'message_read' ? translate('activity.readingMessages')
      : normalized === 'message_search' ? translate('activity.searchingMessages')
      : translate('activity.checkingMessages');
    return {kind: 'messaging', label, ...(logo ? {logo} : {})};
  }
  if (normalized.startsWith('email_')) {
    const label = normalized === 'email_send' ? translate('activity.sendingEmail')
      : normalized === 'email_attachments' ? translate('activity.readingAttachments')
      : normalized === 'email_accounts' || normalized === 'email_folders' ? translate('activity.checkingMailboxes')
      : translate('activity.readingEmails');
    return {kind: 'mail', label, logo: 'mail'};
  }
  return undefined;
}

/** A platform named by the call itself, kept only when it is one the app can
 * draw a mark for. */
function knownPlatform(value: string): AgentActivityItem['logo'] | undefined {
  return platformForChat(value) ?? PLATFORM_LOGOS.has(value) ? value as AgentActivityItem['logo'] : undefined;
}

const PLATFORM_LOGOS = new Set([
  'whatsapp', 'telegram', 'signal', 'slack', 'messenger', 'instagram',
  'linkedin', 'googlechat', 'gmessages', 'twitter', 'bluesky', 'gvoice',
  'zulip', 'imessage', 'wechat', 'matrix', 'mail',
]);

/**
 * A skill's row. Some folder names read awkwardly in the trail, so those carry
 * a display name of their own — and, where the skill has a glyph that says more
 * than its kind's generic one, the glyph travels with the row rather than being
 * looked up from the finished label, which is translated and so no longer a
 * stable key.
 */
const skillDisplay: Record<string, {name: string; icon: AgentActivityItem['icon']}> = {
  'window-control': {name: 'Window Control', icon: 'computer'},
};

function skillActivity(value: string, runId = ''): {kind: AgentActivityKind; label: string; icon?: AgentActivityItem['icon']; preview?: AgentActivityItem['preview']} {
  const known = skillDisplay[value.toLowerCase()];
  const name = known ? known.name : value ? humanize(value) : translate('activity.skill');
  return {
    kind: 'skill',
    label: translate('activity.using', {name}),
    ...(known?.icon ? {icon: known.icon} : {}),
    ...(known?.icon === 'computer' && runId ? {preview: {kind: 'computer', runId} as const} : {}),
  };
}

function humanize(value: string): string {
  return value
    .replaceAll('_', ' ')
    .replaceAll('-', ' ')
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

/** The command's first line, trimmed and capped — it is the row's whole
 * label, so a multi-line script still reads as one line. */
function firstLine(value: string): string {
  const line = value.split('\n').map((part) => part.trim()).find(Boolean) ?? '';
  return line.length > 80 ? `${line.slice(0, 79)}…` : line;
}

const condensedLabels: Record<AgentActivityKind, MessageKey> = {
  thinking: 'activity.summary.thinking',
  compacting: 'activity.summary.compacting',
  reading: 'activity.summary.reading',
  searching: 'activity.summary.searching',
  running: 'activity.summary.running',
  task: 'activity.summary.task',
  memory: 'activity.summary.memory',
  skill: 'activity.summary.skill',
  tool: 'activity.summary.tool',
  resource: 'activity.summary.resource',
  editing: 'activity.summary.editing',
  messaging: 'activity.summary.messaging',
  mail: 'activity.summary.mail',
  plugin: 'activity.summary.tool',
  commentary: 'activity.summary.commentary',
};

export function shouldShowAgentActivity(activities: AgentActivityItem[] = []): boolean {
  return activities.length > 0;
}

export function activitySummary(activities: AgentActivityItem[]): string {
  const kinds = [...new Set(activities.map((activity) => activity.kind))];
  return kinds.map((kind) => translate(condensedLabels[kind])).join(', ');
}

/** ChatGPT-style elapsed label: "49s" under a minute, then "6m 58s". The unit
 * letters are part of the catalog — they are abbreviations of words, and not
 * every language abbreviates them to `s` and `m`. */
export function formatElapsedSeconds(seconds: number): string {
  if (seconds < 60) return translate('time.seconds', {seconds});
  return translate('time.minutesSeconds', {minutes: Math.floor(seconds / 60), seconds: seconds % 60});
}

export function activityDuration(startedAt?: string, completedAt?: string, now = Date.now()): number {
  if (!startedAt) return 0;
  const start = Date.parse(startedAt);
  const end = completedAt ? Date.parse(completedAt) : now;
  if (!Number.isFinite(start) || !Number.isFinite(end)) return 0;
  return Math.max(0, Math.round((end - start) / 1000));
}

/**
 * Milliseconds until the displayed duration next changes. A plain one-second
 * interval drifts and gets coalesced when the main thread is busy, which makes
 * the counter stall and then jump; scheduling each tick to the moment the value
 * actually changes keeps it advancing once per second. `activityDuration`
 * rounds, so that moment is each half-second past the start.
 */
export function nextDurationTickDelay(startedAt?: string, now = Date.now()): number {
  const start = Date.parse(startedAt ?? '');
  if (!Number.isFinite(start)) return 1000;
  const sinceLastChange = (((now - start - 500) % 1000) + 1000) % 1000;
  return 1000 - sinceLastChange;
}
