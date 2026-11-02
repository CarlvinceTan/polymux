import {HOST_PAIRING_CODE, normalizeHostPairingCode} from "./relay.js";

const SETUP_CODE_PREFIX = "pmx1:";

export interface TeamHostSetupCode {
  endpoint: string;
  code: string;
}

/**
 * One paste carries both values needed to pair a Desktop with a personal Host.
 * The code is short-lived and intentionally contains no bearer secret.
 */
export function formatTeamHostSetupCode(endpoint: string, code: string): string {
  const address = secureSetupEndpoint(endpoint);
  const pairing = normalizeHostPairingCode(code);
  if (!address || !HOST_PAIRING_CODE.test(pairing))
    throw new Error("A Host endpoint and pairing code are required");
  return `${SETUP_CODE_PREFIX}${encodeURIComponent(address)}:${pairing}`;
}

/** Accepts either the token itself or the full `Setup code: …` terminal line. */
export function parseTeamHostSetupCode(value: string): TeamHostSetupCode | null {
  const token = value.trim().match(/(?:^|\s)(pmx1:[^\s]+)/i)?.[1];
  if (!token) return null;
  const separator = token.lastIndexOf(":");
  if (separator <= SETUP_CODE_PREFIX.length) return null;
  const code = normalizeHostPairingCode(token.slice(separator + 1));
  if (!HOST_PAIRING_CODE.test(code)) return null;
  try {
    const endpoint = secureSetupEndpoint(
      decodeURIComponent(token.slice(SETUP_CODE_PREFIX.length, separator)),
    );
    return endpoint ? {endpoint, code} : null;
  } catch {
    return null;
  }
}

function secureSetupEndpoint(value: string): string | null {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    return null;
  }
  const hostname = url.hostname.replace(/^\[|\]$/g, "").toLowerCase();
  const loopback = hostname === "localhost" || hostname === "::1" || hostname === "127.0.0.1";
  if (url.protocol !== "https:" && !(url.protocol === "http:" && loopback)) return null;
  if (url.username || url.password) return null;
  url.search = "";
  url.hash = "";
  return url.toString().replace(/\/$/, "");
}
