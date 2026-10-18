#!/bin/sh
# Install Polymux on this Mac. Usage: scripts/install.sh [server|app|both]
# Without a target it asks.
set -eu
. "$(dirname -- "$0")/lib/local.sh"
pick_target install "${1:-}"

# Listen on Tailscale when it is up so other devices can reach this Mac; loopback otherwise.
TAILNET=$(ifconfig | awk '/inet 100\./ { split($2, o, "."); if (o[2] >= 64 && o[2] <= 127) { print $2; exit } }')
if [ -n "$TAILNET" ]; then
  ADDRESS="http://$TAILNET:7337"
  FLAGS="<string>-tailscale</string>"
else
  ADDRESS="http://127.0.0.1:7337"
  FLAGS=""
fi

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
start_server() {
  cat >"$PLIST" <<PLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BIN/polymux</string><string>serve</string>$FLAGS</array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>$HOME_DIR/logs/launchd.log</string>
  <key>StandardErrorPath</key><string>$HOME_DIR/logs/launchd.log</string>
</dict>
</plist>
PLIST_EOF
  launchctl bootstrap "$DOMAIN" "$PLIST"
  # Any HTTP reply means the server is listening; /health itself requires a credential.
  tries=0
  until [ "$(curl -s -o /dev/null -w '%{http_code}' "$ADDRESS/health")" != "000" ]; do
    tries=$((tries + 1))
    [ "$tries" -lt 30 ] || { echo "The server did not start; see $HOME_DIR/logs/launchd.log"; return 1; }
    sleep 1
  done
}

open_app() {
  # Point at the local server when there is one; otherwise the app pairs with a remote server.
  if [ "$server" = 1 ] || [ -f "$PLIST" ]; then defaults write "$BUNDLE" serverAddress "$ADDRESS"; fi
  open "$APP"
}

echo "Installing Polymux ($target)"
[ "$server" = 0 ] || step "Build server" "$ROOT/scripts/build.sh" server
[ "$app" = 0 ] || step "Build app" "$ROOT/scripts/build.sh" macos
[ "$server" = 0 ] || step "Stop previous server" stop_server
[ "$app" = 0 ] || step "Stop previous app" stop_app
[ "$server" = 0 ] || step "Back up data" backup
[ "$server" = 0 ] || step "Install server" install_server
[ "$app" = 0 ] || step "Install app" install_app
[ "$server" = 0 ] || step "Start server" start_server
[ "$app" = 0 ] || step "Open Polymux" open_app
echo
if [ "$server" = 1 ]; then
  echo "Polymux server is running at $ADDRESS."
  echo "Pair other devices from Settings › Devices, or run: $BIN/polymux pair"
else
  echo "Polymux app installed. Pair it with a server from Settings › Devices."
fi
