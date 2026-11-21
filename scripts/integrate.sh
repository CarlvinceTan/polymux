#!/bin/sh
# Integrate one worktree into dev under the machine-wide dev lock.
# Usage: scripts/integrate.sh [worktree]
# Run it from the worktree, or pass its path. dev must be clean.
set -eu

usage() {
	echo "usage: $0 [worktree]" >&2
}

case "${1:-}" in
	-h | --help)
		usage
		exit 0
		;;
esac

# The outer pass only finds the lock owner and the lock path.
if [ "${1:-}" != --locked ]; then
	if [ -n "${1:-}" ]; then
		source=$(git -C "$1" rev-parse --show-toplevel 2>/dev/null) || {
			echo "Not a git worktree: $1" >&2
			exit 2
		}
	else
		source=$(git rev-parse --show-toplevel 2>/dev/null) || {
			echo "Run this from a git worktree, or pass one." >&2
			exit 2
		}
	fi
	main=$(git -C "$source" worktree list --porcelain | awk '
		/^worktree / { path = substr($0, 10) }
		/^branch refs\/heads\/dev$/ { print path; exit }
	')
	if [ -z "$main" ]; then
		echo "No worktree has dev checked out." >&2
		exit 1
	fi
	mkdir -p "$main/.build/locks"
	echo "Waiting for the dev integration lock..."
	exec python3 "$main/scripts/lib/locked.py" "$main/.build/locks/dev.lock" -- "$0" --locked "$@"
fi
shift

if [ $# -gt 0 ]; then
	source=$(git -C "$1" rev-parse --show-toplevel 2>/dev/null) || {
		echo "Not a git worktree: $1" >&2
		exit 2
	}
else
	source=$(git rev-parse --show-toplevel 2>/dev/null) || {
		echo "Run this from a git worktree, or pass one." >&2
		exit 2
	}
fi

main=$(git -C "$source" worktree list --porcelain | awk '
	/^worktree / { path = substr($0, 10) }
	/^branch refs\/heads\/dev$/ { print path; exit }
')
if [ -z "$main" ]; then
	echo "No worktree has dev checked out." >&2
	exit 1
fi
if [ "$source" = "$main" ]; then
	echo "Run this from the worktree you want to integrate, not from the dev checkout." >&2
	exit 1
fi

branch=$(git -C "$source" symbolic-ref --quiet --short HEAD 2>/dev/null || true)
case "$branch" in
	experimental/*)
		echo "Refusing to integrate experimental branch $branch." >&2
		exit 1
		;;
esac

if git -C "$main" rev-parse -q --verify MERGE_HEAD >/dev/null 2>&1 ||
	git -C "$main" rev-parse -q --verify REBASE_HEAD >/dev/null 2>&1; then
	echo "dev is in the middle of a merge or rebase." >&2
	exit 1
fi
if [ -n "$(git -C "$main" status --porcelain)" ]; then
	echo "dev ($main) has uncommitted changes. Commit or move them before integrating." >&2
	exit 1
fi
if [ -n "$(git -C "$source" status --porcelain)" ]; then
	echo "$source has uncommitted changes. Commit them before integrating." >&2
	exit 1
fi

ahead=$(git -C "$source" rev-list --count "dev..HEAD")
if [ "$ahead" -eq 0 ]; then
	echo "Nothing to integrate: $source has no commits ahead of dev."
	exit 0
fi

old=$(git -C "$main" rev-parse --short HEAD)
echo "Rebasing $source onto dev ($old)..."
if ! GIT_EDITOR=true git -C "$source" rebase dev; then
	echo "Rebase stopped. Resolve it in $source, run git -C \"$source\" rebase --continue, then rerun this script." >&2
	exit 1
fi

echo "Running checks in $source..."
"$source/scripts/check.sh" all
"$source/scripts/test.sh" swift
"$source/scripts/test.sh" server ./internal/... ./cmd/...
"$source/scripts/test.sh" packages
"$source/scripts/test.sh" plugins
"$source/scripts/test.sh" tools

new=$(git -C "$source" rev-parse HEAD)
if ! git -C "$main" merge --ff-only "$new" >/dev/null; then
	echo "Could not fast-forward dev; it may have moved while the lock was held." >&2
	exit 1
fi
echo "Integrated $source into dev: $old..$(git -C "$main" rev-parse --short HEAD)"
