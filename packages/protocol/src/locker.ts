/** Shared Locker contract for desktop IPC, the browser extension, and the phone Host. */

export const LOCKER_HOST_METHODS = [
  "locker.status",
  "locker.create",
  "locker.unlock",
  "locker.lock",
  "locker.list",
  "locker.reveal",
  "locker.totp",
  "locker.save",
  "locker.remove",
  "locker.restore",
  "locker.purge",
  "locker.emptyTrash",
  "locker.pin",
  "locker.reorder",
  "locker.changePassword",
  "locker.codes",
  "locker.otpauth",
  "locker.copy",
  "locker.sync",
  "locker.setStorage",
  "locker.export",
  "locker.import",
] as const;

export type LockerHostMethod = (typeof LOCKER_HOST_METHODS)[number];

/** Loopback routes the existing Polymux extension uses when desktop is running. */
export const LOCKER_SURFACE_PATHS = {
  status: "/v1/locker/status",
  unlock: "/v1/locker/unlock",
  lock: "/v1/locker/lock",
  matches: "/v1/locker/matches",
  fill: "/v1/locker/fill",
  save: "/v1/locker/save",
  totp: "/v1/locker/totp",
  export: "/v1/locker/export",
  import: "/v1/locker/import",
  passkeys: "/v1/locker/passkeys",
  passkeyGet: "/v1/locker/passkey/get",
  passkeyCreate: "/v1/locker/passkey/create",
} as const;

export const LOCKER_SURFACE_CAPABILITY = "locker-fill-v1";
