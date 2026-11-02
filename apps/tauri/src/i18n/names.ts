import type {CommsEmailPreset, DriveProviderId} from '@polymux/protocol';
import {translate, type MessageKey} from './index';

/**
 * Names and blurbs for things the backend describes in English.
 *
 * The main process labels a storage provider from `@polymux/protocol`, which is
 * shared with code that has no catalog to read — so the wording arrives in
 * English and is replaced here, where the interface language is known. Product
 * names are left exactly as they are: "Dropbox" is Dropbox in every language,
 * and only the two entries that describe something rather than name it — this
 * machine, generic S3 storage, and the virtual drive — have anything to
 * translate.
 */
const PROVIDER_NAMES: Partial<Record<DriveProviderId, MessageKey>> = {
  all: 'drive.all',
  local: 'drive.thisMac',
  network: 'drive.network',
  s3: 'drive.s3',
};

export function driveProviderName(id: DriveProviderId, fallback: string): string {
  const key = PROVIDER_NAMES[id];
  return key ? translate(key) : fallback;
}

/**
 * Only the providers Settings can list. The virtual drive has no row there —
 * it is a view of the others rather than something to connect — so it has no
 * blurb to translate.
 */
const PROVIDER_DESCRIPTIONS: Partial<Record<DriveProviderId, MessageKey>> = {
  local: 'drive.about.local',
  'google-drive': 'drive.about.googleDrive',
  dropbox: 'drive.about.dropbox',
  onedrive: 'drive.about.onedrive',
  s3: 'drive.about.s3',
};

export function driveProviderDescription(id: DriveProviderId): string {
  const key = PROVIDER_DESCRIPTIONS[id];
  return key ? translate(key) : '';
}

/**
 * Mail providers. The names are the services' own — "Gmail / Google Workspace"
 * is the same everywhere — but the hint under the picker is advice, and advice
 * is written in the reader's language.
 */
const EMAIL_PRESET_HINTS: Record<CommsEmailPreset, MessageKey> = {
  gmail: 'hub.preset.gmail',
  outlook: 'hub.preset.outlook',
  icloud: 'hub.preset.icloud',
  lark: 'hub.preset.lark',
  fastmail: 'hub.preset.fastmail',
  custom: 'hub.preset.custom',
};

export function emailPresetHint(preset: CommsEmailPreset): string {
  const key = EMAIL_PRESET_HINTS[preset];
  return key ? translate(key) : '';
}

/**
 * What to do with a pairing QR, written per network.
 *
 * Bridges send their own instruction line, but it is usually a bare "scan
 * this" — which is the one thing a person already knows. What they do not know
 * is where the scanner lives, and that path is different on every app. These
 * name it, so they take precedence over the bridge's own text — and unlike the
 * bridge's, they are in the reader's language.
 */
const QR_INSTRUCTIONS: Record<string, MessageKey> = {
  whatsapp: 'hub.qr.whatsapp',
};

/** The scan instruction for a platform, or null when we have nothing better. */
export function qrInstructions(platform: string | undefined): string | null {
  if (!platform) return null;
  const key = QR_INSTRUCTIONS[platform.toLowerCase()];
  return key ? translate(key) : null;
}
