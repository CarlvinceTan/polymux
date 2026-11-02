import { Channel, invoke } from '@tauri-apps/api/core';

type Phase = 'idle' | 'preparing' | 'recording' | 'stopping';
export type SpeechEvent = { id: number; kind: 'ready' | 'transcript' | 'level' | 'error' | 'ended';
  text?: string; start?: number; final?: boolean; level?: number; denied?: boolean };
export type SpeechBridge = {
  start: (handler: (event: SpeechEvent) => void) => Promise<number>;
  stop: (id: number, finalize: boolean) => Promise<void>;
};

export const nativeSpeech: SpeechBridge = {
  start: (handler) => invoke<number>('start_dictation', { onEvent: new Channel<SpeechEvent>(handler) }),
  stop: (id, finalize) => invoke('stop_dictation', { id, finalize }),
};

type Session = { id?: number; events: SpeechEvent[]; stopping?: boolean; discard?: boolean };

export function createDictation(bridge: SpeechBridge, callbacks: {
  state: (phase: Phase, level: number, issue?: string) => void;
  transcript: (text: string, start: number, final: boolean) => void;
}) {
  let session: Session | undefined;
  let phase: Phase = 'idle';
  let level = 0;
  let issue = '';
  const notify = () => callbacks.state(phase, level, issue);
  const close = (active: Session) => {
    if (session !== active) return;
    session = undefined;
    phase = 'idle';
    level = 0;
    notify();
  };
  const receive = (active: Session, event: SpeechEvent) => {
    if (session !== active) return;
    if (active.id === undefined) { active.events.push(event); return; }
    if (event.id !== active.id) return;
    switch (event.kind) {
      case 'ready': if (!active.stopping) { phase = 'recording'; notify(); } break;
      case 'level': if (!active.stopping) { level = event.level ?? 0; notify(); } break;
      case 'transcript': if (!active.discard) callbacks.transcript(event.text ?? '', event.start ?? 0, !!event.final); break;
      case 'error':
        if (!active.discard) issue = event.denied ? 'Allow microphone and speech recognition in Settings.' : 'Dictation isn’t available right now.';
        notify();
        break;
      case 'ended': close(active); break;
    }
  };
  return {
    async start() {
      if (session) return;
      const active: Session = { events: [] };
      session = active;
      phase = 'preparing';
      issue = '';
      notify();
      try {
        active.id = await bridge.start((event) => receive(active, event));
        for (const event of active.events) receive(active, event);
        active.events = [];
        if (active.stopping && session === active) await bridge.stop(active.id, !active.discard);
      } catch {
        if (session !== active) return;
        if (!active.discard) issue = 'Dictation isn’t available right now.';
        close(active);
      }
    },
    stop(finalize: boolean) {
      const active = session;
      if (!active || (active.stopping && (finalize || active.discard))) return;
      if (phase === 'preparing') finalize = false;
      active.stopping = true;
      active.discard = !finalize;
      phase = 'stopping';
      level = 0;
      notify();
      if (active.id !== undefined) void bridge.stop(active.id, finalize).catch(() => close(active));
    },
  };
}
