#!/bin/sh
# Keep this Mac's server on its update channel, chosen in Settings: Nightly is the newest dev
# commit whose server tests passed in CI, and Stable is the latest published release.
# Usage: scripts/deploy.sh setup   once, from a checkout; it then runs every 30 seconds
#        polymux-deploy            one deploy pass, as the login job runs it
#        polymux-deploy now        update now, even when server updates are Manual
set -eu
SRC="$HOME/.polymux/src"
STATE="$HOME/.polymux/deploy"
BIN=${POLYMUX_BIN_DIR:-"$HOME/.local/bin"}
AGENTS="$HOME/Library/LaunchAgents"
SERVER=com.flarehq.polymux.server
LABEL=com.flarehq.polymux.deploy
DOMAIN="gui/$(id -u)"
BRANCH=dev
CHECK=server

log() { echo "$(date '+%F %T') $*"; }

setup() {
  url=$(git -C "$(dirname -- "$0")" remote get-url origin | sed 's#^git@github.com:#https://github.com/#')
  [ -d "$SRC/.git" ] || git clone --quiet --branch "$BRANCH" "$url" "$SRC"
  mkdir -p "$BIN" "$STATE" "$AGENTS" "$HOME/.polymux/logs"
  install -S -m 0755 "$0" "$BIN/polymux-deploy"
  cat >"$AGENTS/$LABEL.plist" <<PLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BIN/polymux-deploy</string></array>
  <key>EnvironmentVariables</key><dict><key>PATH</key><string>$PATH</string></dict>
  <key>StartInterval</key><integer>30</integer>
  <key>RunAtLoad</key><true/>
  <key>StandardOutPath</key><string>$HOME/.polymux/logs/deploy.log</string>
  <key>StandardErrorPath</key><string>$HOME/.polymux/logs/deploy.log</string>
</dict>
</plist>
PLIST_EOF
  launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
  launchctl bootstrap "$DOMAIN" "$AGENTS/$LABEL.plist"
  echo "Deploying $BRANCH every 30 seconds. Log: ~/.polymux/logs/deploy.log"
}

repository() {
  git -C "$SRC" remote get-url origin | sed -E 's#^.*github\.com[:/]##; s#\.git$##'
}

channel() {
  plutil -extract settings.updateChannel raw -o - "$HOME/.polymux/data/settings.json" 2>/dev/null || echo nightly
}

# Automatic unless Settings turned server updates to Manual.
mode() {
  plutil -extract settings.updateMode raw -o - "$HOME/.polymux/data/settings.json" 2>/dev/null || echo automatic
}

# Prints success, pending, or the failed conclusion of the commit's CI check.
ci() {
  gh api "repos/$(repository)/commits/$1/check-runs?check_name=$CHECK" \
    --jq '.check_runs[0] | if . == null or .status != "completed" then "pending" else .conclusion end'
}

rollback() {
  [ -f "$STATE/polymux.prev" ] || return 1
  # Only a sealed candidate can be replaced; never roll back over newly admitted work.
  . "$SRC/scripts/lib/drain.sh"
  LABEL=$SERVER
  PLIST="$AGENTS/$SERVER.plist"
  DRAIN_ID=$(cat "$HOME/.polymux/data/maintenance" 2>/dev/null) || return 1
  DRAIN_URL="http://127.0.0.1:7337"
  DRAIN_BINARY="$STATE/polymux.prev"
  "$DRAIN_BINARY" drain --url "$DRAIN_URL" --id "$DRAIN_ID" || return 1
  launchctl bootout "$DOMAIN/$SERVER" || return 1
  install -m 0755 "$STATE/polymux.prev" "$BIN/polymux" || return 1
  launchctl bootstrap "$DOMAIN" "$PLIST" || return 1
  verify_server && resume_drain
}

deploy() {
  # A manual pass (polymux-deploy now) updates even when server updates are set to Manual.
  [ "${1:-}" = now ] || [ "$(mode)" != manual ] || return 0
  mkdir -p "$STATE"
  shlock -f "$STATE/lock" -p $$ || return 0
  trap 'rm -f "$STATE/lock"' EXIT
  last=$(cat "$STATE/sha" 2>/dev/null || true)
  if [ "$(channel)" = stable ]; then
    tag=$(gh release view --repo "$(repository)" --json tagName --jq .tagName) || return 0
    git -C "$SRC" fetch --quiet --force origin "refs/tags/$tag:refs/tags/$tag"
    sha=$(git -C "$SRC" rev-list -n 1 "$tag")
    # Newer data can't run on older code, so switching to Stable waits for the next release.
    if [ -n "$last" ] && git -C "$SRC" merge-base --is-ancestor "$sha" "$last" 2>/dev/null; then return 0; fi
  else
    git -C "$SRC" fetch --quiet origin "$BRANCH"
    sha=$(git -C "$SRC" rev-parse FETCH_HEAD)
  fi
  [ "$sha" != "$last" ] || return 0
  [ "$sha" != "$(cat "$STATE/failed-sha" 2>/dev/null || true)" ] || return 0
  # A published release has already passed its own checks.
  result=success
  if [ "$(channel)" != stable ]; then result=$(ci "$sha"); fi
  [ "$result" != pending ] || return 0
  if [ "$result" != success ]; then
    log "Skipped $sha: CI $CHECK $result"
    return 0
  else
    git -C "$SRC" checkout --quiet --force --detach "$sha"
    # Commits that leave the server alone don't restart it.
    if [ -n "$last" ] && git -C "$SRC" diff --quiet "$last" "$sha" -- apps/server packages/runtime packages/memory packages/search packages/vault scripts go.work go.work.sum; then
      log "No server changes in $sha"
    else
      cp "$BIN/polymux" "$STATE/polymux.prev" 2>/dev/null || true
      result=0
      "$SRC/scripts/install.sh" server || result=$?
      if [ "$result" = 75 ]; then
        log "Deferred $sha: preparation or safe drain did not complete"
        return 0
      fi
      if [ "$result" = 0 ]; then
        log "Deployed $sha"
      else
        echo "$sha" >"$STATE/failed-sha"
        if rollback; then
          log "Failed $sha, rolled back; commit quarantined"
        else
          log "Failed $sha; rollback is unhealthy, manual recovery required"
        fi
        return 1
      fi
    fi
    # install.sh backs up data on every deploy; keep the newest ten.
    ls -1dt "$HOME/.polymux/backups"/data-* 2>/dev/null | tail -n +11 | while read -r dir; do rm -rf "$dir"; done
    # The next pass runs this commit's deploy script.
    if [ -f "$SRC/scripts/deploy.sh" ]; then install -S -m 0755 "$SRC/scripts/deploy.sh" "$BIN/polymux-deploy"; fi
  fi
  echo "$sha" >"$STATE/sha"
}

case ${1:-deploy} in
  setup) setup ;;
  deploy) deploy ;;
  now) deploy now ;;
  *) echo "usage: $0 [setup|deploy|now]" >&2; exit 2 ;;
esac
