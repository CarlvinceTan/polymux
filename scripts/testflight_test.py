"""Verify TestFlight signing options and export policy without contacting Apple."""

import os
from pathlib import Path
import plistlib
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("testflight.sh").resolve()


@unittest.skipUnless(Path("/usr/libexec/PlistBuddy").exists(), "requires macOS plist tools")
class TestFlightTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        tools = self.home / "tools"
        tools.mkdir()
        path = tools / "xcodebuild"
        path.write_text('#!/bin/sh\nset -eu\nprintf "%s\\n" "$@" > "$BUILD_ARGS"\n')
        path.chmod(0o755)
        self.out = self.home / "output"
        self.args = self.home / "args"
        self.env = dict(os.environ, HOME=str(self.home), PATH=f"{tools}:/usr/bin:/bin",
                        POLYMUX_TESTFLIGHT_DIR=str(self.out), POLYMUX_BUILD_NUMBER="2",
                        POLYMUX_TEAM_ID="23YB4896XA", BUILD_ARGS=str(self.args))
        for name in ("ASC_KEY_PATH", "ASC_KEY_ID", "ASC_ISSUER_ID"):
            self.env.pop(name, None)

    def run_script(self, mode, **env):
        return subprocess.run(["/bin/sh", str(SCRIPT), mode], env=dict(self.env, **env),
                              capture_output=True, text=True)

    def archive_info(self, version="2"):
        path = self.out / "Polymux.xcarchive/Info.plist"
        path.parent.mkdir(parents=True)
        path.write_bytes(plistlib.dumps({"ApplicationProperties": {"CFBundleVersion": version}}))

    def test_archive_signs_automatically_with_requested_version(self):
        result = self.run_script("archive")
        self.assertEqual(result.returncode, 0, result.stderr)
        args = self.args.read_text()
        self.assertIn("CURRENT_PROJECT_VERSION=2", args)
        self.assertIn("-allowProvisioningUpdates", args)
        self.assertIn("generic/platform=iOS", args)
        self.assertNotIn("-authenticationKeyPath", args)

    def test_api_key_is_passed_when_configured(self):
        result = self.run_script("archive", ASC_KEY_PATH="/key.p8", ASC_KEY_ID="K", ASC_ISSUER_ID="I")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("-authenticationKeyPath\n/key.p8", self.args.read_text())

    def test_partial_api_key_never_builds(self):
        self.assertNotEqual(self.run_script("archive", ASC_KEY_PATH="/key.p8").returncode, 0)
        self.assertFalse(self.args.exists())

    def test_upload_is_internal_only_and_preserves_build_number(self):
        self.archive_info()
        result = self.run_script("upload")
        self.assertEqual(result.returncode, 0, result.stderr)
        options = plistlib.loads((self.out / "ExportOptions.plist").read_bytes())
        self.assertIs(options["testFlightInternalTestingOnly"], True)
        self.assertIs(options["manageAppVersionAndBuildNumber"], False)
        self.assertEqual(options["method"], "app-store-connect")
        self.assertEqual(options["destination"], "upload")
        self.assertEqual(options["signingStyle"], "automatic")
        self.assertIn("-exportArchive", self.args.read_text())

    def test_mismatched_archive_is_not_uploaded(self):
        self.archive_info("3")
        self.assertNotEqual(self.run_script("upload").returncode, 0)
        self.assertFalse(self.args.exists())

    def test_invalid_number_never_builds(self):
        for version in ("1.0.1", "0", "-3", "bad"):
            with self.subTest(version=version):
                self.assertNotEqual(self.run_script("archive", POLYMUX_BUILD_NUMBER=version).returncode, 0)
                self.assertFalse(self.args.exists())


if __name__ == "__main__":
    unittest.main()
