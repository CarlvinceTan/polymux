import type { Attachment, Message, ReplyContext } from './api/contract';
import type { ServerIdentity } from './cache';

export interface StoredFile {
  id: string;
  bytes: ArrayBuffer;
  name: string;
  type: string;
  modified: number;
  uploaded?: Attachment;
}

export interface Outgoing {
  address: string;
  id: string;
  threadID: string;
  text: string;
  attachments: Attachment[];
  reply?: ReplyContext;
  message: Message;
  files: StoredFile[];
  permanent?: boolean;
  accepted?: Message;
  identity?: ServerIdentity;
}

export class Outbox {
  private database?: Promise<IDBDatabase>;
  private writes: Promise<unknown> = Promise.resolve();
  private readonly owner = crypto.randomUUID();

  constructor(private readonly address: string) {}

  private open(): Promise<IDBDatabase> {
    return this.database ??= new Promise((resolve, reject) => {
      const request = indexedDB.open('polymux-outbox', 1);
      request.onupgradeneeded = () => {
        for (const name of ['entries', 'drafts', 'locks']) request.result.createObjectStore(name);
      };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
      request.onblocked = () => reject(new Error('Outbox unavailable'));
    });
  }

  private write(action: (db: IDBDatabase) => Promise<void>): Promise<void> {
    const next = this.writes.catch(() => undefined).then(async () => action(await this.open()));
    this.writes = next;
    return next;
  }

  private transaction(db: IDBDatabase, stores: string[], action: (tx: IDBTransaction) => void): Promise<void> {
    return new Promise((resolve, reject) => {
      const tx = db.transaction(stores, 'readwrite');
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
      try { action(tx); } catch (error) { tx.abort(); reject(error); }
    });
  }

  async entries(): Promise<Outgoing[]> {
    await this.writes.catch(() => undefined);
    const db = await this.open();
    return new Promise((resolve, reject) => {
      const request = db.transaction('entries').objectStore('entries').getAll();
      request.onsuccess = () => resolve((request.result as Outgoing[]).filter((entry) => entry.address === this.address).sort((a, b) => a.message.createdAt.localeCompare(b.message.createdAt)));
      request.onerror = () => reject(request.error);
    });
  }

  async ifMissing(id: string, action: () => void): Promise<void> {
    await this.writes.catch(() => undefined);
    const db = await this.open();
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction('entries');
      const request = tx.objectStore('entries').getKey(JSON.stringify([this.address, id]));
      request.onsuccess = () => { if (request.result === undefined) action(); };
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  }

  async drafts(threadID: string): Promise<StoredFile[]> {
    await this.writes.catch(() => undefined);
    const db = await this.open();
    return new Promise((resolve, reject) => {
      const request = db.transaction('drafts').objectStore('drafts').get(JSON.stringify([this.address, threadID]));
      request.onsuccess = () => resolve((request.result as StoredFile[] | undefined) ?? []);
      request.onerror = () => reject(request.error);
    });
  }

  saveDraft(threadID: string, files: StoredFile[]): Promise<void> {
    return this.write((db) => this.transaction(db, ['drafts'], (tx) => {
      tx.objectStore('drafts').put(files, JSON.stringify([this.address, threadID]));
    }));
  }

  enqueue(entry: Outgoing, remaining: StoredFile[]): Promise<void> {
    return this.write((db) => this.transaction(db, ['entries', 'drafts'], (tx) => {
      tx.objectStore('entries').put(entry, JSON.stringify([this.address, entry.id]));
      tx.objectStore('drafts').put(remaining, JSON.stringify([this.address, entry.threadID]));
    }));
  }

  save(entry: Outgoing): Promise<void> {
    return this.write((db) => this.transaction(db, ['entries'], (tx) => {
      tx.objectStore('entries').put(entry, JSON.stringify([this.address, entry.id]));
    }));
  }

  remove(id: string): Promise<void> {
    return this.write((db) => this.transaction(db, ['entries'], (tx) => {
      tx.objectStore('entries').delete(JSON.stringify([this.address, id]));
    }));
  }

  async discard(threadID: string, id: string): Promise<'removed' | 'accepted' | 'missing' | 'busy'> {
    const db = await this.open();
    return new Promise((resolve, reject) => {
      const tx = db.transaction(['entries', 'locks'], 'readwrite');
      const locks = tx.objectStore('locks');
      const entries = tx.objectStore('entries');
      const lock = locks.get(JSON.stringify([this.address, threadID]));
      let result: 'removed' | 'accepted' | 'missing' | 'busy' = 'busy';
      lock.onsuccess = () => {
        if (lock.result?.owner !== this.owner || lock.result.expires <= Date.now()) return;
        const key = JSON.stringify([this.address, id]);
        const request = entries.get(key);
        request.onsuccess = () => {
          const entry = request.result as Outgoing | undefined;
          if (!entry) result = 'missing';
          else if (entry.accepted) result = 'accepted';
          else { entries.delete(key); result = 'removed'; }
        };
      };
      tx.oncomplete = () => resolve(result);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  }

  async claim(threadID: string): Promise<boolean> {
    const db = await this.open();
    return new Promise((resolve, reject) => {
      const tx = db.transaction('locks', 'readwrite');
      const store = tx.objectStore('locks');
      const key = JSON.stringify([this.address, threadID]);
      const request = store.get(key);
      let claimed = false;
      request.onsuccess = () => {
        const lock = request.result as { owner: string; expires: number } | undefined;
        if (!lock || lock.owner === this.owner || lock.expires < Date.now()) {
          store.put({ owner: this.owner, expires: Date.now() + 30000 }, key);
          claimed = true;
        }
      };
      tx.oncomplete = () => resolve(claimed);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  }

  async release(threadID: string): Promise<void> {
    const db = await this.open();
    await this.transaction(db, ['locks'], (tx) => {
      const store = tx.objectStore('locks');
      const key = JSON.stringify([this.address, threadID]);
      const request = store.get(key);
      request.onsuccess = () => { if (request.result?.owner === this.owner) store.delete(key); };
    });
  }
}
