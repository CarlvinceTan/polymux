export interface LiveTurnState {
  runId: string | undefined;
  immediate: boolean;
  hasActiveDelegation: boolean;
}

/**
 * Keep the main agent conversational while its delegated work continues.
 * A run whose id is still pending cannot receive steering yet; an ordinary
 * non-delegating turn keeps the explicit queue semantics.
 */
export function shouldSteerLiveTurn(state: LiveTurnState): boolean {
  return Boolean(
    state.runId
    && !state.runId.startsWith('pending:')
    && (state.immediate || state.hasActiveDelegation),
  );
}

export type NaturalQueuePriority = 'background' | 'normal' | 'urgent' | 'attention';

/** Conservative, user-authored priority cues only. Subject matter such as
 * "today" or "latest" is not urgency by itself. */
export function inferQueuePriority(text: string): NaturalQueuePriority {
  const normalized = text.trim().toLowerCase().replace(/\s+/g, ' ');
  if (!normalized) return 'normal';
  if (/\b(stop what you(?:'re| are) doing|do this first|handle this first|answer this first|this is more urgent|highest priority|while (?:that|this) runs)\b/.test(normalized))
    return 'attention';
  if (/\b(urgent|urgently|asap|as soon as possible|right away|immediately)\b/.test(normalized))
    return 'urgent';
  if (/\b(when you have time|when you get a chance|no rush|low priority|in the background)\b/.test(normalized))
    return 'background';
  return 'normal';
}

export function isDependentFollowUp(text: string): boolean {
  const normalized = text.trim().toLowerCase().replace(/\s+/g, ' ');
  return /^(after that|afterwards|once (?:that|this|it)(?:'s| is) done|when (?:that|this|it)(?:'s| is) done|then\b)/.test(normalized)
    || /\b(use (?:those|the) results|based on (?:that|those|the results))\b/.test(normalized);
}
