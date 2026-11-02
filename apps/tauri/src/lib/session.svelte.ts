import type {
  Agent,
  Attachment,
  ClientRequest,
  Event,
  Message,
  ReplyContext,
  Reaction,
  ReactionRequest,
  Run,
  Section,
  SidebarSnapshot,
  Thread,
} from './api/contract';
import { APIClient } from './api/client';
import type { ChatDraftMention, DraftState } from './api/models';
import { applyAppearance } from './appearance';
import { Draft } from './draft';
import { Cache, type ServerIdentity } from './cache';
import { Outbox, type Outgoing, type StoredFile } from './outbox';
import { ResponseError, isTransient, reason } from './api/errors';
import { overlayGroups, restoreGroups, stopGroups } from './shell/groupEdit';
import { replyIssue } from './replyIssue';
import { NotificationStore } from './notifications.svelte';
import { currentLocation } from './shell/location';
import { overlayAvatars, restoreAvatars, stopAvatars } from './avatar/avatarEdit';

export interface ActivityView {
  typing: boolean;
  working: boolean;
  unread: boolean;
}

export interface DraftFile {
  id: string;
  file: File;
  url: string;
}

/** The server's "Unassigned" list, which always exists. */
export const UNASSIGNED = '__unassigned__';
type ChatDestination = { pinned: boolean; before?: string } | { sectionID?: string };
type Placement = Pick<Thread, 'pinned' | 'pinOrder' | 'sectionID' | 'sectionOrder'>;
type ReplayAck = { serial: number; messageID: string } &
  ({ kind: 'choice'; answerID: string; answerText?: string } | { kind: 'reaction'; request: ReactionRequest });

/** The connected server and everything the shell shows, mirroring PolymuxKit's AppSession. */
export class Session {
  readonly client: APIClient;
  readonly address: string;
  readonly notifications: NotificationStore;
  agents: Agent[] = $state([]);
  threads: Thread[] = $state([]);
  sections: Section[] = $state([]);
  messages: Record<string, Message[]> = $state({});
  drafts: Record<string, DraftState> = $state({});
  draftIssues: Record<string, string> = $state({});
  outboxIssues: Record<string, string> = $state({});
  sending: Record<string, number> = $state({});
  files: Record<string, DraftFile[]> = $state({});
  activity: Record<string, ActivityView> = $state({});
  runs: Record<string, Run> = $state({});
  reactionPending: Record<string, ReactionRequest> = $state({});
  reactionIssues: Record<string, string> = $state({});
  online = $state(false);
  locationEnabled = $state(false);
  error = $state('');
  sidebarIssue = $state('');
  sidebarMoves: Record<string, boolean> = $state({});

  private streams = new Map<string, AbortController>();
  private connections?: AbortController;
  private clientSeen = new Set<string>();
  private streamReady = new Set<string>();
  private cursors = new Map<string, number>();
  private chatLoads = new Map<string, Promise<void>>();
  private replayBases = new Map<string, Message[]>();
  private replayAcks = new Map<string, Map<string, ReplayAck>>();
  private ackSerial = 0;
  private opening = new Map<string, Promise<void>>();
  private draftStores = new Map<string, Draft>();
  private mutes: Record<string, number> = {};
  private readonly cache: Cache;
  private identity?: ServerIdentity;
  private readonly outbox: Outbox;
  private channel?: BroadcastChannel;
  private restoredOutbox?: Promise<void>;
  private restoredFiles = new Map<string, Promise<void>>();
  private fileWrites = new Map<string, Promise<void>>();
  private flushing = new Map<string, Promise<void>>();
  private discarding = new Set<string>();
  private unpersisted = new Set<string>();
  private polling?: ReturnType<typeof setTimeout>;
  private syncing?: Promise<void>;
  private stopped = false;
  private visible?: () => void;
  private messageTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private reactionVersions = new Map<string, number>();
  private sidebarPending = 0;
  private sidebarRevision = 0;
  private dates = new Map<string, string>();
  private placementVersions = new Map<string, number>();
  private sidebarRetry?: { id: string; destination: ChatDestination };

  beginSidebarChange(): void {
    this.sidebarPending++;
    this.sidebarRevision++;
  }

  endSidebarChange(): void {
    this.sidebarPending--;
    this.sidebarRevision++;
  }

