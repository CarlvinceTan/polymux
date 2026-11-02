"""Exercise the E2E runner's lifecycle with fake OS and driver calls, without a desktop."""

import contextlib
import io
import json
import os
from pathlib import Path
import plistlib
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
        self.front = "100"
        self.session_error = False
        self.process = 900
        self.env = {
            "POLYMUX_ROOT": str(Path(__file__).resolve().parents[1]),
            "POLYMUX_APP": "Polymux 123", "POLYMUX_STATE": str(self.root),
            "POLYMUX_ADDRESS": "http://127.0.0.1:1234",
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
        elif tool == "open":
            if "--args" not in args:
                return subprocess.CompletedProcess(args, 0, "", "")
            self.process += 1
            data = Path(next(arg.split("=", 1)[1] for arg in args if arg.startswith("POLYMUX_DATA_DIR=")))
            self.launches.append((self.process, sorted(path.name for path in data.iterdir()), args))
            (data / "draft").write_text("persisted draft")
        elif tool == "pgrep":
            output = str(self.process)
        elif tool == "kill":
            code = 1
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

    def test_relaunch_and_crash_keep_data_and_previous_result(self):
        self.results = [{"ok": True, "value": {"draft": "first"}},
                        {"ok": True, "value": {"draft": "second"}}, {"ok": True}]
        self.run_flow("return {};\n// @relaunch\nreturn {};\n// @crash\nreturn {};")
        self.assertEqual([files for _, files, _ in self.launches], [[], ["draft"]])
        self.assertIn('const PREVIOUS = {"draft": "first"}', self.scripts[1]["script"])
        self.assertIn('const PREVIOUS = {"draft": "second"}', self.scripts[2]["script"])
        self.assertEqual([sig for _, sig in self.signals], [signal.SIGTERM, signal.SIGKILL])
        for _, _, args in self.launches:
            self.assertIn("POLYMUX_TEST_ADDRESS=" + self.env["POLYMUX_ADDRESS"], args)

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

    def test_focus_failure_still_cleans_up(self):
        for front in ("800", ""):
            with self.subTest(front=front):
                self.front = front
                self.process = 900
                self.signals.clear()
                self.calls.clear()
                with self.assertRaises(SystemExit):
                    self.run_flow("return 'ok';")
                self.assertEqual(self.signals, [])
                self.assertEqual(len(self.driver_calls("end_session")), 1)


if __name__ == "__main__":
    unittest.main()
