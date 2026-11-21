// Context plugin: macOS device state via JXA (ported from cstack's context tool).
// Running applications and the frontmost app come from AppKit; the window list
// (exact IDs + bounds) comes from CoreGraphics. Prints one JSON object.
//
// The active window is resolved through System Events, which macOS gates behind a
// one-time Automation consent for System Events. Without that consent the window
// falls back to CGWindowList front-to-back order and a warning says so, rather
// than reporting a confident wrong answer. Accessibility (window titles,
// filtering) and Screen Recording are separate grants for the app running this.
ObjC.import("Foundation");
ObjC.import("AppKit");

ObjC.bindFunction("CGWindowListCopyWindowInfo", ["id", ["unsigned int", "unsigned int"]]);
ObjC.bindFunction("LSCopyDefaultHandlerForURLScheme", ["id", ["id"]]);

/** Always returns a string: a JXA nil reference unwraps to undefined, which
 *  would silently drop the key from JSON.stringify. */
function text(value) {
  if (!value) return "";
  try {
    const unwrapped = ObjC.unwrap(value);
    return unwrapped === null || unwrapped === undefined ? "" : String(unwrapped);
  } catch (_) {
    return "";
  }
}

const warnings = [];
const workspace = $.NSWorkspace.sharedWorkspace;
const front = workspace.frontmostApplication;
const frontPid = front ? Number(front.processIdentifier) : null;
const frontId = front ? text(front.bundleIdentifier) : "";

/* ── running applications ──────────────────────────────────────────────────── */
const running = workspace.runningApplications;
const byPid = {};
for (let i = 0; i < running.count; i++) {
  const app = running.objectAtIndex(i);
  if (Number(app.activationPolicy) !== 0) continue; // regular apps only
  const pid = Number(app.processIdentifier);
  const ident = text(app.bundleIdentifier);
  if (!ident) continue;
  const launched = app.launchDate;
  const instance = launched ? `${pid}:${Number(launched.timeIntervalSince1970)}` : `pid:${pid}`;
  byPid[pid] = {
    name: text(app.localizedName) || ident,
    pid,
    app_id: ident,
    instance_id: instance,
    path: app.executableURL ? text(app.executableURL.path) : "",
  };
}

/* ── window list ───────────────────────────────────────────────────────────── */
const raw = $.CGWindowListCopyWindowInfo(16, 0); // optionAll | excludeDesktopElements
const windows = [];
for (let i = 0; i < raw.count; i++) {
  const item = raw.objectAtIndex(i);
  const layer = item.objectForKey("kCGWindowLayer");
  if (!layer || Number(ObjC.unwrap(layer)) !== 0) continue;
  const pidRef = item.objectForKey("kCGWindowOwnerPID");
  const numberRef = item.objectForKey("kCGWindowNumber");
  if (!pidRef || !numberRef) continue;
  const boundsRef = item.objectForKey("kCGWindowBounds");
  const onscreenRef = item.objectForKey("kCGWindowIsOnscreen");
  windows.push({
    pid: Number(ObjC.unwrap(pidRef)),
    id: String(ObjC.unwrap(numberRef)),
    title: text(item.objectForKey("kCGWindowName")),
    onscreen: onscreenRef ? ObjC.unwrap(onscreenRef) === true : false,
    bounds: boundsRef
      ? {
          x: Number(ObjC.unwrap(boundsRef.objectForKey("X"))),
          y: Number(ObjC.unwrap(boundsRef.objectForKey("Y"))),
          width: Number(ObjC.unwrap(boundsRef.objectForKey("Width"))),
          height: Number(ObjC.unwrap(boundsRef.objectForKey("Height"))),
        }
      : null,
  });
}

/* ── surfaces ──────────────────────────────────────────────────────────────── */
// Every regular running application, including ones with no window: a browser
// with all its windows minimised still owns tabs worth reading.
const apps = {};
for (const pid of Object.keys(byPid)) {
  const app = byPid[pid];
  apps[`${app.app_id}:${app.instance_id}`] = app;
}

const surfaces = [];
for (const window of windows) {
  if (!window.bounds) continue;
  if (window.bounds.width < 80 || window.bounds.height < 60) continue;
  const app = byPid[window.pid];
  if (!app) continue; // owner is not a regular running application
  surfaces.push({
    kind: "window",
    window_id: window.id,
    title: window.title,
    onscreen: window.onscreen,
    bounds: window.bounds,
    app_id: app.app_id,
    pid: app.pid,
    instance_id: app.instance_id,
    path: app.path,
    identity: "exact",
    provider: "macos",
    human_active: "unknown",
  });
}

/** The authoritative front window, from System Events. The window list alone
 *  cannot identify it — several windows can share one frame — and JXA cannot
 *  safely hold raw Accessibility references, so ask the accessibility-backed
 *  System Events process instead. Returns null when Automation consent for
 *  System Events is missing or the frontmost process has no window. */
function frontWindowFromSystemEvents() {
  try {
    const system = Application("System Events");
    const front = system.processes.whose({ frontmost: true })();
    if (!front.length) return null;
    const windows = front[0].windows();
    if (!windows.length) return null;
    const window = windows[0]; // System Events lists windows front-to-back
    return { name: window.name(), position: window.position(), size: window.size() };
  } catch (_) {
    return null;
  }
}

/** Resolve the active window by identity: match the authoritative frame, prefer
 *  an exact title match, then fall back to the front-to-back window order. */
function resolveActiveWindow(frontPid, surfaces, windows, warnings) {
  const authoritative = frontWindowFromSystemEvents();
  if (authoritative && frontPid !== null) {
    const [x, y, width, height] = authoritative.position.concat(authoritative.size);
    const near = (s) =>
      s.bounds &&
      Math.abs(s.bounds.x - x) < 2 &&
      Math.abs(s.bounds.y - y) < 2 &&
      Math.abs(s.bounds.width - width) < 2 &&
      Math.abs(s.bounds.height - height) < 2;
    const candidates = surfaces.filter((s) => s.pid === frontPid && near(s));
    const named = candidates.filter((s) => s.title === authoritative.name);
    if (named.length === 1) return named[0].window_id;
    if (candidates.length === 1) return candidates[0].window_id;
    if (candidates.length > 1) warnings.push("front window frame matched several windows");
  } else if (frontPid !== null) {
    warnings.push("front window resolved from window order; System Events unavailable");
  }
  const first = windows.find((w) => w.pid === frontPid && w.onscreen && w.bounds);
  return first ? first.id : null;
}

const activeWindow = resolveActiveWindow(frontPid, surfaces, windows, warnings);
for (const surface of surfaces) {
  if (frontPid === null) surface.human_active = "unknown";
  else if (surface.pid !== frontPid) surface.human_active = false;
  else surface.human_active = activeWindow === null ? "unknown" : surface.window_id === activeWindow;
}

if (activeWindow === null) warnings.push("no frontmost window observed");

const handler = $.LSCopyDefaultHandlerForURLScheme($("http"));

JSON.stringify({
  device_id: text($.NSHost.currentHost.localizedName) || "local",
  platform: "macos",
  name: "local",
  kind: "local",
  user_active: true,
  completeness: warnings.length ? "partial" : "complete",
  capabilities: { apps: true, windows: true, focused_window: activeWindow !== null },
  observed_at: Date.now() / 1000,
  active: { app_id: frontId || null, pid: frontPid, window_id: activeWindow },
  default_browser: text(handler) || null,
  apps,
  surfaces,
  _warnings: warnings,
});
