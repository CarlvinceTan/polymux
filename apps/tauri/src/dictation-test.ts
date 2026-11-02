import { mockIPC } from '@tauri-apps/api/mocks';
import type { Channel } from '@tauri-apps/api/core';

export function mockSpeech() {
  const calls: unknown[] = [];
  let channel: Channel<unknown> | undefined;
  mockIPC((command, payload) => {
    if (command === 'start_dictation') { channel = (payload as { onEvent: Channel<unknown> }).onEvent; return 1; }
    if (command === 'stop_dictation') calls.push(payload);
    return null;
  });
  return { calls, emit: (payload: unknown) => channel?.onmessage(payload) };
}
