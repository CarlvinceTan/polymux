export type AccountOAuthProvider = "google" | "apple";

export interface AccountProfileDto {
  userId: string;
  email: string;
  name: string;
  avatarUrl: string;
}

export interface AccountStatusDto {
  signedIn: boolean;
  /** Supabase is not configured for this build (no URL or publishable key). */
  available: boolean;
  profile: AccountProfileDto | null;
  /** Other sessions saved on this machine, excluding the current profile. */
  accounts: AccountProfileDto[];
}

export type AccountSignInResult = AccountStatusDto & {error?: string};

export interface AccountDeviceDto {
  /** Stable installation id (team.desktop-id on the other machine). */
  deviceId: string;
  deviceName: string;
  deviceType?: import("./device-pairing.js").DeviceType;
  platform: string;
  appVersion: string;
  online: boolean;
}
