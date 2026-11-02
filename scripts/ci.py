"""Select affected checks without skipping the server deployment status."""

import json
import os
from pathlib import Path
import subprocess


def affected(paths):
    shared = any(path.startswith(("scripts/", ".github/workflows/", "contracts/")) for path in paths)
    go_packages = ("packages/runtime/", "packages/memory/", "packages/search/", "packages/vault/")
    server = shared or any(path.startswith(("apps/server/", "go.work", *go_packages)) for path in paths)
    tauri = shared or any(
        path.startswith(("apps/tauri/", "apps/server/", "packages/protocol/", "e2e/"))
        for path in paths
    )
    return {"server": server, "tauri": tauri}


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


if __name__ == "__main__":
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
    base = selection(event, os.environ["GITHUB_EVENT_NAME"])
    result = {"server": True, "tauri": True}
    if base:
        # A missing base runs the full checks, including after a history rewrite.
        diff = subprocess.run(["git", "diff", "--name-only", "-z", base, "HEAD"], capture_output=True)
        if diff.returncode == 0:
            result = affected(diff.stdout.decode().split("\0"))
    for name, value in result.items():
        print(f"{name}={str(value).lower()}")