  async placeChat(id: string, destination: ChatDestination, retry = false): Promise<void> {
    const current = this.threads.find(thread => thread.id === id);
    if (!current || this.sidebarMoves[id]) return;
    if (!retry) this.sidebarRetry = undefined;
    const previous: Placement = {
      pinned: current.pinned, pinOrder: current.pinOrder,
      sectionID: current.sectionID, sectionOrder: current.sectionOrder,
    };
    const pin = 'pinned' in destination && destination.pinned;
    const before = 'pinned' in destination ? destination.before : undefined;
    const sectionID = 'sectionID' in destination ? destination.sectionID : undefined;
    const pins = this.pinned.filter(thread => thread.id !== id);
    const index = before ? pins.findIndex(thread => thread.id === before) : pins.length;
    if (pin && (before === id || index < 0)) return;
    const orderedPins = [...pins.map(thread => thread.id)];
    if (pin) orderedPins.splice(index, 0, id);
    const orders = new Map(orderedPins.map((threadID, order) => [threadID, order]));
    const previousOrders = new Map(pins.map(thread => [thread.id, {
      order: thread.pinOrder, version: this.placementVersions.get(thread.id) ?? 0,
    }]));
    const order = Math.max(-1, ...this.threads.filter(thread => thread.id !== id && !thread.pinned && (thread.sectionID || undefined) === sectionID)
      .map(thread => thread.sectionOrder ?? -1)) + 1;
    const next: Placement = pin
      ? { pinned: true, pinOrder: index, sectionID: undefined, sectionOrder: undefined }
      : { pinned: false, pinOrder: undefined, sectionID, sectionOrder: 'pinned' in destination ? undefined : order };
    const same = (thread: Thread, placement: Placement) =>
      !!thread.pinned === !!placement.pinned && thread.pinOrder === placement.pinOrder &&
      thread.sectionID === placement.sectionID && thread.sectionOrder === placement.sectionOrder;
    this.sidebarMoves[id] = true;
    this.placementVersions.set(id, (this.placementVersions.get(id) ?? 0) + 1);
    this.sidebarIssue = '';
    this.beginSidebarChange();
    this.threads = this.threads.map(thread => thread.id === id ? { ...thread, ...next }
      : pin && orders.has(thread.id) ? { ...thread, pinOrder: orders.get(thread.id) } : thread);
    let saved = false;
    try {
      const updated = await this.client.placeThread(id, sectionID, pin, before, pin && !before ? pins.at(-1)?.id : undefined);
      this.threads = this.threads.map(thread => thread.id === id && same(thread, next)
        ? { ...thread, pinned: updated.pinned, pinOrder: updated.pinOrder, sectionID: updated.sectionID, sectionOrder: updated.sectionOrder }
        : thread);
      saved = true;
      if (this.sidebarRetry?.id === id) this.sidebarRetry = undefined;
    } catch (error) {
      this.threads = this.threads.map(thread => {
        if (thread.id === id && same(thread, next)) return { ...thread, ...previous };
        const old = previousOrders.get(thread.id);
        if (pin && old && thread.pinned && thread.pinOrder === orders.get(thread.id) &&
            (this.placementVersions.get(thread.id) ?? 0) === old.version) return { ...thread, pinOrder: old.order };
        return thread;
      });
      if (isTransient(error)) {
        this.sidebarRetry = { id, destination };
        this.sidebarIssue = 'Couldn’t save that change yet. It will try again.';
      } else {
        this.sidebarIssue = reason(error, 'Couldn’t save that change.');
      }
    } finally {
      this.endSidebarChange();
      delete this.sidebarMoves[id];
      if (saved) void this.refresh().catch(() => {});
    }
  }

  constructor(address: string, client: APIClient) {
    this.address = address;
    this.client = client;
    this.notifications = new NotificationStore(address, client);
    this.cache = new Cache(address);
    this.outbox = new Outbox(address);
    try {
      this.channel = new BroadcastChannel(JSON.stringify(['polymux-outbox', address]));
      this.channel.onmessage = (event: MessageEvent<unknown>) => {
        const value = event.data as { threadID?: unknown; id?: unknown } | null;
        if (typeof value?.threadID === 'string' && typeof value.id === 'string' && value.id.startsWith('local-')) {
          void this.receiveDiscard(value.threadID, value.id);
        }
      };
    } catch { /* Other windows will refresh their queues on restart. */ }
    this.chosen = localStorage.getItem(`lastChat.${address}`) ?? undefined;
    this.locationEnabled = localStorage.getItem(`polymux.location.${address}`) === '1';
  }

  private chosen = $state<string | undefined>(undefined);

  get selection(): string | undefined {
    return this.chosen;
  }

  set selection(id: string | undefined) {
    this.chosen = id;
    if (id) localStorage.setItem(`lastChat.${this.address}`, id);
    else localStorage.removeItem(`lastChat.${this.address}`);
  }

  get ordered(): Thread[] {
    return [...this.threads].sort((left, right) => {
      const leftPin = left.pinned ? left.pinOrder ?? 0 : undefined;
      const rightPin = right.pinned ? right.pinOrder ?? 0 : undefined;
      if (leftPin !== undefined && rightPin !== undefined && leftPin !== rightPin) return leftPin - rightPin;
      if (leftPin !== undefined && rightPin === undefined) return -1;
      if (leftPin === undefined && rightPin !== undefined) return 1;
      const leftDate = this.dates.get(left.id) ?? left.lastMessage?.createdAt ?? '';
      const rightDate = this.dates.get(right.id) ?? right.lastMessage?.createdAt ?? '';
      if (leftDate !== rightDate) return leftDate < rightDate ? 1 : -1;
      return left.id < right.id ? -1 : 1;
    });
  }

  get selectedChat(): Thread | undefined {
    const id = this.selection;
    if (id) {
      const thread = this.threads.find((t) => t.id === id);
      if (thread) return thread;
    }
    return this.ordered[0];
  }

  agent(id?: string): Agent | undefined {
    return this.agents.find((agent) => agent.id === id);
  }

  agentsFor(thread: Thread): Agent[] {
    const ids = thread.agentIDs.length ? thread.agentIDs : [thread.agentID];
    return ids.map((id) => this.agent(id)).filter((a): a is Agent => a !== undefined);
  }

  get pinned(): Thread[] {
    return this.ordered.filter((thread) => thread.pinned);
  }

  /** The sections that show in the sidebar: Unassigned and named ones holding chats. */
  get visibleSections(): Section[] {
    const all = this.sections.some((section) => section.id === UNASSIGNED)
      ? this.sections
      : [{ id: UNASSIGNED, name: 'Unassigned', collapsed: false, order: 0 } as Section, ...this.sections];
    return all
      .filter((section) => section.id !== UNASSIGNED || this.hasChats(section))
      .sort((left, right) => left.order - right.order);
  }

  private hasChats(section: Section): boolean {
    return this.threads.some((thread) => (thread.sectionID || UNASSIGNED) === section.id && !thread.pinned);
  }

  chatsOf(section: Section): Thread[] {
    const chats = this.ordered.filter(
      (thread) => !thread.pinned && (thread.sectionID || UNASSIGNED) === section.id,
    );
    if (section.id === UNASSIGNED) return chats;
    return chats.sort((left, right) => (left.sectionOrder ?? 0) - (right.sectionOrder ?? 0));
  }

  view(thread: Thread): ActivityView {
    return this.activity[thread.id] ?? { typing: false, working: false, unread: thread.unreadCount > 0 };
  }

  issue(message: Message) {
    return replyIssue(message, Object.values(this.runs));
  }

