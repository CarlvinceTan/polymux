import type { Thread } from '../api/contract';
import type { Session } from '../session.svelte';

type Edit = { title: string; agentIDs: string[] };
type Listener = (edit: Edit, issue: string) => void;
const edits = new Map<string, GroupEdit>();
const editKey = (address: string, id: string) => `polymux.group.${JSON.stringify([address, id])}`;

export function groupEdit(session: Session, thread: Thread): GroupEdit {
  const key = editKey(session.address, thread.id);
  let edit = edits.get(key);
  if (!edit) { edit = new GroupEdit(key, session, thread); edits.set(key, edit); }
  else edit.use(session, thread);
  return edit;
}

export function restoreGroups(session: Session): void {
  for (const thread of session.threads) {
    const key = editKey(session.address, thread.id);
    try { if (edits.has(key) || localStorage.getItem(key)) groupEdit(session, thread); } catch { /* Use live data. */ }
  }
}

export function overlayGroups(session: Session, threads: Thread[]): Thread[] {
  return threads.map((thread) => edits.get(editKey(session.address, thread.id))?.overlay(session, thread) ?? thread);
}

export function stopGroups(session: Session): void {
  for (const edit of edits.values()) edit.stop(session);
}

export class GroupEdit {
  private value: Edit;
  private dirty = false;
  private saving = false;
  private issue = '';
  private timer?: ReturnType<typeof setTimeout>;
  private listeners = new Set<Listener>();

  private session?: Session;
  private thread: Thread;

  constructor(private key: string, session: Session, thread: Thread) {
    this.session = session;
    this.thread = thread;
    this.value = { title: thread.title, agentIDs: [...thread.agentIDs] };
    try {
      const saved = JSON.parse(localStorage.getItem(key) ?? 'null') as Edit | null;
      if (saved && typeof saved.title === 'string' && Array.isArray(saved.agentIDs) && saved.agentIDs.every((id) => typeof id === 'string')) {
        this.value = saved;
        this.dirty = true;
        this.merge(saved);
        this.schedule(0);
      }
    } catch { /* Continue without a cached edit. */ }
  }

  use(session: Session, thread: Thread): void {
    this.session = session;
    this.thread = thread;
    if (!this.dirty) this.value = { title: thread.title, agentIDs: [...thread.agentIDs] };
    else { this.merge(this.value); if (!this.saving) this.schedule(0); }
  }

  overlay(session: Session, thread: Thread): Thread {
    return this.session === session && this.dirty ? { ...thread, ...this.value } : thread;
  }

  stop(session: Session): void {
    if (this.session !== session) return;
    this.session = undefined;
    clearTimeout(this.timer);
    this.timer = undefined;
    this.listeners.clear();
    if (!this.saving) edits.delete(this.key);
  }

  connect(listener: Listener): () => void {
    this.listeners.add(listener);
    listener(this.value, this.issue);
    if (this.dirty && !this.saving) this.schedule(0);
    return () => { this.listeners.delete(listener); if (this.dirty && !this.saving) this.schedule(0); };
  }

  change(value: Edit, delay: number): void {
    this.value = { title: value.title, agentIDs: [...value.agentIDs] };
    this.dirty = true;
    this.issue = '';
    try { localStorage.setItem(this.key, JSON.stringify(this.value)); } catch { this.issue = 'Changes couldn’t be saved on this device.'; }
    this.merge(this.value);
    this.notify();
    this.schedule(delay);
  }

  private merge(edit: Edit): void {
    const session = this.session;
    if (!session) return;
    const index = session.threads.findIndex((thread) => thread.id === this.thread.id);
    if (index >= 0) session.threads[index] = { ...session.threads[index], ...edit };
  }

  private schedule(delay: number): void {
    clearTimeout(this.timer);
    if (!this.session) { this.timer = undefined; return; }
    this.timer = setTimeout(() => { this.timer = undefined; void this.save(); }, delay);
  }

  private async save(): Promise<void> {
    const session = this.session;
    if (!session || !this.dirty || this.saving) return;
    this.saving = true;
    const sent = { title: this.value.title, agentIDs: [...this.value.agentIDs] };
    try {
      const accepted = await session.client.updateThread(this.thread.id, sent.title, sent.agentIDs);
      if (this.session !== session) return;
      if (this.value.title === sent.title && JSON.stringify(this.value.agentIDs) === JSON.stringify(sent.agentIDs)) {
        this.value = { title: accepted.title, agentIDs: [...accepted.agentIDs] };
        this.dirty = false;
        this.issue = '';
        this.merge(this.value);
        try { localStorage.removeItem(this.key); } catch { /* Saved remotely. */ }
        this.notify();
      }
    } catch {
      if (this.session !== session) return;
      this.issue = 'Changes haven’t synced yet.';
      this.merge(this.value);
      this.notify();
      this.schedule(5000);
    } finally {
      this.saving = false;
      if (!this.session) edits.delete(this.key);
      else if (this.dirty && !this.timer) this.schedule(0);
    }
  }

  private notify(): void { for (const listener of this.listeners) listener(this.value, this.issue); }
}
