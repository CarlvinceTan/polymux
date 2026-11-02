/** Decodes JSON as UTF-8 regardless of the response's declared charset; WebKit otherwise falls back to Latin-1. */
export async function decodeJSON<T>(response: Response): Promise<T> {
  return JSON.parse(new TextDecoder('utf-8').decode(await response.arrayBuffer())) as T;
}
