#!/bin/sh
# Compile the Tauri frontend and backend without launching the app.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
npm run verify --prefix "$ROOT/apps/tauri"
cargo check --locked --manifest-path "$ROOT/apps/tauri/src-tauri/Cargo.toml"
