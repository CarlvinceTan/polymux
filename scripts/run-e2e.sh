#!/bin/sh
# Run UI flows against a throwaway instance. Usage: scripts/run-e2e.sh [--driver cua|control] [flow.js ...]
# Flows starting with "// @fixture <flags>" run against a copy of the app on the DEBUG fixture server.
# A "// @relaunch" line quits and reopens the app midway; "// @crash" kills it instead.
# A "// @fresh" line starts no server, so the app runs its own as a new install does.
# A "// @second-server" line starts another live server and gives the flow its pairing link as SECOND_LINK.
# ADDRESS is the instance's own server, which flows can use as a provider that rejects every request.
# "// @linked-job" links that server with this one first and runs it under launchd, so it can restart into a server moved to it.
# Flows can call pick(app, item[, trigger]) to choose a menu item (trigger, a query or an opener function, defaults to New chat and reopens a menu that closed early); it returns a fresh handle once the menu has closed.
# The signed fixture bundle is reused, but each flow gets fresh data and preferences.
# Cursor animation is fast only inside the flow's own Cua session; global settings are unchanged.
# A failed flow restarts the shared app so later flows start clean.
# The app stays in the background; native focus samples fail the flow if its test app takes focus. Both drivers need Accessibility.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
OUT=$(mktemp -t polymux-e2e)
SECOND_OUT=$(mktemp -t polymux-e2e)
PID=
SECOND_PID=
SECOND_LINK=
START=$(date +%s)

cleanup() {
  [ -z "$PID" ] || { kill "$PID" 2>/dev/null && wait "$PID" 2>/dev/null || true; }
  [ -z "$SECOND_PID" ] || { kill "$SECOND_PID" 2>/dev/null && wait "$SECOND_PID" 2>/dev/null || true; }
  rm -f "$OUT" "$SECOND_OUT"
}
trap cleanup EXIT
trap 'exit 1' INT TERM HUP

DRIVER=${POLYMUX_E2E_DRIVER:-cua}
if [ "${1:-}" = --driver ]; then DRIVER=${2:-}; shift 2; fi
case "$DRIVER" in
  cua) command -v cua-driver >/dev/null || { echo "cua-driver is not installed" >&2; exit 1; } ;;
  control) command -v node >/dev/null || { echo "Node.js is required for Control flows" >&2; exit 1; } ;;
  *) echo 'Choose --driver cua or --driver control' >&2; exit 2 ;;
esac
if [ $# -eq 0 ]; then set -- "$ROOT"/e2e/flows/*.js; fi

FIXTURES=1
for flow in "$@"; do
  [ -f "$flow" ] || { echo "Missing flow: $flow" >&2; exit 2; }
  case $(head -n 1 "$flow") in '// @fixture'*) ;; *) FIXTURES=0 ;; esac
done
if [ "$FIXTURES" = 1 ]; then
  "$ROOT/scripts/run.sh" --fixture >"$OUT" 2>&1 &
elif grep -q '^// @fresh$' "$@"; then
  "$ROOT/scripts/run.sh" --fresh >"$OUT" 2>&1 &
else
  "$ROOT/scripts/run.sh" --no-watch >"$OUT" 2>&1 &
fi
PID=$!
until grep -q "Press Ctrl-C" "$OUT"; do
  kill -0 "$PID" 2>/dev/null || { cat "$OUT" >&2; echo "The test instance did not start" >&2; exit 1; }
  sleep 0.2
done
NAME=$(sed -n 's/^\(Polymux [0-9]*\) running.*/\1/p' "$OUT")
ADDRESS=$(sed -n 's/^  server: \([^ ]*\).*/\1/p' "$OUT")
STATE=$(sed -n 's/^  data: *\(.*\)\/data$/\1/p' "$OUT")