  private restoreRuns(threads: Thread[]): void {
    const conversations = new Set(threads.map((thread) => thread.id));
    for (const run of Object.values(this.runs)) {
      if (!conversations.has(run.conversationID)) delete this.runs[run.id];
    }
    for (const thread of threads) {
      const runs = thread.runs ?? [];
      const ids = new Set(runs.map((run) => run.id));
      for (const run of Object.values(this.runs)) {
        if (run.conversationID === thread.id && !ids.has(run.id) && run.seq <= thread.lastSeq) delete this.runs[run.id];
      }
      for (const run of runs) this.recordRun(run, true);
    }
  }

  private recordRun(run: Run, restore = false): void {
    const existing = this.runs[run.id];
    if (existing && (run.seq < existing.seq || (!restore && run.seq === existing.seq))) return;
    if (!existing || JSON.stringify(existing) !== JSON.stringify(run)) this.runs[run.id] = run;
  }

  async load(): Promise<void> {
    const revision = this.sidebarRevision;
    const [agents, threads, sidebar, settings, network] = await Promise.all([
      this.client.agents(),
      this.client.threads(),
      this.client.sidebar(),
      this.client.settings().catch(() => undefined),
      this.client.network().catch(() => undefined),
    ]);
    this.agents = restoreAvatars(this.address, agents, this.client, (id, avatar) => {
      this.agents = this.agents.map((entry) => entry.id === id ? { ...entry, avatar } : entry);
    });
    if (!this.sidebarPending && revision === this.sidebarRevision) {
      this.restoreRuns(threads);
      this.dates = new Map(threads.map((thread) => [thread.id, thread.lastMessage?.createdAt ?? '']));
      this.threads = threads;
      restoreGroups(this);
      this.applySidebar(sidebar);
      void this.cache.saveSidebar({ agents: $state.snapshot(this.agents), threads, sections: sidebar.sections });
    }
    if (settings?.appearance) applyAppearance(settings.appearance);
    this.error = '';
    this.online = true;
    this.rememberIdentity(network);
    await this.restoreOutbox();
  }

  async restore(): Promise<boolean> {
    const saved = await this.cache.sidebar();
    if (!saved || !Array.isArray(saved.agents) || !Array.isArray(saved.threads) || !Array.isArray(saved.sections)) return false;
    this.agents = restoreAvatars(this.address, saved.agents, this.client, (id, avatar) => {
      this.agents = this.agents.map((entry) => entry.id === id ? { ...entry, avatar } : entry);
    });
    this.dates = new Map(saved.threads.map((thread) => [thread.id, thread.lastMessage?.createdAt ?? '']));
    this.threads = saved.threads;
    this.restoreRuns(saved.threads);
    restoreGroups(this);
    this.sections = saved.sections;
    this.identity = await this.cache.identity();
    const selected = this.selectedChat;
    if (selected) await this.loadChat(selected.id);
    await this.restoreOutbox();
    return true;
  }

  start(refreshNow = false): void {
    this.stopped = false;
    const poll = async () => {
      if (this.stopped) return;
      await this.sync();
      if (!this.stopped) {
        clearTimeout(this.polling);
        this.polling = setTimeout(poll, document.hidden ? 30000 : 3000);
      }
    };
    this.visible = () => { if (!document.hidden && !this.stopped) { clearTimeout(this.polling); void poll(); } };
    document.addEventListener('visibilitychange', this.visible);
    if (this.online) { void this.flushOutbox(); this.startConnections(); }
    void this.notifications.start();
    if (refreshNow) void poll();
    else this.polling = setTimeout(poll, 3000);
  }

  dispose(): void {
    this.stopped = true;
    stopAvatars(this.address, this.agents);
    stopGroups(this);
    clearTimeout(this.polling);
    if (this.visible) document.removeEventListener('visibilitychange', this.visible);
    this.visible = undefined;
    this.channel?.close();
    this.channel = undefined;
    this.connections?.abort();
    this.connections = undefined;
    this.notifications.dispose();
    for (const id of this.streams.keys()) this.stop(id);
    for (const id of this.messageTimers.keys()) this.persistMessages(id);
  }

  async setLocation(enabled: boolean): Promise<void> {
    if (enabled) await currentLocation();
    this.locationEnabled = enabled;
    try { localStorage.setItem(`polymux.location.${this.address}`, enabled ? '1' : '0'); } catch { /* Keep this session's choice. */ }
    this.connections?.abort();
    this.connections = undefined;
    if (this.online) this.startConnections();
  }

  private startConnections(): void {
    this.connections?.abort();
    const controller = new AbortController();
    this.connections = controller;
    void (async () => {
      await this.reportClient().catch(() => undefined);
      while (!controller.signal.aborted && !this.stopped) {
        try {
          for await (const request of this.client.clientRequests(controller.signal)) {
            if (this.clientSeen.has(request.id)) continue;
            this.clientSeen.add(request.id);
            if (this.clientSeen.size > 1024) this.clientSeen.clear();
            if (request.capability === 'location') void this.answerLocation(request, controller.signal);
            else await this.client.answerClient(request.id, { error: 'This connection is unavailable on this device.' }).catch(() => undefined);
          }
        } catch { /* Reconnect below unless this session closed. */ }
        if (!controller.signal.aborted) await new Promise((resolve) => setTimeout(resolve, 3000));
      }
    })();
  }

  private async reportClient(): Promise<void> {
    if (!this.online) return;
    await this.client.reportClient({
      platform: 'macos',
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
      capabilities: this.locationEnabled ? ['location'] : [],
    });
  }

  private answerLocation(request: ClientRequest, signal: AbortSignal): void {
    if (signal.aborted) return;
    if (!this.locationEnabled) {
      void this.client.answerClient(request.id, { error: 'Location access is off for Polymux on this device.' }).catch(() => undefined);
      return;
    }
    void currentLocation().then((reading) => {
      if (signal.aborted) return;
      void this.client.answerClient(request.id, { output: JSON.stringify(reading) }).catch(() => undefined);
    }).catch((error: Error) => {
      if (signal.aborted) return;
      void this.client.answerClient(request.id, { error: error.message || 'Couldn’t get a location fix.' }).catch(() => undefined);
    });
  }

