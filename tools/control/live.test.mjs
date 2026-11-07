import assert from 'node:assert/strict';
import { test } from 'node:test';
import { connect, observe } from './client.mjs';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { fileURLToPath } from 'node:url';

const instance = process.env.POLYMUX_CONTROL_INSTANCE;

test('native handles, action fence, partial reads and observed button transitions', { skip: !instance }, async () => {
  const before = await observe('desktop');
  const session = await connect(instance);
  try {
    await assert.rejects(connect(instance), error => error.response?.code === 'target_busy');
    const first = await session.request({ op: 'snapshot' });
    assert.equal(first.complete, true);
    const button = first.elements.find(row => row.role === 'AXButton' && row.label === 'Settings');
    assert.ok(button);
    await session.request({ op: 'snapshot' });
    await assert.rejects(session.request({ op: 'press', token: button.token }), error => error.response?.code === 'stale_token');
    const partial = await session.request({ op: 'snapshot', maxElements: 1 });
    assert.equal(partial.complete, false);
    await assert.rejects(session.request({ op: 'press', token: partial.elements[0].token }), error => error.response?.code === 'stale_token');
    await assert.rejects(session.request({ op: 'snapshot', window: 'not-a-window' }), error => error.response?.code === 'stale_window');
    const state = await session.request({ op: 'snapshot' });
    const open = state.elements.find(row => row.label === 'Settings');
    await session.request({ op: 'press', token: open.token });
    await assert.rejects(session.request({ op: 'press', token: open.token }), error => error.response?.code === 'stale_token');
    let close;
    for (let i = 0; i < 20 && !close; i++) {
      await new Promise(resolve => setTimeout(resolve, 50));
      const result = await session.request({ op: 'snapshot' });
      assert.equal(result.complete, true);
      close = result.elements.find(row => row.label === 'Close Settings');
    }
    assert.ok(close, 'Settings appeared after AXPress');
    await session.request({ op: 'press', token: close.token });
    await new Promise(resolve => setTimeout(resolve, 100));
    const result = await session.request({ op: 'snapshot' });
    assert.ok(result.elements.some(row => row.label === 'Settings'));
    assert.ok(!result.elements.some(row => row.label === 'Close Settings'));
    const after = await observe('desktop');
    assert.equal(after.frontmost, before.frontmost);
  } finally { await session.close(); }
  const next = await connect(instance);
  try {
    await assert.rejects(next.request({ op: 'press', token: session.session + ':1:1' }), error => error.response?.code === 'stale_token');
  } finally { await next.close(); }
});

test('terminating a session releases ownership and invalidates its handles', { skip: !instance }, async () => {
  const session = await connect(instance);
  const snapshot = await session.request({ op: 'snapshot' });
  const pending = session.request({ op: 'snapshot' }).catch(error => error);
  await session.terminate();
  await pending;
  await assert.rejects(session.request({ op: 'snapshot' }), error => error.response?.code === 'transport_failed');
  const next = await connect(instance);
  try {
    await assert.rejects(next.request({ op: 'press', token: snapshot.elements[0].token }), error => error.response?.code === 'stale_token');
    assert.equal((await next.request({ op: 'snapshot' })).complete, true);
  } finally { await next.close(); }
});

test('undrained output cannot prevent deadline cleanup', { skip: !instance, timeout: 15000 }, async () => {
  const child = spawn(fileURLToPath(new URL('../../scripts/control.sh', import.meta.url)), [
    'session', JSON.stringify({ instance }),
  ], { stdio: ['pipe', 'pipe', 'ignore'] });
  const ended = once(child, 'exit');
  const deadline = setTimeout(() => child.kill('SIGKILL'), 14000);
  try {
    child.stdout.pause();
    child.stdin.on('error', () => {});
    child.stdin.end((JSON.stringify({ op: 'snapshot', maxElements: 5000 }) + '\n').repeat(100));
    const [code, signal] = await ended;
    assert.equal(signal, null, 'the native deadline must exit before the test kills it');
    assert.equal(code, 1);
  } finally {
    clearTimeout(deadline);
    child.stdout.destroy();
    if (child.exitCode === null) child.kill('SIGKILL');
  }
  const next = await connect(instance);
  await next.close();
});
