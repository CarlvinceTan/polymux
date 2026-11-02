/** Copies of copies share one numbered title family. */
export function conversationCopyTitle(title: string, titles: string[]): string {
  const base = title.replace(/ \(\d+\)$/, '');
  let highest = 0;
  for (const existing of titles) {
    const match = /^(.*) \((\d+)\)$/.exec(existing);
    if (match?.[1] === base) highest = Math.max(highest, Number(match[2]));
  }
  return `${base} (${highest + 1})`;
}