if grep -Eq '^// @(second-server|linked-job)$' "$@"; then
  if grep -q '^// @linked-job$' "$@"; then export POLYMUX_SERVER_JOB=1; fi
  "$ROOT/scripts/run.sh" --server >"$SECOND_OUT" 2>&1 &
  SECOND_PID=$!
  until grep -q "Press Ctrl-C" "$SECOND_OUT"; do
    kill -0 "$SECOND_PID" 2>/dev/null || { cat "$SECOND_OUT" >&2; echo "The second server did not start" >&2; exit 1; }
    sleep 0.2
  done
  SECOND_ADDRESS=$(sed -n 's/^  server: \([^ ]*\).*/\1/p' "$SECOND_OUT")
  SECOND_DATA=$(sed -n 's/^  data: *\(.*\)$/\1/p' "$SECOND_OUT")
fi

failed=0
for flow in "$@"; do
  SECOND_LINK=
  if grep -q '^// @second-server$' "$flow"; then
    SECOND_LINK=$("${SECOND_DATA%/data}/polymux" pair --url "$SECOND_ADDRESS" --token-file "$SECOND_DATA/devices/local-token" --no-qr | grep '^polymux://')
  fi
  if grep -Eq '^// @linked-job$' "$flow"; then
    until [ -s "$STATE/data/devices/local-token" ]; do sleep 0.2; done
    curl -fsS -H "Authorization: Bearer $(cat "$SECOND_DATA/devices/local-token")" "$SECOND_ADDRESS/network/invite?role=peer" |
      curl -fsS -o /dev/null -X POST -H "Authorization: Bearer $(cat "$STATE/data/devices/local-token")" --data-binary @- "$ADDRESS/network/peers"
  fi
  if POLYMUX_E2E_DRIVER="$DRIVER" POLYMUX_ROOT="$ROOT" POLYMUX_APP="$NAME" POLYMUX_FLOW="$flow" POLYMUX_STATE="$STATE" POLYMUX_ADDRESS="$ADDRESS" POLYMUX_SECOND_LINK="$SECOND_LINK" \
    POLYMUX_ENTITLEMENTS="$ROOT/apps/Polymux/Polymux-local.entitlements" python3 - <<'PY'
import atexit, json, os, plistlib, re, shutil, signal, subprocess, sys, time
started = time.monotonic()
flow, app = os.environ["POLYMUX_FLOW"], os.environ["POLYMUX_APP"]
state = os.environ["POLYMUX_STATE"]
name = os.path.basename(flow)
session = "polymux-e2e-%s" % os.getpid()
ui_time = 0


def driver(tool, args):
    if os.environ.get("POLYMUX_E2E_DRIVER") == "control":
        if tool in ("start_session", "end_session"):
            return True, {"ok": True}
        env = dict(os.environ, POLYMUX_CONTROL_FIXTURE="1" if fixture else "0")
        run = subprocess.run(["node", os.environ["POLYMUX_ROOT"] + "/tools/control/flow.mjs"],
                             input=json.dumps(args), env=env, capture_output=True, text=True, timeout=125)
        try:
            result = json.loads(run.stdout)
        except ValueError:
            result = {"ok": False, "error": "Control flow returned no valid result", "console": run.stderr[-1500:]}
        return run.returncode == 0 and result.get("ok", False), result
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

# Starts a bundle in the background, as run.sh does, and returns its pid.
launched = set()
def start(bundle, data, log, args, record):
    env = []
    if os.path.isfile(state + "/app.env"):
        with open(state + "/app.env") as f:
            for line in f.read().split("\n"):
                if line:
                    env += ["--env", line]
    subprocess.run(["open", "-g", "-n", "--env", "POLYMUX_DATA_DIR=" + data] + env + ["--stdout", log, "--stderr", log, bundle, "--args"] + args, check=True)
    for _ in range(100):
        found = subprocess.run(["pgrep", "-nf", bundle + "/Contents/MacOS/Polymux"], capture_output=True, text=True).stdout.strip()
        if found:
            launched.add(found)
            with open(state + "/" + record, "w") as file:
                file.write(found)
            matches = subprocess.run(["pgrep", "-f", bundle + "/Contents/MacOS/Polymux"], capture_output=True, text=True).stdout.split()
            if matches != [found]:
                raise SystemExit("multiple processes use this test bundle")
            return int(found)
        time.sleep(0.1)
    raise SystemExit("the app did not start")

