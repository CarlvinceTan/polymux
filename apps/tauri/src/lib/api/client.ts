import type {
  Agent,
  AgentConfiguration,
  AgentProfile,
  Attachment,
  ChoiceAnswer,
  ClientRequest,
  ClientResult,
  ClientState,
  CreateAgentRequest,
  CreateThreadRequest,
  CredentialAnswer,
  DeleteResponse,
  Event,
  MCPConnection,
  MCPStatus,
  Message,
  NoticePage,
  Proactivity,
  Provider,
  ProviderLogin,
  ProviderOption,
  PushDevice,
  PushStatus,
  ReadRequest,
  ReactionRequest,
  Routine,
  Run,
  SearchHit,
  SearchResult,
  Section,
  SendRequest,
  Settings,
  SettingsPatch,
  SidebarSnapshot,
  Thread,
  UpdateAgentRequest,
  UpdateThreadRequest,
  VaultImport,
  VaultItem,
  VaultStatus,
  VaultSummary,
  VoiceRequest,
  VoiceResponse,
  WireError,
} from './contract';
import type { DraftState, Invite, NetworkInfo } from './models';
import { DraftConflictError, InvalidURLError, ResponseError, isOffline, isTransient, wireError } from './errors';
import { decodeSSE } from './sse';
import { decodeJSON } from './json';
import { loadCredential, localToken, proves } from './credential';
import { request } from './transport';
import { serverBase } from './address';

export interface VoiceFragment {
  text: string;
  agentID?: string;
}

interface SpokenEvent {
  agentID?: string;
  text?: string;
  message?: string;
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/** A bearer the client sends: given, or discovered once and kept. Failed discovery retries after a pause. */
class Credential {
  private readonly address: string;
  private readonly explicit?: string;
  private found?: string;
  private failedAt = 0;
  private running?: Promise<string | undefined>;

  constructor(address: string, explicit?: string) {
    this.address = address;
    this.explicit = explicit;
  }

  async value(): Promise<string | undefined> {
    if (this.explicit) return this.explicit;
    if (this.found !== undefined) return this.found;
    if (this.failedAt && Date.now() - this.failedAt < 2000) return undefined;
    this.running ??= this.discover();
    const token = await this.running;
    this.running = undefined;
    this.found = token;
    this.failedAt = token === undefined ? Date.now() : 0;
    return token;
  }

  private async discover(): Promise<string | undefined> {
    const paired = loadCredential(this.address)?.token;
    const token = await localToken(this.address);
    // The proof check keeps a stray process on the same port from being mistaken for the server.
    if (token && (await proves(token, new URL(this.address)))) return token;
    return paired;
  }
}

export class APIClient {
  readonly baseURL: URL;
  private readonly credential: Credential;
  private readonly endpoint: () => Promise<URL>;
  private readonly recover: () => Promise<void> | void;

  constructor(baseURL: URL | string, token?: string, recover: () => Promise<void> | void = () => {}) {
    this.baseURL = serverBase(baseURL);
    this.endpoint = async () => this.baseURL;
    this.recover = recover;
    this.credential = new Credential(this.baseURL.toString(), token);
  }

  resolve(path: string): URL {
    return new URL(path, this.baseURL);
  }

  async agents(): Promise<Agent[]> {
    return this.get('agents');
  }

  async network(): Promise<NetworkInfo> {
    return this.get('network');
  }

  async invite(): Promise<Invite> {
    return this.get('network/invite', undefined, false);
  }

  async draft(threadID: string): Promise<DraftState> {
    return this.get(`threads/${threadID}/draft`);
  }

  async saveDraft(threadID: string, value: DraftState): Promise<DraftState> {
    const response = await this.fetch('threads/' + threadID + '/draft', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(value),
    });
    if (response.status === 409) throw new DraftConflictError(await decodeJSON<DraftState>(response));
    await this.validate(response);
    return await decodeJSON<DraftState>(response);
  }

  async renameDevice(name: string): Promise<void> {
    await this.write('network/devices/self', 'PATCH', { name });
  }

