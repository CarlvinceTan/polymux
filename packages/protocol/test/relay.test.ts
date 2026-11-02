import assert from "node:assert/strict";
import test from "node:test";
import {
  HOST_RELAY_PROTOCOL_VERSION,
  hostRelayCodePairEndpoint,
  hostRelayConnectCodeEndpoint,
  hostRelayConnectConfigEndpoint,
  hostRelayPublicEndpoint,
  hostRelayWebSocketEndpoint,
  isHostRelayConnectConfig,
  isHostRelayRequest,
  isHostRelayResponse,
} from "../src/relay.js";

test("Host relay addresses keep the opaque Host id on the Polymux domain", () => {
  assert.equal(
    hostRelayPublicEndpoint("https://connect.polymux.com/", "host-a"),
    "https://connect.polymux.com/h/host-a",
  );
  assert.equal(
    hostRelayConnectConfigEndpoint("https://connect.polymux.com/"),
    "https://connect.polymux.com/connect-config",
  );
  assert.equal(
    hostRelayCodePairEndpoint("https://connect.polymux.com/"),
    "https://connect.polymux.com/connect",
  );
  assert.equal(
    hostRelayConnectCodeEndpoint("https://connect.polymux.com/", "host-a"),
    "https://connect.polymux.com/relay/host-a/connect-code",
  );
  assert.equal(
    hostRelayWebSocketEndpoint("https://connect.polymux.com", "host-a"),
    "wss://connect.polymux.com/relay/host-a",
  );
  assert.equal(
    hostRelayWebSocketEndpoint("http://127.0.0.1:8787", "host-a"),
    "ws://127.0.0.1:8787/relay/host-a",
  );
  assert.throws(() => hostRelayPublicEndpoint("http://connect.polymux.com", "host-a"), /HTTPS/);
});

test("Host relay accepts only a bounded socket discovery document", () => {
  const config = {
    version: HOST_RELAY_PROTOCOL_VERSION,
    webSocketOrigin: "https://polymux-connect.example.workers.dev",
  };
  assert.equal(isHostRelayConnectConfig(config), true);
  assert.equal(isHostRelayConnectConfig({...config, version: 2}), false);
  assert.equal(isHostRelayConnectConfig({...config, webSocketOrigin: 4}), false);
  assert.equal(isHostRelayConnectConfig({...config, webSocketOrigin: "x".repeat(2_049)}), false);
});

test("Host relay accepts only the bounded HTTP bridge message shapes", () => {
  const request = {
    type: "request",
    version: HOST_RELAY_PROTOCOL_VERSION,
    id: "request-a",
    method: "POST",
    path: "/polymux-host/v1/rpc",
    headers: {"content-type": "application/json"},
    bodyBase64: "e30=",
  };
  assert.equal(isHostRelayRequest(request), true);
  assert.equal(isHostRelayRequest({...request, path: "/admin"}), false);
  assert.equal(isHostRelayRequest({...request, method: "DELETE"}), false);

  const response = {
    type: "response",
    version: HOST_RELAY_PROTOCOL_VERSION,
    id: "request-a",
    status: 200,
    headers: {"content-type": "application/json"},
    bodyBase64: "e30=",
  };
  assert.equal(isHostRelayResponse(response), true);
  assert.equal(isHostRelayResponse({...response, status: 700}), false);
  assert.equal(isHostRelayResponse({...response, headers: {authorization: 4}}), false);
});
