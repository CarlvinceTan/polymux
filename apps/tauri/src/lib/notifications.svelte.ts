import type { Notice, NoticePage } from './api/contract';
import type { APIClient } from './api/client';
import { isTauri } from './api/bridge';
import { shouldNotify } from './notificationPolicy';

export interface NotificationPayload {
  title: string;
  body: string;
  threadID: string;
  messageID: string;
  address: string;
  seq: number;
}

export interface Notifier {
  request(): Promise<NotificationPermission>;
  send(payload: NotificationPayload): void;
  onAction?(select: (threadID: string) => void): Promise<() => void> | (() => void);
}

const enabledKey = 'polymux.notifications.enabled';
const mutedKey = (address: string) => `polymux.notifications.muted.${address}`;
const cursorKey = (address: string) => `polymux.notifications.cursor.${address}`;

const browserNotifier: Notifier = {
  async request() {
    if (typeof Notification === 'undefined') return 'denied';
    if (Notification.permission !== 'default') return Notification.permission;
    return await Notification.requestPermission();
  },
  send(payload) {
    if (typeof Notification === 'undefined' || Notification.permission !== 'granted') return;
    const notification = new Notification(payload.title, { body: payload.body, data: payload });
    notification.onclick = () => { window.focus(); notification.close(); };
  },
};

let native: Promise<Notifier> | undefined;
function notifier(): Notifier | Promise<Notifier> {
  if (!isTauri) return browserNotifier;
  return native ??= import('@tauri-apps/plugin-notification').then((plugin) => {
    const threads = new Map<number, string>();
    return {
      async request() { return await plugin.requestPermission(); },
      send(payload) {
        const id = Math.max(1, payload.seq % 2_147_483_647);
        threads.set(id, payload.threadID);
        plugin.sendNotification({
          id, title: payload.title, body: payload.body, sound: 'default',
          extra: { address: payload.address, threadID: payload.threadID, messageID: payload.messageID, seq: payload.seq },
        });
      },
      async onAction(select) {
        const listener = await plugin.onAction((event) => {
          const id = event.notification?.id;
          const thread = id === undefined ? undefined : threads.get(id);
          if (thread) select(thread);
        });
        return () => { void listener.unregister(); };
      },
    } satisfies Notifier;
  });
}

export class NotificationStore {
  enabled = $state(false);
  muted = $state<Set<string>>(new Set());
  issue = $state('');
  destination = $state<string>();
  routeID = $state(0);
  visibleThread = $state<string>();
  private cursor?: number;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private busy = false;
  private disposed = false;
  private stopAction?: () => void;

  constructor(private readonly address: string, private readonly client: APIClient) {
    try { this.enabled = localStorage.getItem(enabledKey) === '1'; } catch { /* Keep notifications off. */ }
    try {
      const saved = JSON.parse(localStorage.getItem(mutedKey(address)) ?? '[]') as unknown;
      if (Array.isArray(saved)) this.muted = new Set(saved.filter((value): value is string => typeof value === 'string'));
    } catch { /* Keep the mute list empty. */ }
    try {
      const raw = localStorage.getItem(cursorKey(address));
      const value = raw === null ? NaN : Number(raw);
      if (Number.isFinite(value) && value >= 0) this.cursor = value;
    } catch { /* Start from the server's current cursor. */ }
  }

  async start(): Promise<void> {
    this.disposed = false;
    const current = await notifier();
    if (current.onAction) {
      this.stopAction = await current.onAction((threadID) => {
        this.destination = threadID;
        this.routeID++;
      });
    }
    if (this.enabled) void this.poll();
  }

  dispose(): void {
    this.disposed = true;
    clearTimeout(this.timer);
    this.timer = undefined;
    this.stopAction?.();
    this.stopAction = undefined;
  }

  async setEnabled(enabled: boolean): Promise<boolean> {
    if (enabled) {
      const current = await notifier();
      const permission = await current.request();
      if (permission !== 'granted') {
        this.enabled = false;
        this.issue = 'Allow Polymux notifications in System Settings.';
        return false;
      }
    }
    this.enabled = enabled;
    this.issue = '';
    try { localStorage.setItem(enabledKey, enabled ? '1' : '0'); } catch { /* Keep this session's choice. */ }
    if (enabled) void this.poll();
    else { clearTimeout(this.timer); this.timer = undefined; }
    return true;
  }

  setMuted(agentID: string, muted: boolean): void {
    if (muted) this.muted.add(agentID);
    else this.muted.delete(agentID);
    this.muted = new Set(this.muted);
    try { localStorage.setItem(mutedKey(this.address), JSON.stringify([...this.muted])); } catch { /* Keep this session's mute list. */ }
  }

  isMuted(agentID: string): boolean {
    return this.muted.has(agentID);
  }

  private async poll(): Promise<void> {
    if (this.disposed || !this.enabled || this.busy) return;
    this.busy = true;
    let cursor = this.cursor;
    try {
      const page: NoticePage = await this.client.notices(cursor);
      if (this.disposed) return;
      const initial = cursor === undefined;
      cursor = page.cursor;
      if (!initial) {
        const current = await notifier();
        for (const notice of page.notices) {
          if (!this.deliver(notice)) continue;
          current.send({
            title: notice.title,
            body: notice.body || 'New message in Polymux',
            threadID: notice.conversationID,
            messageID: notice.messageID,
            address: this.address,
            seq: notice.seq,
          });
        }
      }
      this.cursor = cursor;
      try { localStorage.setItem(cursorKey(this.address), String(cursor)); } catch { /* The next poll may repeat a notice. */ }
      this.issue = '';
    } catch {
      // A disconnected server should not disable alerts for the next connection.
    } finally {
      this.busy = false;
      if (!this.disposed && this.enabled) {
        clearTimeout(this.timer);
        this.timer = setTimeout(() => { void this.poll(); }, document.hidden ? 10_000 : 3_000);
      }
    }
  }

  private deliver(notice: Notice): boolean {
    if (this.disposed || !this.enabled) return false;
    return shouldNotify(notice, { muted: this.muted, visibleThread: this.visibleThread });
  }
}