  async labelDevice(id: string, label: string): Promise<void> {
    await this.write(`network/devices/${id}`, 'PATCH', { label });
  }

  async removeDevice(id: string): Promise<void> {
    await this.write(`network/devices/${id}`, 'DELETE');
  }

  async notices(after?: number): Promise<NoticePage> {
    const query = after !== undefined ? { after: String(after) } : undefined;
    return this.get('notifications', query);
  }

  async registerPush(device: PushDevice): Promise<PushStatus> {
    return this.write('notifications/device', 'PUT', device);
  }

  async settings(): Promise<Settings> {
    return this.get('settings');
  }

  async updateSettings(patch: SettingsPatch): Promise<Settings> {
    return this.write('settings', 'PATCH', patch);
  }

  async createAgent(contact: CreateAgentRequest): Promise<Agent> {
    return this.write('agents', 'POST', contact);
  }

  agentProfile(id: string): Promise<AgentProfile> {
    return this.get(`agents/${id}`);
  }

  deleteAgent(id: string): Promise<DeleteResponse> {
    return this.write(`agents/${id}`, 'DELETE');
  }

  deleteThread(id: string): Promise<DeleteResponse> {
    return this.write(`threads/${id}`, 'DELETE');
  }

  updateAgent(id: string, update: UpdateAgentRequest): Promise<AgentProfile> {
    return this.write(`agents/${id}`, 'PATCH', update);
  }

  updateAvatar(id: string, design: Agent['avatar']): Promise<Agent> {
    return this.write(`agents/${id}/avatar`, 'PATCH', design);
  }

  async threads(): Promise<Thread[]> {
    return this.get('threads');
  }

  routines(agentID: string): Promise<Routine[]> {
    return this.get(`agents/${agentID}/routines`);
  }

  /** The bot's workspace, tools and skills. */
  configuration(agentID: string): Promise<AgentConfiguration> {
    return this.get(`agents/${agentID}/configuration`);
  }

  /** The MCP connections the bot can use. */
  connections(agentID: string): Promise<MCPConnection[]> {
    return this.get(`agents/${agentID}/mcp`);
  }

  /** Whether each of the bot's MCP connections answers, probed on request. */
  mcpStatus(agentID: string): Promise<MCPStatus[]> {
    return this.get(`agents/${agentID}/mcp/status`);
  }

  proactivity(agentID: string): Promise<Proactivity> {
    return this.get(`agents/${agentID}/proactivity`);
  }

  /** Cancels a routine the agent created. Cheap to redo, so it applies immediately. */
  deleteRoutine(agentID: string, routineID: string): Promise<DeleteResponse> {
    return this.write(`agents/${agentID}/routines/${routineID}`, 'DELETE');
  }

  answerChoice(threadID: string, messageID: string, optionID: string, text?: string): Promise<Message> {
    const answer: ChoiceAnswer = { optionID, text };
    return this.write(`threads/${threadID}/messages/${messageID}/answer`, 'POST', answer);
  }

  react(threadID: string, messageID: string, request: ReactionRequest): Promise<Message> {
    return this.write(`threads/${threadID}/messages/${messageID}/reaction`, 'PUT', request);
  }

  answerCredential(threadID: string, messageID: string, value?: string, skip = false): Promise<Message> {
    const answer: CredentialAnswer = { value, skip };
    return this.write(`threads/${threadID}/messages/${messageID}/credential`, 'POST', answer);
  }

  vaultStatus(): Promise<VaultStatus> {
    return this.get('vault');
  }

  vaultItems(): Promise<VaultSummary[]> {
    return this.get('vault/items');
  }

  vaultItem(id: string): Promise<VaultItem> {
    return this.get(`vault/items/${encodeURIComponent(id)}`);
  }

  vaultSaveItem(item: VaultItem): Promise<VaultItem> {
    return this.write(`vault/items/${encodeURIComponent(item.id)}`, 'PUT', item);
  }

