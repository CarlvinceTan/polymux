import type { APIClient } from '../api/client';

type Entry = { bytes: ArrayBuffer; type: string; used?: number };
type Metadata = { size: number; used: number };
type Download = { controller: AbortController; users: number; task: Promise<Blob> };
const limit = 200 * 1024 * 1024;
const pending = new Map<string, Download>();
let database: Promise<IDBDatabase> | undefined;

function open(): Promise<IDBDatabase> {
  return database ??= new Promise((resolve, reject) => {
    const request = indexedDB.open('polymux-media', 2);
    request.onupgradeneeded = () => {
      const db = request.result;
      const transaction = request.transaction!;
      if (!db.objectStoreNames.contains('files')) db.createObjectStore('files');
      const metadata = db.createObjectStore('metadata');
      const cursor = transaction.objectStore('files').openCursor();
      cursor.onsuccess = () => {
        const item = cursor.result;
        if (!item) return;
        const entry = item.value as Entry;
        metadata.put({ size: entry.bytes.byteLength, used: entry.used ?? 0 } satisfies Metadata, item.key);
        item.continue();
      };
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
    request.onblocked = () => reject(new Error('Cache unavailable'));
  });
}

async function read(key: string): Promise<Blob | undefined> {
  try {
    const db = await open();
    return await new Promise((resolve, reject) => {
      const transaction = db.transaction(['files', 'metadata'], 'readwrite');
      const request = transaction.objectStore('files').get(key);
      request.onsuccess = () => {
        const entry = request.result as Entry | undefined;
        if (entry) transaction.objectStore('metadata').put({ size: entry.bytes.byteLength, used: Date.now() } satisfies Metadata, key);
        resolve(entry ? new Blob([entry.bytes], { type: entry.type }) : undefined);
      };
      request.onerror = () => reject(request.error);
    });
  } catch { return undefined; }
}

async function write(key: string, blob: Blob): Promise<void> {
  try {
    const bytes = await blob.arrayBuffer();
    const db = await open();
    await new Promise<void>((resolve, reject) => {
      const transaction = db.transaction(['files', 'metadata'], 'readwrite');
      const files = transaction.objectStore('files');
      const metadata = transaction.objectStore('metadata');
      const entries: { key: IDBValidKey; size: number; used: number }[] = [];
      const cursor = metadata.openCursor();
      cursor.onsuccess = () => {
        const item = cursor.result;
        if (item) {
          if (item.key !== key) entries.push({ key: item.key, ...item.value as Metadata });
          item.continue();
        } else {
          let size = entries.reduce((sum, entry) => sum + entry.size, blob.size);
          for (const entry of entries.sort((a, b) => a.used - b.used)) {
            if (size <= limit) break;
            files.delete(entry.key);
            metadata.delete(entry.key);
            size -= entry.size;
          }
          files.put({ bytes, type: blob.type } satisfies Entry, key);
          metadata.put({ size: blob.size, used: Date.now() } satisfies Metadata, key);
        }
      };
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(transaction.error);
      transaction.onabort = () => reject(transaction.error);
    });
  } catch { /* The attachment remains usable without a cache. */ }
}

export function mediaFile(address: string, url: string, client: APIClient, signal?: AbortSignal): Promise<Blob> {
  if (signal?.aborted) return Promise.reject(new DOMException('Aborted', 'AbortError'));
  const key = JSON.stringify([address, url]);
  let download = pending.get(key);
  if (!download) {
    const controller = new AbortController();
    const entry: Download = { controller, users: 0, task: Promise.resolve(new Blob()) };
    entry.task = (async () => {
      const cached = await read(key);
      if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError');
      if (cached) return cached;
      const blob = await client.attachment(url, controller.signal);
      if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError');
      await write(key, blob);
      return blob;
    })().finally(() => { if (pending.get(key) === entry) pending.delete(key); });
    pending.set(key, entry);
    download = entry;
  }
  const entry = download;
  entry.users++;
  return new Promise((resolve, reject) => {
    let finished = false;
    const release = () => {
      if (finished) return false;
      finished = true;
      signal?.removeEventListener('abort', abort);
      if (--entry.users === 0 && pending.get(key) === entry) {
        pending.delete(key);
        entry.controller.abort();
      }
      return true;
    };
    const abort = () => { if (release()) reject(new DOMException('Aborted', 'AbortError')); };
    signal?.addEventListener('abort', abort, { once: true });
    entry.task.then((blob) => { if (release()) resolve(blob); }, (error) => { if (release()) reject(error); });
  });
}
