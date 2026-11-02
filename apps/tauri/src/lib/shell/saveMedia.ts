import { isTauri } from '../api/bridge';

const chunkSize = 1024 * 1024;

export async function saveMedia(blob: Blob, name: string, signal?: AbortSignal): Promise<boolean> {
  if (!isTauri) throw new Error('Native save unavailable');
  if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
  const { invoke } = await import('@tauri-apps/api/core');
  const id = await invoke<number | null>('start_save', { name });
  if (id === null) return false;
  let committed = false;
  try {
    for (let offset = 0; offset < blob.size; offset += chunkSize) {
      if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
      const chunk = await blob.slice(offset, offset + chunkSize).arrayBuffer();
      if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
      await invoke('save_chunk', chunk, { headers: { 'x-resource-id': String(id) } });
    }
    if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
    await invoke('finish_save', { id, commit: true });
    committed = true;
    return true;
  } finally {
    if (!committed) await invoke('finish_save', { id, commit: false }).catch(() => undefined);
  }
}
