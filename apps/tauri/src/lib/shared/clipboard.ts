/**
 * `navigator.clipboard` is the good path, but it rejects whenever the document
 * is not focused or the async API is unavailable, which is exactly what happens
 * when a click lands while another surface (a webview, the workspace pane) holds
 * focus. A failed copy used to be swallowed, so the button claimed "Copied" with
 * an empty clipboard. Every copy goes through here instead: the async API first,
 * then a selection-based fallback, and the caller learns whether it worked.
 */
export async function copyText(text: string): Promise<boolean> {
  if (!text) return false;
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return copyViaSelection(text);
  }
}

function copyViaSelection(text: string): boolean {
  const holder = document.createElement('textarea');
  holder.value = text;
  // Off-screen rather than hidden: `display: none` and `visibility: hidden`
  // elements cannot hold a selection, so the copy would silently do nothing.
  holder.setAttribute('readonly', '');
  holder.style.cssText = 'position:fixed;top:0;left:-9999px;opacity:0';
  document.body.appendChild(holder);
  try {
    holder.select();
    holder.setSelectionRange(0, text.length);
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    holder.remove();
  }
}
