#!/bin/bash
# Check native titlebar hit testing and glass bounds without showing a window.
set -euo pipefail
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
OUT=$(mktemp -d -t polymux-window)
trap 'rm -rf "$OUT"' EXIT
xcrun swiftc -swift-version 6 -parse-as-library -module-name WindowChecks \
  "$ROOT/apps/Polymux/Sources/Views/"{PaneWidth,SidebarLimits,PanelBar,PanelPresentation,Dropdown,PanelView,BubbleStyle}.swift \
  "$ROOT/apps/Polymux/Tests/WindowTests/WindowChecks.swift" -o "$OUT/checks"
"$OUT/checks"
