import assert from 'node:assert/strict';
import test from 'node:test';
import { Draft } from './draft';
import { DraftConflictError } from './api/errors';
import type { DraftState } from './api/models';

const storage = new Map<string, string>();
Object.assign(globalThis, { localStorage: {
  getItem: (key: string) => storage.get(key) ?? null,
  setItem: (key: string, value: string) => storage.set(key, value),
} });
const empty = (): DraftState => ({ revision: 0, writeID: '', value: { text: '', mentions: [] } });

test('changing only a mention target is saved and clearing text clears the token', async () => {
  storage.clear();
  const writes: DraftState[] = [];
  const store = new Draft('mentions-draft', {
    draft: async () => empty(),
    saveDraft: async (_id, state) => { writes.push(structuredClone(state)); return { ...state, revision: writes.length }; },
  }, 'chat', () => {});
  const first = { id: 'one', text: '@Alex', location: 0, length: 5 };
  store.edit('@Alex', [first]);
  await store.flush();
  store.edit('@Alex', [{ ...first, id: 'two' }]);
  await store.flush();
  store.edit('');
  await store.flush();
  assert.deepEqual(writes.map((item) => item.value.mentions.map((token) => token.id)), [['one'], ['two'], []]);
  store.stop();
});

test('a delayed save cannot overwrite newer typing and the next write uses the accepted revision', async () => {
  storage.clear();
  let release!: (state: DraftState) => void;
  const sent: DraftState[] = [];
  let latest = empty();
  const store = new Draft('draft', {
    draft: async () => empty(),
    saveDraft: async (_id, state) => {
      sent.push(structuredClone(state));
      if (sent.length === 1) return await new Promise<DraftState>((resolve) => { release = resolve; });
      return { ...state, revision: 2 };
    },
  }, 'chat', (state) => { latest = state; });
  store.edit('first');
  const first = store.flush();
  store.edit('newer');
  release({ ...sent[0], revision: 1 });
  await first;
  assert.equal(latest.value.text, 'newer');
  await store.flush();
  assert.equal(sent[1].revision, 1);
  assert.equal(sent[1].value.text, 'newer');
  assert.notEqual(sent[0].writeID, sent[1].writeID);
  store.stop();
});

test('a conflict preserves local text and the other device draft until the user edits again', async () => {
  storage.clear();
  let attempts = 0;
  let issue = '';
  let current = empty();
  const store = new Draft('draft', {
    draft: async () => empty(),
    saveDraft: async (_id, state) => {
      attempts++;
      if (attempts === 1) throw new DraftConflictError({ ...empty(), revision: 3, value: { text: 'remote', mentions: [] } });
      return { ...state, revision: 4 };
    },
  }, 'chat', (state, message) => { current = state; issue = message; });
  store.edit('local');
  await store.flush();
  assert.equal(current.value.text, 'local');
  assert.match(issue, /another device/);
  await store.flush();
  assert.equal(attempts, 1);
  store.edit('local revised');
  await store.flush();
  assert.equal(attempts, 2);
  assert.equal(issue, '');
  store.stop();
});

test('a lost acknowledgement retries the same write before saving newer offline text', async () => {
  storage.clear();
  const sent: DraftState[] = [];
  let offline = true;
  const client = {
    draft: async () => empty(),
    saveDraft: async (_id: string, state: DraftState) => {
      sent.push(structuredClone(state));
      if (offline) throw new Error('offline');
      return { ...state, revision: state.writeID === sent[0].writeID ? 1 : 2 };
    },
  };
  const first = new Draft('draft', client, 'chat', () => {});
  first.edit('committed');
  await first.flush();
  first.edit('newer offline text');
  first.stop();
  await Promise.resolve();
  const retry = sent.length;
  const second = new Draft('draft', client, 'chat', () => {});
  offline = false;
  await second.flush();
  assert.equal(sent[retry].writeID, sent[0].writeID);
  await second.flush();
  assert.equal(sent[retry + 1].value.text, 'newer offline text');
  assert.equal(sent[retry + 1].revision, sent[retry].revision + 1);
  second.stop();
});

test('reply edits survive a delayed write and draft conflicts', async () => {
  storage.clear();
  const reply = { messageID: 'm1', role: 'assistant' as const, text: 'Quoted', partIndex: 1 };
  const sent: DraftState[] = [];
  let release!: (state: DraftState) => void;
  let current = empty();
  let conflict = false;
  const store = new Draft('reply-draft', {
    draft: async () => empty(),
    saveDraft: async (_id, state) => {
      sent.push(structuredClone(state));
      if (sent.length === 1) return new Promise<DraftState>((resolve) => { release = resolve; });
      if (conflict) throw new DraftConflictError({ ...empty(), revision: 3 });
      return { ...state, revision: state.revision + 1 };
    },
  }, 'chat', (state) => { current = state; });
  store.edit('First');
  const pending = store.flush();
  store.reply(reply);
  store.edit('Newer');
  release({ ...sent[0], revision: 1 });
  await pending;
  await store.flush();
  assert.equal(sent[1].revision, 1);
  assert.equal(sent[1].value.text, 'Newer');
  assert.deepEqual(sent[1].value.reply, reply);
  conflict = true;
  store.reply(undefined);
  await store.flush();
  assert.equal(current.value.reply, undefined);
  assert.equal(current.value.text, 'Newer');
  conflict = false;
  store.reply(reply);
  await store.flush();
  assert.deepEqual(current.value.reply, reply);
  store.stop();
});
