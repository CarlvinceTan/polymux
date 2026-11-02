import type {
  LockerItemDto,
  LockerItemInputDto,
  LockerSecretsDto,
} from "./types.js";

export type LockerDraftKind = "password" | "totp" | "recovery" | "passkey";

export function emptyLockerDraft(kind: LockerDraftKind): LockerItemInputDto {
  return {
    title: "",
    username: "",
    url: "",
    notes: "",
    groupName: "",
    password: "",
    totpSecret: "",
    recoveryCodes: kind === "recovery" ? [""] : undefined,
    passkey:
      kind === "passkey"
        ? {
            relyingParty: "",
            username: "",
            credentialId: "",
            userHandle: "",
            privateKeyPem: "",
          }
        : undefined,
  };
}

export function lockerEditDraft(
  item: LockerItemDto,
  secrets: LockerSecretsDto,
): LockerItemInputDto {
  return {
    id: item.id,
    title: item.title,
    username: item.username,
    url: item.url,
    notes: item.notes,
    groupName: item.groupName,
    password: secrets.password,
    totpSecret: "",
    recoveryCodes: secrets.recoveryCodes.length
      ? [...secrets.recoveryCodes]
      : [""],
    passkey: secrets.passkey
      ? {
          relyingParty: secrets.passkey.relyingParty,
          username: secrets.passkey.username,
          credentialId: secrets.passkey.credentialId,
          userHandle: secrets.passkey.userHandle,
          privateKeyPem: "",
        }
      : undefined,
  };
}

export function normalizeLockerSaveDraft(
  draft: LockerItemInputDto,
): LockerItemInputDto {
  const payload: LockerItemInputDto = {
    ...draft,
    title: draft.title.trim(),
    recoveryCodes: draft.recoveryCodes
      ?.map((code) => code.trim())
      .filter(Boolean),
    passkey: draft.passkey ? { ...draft.passkey } : draft.passkey,
  };
  if (!payload.totpSecret) delete payload.totpSecret;
  if (payload.id && payload.password === "") delete payload.password;
  if (payload.id && payload.passkey && !payload.passkey.privateKeyPem)
    delete payload.passkey.privateKeyPem;
  return payload;
}

export interface LockerSelectionRequest {
  id: string;
  generation: number;
}

export class LockerSelectionGate {
  #generation = 0;

  begin(id: string): LockerSelectionRequest {
    return { id, generation: ++this.#generation };
  }

  invalidate(): void {
    this.#generation += 1;
  }

  accepts(request: LockerSelectionRequest, selectedId: string | null): boolean {
    return request.generation === this.#generation && request.id === selectedId;
  }
}
