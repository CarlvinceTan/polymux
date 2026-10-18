#!/bin/sh
# Build Polymux targets into dist/. Usage: scripts/build.sh [macos|ios|server|all]
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
IDENTITY=${POLYMUX_SIGN_IDENTITY:-"Developer ID Application: FlareHQ Pty Ltd (23YB4896XA)"}
TEAM=${POLYMUX_TEAM_ID:-23YB4896XA}
PROJECT="$ROOT/apps/Polymux/Polymux.xcodeproj"
DIST="$ROOT/dist"

# Developer ID-signed macOS client. No APNS entitlement is used.
macos() {
  xcodebuild build \
    -project "$PROJECT" -scheme Polymux -configuration Release \
    -destination platform=macOS \
    -derivedDataPath "$ROOT/.build/xcode-release" \
    -jobs 6 \
    CODE_SIGN_STYLE=Manual \
    CODE_SIGN_IDENTITY="$IDENTITY" \
    DEVELOPMENT_TEAM="$TEAM" \
    CODE_SIGN_ENTITLEMENTS="$ROOT/apps/Polymux/Polymux-local.entitlements" \
    ENABLE_HARDENED_RUNTIME=YES
  rm -rf "$DIST/macos" && mkdir -p "$DIST/macos"
  ditto "$ROOT/.build/xcode-release/Build/Products/Release/Polymux.app" "$DIST/macos/Polymux.app"
}

# Unsigned simulator build.
ios() {
  xcodebuild build \
    -project "$PROJECT" -scheme Polymux -configuration Release \
    -destination 'generic/platform=iOS Simulator' \
    -derivedDataPath "$ROOT/.build/xcode-ios" \
    -jobs 6 \
    CODE_SIGNING_ALLOWED=NO
  rm -rf "$DIST/ios" && mkdir -p "$DIST/ios"
  ditto "$ROOT/.build/xcode-ios/Build/Products/Release-iphonesimulator/Polymux.app" "$DIST/ios/Polymux.app"
}

# Host-platform Go server.
server() {
  mkdir -p "$DIST/server"
  (cd "$ROOT/apps/server" && go build -tags sqlite_fts5 -o "$DIST/server/polymux" ./cmd/polymux)
}

case ${1:-all} in
  macos) macos ;;
  ios) ios ;;
  server) server ;;
  all) macos; ios; server ;;
  *) echo "usage: $0 [macos|ios|server|all]" >&2; exit 2 ;;
esac
