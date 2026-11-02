/** Port of PolymuxKit's SSEParser: line-buffered `data:` accumulation into JSON values. */
export class SSEParser<T> {
  private line: number[] = [];
  private readonly decoder = new TextDecoder();
  private payload: string[] = [];

  feed(byte: number): T | undefined {
    if (byte === 10) return this.completeLine();
    this.line.push(byte);
    return undefined;
  }

  finish(): T | undefined {
    if (this.line.length) this.completeLine();
    return this.completeEvent();
  }

  private completeLine(): T | undefined {
    if (this.line.at(-1) === 13) this.line.pop();
    const text = this.decoder.decode(Uint8Array.from(this.line));
    this.line = [];
    if (!text) return this.completeEvent();
    if (text.startsWith('data:')) {
      let value = text.slice(5);
      if (value.startsWith(' ')) value = value.slice(1);
      this.payload.push(value);
    }
    return undefined;
  }

  private completeEvent(): T | undefined {
    if (!this.payload.length) return undefined;
    const data = this.payload.join('\n');
    this.payload = [];
    if (!data) return undefined;
    return JSON.parse(data) as T;
  }
}

/** Decodes a `text/event-stream` response body, yielding one parsed event per `data:` block. */
export async function* decodeSSE<T>(body: ReadableStream<Uint8Array>, signal?: AbortSignal, idleMs?: number): AsyncGenerator<T> {
  const parser = new SSEParser<T>();
  const reader = body.getReader();
  let complete = false;
  try {
    while (true) {
      const { done, value } = await readChunk(reader, signal, idleMs);
      if (done) { complete = true; break; }
      for (const byte of value) {
        const event = parser.feed(byte);
        if (event !== undefined) yield event;
      }
    }
    const last = parser.finish();
    if (last !== undefined) yield last;
  } finally {
    if (!complete) await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}

async function readChunk(reader: ReadableStreamDefaultReader<Uint8Array>, signal?: AbortSignal, idleMs?: number): Promise<ReadableStreamReadResult<Uint8Array>> {
  if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
  let timer: ReturnType<typeof setTimeout> | undefined;
  let abort: (() => void) | undefined;
  try {
    return await Promise.race([
      reader.read(),
      new Promise<never>((_, reject) => {
        abort = () => reject(new DOMException('Aborted', 'AbortError'));
        signal?.addEventListener('abort', abort, { once: true });
        if (idleMs !== undefined) timer = setTimeout(() => reject(new DOMException('Stream timed out', 'TimeoutError')), idleMs);
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
    if (abort) signal?.removeEventListener('abort', abort);
  }
}
