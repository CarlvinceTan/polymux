#!/bin/sh
set -eu

mode="${1:-cli}"
case "$mode" in
  cli|host|connect) ;;
  *)
    echo "Usage: install.sh [cli|host|connect INVITATION]" >&2
    exit 2
    ;;
esac

if ! command -v curl >/dev/null 2>&1; then
  echo "Polymux installation requires curl." >&2
  exit 1
fi
invitation="${2:-}"
if [ "$mode" = "connect" ]; then
  case "$invitation" in
    ""|*[!A-Za-z0-9_-]*) echo "Paste the complete installation command from Devices." >&2; exit 2 ;;
  esac
fi
archive="polymux-cli.tar.gz"
base="https://github.com/CarlvinceTan/polymux/releases/latest/download"
asset_revision="20260831-1"
node_version="24.20.0"
case "$(uname -s 2>/dev/null)" in
  Darwin) platform="darwin" ;;
  Linux) platform="linux" ;;
  *) echo "Polymux Host currently supports Linux and macOS." >&2; exit 1 ;;
esac
case "$(uname -m 2>/dev/null)" in
  x86_64|amd64) architecture="x64" ;;
  arm64|aarch64) architecture="arm64" ;;
  *) echo "Polymux does not yet provide a runtime for this processor." >&2; exit 1 ;;
esac
node_archive="node-v${node_version}-${platform}-${architecture}.tar.gz"
node_base="https://nodejs.org/dist/v${node_version}"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/polymux-install.XXXXXX")"
cleanup() {
  rm -rf "$temporary"
}
trap cleanup EXIT HUP INT TERM

verify_checksum() {
  checksum_file="$1"
  target_file="$2"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$temporary" && sha256sum -c "$checksum_file")
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$temporary" && shasum -a 256 -c "$checksum_file")
  else
    echo "Polymux could not verify the download because no SHA-256 tool is installed." >&2
    exit 1
  fi
  test -f "$temporary/$target_file"
}

curl -fL "$base/$archive?revision=$asset_revision" -o "$temporary/$archive"
curl -fL "$base/$archive.sha256?revision=$asset_revision" -o "$temporary/$archive.sha256"
verify_checksum "$archive.sha256" "$archive"
tar -xzf "$temporary/$archive" -C "$temporary"

prefix="${POLYMUX_INSTALL_DIR:-$HOME/.local}"
library="$prefix/share/polymux-cli"
bin="$prefix/bin"
runtime="$library/node-v${node_version}-${platform}-${architecture}"
mkdir -p "$library" "$bin"

if [ ! -x "$runtime/bin/node" ]; then
  curl -fL "$node_base/$node_archive" -o "$temporary/$node_archive"
  curl -fL "$node_base/SHASUMS256.txt" -o "$temporary/SHASUMS256.txt"
  expected="$(awk -v file="$node_archive" '$2 == file {print $1; exit}' "$temporary/SHASUMS256.txt")"
  if [ -z "$expected" ]; then
    echo "Polymux could not verify the portable Node.js runtime." >&2
    exit 1
  fi
  printf '%s  %s\n' "$expected" "$node_archive" > "$temporary/node.sha256"
  verify_checksum "node.sha256" "$node_archive"
  tar -xzf "$temporary/$node_archive" -C "$temporary"
  rm -rf "$runtime"
  mv "$temporary/node-v${node_version}-${platform}-${architecture}" "$runtime"
fi

install -m 755 "$temporary/polymux.mjs" "$library/polymux.mjs"
node_path="$runtime/bin/node"
launcher="$bin/polymux"
{
  printf '%s\n' '#!/bin/sh'
  printf 'exec "%s" "%s" "$@"\n' "$node_path" "$library/polymux.mjs"
} > "$launcher"
chmod 755 "$launcher"

echo "Installed Polymux CLI and its private runtime at $launcher"
case ":${PATH}:" in
  *":$bin:"*) ;;
  *) echo "Add $bin to PATH to run polymux directly." ;;
esac

if [ "$mode" = "host" ]; then
  "$launcher" host install
fi

if [ "$mode" = "connect" ]; then
  "$launcher" connect "$invitation"
fi
