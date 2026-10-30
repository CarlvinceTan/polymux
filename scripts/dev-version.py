#!/usr/bin/env python3
"""Emit monotonic development build numbers, including workflow reruns."""

import pathlib
import re
import sys


def versions(run, attempt, version):
    if not 1 <= run < 999900 or not 1 <= attempt <= 99:
        raise ValueError("run must be 1..999899 and attempt 1..99")
    major, minor, patch = map(int, version.split("."))
    base = major * 1000000 + minor * 1000 + patch
    suffix = f"{run % 100}.{attempt}"
    return {
        "ios": f"{1 + run // 100}.{suffix}",
        "macos": f"{base + run // 100}.{suffix}",
    }


if __name__ == "__main__":
    project = pathlib.Path(__file__).resolve().parents[1] / "apps/Polymux/Polymux.xcodeproj/project.pbxproj"
    version = re.search(r"MARKETING_VERSION = (\d+\.\d+\.\d+);", project.read_text()).group(1)
    for key, value in versions(int(sys.argv[1]), int(sys.argv[2]), version).items():
        print(f"{key}={value}")
