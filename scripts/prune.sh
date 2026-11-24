#!/bin/sh
# Remove regenerable build output from idle agent worktrees (the dev checkout is never touched).
# Usage: scripts/prune.sh [--apply] [--days N]   (dry run unless --apply; N defaults to 2)
# A worktree is idle when nothing in it changed for N days, no process names its path, and
# nobody holds its build lock. Sources and the worktree itself are left alone; the next build is cold.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
apply=0 days=2
while [ $# -gt 0 ]; do
  case $1 in
    --apply) apply=1 ;;
    --days) shift; days=${1:?--days needs a number} ;;
    *) echo "usage: $0 [--apply] [--days N]" >&2; exit 2 ;;
  esac
  shift
done
case $days in '' | *[!0-9]*) echo "--days needs a number" >&2; exit 2 ;; esac

main=$(git -C "$ROOT" worktree list --porcelain | awk '
  /^worktree / { path = substr($0, 10) }
  /^branch refs\/heads\/dev$/ { print path; exit }')
[ -n "$main" ] || { echo "No worktree has dev checked out." >&2; exit 1; }

total=0
for tree in $(git -C "$main" worktree list --porcelain | awk '/^worktree / { print substr($0, 10) }'); do
  [ "$tree" != "$main" ] && [ -d "$tree/.build" ] || continue
  # Only worktrees under the agent root, never an unrelated checkout.
  case $tree in "$(dirname "$main")"/.worktrees/*) ;; *) continue ;; esac
  find "$tree" -path "$tree/.git" -prune -o -mtime -"$days" -print -quit | grep -q . && continue
  pgrep -f -- "$tree/" >/dev/null 2>&1 && continue
  lsof -t "$tree/.build/locks/xcode.lock" >/dev/null 2>&1 && continue
  kb=0
  for dir in "$tree"/.build/xcode-* "$tree"/.build/repositories "$tree"/.build/checkouts "$tree"/.build/artifacts \
             "$tree"/.build/out "$tree"/.build/index-build; do
    [ -d "$dir" ] && [ ! -L "$dir" ] || continue
    kb=$((kb + $(du -sk "$dir" | cut -f1)))
    [ "$apply" = 1 ] && rm -rf "$dir"
  done
  [ "$kb" -gt 0 ] || continue
  total=$((total + kb))
  printf '%s %s MB  %s\n' "$([ "$apply" = 1 ] && echo pruned || echo would-prune)" "$((kb / 1024))" "$tree"
done
printf 'total %s MB%s\n' "$((total / 1024))" "$([ "$apply" = 1 ] || echo ' (dry run; pass --apply)')"
