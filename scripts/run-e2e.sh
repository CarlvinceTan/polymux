#!/bin/sh
# Run UI flows against a throwaway instance. Usage: scripts/run-e2e.sh [--verify-server] [flow.js ...]
# A "// @relaunch" line quits and reopens the app midway; "// @crash" kills it instead.
# Flows can call pick(app, item[, trigger]) to choose a menu item (trigger, a query or an opener function, defaults to New chat and reopens a menu that closed early); it returns a fresh handle once the menu has closed.
# The app stays in the background; native focus samples fail the flow if its test app takes focus.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
OUT=$(mktemp -t polymux-e2e)
PID=
START=$(date +%s)

cleanup() {
  [ -z "$PID" ] || { kill "$PID" 2>/dev/null && wait "$PID" 2>/dev/null || true; }
  rm -f "$OUT"
}
trap cleanup EXIT
trap 'exit 1' INT TERM HUP

VERIFY_SERVER=0
while [ $# -gt 0 ]; do
  case "$1" in
    --verify-server) VERIFY_SERVER=1; shift ;;
    *) break ;;
  esac
done
if [ $# -eq 0 ]; then set -- "$ROOT"/e2e/tauri/*.js; fi
for flow in "$@"; do
  [ -f "$flow" ] || { echo "Missing flow: $flow" >&2; exit 2; }
done
if [ "$VERIFY_SERVER" = 1 ]; then
  "$ROOT/scripts/run.sh" --verify-server >"$OUT" 2>&1 &
else
  "$ROOT/scripts/run.sh" >"$OUT" 2>&1 &
fi
PID=$!
until grep -q "Press Ctrl-C" "$OUT"; do
  kill -0 "$PID" 2>/dev/null || { cat "$OUT" >&2; echo "The test instance did not start" >&2; exit 1; }
  sleep 0.2
done
NAME=$(sed -n 's/^\(Polymux [0-9]*\) running.*/\1/p' "$OUT")
ADDRESS=$(sed -n 's/^  server: \([^ ]*\).*/\1/p' "$OUT")
STATE=$(sed -n 's/^  data: *\(.*\)\/data$/\1/p' "$OUT")

failed=0
for flow in "$@"; do
  if POLYMUX_ROOT="$ROOT" POLYMUX_APP="$NAME" POLYMUX_FLOW="$flow" POLYMUX_STATE="$STATE" POLYMUX_ADDRESS="$ADDRESS" \
    python3 - <<'PY'
import atexit, json, os, re, signal, subprocess, sys, time
started = time.monotonic()
flow, app = os.environ["POLYMUX_FLOW"], os.environ["POLYMUX_APP"]
state = os.environ["POLYMUX_STATE"]
name = os.path.basename(flow)
session = "polymux-e2e-%s" % os.getpid()
ui_time = 0


def driver(tool, args):
    run = subprocess.run(["cua-driver", tool, json.dumps(dict(args, session=session))], capture_output=True, text=True)
    try:
        out, _ = json.JSONDecoder().raw_decode(run.stdout[run.stdout.index("{"):])
    except ValueError:
        out = {"ok": False, "error": (run.stdout + run.stderr)[:600]}
    return run.returncode == 0 and out.get("ok", True), out


atexit.register(driver, "end_session", {})
ok, out = driver("start_session", {"cursor_motion": {
    "timing": "fixed", "glide_duration_ms": 1, "dwell_after_click_ms": 0,
}})
if not ok:
    raise SystemExit("couldn't configure the E2E cursor: " + json.dumps(out))

def fail(out):
    print("  FAIL %s (%.1fs)" % (name, time.monotonic() - started))
    print(json.dumps({k: v for k, v in out.items() if k in ("error", "console", "value")}, indent=2)[:1500])
    sys.exit(1)

# Launches the bundle in the background, as run.sh does, and returns its pid.
launched = set()
def launch(bundle, data, log, args):
    environment = ["--env", "POLYMUX_TEST_ADDRESS=" + os.environ["POLYMUX_ADDRESS"]]
    if window_size:
        environment += ["--env", "POLYMUX_TEST_WINDOW_SIZE=" + window_size]
    subprocess.run(["open", "-g", "-n", "--env", "POLYMUX_DATA_DIR=" + data, "--stdout", log, "--stderr", log,
                    *environment, bundle, "--args"] + args, check=True)
    for _ in range(100):
        found = subprocess.run(["pgrep", "-nf", bundle + "/Contents/MacOS/polymux-tauri"], capture_output=True, text=True).stdout.strip()
        if found:
            launched.add(found)
            with open(state + "/app.pid", "w") as file:
                file.write(found)
            matches = subprocess.run(["pgrep", "-f", bundle + "/Contents/MacOS/polymux-tauri"], capture_output=True, text=True).stdout.split()
            if matches != [found]:
                raise SystemExit("multiple processes use this test bundle; exact background reopen is unproven")
            # A background launch can start without opening its initial window.
            for attempt in range(10):
                reopened = subprocess.run(["open", "-g", bundle], capture_output=True)
                if reopened.returncode == 0:
                    return int(found)
                time.sleep(0.1)
            raise SystemExit("the owned app could not open its initial window")
        time.sleep(0.1)
    raise SystemExit("the app did not start")

def stop(pid, kill=signal.SIGTERM):
    try:
        os.kill(pid, kill)
    except ProcessLookupError:
        return
    while subprocess.run(["kill", "-0", str(pid)], capture_output=True).returncode == 0:
        time.sleep(0.2)

# Restart only the app, as run.sh does, and record its pid so run.sh still stops it.
# A crash kills it without the chance to save anything on the way out.
def relaunch(crash):
    kill = signal.SIGKILL if crash else signal.SIGTERM
    with open(state + "/app.pid") as f:
        stop(int(f.read()), kill)
    launch("%s/%s.app" % (state, app), state + "/data", state + "/app.log", [])

# Samples the frontmost app for the whole flow; a failed sample counts against the flow, so the check fails closed.
import threading
samples, sampling = [], threading.Event()
def frontmost():
    run = subprocess.run(["cua-driver", "list_apps", "{}"], capture_output=True, text=True)
    try:
        result = json.loads(run.stdout)
        active = next((item for item in result.get("apps", []) if item.get("active")), None)
        return str(active["pid"]) if run.returncode == 0 and active else None
    except (ValueError, KeyError):
        return None
def sample():
    while not sampling.wait(0.5):
        samples.append(frontmost())
watcher = threading.Thread(target=sample, daemon=True)
watcher.start()

with open(flow) as f:
    source = f.read()
size = re.search(r"^// @size (\d+)x(\d+)$", source, re.M)
window_size = f"{size.group(1)}x{size.group(2)}" if size else None
if window_size:
    relaunch(False)
setup_time = time.monotonic() - started

# A closing menu keeps its window for a moment, and a handle taken then goes stale.
PICK = """async function pick(app, item, trigger = { role: "button", name: "New chat" }) {
  // Input elsewhere can close the menu first; reopen it from its trigger and choose again.
  for (let attempt = 0; ; attempt++) {
    try { await app.click({ role: "menuitem", name: item }); break; }
    catch (error) {
      if (attempt >= 2 || ["action_uncertain", "interference_detected", "transport_failed"].includes(error.response?.code)) throw error;
      await cua.sleep(250);
      app = await cua.getApp(APP);
      if (typeof trigger === "function") await trigger(app); else await app.click(trigger);
      await app.waitFor({ role: "menuitem", name: item, timeout_ms: 3000 });
    }
  }
  for (let i = 0; ; i++) {
    await cua.sleep(250);
    try { const next = await cua.getApp(APP); if ((await next.query({ role: "button" })).length) return next; }
    catch (error) { if (i >= 40) throw error; }
  }
}
"""

# A flow may relaunch or crash the app midway at a "// @relaunch" or "// @crash" line; the next
# part gets the previous result as PREVIOUS.
previous = None
pieces = re.split(r"\n// @(relaunch|crash)\n", source)
parts, markers = pieces[0::2], [None] + pieces[1::2]
for index, part in enumerate(parts):
    if index:
        relaunch(markers[index] == "crash")
    # After a launch or relaunch, wait for the new window before the part runs.
    ready = "for (let i = 0; ; i++) { try { await cua.getApp(APP); break; } catch (error) { if (i >= 60) throw error; await cua.sleep(500); } }\n"
    kdbx = os.path.abspath(os.path.join(os.path.dirname(flow), "..", "fixtures", "vault-kdbx.kdbx"))
    script = "const APP = %s;\nconst PREVIOUS = %s;\nconst KDBX = %s;\n%s%s%s" % (json.dumps(app), json.dumps(previous), json.dumps(kdbx), PICK, ready, part)
    ui_start = time.monotonic()
    ok, out = driver("run_script", {"script": script, "timeout_ms": 120000})
    ui_time += time.monotonic() - ui_start
    out.pop("screenshot_png_b64", None)
    if not ok or not out.get("ok"):
        # A failure can leave the shared app mid-panel; restart it so later flows start clean.
        relaunch(False)
        fail(out)
    previous = out.get("value")
sampling.set()
watcher.join()
samples.append(frontmost())
# Only the test's own windows count; the user may switch apps during a run.
with open(state + "/app.pid") as f:
    ours = {f.read().strip()} | launched
if None in samples:
    print("  FAIL " + name + ": couldn't check which app was in front")
    sys.exit(1)
if ours & set(samples):
    front = ["%s %s" % (pid, os.path.basename(subprocess.run(["ps", "-o", "comm=", "-p", pid], capture_output=True, text=True).stdout.strip()))
             for pid in sorted(ours & set(samples))]
    print("  FAIL " + name + ": the app took focus during the flow (in front: %s)" % ", ".join(front))
    sys.exit(1)
print("  ok   %s (%.1fs; setup %.1fs, UI %.1fs)" % (name, time.monotonic() - started, setup_time, ui_time))
PY
  then :; else failed=1; fi
done

cleanup
trap - EXIT
ELAPSED=$(($(date +%s) - START))
[ "$failed" = 0 ] && echo "E2E passed (${ELAPSED}s)" || echo "E2E failed (${ELAPSED}s)"
exit "$failed"
