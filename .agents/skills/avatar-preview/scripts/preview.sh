#!/bin/bash
set -euo pipefail
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH='' cd -- "$HERE/../../../.." && pwd)

case "${1:-}" in
  -h|--help)
    printf '%s\n' \
      'Usage: preview.sh AVATAR [AVATAR ...] [options]' \
      '  --output PATH          GIF destination (default: avatar-preview.gif)' \
      '  --size PIXELS          Resolution per avatar, 256–2048 (default: 768)' \
      '  --fps 20|25|50         Playback frame rate (default: 25)' \
      '  --appearance light|dark' \
      '  --color NAME           Palette for original geometric shapes' \
      '  --sections LIST        moods,states,expressions,interactions,props (default: all five)' \
      '  --moods LIST           Filter the moods section, e.g. thinking,happy,blocked' \
      '  --seconds NUMBER       Override each segment duration; otherwise use native timings' \
      '  --output PATH --force  Explicitly replace a previous export and its JSON manifest' \
      '  --list                 List current avatars and available behaviours as JSON' \
      '  --plan                 Print the export timeline without rendering' \
      '  --check                Run isolated export checks in light and dark mode'
    exit 0 ;;
esac

if [ "${1:-}" != --locked ]; then
  exec python3 "$ROOT/scripts/lib/locked.py" "$ROOT/.build/locks/xcode.lock" -- "$0" --locked "$@"
fi
shift

[ "$(uname -s)" = Darwin ] || { echo 'Avatar previews require macOS and Xcode.' >&2; exit 1; }
command -v ffmpeg >/dev/null || { echo 'ffmpeg is required. Ask before installing it.' >&2; exit 1; }
mkdir -p "$ROOT/.build/avatar-preview"
RUN=$(mktemp -d "$ROOT/.build/avatar-preview/run.XXXXXX")
CHILD=
cleanup() {
  if [ -n "$CHILD" ] && kill -0 "$CHILD" 2>/dev/null; then
    kill -TERM "$CHILD" 2>/dev/null || true
    wait "$CHILD" 2>/dev/null || true
  fi
  rm -rf "$RUN"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

# Use the existing build lock and the same native sources as the app compositor checks.
if ! "$ROOT/scripts/check.sh" --locked macos > "$RUN/build.log" 2>&1; then
  tail -60 "$RUN/build.log" >&2
  exit 1
fi
PRODUCTS="$ROOT/.build/xcode-test/Build/Products/Debug"
xcrun swiftc -swift-version 6 -O -parse-as-library -module-name AvatarPreview \
  -I "$PRODUCTS" -I "$PRODUCTS/include" -I "$ROOT/packages/CSQLite" \
  -Xcc -fmodule-map-file="$ROOT/packages/CSQLite/module.modulemap" \
  "$ROOT"/apps/Polymux/Sources/Avatar/*.swift \
  "$ROOT"/apps/Polymux/Sources/AvatarUI/Avatar{Canvas,Layers,Plane,View}.swift \
  "$HERE/Preview.swift" \
  "$PRODUCTS/AvatarKit.o" "$PRODUCTS/PolymuxKit.o" "$PRODUCTS/IrohLib.o" \
  "$PRODUCTS/libiroh_ffi.a" -lsqlite3 -framework SystemConfiguration -o "$RUN/preview"
"$RUN/preview" "$@" &
CHILD=$!
wait "$CHILD"
CHILD=
