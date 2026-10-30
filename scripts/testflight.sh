#!/bin/sh
# Archive or upload an internal-only iOS build using an App Store distribution profile.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
OUT=${POLYMUX_TESTFLIGHT_DIR:-"$ROOT/dist/testflight"}
TEAM=${POLYMUX_TEAM_ID:-23YB4896XA}
BUNDLE=com.flarehq.polymux.app
MODE=${1:-archive}
case "$MODE" in archive|upload) ;; *) echo "usage: $0 [archive|upload]" >&2; exit 2 ;; esac
: "${POLYMUX_BUILD_NUMBER:?Set a unique build number (CI uses scripts/dev-version.py)}"
: "${POLYMUX_IOS_PROFILE:?Set the path to the App Store distribution .mobileprovision}"
printf '%s\n' "$POLYMUX_BUILD_NUMBER" | grep -Eq '^[1-9][0-9]{0,3}\.[0-9]{1,2}\.[0-9]{1,2}$' || {
  echo "Use Apple's numeric major.minor.patch build format (4.2.2 digits)." >&2; exit 1;
}
mkdir -p "$OUT"
PROFILE=$(mktemp)
trap 'rm -f "$PROFILE"' EXIT
security cms -D -i "$POLYMUX_IOS_PROFILE" > "$PROFILE"
UUID=$(/usr/libexec/PlistBuddy -c 'Print :UUID' "$PROFILE")
APP_ID=$(/usr/libexec/PlistBuddy -c 'Print :Entitlements:application-identifier' "$PROFILE")
[ "$APP_ID" = "$TEAM.$BUNDLE" ] || { echo "Profile is not for $TEAM.$BUNDLE" >&2; exit 1; }
BETA=$(/usr/libexec/PlistBuddy -c 'Print :Entitlements:beta-reports-active' "$PROFILE" 2>/dev/null || true)
[ "$BETA" = true ] || { echo "Profile does not allow App Store/TestFlight distribution." >&2; exit 1; }
/usr/libexec/PlistBuddy -c 'Print :ProvisionedDevices' "$PROFILE" >/dev/null 2>&1 && {
  echo "Use an App Store profile, not a development or ad-hoc profile." >&2; exit 1;
}
PROFILES="$HOME/Library/Developer/Xcode/UserData/Provisioning Profiles"
mkdir -p "$PROFILES"
cp "$POLYMUX_IOS_PROFILE" "$PROFILES/$UUID.mobileprovision"

if [ "$MODE" = archive ]; then
  xcodebuild archive -project "$ROOT/apps/Polymux/Polymux.xcodeproj" \
    -scheme Polymux -configuration Release -destination 'generic/platform=iOS' \
    -derivedDataPath "$ROOT/.build/xcode-testflight" -archivePath "$OUT/Polymux.xcarchive" \
    -jobs 6 CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY='Apple Distribution' \
    DEVELOPMENT_TEAM="$TEAM" PROVISIONING_PROFILE_SPECIFIER="$UUID" \
    CURRENT_PROJECT_VERSION="$POLYMUX_BUILD_NUMBER"
else
  : "${ASC_KEY_PATH:?Set the path to the App Store Connect API .p8 key}"
  : "${ASC_KEY_ID:?Set the App Store Connect key ID}"
  : "${ASC_ISSUER_ID:?Set the App Store Connect issuer ID}"
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
  <key>signingStyle</key><string>manual</string>
  <key>signingCertificate</key><string>Apple Distribution</string>
  <key>provisioningProfiles</key><dict><key>$BUNDLE</key><string>$UUID</string></dict>
  <key>manageAppVersionAndBuildNumber</key><false/>
  <key>testFlightInternalTestingOnly</key><true/>
  <key>uploadSymbols</key><true/>
</dict></plist>
EOF
  xcodebuild -exportArchive -archivePath "$ARCHIVE" -exportPath "$OUT/export" \
    -exportOptionsPlist "$OPTIONS" -authenticationKeyPath "$ASC_KEY_PATH" \
    -authenticationKeyID "$ASC_KEY_ID" -authenticationKeyIssuerID "$ASC_ISSUER_ID"
fi
