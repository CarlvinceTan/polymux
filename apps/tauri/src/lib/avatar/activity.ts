import type { Message, Run } from '../api/contract';

/** The current run follows ActivityStore's per-conversation, per-bot precedence. */
export function thinking(runs: Run[], messages: Message[], conversationID: string, agentID: string): boolean {
  const scoped = runs.filter((run) => run.conversationID === conversationID && run.agentID === agentID);
  const newer = (a: Run, b: Run) => Date.parse(a.createdAt) > Date.parse(b.createdAt);
  const latest = (values: Run[]) => values.sort((a, b) =>
    (Date.parse(b.createdAt) || 0) - (Date.parse(a.createdAt) || 0) || b.seq - a.seq)[0];
  const waiting = scoped.filter((run) => run.status === 'waiting_input' && !scoped.some((other) =>
    newer(other, run) && other.status !== 'failed' && other.status !== 'interrupted'));
  const current = latest(waiting) ?? latest(scoped.filter((run) => run.status === 'running' || run.status === 'resuming'))
    ?? latest(scoped.filter((run) => run.status === 'waiting_peer')) ?? latest(scoped);
  if (!current || !['running', 'resuming'].includes(current.status)) return false;
  if (['tool', 'rendering', 'handoff'].includes(current.activity)) return false;
  if (current.trigger === 'created' && current.status === 'running' && !current.tools?.length
      && ['preparing', 'generating'].includes(current.activity)) return false;
  return !messages.some((message) => message.runID === current.id && message.status === 'streaming'
    && message.parts.some((part) => !!part.text));
}
