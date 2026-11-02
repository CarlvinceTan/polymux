export type RunsByConversation = Record<string, string[]>;
export type LiveAssistantByRun = Record<string, string>;

export function latestConversationRun(runs: RunsByConversation, conversationId: string): string | undefined {
  return runs[conversationId]?.at(-1);
}

export function addConversationRun(runs: RunsByConversation, conversationId: string, runId: string): RunsByConversation {
  const current = runs[conversationId] ?? [];
  return current.includes(runId) ? runs : {...runs, [conversationId]: [...current, runId]};
}

export function removeConversationRun(runs: RunsByConversation, conversationId: string, runId: string): RunsByConversation {
  const remaining = (runs[conversationId] ?? []).filter((candidate) => candidate !== runId);
  if (remaining.length) return {...runs, [conversationId]: remaining};
  const next = {...runs};
  delete next[conversationId];
  return next;
}

export function bindPendingRun(
  runs: RunsByConversation,
  assistants: LiveAssistantByRun,
  conversationId: string,
  pendingRunId: string,
  runId: string,
): {runs: RunsByConversation; assistants: LiveAssistantByRun} {
  const assistantId = assistants[pendingRunId];
  const nextAssistants = {...assistants};
  delete nextAssistants[pendingRunId];
  if (assistantId) nextAssistants[runId] = assistantId;
  const current = runs[conversationId] ?? [];
  return {
    assistants: nextAssistants,
    runs: {...runs, [conversationId]: [...new Set(current.map((candidate) => candidate === pendingRunId ? runId : candidate))]},
  };
}
