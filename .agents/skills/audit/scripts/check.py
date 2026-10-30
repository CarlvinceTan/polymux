#!/usr/bin/env python3
"""Read-only, diff-scoped checks for the Polymux audit skill."""

import argparse
import ast
import json
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Finding:
    level: str
    rule: str
    path: str
    line: int
    message: str
    source: str


PATTERNS = (
    ("FAIL", "conflict", re.compile(r"^(?:<{7}|>{7})(?: |$)"), None,
     "Resolve the apparent merge conflict marker.", "Git merge format"),
    ("FAIL", "whitespace", re.compile(r"[ \t]+\r?$"), None,
     "Remove trailing whitespace.", "Git patch hygiene"),
    ("REVIEW", "private-key", re.compile(r"-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----"), None,
     "Check for an exposed private key; fixtures and examples need context.", "Security review"),
    ("REVIEW", "fixed-size", re.compile(r"\.fixedSize\s*\("), ".swift",
     "Check that this is not text in a flexible row or panel.", "DESIGN.md / Text"),
    ("REVIEW", "alert", re.compile(r"\.alert\s*\(|\bNSAlert\s*\("), ".swift",
     "Check that this alert is not for a recoverable failure.", "DESIGN.md / Status and errors"),
    ("REVIEW", "retry-button", re.compile(r'\bButton\s*\(\s*"Retry"'), ".swift",
     "Check for a prohibited retry button.", "DESIGN.md / Status and errors"),
)

LOCKS = {
    "Package.swift": "Package.resolved",
    "apps/Polymux/Package.swift": "apps/Polymux/Package.resolved",
    "apps/site/package.json": "apps/site/package-lock.json",
    "apps/server/go.mod": "apps/server/go.sum",
}


def git(root, *args, optional=False):
    result = subprocess.run(
        ["git", "--no-optional-locks", "-c", "core.quotePath=false", "-C", str(root), *args],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        env={**os.environ, "GIT_OPTIONAL_LOCKS": "0"},
    )
    if result.returncode and not optional:
        raise ValueError("Git could not read the requested repository or scope.")
    return result


def decode(data):
    return data.decode("utf-8", errors="surrogateescape")


def paths(data):
    return [decode(path) for path in data.split(b"\0") if path]


def added_lines(patch):
    path, number = None, None
    for line in decode(patch).split("\n"):
        if line.startswith("diff --git "):
            path, number = None, None
        elif number is None and line.startswith("+++ "):
            value = line[4:].rstrip("\t")
            value = ast.literal_eval(value) if value.startswith('"') else value
            path = None if value == "/dev/null" else value[2:]
        elif line.startswith("@@ "):
            match = re.match(r"@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@", line)
            number = int(match[1]) if match else None
        elif path is not None and number is not None:
            if line.startswith("+"):
                yield path, number, line[1:]
                number += 1
            elif line.startswith(" "):
                number += 1


def scan(lines):
    findings = []
    for path, number, text in lines:
        for level, rule, pattern, suffix, message, source in PATTERNS:
            if (suffix is None or path.endswith(suffix)) and pattern.search(text):
                findings.append(Finding(level, rule, path, number, message, source))
    return findings


def commit(root, ref):
    result = git(root, "rev-parse", "--verify", "--end-of-options", ref + "^{commit}", optional=True)
    if result.returncode:
        raise ValueError("Cannot resolve commit reference: " + json.dumps(ref))
    return decode(result.stdout).strip()


def snapshot(root, path, target):
    if target is None:
        file = root / path
        if file.is_symlink() or not file.is_file():
            return None
        return decode(file.read_bytes())
    result = git(root, "cat-file", "blob", target + ":" + path, optional=True)
    return decode(result.stdout) if result.returncode == 0 else None


def versions(root, changed, target):
    files = ("go.work", "apps/server/go.mod")
    if not set(files).intersection(changed):
        return []
    findings, directives = [], {}
    for path in files:
        content = snapshot(root, path, target)
        if content is None:
            continue
        match = re.search(r"(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*(?://.*)?$", content)
        if not match:
            findings.append(Finding("FAIL", "go-version", path, 0,
                                    "Missing or invalid Go directive.", "AGENTS.md / Versions and Upgrades"))
            continue
        directives[path] = match[1]
        toolchain = re.search(r"(?m)^toolchain\s+go(\d+\.\d+(?:\.\d+)?)\s*(?://.*)?$", content)
        if toolchain:
            version = lambda value: tuple((list(map(int, value.split("."))) + [0])[:3])
            lagging = version(toolchain[1]) < version(match[1])
            findings.append(Finding("FAIL" if lagging else "REVIEW", "go-toolchain", path, 0,
                                    "Lagging toolchain directive." if lagging else
                                    "Confirm this toolchain directive preserves one Go version.",
                                    "AGENTS.md / Versions and Upgrades"))
    if len(set(directives.values())) > 1:
        findings.append(Finding("FAIL", "go-version", "go.work", 0,
                                "Go directives differ between go.work and apps/server/go.mod.",
                                "AGENTS.md / Versions and Upgrades"))
    return findings


