import subprocess
import tempfile
import unittest

from pathlib import Path
from runpy import run_path

namespace = run_path(str(Path(__file__).with_name("dev-version.py")))
versions, ios_build = namespace["versions"], namespace["ios_build"]


class VersionTests(unittest.TestCase):
    def test_uploads_and_reruns_increase(self):
        sequence = [(1, 1), (1, 2), (99, 99), (100, 1), (101, 1), (999899, 99)]
        values = [tuple(map(int, versions(run, attempt, "0.4.0")["macos"].split("."))) for run, attempt in sequence]
        self.assertEqual(values, sorted(set(values)))

    def test_ios_build_counts_commits(self):
        with tempfile.TemporaryDirectory() as root:
            git = ["git", "-C", root, "-c", "user.name=t", "-c", "user.email=t@t"]
            subprocess.check_call(git[:3] + ["init", "-q"])
            for _ in range(3):
                subprocess.check_call(git + ["commit", "-q", "--allow-empty", "-m", "c"])
            self.assertEqual(ios_build(root), "3")

    def test_mac_dev_build_exceeds_stable(self):
        for version, stable in (("0.4.0", 4000), ("0.4.10", 4010), ("1.0.0", 1000000)):
            value = tuple(map(int, versions(1, 1, version)["macos"].split(".")))
            self.assertGreater(value, (stable,))

    def test_rejects_invalid_counters(self):
        for run, attempt in ((0, 1), (999900, 1), (1, 0), (1, 100)):
            with self.assertRaises(ValueError):
                versions(run, attempt, "0.4.0")


if __name__ == "__main__":
    unittest.main()
