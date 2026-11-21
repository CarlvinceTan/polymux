#!/bin/sh
# Build Polymux targets into dist/. Usage: scripts/build.sh [macos|macos-stage|macos-sign|ios|dmg|server|all]
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
case ${1:-all} in
  --locked) shift ;;
  macos|macos-stage|macos-sign|ios|dmg|all)
    exec python3 "$ROOT/scripts/lib/locked.py" "$ROOT/.build/locks/xcode.lock" -- "$0" --locked "$@" ;;
esac
IDENTITY=${POLYMUX_SIGN_IDENTITY:-"Developer ID Application: FlareHQ Pty Ltd (23YB4896XA)"}
TEAM=${POLYMUX_TEAM_ID:-23YB4896XA}
PROJECT="$ROOT/apps/Polymux/Polymux.xcodeproj"
DIST="$ROOT/dist"
# shellcheck source=lib/sign.sh
. "$ROOT/scripts/lib/sign.sh"
# shellcheck source=lib/server-app.sh
. "$ROOT/scripts/lib/server-app.sh"

# Sparkle compares CFBundleVersion, so derive a monotonic build number from the
# marketing version (0.4.0 -> 4000, 0.4.10 -> 4010, 1.0.0 -> 1000000).
MARKETING_VERSION=$(sed -n 's/.*MARKETING_VERSION = \([^;]*\);.*/\1/p' "$PROJECT/project.pbxproj" | head -1)
BUILD_NUMBER=${POLYMUX_BUILD_NUMBER:-$(printf '%s' "$MARKETING_VERSION" | awk -F. '{printf "%d", $1 * 1000000 + $2 * 1000 + $3}')}
printf '%s\n' "$BUILD_NUMBER" | grep -Eq '^[0-9]+(\.[0-9]+){0,2}$' || {
  echo "Invalid build number: $BUILD_NUMBER" >&2; exit 1;
}
# Archive this build's symbols, which crash logs need to name its functions.
symbols() {
  built="$ROOT/.build/xcode-release/Build/Products/Release"
  uuid=$(dwarfdump --uuid "$built/Polymux.app/Contents/MacOS/Polymux" | cut -d' ' -f2)
  [ "$uuid" = "$(dwarfdump --uuid "$built/Polymux.app.dSYM" | cut -d' ' -f2)" ] || {
    echo 'The dSYM does not match the app.' >&2; exit 1;
  }
  mkdir -p "$DIST/symbols"
  ditto -c -k --keepParent "$built/Polymux.app.dSYM" \
    "$DIST/symbols/Polymux-$MARKETING_VERSION-$BUILD_NUMBER-arm64.dSYM.zip"
}

build_macos() {
  xcodebuild build \
    -project "$PROJECT" -scheme Polymux -configuration Release \
    -destination platform=macOS \
    -derivedDataPath "$ROOT/.build/xcode-release" \
    -jobs 6 \
    CURRENT_PROJECT_VERSION="$BUILD_NUMBER" \
    ENABLE_HARDENED_RUNTIME=YES "$@"
}

stage_macos() {
  rm -rf "$DIST/macos" && mkdir -p "$DIST/macos"
  ditto "$ROOT/.build/xcode-release/Build/Products/Release/Polymux.app" "$DIST/macos/Polymux.app"
  symbols
  # The app runs this copy itself on a Mac with no other server.
  server
  server_app "$DIST/server/polymux" "$DIST/macos/Polymux.app"
  if [ -n "${POLYMUX_FEED_URL:-}" ]; then
    plist="$DIST/macos/Polymux.app/Contents/Info.plist"
    /usr/libexec/PlistBuddy -c "Set :SUFeedURL $POLYMUX_FEED_URL" "$plist"
    /usr/libexec/PlistBuddy -c 'Add :SUAutomaticallyUpdate bool true' "$plist"
    /usr/libexec/PlistBuddy -c 'Set :SUScheduledCheckInterval 3600' "$plist"
  fi
}

sign_macos() {
  APP="$DIST/macos/Polymux.app"
  [ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$APP/Contents/Info.plist")" = "$BUILD_NUMBER" ] ||
    { echo "The staged app does not match build $BUILD_NUMBER." >&2; exit 1; }
  sign_app "$APP" "$IDENTITY" "$ROOT/apps/Polymux/Polymux-local.entitlements"
}

