import type { Agent } from '../api/contract';
import type { ChatDraftMention } from '../api/models';

export interface TextRange { location: number; length: number }

function intersects(a: TextRange, b: TextRange): boolean {
  return Math.max(a.location, b.location) < Math.min(a.location + a.length, b.location + b.length);
}

function scalarBoundary(text: string, index: number): boolean {
  if (index <= 0 || index >= text.length) return true;
  const first = text.charCodeAt(index - 1), second = text.charCodeAt(index);
  return !(first >= 0xd800 && first <= 0xdbff && second >= 0xdc00 && second <= 0xdfff);
}

function word(value?: string): boolean { return !!value && /^[\p{L}\p{N}_-]$/u.test(value); }

export function validMention(token: ChatDraftMention, text: string): boolean {
  const { location, length } = token;
  const end = location + length;
  if (!Number.isInteger(location) || !Number.isInteger(length) || location < 0 || length < 0 || end > text.length
    || !scalarBoundary(text, location) || !scalarBoundary(text, end) || text.slice(location, end) !== token.text) return false;
  return !word(Array.from(text.slice(0, location)).at(-1)) && !word(Array.from(text.slice(end))[0]);
}

function nativeEdit(before: string, after: string, selection?: TextRange): { range: TextRange; inserted: number } | undefined {
  if (!selection) return;
  const delta = after.length - before.length;
  const candidates = selection.length === 0 && delta < 0
    ? [{ location: selection.location + delta, length: -delta }, { location: selection.location, length: -delta }]
    : [selection];
  const matches: { range: TextRange; inserted: number }[] = [];
  for (const range of candidates) {
    const inserted = delta + range.length;
    if (range.location < 0 || range.length < 0 || inserted < 0 || range.location + range.length > before.length
      || range.location + inserted > after.length || before.slice(0, range.location) !== after.slice(0, range.location)
      || before.slice(range.location + range.length) !== after.slice(range.location + inserted)) continue;
    if (!matches.some((item) => item.range.location === range.location && item.range.length === range.length)) matches.push({ range, inserted });
  }
  return matches.length === 1 ? matches[0] : undefined;
}

export function updateMentions(tokens: ChatDraftMention[], before: string, after: string, selection?: TextRange): ChatDraftMention[] {
  if (before === after) return tokens;
  const native = nativeEdit(before, after, selection);
  let changed: TextRange, inserted: number, ambiguous: TextRange | undefined;
  if (native) { changed = native.range; inserted = native.inserted; }
  else {
    const shared = Math.min(before.length, after.length);
    let prefix = 0, suffix = 0;
    while (prefix < shared && before.charCodeAt(prefix) === after.charCodeAt(prefix)) prefix++;
    while (suffix < shared && before.charCodeAt(before.length - suffix - 1) === after.charCodeAt(after.length - suffix - 1)) suffix++;
    const bounded = Math.min(suffix, shared - prefix);
    changed = { location: prefix, length: before.length - prefix - bounded };
    inserted = after.length - prefix - bounded;
    const overlap = Math.max(0, prefix + suffix - shared);
    if (overlap) ambiguous = { location: prefix - overlap, length: changed.length + overlap };
  }
  const shift = inserted - changed.length;
  return tokens.flatMap((token) => {
    if ((ambiguous && intersects(token, ambiguous)) || intersects(token, changed)
      || (changed.length === 0 && inserted > 0 && changed.location > token.location && changed.location < token.location + token.length)) return [];
    const next = { ...token, location: changed.location + changed.length <= token.location ? token.location + shift : token.location };
    return validMention(next, after) ? [next] : [];
  });
}

export function mentionTargets(tokens: ChatDraftMention[], text: string, members: Set<string>): string[] | undefined {
  const ids: string[] = [];
  for (const token of tokens) {
    if (!validMention(token, text)) continue;
    if (!members.has(token.id)) return;
    if (!ids.includes(token.id)) ids.push(token.id);
  }
  return ids;
}

export function mentionQuery(text: string, tokens: ChatDraftMention[], memberCount: number): { start: number; term: string } | undefined {
  if (memberCount <= 1) return;
  const start = text.lastIndexOf('@');
  if (start < 0 || (start > 0 && !/[\s([{]/u.test(text[start - 1]))) return;
  if (tokens.some((token) => token.location === start && /\s/u.test(text[token.location + token.length] ?? ''))) return;
  const term = text.slice(start + 1);
  if (term.includes('\n') || [...new Intl.Segmenter(undefined, { granularity: 'grapheme' }).segment(term)].length > 40) return;
  return { start, term };
}

function fold(value: string): string { return value.normalize('NFKD').replace(/\p{M}/gu, '').toLocaleLowerCase(); }

export function mentionChoices(members: Agent[], term: string): Agent[] {
  const everyone: Agent = { id: 'everyone', name: 'everyone', role: 'All members', model: '', active: false };
  const query = fold(term);
  return [everyone, ...members].map((agent, index) => {
    const name = fold(agent.name);
    const rank = !query || name === query ? 0 : name.startsWith(query) ? 1 : name.includes(query) ? 2 : 3;
    return { agent, index, rank };
  }).filter((item) => item.rank < 3).sort((a, b) => a.rank - b.rank || a.index - b.index).map((item) => item.agent);
}