def has_window(record):
    request = json.dumps({"instance": state, "fixture": record == "fixture.pid"})
    run = subprocess.run([os.environ["POLYMUX_ROOT"] + "/scripts/control.sh", "windows", request], capture_output=True, text=True)
    try:
        if any(window.get("onscreen") for window in json.loads(run.stdout).get("windows", [])):
            return True
        # As in frontmost(): the Cua daemon can see what an older shell can't.
        with open(state + "/" + record) as file:
            request = json.dumps({"pid": int(file.read())})
        run = subprocess.run(["cua-driver", "call", "list_windows", request], capture_output=True, text=True, timeout=5)
        return any(window.get("is_on_screen") for window in json.loads(run.stdout).get("windows", []))
    except (ValueError, OSError, subprocess.TimeoutExpired):
        return False

# A background launch can start without opening its initial SwiftUI window. Reopening would fix
# that but always brings a running app to the front, even with open -g, so start over instead.
def launch(bundle, data, log, args):
    record = "fixture.pid" if bundle.startswith(state + "/fixture/") else "app.pid"
    for _ in range(3):
        pid = start(bundle, data, log, args, record)
        for _ in range(150):
            if has_window(record):
                return pid
            time.sleep(0.1)
        stop(pid)
    raise SystemExit("the app opened no window")

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
    global fixture
    kill = signal.SIGKILL if crash else signal.SIGTERM
    if fixture:
        stop(fixture[0], kill)
        fixture = (launch(*fixture_args), fixture[1])
        with open(state + "/fixture.pid", "w") as f:
            f.write(str(fixture[0]))
        return
    with open(state + "/app.pid") as f:
        stop(int(f.read()), kill)
    pid = launch("%s/%s.app" % (state, app), state + "/data", state + "/app.log", [])
    with open(state + "/app.pid", "w") as f:
        f.write(str(pid))

# A "// @fixture <flags>" first line runs the flow against a copy of the app on the DEBUG fixture
# server instead of the live one. The copy has its own name, bundle id and data, inside run.sh's state.
fixture = fixture_args = None
def start_fixture(flags):
    global app, fixture, fixture_args
    root = os.path.join(state, "fixture")
    bundle = "%s/%s Fixture.app" % (root, app)
    plist = bundle + "/Contents/Info.plist"
    app += " Fixture"
    if not os.path.isfile(root + "/signed"):
        if os.path.isdir(bundle):
            shutil.rmtree(bundle)
        subprocess.run(["ditto", "%s/%s.app" % (state, os.environ["POLYMUX_APP"]), bundle], check=True)
        with open(plist, "rb") as file:
            values = plistlib.load(file)
        identifier = values["CFBundleIdentifier"] + ".fixture"
        values.update(CFBundleIdentifier=identifier, CFBundleName=app, CFBundleDisplayName=app)
        with open(plist, "wb") as file:
            plistlib.dump(values, file)
        subprocess.run(["codesign", "--force", "--deep", "--sign", "-", "--entitlements", os.environ["POLYMUX_ENTITLEMENTS"], bundle], capture_output=True, check=True)
        with open(root + "/signed", "w"):
            pass
    else:
        identifier = subprocess.run(["/usr/libexec/PlistBuddy", "-c", "Print :CFBundleIdentifier", plist], capture_output=True, text=True, check=True).stdout.strip()
    if os.path.isdir(root + "/data"):
        shutil.rmtree(root + "/data")
    os.makedirs(root + "/data")
    subprocess.run(["defaults", "delete", identifier], capture_output=True)
    style = "Dark" if "--verify-dark" in flags else "Light"
    fixture_args = (bundle, root + "/data", root + "/app.log", flags + ["-AppleInterfaceStyle", style])
    fixture = (launch(*fixture_args), identifier)
    with open(state + "/fixture.pid", "w") as f:
        f.write(str(fixture[0]))