# Developer ID-signed macOS client. No APNS entitlement is used.
macos() {
  build_macos CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY="$IDENTITY" DEVELOPMENT_TEAM="$TEAM" \
    CODE_SIGN_ENTITLEMENTS="$ROOT/apps/Polymux/Polymux-local.entitlements"
  stage_macos
  sign_macos
}

# Unsigned simulator build.
ios() {
  xcodebuild build \
    -project "$PROJECT" -scheme Polymux -configuration Release \
    -destination 'generic/platform=iOS Simulator' \
    -derivedDataPath "$ROOT/.build/xcode-ios" \
    -jobs 6 \
    CODE_SIGNING_ALLOWED=NO \
    CURRENT_PROJECT_VERSION="$BUILD_NUMBER"
  rm -rf "$DIST/ios" && mkdir -p "$DIST/ios"
  ditto "$ROOT/.build/xcode-ios/Build/Products/Release-iphonesimulator/Polymux.app" "$DIST/ios/Polymux.app"
}

# Signed disk image for Sparkle and direct downloads. Run after `macos`.
dmg() (
  APP="$DIST/macos/Polymux.app"
  [ -d "$APP" ] || { echo "Build the app first: scripts/build.sh macos" >&2; exit 1; }
  VERSION=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$APP/Contents/Info.plist")
  OUT="$DIST/macos/Polymux-$VERSION-arm64.dmg"
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/polymux-dmg.XXXXXX")
  STAGE="$WORK/stage" IMAGE="$WORK/layout.dmg" DISK=
  trap '[ -z "$DISK" ] || diskutil eject "$DISK" >/dev/null 2>&1; rm -rf "$WORK"' EXIT
  mkdir -p "$STAGE/.background"
  ditto "$APP" "$STAGE/Polymux.app"
  ln -s /Applications "$STAGE/Applications"
  cp "$ROOT/apps/Polymux/Installer/Background.tiff" "$STAGE/.background/"
  rm -f "$OUT"
  # diskutil leaves .DS_Store out of a folder, so the window layout is written
  # into a writable copy of the volume before it is compressed.
  diskutil image create from --volumeName "Polymux $VERSION" --format RAW "$STAGE" "$IMAGE"
  ATTACHED=$(diskutil image attach --mountOptions nobrowse "$IMAGE")
  DISK=$(printf '%s\n' "$ATTACHED" | awk 'NR == 1 {print $1}')
  VOLUME=$(printf '%s\n' "$ATTACHED" | awk -F '\t' '$3 != "" {print $3}')
  [ -d "$VOLUME/Polymux.app" ] || { echo "The disk image did not mount." >&2; exit 1; }
  python3 "$ROOT/scripts/lib/dmg-layout.py" "$VOLUME/.DS_Store" "Polymux $VERSION"
  diskutil eject "$DISK" >/dev/null && DISK=
  if ! diskutil image create from --format UDZO "$IMAGE" "$OUT"; then
    rm -f "$OUT"
    exit 1
  fi
  if [ "$IDENTITY" = "-" ]; then
    codesign --force --sign - "$OUT"
  else
    codesign --force --sign "$IDENTITY" --timestamp "$OUT"
  fi
  echo "$OUT"
)

# Host-platform Go server, stamped with its commit for `polymux version`.
server() {
  mkdir -p "$DIST/server"
  SHA=$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unknown)
  (cd "$ROOT/apps/server" && go build -tags sqlite_fts5 -ldflags "-X main.buildSHA=$SHA" -o "$DIST/server/polymux" ./cmd/polymux)
}

case ${1:-all} in
  macos) macos ;;
  macos-stage) build_macos CODE_SIGNING_ALLOWED=NO; stage_macos ;;
  macos-sign) sign_macos ;;
  ios) ios ;;
  dmg) dmg ;;
  server) server ;;
  all) macos; ios; server ;;
  *) echo "usage: $0 [macos|macos-stage|macos-sign|ios|dmg|server|all]" >&2; exit 2 ;;
esac
