"""Verify public Go examples in a separate module without the server or workspace."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class PackageTests(unittest.TestCase):
    def test_external_consumer(self):
        with tempfile.TemporaryDirectory(prefix="polymux-consumer-") as directory:
            consumer = Path(directory)
            version = next(line for line in (ROOT / "go.work").read_text().splitlines() if line.startswith("go "))
            lines = ["module example.com/consumer", "", version, ""]
            for package in ("runtime", "memory", "search", "vault"):
                module = f"github.com/CarlvinceTan/polymux/packages/{package}"
                lines.extend([f"require {module} v0.0.0", f'replace {module} => "{ROOT / "packages" / package}"'])
                target = consumer / package
                target.mkdir()
                (target / "example_test.go").write_text((ROOT / "packages" / package / "example_test.go").read_text())
            (consumer / "go.mod").write_text("\n".join(lines) + "\n")
            result = subprocess.run(
                ["go", "test", "-mod=mod", "-race", "./..."], cwd=consumer,
                env=dict(os.environ, GOWORK="off"), capture_output=True, text=True, timeout=180,
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
