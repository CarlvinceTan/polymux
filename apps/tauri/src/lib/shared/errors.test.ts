import assert from 'node:assert/strict';
import test from 'node:test';
import {readableError} from './errors';

test('internal lowercase error codes never reach the UI', () => {
  assert.equal(
    readableError(
      new Error(
        "Error invoking remote method 'polymux:comms:chat:send-files': Error: wechat_not_running",
      ),
    ),
    'Something went wrong. Please try again.',
  );
});

test('actionable sentences survive IPC error cleanup', () => {
  assert.equal(
    readableError(
      new Error(
        "Error invoking remote method 'polymux:comms:chat:send-files': Error: Open WeChat and make sure you are signed in, then try again.",
      ),
    ),
    'Open WeChat and make sure you are signed in, then try again.',
  );
});
