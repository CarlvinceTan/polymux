"""Verify simulator ownership and cleanup without touching a real device."""

import contextlib
import io
import json
from pathlib import Path
import plistlib
import tempfile
import unittest
from unittest.mock import patch

import sim


class SimulatorTests(unittest.TestCase):
    def test_selects_hardware_without_reusing_device(self):
        devices = {"com.apple.CoreSimulator.SimRuntime.iOS-27-0": [
            {"name": "iPhone", "udid": "personal", "deviceTypeIdentifier": "phone", "isAvailable": True}]}
        self.assertEqual(sim.device_spec(devices), ("phone", next(iter(devices))))
        with self.assertRaises(ValueError):
            sim.device_spec(devices, "missing")

    def test_success_and_failure_delete_only_created_device(self):
        for fail in (False, True):
            with self.subTest(fail=fail), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                devices = {"iOS-27": [{"name": "iPhone", "udid": "personal",
                                      "deviceTypeIdentifier": "phone", "isAvailable": True}]}
                calls = []

                def command(*args):
                    calls.append(args)
                    if args[2:4] == ("list", "devices"):
                        return json.dumps({"devices": devices})
                    if args[2] == "create": return "owned-device"
                    if args[2] == "install":
                        value = plistlib.loads((Path(args[-1]) / "Info.plist").read_bytes())
                        self.assertIn(".test.sim.", value["CFBundleIdentifier"])
                        extension = plistlib.loads((Path(args[-1]) / "PlugIns/Call.appex/Info.plist").read_bytes())
                        self.assertEqual(extension["CFBundleIdentifier"], value["CFBundleIdentifier"] + ".call")
                    if args[2] == "launch" and fail: raise RuntimeError("launch failed")
                    return ""

                def run(args, **_kwargs):
                    calls.append(tuple(args))
                    if args[0].endswith("check.sh"):
                        bundle = Path(args[-1]); bundle.mkdir()
                        (bundle / "Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier": "real.app"}))
                        extension = bundle / "PlugIns/Call.appex"; extension.mkdir(parents=True)
                        (extension / "Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier": "real.app.call"}))

                with patch.object(sim, "ROOT", root), patch.object(sim, "command", side_effect=command), \
                        patch.object(sim.subprocess, "run", side_effect=run), patch.object(sim.time, "sleep"), \
                        patch.dict(sim.os.environ, {}, clear=True), contextlib.redirect_stdout(io.StringIO()):
                    if fail:
                        with self.assertRaises(RuntimeError): sim.capture(root / "out.png", [])
                    else:
                        sim.capture(root / "out.png", [])
                self.assertEqual(calls[-2:], [("xcrun", "simctl", "shutdown", "owned-device"),
                                             ("xcrun", "simctl", "delete", "owned-device")])
                self.assertFalse(list((root / ".build/sim").iterdir()))


if __name__ == "__main__":
    unittest.main()
