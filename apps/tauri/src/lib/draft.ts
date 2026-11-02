import { DraftConflictError } from './api/errors';
import type { ChatDraftMention, DraftState } from './api/models';
import type { ReplyContext } from './api/contract';
import { updateMentions } from './shell/mentions';

interface Saved {
  state: DraftState;
  dirty: boolean;
  blocked: boolean;
  pending?: DraftState;
}

export class Draft {
  private saved: Saved;
  private timer?: ReturnType<typeof setTimeout>;
  private watcher?: ReturnType<typeof setInterval>;
  private saving = false;
  private lastEdit = 0;

  constructor(
    private readonly key: string,
    private readonly client: { draft(id: string): Promise<DraftState>; saveDraft(id: string, state: DraftState): Promise<DraftState> },
    private readonly id: string,
    private readonly changed: (state: DraftState, issue: string) => void,
  ) {
    this.saved = { state: { revision: 0, writeID: '', value: { text: '', mentions: [] } }, dirty: false, blocked: false };
    try {
      const cached = localStorage.getItem(key);
      if (cached) this.saved = JSON.parse(cached) as Saved;
    } catch { /* Storage may be unavailable. */ }
    this.publish();
  }

  start() {
    if (this.watcher) return;
    void this.refresh();
    if (this.saved.dirty && !this.saved.blocked) this.schedule(0);
    this.watcher = setInterval(() => {
      void this.refresh();
      if (!this.timer) void this.flush();
    }, 1000);
  }

  stop() {
    clearInterval(this.watcher);
    this.watcher = undefined;
    clearTimeout(this.timer);
    this.timer = undefined;
    void this.flush();
  }

  edit(text: string, mentions?: ChatDraftMention[]) {
    const old = this.saved.state.value;
    const next = mentions ?? updateMentions(old.mentions ?? [], old.text, text);
    if (old.text === text && JSON.stringify(old.mentions ?? []) === JSON.stringify(next)) return;
    this.update({ ...old, text, mentions: next }, text ? 400 : 0);
  }

  reply(reply?: ReplyContext) {
    if (JSON.stringify(this.saved.state.value.reply) === JSON.stringify(reply)) return;
    this.update({ ...this.saved.state.value, reply }, 400);
  }

  private update(value: DraftState['value'], delay: number) {
    this.saved.state = {
      ...this.saved.state,
      writeID: crypto.randomUUID(),
      value,
    };
    this.saved.dirty = true;
    this.saved.blocked = false;
    this.lastEdit = Date.now();
    this.publish();
    this.schedule(delay);
  }

  async refresh() {
    if (this.saved.dirty) return;
    try {
      const fresh = await this.client.draft(this.id);
      if (this.saved.dirty || fresh.revision < this.saved.state.revision || Date.now() - this.lastEdit < 2000) return;
      this.saved.state = fresh;
      this.publish();
    } catch { /* Keep the local draft while offline. */ }
  }

  async flush() {
    if (!this.saved.dirty || this.saved.blocked || this.saving) return;
    this.saving = true;
    const outgoing = this.saved.pending ?? structuredClone(this.saved.state);
    this.saved.pending = outgoing;
    this.publish();
    try {
      const accepted = await this.client.saveDraft(this.id, outgoing);
      this.saved.state.revision = accepted.revision;
      this.saved.pending = undefined;
      if (this.saved.state.writeID === outgoing.writeID) this.saved.dirty = false;
      this.publish();
      if (this.saved.dirty && !this.timer) this.schedule(400);
    } catch (error) {
      if (error instanceof DraftConflictError) {
        this.saved.state.revision = error.current.revision;
        this.saved.pending = undefined;
        this.saved.blocked = true;
        this.publish();
      }
    } finally {
      this.saving = false;
    }
  }

  private schedule(delay: number) {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      this.timer = undefined;
      void this.flush();
    }, delay);
  }

  private publish() {
    try { localStorage.setItem(this.key, JSON.stringify(this.saved)); } catch { /* Keep the draft in memory. */ }
    this.changed(this.saved.state, this.saved.blocked ? 'Draft changed on another device. Your text is kept here.' : '');
  }
}
