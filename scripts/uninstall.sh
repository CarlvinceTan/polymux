#!/bin/sh
# Remove Polymux from this Mac, including its data. Usage: scripts/uninstall.sh [server|app|both]
# Without a target it asks.
set -eu
. "$(dirname -- "$0")/lib/local.sh"
pick_target remove "${1:-}"
DEPLOY=com.flarehq.polymux.deploy

stop_deploy() { launchctl bootout "$DOMAIN/$DEPLOY" 2>/dev/null || true; }

remove_server() {
  rm -f "$PLIST" "$AGENTS/$DEPLOY.plist" "$BIN/polymux" "$BIN/polymux-deploy"
  rm -rf "$HOME_DIR/data" "$HOME_DIR/logs" "$HOME_DIR/backups" "$HOME_DIR/src" "$HOME_DIR/deploy"
}

remove_app() {
  rm -rf "$APP" "$HOME_DIR"/app-* "$HOME_DIR"/server-*
  defaults delete "$BUNDLE" 2>/dev/null || true
  rm -rf "$LIB/Caches/$BUNDLE" "$LIB/Application Support/$BUNDLE" "$LIB/HTTPStorages/$BUNDLE" \
    "$LIB/WebKit/$BUNDLE" "$LIB/Saved Application State/$BUNDLE.savedState"
}

tidy_home() { rmdir "$HOME_DIR" 2>/dev/null || true; }

echo "Removing Polymux ($target). This permanently deletes:"
if [ "$server" = 1 ]; then
  echo "  - server: $BIN/polymux, its login and deploy jobs, and $HOME_DIR/{data,logs,backups,src}"
fi
if [ "$app" = 1 ]; then
  echo "  - app: $APP, its settings, caches, and $HOME_DIR/server-*"
fi
[ "$server" = 0 ] || step "Stop deploys" stop_deploy
[ "$server" = 0 ] || step "Stop server" stop_server
[ "$app" = 0 ] || step "Stop app" stop_app
[ "$server" = 0 ] || step "Remove server and data" remove_server
[ "$app" = 0 ] || step "Remove app and data" remove_app
step "Tidy up" tidy_home