  private sync(): Promise<void> {
    if (this.syncing) return this.syncing;
    this.syncing = (async () => {
      const before = JSON.stringify([this.agents, this.threads, this.sections]);
      const revision = this.sidebarRevision;
      try {
        const [agents, freshThreads, sidebar, settings, network] = await Promise.all([
          this.client.agents(), this.client.threads(), this.client.sidebar(),
          this.client.settings().catch(() => undefined),
          this.client.network().catch(() => undefined),
        ]);
        if (this.stopped) return;
        const threads = overlayGroups(this, freshThreads);
        const shownAgents = overlayAvatars(this.address, agents);
        if (!this.sidebarPending && revision === this.sidebarRevision && JSON.stringify([this.agents, this.threads, this.sections]) === before) {
          this.restoreRuns(freshThreads);
          const agentsChanged = JSON.stringify(this.agents) !== JSON.stringify(shownAgents);
          const threadsChanged = JSON.stringify(this.threads) !== JSON.stringify(threads);
          const sectionsChanged = JSON.stringify(this.sections) !== JSON.stringify(sidebar.sections);
          if (agentsChanged) this.agents = shownAgents;
          if (threadsChanged) {
            this.dates = new Map(threads.map((thread) => [thread.id, thread.lastMessage?.createdAt ?? '']));
            this.threads = threads;
          }
          if (sectionsChanged) this.sections = sidebar.sections;
          if (agentsChanged || threadsChanged || sectionsChanged) void this.cache.saveSidebar({ agents: shownAgents, threads, sections: sidebar.sections });
        }
        if (settings?.appearance && settings.appearance !== localStorage.getItem('polymux.appearance')) applyAppearance(settings.appearance);
        this.online = true;
        this.error = '';
        this.rememberIdentity(network);
        if (!this.connections) this.startConnections();
        void this.flushOutbox();
        if (!this.sidebarPending && this.sidebarRetry) {
          const { id, destination } = this.sidebarRetry;
          if (this.threads.some(thread => thread.id === id)) void this.placeChat(id, destination, true);
          else { this.sidebarRetry = undefined; this.sidebarIssue = ''; }
        }
      } catch {
        if (!this.stopped) this.online = false;
      }
    })().finally(() => { this.syncing = undefined; });
    return this.syncing;
  }

  applySidebar(snapshot: SidebarSnapshot): void {
    this.sections = snapshot.sections;
  }

  private rememberIdentity(network?: { host?: string; current?: string }): void {
    if (!network?.host || !network.current) return;
    this.identity = { host: network.host, current: network.current };
    void this.cache.saveIdentity(this.identity);
  }

  async refresh(): Promise<void> {
    const revision = this.sidebarRevision;
    const before = JSON.stringify([this.threads, this.sections]);
    const [threads, sidebar] = await Promise.all([this.client.threads(), this.client.sidebar()]);
    if (this.sidebarPending || revision !== this.sidebarRevision || before !== JSON.stringify([this.threads, this.sections])) return;
    this.restoreRuns(threads);
    this.threads = overlayGroups(this, threads);
    this.applySidebar(sidebar);
  }

  private loadChat(id: string): Promise<void> {
    const running = this.chatLoads.get(id);
    if (running) return running;
    const task = (async () => {
      const cached = await this.cache.chat(id);
      this.cursors.set(id, cached?.cursor ?? 0);
      if (cached && !this.messages[id]) this.messages[id] = cached.messages;
      for (const run of cached?.runs ?? []) this.recordRun(run, true);
    })();
    this.chatLoads.set(id, task);
    return task;
  }

  async open(thread: Thread, select = true): Promise<void> {
    if (select) this.selection = thread.id;
    const running = this.opening.get(thread.id);
    if (running) return running;
    const task = Promise.resolve().then(async () => {
      this.draftStore(thread.id).start();
      await this.restoreFiles(thread.id);
      await this.loadChat(thread.id);
      if (!this.streamReady.has(thread.id) && !this.replayBases.has(thread.id)) {
        const base = $state.snapshot(this.messages[thread.id] ?? []);
        this.replayBases.set(thread.id, (this.cursors.get(thread.id) ?? 0) > 0 ? base : base.filter((message) => message.id.startsWith('local-')));
        if (!this.replayAcks.has(thread.id)) this.replayAcks.set(thread.id, new Map());
      }
      const before = new Map((this.messages[thread.id] ?? []).map((message) => [message.id, JSON.stringify(message)]));
      if (!this.streamReady.has(thread.id)) try {
        const messages = await this.client.messages(thread.id);
        const existing = this.messages[thread.id] ?? [];
        const current = new Map(existing.map((message) => [message.id, message]));
        const merged = messages.map((message) => {
          const previous = current.get(message.id);
          return previous && before.get(message.id) !== JSON.stringify(previous) ? previous : message;
        });
        const fetched = new Set(messages.map((message) => message.id));
        const pending = existing.filter((message) => message.id.startsWith('local-') && !fetched.has(message.id.slice(6).replace(/-/g, '')));
        this.messages[thread.id] = [...existing.filter((message) => !message.id.startsWith('local-') && !fetched.has(message.id)), ...merged].sort((a, b) => a.seq - b.seq).concat(pending);
        this.persistMessages(thread.id);
      } catch { /* Keep cached messages available while offline. */ }
      void this.client.markRead(thread.id, thread.lastSeq).catch(() => undefined);
      const current = this.threads.find((t) => t.id === thread.id);
      if (current) current.unreadCount = 0;
    });
    this.opening.set(thread.id, task);
    try { await task; } finally { this.opening.delete(thread.id); }
  }

  addFiles(threadID: string, files: File[]): void {
    const added = files.map((file) => ({ id: crypto.randomUUID(), file, url: URL.createObjectURL(file) }));
    this.files[threadID] = [...(this.files[threadID] ?? []), ...added];
    void this.saveFiles(threadID);
  }

