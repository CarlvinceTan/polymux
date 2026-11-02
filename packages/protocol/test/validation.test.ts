import assert from "node:assert/strict";
import { test } from "node:test";
import {
  DRIVE_CONNECTABLE,
  driveProvider,
  driveS3Config,
  driveSaveOrder,
  validateGoalCommand,
  validateStartRun,
  validateSaveMailSignatures,
} from "../src/index.js";

test("validates renderer run requests at the Electron boundary", () => {
  assert.deepEqual(validateStartRun({ conversationId: "one", text: "hello" }), {
    conversationId: "one",
    text: "hello",
    deviceId: undefined,
    messageId: undefined,
    attachments: undefined,
    asGoal: undefined,
    reasoning: undefined,
    speechMode: undefined,
    reuseUserMessage: undefined,
    rewind: undefined,
  });
  assert.equal(validateStartRun({conversationId: "one", text: "remote", deviceId: "device-b"}).deviceId, "device-b");
  assert.throws(() => validateStartRun({conversationId: "one", text: "remote", deviceId: 123}));
  assert.equal(
    validateStartRun({ conversationId: "one", text: "ship it", asGoal: true })
      .asGoal,
    true,
  );
  assert.equal(
    validateStartRun({ conversationId: "one", text: "hi", speechMode: true })
      .speechMode,
    true,
  );
  assert.throws(
    () => validateStartRun({ conversationId: "one", text: "hi", speechMode: "yes" }),
    /speechMode/,
  );
  assert.equal(
    validateStartRun({ conversationId: "one", text: "think", reasoning: "high" })
      .reasoning,
    "high",
  );
  assert.throws(
    () => validateStartRun({ conversationId: "", text: "hello" }),
    /conversationId/,
  );
  assert.equal(
    validateStartRun({
      conversationId: "one",
      text: "revised",
      messageId: "user-1",
      rewind: true,
      reuseUserMessage: true,
    }).rewind,
    true,
  );
  assert.throws(
    () => validateStartRun({ conversationId: "one", text: "revised", rewind: true }),
    /messageId/,
  );
});

test("validates account-scoped mail signatures without flattening their layout", () => {
  assert.deepEqual(
    validateSaveMailSignatures({
      account: "work",
      signatures: [{id: "usual", name: " Usual ", body: "Best,\r\n  Carlvince", html: "<b>Best,</b><br>  Carlvince"}],
      defaultSignatureId: "usual",
    }),
    {
      account: "work",
      signatures: [{id: "usual", name: "Usual", body: "Best,\n  Carlvince", html: "<b>Best,</b><br>  Carlvince"}],
      defaultSignatureId: "usual",
    },
  );
  assert.throws(
    () => validateSaveMailSignatures({
      account: "work",
      signatures: [{id: "one", name: "One", body: "x", html: null}],
      defaultSignatureId: "missing",
    }),
    /default signature/i,
  );
  assert.throws(
    () => validateSaveMailSignatures({
      account: "work",
      signatures: [{id: "one", name: "One", body: "x", html: '<img src=x onerror="alert(1)">'}],
      defaultSignatureId: "one",
    }),
    /unsupported markup/i,
  );
});

test("accepts only storage providers this build knows", () => {
  assert.equal(driveProvider("google-drive"), "google-drive");
  assert.throws(() => driveProvider("icloud"), /not a supported storage provider/);
  assert.throws(() => driveProvider(7), /not a supported storage provider/);
});

test("keeps every storage provider reachable in the save order", () => {
  // Connectable, not every provider: the save order answers where a new file
  // goes, and the virtual drive holds nothing — naming it would make it the
  // destination for the files it only shows.
  const all = DRIVE_CONNECTABLE.map((entry) => entry.value);
  // A stored order naming only some providers must still list the rest, or
  // one left out of it could never be written to again.
  assert.deepEqual(driveSaveOrder(["s3"]), [
    "s3",
    ...all.filter((id) => id !== "s3"),
  ]);
  // An order written by a build that knew a provider this one does not must
  // load rather than throw.
  assert.deepEqual(driveSaveOrder(["icloud", "local"]), [
    "local",
    ...all.filter((id) => id !== "local"),
  ]);
  assert.deepEqual(driveSaveOrder("nonsense"), all);
  // Duplicates collapse rather than making a provider appear twice.
  assert.deepEqual(driveSaveOrder(["local", "local"]), [
    "local",
    ...all.filter((id) => id !== "local"),
  ]);
});

test("requires the fields an S3 bucket cannot be reached without", () => {
  const config = driveS3Config({
    bucket: " files ",
    region: "auto",
    accessKeyId: "AKIA",
    secretAccessKey: "shh",
    endpoint: "  ",
    forcePathStyle: true,
  });
  assert.equal(config.bucket, "files");
  // A blank endpoint means AWS, which is an absent value rather than "".
  assert.equal(config.endpoint, null);
  assert.equal(config.forcePathStyle, true);
  // An omitted secret is how an edit keeps the stored one, so it must survive
  // validation rather than being rejected as missing.
  assert.equal(driveS3Config({...config, secretAccessKey: ""}).secretAccessKey, undefined);
  assert.throws(() => driveS3Config({region: "auto", accessKeyId: "AKIA"}), /Bucket/);
  assert.throws(() => driveS3Config(null), /required/);
});

test("requires an objective when creating or updating a goal", () => {
  assert.equal(
    validateGoalCommand({ conversationId: "one", action: "pause" }).action,
    "pause",
  );
  assert.throws(
    () => validateGoalCommand({ conversationId: "one", action: "create" }),
    /objective/,
  );
  assert.throws(
    () => validateGoalCommand({ conversationId: "one", action: "update" }),
    /objective/,
  );
});
