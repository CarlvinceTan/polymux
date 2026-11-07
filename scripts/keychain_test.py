"""Keychain signing never widens a profile's app scope or drops its protection."""

import copy
import datetime
from importlib.util import module_from_spec, spec_from_file_location
import os
from pathlib import Path
import plistlib
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
spec = spec_from_file_location("keychain", ROOT / "scripts/lib/keychain.py")
keychain = module_from_spec(spec)
spec.loader.exec_module(keychain)


class KeychainTests(unittest.TestCase):
    def setUp(self):
        self.bundle = "com.example.vault"
        self.identifier = "TEAM." + self.bundle
        self.profile = {
            "TeamIdentifier": ["TEAM"], "ProvisionsAllDevices": True,
            "ExpirationDate": datetime.datetime.now() + datetime.timedelta(days=1),
            "Entitlements": {"com.apple.application-identifier": self.identifier,
                             "keychain-access-groups": ["TEAM.*"]},
        }
        self.base = {"com.apple.security.device.camera": True}

    def test_access_is_narrowed_without_mutating_inputs(self):
        before = copy.deepcopy(self.profile)
        actual = keychain.entitlements(self.profile, self.base, self.bundle)
        self.assertEqual(actual["keychain-access-groups"], [self.identifier])
        self.assertEqual(actual["com.apple.application-identifier"], self.identifier)
        self.assertEqual(actual["com.apple.developer.team-identifier"], "TEAM")
        self.assertTrue(actual["com.apple.security.device.camera"])
        self.assertEqual(self.profile, before)
        self.assertEqual(self.base, {"com.apple.security.device.camera": True})

    def test_invalid_profiles_fail_closed(self):
        for problem in ("expired", "development", "other_app", "other_team", "no_keychain", "no_expiry"):
            with self.subTest(problem=problem):
                profile = copy.deepcopy(self.profile)
                if problem == "expired": profile["ExpirationDate"] = datetime.datetime.now() - datetime.timedelta(days=1)
                if problem == "development": profile["ProvisionsAllDevices"] = False
                if problem == "other_app": profile["Entitlements"]["com.apple.application-identifier"] = "TEAM.other"
                if problem == "other_team": profile["TeamIdentifier"] = ["OTHER"]
                if problem == "no_keychain": profile["Entitlements"]["keychain-access-groups"] = ["OTHER.*"]
                if problem == "no_expiry": del profile["ExpirationDate"]
                with self.assertRaises(ValueError):
                    keychain.entitlements(profile, self.base, self.bundle)

    def test_signer_embeds_profile_and_uses_scoped_entitlements(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "Polymux 900.app"
            (app / "Contents").mkdir(parents=True)
            (app / "Contents/Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier": self.bundle}))
            source, profile, capture = (root / name for name in ("base.plist", "profile", "signed.plist"))
            source.write_bytes(plistlib.dumps(self.base))
            profile.write_bytes(plistlib.dumps(self.profile))
            tools = root / "tools"
            tools.mkdir()
            commands = {
                "security": "import pathlib,sys; sys.stdout.buffer.write(pathlib.Path(sys.argv[-1]).read_bytes())",
                "codesign": "import os,pathlib,sys; pathlib.Path(os.environ['CAPTURE']).write_bytes(pathlib.Path(sys.argv[sys.argv.index('--entitlements')+1]).read_bytes())",
            }
            for name, command in commands.items():
                tool = tools / name
                tool.write_text("#!/usr/bin/env python3\n" + command + "\n")
                tool.chmod(0o755)
            result = subprocess.run(["/bin/sh", "-c", '. "$1"; sign_app "$2" "Test identity" "$3"',
                                     "test", str(ROOT / "scripts/lib/sign.sh"), str(app), str(source)],
                                    env=dict(os.environ, ROOT=str(ROOT), POLYMUX_MAC_PROFILE=str(profile),
                                             CAPTURE=str(capture), PATH=f"{tools}:{os.environ['PATH']}"),
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((app / "Contents/embedded.provisionprofile").read_bytes(), profile.read_bytes())
            self.assertEqual(plistlib.loads(capture.read_bytes())["keychain-access-groups"], [self.identifier])
            self.assertEqual(plistlib.loads(source.read_bytes()), self.base)


if __name__ == "__main__":
    unittest.main()
