import assert from "node:assert/strict";
import { test } from "node:test";
import {
  LockerSelectionGate,
  lockerEditDraft,
  normalizeLockerSaveDraft,
  type LockerItemDto,
  type LockerSecretsDto,
} from "../src/index.js";

const item: LockerItemDto = {
  id: "item-1",
  title: "Mail",
  username: "me",
  url: "https://example.com",
  notes: "note",
  groupId: "group-1",
  groupName: "Personal",
  hasPassword: true,
  hasTotp: true,
  hasRecoveryCodes: true,
  hasPasskey: true,
  updatedAt: null,
};
const secrets: LockerSecretsDto = {
  password: "secret",
  totp: null,
  recoveryCodes: ["one"],
  passkey: {
    relyingParty: "example.com",
    username: "me",
    credentialId: "credential",
    userHandle: "handle",
  },
};

test("editing preserves hidden TOTP and passkey private key fields", () => {
  const draft = lockerEditDraft(item, secrets);
  const payload = normalizeLockerSaveDraft(draft);
  assert.equal(payload.password, "secret");
  assert.equal("totpSecret" in payload, false);
  assert.equal("privateKeyPem" in (payload.passkey ?? {}), false);
});

test("editing omits a blank hidden password instead of deleting it", () => {
  const payload = normalizeLockerSaveDraft({
    ...lockerEditDraft(item, secrets),
    password: "",
  });
  assert.equal("password" in payload, false);
});

test("selection gate rejects late responses and invalidated work", () => {
  const gate = new LockerSelectionGate();
  const first = gate.begin("first");
  const second = gate.begin("second");
  assert.equal(gate.accepts(first, "second"), false);
  assert.equal(gate.accepts(second, "second"), true);
  gate.invalidate();
  assert.equal(gate.accepts(second, "second"), false);
});
