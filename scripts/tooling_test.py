"""Check command forwarding, build ownership, cancellation, and Control caching."""

import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

SCRIPTS = Path(__file__).resolve().parent
LOCKED = SCRIPTS / "lib/locked.py"


class ToolingTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def executable(self, path, source):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(source)
        path.chmod(0o755)

    def wait_file(self, path):
        deadline = time.monotonic() + 5
        while not path.exists():
            if time.monotonic() > deadline:
                self.fail(f"Timed out waiting for {path.name}")
            time.sleep(0.02)

    def test_swift_filter_is_forwarded(self):
        self.executable(self.root / "swift", '#!/bin/sh\nprintf "%s\\n" "$@"\n')
        result = subprocess.run([str(SCRIPTS / "test.sh"), "swift", "--filter", "TargetTests"],
                                env=dict(os.environ, PATH=f"{self.root}:{os.environ['PATH']}"),
                                text=True, capture_output=True, check=True)
        self.assertEqual(result.stdout.splitlines(), ["test", "--filter", "TargetTests"])

    def test_empty_lock_is_available_and_waiters_serialize(self):
        lock = self.root / "build.lock"
        lock.touch()
        first = self.root / "first"
        second = self.root / "second"
        command = [sys.executable, "-c", "import pathlib,sys,time; pathlib.Path(sys.argv[1]).touch(); time.sleep(.4)"]
        one = subprocess.Popen([sys.executable, str(LOCKED), str(lock), "--", *command, str(first)])
        try:
            self.wait_file(first)
            two = subprocess.Popen([sys.executable, str(LOCKED), str(lock), "--", *command, str(second)])
            try:
                time.sleep(.1)
                self.assertFalse(second.exists())
                self.assertEqual(one.wait(timeout=5), 0)
                self.assertEqual(two.wait(timeout=5), 0)
                self.assertTrue(second.exists())
            finally:
                if two.poll() is None: two.terminate(); two.wait(timeout=5)
        finally:
            if one.poll() is None: one.terminate(); one.wait(timeout=5)

    def test_term_stops_build_and_does_not_start_next_platform(self):
        scripts = self.root / "scripts"
        self.executable(scripts / "check.sh", (SCRIPTS / "check.sh").read_text())
        self.executable(scripts / "lib/locked.py", LOCKED.read_text())
        calls = self.root / "calls"
        self.executable(self.root / "bin/xcodebuild", '#!/bin/sh\necho build >> "$CALLS"\nsleep 30\n')
        process = subprocess.Popen([str(scripts / "check.sh"), "all"],
                                   env=dict(os.environ, CALLS=str(calls), PATH=f"{self.root}/bin:{os.environ['PATH']}"),
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            self.wait_file(calls)
            process.terminate()
            process.communicate(timeout=6)
            self.assertEqual(process.returncode, 143)
            self.assertEqual(calls.read_text().splitlines(), ["build"])
        finally:
            if process.poll() is None: process.terminate(); process.communicate(timeout=6)

    def test_check_does_not_replace_existing_destination(self):
        scripts = self.root / "scripts"
        self.executable(scripts / "check.sh", (SCRIPTS / "check.sh").read_text())
        self.executable(scripts / "lib/locked.py", LOCKED.read_text())
        self.executable(self.root / "bin/xcodebuild", '#!/bin/sh\nexit 0\n')
        destination = self.root / "existing.app"
        destination.mkdir()
        marker = destination / "data"
        marker.write_text("keep")
        result = subprocess.run([str(scripts / "check.sh"), "macos", str(destination)],
                                env=dict(os.environ, PATH=f"{self.root}/bin:{os.environ['PATH']}"),
                                text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(marker.read_text(), "keep")


if __name__ == "__main__":
    unittest.main()
