import { isTauri } from '../api/bridge';

const chunkSize = 1024 * 1024;

export type NativeMediaAction = 'quicklook' | 'open' | 'share';

export async function openNativeMedia(blob: Blob, name: string, action: NativeMediaAction, signal?: AbortSignal): Promise<void> {
  if (!isTauri) throw new Error('Native media is unavailable');
  if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
  const { invoke } = await import('@tauri-apps/api/core');
  const id = await invoke<number>('start_preview', { name });
  let finished = false;
  try {
    for (let offset = 0; offset < blob.size; offset += chunkSize) {
      if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
      const chunk = await blob.slice(offset, offset + chunkSize).arrayBuffer();
      if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
      await invoke('preview_chunk', chunk, { headers: { 'x-resource-id': String(id) } });
    }
    if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
    await invoke('finish_preview', { id, action });
    finished = true;
  } finally {
    if (!finished) await invoke('finish_preview', { id, action: 'cancel' }).catch(() => undefined);
  }
}
