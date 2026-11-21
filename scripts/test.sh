#!/bin/sh
# Run unit tests. Usage: scripts/test.sh [server|packages|plugins|swift|tools|all] [test args...]
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TARGET=${1:-all}
[ $# -gt 0 ] && shift

# The store needs SQLite FTS5, which go-sqlite3 enables only with this tag.
server() { (cd "$ROOT/apps/server" && go test -tags sqlite_fts5 "${@:-./...}"); }
package_tests() {
  for package in runtime memory search vault; do
    (cd "$ROOT/packages/$package" && GOWORK=off go test "${@:-./...}")
  done
}
plugin_tests() { (cd "$ROOT/plugins" && GOWORK=off go test "${@:-./...}"); }
swift_tests() { (cd "$ROOT" && swift test "$@"); }
tool_tests() {
  for test in "$ROOT"/scripts/*_test.py; do
    basename "$test"
    python3 -B "$test"
  done
  python3 -B "$ROOT/scripts/docs_check.py"
}

case "$TARGET" in
  server) server "$@" ;;
  packages) package_tests "$@" ;;
  plugins) plugin_tests "$@" ;;
  swift) swift_tests "$@" ;;
  tools) tool_tests ;;
  all)
    [ $# = 0 ] || { echo 'Choose server, packages, plugins or swift when passing test arguments.' >&2; exit 2; }
    server; package_tests; plugin_tests; swift_tests; tool_tests ;;
  *) echo "usage: $0 [server|packages|plugins|swift|tools|all] [test args...]" >&2; exit 2 ;;
esac
