type Range = { location: number; length: number };
const boundary = (text: string, index: number) => index <= 0 || index >= text.length
  || !(text.charCodeAt(index - 1) >= 0xd800 && text.charCodeAt(index - 1) <= 0xdbff
    && text.charCodeAt(index) >= 0xdc00 && text.charCodeAt(index) <= 0xdfff);

/** Tracks the currently revisable speech segment in a UTF-16 draft. */
export class DictationDraft {
  partial?: Range;
  private segment?: number;
  private committed = new Set<number>();

  stop() {
    this.partial = undefined;
    this.segment = undefined;
    this.committed.clear();
  }

  update(transcript: string, start: number, final: boolean, text: string, selection: Range) {
    if (this.committed.has(start) || (!transcript && (this.segment !== start || !this.partial))) return;
    if (this.segment !== start) this.partial = undefined;
    const range = this.partial ?? selection;
    if (range.location < 0 || range.length < 0 || range.location + range.length > text.length
      || !boundary(text, range.location) || !boundary(text, range.location + range.length)) return;
    const updated = text.slice(0, range.location) + transcript + text.slice(range.location + range.length);
    const inserted = { location: range.location, length: transcript.length };
    this.partial = final ? undefined : inserted;
    this.segment = start;
    if (final) this.committed.add(start);
    return { text: updated, cursor: inserted.location + inserted.length, range };
  }

  edit(old: string, next: string) {
    const range = this.partial;
    if (!range || old === next) return;
    let prefix = 0;
    while (prefix < Math.min(old.length, next.length) && old[prefix] === next[prefix]) prefix++;
    let suffix = 0;
    while (suffix < Math.min(old.length, next.length) - prefix
      && old[old.length - suffix - 1] === next[next.length - suffix - 1]) suffix++;
    if (old.length - suffix <= range.location) {
      this.partial = { location: range.location + next.length - old.length, length: range.length };
    } else if (prefix < range.location + range.length) {
      if (this.segment !== undefined) this.committed.add(this.segment);
      this.partial = undefined;
    }
  }
}
