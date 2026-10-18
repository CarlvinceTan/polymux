# Shared by install.sh and uninstall.sh. Source it, then call: pick_target "install|remove" "${1:-}"
[ "$(uname -s)" = Darwin ] || { echo "$(basename "$0") runs on macOS only" >&2; exit 1; }
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/scripts/lib/steps.sh"

BIN=${POLYMUX_BIN_DIR:-"$HOME/.local/bin"}
APP=${POLYMUX_APP_DIR:-/Applications}/Polymux.app
LABEL=com.flarehq.polymux.server
BUNDLE=com.flarehq.polymux.app
AGENTS="$HOME/Library/LaunchAgents"
PLIST="$AGENTS/$LABEL.plist"
HOME_DIR="$HOME/.polymux"
LIB="$HOME/Library"
DOMAIN="gui/$(id -u)"

# Sets server and app to 1 or 0. Asks when no target is given.
pick_target() {
  target=${2:-}
  if [ -z "$target" ]; then
    echo "What to $1?"
    echo "  1) Server and app"
    echo "  2) Server only"
    echo "  3) App only"
    printf 'Choose [1]: '
    read -r choice || choice=
    case ${choice:-1} in 1) target=both ;; 2) target=server ;; 3) target=app ;; esac
  fi
  case $target in
    both) server=1; app=1 ;;
    server) server=1; app=0 ;;
    app) server=0; app=1 ;;
    *) echo "usage: $0 [server|app|both]" >&2; exit 2 ;;
  esac
}

stop_server() {
  launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
}

stop_app() {
  osascript -e "quit app id \"$BUNDLE\"" 2>/dev/null || true
  # Wait for the old app to exit so the new one is not mistaken for it.
  tries=0
  while pgrep -f "$APP/Contents/MacOS/" >/dev/null && [ "$tries" -lt 20 ]; do tries=$((tries + 1)); sleep 0.5; done
  pkill -f "$APP/Contents/MacOS/" 2>/dev/null || true
}
