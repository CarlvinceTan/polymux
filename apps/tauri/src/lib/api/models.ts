import type { ReplyContext } from './contract';

/** Types defined by hand in PolymuxKit rather than generated from the wire schema. */

export interface Invite {
  url: string;
  token: string;
  ticket?: string;
}

export interface NetworkInfoServer {
  name: string;
  build: string;
  platform: string;
  role: string;
}

export interface NetworkDevice {
  id: string;
  /** What the device reports, such as its hostname. */
  name: string;
  /** The name someone chose for it; empty when unset. */
  label?: string;
  /** False while an invite for it hasn't been used yet. */
  paired?: boolean;
}

export interface NetworkInfo {
  server: NetworkInfoServer;
  devices: NetworkDevice[];
  /** The device making the request, when the server identifies it. */
  current?: string;
  /** The device on the server's own machine. */
  host?: string;
  /** Devices online now: the server or a connected app. */
  online?: string[];
}

export interface ChatDraftMention {
  id: string;
  text: string;
  location: number;
  length: number;
}

export interface ChatDraft {
  text: string;
  mentions: ChatDraftMention[];
  reply?: ReplyContext;
}

export interface DraftState {
  revision: number;
  writeID: string;
  value: ChatDraft;
}

/** A composer file handed to the chat store before the server knows about it. */
export interface LocalFile {
  id: string;
  url: string;
  name: string;
  kind: string;
  mimeType: string;
  size: number;
}

/** Apple's starting recents, minus the fixed tapbacks; fills the row until there is history. */
export const REACTION_EMOJIS = ['❤️', '👍', '👎', '😂', '‼️', '❓'] as const;
export const REACTION_DEFAULT_RECENTS = ['😍', '😒', '👌', '😊', '😘'] as const;

/** Recorded instead of an option id when the user skips a question card. */
export const CHOICE_DISMISSED = '_dismissed';

/** Client-only message status: a send the outbox stopped retrying. */
export const MESSAGE_STATUS_FAILED = 'failed';

export function messageText(parts: { text?: string }[]): string {
  return parts.map((part) => part.text ?? '').join('');
}

export function messageAttachments(parts: { attachment?: unknown }[]): unknown[] {
  return parts.map((part) => part.attachment).filter(Boolean);
}

/** The message status the bubble shows while the outbox retries a failed send. */
export function deviceTitle(device: NetworkDevice): string {
  return device.label ? device.label : device.name;
}