def check(root, args):
    filters = []
    for path in args.paths:
        if Path(path).is_absolute() or ".." in Path(path).parts:
            raise ValueError("Paths must be repository-relative and cannot contain '..'.")
        filters.append(":(literal)" + path)
    if args.base is not None:
        head = "HEAD" if args.head is None else args.head
        base, target = commit(root, args.base), commit(root, head)
        scope, refs = "range " + base + ".." + target, [base, target]
    elif args.staged:
        scope, refs, target = "staged index", ["--cached"], ""
    else:
        scope, refs, target = "worktree against HEAD (including untracked files)", [commit(root, "HEAD")], None
    options = ["--no-ext-diff", "--no-textconv", "--no-renames"]
    changed = set(paths(git(root, "diff", *options, "--name-only", "-z", *refs, "--", *filters).stdout))
    patch = git(root, "diff", *options, "--no-color", "--unified=0", "--src-prefix=a/",
                "--dst-prefix=b/", *refs, "--", *filters).stdout
    findings = scan(added_lines(patch))
    skipped = 0
    if target is None:
        untracked = paths(git(root, "ls-files", "--others", "--exclude-standard", "-z", "--", *filters).stdout)
        changed.update(untracked)
        for path in untracked:
            file = root / path
            if file.is_symlink() or not file.is_file():
                skipped += 1
                continue
            data = file.read_bytes()
            if b"\0" in data:
                skipped += 1
                continue
            findings.extend(scan((path, number, line) for number, line in
                                 enumerate(decode(data).split("\n"), 1)))
    unmerged = []
    if args.base is None:
        unmerged = paths(git(root, "diff", "--name-only", "--diff-filter=U", "-z", "--", *filters).stdout)
    for path in unmerged:
        findings.append(Finding("FAIL", "unmerged", path, 0, "Resolve the unmerged index entry.", "Git index"))
    for path in sorted(changed):
        if path in ("AGENTS.md", "DESIGN.md") or Path(path).name == "README.md":
            findings.append(Finding("REVIEW", "permission", path, 0,
                                    "Confirm the user authorized this protected-file change.",
                                    "AGENTS.md / Working on Polymux"))
        if path in LOCKS and LOCKS[path] not in changed:
            findings.append(Finding("REVIEW", "lockfile", path, 0,
                                    "If dependencies changed, check the unchanged lockfile: " + LOCKS[path],
                                    "AGENTS.md / Versions and Upgrades"))
    findings.extend(versions(root, changed, target))
    print("Scope: " + scope)
    print("Changed paths: " + str(len(changed)))
    findings = set(findings)
    for item in sorted(findings, key=lambda item: (item.path, item.line, item.rule)):
        location = json.dumps(item.path) + (":" + str(item.line) if item.line else "")
        print(f"{item.level} {location} [{item.rule}] {item.message} ({item.source})")
    failures = sum(item.level == "FAIL" for item in findings)
    reviews = sum(item.level == "REVIEW" for item in findings)
    print(f"Fast checks: {failures} failure(s), {reviews} review candidate(s), {skipped} untracked binary/symlink skip(s).")
    print("Not a full audit: no builds, tests, exhaustive secret scan, or contextual review; tracked binary content is not scanned.")
    return 1 if failures else 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--staged", action="store_true", help="inspect index content only")
    group.add_argument("--worktree", action="store_true", help="inspect HEAD to working tree plus untracked files (default)")
    group.add_argument("--base", help="inspect this commit to --head (default HEAD), excluding local edits")
    parser.add_argument("--head", help="range endpoint; requires --base")
    parser.add_argument("paths", nargs="*", help="optional literal repository-relative files or directories")
    args = parser.parse_args()
    if args.head is not None and args.base is None:
        parser.error("--head requires --base")
    try:
        root = Path(decode(git(Path.cwd(), "rev-parse", "--show-toplevel").stdout).strip())
        return check(root, args)
    except (OSError, ValueError, SyntaxError) as error:
        print("UNVERIFIED: " + str(error), file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
