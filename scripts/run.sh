#!/bin/sh
# Run Polymux on throwaway data. Usage: scripts/run.sh [--watch|--server|--fixture <flags>...]
# Type r and Enter to rebuild and restart; --watch does it automatically when sources change.
# Each run has its own id, data, port, and preferences, so several can run at once.
# Never touches ~/.polymux, the installed Polymux.app, or its preferences.
set -eu
umask 077
watch=0
server_only=0
fixture=0
case ${1:-} in
  "") ;;
  --watch) watch=1; shift ;;
  --no-watch) watch=0; shift ;;
  --server) server_only=1; shift ;;
  --fixture)
    fixture=1
    shift
    appearance=Light
    for flag in "$@"; do
      case "$flag" in --verify-dark|--tools-dark) appearance=Dark ;; esac
    done
    case ${POLYMUX_VERIFY_APPEARANCE:-${POLYMUX_VERIFY_THEME:-}} in
      light) appearance=Light ;;
      dark) appearance=Dark ;;
    esac
    set -- --verify-fixture "$@" -AppleInterfaceStyle "$appearance"
    ;;
  *) echo "usage: $0 [--watch|--no-watch|--server|--fixture <flags>...]" >&2; exit 2 ;;
esac
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
# Share instance numbers even when callers override TMPDIR.
PLATFORM=$(uname -s)
if [ "$PLATFORM" = Darwin ]; then
  BASE=$(getconf DARWIN_USER_TEMP_DIR); BASE=${BASE%/}/polymux-test
else
  [ "$server_only" = 1 ] || { echo 'The app runner requires macOS.' >&2; exit 2; }
  BASE=${TMPDIR:-/tmp}/polymux-test-$(id -u)
fi
# Removes an instance's vault key from the login Keychain; the server names it by its vault folder's SHA-256.
forget_key() {
  [ "$PLATFORM" = Darwin ] || return 0
  account=$(printf %s "$1/data/vault" | shasum -a 256 | cut -d' ' -f1)
  /usr/bin/security delete-generic-password -s com.polymux.vault -a "$account" >/dev/null 2>&1 || true
}
stop_owned() {
  physical=$(CDPATH='' cd -- "$1" && pwd -P)
  for record in "$1"/app.pid "$1"/server.pid "$1"/fixture.pid; do
    [ -f "$record" ] || continue
    pid=$(cat "$record")
    executable=$(ps -p "$pid" -o comm= 2>/dev/null || true)
    case "$executable" in "$physical/"*|"$1/"*) ;; *) continue ;; esac
    kill -TERM "$pid" 2>/dev/null || true
    attempts=0
    while [ "$attempts" -lt 30 ]; do
      executable=$(ps -p "$pid" -o comm= 2>/dev/null || true)
      case "$executable" in "$physical/"*|"$1/"*) ;; *) break ;; esac
      attempts=$((attempts + 1))
      sleep 0.1
    done
    [ "$attempts" -lt 30 ] || kill -KILL "$pid" 2>/dev/null || true
  done
}
# An instance whose run.sh has died (its session ended without Ctrl-C) is stopped and deleted.
mkdir -p "$BASE"
for dir in "$BASE"/[0-9]*; do
  owner=$(cat "$dir/run.pid" 2>/dev/null) || continue
  kill -0 "$owner" 2>/dev/null && continue
  stop_owned "$dir"
  forget_key "$dir"
  defaults delete "com.flarehq.polymux.app.test.${dir##*/}" 2>/dev/null || true
  rm -rf "$dir"
done
# Claim the lowest free number; mkdir is atomic, so parallel runs never share one.
ID=1
until mkdir -p "$BASE" && mkdir "$BASE/$ID" 2>/dev/null; do ID=$((ID + 1)); done
STATE=$BASE/$ID
echo $$ >"$STATE/run.pid"
BUNDLE=com.flarehq.polymux.app.test.$ID
BUILD_PID=