  removeFile(threadID: string, id: string): void {
    const files = this.files[threadID] ?? [];
    const removed = files.find((file) => file.id === id);
    if (removed) URL.revokeObjectURL(removed.url);
    this.files[threadID] = files.filter((file) => file.id !== id);
    void this.saveFiles(threadID);
  }

  private async storedFiles(files: DraftFile[]): Promise<StoredFile[]> {
    return Promise.all(files.map(async ({ id, file }) => ({ id, bytes: await file.arrayBuffer(), name: file.name, type: file.type, modified: file.lastModified })));
  }

  private draftFile(file: StoredFile): DraftFile {
    const value = new File([file.bytes], file.name, { type: file.type, lastModified: file.modified });
    return { id: file.id, file: value, url: URL.createObjectURL(value) };
  }

  private restoreFiles(threadID: string): Promise<void> {
    let task = this.restoredFiles.get(threadID);
    if (!task) {
      task = this.outbox.drafts(threadID).then((files) => {
        const current = this.files[threadID] ?? [];
        const ids = new Set(current.map((file) => file.id));
        this.files[threadID] = [...files.filter((file) => !ids.has(file.id)).map((file) => this.draftFile(file)), ...current];
      }).catch(() => undefined);
      this.restoredFiles.set(threadID, task);
    }
    return task;
  }

  private fileWrite(threadID: string, action: () => Promise<void>): Promise<void> {
    const previous = this.fileWrites.get(threadID) ?? Promise.resolve();
    const task = previous.catch(() => undefined).then(action);
    this.fileWrites.set(threadID, task);
    void task.finally(() => { if (this.fileWrites.get(threadID) === task) this.fileWrites.delete(threadID); }).catch(() => undefined);
    return task;
  }

  private async saveFiles(threadID: string): Promise<void> {
    const initial = [...(this.files[threadID] ?? [])];
    try {
      await this.fileWrite(threadID, async () => {
        await this.restoreFiles(threadID);
        const ids = new Set(initial.map((file) => file.id));
        const current = this.files[threadID] ?? [];
        await this.outbox.saveDraft(threadID, await this.storedFiles([...initial, ...current.filter((file) => !ids.has(file.id))]));
      });
    } catch { this.outboxIssues[threadID] = 'Couldn’t save attachments on this device.'; }
  }

  private restoreOutbox(): Promise<void> {
    return this.restoredOutbox ??= (async () => {
      try {
        const entries = await this.outbox.entries();
        for (const entry of entries) {
          if (!this.messages[entry.threadID]) { await this.loadChat(entry.threadID); this.messages[entry.threadID] ??= []; }
          const list = this.messages[entry.threadID];
          const serverID = entry.id.slice(6).replace(/-/g, '');
          if (list.some((message) => message.id === entry.id || message.id === serverID)) continue;
          if (entry.accepted) { this.messages[entry.threadID] = [...list, entry.accepted]; continue; }
          const urls = new Map(entry.files.map((file) => [file.id, this.draftFile(file).url]));
          const message = { ...entry.message, status: entry.permanent ? 'error' as const : entry.message.status,
            parts: entry.message.parts.map((part) => part.kind === 'attachment' && part.attachment && urls.has(part.attachment.id)
              ? { ...part, attachment: { ...part.attachment, url: urls.get(part.attachment.id)! } } : part) };
          this.messages[entry.threadID] = [...list, message];
        }
      } catch { /* The composer reports storage failures when sending. */ }
    })();
  }

  async send(thread: Thread, text: string, attachments: Attachment[] = [], reply?: ReplyContext, files: DraftFile[] = [], mentions: string[] = []): Promise<boolean> {
    const local = files.map(({ id, file, url }): Attachment => ({
      id, url, name: file.name, mimeType: file.type || 'application/octet-stream', size: file.size,
      kind: file.type.startsWith('image/') ? 'image' : file.type.startsWith('video/') ? 'video' : file.type.startsWith('audio/') ? 'audio' : 'file',
      indexingState: 'pending',
    }));
    const optimistic: Message = {
      id: `local-${crypto.randomUUID()}`,
      threadID: thread.id,
      role: 'user',
      parts: [
        ...(text ? [{ kind: 'text', text } as const] : []),
        ...attachments.map((attachment) => ({ kind: 'attachment', attachment } as const)),
        ...local.map((attachment) => ({ kind: 'attachment', attachment } as const)),
      ],
      createdAt: new Date().toISOString(),
      status: 'pending',
      seq: 0,
      agentID: '',
      runID: '',
      mentions: [...mentions],
      reactions: [],
      replyToID: reply?.messageID,
      reply,
    };
    this.messages[thread.id] = [...(this.messages[thread.id] ?? []), optimistic];
    const selected = new Set(files.map((file) => file.id));
    const identity = this.identity ? { ...this.identity } : undefined;
    this.files[thread.id] = (this.files[thread.id] ?? []).filter((file) => !selected.has(file.id));
    this.sending[thread.id] = (this.sending[thread.id] ?? 0) + 1;
    try {
      await this.fileWrite(thread.id, async () => {
        await this.restoreFiles(thread.id);
        const entry: Outgoing = {
          address: this.address, id: optimistic.id, threadID: thread.id, text,
          attachments: $state.snapshot(attachments), reply: reply ? $state.snapshot(reply) : undefined,
          message: $state.snapshot(optimistic), files: await this.storedFiles(files), identity,
        };
        await this.outbox.enqueue(entry, await this.storedFiles(this.files[thread.id] ?? []));
      });
      delete this.outboxIssues[thread.id];
      void this.flushThread(thread.id);
      return true;
    } catch {
      this.unpersisted.add(optimistic.id);
      this.messages[thread.id] = this.messages[thread.id].map((m) =>
        m.id === optimistic.id ? { ...m, status: 'error' as const } : m,
      );
      this.files[thread.id] = [...files, ...(this.files[thread.id] ?? [])];
      this.outboxIssues[thread.id] = 'Couldn’t save this message on this device. Your draft and attachments are kept here.';
      return false;
    } finally { this.sending[thread.id] -= 1; }
  }

