import assert from "node:assert/strict";
import test from "node:test";
import {formatTeamHostSetupCode, parseTeamHostSetupCode} from "../src/host-setup.js";

test("Host setup code carries the endpoint and short-lived pairing code in one paste", () => {
  const setup = formatTeamHostSetupCode(
    "https://connect.polymux.com/h/86c92dd5-5042-4aa4-a33f-b656bf641e28/",
    "K7M2P9X4Q",
  );
  assert.equal(
    setup,
    "pmx1:https%3A%2F%2Fconnect.polymux.com%2Fh%2F86c92dd5-5042-4aa4-a33f-b656bf641e28:K7M2P9X4Q",
  );
  assert.deepEqual(parseTeamHostSetupCode(setup), {
    endpoint: "https://connect.polymux.com/h/86c92dd5-5042-4aa4-a33f-b656bf641e28",
    code: "K7M2P9X4Q",
  });
  assert.deepEqual(parseTeamHostSetupCode(`Setup code: ${setup}`), {
    endpoint: "https://connect.polymux.com/h/86c92dd5-5042-4aa4-a33f-b656bf641e28",
    code: "K7M2P9X4Q",
  });
  assert.deepEqual(
    parseTeamHostSetupCode(
      formatTeamHostSetupCode(
        "https://connect.polymux.com/h/86c92dd5-5042-4aa4-a33f-b656bf641e28/",
        "k7m-2p9-x4q",
      ),
    ),
    {
      endpoint: "https://connect.polymux.com/h/86c92dd5-5042-4aa4-a33f-b656bf641e28",
      code: "K7M2P9X4Q",
    },
  );
});

test("Host setup code rejects incomplete or malformed values", () => {
  assert.equal(parseTeamHostSetupCode("K7M2P9X4Q"), null);
  assert.equal(parseTeamHostSetupCode("pmx1:not-a-url:K7M2P9X4Q"), null);
  assert.equal(parseTeamHostSetupCode("pmx1:https%3A%2F%2Fhost:12"), null);
  assert.equal(parseTeamHostSetupCode("pmx1:http%3A%2F%2F100.90.80.70%3A47680:K7M2P9X4Q"), null);
  assert.equal(parseTeamHostSetupCode("pmx1:http%3A%2F%2F192.168.1.20%3A47680:K7M2P9X4Q"), null);
  assert.equal(parseTeamHostSetupCode("pmx1:https%3A%2F%2Fuser%3Asecret%40host.example:K7M2P9X4Q"), null);
  assert.throws(
    () => formatTeamHostSetupCode("http://100.90.80.70:47680", "K7M2P9X4Q"),
    /Host endpoint/,
  );
});

test("Host setup code permits cleartext loopback for local development only", () => {
  const setup = formatTeamHostSetupCode("http://127.0.0.1:47680/", "K7M2P9X4Q");
  assert.deepEqual(parseTeamHostSetupCode(setup), {
    endpoint: "http://127.0.0.1:47680",
    code: "K7M2P9X4Q",
  });
});
