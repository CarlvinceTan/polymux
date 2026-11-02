import test from 'node:test';
import assert from 'node:assert/strict';
import {
  loadAgentDraft,
  loadChatDraft,
  loadMailDraft,
  saveAgentDraft,
  saveChatDraft,
  saveMailDraft,
} from './composerDrafts';

class MemoryStorage {
  readonly #values = new Map<string, string>();
  get length(): number { return this.#values.size; }
  clear(): void { this.#values.clear(); }
  getItem(key: string): string | null { return this.#values.get(key) ?? null; }
  key(index: number): string | null { return [...this.#values.keys()][index] ?? null; }
  removeItem(key: string): void { this.#values.delete(key); }
  setItem(key: string, value: string): void { this.#values.set(key, value); }
}

test('agent and Hub chat drafts are isolated by conversation and removed after send', () => {
  Object.defineProperty(globalThis, 'localStorage', {value: new MemoryStorage(), configurable: true});

  saveAgentDraft('agent-a', 'one');
  saveAgentDraft('agent-b', 'two');
  saveChatDraft('chat-a', {text: 'hello', replyTo: 'message-1', files: ['/tmp/photo.png']});

  assert.equal(loadAgentDraft('agent-a'), 'one');
  assert.equal(loadAgentDraft('agent-b'), 'two');
  assert.deepEqual(loadChatDraft('chat-a'), {
    text: 'hello',
    replyTo: 'message-1',
    files: ['/tmp/photo.png'],
  });
  assert.deepEqual(loadChatDraft('chat-b'), {text: '', replyTo: null, files: []});

  saveAgentDraft('agent-a', '');
  saveChatDraft('chat-a', {text: '', replyTo: null, files: []});
  assert.equal(loadAgentDraft('agent-a'), '');
  assert.deepEqual(loadChatDraft('chat-a'), {text: '', replyTo: null, files: []});
});

test('older Hub chat drafts load without an attachment list', () => {
  const target = new MemoryStorage();
  target.setItem('polymux.composer-drafts.v1', JSON.stringify({
    agent: {},
    chat: {'chat-a': {text: 'kept', replyTo: null}},
    mail: {},
  }));
  Object.defineProperty(globalThis, 'localStorage', {value: target, configurable: true});

  assert.deepEqual(loadChatDraft('chat-a'), {text: 'kept', replyTo: null, files: []});
});

test('mail drafts preserve valid inline file offsets and discard forged ones', () => {
  Object.defineProperty(globalThis, 'localStorage', {value: new MemoryStorage(), configurable: true});
  saveMailDraft({
    localId: 'mail-1',
    revision: 1,
    pending: true,
    account: 'work',
    folder: 'INBOX',
    to: '',
    cc: '',
    bcc: '',
    subject: '',
    body: 'Before\nAfter',
    signatureId: '',
    signatureBody: '',
    signatureHtml: null,
    files: ['/tmp/report.pdf'],
    inlineFiles: [
      {path: '/tmp/report.pdf', contentId: 'report@polymux.local', offset: 7},
      {path: '/tmp/not-attached.pdf', contentId: 'forged@polymux.local', offset: 1},
    ],
    importance: 'normal',
    reply: null,
    remoteDraft: null,
  });

  assert.deepEqual(loadMailDraft('work')?.inlineFiles, [
    {path: '/tmp/report.pdf', contentId: 'report@polymux.local', offset: 7},
  ]);
});
