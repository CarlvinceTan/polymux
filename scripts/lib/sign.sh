# shellcheck shell=sh
# Sign the server and Sparkle's nested executables before signing the enclosing app.
sign_item() {
  if [ "$2" = - ]; then
    codesign --force --options runtime --sign - "$1"
  else
    codesign --force --timestamp --options runtime --sign "$2" "$1"
  fi
}

sign_app() (
  entitlements=$3
  prepared=
  trap '[ -z "$prepared" ] || rm -f "$prepared"' EXIT
  if [ "$2" != - ] && [ -n "${POLYMUX_MAC_PROFILE:-}" ]; then
    mkdir -p "$ROOT/.build"
    prepared=$(mktemp "$ROOT/.build/sign-entitlements.XXXXXX") || exit 1
    python3 "$ROOT/scripts/lib/keychain.py" "$POLYMUX_MAC_PROFILE" "$3" "$1/Contents/Info.plist" >"$prepared" || exit 1
    cp "$POLYMUX_MAC_PROFILE" "$1/Contents/embedded.provisionprofile" || exit 1
    entitlements=$prepared
  fi
  for item in "$1/Contents/Helpers/Polymux Server.app" \
    "$1/Contents/Frameworks/Sparkle.framework/Versions/B/Autoupdate" \
    "$1/Contents/Frameworks/Sparkle.framework/Versions/B/Updater.app" \
    "$1/Contents/Frameworks/Sparkle.framework/Versions/B/XPCServices/Downloader.xpc" \
    "$1/Contents/Frameworks/Sparkle.framework/Versions/B/XPCServices/Installer.xpc" \
    "$1/Contents/Frameworks/Sparkle.framework"; do
    [ ! -e "$item" ] || sign_item "$item" "$2"
  done
  if [ "$2" = - ]; then
    codesign --force --options runtime --entitlements "$entitlements" --sign - "$1"
  else
    codesign --force --timestamp --options runtime --entitlements "$entitlements" --sign "$2" "$1"
  fi
)
