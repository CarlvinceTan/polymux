/** A server path is a directory even when the supplied address has no trailing slash. */
export function serverBase(value: string | URL): URL {
  const url = new URL(value);
  if (!url.pathname.endsWith('/')) url.pathname += '/';
  return url;
}