  async discard(threadID: string, id: string): Promise<void> {
    const message = this.messages[threadID]?.find((item) => item.id === id);
    if (!message || !id.startsWith('local-') || message.role !== 'user' || message.seq !== 0 || this.sending[threadID]) return;
    this.discarding.add(threadID);
    this.sending[threadID] = (this.sending[threadID] ?? 0) + 1;
    let claimed = false;
    try {
      if (!this.unpersisted.has(id)) {
        claimed = await this.outbox.claim(threadID);
        if (!claimed) {
          this.outboxIssues[threadID] = 'Couldn’t delete this message while it is being sent.';
          return;
        }
        const result = await this.outbox.discard(threadID, id);
        if (result !== 'removed') {
          this.outboxIssues[threadID] = result === 'busy' ? 'Couldn’t delete this message while it is being sent.' : 'This message may have been sent. Refresh to check.';
          return;
        }
      }
      const persisted = !this.unpersisted.has(id);
      this.unpersisted.delete(id);
      this.dropMessage(threadID, id);
      if (persisted) {
        try { this.channel?.postMessage({ threadID, id }); } catch { /* The durable removal already succeeded. */ }
      }
      delete this.outboxIssues[threadID];
    } catch { this.outboxIssues[threadID] = 'Couldn’t delete this message.'; }
    finally {
      if (claimed) await this.outbox.release(threadID).catch(() => undefined);
      this.discarding.delete(threadID);
      this.sending[threadID] -= 1;
    }
  }

  private dropMessage(threadID: string, id: string): void {
    const list = this.messages[threadID] ?? [];
    const message = list.find((item) => item.id === id && item.role === 'user' && item.seq === 0);
    if (!message) return;
    this.messages[threadID] = list.filter((item) => item !== message);
    const draftURLs = new Set((this.files[threadID] ?? []).map((file) => file.url));
    for (const part of message.parts) {
      if (part.kind === 'attachment' && part.attachment?.url?.startsWith('blob:') && !draftURLs.has(part.attachment.url)) URL.revokeObjectURL(part.attachment.url);
    }
  }

  private async receiveDiscard(threadID: string, id: string): Promise<void> {
    try {
      await this.restoreOutbox();
      await this.outbox.ifMissing(id, () => {
        if (!this.stopped && !this.unpersisted.has(id)) this.dropMessage(threadID, id);
      });
    } catch { /* Keep the visible message if its durable state cannot be checked. */ }
  }

  private async flushOutbox(): Promise<void> {
    try {
      await this.restoreOutbox();
      const entries = await this.outbox.entries();
      for (const threadID of new Set(entries.map((entry) => entry.threadID))) void this.flushThread(threadID);
    } catch { /* Preserve the queue for a later retry. */ }
  }

  private flushThread(threadID: string): Promise<void> {
    if (this.discarding.has(threadID)) return Promise.resolve();
    const running = this.flushing.get(threadID);
    if (running) return running;
    this.sending[threadID] = (this.sending[threadID] ?? 0) + 1;
    const task = this.deliverThread(threadID).finally(() => { this.flushing.delete(threadID); this.sending[threadID] -= 1; });
    this.flushing.set(threadID, task);
    return task;
  }

  private async deliverThread(threadID: string): Promise<void> {
    let claimed = false;
    let heartbeat: ReturnType<typeof setInterval> | undefined;
    try {
      claimed = await this.outbox.claim(threadID);
      if (!claimed) return;
      heartbeat = setInterval(() => { void this.outbox.claim(threadID).catch(() => undefined); }, 10000);
      const entries = (await this.outbox.entries()).filter((entry) => entry.threadID === threadID);
      for (const entry of entries) {
        if (this.stopped) break;
        if (entry.permanent) continue;
        try {
          if (!entry.accepted) {
            const network = await this.client.network().catch(() => undefined);
            if (!entry.identity || !network?.host || !network.current || entry.identity.host !== network.host || entry.identity.current !== network.current) {
              this.outboxIssues[threadID] = !entry.identity
                ? 'Can’t verify which server this message belongs to. It’s kept on this device.'
                : !network?.host || !network.current
                  ? 'Waiting to verify the server before sending. Your message is kept on this device.'
                  : 'The server changed. Your message is kept on this device.';
              return;
            }
            delete this.outboxIssues[threadID];
            for (const file of entry.files) {
              if (file.uploaded) continue;
              if (!await this.outbox.claim(threadID) || this.stopped) return;
              file.uploaded = await this.client.upload(new File([file.bytes], file.name, { type: file.type, lastModified: file.modified }));
              await this.outbox.save(entry);
            }
            if (!await this.outbox.claim(threadID) || this.stopped) return;
            entry.accepted = await this.client.send(threadID, {
              id: entry.id.slice(6).replace(/-/g, ''), text: entry.text,
              attachmentIDs: [...entry.attachments.map((file) => file.id), ...entry.files.map((file) => file.uploaded!.id)],
              replyToID: entry.reply?.messageID, replyPart: entry.reply?.partIndex,
              mentions: entry.message.mentions,
            });
            await this.outbox.save(entry);
          }
          const sent = entry.accepted;
          const list = this.messages[threadID] ?? [];
          const current = list.find((message) => message.id === sent.id) ?? sent;
          this.messages[threadID] = [...list.filter((message) => message.id !== entry.id && message.id !== sent.id), current].sort((a, b) => a.seq - b.seq);
          const keptURLs = new Set((this.files[threadID] ?? []).map((file) => file.url));
          for (const other of list.filter((message) => message.id !== entry.id)) {
            for (const part of other.parts) if (part.attachment?.url) keptURLs.add(part.attachment.url);
          }
          for (const part of list.find((message) => message.id === entry.id)?.parts ?? []) {
            const url = part.attachment?.url;
            if (url?.startsWith('blob:') && !keptURLs.has(url)) URL.revokeObjectURL(url);
          }
          const saved = await this.saveChat(threadID);
          if (!saved) return;
          await this.outbox.remove(entry.id);
        } catch (error) {
          if (error instanceof ResponseError && error.status >= 400 && error.status < 500 && error.status !== 408 && error.status !== 429) {
            entry.permanent = true;
            await this.outbox.save(entry);
          }
          this.messages[threadID] = (this.messages[threadID] ?? []).map((message) => message.id === entry.id ? { ...message, status: 'error' as const } : message);
          if (!entry.permanent) return;
        }
      }
    } catch { /* Preserve the queue for a later retry. */ }
    finally {
      clearInterval(heartbeat);
      if (claimed) await this.outbox.release(threadID).catch(() => undefined);
    }
  }

