#!/bin/sh
# Hand a server update to an independent process; safe to invoke from a Polymux agent.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
case ${1:-} in
  '') installer="$ROOT/scripts/install.sh" ;;
  --personal) installer="$ROOT/scripts/local/install.sh" ;;
  *) echo 'usage: scripts/update.sh [--personal]' >&2; exit 2 ;;
esac
[ "$#" -le 1 ] || exit 2
[ -f "$installer" ] || { echo 'Installer is unavailable.' >&2; exit 1; }
mkdir -p "$ROOT/.build"
python3 - "$ROOT" "$installer" <<'PY'
import pathlib, subprocess, sys
root = pathlib.Path(sys.argv[1])
log = root / '.build/update.log'
with log.open('a') as output:
    process = subprocess.Popen(
        [sys.executable, str(root / 'scripts/lib/locked.py'), str(root / '.build/update.lock'),
         '--', '/bin/bash', sys.argv[2], 'server'],
        stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT,
        start_new_session=True, close_fds=True,
    )
print(f'Update handed off to process {process.pid}. Do not wait for it from the requesting agent turn.')
print(f'Progress and any failure: {log}')
PY
