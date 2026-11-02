import type { Notice } from './api/contract';

export function shouldNotify(
  notice: Notice,
  options: { muted: ReadonlySet<string>; visibleThread?: string; now?: number },
): boolean {
  if (notice.conversationID === options.visibleThread) return false;
  if (notice.agentID && options.muted.has(notice.agentID)) return false;
  const created = Date.parse(notice.createdAt);
  return Number.isFinite(created) && Math.abs((options.now ?? Date.now()) - created) <= 600_000;
}
