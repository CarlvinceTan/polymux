export type ChatSearchSnippet = {
  messageId: string;
  before: string;
  match: string;
  after: string;
};

const EXCERPT_MAX = 72;

/** Up to three matching messages, oldest first, with stable navigation targets. */
export function chatSearchSnippets(
  messages: readonly {id: string; text: string}[] | undefined,
  query: string,
): ChatSearchSnippet[] {
  const needle = query.trim();
  if (!needle || !messages?.length) return [];

  const matches: ChatSearchSnippet[] = [];
  for (const message of messages) {
    const excerpt = excerptAroundMatch(message.text, needle);
    if (excerpt) matches.push({messageId: message.id, ...excerpt});
    if (matches.length === 3) break;
  }
  return matches;
}

function excerptAroundMatch(source: string, needle: string): Omit<ChatSearchSnippet, 'messageId'> | null {
  const text = source.replace(/\s+/g, ' ').trim();
  if (!text) return null;
  const index = text.toLowerCase().indexOf(needle.toLowerCase());
  if (index < 0) return null;
  const matchEnd = index + needle.length;
  const match = text.slice(index, matchEnd);
  if (text.length <= EXCERPT_MAX) {
    return {before: text.slice(0, index), match, after: text.slice(matchEnd)};
  }

  const budget = Math.max(0, EXCERPT_MAX - needle.length);
  let left = Math.min(index, Math.ceil(budget / 2));
  let right = Math.min(text.length - matchEnd, budget - left);
  left = Math.min(index, budget - right);
  let start = index - left;
  let end = matchEnd + right;
  if (start > 0) {
    const nextSpace = text.indexOf(' ', start);
    if (nextSpace > start && nextSpace < index) start = nextSpace + 1;
  }
  if (end < text.length) {
    const prevSpace = text.lastIndexOf(' ', end);
    if (prevSpace > matchEnd) end = prevSpace;
  }

  const before = `${start > 0 ? '…' : ''}${text.slice(start, index)}`;
  const after = `${text.slice(matchEnd, end)}${end < text.length ? '…' : ''}`;
  return {before, match, after};
}