  vaultDeleteItem(id: string, revision: number): Promise<void> {
    return this.write(`vault/items/${encodeURIComponent(id)}`, 'DELETE', undefined, { revision: String(revision) });
  }

  async vaultImport(body: Blob | FormData): Promise<VaultImport> {
    const headers = body instanceof Blob ? { 'Content-Type': body.type || 'application/octet-stream' } : undefined;
    const response = await this.fetch('vault/import', { method: 'POST', body, headers });
    await this.validate(response);
    return decodeJSON<VaultImport>(response);
  }

  saveVaultCard(messageID: string, item: VaultItem): Promise<VaultItem> {
    return this.write(`vault/cards/${encodeURIComponent(messageID)}/save`, 'POST', item);
  }

  async importVaultCard(messageID: string, body: Blob | FormData): Promise<VaultImport> {
    const headers = body instanceof Blob ? { 'Content-Type': body.type || 'application/octet-stream' } : undefined;
    const response = await this.fetch(`vault/cards/${encodeURIComponent(messageID)}/import`, { method: 'POST', body, headers });
    await this.validate(response);
    return decodeJSON<VaultImport>(response);
  }

  setProactivity(agentID: string, settings: Proactivity): Promise<Proactivity> {
    return this.write(`agents/${agentID}/proactivity`, 'PATCH', settings);
  }

  createThread(agentIDs: string[], title?: string): Promise<Thread> {
    const request: CreateThreadRequest = { agentIDs, title };
    return this.write('threads', 'POST', request);
  }

  updateThread(id: string, title?: string, agentIDs?: string[], pinned?: boolean, pinOrder?: number): Promise<Thread> {
    const request: UpdateThreadRequest = { agentIDs, title, pinned, pinOrder };
    return this.write(`threads/${id}`, 'PATCH', request);
  }

  async messages(threadID: string, before?: number): Promise<Message[]> {
    const query: Record<string, string> = { limit: '100' };
    if (before !== undefined) query.before = String(before);
    return this.get(`threads/${threadID}/messages`, query);
  }

  async sidebar(): Promise<SidebarSnapshot> {
    return this.get('sidebar');
  }

  createSection(name: string): Promise<Section> {
    return this.write('sections', 'POST', { name });
  }

  updateSection(id: string, name?: string, collapsed?: boolean): Promise<Section> {
    return this.write(`sections/${id}`, 'PATCH', { name, collapsed });
  }

  async providers(): Promise<Provider[]> {
    return this.get('providers');
  }

  orderProviders(ids: string[]): Promise<Provider[]> {
    return this.write('providers/order', 'PUT', ids);
  }

  async providerOptions(): Promise<ProviderOption[]> {
    return this.get('providers/options');
  }

  startLogin(type: string, auth: string, provider?: string): Promise<ProviderLogin> {
    return this.write('providers/logins', 'POST', { type, auth, provider });
  }

  async providerLogin(id: string, after: number): Promise<ProviderLogin> {
    const response = await this.fetch(`providers/logins/${id}`, {
      headers: { Accept: 'application/json' },
      query: { after: String(after) },
      timeout: 35000,
    });
    await this.validate(response);
    return await decodeJSON<ProviderLogin>(response);
  }

  answerLogin(id: string, prompt: number, value: string): Promise<void> {
    return this.write(`providers/logins/${id}/answer`, 'POST', { prompt, value });
  }

  async cancelLogin(id: string): Promise<void> {
    await this.write(`providers/logins/${id}`, 'DELETE');
  }

  saveProvider(provider: Provider, isNew: boolean): Promise<Provider> {
    return this.write(isNew ? 'providers' : `providers/${provider.id}`, isNew ? 'POST' : 'PATCH', provider);
  }

  renameProvider(id: string, name: string): Promise<Provider> {
    return this.write(`providers/${id}`, 'PATCH', { name });
  }

  updateProvider(type: string, enabled?: boolean, models?: Record<string, boolean>): Promise<Provider[]> {
    return this.write(`providers/groups/${type}`, 'PATCH', { enabled, models });
  }

