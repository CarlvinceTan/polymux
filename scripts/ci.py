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
    plugins = shared or any(path.startswith("plugins/") for path in paths)
    return {"server": server, "swift": swift, "plugins": plugins}


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


if __name__ == "__main__":
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
    base = selection(event, os.environ["GITHUB_EVENT_NAME"])
    result = {"server": True, "swift": True, "plugins": True}
    if base:
        # A missing base runs the full checks, including after a history rewrite.
        paths = changed(base)
        if paths is not None:
            result = affected(paths)
    for name, value in result.items():
        print(f"{name}={str(value).lower()}")
