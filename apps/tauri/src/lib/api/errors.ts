import type { WireError } from './contract';
import { decodeJSON } from './json';

export class ResponseError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
  }
  get isOffline(): boolean {
    return this.status === 0;
  }
  get isTransient(): boolean {
    return this.isOffline || this.status >= 500;
  }
  reason(fallback: string): string {
    return this.status >= 400 && this.status < 500 && this.message ? this.message : fallback;
  }
}

export class InvalidURLError extends Error {
  constructor() {
    super('The server address is invalid.');
  }
}

export class InvalidResponseError extends Error {
  constructor() {
    super('The server returned an unexpected response.');
  }
}

import type { DraftState } from './models';

export class DraftConflictError extends Error {
  constructor(readonly current: DraftState) {
    super('Draft conflict');
  }
}

export type ClientFault = ResponseError | InvalidURLError | InvalidResponseError;

export function isOffline(error: unknown): boolean {
  return error instanceof TypeError || (error instanceof ResponseError && error.isOffline);
}

export function isTransient(error: unknown): boolean {
  if (isOffline(error)) return true;
  if (error instanceof ResponseError) return error.status >= 500;
  return false;
}

/** The server's own explanation of a refused request, otherwise the given wording. */
export function reason(error: unknown, fallback: string): string {
  if (error instanceof ResponseError && error.status >= 400 && error.status < 500 && error.message) {
    return error.message;
  }
  return fallback;
}

export async function faultMessage(error: unknown): Promise<string> {
  if (error instanceof ResponseError || error instanceof InvalidURLError || error instanceof InvalidResponseError) {
    return error.message;
  }
  return "Couldn't complete this request.";
}

export async function wireError(response: Response): Promise<string> {
  try {
    const body = await decodeJSON<WireError>(response);
    if (body?.message) return body.message;
  } catch {
    // fall through to the status text
  }
  return response.statusText || `HTTP ${response.status}`;
}
