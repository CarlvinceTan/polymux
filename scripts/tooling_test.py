"""Check build locking and test command forwarding."""

import os
from pathlib import Path
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

    def test_server_arguments_are_forwarded(self):
        self.executable(self.root / "go", '#!/bin/sh\nprintf "%s\\n" "$@"\n')
        result = subprocess.run([str(SCRIPTS / "test.sh"), "server", "-run", "TestThing"],
                                env=dict(os.environ, PATH=f"{self.root}:{os.environ['PATH']}"),
                                text=True, capture_output=True, check=True)
        self.assertEqual(result.stdout.splitlines(), ["test", "-tags", "sqlite_fts5", "-run", "TestThing"])

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


if __name__ == "__main__":
    unittest.main()
