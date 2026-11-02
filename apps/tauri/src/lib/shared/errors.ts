/**
 * Turns whatever a failed call threw into a line a person can act on.
 *
 * Electron wraps every rejected `ipcRenderer.invoke` in its own prose — "Error
 * invoking remote method 'polymux:models:assign-role': Error: …" — which puts a
 * channel name and two "Error" labels in front of the only sentence that
 * matters. Stacks arrive in the same string. Both are for a developer reading a
 * console, not for someone reading a settings pane, so neither is shown.
 */
const IPC_WRAPPER = /^Error invoking remote method '[^']*':\s*/;
/** Repeated `Error:` / `TypeError:` labels the wrapper leaves behind. */
const ERROR_LABEL = /^(?:[A-Z][A-Za-z]*Error|Error):\s*/;
const STACK_FRAME = /\n\s*at\s.*/s;
const FALLBACK = 'Something went wrong. Please try again.';

export function readableError(reason: unknown): string {
  const raw = reason instanceof Error ? reason.message : String(reason ?? '');
  let message = raw.replace(IPC_WRAPPER, '').replace(STACK_FRAME, '').trim();
  // Nested labels stack up ("Error: Error: …"), so peel until none is left.
  while (ERROR_LABEL.test(message)) message = message.replace(ERROR_LABEL, '').trim();
  if (!message) return FALLBACK;
  // A bare error code says nothing on its own; anything that reads as a
  // sentence is kept exactly as the thrower wrote it.
  if (
    /^[A-Z][A-Z0-9_]+$/.test(message) ||
    /^[a-z][a-z0-9]*(?:_[a-z0-9]+)+$/.test(message)
  )
    return FALLBACK;
  return message;
}
