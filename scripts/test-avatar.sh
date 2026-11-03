#!/bin/bash
# Check the native avatar compositor without launching the app.
set -euo pipefail
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
if [ "${1:-}" != --locked ]; then
  exec python3 "$ROOT/scripts/lib/locked.py" "$ROOT/.build/locks/xcode.lock" -- "$0" --locked "$@"
fi
shift
cd "$ROOT"
scripts/check.sh --locked macos
PRODUCTS="$ROOT/.build/xcode-test/Build/Products/Debug"
OUT=$(mktemp -d -t polymux-avatar)
trap 'rm -rf "$OUT"' EXIT
xcrun swiftc -swift-version 6 -parse-as-library -module-name AvatarRenderChecks \
  -I "$PRODUCTS" -I "$PRODUCTS/include" -I packages/CSQLite \
  -Xcc -fmodule-map-file="$ROOT/packages/CSQLite/module.modulemap" \
  apps/Polymux/Sources/Avatar/*.swift \
  apps/Polymux/Sources/AvatarUI/Avatar{Canvas,Layers,Plane,View}.swift \
  apps/Polymux/Tests/AvatarTests/RenderChecks.swift \
  "$PRODUCTS/AvatarKit.o" "$PRODUCTS/PolymuxKit.o" "$PRODUCTS/IrohLib.o" \
  "$PRODUCTS/libiroh_ffi.a" -lsqlite3 -framework SystemConfiguration -o "$OUT/checks"
python3 - "$OUT/checks" <<'PY'
import os, subprocess, sys
# CI's virtual GPU takes 50 to 80 seconds; a real one takes about 3.
limit = 180 if os.environ.get("CI") else 30
checks = subprocess.Popen([sys.argv[1]])
try:
    code = checks.wait(timeout=limit)
except subprocess.TimeoutExpired:
    # The timeout alone can't say where it stuck.
    subprocess.run(["sample", str(checks.pid), "3", "-file", "/dev/stdout"])
    checks.kill()
    raise SystemExit(f"Avatar rendering stalled for more than {limit} seconds")
raise SystemExit(code)
PY
