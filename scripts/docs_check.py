#!/usr/bin/env python3
"""Fail when README.md, AGENTS.md, DESIGN.md or docs/ point at things that no longer exist.

Checks: relative markdown links resolve, repository paths in backticks exist, and docs/README.md lists every guide.
Whether the prose is still true is not mechanical: that is the audit and pre-commit skills' job.
Usage: scripts/docs_check.py
"""

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DOCS = ["README.md", "AGENTS.md", "DESIGN.md", *sorted(str(p.relative_to(ROOT)) for p in (ROOT / "docs").glob("*.md"))]
LINK = re.compile(r"\[[^\]]*\]\(([^)\s]+)\)")
TICK = re.compile(r"`([^`\n]+)`")
# Built, per-machine or per-run locations are not in the checkout.
GENERATED = (".build/", "dist/", "node_modules/", "~/", "/tmp", "AGENTS.local.md", "scripts/local/", "ISSUE.md")
TOP = {path.name for path in ROOT.iterdir() if not path.name.startswith(".git")} | {".github", ".agents", ".githooks"}


def candidates(text):
    for token in TICK.findall(text):
        token = token.strip()
        if not re.fullmatch(r"[\w./@-]+", token) or "/" not in token or token.startswith(("/", "./", "../", "-")):
            continue
        if token.split("/")[0] not in TOP or token.startswith(GENERATED) or token.endswith("/..."):
            continue
        yield token


def main():
    problems = []
    for doc in DOCS:
        path = ROOT / doc
        text = path.read_text()
        for target in LINK.findall(text):
            target = target.split("#")[0]
            if not target or "://" in target or target.startswith(("mailto:", "#")):
                continue
            resolved = (path.parent / target).resolve()
            try:
                rel = str(resolved.relative_to(ROOT))
            except ValueError:
                problems.append("%s: link leaves the repository: %s" % (doc, target))
                continue
            if not resolved.exists():
                problems.append("%s: broken link: %s" % (doc, target))
        # Experiments name paths that live on other branches.
        for token in [] if doc == "docs/experiments.md" else dict.fromkeys(candidates(text)):
            if not (ROOT / token).exists():
                problems.append("%s: missing path: %s" % (doc, token))
    index = (ROOT / "docs/README.md").read_text()
    for doc in DOCS:
        if doc.startswith("docs/") and doc != "docs/README.md" and Path(doc).name not in index:
            problems.append("docs/README.md: does not list %s" % doc)
    for line in problems:
        print(line, file=sys.stderr)
    if problems:
        print("Docs check failed: update the docs or the code they describe.", file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
