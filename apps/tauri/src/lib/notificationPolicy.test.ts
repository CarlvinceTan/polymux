import assert from 'node:assert/strict';
import test from 'node:test';
import type { Notice } from './api/contract';
import { shouldNotify } from './notificationPolicy';

const notice = (patch: Partial<Notice> = {}): Notice => ({
  body: 'A message', outcome: 'notice', seq: 1, conversationID: 'chat', messageID: 'm1',
  agentID: 'bot', title: 'Bot', createdAt: new Date().toISOString(), ...patch,
});

test('notifications respect the visible chat, mute list and freshness', () => {
  const now = Date.parse('2026-10-31T10:00:00Z');
  assert.equal(shouldNotify(notice({ createdAt: '2026-10-31T09:59:00Z' }), { muted: new Set(), now }), true);
  assert.equal(shouldNotify(notice({ createdAt: '2026-10-31T09:59:00Z' }), { muted: new Set(), visibleThread: 'chat', now }), false);
  assert.equal(shouldNotify(notice({ createdAt: '2026-10-31T09:59:00Z' }), { muted: new Set(['bot']), now }), false);
  assert.equal(shouldNotify(notice({ createdAt: '2026-10-31T09:00:00Z' }), { muted: new Set(), now }), false);
  assert.equal(shouldNotify(notice({ createdAt: 'invalid' }), { muted: new Set(), now }), false);
});
