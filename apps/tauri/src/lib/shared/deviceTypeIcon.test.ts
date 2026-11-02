import assert from 'node:assert/strict';
import test from 'node:test';
import {deviceTypeIconName} from './deviceTypeIcon';

test('every device kind keeps the glyph the Devices panel draws', () => {
  assert.equal(deviceTypeIconName('laptop'), 'laptop');
  assert.equal(deviceTypeIconName('tablet'), 'tablet');
  assert.equal(deviceTypeIconName('phone'), 'phone');
  assert.equal(deviceTypeIconName('server'), 'server');
  assert.equal(deviceTypeIconName('pc'), 'computer');
  assert.equal(deviceTypeIconName(undefined), 'computer');
});
