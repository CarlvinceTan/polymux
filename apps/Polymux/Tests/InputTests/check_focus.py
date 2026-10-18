#!/usr/bin/env python3
"""Exercise shared input focus with hidden AppKit windows."""

from pathlib import Path
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parents[4]
sources = [
    ROOT / "apps/Polymux/Sources/Views/InputFocus.swift",
    ROOT / "apps/Polymux/Sources/Views/SearchFocus.swift",
    Path(__file__).with_name("FocusCheck.swift"),
]

temp = Path(tempfile.gettempdir()) / "opencode"
with tempfile.TemporaryDirectory(prefix="polymux-focus-", dir=temp if temp.is_dir() else None) as directory:
    run = Path(directory)
    executable = run / "focus-check"
    subprocess.run(["swiftc", "-swift-version", "6", "-parse-as-library", *map(str, sources),
                    "-o", str(executable)], check=True, timeout=60)
    subprocess.run([str(executable)], check=True, timeout=15)
