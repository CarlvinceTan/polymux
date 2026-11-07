# shellcheck shell=sh
# Puts the server inside the app as Polymux Server.app, so System Settings lists it with an icon and
# the bots it runs inherit what it's granted. Usage: server_app BINARY APP [BUNDLE_ID]
server_app() {
  bundle="$2/Contents/Helpers/Polymux Server.app"
  rm -rf "$bundle"
  mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources" || return 1
  cp "$1" "$bundle/Contents/MacOS/polymux" || return 1
  cp "$2/Contents/Resources/AppIcon.icns" "$bundle/Contents/Resources/AppIcon.icns" || return 1
  info="$2/Contents/Info.plist"
  version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$info") || return 1
  build=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$info") || return 1
  minimum=$(/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' "$info") || return 1
  cat >"$bundle/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDisplayName</key>
	<string>Polymux Server</string>
	<key>CFBundleExecutable</key>
	<string>polymux</string>
	<key>CFBundleIconFile</key>
	<string>AppIcon</string>
	<key>CFBundleIdentifier</key>
	<string>${3:-com.flarehq.polymux.server}</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>Polymux Server</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>$version</string>
	<key>CFBundleVersion</key>
	<string>$build</string>
	<key>LSMinimumSystemVersion</key>
	<string>$minimum</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSAppleEventsUsageDescription</key>
	<string>A bot in Polymux wants to control other apps with AppleScript. Polymux Server doesn’t send these commands itself.</string>
	<key>NSAudioCaptureUsageDescription</key>
	<string>A bot in Polymux wants to record your system’s audio. Polymux Server doesn’t record audio itself.</string>
	<key>NSBluetoothAlwaysUsageDescription</key>
	<string>A bot in Polymux wants to use Bluetooth. Polymux Server doesn’t use Bluetooth itself.</string>
	<key>NSCalendarsUsageDescription</key>
	<string>A bot in Polymux wants to read your calendars. Polymux Server doesn’t read them itself.</string>
	<key>NSCameraUsageDescription</key>
	<string>A bot in Polymux wants to use your camera. Polymux Server doesn’t use the camera itself.</string>
	<key>NSContactsUsageDescription</key>
	<string>A bot in Polymux wants to read your contacts. Polymux Server doesn’t read them itself.</string>
	<key>NSLocalNetworkUsageDescription</key>
	<string>Polymux Server and its bots use this to find and connect to devices on your local network.</string>
	<key>NSLocationUsageDescription</key>
	<string>A bot in Polymux wants to know your location. Polymux Server doesn’t track location itself.</string>
	<key>NSMicrophoneUsageDescription</key>
	<string>A bot in Polymux wants to use your microphone. Polymux Server doesn’t record audio itself.</string>
	<key>NSPhotoLibraryUsageDescription</key>
	<string>A bot in Polymux wants to access your photo library. Polymux Server doesn’t read your photos itself.</string>
</dict>
</plist>
EOF
}
