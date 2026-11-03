import assert from 'node:assert/strict';
import { test } from 'node:test';
import { observe, ControlError } from './client.mjs';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { fileURLToPath } from 'node:url';

test('agent and script preflight expose the session capability boundary', async () => {
  const result = await observe('permissions');
  assert.equal(typeof result.accessibility, 'boolean');
  assert.equal(typeof result.screenRecording, 'boolean');
  assert.equal(result.mutation, 'session-only');
  assert.equal(result.permissionPrompts, 'never requested');
  assert.deepEqual(result.setupOperations, ['request-accessibility']);
  assert.deepEqual(result.operations, ['permissions', 'desktop', 'windows', 'inspect', 'capture', 'session']);
});

test('mutations have no fallback and no target-discovery side effect', async () => {
  for (const operation of ['press', 'type', 'key', 'scroll', 'acquire', 'run_script']) {
    await assert.rejects(observe(operation), error =>
      error instanceof ControlError && error.response.code === 'unsupported_operation');
  }
});

test('foreign instances, missing scope, unknown fields and invalid budgets fail explicitly', async () => {
  for (const [request, code] of [
    [{}, 'instance_required'],
    [{ instance: '/Applications/Polymux.app' }, 'foreign_instance'],
    [{ pid: process.pid }, 'invalid_request'],
    [{ maxElements: 0 }, 'invalid_budget'],
    [{ window: -1 }, 'invalid_request'],
  ]) {
    await assert.rejects(observe('windows', request), error => error.response?.code === code);
  }
});

test('a closed response pipe exits without a native abort', async () => {
  const child = spawn(fileURLToPath(new URL('../../scripts/control.sh', import.meta.url)), ['permissions'], {
    stdio: ['ignore', 'pipe', 'ignore'],
  });
  const ended = once(child, 'exit');
  child.stdout.destroy();
  const [code, signal] = await ended;
  assert.equal(signal, null);
  assert.equal(code, 1);
});
