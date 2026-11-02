// The conversation never compresses below a readable measure, so both side
// surfaces resize against this floor rather than against the viewport alone.
export const MIN_MAIN_PANE_WIDTH = 432;
// Assistant/Team, their divider, and the two heading actions must all retain
// their intrinsic width. The former 183px floor predated that header and let
// its labels paint through the action buttons when the drawer was narrowed.
export const MIN_CHAT_DRAWER_WIDTH = 240;
export const MAX_CHAT_DRAWER_WIDTH = 480;
// Keeps split workspace views, especially Hub's rail and reader, from being
// compressed into two impractically narrow columns.
// Hub's compact mail toolbar fits all ten possible actions at this floor.
export const MIN_WORKSPACE_WIDTH = 480;
export const MAX_WORKSPACE_WIDTH = 720;
export const SPLIT_LAYOUT_MIN_WIDTH = MIN_CHAT_DRAWER_WIDTH + MIN_MAIN_PANE_WIDTH + MIN_WORKSPACE_WIDTH + 1;
// Space reserved for the Summary card and its horizontal insets. Keep the
// matching fixed sizes in style.css in step with this value.
export const SUMMARY_RESERVED_COLUMN = 337;

export type ResizeBounds = {min: number; max: number};

function bounds(viewportWidth: number, reservedWidth: number, minimum: number, maximum: number): ResizeBounds {
  return {
    min: minimum,
    max: Math.max(minimum, Math.min(maximum, viewportWidth - reservedWidth - MIN_MAIN_PANE_WIDTH)),
  };
}

export function chatDrawerResizeBounds(viewportWidth: number, workspaceWidth: number): ResizeBounds {
  return bounds(viewportWidth, workspaceWidth, MIN_CHAT_DRAWER_WIDTH, MAX_CHAT_DRAWER_WIDTH);
}

export function workspaceResizeBounds(viewportWidth: number, chatDrawerWidth: number): ResizeBounds {
  return bounds(viewportWidth, chatDrawerWidth, MIN_WORKSPACE_WIDTH, MAX_WORKSPACE_WIDTH);
}

export function clampPanelWidth(width: number, resizeBounds: ResizeBounds): number {
  return Math.round(Math.max(resizeBounds.min, Math.min(resizeBounds.max, width)));
}

/**
 * Dragging the chat divider past its max is a no-op while the workspace is
 * docked. Once the workspace is expanded, the same overshoot into the right
 * half of the window is a minimise gesture, and the chat drawer returns to
 * its default width rather than staying pinned at the ceiling.
 */
export function chatDrawerOvershootMinimisesWorkspace(
  clientX: number,
  viewportWidth: number,
  workspaceExpanded: boolean,
  reservedWidth: number,
): boolean {
  if (!workspaceExpanded) return false;
  const {max} = chatDrawerResizeBounds(viewportWidth, reservedWidth);
  return clientX >= max && clientX >= viewportWidth / 2;
}

/** Horizontal centre of the conversation column while both side surfaces are docked. */
export function dockedMainMidX(
  viewportWidth: number,
  leftColumn: number,
  rightColumn: number,
): number {
  return leftColumn + Math.max(0, viewportWidth - leftColumn - rightColumn) / 2;
}

/**
 * Dragging the workspace divider past its max leaves the docked edge on the
 * ceiling. The pointer can keep travelling; once it crosses the left half of
 * the main pane the workspace expands. `mainMidX` is that pane's current
 * centre; omit it to fall back to the window midpoint.
 */
export function workspaceOvershootExpands(
  clientX: number,
  viewportWidth: number,
  workspaceExpanded: boolean,
  reservedWidth: number,
  mainMidX = viewportWidth / 2,
): boolean {
  if (workspaceExpanded) return false;
  const {max} = workspaceResizeBounds(viewportWidth, reservedWidth);
  return viewportWidth - clientX > max && clientX <= mainMidX;
}

export interface PanelLayoutRequest {
  viewportWidth: number;
  chatDrawerOpen: boolean;
  workspaceOpen: boolean;
  chatDrawerWidth: number;
  workspaceWidth: number;
  /** The drawer whose width reflects the most recent user intent. */
  priority: "chatDrawer" | "workspace";
}

export interface PanelLayout {
  chatDrawerWidth: number;
  workspaceWidth: number;
}

/**
 * Resolves both drawer widths against the conversation floor. Each drawer may
 * grow into the other's space — its bounds reserve only the other's minimum —
 * and any overflow is settled by shrinking the drawer the user touched least
 * recently down to its own minimum before the prioritised one gives way. The
 * conversation column therefore never drops below MIN_MAIN_PANE_WIDTH, and no
 * open drawer is ever pushed below its own minimum.
 */
export function resolvePanelWidths(request: PanelLayoutRequest): PanelLayout {
  const {viewportWidth} = request;
  let chats = request.chatDrawerOpen
    ? clampPanelWidth(request.chatDrawerWidth, chatDrawerResizeBounds(viewportWidth, request.workspaceOpen ? MIN_WORKSPACE_WIDTH : 0))
    : request.chatDrawerWidth;
  let workspace = request.workspaceOpen
    ? clampPanelWidth(request.workspaceWidth, workspaceResizeBounds(viewportWidth, request.chatDrawerOpen ? MIN_CHAT_DRAWER_WIDTH : 0))
    : request.workspaceWidth;
  if (request.chatDrawerOpen && request.workspaceOpen) {
    const overflow = () => chats + workspace + MIN_MAIN_PANE_WIDTH - viewportWidth;
    if (overflow() > 0) {
      if (request.priority === "chatDrawer") workspace = Math.max(MIN_WORKSPACE_WIDTH, workspace - overflow());
      else chats = Math.max(MIN_CHAT_DRAWER_WIDTH, chats - overflow());
    }
    if (overflow() > 0) {
      if (request.priority === "chatDrawer") chats = Math.max(MIN_CHAT_DRAWER_WIDTH, chats - overflow());
      else workspace = Math.max(MIN_WORKSPACE_WIDTH, workspace - overflow());
    }
  }
  return {chatDrawerWidth: Math.round(chats), workspaceWidth: Math.round(workspace)};
}
