#!/bin/sh
# Run the cua-driver E2E flows in e2e/flows against a throwaway instance. Usage: scripts/run-e2e.sh [flow.js ...]
# The app stays in the background; the run fails if it takes focus. Needs cua-driver with Accessibility access.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT=$(mktemp -t polymux-e2e)
PID=

front() { osascript -e 'tell application "System Events" to get name of first process whose frontmost is true'; }

cleanup() {
  [ -z "$PID" ] || { kill "$PID" 2>/dev/null && wait "$PID" 2>/dev/null || true; }
  rm -f "$OUT"
}
trap cleanup EXIT
trap 'exit 1' INT TERM HUP

command -v cua-driver >/dev/null || { echo "cua-driver is not installed" >&2; exit 1; }
if [ $# -eq 0 ]; then set -- "$ROOT"/e2e/flows/*.js; fi

before=$(front)
"$ROOT/scripts/run.sh" >"$OUT" 2>&1 &
PID=$!
until grep -q "Press Ctrl-C" "$OUT"; do
  kill -0 "$PID" 2>/dev/null || { cat "$OUT" >&2; echo "The test instance did not start" >&2; exit 1; }
  sleep 2
done
NAME=$(sed -n 's/^\(Polymux [0-9]*\) running.*/\1/p' "$OUT")
sleep 2

failed=0
for flow in "$@"; do
  if POLYMUX_APP="$NAME" POLYMUX_FLOW="$flow" python3 - <<'PY'
import json, os, subprocess, sys
flow = os.environ["POLYMUX_FLOW"]
script = 'const APP = %s;\n%s' % (json.dumps(os.environ["POLYMUX_APP"]), open(flow).read())
run = subprocess.run(["cua-driver", "run_script", json.dumps({"script": script})], capture_output=True, text=True)
try:
    out, _ = json.JSONDecoder().raw_decode(run.stdout[run.stdout.index("{"):])
except ValueError:
    out = {"ok": False, "error": (run.stdout + run.stderr)[:600]}
out.pop("screenshot_png_b64", None)
name = os.path.basename(flow)
if run.returncode == 0 and out.get("ok"):
    print("  ok   " + name)
else:
    print("  FAIL " + name)
    print(json.dumps({k: v for k, v in out.items() if k in ("error", "console", "value")}, indent=2)[:1500])
    sys.exit(1)
PY
  then :; else failed=1; fi
done

after=$(front)
if [ "$before" != "$after" ]; then echo "  FAIL focus moved from $before to $after"; failed=1; fi
[ "$failed" = 0 ] && echo "E2E passed" || echo "E2E failed"
exit "$failed"
