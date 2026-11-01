#!/bin/sh
# Compile the app without running it. Usage: scripts/check.sh [macos|ios|all]
# An optional second argument copies the bundle while the build is locked.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
if [ "${1:-}" != --locked ]; then
  exec python3 "$ROOT/scripts/lib/locked.py" "$ROOT/.build/locks/xcode.lock" -- "$0" --locked "$@"
fi
shift
LOG=$(mktemp "${TMPDIR:-/tmp}/polymux-build.XXXXXX")
trap 'rm -f "$LOG"' EXIT

build() {
  destination=$1; data=$2
  if xcodebuild build -quiet -project "$ROOT/apps/Polymux/Polymux.xcodeproj" -scheme Polymux -configuration Debug \
    -destination "$destination" -derivedDataPath "$ROOT/.build/$data" CODE_SIGNING_ALLOWED=NO >"$LOG" 2>&1; then
    echo "$destination: ok"
  else
    status=$?
    tail -n 60 "$LOG" >&2
    echo "$destination: failed" >&2
    return "$status"
  fi
}

copy_bundle() {
  [ ! -e "$2" ] && [ ! -L "$2" ] || { echo "Copy destination already exists: $2" >&2; return 1; }
  ditto "$1" "$2"
}

case ${1:-macos} in
  macos)
    build platform=macOS xcode-test
    if [ -n "${2:-}" ]; then
      copy_bundle "$ROOT/.build/xcode-test/Build/Products/Debug/Polymux.app" "$2"
    fi
    ;;
  ios)
    build 'generic/platform=iOS Simulator' xcode-ios
    if [ -n "${2:-}" ]; then
      copy_bundle "$ROOT/.build/xcode-ios/Build/Products/Debug-iphonesimulator/Polymux.app" "$2"
    fi
    ;;
  all) build platform=macOS xcode-test && build 'generic/platform=iOS Simulator' xcode-ios ;;
  *) echo "usage: $0 [macos|ios|all]" >&2; exit 2 ;;
esac
