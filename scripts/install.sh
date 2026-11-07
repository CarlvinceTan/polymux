#!/bin/sh
# Install Polymux on this Mac. Usage: scripts/install.sh [server|app|both]
# Without a target it asks.
set -eu
. "$(dirname -- "$0")/lib/local.sh"
. "$ROOT/scripts/lib/drain.sh"
trap 'cancel_drain; rm -f "$LOG"' EXIT
trap 'exit 130' INT TERM HUP
pick_target install "${1:-}"

# The server listens on loopback; other devices reach it through a tunnel.
ADDRESS="http://127.0.0.1:7337"

backup() {
  [ -d "$HOME_DIR/data" ] || return 0
  dest="$HOME_DIR/backups/data-$(date +%Y%m%d-%H%M%S)"
  mkdir -p "$(dirname "$dest")"
  ditto "$HOME_DIR/data" "$dest"
}

install_server() {
  mkdir -p "$BIN" "$HOME_DIR/logs" "$AGENTS"
  install -m 0755 "$ROOT/dist/server/polymux" "$BIN/polymux"
  # What is installed must be exactly what was just built.
  cmp -s "$ROOT/dist/server/polymux" "$BIN/polymux" || { echo "Installed server differs from the new build"; return 1; }
}

install_app() {
  rm -rf "$APP"
  ditto "$ROOT/dist/macos/Polymux.app" "$APP"
  cmp -s "$ROOT/dist/macos/Polymux.app/Contents/MacOS/Polymux" "$APP/Contents/MacOS/Polymux" || { echo "Installed app differs from the new build"; return 1; }
}

# The server starts at login and restarts if it exits.
start_server() { "$BIN/polymux" host install; }

open_app() {
  # Point at the local server when there is one; otherwise the app pairs with a remote server.
  if [ "$server" = 1 ] || [ -f "$PLIST" ]; then defaults write "$BUNDLE" serverAddress "$ADDRESS"; fi
  open "$APP"
}

echo "Installing Polymux ($target)"
[ "$server" = 0 ] || (step "Build server" "$ROOT/scripts/build.sh" server) || exit 75
[ "$app" = 0 ] || (step "Build app" "$ROOT/scripts/build.sh" macos) || exit 75
if [ "$server" = 1 ]; then
  drain_server "$ROOT/dist/server/polymux" "$ADDRESS" || exit 75
  if [ -n "$DRAIN_ID" ]; then
    step "Stop previous server" launchctl bootout "$DOMAIN/$LABEL"
  fi
  DRAIN_STOPPED=1
fi
[ "$app" = 0 ] || step "Stop previous app" stop_app
[ "$server" = 0 ] || step "Back up data" backup
[ "$server" = 0 ] || step "Install server" install_server
[ "$app" = 0 ] || step "Install app" install_app
[ "$server" = 0 ] || step "Start server" start_server
[ "$server" = 0 ] || step "Verify server" verify_server
[ "$server" = 0 ] || step "Resume queued work" resume_drain
[ "$app" = 0 ] || step "Open Polymux" open_app
echo
if [ "$server" = 1 ]; then
  echo "Polymux server is running at $ADDRESS."
  echo "Pair other devices from Settings › Devices, or run: $BIN/polymux pair"
else
  echo "Polymux app installed. Pair it with a server from Settings › Devices."
fi
