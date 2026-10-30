#!/bin/sh
# Capture the app on a fresh simulator; its device and app are removed on exit.
# Usage: scripts/sim.sh out.png [app flags...]
# POLYMUX_SIM_DEVICE selects the source device's hardware/runtime, never its data.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
exec python3 "$ROOT/scripts/sim.py" "$@"
