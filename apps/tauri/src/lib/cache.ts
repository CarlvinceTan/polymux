import type { Agent, Message, Routine, Run, Section, Thread } from './api/contract';

export interface SidebarCache {
  agents: Agent[];
  threads: Thread[];
  sections: Section[];
}

export interface ServerIdentity { host: string; current: string }
export interface ChatCache { messages: Message[]; cursor: number; runs: Run[] }

export class Cache {
  private database?: Promise<IDBDatabase>;
  constructor(private readonly address: string) {}

  private open(): Promise<IDBDatabase> {
    return this.database ??= new Promise((resolve, reject) => {
      const request = indexedDB.open('polymux-cache', 1);
      request.onupgradeneeded = () => request.result.createObjectStore('snapshots');
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
      request.onblocked = () => reject(new Error('Cache unavailable'));
    });
  }

  private async read<T>(key: string): Promise<T | undefined> {
    try {
      const db = await this.open();
      return await new Promise((resolve, reject) => {
        const request = db.transaction('snapshots').objectStore('snapshots').get(JSON.stringify([this.address, key]));
        request.onsuccess = () => resolve(request.result as T | undefined);
        request.onerror = () => reject(request.error);
      });
    } catch { return undefined; }
  }

  private async write(key: string, value: unknown): Promise<boolean> {
    try {
      const db = await this.open();
      await new Promise<void>((resolve, reject) => {
        const transaction = db.transaction('snapshots', 'readwrite');
        transaction.objectStore('snapshots').put(value, JSON.stringify([this.address, key]));
        transaction.oncomplete = () => resolve();
        transaction.onerror = () => reject(transaction.error);
        transaction.onabort = () => reject(transaction.error);
      });
      return true;
    } catch { return false; }
  }

  sidebar(): Promise<SidebarCache | undefined> { return this.read('sidebar'); }
  async saveSidebar(value: SidebarCache): Promise<void> { await this.write('sidebar', value); }
  messages(id: string): Promise<Message[] | undefined> { return this.read(`chat/${id}`); }
  async chat(id: string): Promise<ChatCache | undefined> {
    try {
      const db = await this.open();
      return await new Promise((resolve, reject) => {
        const transaction = db.transaction('snapshots');
        const store = transaction.objectStore('snapshots');
        const messages = store.get(JSON.stringify([this.address, `chat/${id}`]));
        const state = store.get(JSON.stringify([this.address, `chat-state/${id}`]));
        transaction.oncomplete = () => resolve(Array.isArray(messages.result)
          ? { messages: messages.result, cursor: state.result?.cursor ?? 0, runs: state.result?.runs ?? [] } : undefined);
        transaction.onerror = () => reject(transaction.error);
        transaction.onabort = () => reject(transaction.error);
      });
    } catch { return undefined; }
  }
  async saveChat(id: string, value: ChatCache): Promise<boolean> {
    try {
      const db = await this.open();
      return await new Promise((resolve, reject) => {
        const transaction = db.transaction('snapshots', 'readwrite');
        const store = transaction.objectStore('snapshots');
        const stateKey = JSON.stringify([this.address, `chat-state/${id}`]);
        const current = store.get(stateKey);
        let saved = false;
        current.onsuccess = () => {
          if ((current.result?.cursor ?? 0) > value.cursor) return;
          try {
            store.put(value.messages, JSON.stringify([this.address, `chat/${id}`]));
            store.put({ cursor: value.cursor, runs: value.runs }, stateKey);
            saved = true;
          } catch { transaction.abort(); }
        };
        transaction.oncomplete = () => resolve(saved);
        transaction.onerror = () => reject(transaction.error);
        transaction.onabort = () => reject(transaction.error);
      });
    } catch { return false; }
  }
  routines(id: string): Promise<Routine[] | undefined> { return this.read(`routines/${id}`); }
  async saveRoutines(id: string, value: Routine[]): Promise<void> { await this.write(`routines/${id}`, value); }
  identity(): Promise<ServerIdentity | undefined> { return this.read('identity'); }
  async saveIdentity(value: ServerIdentity): Promise<void> { await this.write('identity', value); }
}