  async saveDraft(threadID: string, text: string, mentions?: ChatDraftMention[]): Promise<void> {
    this.draftStore(threadID).edit(text, mentions ? $state.snapshot(mentions) : undefined);
  }

  saveReply(threadID: string, reply?: ReplyContext): void {
    this.draftStore(threadID).reply(reply);
  }

  acceptChoice(threadID: string, accepted: Message): void {
    const current = this.messages[threadID]?.find((item) => item.id === accepted.id);
    const source = accepted.parts.find((part) => part.kind === 'choice');
    const target = current?.parts.find((part) => part.kind === 'choice');
    if (!source?.answerID) return;
    this.rememberAck(threadID, { kind: 'choice', messageID: accepted.id, answerID: source.answerID, answerText: source.answerText });
    if (!target || target.answerID) return;
    target.answerID = source.answerID;
    target.answerText = source.answerText;
    this.persistMessages(threadID);
  }

  reactionKey(threadID: string, messageID: string): string {
    return JSON.stringify([threadID, messageID]);
  }

  reactions(threadID: string, message: Message): Reaction[] {
    const pending = this.reactionPending[this.reactionKey(threadID, message.id)];
    if (!pending) return message.reactions ?? [];
    const reactions = (message.reactions ?? []).filter((item) => item.agentID || item.emoji !== pending.emoji);
    if (!pending.remove) reactions.push({ emoji: pending.emoji });
    return reactions;
  }

  async react(threadID: string, message: Message, emoji: string): Promise<void> {
    const key = this.reactionKey(threadID, message.id);
    if (this.reactionPending[key] || message.seq <= 0 || !['done', 'error'].includes(message.status)) return;
    const request = { emoji, remove: (message.reactions ?? []).some((item) => !item.agentID && item.emoji === emoji) };
    const version = this.reactionVersions.get(key);
    this.reactionPending[key] = request;
    delete this.reactionIssues[key];
    try {
      const accepted = await this.client.react(threadID, message.id, request);
      if (this.reactionVersions.get(key) === version) {
        this.rememberAck(threadID, { kind: 'reaction', messageID: message.id, request });
        const current = this.messages[threadID]?.find((item) => item.id === message.id);
        if (current) {
          current.reactions = accepted.reactions;
          this.persistMessages(threadID);
        }
      }
    } catch {
      this.reactionIssues[key] = 'Couldn’t update this reaction.';
    } finally {
      delete this.reactionPending[key];
    }
  }

  private rememberAck(threadID: string, ack: Omit<Extract<ReplayAck, { kind: 'choice' }>, 'serial'> | Omit<Extract<ReplayAck, { kind: 'reaction' }>, 'serial'>): void {
    const pending = this.replayAcks.get(threadID);
    if (!pending) return;
    const key = JSON.stringify([ack.kind, ack.messageID, ack.kind === 'reaction' ? ack.request.emoji : '']);
    pending.set(key, { ...ack, serial: ++this.ackSerial });
  }

  private applyAck(threadID: string, ack: ReplayAck): void {
    const message = this.messages[threadID]?.find((item) => item.id === ack.messageID);
    if (!message) return;
    if (ack.kind === 'choice') {
      const part = message.parts.find((item) => item.kind === 'choice');
      if (part && !part.answerID) { part.answerID = ack.answerID; part.answerText = ack.answerText; }
    } else {
      const reactions = (message.reactions ?? []).filter((item) => item.agentID || item.emoji !== ack.request.emoji);
      if (!ack.request.remove) reactions.push({ emoji: ack.request.emoji });
      message.reactions = reactions;
    }
  }

  private draftStore(threadID: string): Draft {
    let store = this.draftStores.get(threadID);
    if (!store) {
      store = new Draft(`polymux.draft.${this.address}.${threadID}`, this.client, threadID, (state, issue) => {
        this.drafts[threadID] = state;
        this.draftIssues[threadID] = issue;
      });
      this.draftStores.set(threadID, store);
    }
    return store;
  }

