export type DeviceType = 'tablet' | 'phone' | 'pc' | 'laptop' | 'server';
export function deviceType(value: unknown): DeviceType | undefined { return ['tablet', 'phone', 'pc', 'laptop', 'server'].includes(value as string) ? value as DeviceType : undefined; }
export interface DeviceApprovalDto {
  id: string;
  deviceName: string;
  choices: string[];
  expiresAt: string;
}
export interface DevicePairingState {
  approvals: DeviceApprovalDto[];
  outgoing: {id: string; deviceName: string; number: string; expiresAt: string} | null;
  connectedDevices: Array<{deviceId: string; deviceName: string; pairedAt: string; deviceType?: DeviceType; online?: boolean}>;
  error?: string;
  connected?: boolean;
  installCommand?: string;
  executionDeviceId?: string;
}
export type DevicePairingRequest =
  | {action: 'state'}
  | {action: 'execution'; conversationId: string}
  | {action: 'start'; code?: string; endpoint?: string; invitation?: string}
  | {action: 'approve'; id: string; number: string | null}
  | {action: 'cancel'}
  | {action: 'invitation'}
  | {action: 'revoke'; deviceId: string};
