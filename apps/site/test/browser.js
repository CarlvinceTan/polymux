export function browserGlobals(window) {
  const globals = new Map();
  window.HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '');
    this.querySelector('button')?.focus();
  };
  window.HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open');
    this.dispatchEvent(new window.Event('close'));
  };
  for (const key of ['window', 'document', 'location', 'navigator', 'Node', 'Element', 'HTMLElement', 'HTMLMediaElement', 'HTMLDialogElement', 'SVGElement', 'Text', 'Comment', 'Event', 'MouseEvent', 'KeyboardEvent', 'CustomEvent', 'MutationObserver', 'getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame']) {
    globals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, {configurable: true, writable: true, value: key === 'window' ? window : window[key]});
  }
  return () => {
    window.close();
    for (const [key, descriptor] of globals) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else delete globalThis[key];
    }
  };
}
