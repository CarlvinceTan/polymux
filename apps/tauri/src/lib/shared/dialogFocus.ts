const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function focusableElements(dialog: HTMLElement): HTMLElement[] {
  return Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (element) => !element.closest('[hidden], [aria-hidden="true"]') && element.getClientRects().length > 0,
  );
}

export function activateModalDialog(dialog: HTMLElement): () => void {
  const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  dialog.focus({preventScroll: true});

  return () => {
    // A newer dialog can already have opened on top while this one is being
    // torn down. Handing focus back then would steal it from the new dialog,
    // so only restore when nothing else has claimed focus.
    const active = document.activeElement;
    const claimed = active instanceof HTMLElement && active !== document.body && !dialog.contains(active);
    if (!claimed && opener?.isConnected) opener.focus({preventScroll: true});
  };
}

export function trapModalFocus(event: KeyboardEvent, dialog: HTMLElement): void {
  if (event.key !== 'Tab') return;

  const elements = focusableElements(dialog);
  if (elements.length === 0) {
    event.preventDefault();
    dialog.focus({preventScroll: true});
    return;
  }

  const first = elements[0];
  const last = elements[elements.length - 1];
  const active = document.activeElement;

  if (active === dialog || !(active instanceof Node) || !dialog.contains(active)) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
    return;
  }

  if (event.shiftKey && active === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && active === last) {
    event.preventDefault();
    first.focus();
  }
}
