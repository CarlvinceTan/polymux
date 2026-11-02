import assert from "node:assert/strict";
import test from "node:test";
import {
  HOST_PAIRING_CODE,
  createHostPairingCode,
  isHostPairingCode,
  normalizeHostPairingCode,
  sanitizeHostPairingCodeInput,
} from "../src/relay.js";

test("pairing codes mix letters and digits and omit lookalike characters", () => {
  let next = 0;
  const code = createHostPairingCode((max) => {
    const index = next % max;
    next += 1;
    return index;
  });
  assert.match(code, HOST_PAIRING_CODE);
  assert.match(code, /[A-Z]/);
  assert.match(code, /\d/);
  assert.equal(isHostPairingCode(code.toLowerCase()), true);
});

test("pairing code input maps lookalikes, strips separators, and ignores other characters", () => {
  assert.equal(normalizeHostPairingCode("k7m-2p9 x4q"), "K7M2P9X4Q");
  assert.equal(sanitizeHostPairingCodeInput("ilo 234 567"), "110234567");
  assert.equal(isHostPairingCode("K7M2P9X4Q"), true);
  assert.equal(isHostPairingCode("318204771"), true);
  assert.equal(isHostPairingCode("318204"), false);
  assert.equal(isHostPairingCode("U7M2P9X4Q"), false);
});
