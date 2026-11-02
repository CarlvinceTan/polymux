export const HOST_RELAY_PROTOCOL_VERSION = 1;
export const HOST_RELAY_CONNECT_CONFIG_PATH = "/connect-config";
export const HOST_RELAY_CODE_PAIR_PATH = "/connect";
export const HOST_PAIRING_CODE_LENGTH = 9;
/** Crockford Base32: digits and letters without I, L, O, or U. */
export const HOST_PAIRING_CODE_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
export const HOST_PAIRING_CODE = new RegExp(
  `^[${HOST_PAIRING_CODE_ALPHABET}]{${HOST_PAIRING_CODE_LENGTH}}$`,
);

export function normalizeHostPairingCode(value: string): string {
  return value.replace(/[\s-]/g, "").toUpperCase().replace(/[IL]/g, "1").replace(/O/g, "0");
}

export function isHostPairingCode(value: string): boolean {
  return HOST_PAIRING_CODE.test(normalizeHostPairingCode(value));
}

export function sanitizeHostPairingCodeInput(value: string): string {
  return [...normalizeHostPairingCode(value)]
    .filter((char) => HOST_PAIRING_CODE_ALPHABET.includes(char))
    .join("")
    .slice(0, HOST_PAIRING_CODE_LENGTH);
}

export function createHostPairingCode(randomIndex: (exclusiveMax: number) => number): string {
  let code = "";
  for (let attempt = 0; attempt < 32; attempt += 1) {
    code = "";
    let letters = false;
    let digits = false;
    for (let index = 0; index < HOST_PAIRING_CODE_LENGTH; index += 1) {
      const char = HOST_PAIRING_CODE_ALPHABET[randomIndex(HOST_PAIRING_CODE_ALPHABET.length)];
      if (!char) throw new Error("Pairing code generator returned an invalid index");
      code += char;
      if (char <= "9") digits = true;
      else letters = true;
    }
    if (letters && digits) return code;
  }
  return code;
}

export interface HostRelayConnectConfig {
  version: typeof HOST_RELAY_PROTOCOL_VERSION;
  webSocketOrigin: string;
}

export interface HostRelayRequest {
  type: "request";
  version: typeof HOST_RELAY_PROTOCOL_VERSION;
  id: string;
  method: "GET" | "POST" | "OPTIONS";
  path: string;
  headers: Record<string, string>;
  bodyBase64: string;
}

export interface HostRelayResponse {
  type: "response";
  version: typeof HOST_RELAY_PROTOCOL_VERSION;
  id: string;
  status: number;
  headers: Record<string, string>;
  bodyBase64: string;
}

export function hostRelayPublicEndpoint(relayOrigin: string, hostId: string): string {
  return `${normalizeHostRelayOrigin(relayOrigin)}/h/${encodeURIComponent(hostId)}`;
}

export function hostRelayConnectConfigEndpoint(relayOrigin: string): string {
  return `${normalizeHostRelayOrigin(relayOrigin)}${HOST_RELAY_CONNECT_CONFIG_PATH}`;
}

export function hostRelayCodePairEndpoint(relayOrigin: string): string {
  return `${normalizeHostRelayOrigin(relayOrigin)}${HOST_RELAY_CODE_PAIR_PATH}`;
}

export function hostRelayConnectCodeEndpoint(relayOrigin: string, hostId: string): string {
  return `${normalizeHostRelayOrigin(relayOrigin)}/relay/${encodeURIComponent(hostId)}/connect-code`;
}

export function hostRelayWebSocketEndpoint(relayOrigin: string, hostId: string): string {
  const url = new URL(`${normalizeHostRelayOrigin(relayOrigin)}/relay/${encodeURIComponent(hostId)}`);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}

export function normalizeHostRelayOrigin(value: string): string {
  const url = new URL(value.trim());
  const loopback = url.hostname === "127.0.0.1" || url.hostname === "localhost" || url.hostname === "[::1]";
  if (url.protocol !== "https:" && !(loopback && url.protocol === "http:"))
    throw new Error("The Polymux relay must use HTTPS");
  if (url.username || url.password || url.search || url.hash)
    throw new Error("The Polymux relay address cannot contain credentials, a query, or a fragment");
  url.pathname = url.pathname.replace(/\/+$/, "");
  return url.toString().replace(/\/$/, "");
}

export function isHostRelayRequest(value: unknown): value is HostRelayRequest {
  if (!record(value)) return false;
  return value.type === "request"
    && value.version === HOST_RELAY_PROTOCOL_VERSION
    && shortId(value.id)
    && (value.method === "GET" || value.method === "POST" || value.method === "OPTIONS")
    && typeof value.path === "string"
    && value.path.startsWith("/polymux-host/v1/")
    && value.path.length <= 2_048
    && stringRecord(value.headers)
    && typeof value.bodyBase64 === "string";
}

export function isHostRelayConnectConfig(value: unknown): value is HostRelayConnectConfig {
  return record(value)
    && value.version === HOST_RELAY_PROTOCOL_VERSION
    && typeof value.webSocketOrigin === "string"
    && value.webSocketOrigin.length > 0
    && value.webSocketOrigin.length <= 2_048;
}

export function isHostRelayResponse(value: unknown): value is HostRelayResponse {
  if (!record(value)) return false;
  return value.type === "response"
    && value.version === HOST_RELAY_PROTOCOL_VERSION
    && shortId(value.id)
    && typeof value.status === "number"
    && Number.isInteger(value.status)
    && value.status >= 100
    && value.status <= 599
    && stringRecord(value.headers)
    && typeof value.bodyBase64 === "string";
}

function shortId(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 120;
}

function record(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function stringRecord(value: unknown): value is Record<string, string> {
  return record(value)
    && Object.entries(value).length <= 24
    && Object.entries(value).every(([key, item]) => key.length <= 120 && typeof item === "string" && item.length <= 8_192);
}
