# Checklist output for the local scripts. Source it; run each stage with: step "Label" command args...
# A step's own output goes to a log and is shown only when the step fails.
LOG=$(mktemp -t polymux)
trap 'rm -f "$LOG"' EXIT
if [ -t 1 ]; then TTY=1; DIM=$(printf '\033[2m'); RED=$(printf '\033[31m'); GREEN=$(printf '\033[32m'); OFF=$(printf '\033[0m')
else TTY=0; DIM=""; RED=""; GREEN=""; OFF=""; fi

step() {
  label=$1; shift
  started=$(date +%s)
  : >"$LOG"
  [ "$TTY" = 1 ] && printf '  %s…%s %s' "$DIM" "$OFF" "$label"
  set +e
  ( set -e; "$@" ) >>"$LOG" 2>&1
  rc=$?
  set -e
  took=$(( $(date +%s) - started ))
  suffix=""; [ "$took" -ge 3 ] && suffix=" ${DIM}(${took}s)${OFF}"
  [ "$TTY" = 1 ] && printf '\r\033[K'
  if [ "$rc" = 0 ]; then
    printf '  %s✓%s %s%s\n' "$GREEN" "$OFF" "$label" "$suffix"
  else
    printf '  %s✗%s %s\n\n' "$RED" "$OFF" "$label" >&2
    tail -n 25 "$LOG" >&2
    exit 1
  fi
}
