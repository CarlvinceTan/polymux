"""Verify TestFlight signing guards and export policy without contacting Apple."""

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
        for name, body in {
            "security": 'cat "$PROFILE_FIXTURE"',
            "xcodebuild": 'printf "%s\\n" "$@" > "$BUILD_ARGS"',
        }.items():
            path = tools / name
            path.write_text("#!/bin/sh\nset -eu\n" + body + "\n")
            path.chmod(0o755)
        self.out = self.home / "output"
        self.profile = self.home / "profile.plist"
        self.args = self.home / "args"
        self.env = dict(os.environ, HOME=str(self.home), PATH=f"{tools}:/usr/bin:/bin",
                        POLYMUX_TESTFLIGHT_DIR=str(self.out), POLYMUX_BUILD_NUMBER="1.0.1",
                        POLYMUX_TEAM_ID="23YB4896XA", POLYMUX_IOS_PROFILE=str(self.profile),
                        PROFILE_FIXTURE=str(self.profile), BUILD_ARGS=str(self.args),
                        ASC_KEY_PATH=str(self.home / "fixture.p8"), ASC_KEY_ID="fixture", ASC_ISSUER_ID="fixture")
        self.write_profile()

    def write_profile(self, **changes):
        value = {"UUID": "fixture-profile", "Entitlements": {
            "application-identifier": "23YB4896XA.com.flarehq.polymux.app", "beta-reports-active": True}}
        value.update(changes)
        self.profile.write_bytes(plistlib.dumps(value))

    def run_script(self, mode, **env):
        return subprocess.run(["/bin/sh", str(SCRIPT), mode], env=dict(self.env, **env),
                              capture_output=True, text=True)

    def archive_info(self, version="1.0.1"):
        path = self.out / "Polymux.xcarchive/Info.plist"
        path.parent.mkdir(parents=True)
        path.write_bytes(plistlib.dumps({"ApplicationProperties": {"CFBundleVersion": version}}))

    def test_archive_uses_requested_version_and_profile(self):
        result = self.run_script("archive")
        self.assertEqual(result.returncode, 0, result.stderr)
        args = self.args.read_text()
        self.assertIn("CURRENT_PROJECT_VERSION=1.0.1", args)
        self.assertIn("PROVISIONING_PROFILE_SPECIFIER=fixture-profile", args)
        self.assertIn("generic/platform=iOS", args)

    def test_upload_is_internal_only_and_preserves_build_number(self):
        self.archive_info()
        result = self.run_script("upload")
        self.assertEqual(result.returncode, 0, result.stderr)
        options = plistlib.loads((self.out / "ExportOptions.plist").read_bytes())
        self.assertIs(options["testFlightInternalTestingOnly"], True)
        self.assertIs(options["manageAppVersionAndBuildNumber"], False)
        self.assertEqual(options["method"], "app-store-connect")
        self.assertEqual(options["destination"], "upload")
        self.assertIn("-exportArchive", self.args.read_text())

    def test_mismatched_archive_is_not_uploaded(self):
        self.archive_info("1.0.2")
        self.assertNotEqual(self.run_script("upload").returncode, 0)
        self.assertFalse(self.args.exists())

    def test_wrong_profile_is_rejected(self):
        profiles = [
            {"Entitlements": {"application-identifier": "other.app", "beta-reports-active": True}},
            {"Entitlements": {"application-identifier": "23YB4896XA.com.flarehq.polymux.app"}},
            {"ProvisionedDevices": ["fixture-device"]},
        ]
        for changes in profiles:
            with self.subTest(changes=changes):
                self.write_profile(**changes)
                self.assertNotEqual(self.run_script("archive").returncode, 0)
                self.assertFalse(self.args.exists())

    def test_invalid_number_never_builds(self):
        for version in ("", "1.100.1", "0.1.1", "10000.1.1", "1.2.bad"):
            with self.subTest(version=version):
                self.assertNotEqual(self.run_script("archive", POLYMUX_BUILD_NUMBER=version).returncode, 0)
                self.assertFalse(self.args.exists())


if __name__ == "__main__":
    unittest.main()
