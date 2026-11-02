import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import test from "node:test";
import {formatTeamHostSetupCode} from "../src/host-setup.js";
import {qrMatrix, qrSvgPath} from "../src/qr.js";

const SETUP_CODE = formatTeamHostSetupCode(
  "https://connect.polymux.com/h/86c92dd5-5042-4aa4-a33f-b656bf641e28",
  "318204771",
);

test("Host pairing QR stays compatible with the independently decoded reference symbol", () => {
  const matrix = qrMatrix(SETUP_CODE);
  const modules = matrix.map((row) => row.map((dark) => dark ? "1" : "0").join("")).join("");
  assert.equal(matrix.length, 41);
  assert.equal(
    createHash("sha256").update(modules).digest("hex"),
    "e7af73e5cbcb0c57e31b99a71f35d88cf2904f817e48468b8f88928fa19bc69d",
  );

  const svg = qrSvgPath(SETUP_CODE);
  assert.equal(svg.size, matrix.length);
  assert.match(svg.path, /^M0,0h7v1h-7z/);
});

test("QR encoder refuses a payload larger than the standard byte-mode capacity", () => {
  assert.throws(() => qrMatrix("x".repeat(2_954)), /limit is 2953 bytes/);
});
