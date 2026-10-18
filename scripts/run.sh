#!/bin/sh
# Run Polymux on throwaway data, then delete it all on Ctrl-C. Usage: scripts/run.sh [--watch]
# --watch rebuilds and restarts the server or relaunches the app when its sources change.
# Each run has its own id, data, port, and preferences, so several can run at once.
# Never touches ~/.polymux, the installed Polymux.app, or its preferences.
set -eu
watch=0
case ${1:-} in
  "") ;;
  --watch) watch=1 ;;
  *) echo "usage: $0 [--watch]" >&2; exit 2 ;;
esac
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
BASE=${TMPDIR:-/tmp}/polymux-test
LOCK=$BASE/build.lock
# Claim the lowest free number; mkdir is atomic, so parallel runs never share one.
ID=1
until mkdir -p "$BASE" && mkdir "$BASE/$ID" 2>/dev/null; do ID=$((ID + 1)); done
STATE=$BASE/$ID
BUNDLE=com.flarehq.polymux.app.test.$ID
locked=0

# Xcode allows one build per derived data folder at a time.
lock() {
  until mkdir "$LOCK" 2>/dev/null; do
    holder=$(cat "$LOCK/pid" 2>/dev/null || true)
    [ -z "$holder" ] || kill -0 "$holder" 2>/dev/null || rm -rf "$LOCK"
    sleep 1
  done
  echo $$ >"$LOCK/pid"
  locked=1
}

unlock() { rm -rf "$LOCK"; locked=0; }

cleanup() {
  [ "$locked" = 0 ] || unlock
  for name in app server; do
    [ -f "$STATE/$name.pid" ] && kill "$(cat "$STATE/$name.pid")" 2>/dev/null || true
  done
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
  (cd "$ROOT/apps/server" && go build -tags sqlite_fts5 -o "$STATE/polymux.new" ./cmd/polymux) || return 1
  mv "$STATE/polymux.new" "$STATE/polymux"
}

build_app() {
  lock
  if ! xcodebuild build -quiet \
    -project "$ROOT/apps/Polymux/Polymux.xcodeproj" -scheme Polymux -configuration Debug \
    -destination platform=macOS -derivedDataPath "$ROOT/.build/xcode-test" \
    CODE_SIGNING_ALLOWED=NO >"$STATE/build.log" 2>&1; then
    tail -20 "$STATE/build.log" >&2
    unlock
    return 1
  fi
  # Copy the build out before the next instance rebuilds into the same folder.
  rm -rf "$APP"
  ditto "$ROOT/.build/xcode-test/Build/Products/Debug/Polymux.app" "$APP"
  unlock

  # Rename the build so it has its own name, bundle id and preferences.
  PLIST="$APP/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier $BUNDLE" "$PLIST"
  /usr/libexec/PlistBuddy -c "Set :CFBundleName Polymux $ID" "$PLIST"
  /usr/libexec/PlistBuddy -c "Add :CFBundleDisplayName string Polymux $ID" "$PLIST" 2>/dev/null ||
    /usr/libexec/PlistBuddy -c "Set :CFBundleDisplayName Polymux $ID" "$PLIST"
  codesign --force --deep --sign - --entitlements "$ROOT/apps/Polymux/Polymux-local.entitlements" "$APP"
}

# Stops the named process (server or app) if it is running.
halt() {
  [ -f "$STATE/$1.pid" ] || return 0
  pid=$(cat "$STATE/$1.pid")
  kill "$pid" 2>/dev/null && wait "$pid" 2>/dev/null || true
  rm -f "$STATE/$1.pid"
}

start_server() {
  POLYMUX_DATA_DIR="$STATE/data" POLYMUX_LOG_DIR="$STATE/logs" POLYMUX_NO_MDNS=1 POLYMUX_TUNNEL=0 \
    "$STATE/polymux" serve -dev -listen "127.0.0.1:$PORT" >>"$STATE/server.log" 2>&1 &
  echo $! >"$STATE/server.pid"
}

start_app() {
  POLYMUX_DATA_DIR="$STATE/data" \
    "$APP/Contents/MacOS/Polymux" -serverAddress "$ADDRESS" >>"$STATE/app.log" 2>&1 &
  echo $! >"$STATE/app.pid"
}

# True when any matching source file is newer than the stamp.
changed() {
  stamp=$1; shift
  [ -n "$(find "$@" -type f \( -name '*.go' -o -name '*.swift' -o -name '*.xcassets' -o -name 'Contents.json' -o -name '*.pbxproj' -o -name Package.swift \) -newer "$stamp" 2>/dev/null | head -1)" ]
}

build_server
build_app
start_server
start_app

echo "Polymux $ID running"
echo "  server: $ADDRESS  (binary: $STATE/polymux)"
echo "  data:   $STATE/data"
echo "  logs:   $STATE/logs, $STATE/server.log, $STATE/app.log"
[ "$watch" = 0 ] || echo "Watching for changes."
echo "Press Ctrl-C to stop and delete it."

touch "$STATE/server.stamp" "$STATE/app.stamp"
while :; do
  sleep 1
  [ "$watch" = 1 ] || continue
  if changed "$STATE/server.stamp" "$ROOT/apps/server"; then
    touch "$STATE/server.stamp"
    echo "Server changed; rebuilding"
    if build_server; then halt server; start_server; echo "Server restarted"; else echo "Server build failed; fix it and save again"; fi
  fi
  if changed "$STATE/app.stamp" "$ROOT/apps/Polymux" "$ROOT/packages" "$ROOT/Package.swift"; then
    touch "$STATE/app.stamp"
    echo "App changed; rebuilding"
    if build_app; then halt app; start_app; echo "App relaunched"; else echo "App build failed; fix it and save again"; fi
  fi
done