cleanup() {
  if [ -n "$BUILD_PID" ]; then
    kill -TERM "$BUILD_PID" 2>/dev/null || true
    wait "$BUILD_PID" 2>/dev/null || true
  fi
  # Remove this instance's provider secrets before its server and metadata disappear.
  if [ -s "$STATE/data/devices/local-token" ] && [ -n "${ADDRESS:-}" ]; then
    python3 - "$ADDRESS" "$STATE/data/devices/local-token" <<'PY' || true
import json, pathlib, sys, urllib.parse, urllib.request
try:
    headers = {"Authorization": "Bearer " + pathlib.Path(sys.argv[2]).read_text().strip()}
    def call(path, method="GET"):
        request = urllib.request.Request(sys.argv[1] + path, headers=headers, method=method)
        return urllib.request.urlopen(request, timeout=5)
    with call("/providers") as response:
        providers = json.load(response)
    for provider in providers:
        with call("/providers/" + urllib.parse.quote(provider["id"], safe=""), "DELETE"):
            pass
except (OSError, ValueError):
    pass
PY
  fi
  stop_owned "$STATE"
  [ ! -f "$STATE/server.pid" ] || wait "$(cat "$STATE/server.pid")" 2>/dev/null || true
  # Remove only this throwaway instance's vault key, including after a failed flow.
  forget_key "$STATE"
  defaults delete "$BUNDLE" 2>/dev/null || true
  rm -rf "$STATE"
  rmdir "$BASE" 2>/dev/null || true
}

trap cleanup EXIT
trap 'exit 0' INT TERM HUP

mkdir -p "$BASE" "$STATE/data" "$STATE/logs"
PORT=$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
ADDRESS=http://127.0.0.1:$PORT
APP="$STATE/Polymux $ID.app"

build_server() {
  (cd "$ROOT/apps/server" && exec python3 "$ROOT/scripts/lib/locked.py" "$STATE/server-build.lock" -- \
    go build -tags sqlite_fts5 -o "$STATE/polymux.new" ./cmd/polymux) &
  BUILD_PID=$!
  if ! wait "$BUILD_PID"; then BUILD_PID=; return 1; fi
  BUILD_PID=
  mv "$STATE/polymux.new" "$STATE/polymux"
}

build_app() {
  staged="$STATE/build.app"
  rm -rf "$staged"
  "$ROOT/scripts/check.sh" macos "$staged" >"$STATE/build.log" 2>&1 &
  BUILD_PID=$!
  if ! wait "$BUILD_PID"; then
    BUILD_PID=
    tail -60 "$STATE/build.log" >&2
    return 1
  fi
  BUILD_PID=

  # Rename the build so it has its own name, bundle id and preferences.
  PLIST="$staged/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier $BUNDLE" "$PLIST" || return 1
  /usr/libexec/PlistBuddy -c "Set :CFBundleName Polymux $ID" "$PLIST" || return 1
  /usr/libexec/PlistBuddy -c "Add :CFBundleDisplayName string Polymux $ID" "$PLIST" 2>/dev/null ||
    /usr/libexec/PlistBuddy -c "Set :CFBundleDisplayName Polymux $ID" "$PLIST" || return 1
  codesign --force --deep --sign - --entitlements "$ROOT/apps/Polymux/Polymux-local.entitlements" "$staged" || return 1
  halt app
  rm -rf "$APP"
  mv "$staged" "$APP"
}

# Stops the named process (server or app) if it is running.
halt() {
  [ -f "$STATE/$1.pid" ] || return 0
  pid=$(cat "$STATE/$1.pid")
  kill "$pid" 2>/dev/null || true
  # The app is launched through open, so it isn't this shell's child to wait for.
  while kill -0 "$pid" 2>/dev/null; do sleep 0.1; done
  rm -f "$STATE/$1.pid"
}

# Model settings come from apps/server/.env so every instance gets the same key.
load_env() {
  [ -f "$ROOT/apps/server/.env" ] || return 0
  set -a; . "$ROOT/apps/server/.env"; set +a
  # The opencode-go endpoint rejects requests without a session header.
  case ${POLYMUX_MODEL_URL:-} in
    *opencode.ai*) echo '{"sessionHeader":"x-opencode-session"}' >"$STATE/data/model.json" ;;
  esac
}

