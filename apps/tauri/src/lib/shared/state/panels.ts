export type PanelMode = 'none' | 'summary' | 'workspace';

export interface PanelState {
  mode: PanelMode;
  /** Whether Summary was open when Workspace took the space, so it can return. */
  summaryReturns: boolean;
}

export const initialPanelState: PanelState = {mode: 'none', summaryReturns: false};

/**
 * Summary and Workspace stay mutually exclusive, but opening Workspace only
 * borrows the space: closing it again restores Summary to whatever it was.
 */
export function togglePanelState(state: PanelState, requested: Exclude<PanelMode, 'none'>): PanelState {
  if (requested === 'workspace') {
    if (state.mode === 'workspace') {
      return {mode: state.summaryReturns ? 'summary' : 'none', summaryReturns: false};
    }
    return {mode: 'workspace', summaryReturns: state.mode === 'summary'};
  }

  // Toggling Summary directly replaces anything remembered for it.
  return {mode: state.mode === 'summary' ? 'none' : 'summary', summaryReturns: false};
}

export interface ConversationPanelContext {
  /** Summary has nothing to show until the conversation has started. */
  hasMessages: boolean;
  /** The user closed Summary for this conversation, so leave it closed. */
  summaryDismissed: boolean;
  /** Whether the viewport is wide enough to show a panel beside the conversation. */
  splitLayout: boolean;
}

/**
 * Summary is the conversation's default companion rather than something to open
 * each time: it appears on its own as soon as a thread has messages. Closing it
 * is remembered for that conversation so it cannot reopen behind the user, and
 * starting or reopening a conversation begins from the default again. On a
 * viewport too narrow to place a panel beside the conversation it stays closed,
 * because there it would cover the messages rather than sit next to them.
 */
export function conversationPanelState(state: PanelState, context: ConversationPanelContext): PanelState {
  if (!context.hasMessages || context.summaryDismissed || !context.splitLayout) return state;
  // Anything the user has open already wins; the default only fills empty space.
  if (state.mode !== 'none') return state;
  return {mode: 'summary', summaryReturns: false};
}

/** Whether a Summary toggle just closed it, which is what makes the choice stick. */
export function summaryWasDismissed(previous: PanelState, next: PanelState): boolean {
  return previous.mode === 'summary' && next.mode !== 'summary';
}
