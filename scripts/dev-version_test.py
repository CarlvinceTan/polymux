import unittest

from pathlib import Path
from runpy import run_path

versions = run_path(str(Path(__file__).with_name("dev-version.py")))["versions"]


class VersionTests(unittest.TestCase):
    def test_uploads_and_reruns_increase(self):
        sequence = [(1, 1), (1, 2), (99, 99), (100, 1), (101, 1), (999899, 99)]
        for platform in ("ios", "macos"):
            values = [tuple(map(int, versions(run, attempt, "0.4.0")[platform].split("."))) for run, attempt in sequence]
            self.assertEqual(values, sorted(set(values)))

    def test_apple_component_limits(self):
        for run in (1, 99, 100, 999899):
            major, minor, patch = map(int, versions(run, 99, "1.0.0")["ios"].split("."))
            self.assertLessEqual(major, 9999)
            self.assertLessEqual(minor, 99)
            self.assertLessEqual(patch, 99)

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
