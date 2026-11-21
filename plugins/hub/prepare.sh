#!/bin/sh
# Fetch or build the programs Hub runs beside its service. Usage: plugins/hub/prepare.sh <target> <out dir>
# <target> is darwin-arm64, linux-amd64 or linux-arm64. Needs gh, git, go, python3, and cargo and brew for macOS.
set -eu
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
TARGET=${1:?usage: prepare.sh <target> <out dir>}
OUT=${2:?usage: prepare.sh <target> <out dir>}
# Tuwunel publishes Linux builds only, so macOS compiles the pinned tag.
TUWUNEL_TAG=v1.9.4
mkdir -p "$OUT"
# Building libsignal on macOS needs protoc.
case "$TARGET" in darwin-*) command -v protoc >/dev/null || brew install protobuf ;; esac
python3 "$HERE/fetch-binaries.py" "$TARGET" "$OUT"
case "$TARGET" in
  darwin-*)
    SRC=$(mktemp -d)
    trap 'rm -rf "$SRC"' EXIT
    git clone --depth 1 --branch "$TUWUNEL_TAG" https://github.com/matrix-construct/tuwunel.git "$SRC"
    (cd "$SRC" && cargo build --release --locked --no-default-features \
      --features brotli_compression,gzip_compression,zstd_compression,media_thumbnail,url_preview -p tuwunel)
    cp "$SRC/target/release/tuwunel" "$OUT/tuwunel"
    ;;
  linux-amd64|linux-arm64)
    case "$TARGET" in linux-amd64) ASSET=x86_64-v2 ;; *) ASSET=aarch64-v8 ;; esac
    NAME="$TUWUNEL_TAG-release-all-$ASSET-linux-gnu-tuwunel.zst"
    FILE=$(mktemp)
    trap 'rm -f "$FILE"' EXIT
    gh release download "$TUWUNEL_TAG" -R matrix-construct/tuwunel -p "$NAME" -O "$FILE" --clobber
    zstd -d -f "$FILE" -o "$OUT/tuwunel"
    ;;
  *) echo "Unknown target $TARGET." >&2; exit 2 ;;
esac
chmod 755 "$OUT"/*