def stop_fixture():
    global fixture
    if fixture:
        stop(fixture[0])
        subprocess.run(["defaults", "delete", fixture[1]], capture_output=True)
        fixture = None
        try:
            os.unlink(state + "/fixture.pid")
        except FileNotFoundError:
            pass

atexit.register(stop_fixture)

# Samples the frontmost app for the whole flow; a failed sample counts against the flow, so the check fails closed.
import threading
samples, sampling = [], threading.Event()
def frontmost():
    run = subprocess.run([os.environ["POLYMUX_ROOT"] + "/scripts/control.sh", "desktop"], capture_output=True, text=True)
    try:
        pid = json.loads(run.stdout).get("frontmost") if run.returncode == 0 else None
        if pid and pid > 0:
            return str(pid)
        # A shell from before the current login can't see the front app, but the Cua daemon can.
        run = subprocess.run(["cua-driver", "call", "list_apps", "{}"], capture_output=True, text=True, timeout=5)
        active = [app["pid"] for app in json.loads(run.stdout)["apps"] if app.get("active")]
        return str(active[0]) if active else "none"
    except (ValueError, KeyError, OSError, subprocess.TimeoutExpired):
        return None
def sample():
    while not sampling.wait(0.5):
        samples.append(frontmost())
watcher = threading.Thread(target=sample, daemon=True)
watcher.start()

with open(flow) as f:
    source = f.read()
if source.startswith("// @fixture"):
    start_fixture(source.split("\n", 1)[0].split()[2:])
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
    # A background launch can show the default window title before the shell; fixture flows check startup themselves.
    if not fixture:
        ready += "for (let i = 0; i < 40; i++) { try { const state = await (await cua.getApp(APP)).getState({ include_screenshot: false, max_elements: 1 }); if (state.window_title === 'Polymux') break; } catch (error) {} await cua.sleep(250); }\n"
    kdbx = os.path.abspath(os.path.join(os.path.dirname(flow), "..", "fixtures", "vault-kdbx.kdbx"))
    script = "const APP = %s;\nconst PREVIOUS = %s;\nconst KDBX = %s;\nconst SECOND_LINK = %s;\nconst ADDRESS = %s;\n%s%s%s" % (json.dumps(app), json.dumps(previous), json.dumps(kdbx), json.dumps(os.environ.get("POLYMUX_SECOND_LINK", "")), json.dumps(os.environ.get("POLYMUX_ADDRESS", "")), PICK, ready, part)
    ui_start = time.monotonic()
    ok, out = driver("run_script", {"script": script, "timeout_ms": 120000, "max_calls": 500})
    ui_time += time.monotonic() - ui_start
    out.pop("screenshot_png_b64", None)
    if not ok or not out.get("ok"):
        was_fixture = fixture is not None
        stop_fixture()
        # A failure can leave the shared app mid-panel; restart it so later flows start clean.
        if not was_fixture:
            relaunch(False)
        fail(out)
    previous = out.get("value")
sampling.set()
watcher.join()
samples.append(frontmost())
# Only the test's own windows count; the user may switch apps during a run.
with open(state + "/app.pid") as f:
    ours = {f.read().strip()} | launched
stop_fixture()
if None in samples:
    print("  FAIL " + name + ": couldn't check which app was in front")
    sys.exit(1)
# Instances side by side can hand focus to each other as they quit; scripts/e2e.py checks focus in its serial lane.
if ours & set(samples) and not os.environ.get("POLYMUX_E2E_PARALLEL"):
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
