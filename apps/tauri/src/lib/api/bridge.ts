/** Thin Tauri bridge so the renderer also runs in a plain browser during development. */
import type { InvokeArgs } from '@tauri-apps/api/core';

export const isTauri =
  typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;

export async function invoke<T>(command: string, args?: InvokeArgs): Promise<T> {
  const { invoke } = await import('@tauri-apps/api/core');
  return invoke<T>(command, args);
}
