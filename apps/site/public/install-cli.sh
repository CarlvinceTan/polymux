#!/bin/sh
set -eu

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

main() {
  [ "$(/usr/bin/uname -s)" = Darwin ] && [ "$(/usr/sbin/sysctl -n hw.optional.arm64 2>/dev/null)" = 1 ] ||
    fail "This installer requires an Apple silicon Mac."
  bin="$HOME/.local/bin"
  destination="$bin/polymux"
  [ ! -e "$destination" ] && [ ! -L "$destination" ] ||
    fail "Polymux CLI is already installed at $destination. No files were changed."

  /bin/mkdir -p "$bin"
  work=$(/usr/bin/mktemp -d "$bin/.polymux.XXXXXX")
  trap '/bin/rm -rf "$work"' EXIT
  trap 'exit 1' HUP INT TERM
  asset=https://github.com/CarlvinceTan/polymux/releases/latest/download/polymux-cli-darwin-arm64.zip
  /usr/bin/curl --proto '=https' --proto-redir '=https' -fsSL "$asset" -o "$work/cli.zip" ||
    fail "A stable CLI download is not available. See https://polymux.com/docs/installation/."
  /usr/bin/unzip -p "$work/cli.zip" polymux > "$work/polymux"
  /bin/chmod 755 "$work/polymux"
  /usr/bin/codesign --verify --strict \
    -R='anchor apple generic and identifier "com.flarehq.polymux.cli" and certificate leaf[subject.OU] = "23YB4896XA" and certificate leaf[field.1.2.840.113635.100.6.1.13] exists' \
    "$work/polymux"
  /bin/ln -h "$work/polymux" "$bin/" || fail "Could not install the CLI without replacing an existing file."

  case ":$PATH:" in
    *":$bin:"*) printf '%s\n' "Installed Polymux CLI. Run: polymux app download" ;;
    *) printf '%s\n' "Installed Polymux CLI. Run: \"$destination\" app download" ;;
  esac
}

main </dev/null
