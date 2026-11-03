#!/bin/sh
# Archive or upload an internal-only iOS build with automatic signing.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
OUT=${POLYMUX_TESTFLIGHT_DIR:-"$ROOT/dist/testflight"}
TEAM=${POLYMUX_TEAM_ID:-23YB4896XA}
MODE=${1:-archive}
case "$MODE" in archive|upload) ;; *) echo "usage: $0 [archive|upload]" >&2; exit 2 ;; esac
POLYMUX_BUILD_NUMBER=${POLYMUX_BUILD_NUMBER:-$(git -C "$ROOT" rev-list --count HEAD)}
printf '%s\n' "$POLYMUX_BUILD_NUMBER" | grep -Eq '^[1-9][0-9]{0,8}$' || {
  echo "Use a plain increasing integer build number." >&2; exit 1;
}
mkdir -p "$OUT"
# Signing is automatic: an App Store Connect API key (CI) or the account signed in to Xcode (local).
AUTH=
if [ -n "${ASC_KEY_PATH:-}" ]; then
  : "${ASC_KEY_ID:?Set the App Store Connect key ID}"
  : "${ASC_ISSUER_ID:?Set the App Store Connect issuer ID}"
  AUTH="-authenticationKeyPath $ASC_KEY_PATH -authenticationKeyID $ASC_KEY_ID -authenticationKeyIssuerID $ASC_ISSUER_ID"
fi

if [ "$MODE" = archive ]; then
  xcodebuild archive -project "$ROOT/apps/Polymux/Polymux.xcodeproj" \
    -scheme Polymux -configuration Release -destination 'generic/platform=iOS' \
    -derivedDataPath "$ROOT/.build/xcode-testflight" -archivePath "$OUT/Polymux.xcarchive" \
    -jobs 6 -allowProvisioningUpdates $AUTH DEVELOPMENT_TEAM="$TEAM" \
    CURRENT_PROJECT_VERSION="$POLYMUX_BUILD_NUMBER"
else
  ARCHIVE="$OUT/Polymux.xcarchive"
  BUILT=$(/usr/libexec/PlistBuddy -c 'Print :ApplicationProperties:CFBundleVersion' "$ARCHIVE/Info.plist")
  [ "$BUILT" = "$POLYMUX_BUILD_NUMBER" ] || { echo "Archive build number differs from requested upload." >&2; exit 1; }
  OPTIONS="$OUT/ExportOptions.plist"
  cat > "$OPTIONS" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>method</key><string>app-store-connect</string>
  <key>destination</key><string>upload</string>
  <key>teamID</key><string>$TEAM</string>
  <key>signingStyle</key><string>automatic</string>
  <key>manageAppVersionAndBuildNumber</key><false/>
  <key>testFlightInternalTestingOnly</key><true/>
  <key>uploadSymbols</key><true/>
</dict></plist>
EOF
  xcodebuild -exportArchive -archivePath "$ARCHIVE" -exportPath "$OUT/export" \
    -exportOptionsPlist "$OPTIONS" -allowProvisioningUpdates $AUTH
fi