  async setProviderKey(id: string, key: string): Promise<void> {
    await this.write(`providers/${id}/key`, 'PUT', { key });
  }

  /** Asks the provider for its models; the server replaces the provider's model list with them. */
  discoverModels(id: string): Promise<Provider> {
    return this.write(`providers/${id}/test`, 'POST');
  }

  async deleteProvider(id: string): Promise<void> {
    await this.write(`providers/${id}`, 'DELETE');
  }

  /** Deletes a section. Its chats return to the main list, or are deleted too when `chats` is set. */
  async deleteSection(id: string, chats: boolean): Promise<void> {
    await this.write(`sections/${id}`, 'DELETE', undefined, chats ? { chats: '1' } : undefined);
  }

  reorderSection(id: string, beforeID?: string, afterID?: string): Promise<void> {
    return this.write('sidebar/reorder', 'POST', { kind: 'sections', id, beforeID, afterID });
  }

  placeThread(
    id: string,
    sectionID?: string,
    pinned = false,
    before?: string,
    after?: string,
  ): Promise<Thread> {
    const body: Record<string, unknown> = { pinned };
    if (!pinned) body.sectionID = sectionID ?? null;
    if (before !== undefined) body.beforeID = before;
    if (after !== undefined) body.afterID = after;
    return this.write(`threads/${id}/placement`, 'PATCH', body);
  }

  async toolRuns(threadID: string, before?: number): Promise<Run[]> {
    const query: Record<string, string> = { limit: '100' };
    if (before !== undefined) query.before = String(before);
    return this.get(`threads/${threadID}/tool-runs`, query);
  }

  async events(threadID: string, after: number): Promise<Event[]> {
    return this.get(`threads/${threadID}/events`, { after: String(after), limit: '1000' });
  }

  async send(threadID: string, request: SendRequest): Promise<Message> {
    return this.write(`threads/${threadID}/messages`, 'POST', request);
  }

