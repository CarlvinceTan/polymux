const preference = typeof window === 'undefined' ? undefined : window.matchMedia('(prefers-color-scheme: dark)');
let selected = 'system';
try { selected = localStorage.getItem('polymux.appearance') ?? 'system'; } catch { /* Keep the default. */ }

function render(dark: boolean) {
  const mode = dark ? 'dark' : 'light';
  if (document.documentElement.dataset.mode !== mode) document.documentElement.dataset.mode = mode;
}

preference?.addEventListener('change', (event) => {
  if (selected !== 'light' && selected !== 'dark') render(event.matches);
});

/** Applies the server's appearance setting to the page, as the reference app does. */
export function applyAppearance(value: string, store = true): void {
  selected = value;
  render(value === 'dark' || (value !== 'light' && (preference?.matches ?? false)));
  if (store) {
    try { localStorage.setItem('polymux.appearance', value); } catch { /* Keep the choice for this session. */ }
  }
}