start_server() {
  load_env
  POLYMUX_DATA_DIR="$STATE/data" POLYMUX_LOG_DIR="$STATE/logs" POLYMUX_NO_MDNS=1 POLYMUX_TUNNEL=0 \
    "$STATE/polymux" serve -dev -listen "127.0.0.1:$PORT" -device-listen "127.0.0.1:0" >>"$STATE/server.log" 2>&1 &
  echo $! >"$STATE/server.pid"
  attempts=0
  until [ -s "$STATE/data/devices/local-token" ]; do
    kill -0 "$(cat "$STATE/server.pid")" 2>/dev/null || return 1
    attempts=$((attempts + 1))
    [ "$attempts" -lt 100 ] || { echo "Server credential was not ready" >&2; return 1; }
    sleep 0.1
  done
}

# open -g asks LaunchServices for a background launch, so the app never takes focus.
start_app() {
  if [ "$fixture" = 0 ]; then set -- -serverAddress "$ADDRESS" "$@"; fi
  open -g -n --env POLYMUX_DATA_DIR="$STATE/data" --stdout "$STATE/app.log" --stderr "$STATE/app.log" \
    "$APP" --args "$@"
  attempts=0
  until pid=$(pgrep -nf "$APP/Contents/MacOS/Polymux"); do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 100 ] || { echo "The app did not start" >&2; return 1; }
    sleep 0.1
  done
  echo "$pid" >"$STATE/app.pid"
}

# Fingerprints every file under the given paths, so edits, additions and deletions all count.
fingerprint() {
  if [ "$PLATFORM" = Darwin ]; then format='-f'; template='%m %z %N'
  else format='-c'; template='%Y %s %n'; fi
  find "$@" -type f -not -path '*/.build/*' -not -path '*/xcuserdata/*' -not -name '.DS_Store' \
    -exec stat "$format" "$template" {} + 2>/dev/null | sort | cksum
}

# True when the files under the paths differ from the saved fingerprint; saves the new one.
changed() {
  stamp=$1; shift
  now=$(fingerprint "$@")
  [ "$now" != "$(cat "$stamp" 2>/dev/null)" ] || return 1
  echo "$now" >"$stamp"
}

if [ "$fixture" = 0 ]; then build_server; start_server; fi
if [ "$server_only" = 0 ]; then build_app; start_app "$@"; fi

echo "Polymux $ID running"
if [ "$fixture" = 0 ]; then echo "  server: $ADDRESS  (binary: $STATE/polymux)"; fi
echo "  data:   $STATE/data"
echo "  logs:   $STATE/logs, $STATE/server.log, $STATE/app.log"
if [ "$watch" = 1 ]; then
  echo "Watching for changes."
else
  echo "Type r (both), s (server), or a (app), then Enter to rebuild."
fi
echo "Press Ctrl-C to stop and delete it."

fingerprint "$ROOT/apps/server" "$ROOT/packages/runtime" "$ROOT/packages/memory" "$ROOT/packages/search" "$ROOT/packages/vault" "$ROOT/go.work" "$ROOT/go.work.sum" >"$STATE/server.stamp"
fingerprint "$ROOT/apps/Polymux" "$ROOT/packages" "$ROOT/Package.swift" >"$STATE/app.stamp"
input=1
while :; do
  if [ "$watch" = 1 ]; then
    sleep 1
  elif [ "$input" = 1 ]; then
    command=
    if ! IFS= read -r command; then input=0; fi
    case "$command" in r|s|a) ;; *) continue ;; esac
  else
    sleep 1
    continue
  fi
  if [ "$fixture" = 0 ] && { [ "$watch" = 1 ] || [ "$command" != a ]; } && \
    { [ "$watch" = 0 ] || changed "$STATE/server.stamp" "$ROOT/apps/server" "$ROOT/packages/runtime" "$ROOT/packages/memory" "$ROOT/packages/search" "$ROOT/packages/vault" "$ROOT/go.work" "$ROOT/go.work.sum"; }; then
    echo "Rebuilding server"
    if build_server; then halt server; start_server; echo "Server restarted"; else echo "Server build failed; fix it and reload again"; fi
  fi
  if [ "$server_only" = 0 ] && { [ "$watch" = 1 ] || [ "$command" != s ]; } && \
    { [ "$watch" = 0 ] || changed "$STATE/app.stamp" "$ROOT/apps/Polymux" "$ROOT/packages" "$ROOT/Package.swift"; }; then
    echo "Rebuilding app"
    if build_app; then start_app "$@"; echo "App relaunched"; else echo "App build failed; fix it and reload again"; fi
  fi
done