  async voiceTask(threadID: string, task: VoiceRequest): Promise<VoiceResponse> {
    const response = await this.fetch(`threads/${threadID}/voice`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(task),
      timeout: 600000,
    });
    await this.validate(response);
    return await decodeJSON<VoiceResponse>(response);
  }

  /** Talks to the chat's agents by voice: their reply streams as it is written, so speech starts at the first sentence. */
  async *voiceStream(threadID: string, task: VoiceRequest, signal?: AbortSignal): AsyncGenerator<VoiceFragment> {
    const response = await this.fetch(`threads/${threadID}/voice/stream`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
      body: JSON.stringify(task),
      timeout: 600000,
      signal,
    });
    await this.validate(response);
    for await (const event of decodeSSE<SpokenEvent>(response.body!, signal, 600000)) {
      if (event.message) throw new ResponseError(502, event.message);
      if (event.text === undefined || event.text === null) break;
      yield { text: event.text, agentID: event.agentID };
    }
  }

  async markRead(threadID: string, seq: number): Promise<void> {
    const request: ReadRequest = { seq };
    await this.write(`threads/${threadID}/read`, 'POST', request);
  }

  async stop(threadID: string, withdrawQueued = false): Promise<void> {
    await this.write(`threads/${threadID}/stop`, 'POST', { seq: 0 }, withdrawQueued ? { queued: '1' } : undefined);
  }

  /** Searches everything, or one chat when `scope` is its ID. */
  async search(query: string, kind: string, scope?: string): Promise<SearchResult[]> {
    const params: Record<string, string> = { q: query, type: kind };
    if (scope) params.thread = scope;
    return this.get('search', params);
  }

  searchInThread(threadID: string, query: string): Promise<SearchHit[]> {
    return this.get(`threads/${threadID}/search`, { q: query });
  }

  async upload(file: File, onProgress: (fraction: number) => void = () => {}): Promise<Attachment> {
    const body = new FormData();
    body.append('file', file, file.name.replace(/["\r\n]/g, ''));
    onProgress(0);
    const response = await this.fetch('attachments', { method: 'POST', body });
    await this.validate(response);
    const attachment = await decodeJSON<Attachment>(response);
    onProgress(1);
    return attachment;
  }

  async attachment(url: string, signal?: AbortSignal): Promise<Blob> {
    const target = new URL(url, this.baseURL);
    if (target.origin !== this.baseURL.origin) throw new InvalidURLError();
    const response = await this.fetch(target.href, { signal });
    await this.validate(response);
    return response.blob();
  }

  /** Tells the server this device's platform, timezone and allowed connections. */
  async reportClient(state: ClientState): Promise<void> {
    await this.write('client', 'PUT', state);
  }

  /** Tool calls the server asks this device to run. */
  async *clientRequests(signal?: AbortSignal): AsyncGenerator<ClientRequest> {
    const response = await this.fetch('client/requests', {
      headers: { Accept: 'text/event-stream' },
      timeout: 45000,
      signal,
    });
    await this.validate(response);
    yield* decodeSSE<ClientRequest>(response.body!, signal, 45000);
  }

  answerClient(id: string, result: ClientResult): Promise<void> {
    return this.write(`client/requests/${id}`, 'POST', result);
  }

  async *stream(threadID: string, after: number, signal?: AbortSignal): AsyncGenerator<Event> {
    const response = await this.fetch(`threads/${threadID}/stream`, {
      query: { after: String(after) },
      headers: { Accept: 'text/event-stream' },
      timeout: 45000,
      signal,
    });
    await this.validate(response);
    yield* decodeSSE<Event>(response.body!, signal, 45000);
  }

  speechSocket(): URL {
    const url = new URL('speech/live', this.baseURL);
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
    return url;
  }

  private async get<T>(path: string, query?: Record<string, string>, retrying = true): Promise<T> {
    for (let attempt = 0; ; attempt++) {
      try {
        const response = await this.fetch(path, { query });
        await this.validate(response);
        return await decodeJSON<T>(response);
      } catch (error) {
        if (!retrying || signalAborted(error) || !isTransient(error) || attempt >= 2) throw error;
        if (isOffline(error)) await this.recover();
        await sleep(250 * (attempt + 1));
      }
    }
  }

  private async write<T>(path: string, method: string, body?: unknown, query?: Record<string, string>): Promise<T> {
    const response = await this.fetch(path, {
      method,
      query,
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
    await this.validate(response);
    if (response.status === 204 || response.status === 205) return undefined as T;
    return await decodeJSON<T>(response);
  }

  private async fetch(
    path: string,
    init: RequestInit & { query?: Record<string, string>; timeout?: number } = {},
  ): Promise<Response> {
    const url = await this.request(path, init.query);
    const controller = new AbortController();
    const timer = init.timeout ? setTimeout(() => controller.abort(), init.timeout) : undefined;
    const signal = init.signal ? AbortSignal.any([init.signal, controller.signal]) : controller.signal;
    try {
      const token = await this.credential.value();
      const headers = new Headers(init.headers);
      headers.set('Accept', headers.get('Accept') ?? 'application/json');
      headers.set('X-Polymux-Client', 'polymux-kit');
      if (token) headers.set('Authorization', `Bearer ${token}`);
      return await request(url, { ...init, headers, signal, redirect: 'error' });
    } catch (error) {
      if (error instanceof TypeError) throw new ResponseError(0, "Couldn't reach the server.");
      throw error;
    } finally {
      if (timer) clearTimeout(timer);
    }
  }

  private async request(path: string, query?: Record<string, string>): Promise<URL> {
    const base = await this.endpoint();
    const url = new URL(path, base);
    if (query) for (const [name, value] of Object.entries(query)) url.searchParams.set(name, value);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new InvalidURLError();
    return url;
  }

  private async validate(response: Response): Promise<void> {
    if (response.ok) return;
    let message = response.statusText || `HTTP ${response.status}`;
    try {
      const body = await decodeJSON<WireError>(response.clone());
      if (body?.message) message = body.message;
    } catch {
      // keep the status text
    }
    throw new ResponseError(response.status, message);
  }
}

function signalAborted(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}
