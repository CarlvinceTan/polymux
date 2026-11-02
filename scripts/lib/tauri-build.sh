#!/bin/sh
# Called under run.sh's build lock; stage a disposable app before releasing it.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
export CARGO_TARGET_DIR="$ROOT/.build/tauri"
cd "$ROOT/apps/tauri"
npm run tauri -- build --debug --bundles app
ditto "$CARGO_TARGET_DIR/debug/bundle/macos/Polymux.app" "$1"
