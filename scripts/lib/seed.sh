# shellcheck shell=sh
# Seed a fresh build folder with the dev checkout's resolved packages. An APFS clone costs no disk
# until written and skips the fetch, which also waits on SwiftPM's machine-wide cache lock.
# Usage: seed_packages <folder under .build>   (needs $ROOT; sets no variables the caller uses)
seed_packages() {
  seed_to="$ROOT/.build/$1/SourcePackages"
  [ ! -e "$seed_to" ] || return 0
  seed_main=$(git -C "$ROOT" worktree list --porcelain 2>/dev/null |
    awk '/^worktree / { path = substr($0, 10) } /^branch refs\/heads\/dev$/ { print path; exit }')
  [ -n "$seed_main" ] && [ "$seed_main" != "$ROOT" ] && [ -d "$seed_main/.build/$1/SourcePackages" ] || return 0
  mkdir -p "$ROOT/.build/$1"
  cp -cR "$seed_main/.build/$1/SourcePackages" "$seed_to" 2>/dev/null || rm -rf "$seed_to"
}
