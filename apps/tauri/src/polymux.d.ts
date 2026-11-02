import type { PolymuxApi } from "@polymux/protocol";

declare global {
  interface Window {
    polymux: PolymuxApi;
  }
}

export {};
