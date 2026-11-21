"""Select native checks without skipping the server deployment status."""

import json
import os
from pathlib import Path
import subprocess


def affected(paths):
    shared = any(path.startswith(("scripts/", ".github/workflows/", "contracts/")) for path in paths)
    go_packages = ("packages/runtime/", "packages/memory/", "packages/search/", "packages/vault/")
    server = shared or any(path.startswith(("apps/server/", "go.work", *go_packages)) for path in paths)
    swift = shared or any(
        path.startswith(("apps/Polymux/", "packages/", "Package.", ".agents/skills/avatar-preview/"))
        and not path.startswith(go_packages) for path in paths
    )
    plugins = shared or any(path.startswith("plugins/") or path == "apps/server/go.mod" for path in paths)
    site_inputs = {"apps/Polymux/Polymux.xcodeproj/project.pbxproj", "plugins/index.json",
                   "plugins/hub/internal/hub/bridges.go", "scripts/ci.py"}
    site = any(path.startswith(("apps/site/", ".github/workflows/")) or path in site_inputs for path in paths)
    return {"server": server, "swift": swift, "plugins": plugins, "site": site}


def selection(event, kind):
    if kind == "pull_request":
        return event["pull_request"]["base"]["sha"]
    if kind == "push":
        if event.get("ref", "").startswith("refs/tags/"):
            return None
        base = event.get("before", "")
        if base and set(base) != {"0"}:
            return base
    return None


def changed(base, cwd=None):
    # A rename would list only the new path, hiding the area a file left.
    diff = subprocess.run(
        ["git", "diff", "--name-only", "--no-renames", "-z", base, "HEAD"], capture_output=True, cwd=cwd,
    )
    return diff.stdout.decode().split("\0") if diff.returncode == 0 else None


def plan(event, kind, main=None, diff=changed):
    before = selection(event, kind)
    dev = kind == "push" and event.get("ref") == "refs/heads/dev"
    bases = (before, main) if dev else (before,)
    paths = [diff(base) if base else None for base in bases]
    if any(path_set is None for path_set in paths):
        return {"server": True, "swift": True, "plugins": True, "site": True}, "full"
    return affected([path for path_set in paths for path in path_set]), main if dev else "full"


if __name__ == "__main__":
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
    main = subprocess.run(
        ["git", "rev-parse", "--verify", "refs/remotes/origin/main"],
        capture_output=True, text=True,
    ).stdout.strip()
    result, baseline = plan(event, os.environ["GITHUB_EVENT_NAME"], main)
    print(f"promotion_base={baseline}")
    for name, value in result.items():
        print(f"{name}={str(value).lower()}")
