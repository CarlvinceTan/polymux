#!/bin/bash
# Check native titlebar hit testing and glass bounds without showing a window.
set -euo pipefail
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
if [ "${1:-}" != --locked ]; then
  exec python3 "$ROOT/scripts/lib/locked.py" "$ROOT/.build/locks/xcode.lock" -- "$0" --locked "$@"
fi
shift
cd "$ROOT"
# The sidebar's + menu names avatar icons, so the checks link the app's AvatarKit build.
scripts/check.sh --locked macos
PRODUCTS="$ROOT/.build/xcode-test/Build/Products/Debug"
OUT=$(mktemp -d -t polymux-window)
trap 'rm -rf "$OUT"' EXIT
xcrun swiftc -swift-version 6 -parse-as-library -module-name WindowChecks -I "$PRODUCTS" \
  apps/Polymux/Sources/Views/{PaneWidth,SidebarLimits,PanelBar,PanelPresentation,Dropdown,PanelView,BubbleStyle,Motion,PointerIntent}.swift \
  apps/Polymux/Sources/Plugins/PluginAction.swift apps/Polymux/Sources/AvatarUI/AvatarIcon.swift \
  apps/Polymux/Tests/WindowTests/WindowChecks.swift "$PRODUCTS/AvatarKit.o" -o "$OUT/checks"
"$OUT/checks"
