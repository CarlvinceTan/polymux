# shellcheck shell=sh
# Source from an installer after building, before stopping or replacing the server.
DRAIN_ID=
DRAIN_BINARY=
DRAIN_URL=
DRAIN_PID=
DRAIN_STOPPED=0

drain_server() {
  DRAIN_BINARY=$1
  DRAIN_URL=$2
  [ -f "$PLIST" ] || return 0
  DRAIN_PID=$(launchctl print "$DOMAIN/$LABEL" 2>/dev/null | awk '$1 == "pid" { print $3 }')
  [ -n "$DRAIN_PID" ] || { echo 'Cannot confirm the existing server process; no update applied.' >&2; return 1; }
  "$DRAIN_BINARY" drain --url "$DRAIN_URL" --status >/dev/null || return 1
  DRAIN_ID=$(uuidgen) || return 1
  echo "Update identity: $DRAIN_ID" >&2
  "$DRAIN_BINARY" drain --url "$DRAIN_URL" --id "$DRAIN_ID" || return 1
  current=$(launchctl print "$DOMAIN/$LABEL" 2>/dev/null | awk '$1 == "pid" { print $3 }')
  [ "$current" = "$DRAIN_PID" ] || { echo 'Server changed while draining; no update applied.' >&2; return 1; }
}

verify_server() {
  expected=$("$DRAIN_BINARY" version) || return 1
  attempts=0
  set --
  if [ -n "$DRAIN_ID" ]; then set -- --ready; fi
  until "$DRAIN_BINARY" drain --url "$DRAIN_URL" --status --build "$expected" "$@" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 30 ] || { echo 'Candidate health failed; admission remains paused.' >&2; return 1; }
    sleep 1
  done
  stable=$(launchctl print "$DOMAIN/$LABEL" 2>/dev/null | awk '$1 == "pid" { print $3 }')
  [ -n "$stable" ] || return 1
  sample=0
  while [ "$sample" -lt 5 ]; do
    sleep 3
    current=$(launchctl print "$DOMAIN/$LABEL" 2>/dev/null | awk '$1 == "pid" { print $3 }')
    [ "$stable" = "$current" ] || return 1
    "$DRAIN_BINARY" drain --url "$DRAIN_URL" --status --build "$expected" "$@" >/dev/null || return 1
    sample=$((sample + 1))
  done
}

# Once stopped, leave admission paused until authenticated health succeeds.
resume_drain() {
  if [ -n "$DRAIN_ID" ]; then
    "$DRAIN_BINARY" drain --url "$DRAIN_URL" --id "$DRAIN_ID" --cancel || return 1
  fi
  DRAIN_ID=
}

cancel_drain() {
  if [ "$DRAIN_STOPPED" = 0 ] && [ -n "$DRAIN_ID" ]; then
    resume_drain || echo "Could not confirm admission resumed. Inspect status; retry with: $DRAIN_BINARY drain --url $DRAIN_URL --id $DRAIN_ID --cancel" >&2
  fi
}
