import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createDictation, type SpeechBridge, type SpeechEvent } from './dictation';

function fixture() {
  let handler = (_: SpeechEvent) => {};
  const calls: [number, boolean][] = [];
  const states: string[] = [];
  const words: string[] = [];
  const bridge: SpeechBridge = {
    start: async (next) => { handler = next; return 7; },
    stop: async (id, finalize) => { calls.push([id, finalize]); },
  };
  const dictation = createDictation(bridge, {
    state: (phase) => states.push(phase),
    transcript: (text) => words.push(text),
  });
  return { dictation, calls, states, words, emit: (event: SpeechEvent) => handler(event) };
}

test('dictation receives its own partials and preserves final speech when paused', async () => {
  const { dictation, calls, states, words, emit } = fixture();
  await dictation.start();
  emit({ id: 8, kind: 'transcript', text: 'other' });
  emit({ id: 7, kind: 'ready' });
  emit({ id: 7, kind: 'transcript', text: 'hello' });
  dictation.stop(true);
  emit({ id: 7, kind: 'transcript', text: 'hello there', final: true });
  emit({ id: 7, kind: 'ended' });
  emit({ id: 7, kind: 'transcript', text: 'late' });
  assert.deepEqual(states, ['preparing', 'recording', 'stopping', 'idle']);
  assert.deepEqual(words, ['hello', 'hello there']);
  assert.deepEqual(calls, [[7, true]]);
});

test('switching chats discards late speech even during finalization', async () => {
  const { dictation, calls, words, emit } = fixture();
  await dictation.start();
  emit({ id: 7, kind: 'ready' });
  dictation.stop(true);
  dictation.stop(false);
  emit({ id: 7, kind: 'transcript', text: 'late' });
  emit({ id: 7, kind: 'ended' });
  assert.deepEqual(calls, [[7, true], [7, false]]);
  assert.deepEqual(words, []);
});

test('cancelling while native start is pending discards speech and stops the returned session', async () => {
  let complete!: (id: number) => void;
  let emit = (_: SpeechEvent) => {};
  const calls: [number, boolean][] = [];
  const words: string[] = [];
  const bridge: SpeechBridge = {
    start: (handler) => { emit = handler; return new Promise((resolve) => { complete = resolve; }); },
    stop: async (id, finalize) => { calls.push([id, finalize]); },
  };
  const phases: string[] = [];
  const dictation = createDictation(bridge, { state: (phase) => phases.push(phase), transcript: (text) => words.push(text) });
  const starting = dictation.start();
  dictation.stop(true);
  emit({ id: 1, kind: 'transcript', text: 'late' });
  complete(1);
  await starting;
  emit({ id: 1, kind: 'ended' });
  assert.deepEqual(calls, [[1, false]]);
  assert.deepEqual(words, []);
  assert.deepEqual(phases, ['preparing', 'stopping', 'idle']);
});

test('events arriving before the native start response are processed for the correct session', async () => {
  let complete!: (id: number) => void;
  let emit = (_: SpeechEvent) => {};
  const phases: string[] = [];
  const words: string[] = [];
  const bridge: SpeechBridge = {
    start: (handler) => { emit = handler; return new Promise((resolve) => { complete = resolve; }); },
    stop: async () => {},
  };
  const dictation = createDictation(bridge, { state: (phase) => phases.push(phase), transcript: (text) => words.push(text) });
  const starting = dictation.start();
  await Promise.resolve();
  emit({ id: 9, kind: 'ready' });
  emit({ id: 9, kind: 'transcript', text: 'Hi' });
  complete(9);
  await starting;
  assert.deepEqual(phases, ['preparing', 'recording']);
  assert.deepEqual(words, ['Hi']);
  dictation.stop(false);
  emit({ id: 9, kind: 'ended' });
});