  /** Follows one conversation's events, applying them to the message list. */
  async follow(threadID: string): Promise<void> {
    if (this.streams.has(threadID)) return;
    const abort = new AbortController();
    this.streams.set(threadID, abort);
    // Keep the caller's reactive effect from tracking the session reads below.
    await Promise.resolve();
    if (abort.signal.aborted) return;
    const thread = this.threads.find((item) => item.id === threadID);
    if (thread) await this.open(thread, false);
    else await this.loadChat(threadID);
    if (abort.signal.aborted) return;
    this.streamReady.add(threadID);
    const run = async (): Promise<void> => {
      if (abort.signal.aborted) return;
      try {
        if (!this.replayAcks.has(threadID)) this.replayAcks.set(threadID, new Map());
        let after = this.cursors.get(threadID) ?? 0;
        let fence = this.ackSerial;
        const replay: Event[] = [];
        while (true) {
          fence = this.ackSerial;
          const page = await this.client.events(threadID, after);
          if (abort.signal.aborted) return;
          if (!page.length) break;
          for (const event of page) {
            if (event.conversationID !== threadID || event.seq !== after + 1) throw new Error('Event sequence gap');
            replay.push(event);
            after = event.seq;
          }
        }
        const base = this.replayBases.get(threadID);
        const shown = base ? this.messages[threadID] ?? [] : [];
        if (base) this.messages[threadID] = base;
        for (const event of replay) {
          this.apply(threadID, event, true);
          this.cursors.set(threadID, event.seq);
        }
        if (base) {
          const reconstructed = this.messages[threadID] ?? [];
          const known = new Set(reconstructed.map((message) => message.id));
          const missing = shown.filter((message) => !known.has(message.id)
            && (!message.id.startsWith('local-') || !known.has(message.id.slice(6).replace(/-/g, ''))));
          const combined = [...reconstructed, ...missing];
          this.messages[threadID] = [...combined.filter((message) => !message.id.startsWith('local-')).sort((a, b) => a.seq - b.seq),
            ...combined.filter((message) => message.id.startsWith('local-'))];
          this.replayBases.delete(threadID);
        }
        for (const ack of this.replayAcks.get(threadID)?.values() ?? []) {
          if (ack.serial > fence) this.applyAck(threadID, ack);
        }
        this.replayAcks.delete(threadID);
        if (base || replay.length) this.scheduleMessages(threadID);
        if (base) {
          const running = this.flushing.get(threadID);
          if (running) void running.then(() => this.flushThread(threadID));
          else void this.flushThread(threadID);
        }
        for await (const event of this.client.stream(threadID, after, abort.signal)) {
          if (abort.signal.aborted) return;
          if (event.seq <= (this.cursors.get(threadID) ?? after)) continue;
          if (event.conversationID !== threadID || event.seq !== (this.cursors.get(threadID) ?? after) + 1) throw new Error('Event sequence gap');
          this.apply(threadID, event);
          this.cursors.set(threadID, event.seq);
          this.scheduleMessages(threadID);
        }
        if (!abort.signal.aborted) setTimeout(() => void run(), 500);
      } catch {
        if (abort.signal.aborted) return;
        setTimeout(() => void run(), 2000);
      }
    };
    await run();
  }

  stop(threadID: string): void {
    this.draftStores.get(threadID)?.stop();
    this.streams.get(threadID)?.abort();
    this.streams.delete(threadID);
    this.streamReady.delete(threadID);
  }

  private apply(threadID: string, event: Event, replay = false): void {
    if (event.type === 'message.reactions') {
      const id = event.messageID ?? event.message?.id;
      if (id) {
        const key = this.reactionKey(threadID, id);
        if (!replay) this.reactionVersions.set(key, (this.reactionVersions.get(key) ?? 0) + 1);
        delete this.reactionIssues[key];
      }
    }
    const list = this.messages[threadID] ?? [];
    const push = (message: Message) => {
      let index = list.findIndex((m) => m.id === message.id);
      if (index < 0) {
        index = list.findIndex(
          (m) => m.id.startsWith('local-') && message.role === 'user' && m.seq === 0 && message.seq > 0
            && m.id.slice(6).replace(/-/g, '') === message.id,
        );
      }
      if (index >= 0) list.splice(index, 1, message);
      else list.push(message);
      list.sort((a, b) => a.seq - b.seq);
      this.messages[threadID] = [...list];
    };
    const thisRun = event.run;
    if (thisRun && thisRun.conversationID === event.conversationID) {
      this.recordRun(thisRun);
      const status = thisRun.status ?? '';
      const typing = status === 'running' && !thisRun.activity;
      const working = status === 'running' && !!thisRun.activity;
      this.activity[threadID] = {
        typing,
        working,
        unread: this.activity[threadID]?.unread ?? false,
      };
    }
    switch (event.type) {
      case 'message.start':
      case 'message.delta':
      case 'message.tool_call':
      case 'message.attachment':
      case 'message.error':
      case 'message.done':
      case 'message.reactions':
        if (event.message) push(event.message);
        else if (event.messageID) {
          const current = list.find((message) => message.id === event.messageID);
          if (!current) break;
          const message = $state.snapshot(current);
          if (event.type === 'message.delta' && event.text) {
            const part = message.parts.find((item) => item.kind === 'text');
            if (part) part.text = (part.text ?? '') + event.text;
            else message.parts.push({ kind: 'text', text: event.text });
            message.status = 'streaming';
          } else if (event.type === 'message.attachment' && event.attachment && !message.parts.some((item) => item.attachment?.id === event.attachment?.id)) {
            message.parts.push({ kind: 'attachment', attachment: event.attachment });
          } else if (event.type === 'message.done') message.status = 'done';
          else if (event.type === 'message.error') { message.status = 'error'; message.error = event.error; }
          push(message);
        }
        break;
      case 'run.update':
      case 'run.done':
        this.bumpPreview(threadID);
        break;
      default:
        break;
    }
  }

  private scheduleMessages(threadID: string): void {
    clearTimeout(this.messageTimers.get(threadID));
    this.messageTimers.set(threadID, setTimeout(() => this.persistMessages(threadID), 1000));
  }

  private async saveChat(threadID: string): Promise<boolean> {
    await this.loadChat(threadID);
    if (this.replayBases.has(threadID)) return false;
    const messages = this.messages[threadID];
    if (!messages) return false;
    return this.cache.saveChat(threadID, {
      messages: $state.snapshot(messages).filter((message) => !message.id.startsWith('local-')),
      cursor: this.cursors.get(threadID) ?? 0,
      runs: $state.snapshot(Object.values(this.runs).filter((run) => run.conversationID === threadID)),
    });
  }

  private persistMessages(threadID: string): void {
    clearTimeout(this.messageTimers.get(threadID));
    this.messageTimers.delete(threadID);
    void this.saveChat(threadID);
  }

  private bumpPreview(threadID: string): void {
    const list = this.messages[threadID];
    if (!list?.length) return;
    const now = Date.now();
    if (now - (this.mutes[threadID] ?? 0) < 1500) return;
    this.mutes[threadID] = now;
    const thread = this.threads.find((t) => t.id === threadID);
    if (thread) thread.lastMessage = list[list.length - 1];
  }

  bumpUnread(threadID: string): void {
    const thread = this.threads.find((t) => t.id === threadID);
    if (thread && thread.id !== this.selection) thread.unreadCount += 1;
  }
}
