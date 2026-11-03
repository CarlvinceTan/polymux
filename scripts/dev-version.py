#!/usr/bin/env python3
"""Emit development build numbers: iOS is the commit count, macOS is monotonic across reruns."""

import pathlib
import re
import subprocess
import sys


def versions(run, attempt, version):
    if not 1 <= run < 999900 or not 1 <= attempt <= 99:
        raise ValueError("run must be 1..999899 and attempt 1..99")
    major, minor, patch = map(int, version.split("."))
    base = major * 1000000 + minor * 1000 + patch
    suffix = f"{run % 100}.{attempt}"
    return {"macos": f"{base + run // 100}.{suffix}"}


def ios_build(root):
    return subprocess.check_output(["git", "-C", str(root), "rev-list", "--count", "HEAD"], text=True).strip()


if __name__ == "__main__":
    root = pathlib.Path(__file__).resolve().parents[1]
    project = root / "apps/Polymux/Polymux.xcodeproj/project.pbxproj"
    version = re.search(r"MARKETING_VERSION = (\d+\.\d+\.\d+);", project.read_text()).group(1)
    print(f"ios={ios_build(root)}")
    for key, value in versions(int(sys.argv[1]), int(sys.argv[2]), version).items():
        print(f"{key}={value}")
