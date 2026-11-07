"""Installer drain/health guards with fake commands and disposable state."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest

LIB = Path(__file__).with_name("lib").joinpath("drain.sh").resolve()


class DrainTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.tools = self.root / "tools"
        self.tools.mkdir()
        self.plist = self.root / "server.plist"
        self.plist.touch()
        self.calls = self.root / "calls"
        self.env = dict(os.environ, HOME=str(self.root), PATH=f"{self.tools}:/usr/bin:/bin",
                        CALLS=str(self.calls), PLIST=str(self.plist), DOMAIN="test", LABEL="test.server")
        self.tool("uuidgen", "echo test-update-identity")
        self.tool("sleep", ":")
        self.tool("launchctl", '''n=$(cat "$HOME/polls" 2>/dev/null || echo 0)
        n=$((n + 1)); echo "$n" > "$HOME/polls"
        if [ "${PID_CHANGE:-0}" = 1 ] && [ "$n" -gt 1 ]; then echo 'pid = 43'; else echo 'pid = 42'; fi''')
        self.tool("polymux", '''echo "$*" >> "$CALLS"
        case "$*" in
          version) echo candidate ;;
          *--cancel*) exit "${CANCEL_ERROR:-0}" ;;
          *--build*) exit "${HEALTH_ERROR:-0}" ;;
          *--status*) exit "${STATUS_ERROR:-0}" ;;
          *) exit "${DRAIN_ERROR:-0}" ;;
        esac''')

    def tool(self, name, body):
        path = self.tools / name
        path.write_text("#!/bin/sh\nset -eu\n" + body + "\n")
        path.chmod(0o755)

    def run_guard(self, body, **env):
        script = f'. "{LIB}"\n' + body
        return subprocess.run(["/bin/sh", "-ec", script], env=dict(self.env, **env),
                              capture_output=True, text=True)

    def test_seals_before_activation_and_checks_build_before_resume(self):
        result = self.run_guard('''drain_server polymux http://127.0.0.1:1234
        echo activate >> "$CALLS"
        DRAIN_STOPPED=1
        verify_server
        resume_drain''')
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls.read_text()
        self.assertLess(calls.index("--id"), calls.index("activate"))
        self.assertLess(calls.index("--build candidate"), calls.index("--cancel"))
        self.assertEqual(calls.count("--build candidate"), 6)

    def test_unsupported_api_never_begins_or_cancels_a_drain(self):
        result = self.run_guard('trap cancel_drain EXIT; drain_server polymux http://localhost', STATUS_ERROR="1")
        self.assertNotEqual(result.returncode, 0)
        calls = self.calls.read_text()
        self.assertIn("--status", calls)
        self.assertNotIn("--id", calls)
        self.assertNotIn("--cancel", calls)
        self.assertNotIn("admission resumed", result.stderr)

    def test_failed_drain_never_activates(self):
        result = self.run_guard('drain_server polymux http://localhost || exit 75; echo activate >> "$CALLS"', DRAIN_ERROR="1")
        self.assertEqual(result.returncode, 75)
        self.assertNotIn("activate", self.calls.read_text())

    def test_process_change_never_activates(self):
        result = self.run_guard('drain_server polymux http://localhost || exit 75; echo activate >> "$CALLS"', PID_CHANGE="1")
        self.assertEqual(result.returncode, 75)
        self.assertNotIn("activate", self.calls.read_text())

    def test_failed_health_keeps_admission_paused(self):
        result = self.run_guard('''trap cancel_drain EXIT
        drain_server polymux http://localhost
        DRAIN_STOPPED=1
        verify_server
        resume_drain''', HEALTH_ERROR="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("--cancel", self.calls.read_text())

    def test_cancel_failure_is_not_reported_as_success(self):
        result = self.run_guard('drain_server polymux http://localhost; resume_drain', CANCEL_ERROR="1")
        self.assertNotEqual(result.returncode, 0)

    def test_aborted_preparation_resumes_old_server(self):
        result = self.run_guard('trap cancel_drain EXIT; drain_server polymux http://localhost; exit 75')
        self.assertEqual(result.returncode, 75)
        self.assertIn("--cancel", self.calls.read_text())


if __name__ == "__main__":
    unittest.main()
