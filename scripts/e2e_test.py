"""Exercise the E2E runner's lifecycle with fake OS and driver calls, without a desktop."""

import contextlib
import io
import json
import os
from pathlib import Path
import plistlib
import shutil
import signal
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).with_name("run-e2e.sh").read_text().split("python3 - <<'PY'\n", 1)[1].split("\nPY\n", 1)[0]


class E2ETests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app = self.root / "Polymux 123.app"
        (self.app / "Contents").mkdir(parents=True)
        self.plist = self.app / "Contents/Info.plist"
        self.plist.write_bytes(plistlib.dumps({"CFBundleIdentifier": "test.polymux.123"}))
        (self.root / "app.pid").write_text("800")
        (self.root / "data").mkdir()
        self.calls = []
        self.signals = []
        self.launches = []
        self.results = []
        self.scripts = []
        self.preferences = {}
        self.callbacks = []
        self.front = "100"
        self.sign_error = False
        self.session_error = False
        self.process = 900
        self.env = {
            "POLYMUX_ROOT": str(Path(__file__).resolve().parents[1]),
            "POLYMUX_APP": "Polymux 123", "POLYMUX_STATE": str(self.root),
            "POLYMUX_ADDRESS": "http://127.0.0.1:1234", "POLYMUX_ENTITLEMENTS": "test.entitlements",
        }

    def tool(self, args, **kwargs):
        self.calls.append(args)
        tool = Path(args[0]).name
        output, code = "", 0
        if tool == "cua-driver":
            request = json.loads(args[2])
            if args[1] == "run_script":
                self.scripts.append(request)
                result = self.results.pop(0) if self.results else {"ok": True, "value": "ok"}
            elif args[1] == "list_apps":
                result = {"apps": [{"pid": int(self.front), "active": True}] if self.front else []}
            else:
                result = {"ok": not (args[1] == "start_session" and self.session_error)}
            output = json.dumps(result)
        elif tool == "control.sh":
            output = json.dumps({"ok": True, "frontmost": int(self.front) if self.front else 0})
        elif tool == "node":
            self.scripts.append(json.loads(kwargs["input"]))
            self.control_env = kwargs["env"]
            result = self.results.pop(0) if self.results else {"ok": True, "value": "ok"}
            output = json.dumps(result)
            code = 0 if result.get("ok") else 1
        elif tool == "ditto":
            shutil.copytree(args[1], args[2])
        elif tool == "PlistBuddy":
            path = Path(args[-1])
            data = plistlib.loads(path.read_bytes())
            command = args[2].split(" ", 2)
            key = command[1].lstrip(":")
            if command[0] == "Print":
                output = data[key]
            else:
                data[key] = command[2]
                path.write_bytes(plistlib.dumps(data))
        elif tool == "codesign":
            if self.sign_error:
                raise subprocess.CalledProcessError(1, args)
        elif tool == "open":
            if "--args" not in args:
                return subprocess.CompletedProcess(args, 0, "", "")
            self.process += 1
            data = Path(next(arg.split("=", 1)[1] for arg in args if arg.startswith("POLYMUX_DATA_DIR=")))
            self.launches.append((self.process, sorted(path.name for path in data.iterdir()), args))
            (data / "draft").write_text("persisted draft")
            self.preferences["test.polymux.123.fixture"] = "changed"
        elif tool == "pgrep":
            output = str(self.process)
        elif tool == "kill":
            code = 1
        elif tool == "defaults":
            self.preferences.pop(args[2], None)
        elif tool == "ps":
            output = "Polymux"
        else:
            raise AssertionError("unexpected tool: " + repr(args))
        return subprocess.CompletedProcess(args, code, output, "")

    def run_flow(self, source):
        flow = self.root / "flow.js"
        flow.write_text(source)
        self.callbacks = []
        output = io.StringIO()
        with patch.dict(os.environ, dict(self.env, POLYMUX_FLOW=str(flow))), \
                patch("subprocess.run", side_effect=self.tool), \
                patch("os.kill", side_effect=lambda pid, sig: self.signals.append((pid, sig))), \
                patch("atexit.register", side_effect=lambda fn, *args: self.callbacks.append((fn, args))), \
                patch("threading.Thread"), contextlib.redirect_stdout(output):
            try:
                exec(compile(SCRIPT, "run-e2e.sh", "exec"), {})
            finally:
                for fn, args in reversed(self.callbacks):
                    fn(*args)
        return output.getvalue()

    def driver_calls(self, name):
        return [json.loads(args[2]) for args in self.calls if args[:2] == ["cua-driver", name]]

    def test_fast_motion_is_scoped_and_session_ends(self):
        self.assertIn("ok   flow.js", self.run_flow("return 'ok';"))
        start, = self.driver_calls("start_session")
        run, = self.driver_calls("run_script")
        end, = self.driver_calls("end_session")
        self.assertEqual(start["session"], run["session"])
        self.assertEqual(run["session"], end["session"])
        self.assertEqual(start["cursor_motion"], {
            "timing": "fixed", "glide_duration_ms": 1, "dwell_after_click_ms": 0,
        })
        self.assertFalse(self.driver_calls("set_config"))
        self.assertFalse(self.driver_calls("set_agent_cursor_enabled"))

    def test_control_driver_has_no_cua_fallback_and_cleans_up(self):
        self.env["POLYMUX_E2E_DRIVER"] = "control"
        source = "// @fixture --verify-fixture --verify-tools\nreturn 'ok';"
        self.assertIn("ok   flow.js", self.run_flow(source))
        self.assertEqual(self.control_env["POLYMUX_CONTROL_FIXTURE"], "1")
        self.results = [{"ok": False, "code": "unsupported_action", "error": "native refusal"}]
        with self.assertRaises(SystemExit):
            self.run_flow(source)
        self.assertFalse(any(args[0] == "cua-driver" for args in self.calls))
        self.assertEqual(self.signals, [(901, signal.SIGTERM), (902, signal.SIGTERM)])
        self.assertFalse((self.root / "fixture.pid").exists())

    def test_fixture_reuses_bundle_but_not_data_or_preferences(self):
        source = "// @fixture --verify-fixture --verify-tools\nreturn 'ok';"
        self.run_flow(source)
        self.preferences["test.polymux.123.fixture"] = "stale"
        self.run_flow(source)
        for tool in ("ditto", "codesign"):
            self.assertEqual(sum(args[0] == tool for args in self.calls), 1)
        self.assertEqual([files for _, files, _ in self.launches], [[], []])
        self.assertEqual(self.preferences, {})
        self.assertEqual(len(self.signals), 2)
        self.assertTrue(all(sig == signal.SIGTERM for _, sig in self.signals))
        self.assertEqual(plistlib.loads(self.plist.read_bytes())["CFBundleIdentifier"], "test.polymux.123")

    def test_relaunch_and_crash_keep_data_and_previous_result(self):
        self.results = [{"ok": True, "value": {"draft": "first"}},
                        {"ok": True, "value": {"draft": "second"}}, {"ok": True}]
        self.run_flow("// @fixture --verify-fixture --verify-dark\nreturn {};\n// @relaunch\nreturn {};\n// @crash\nreturn {};")
        self.assertEqual([files for _, files, _ in self.launches], [[], ["draft"], ["draft"]])
        self.assertIn('const PREVIOUS = {"draft": "first"}', self.scripts[1]["script"])
        self.assertIn('const PREVIOUS = {"draft": "second"}', self.scripts[2]["script"])
        self.assertEqual([sig for _, sig in self.signals], [signal.SIGTERM, signal.SIGKILL, signal.SIGTERM])
        for _, _, args in self.launches:
            self.assertEqual(args[-2:], ["-AppleInterfaceStyle", "Dark"])

    def test_failed_fixture_cleans_up_without_restarting_live_app(self):
        self.results = [{"ok": False, "error": "expected test failure"}]
        with self.assertRaises(SystemExit):
            self.run_flow("// @fixture --verify-fixture\nthrow new Error('fail');")
        self.assertEqual(len(self.launches), 1)
        self.assertEqual(self.signals, [(901, signal.SIGTERM)])
        self.assertEqual(self.preferences, {})
        self.assertEqual(len(self.driver_calls("end_session")), 1)
        self.run_flow("// @fixture --verify-fixture\nreturn 'ok';")
        self.assertEqual(self.launches[-1][1], [])

    def test_live_failure_restarts_only_the_test_app(self):
        self.results = [{"ok": False, "error": "expected test failure"}]
        with self.assertRaises(SystemExit):
            self.run_flow("throw new Error('fail');")
        self.assertEqual(self.signals, [(800, signal.SIGTERM)])
        self.assertEqual((self.root / "app.pid").read_text(), "901")
        self.assertIn(str(self.app), self.launches[0][2])
        self.assertEqual(len(self.driver_calls("end_session")), 1)

    def test_cursor_setup_failure_ends_session_without_input(self):
        self.session_error = True
        with self.assertRaises(SystemExit):
            self.run_flow("return 'ok';")
        self.assertFalse(self.driver_calls("run_script"))
        self.assertEqual(len(self.driver_calls("end_session")), 1)

    def test_failed_signing_is_not_cached(self):
        self.sign_error = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.run_flow("// @fixture --verify-fixture\nreturn 'ok';")
        self.assertFalse((self.root / "fixture/signed").exists())
        self.sign_error = False
        self.run_flow("// @fixture --verify-fixture\nreturn 'ok';")
        self.assertEqual(sum(args[0] == "codesign" for args in self.calls), 2)

    def test_focus_failure_still_cleans_up(self):
        for front in ("901", ""):
            with self.subTest(front=front):
                self.front = front
                self.process = 900
                self.signals.clear()
                self.calls.clear()
                with self.assertRaises(SystemExit):
                    self.run_flow("// @fixture --verify-fixture\nreturn 'ok';")
                self.assertEqual(self.signals, [(901, signal.SIGTERM)])
                self.assertEqual(len(self.driver_calls("end_session")), 1)


if __name__ == "__main__":
    unittest.main()
