import type { Message, Run, WireError } from './api/contract';

function newer(run: Run, date: string): boolean {
  const next = Date.parse(run.createdAt);
  const previous = Date.parse(date);
  return Number.isFinite(next) && Number.isFinite(previous) && next > previous;
}

/** Matches ActivityStore's current, unsuperseded assistant failure. */
export function replyIssue(message: Message, runs: Run[]): WireError | undefined {
  if (message.role !== 'assistant' || message.status !== 'error') return;
  const run = runs.find((item) => item.id === message.runID);
  const agent = run?.agentID ?? message.agentID;
  if (!agent) return message.error;
  if (!run) {
    return runs.some((item) => item.agentID === agent && newer(item, message.createdAt)) ? undefined : message.error;
  }
  if (!['failed', 'interrupted'].includes(run.status)) return;
  if (runs.some((item) => item.agentID === agent && newer(item, run.createdAt))) return;
  const scoped = runs.filter((item) => item.conversationID === message.threadID && item.agentID === agent);
  const ordered = (a: Run, b: Run) => {
    const first = Date.parse(a.createdAt) || 0;
    const second = Date.parse(b.createdAt) || 0;
    return first === second ? a.seq - b.seq : first - second;
  };
  const waiting = scoped.filter((item) => item.status === 'waiting_input' && !scoped.some((next) =>
    newer(next, item.createdAt) && !['failed', 'interrupted'].includes(next.status)));
  const running = scoped.filter((item) => ['running', 'resuming'].includes(item.status));
  const peer = scoped.filter((item) => item.status === 'waiting_peer');
  const candidates = waiting.length ? waiting : running.length ? running : peer.length ? peer : scoped;
  if (candidates.sort(ordered).at(-1)?.id !== run.id) return;
  return run.error ?? message.error;
}
