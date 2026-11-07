#!/bin/sh
# Native Control helper. Agents and the JS client use this exact executable.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
case $# in 1|2) ;; *) echo 'usage: scripts/control.sh permissions|desktop|request-accessibility|windows|inspect|capture|session [JSON request]' >&2; exit 2 ;; esac
exec python3 "$ROOT/scripts/control.py" "$@"
