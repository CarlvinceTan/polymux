import assert from 'node:assert/strict';
import test from 'node:test';
import { inviteLink, parseInvite, redeem, finish } from './pairing';
import { loadCredential } from './credential';
import { APIClient } from './client';

const storage = new Map<string, string>();
Object.assign(globalThis, { localStorage: {
  getItem: (key: string) => storage.get(key) ?? null,
  setItem: (key: string, value: string) => storage.set(key, value),
  removeItem: (key: string) => storage.delete(key),
} });

test('Reference pairing links round-trip and reject ambiguous links', () => {
  const invite = { url: 'https://server.example', token: 'one-time', ticket: 'ticket+/' };
  const link = inviteLink(invite);
  assert.ok(link.startsWith('polymux://connect?'));
  assert.deepEqual(parseInvite(link), invite);
  assert.equal(parseInvite(link + '&token=other'), undefined);
  assert.equal(parseInvite('polymux://connect/?url=https%3A%2F%2Fexample.com&token=x'), undefined);
});

test('server addresses with a path retain that path for API calls and credentials', async () => {
  storage.clear();
  const invite = { url: 'https://server.example/polymux', token: 'code' };
  let requestUrl = '';
  const original = globalThis.fetch;
  globalThis.fetch = async (input) => { requestUrl = String(input); return new Response(null, { status: 204 }); };
  try {
    const token = await redeem(invite);
    assert.equal(requestUrl, 'https://server.example/polymux/network/pair');
    await finish(invite, token);
    const client = new APIClient(invite.url);
    assert.equal(client.resolve('threads').toString(), 'https://server.example/polymux/threads');
    assert.equal(loadCredential(client.baseURL.toString())?.token, token);
  } finally { globalThis.fetch = original; }
});

test('pairing never sends credentials over untrusted plain HTTP', async () => {
  storage.clear();
  let calls = 0;
  const original = globalThis.fetch;
  globalThis.fetch = async () => { calls++; return new Response(null, { status: 204 }); };
  try {
    await assert.rejects(redeem({ url: 'http://192.168.1.10:7339', token: 'code' }));
    assert.equal(calls, 0);
    await redeem({ url: 'http://127.0.0.1:7339', token: 'code' });
    assert.equal(calls, 1);
  } finally { globalThis.fetch = original; }
});

test('claims survive a lost response, remain distinct per invite, and commit under the canonical address', async () => {
  storage.clear();
  const sent: string[] = [];
  const original = globalThis.fetch;
  let fail = true;
  globalThis.fetch = async (_url, init) => {
    sent.push(JSON.parse(String(init?.body)).token);
    if (fail) { fail = false; throw new TypeError('lost response'); }
    return new Response(null, { status: 204 });
  };
  try {
    const first = { url: 'https://first.example', token: 'first-code' };
    await assert.rejects(redeem(first));
    const token = await redeem(first);
    assert.equal(sent[0], sent[1]);
    await finish(first, token);
    assert.equal(loadCredential('https://first.example/')?.token, token);
    await redeem({ url: 'https://second.example', token: 'second-code' });
    assert.notEqual(sent[1], sent[2]);
  } finally { globalThis.fetch = original; }
});
