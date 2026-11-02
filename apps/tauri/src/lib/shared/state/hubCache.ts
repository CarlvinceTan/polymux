/**
 * A one-line signal that the hub's cached copy is no longer about the same
 * accounts.
 *
 * Unlinking an account happens in Settings, while the copy of what that
 * account said — the chat list and every page of every conversation — is held
 * by the workspace's hub, outside its component so a return paints instantly.
 * Nothing connected the two, so after signing out of one WhatsApp account and
 * into another the hub still drew the first account's conversations, and each
 * one still opened on its messages.
 *
 * A signal rather than an import: the two live in different features, and the
 * hub is the only thing that knows how its own cache is shaped.
 */
const listeners = new Set<() => void>();

/** Called by whatever removes an account. Safe when no hub is mounted — the
 * subscriber list is simply empty, and the main process has cleared the disk
 * copy the next mount would have seeded from. */
export function invalidateHubCache(): void {
  for (const listener of [...listeners]) listener();
}

/** Registers a hub's own reset. Returns the unsubscribe for `onDestroy`. */
export function onHubCacheInvalidated(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
