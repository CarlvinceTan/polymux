"""An update handoff outlives its caller and runs outside the caller's process session."""

import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]


class UpdateTests(unittest.TestCase):
    def test_independent_handoff(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts/lib").mkdir(parents=True)
            shutil.copy(ROOT / "scripts/update.sh", root / "scripts/update.sh")
            shutil.copy(ROOT / "scripts/lib/locked.py", root / "scripts/lib/locked.py")
            (root / "scripts/install.sh").write_text('''#!/bin/sh
exec python3 - <<'PY'
import os, pathlib, time
root = pathlib.Path(os.environ['FIXTURE'])
(root / 'started').write_text(str(os.getsid(0)))
while not (root / 'release').exists(): time.sleep(.02)
(root / 'finished').touch()
PY
''')
            result = subprocess.run(["/bin/sh", str(root / "scripts/update.sh")],
                                    env=dict(os.environ, FIXTURE=str(root)), capture_output=True,
                                    text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            pid = int(re.search(r'process (\d+)', result.stdout).group(1))
            try:
                self.wait_for(root / "started")
                self.assertNotEqual(int((root / "started").read_text()), os.getsid(0))
                self.assertFalse((root / "finished").exists())
                (root / "release").touch()
                self.wait_for(root / "finished")
            finally:
                try:
                    os.kill(pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
                # Allow the lock supervisor to reap its owned child before deleting the fixture.
                time.sleep(.2)

    def wait_for(self, path):
        deadline = time.monotonic() + 5
        while not path.exists():
            if time.monotonic() >= deadline:
                self.fail(f"Timed out waiting for {path.name}")
            time.sleep(.02)


if __name__ == "__main__":
    unittest.main()
