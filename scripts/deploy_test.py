"""Deploy decisions use isolated HOME, fake CI and fake launchd; never the installed server."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("deploy.sh").resolve()


class DeployTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        self.state = self.home / ".polymux/deploy"
        self.state.mkdir(parents=True)
        (self.state / "sha").write_text("old\n")
        self.bin = self.home / ".local/bin"
        self.bin.mkdir(parents=True)
        (self.bin / "polymux").write_text("original\n")
        self.src = self.home / ".polymux/src/scripts"
        (self.src / "lib").mkdir(parents=True)
        (self.src / "lib/drain.sh").write_text(SCRIPT.with_name("lib").joinpath("drain.sh").read_text())
        self.tools = self.home / "tools"
        self.tools.mkdir()
        self.env = dict(os.environ, HOME=str(self.home), PATH=f"{self.tools}:/usr/bin:/bin",
                        TEST_LOG=str(self.home / "calls"), RESULT="success")
        self.tool("git", '''case "$*" in
          *"remote get-url origin"*) echo https://github.com/example/repo.git ;;
          *"rev-parse FETCH_HEAD"*) echo "${SHA:-new-sha}" ;;
          *"rev-list -n 1"*) echo "$*" >> "$TEST_LOG"; echo "${TAG_SHA:-release-sha}" ;;
          *"merge-base --is-ancestor"*) [ "${BEHIND:-0}" = 1 ] ;;
          *"diff --quiet"*) echo "$*" >> "$TEST_LOG"; [ "${UI_ONLY:-0}" = 1 ] ;;
        esac''')
        self.tool("gh", '''case "$*" in
          *"release view"*) echo "$*" >> "$TEST_LOG"; echo v9.9.9 ;;
          *) echo "$RESULT" ;;
        esac''')
        if not shutil.which("plutil"):
            # deploy.sh runs on macOS; elsewhere, stand in for plutil -extract KEY raw -o - FILE.
            self.tool("plutil", '''exec python3 -c 'import functools, json, sys
print(functools.reduce(lambda v, k: v[k], sys.argv[1].split("."), json.load(open(sys.argv[2]))))' "$2" "$6"''')
        self.tool("shlock", 'touch "$2"')
        # The old server is reachable unless a test sets CURL_EXIT.
        self.tool("curl", 'exit "${CURL_EXIT:-0}"')
        self.tool("launchctl", 'echo "launchctl $*" >> "$TEST_LOG"; exit 99')
        self.tool("install", '[ "$1" != -S ] || shift; exec /usr/bin/install "$@"')
        self.executable(self.src / "install.sh", '''echo install >> "$TEST_LOG"
        [ "${DEFER:-0}" = 0 ] || exit 75
        printf 'candidate\n' > "$HOME/.local/bin/polymux"
        exit "${INSTALL_RESULT:-0}"''')

    def executable(self, path, body):
        path.write_text("#!/bin/sh\nset -eu\n" + body + "\n")
        path.chmod(0o755)

    def tool(self, name, body):
        self.executable(self.tools / name, body)

    def deploy(self, **env):
        return subprocess.run(["/bin/sh", str(SCRIPT)], env=dict(self.env, **env),
                              capture_output=True, text=True)

    def test_success_records_verified_installed_commit(self):
        result = self.deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.state / "sha").read_text().strip(), "new-sha")
        self.assertEqual((self.bin / "polymux").read_text().strip(), "candidate")
        self.assertFalse((self.state / "lock").exists())

    def stable(self):
        data = self.home / ".polymux/data"
        data.mkdir(parents=True, exist_ok=True)
        (data / "settings.json").write_text('{"settings":{"updateChannel":"stable"}}')

    def test_stable_deploys_the_latest_release_without_dev_ci(self):
        self.stable()
        result = self.deploy(RESULT="failure")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.state / "sha").read_text().strip(), "release-sha")
        self.assertIn("rev-list -n 1 v9.9.9", (self.home / "calls").read_text())

    def test_stable_never_moves_the_server_backwards(self):
        self.stable()
        result = self.deploy(BEHIND="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.state / "sha").read_text().strip(), "old")
        self.assertNotIn("install", (self.home / "calls").read_text())

    def test_manual_mode_waits_until_asked(self):
        data = self.home / ".polymux/data"
        data.mkdir(parents=True, exist_ok=True)
        (data / "settings.json").write_text('{"settings":{"updateMode":"manual"}}')
        self.assertEqual(self.deploy().returncode, 0)
        self.assertEqual((self.state / "sha").read_text().strip(), "old")
        result = subprocess.run(["/bin/sh", str(SCRIPT), "now"], env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.state / "sha").read_text().strip(), "new-sha")

    def test_unset_channel_follows_dev(self):
        result = self.deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.state / "sha").read_text().strip(), "new-sha")
        self.assertNotIn("release view", (self.home / "calls").read_text())

    def test_failed_ci_does_not_advance_deployed_commit(self):
        self.assertEqual(self.deploy(RESULT="failure").returncode, 0)
        self.assertEqual((self.state / "sha").read_text().strip(), "old")
        result = self.deploy(SHA="next-sha", UI_ONLY="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("diff --quiet old next-sha -- apps/server packages/runtime packages/memory packages/search packages/vault scripts go.work go.work.sum",
                      (self.home / "calls").read_text())
        self.assertNotIn("install", (self.home / "calls").read_text())

    def test_unsupported_or_cancelled_drain_defers_without_quarantine(self):
        result = self.deploy(DEFER="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.bin / "polymux").read_text().strip(), "original")
        self.assertEqual((self.state / "sha").read_text().strip(), "old")
        self.assertFalse((self.state / "failed-sha").exists())
        self.assertNotIn("launchctl", (self.home / "calls").read_text())

    def rollback_fixture(self):
        self.executable(self.bin / "polymux", '''echo "previous $*" >> "$TEST_LOG"
        if [ "$1" = version ]; then echo original; fi''')
        data = self.home / ".polymux/data"
        data.mkdir(parents=True)
        (data / "maintenance").write_text("test-update-identity")
        self.tool("launchctl", '''echo "launchctl $*" >> "$TEST_LOG"
        if [ "$1" = print ]; then echo 'pid = 42'; fi''')
        self.tool("sleep", ":")
        self.tool("ifconfig", ":")

    def test_rollback_resumes_only_after_verifying_previous_build(self):
        self.rollback_fixture()
        previous = (self.bin / "polymux").read_text()
        result = self.deploy(INSTALL_RESULT="1")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertEqual((self.bin / "polymux").read_text(), previous)
        calls = (self.home / "calls").read_text()
        self.assertLess(calls.index("--id test-update-identity"), calls.index("launchctl bootout"))
        self.assertLess(calls.index("--build original --ready"), calls.index("--cancel"))
        self.assertIn("rolled back; commit quarantined", result.stdout)

    def test_rollback_restarts_previous_build_when_the_server_already_stopped(self):
        self.rollback_fixture()
        result = self.deploy(INSTALL_RESULT="1", CURL_EXIT="7")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        calls = (self.home / "calls").read_text()
        self.assertIn("launchctl bootout", calls)
        self.assertNotIn("--id test-update-identity", calls.split("launchctl bootout")[0])
        self.assertIn("rolled back; commit quarantined", result.stdout)

    def test_failed_restore_never_starts_or_resumes_candidate(self):
        self.rollback_fixture()
        self.tool("install", '''[ "$1" != -S ] || shift
        echo "install $*" >> "$TEST_LOG"
        case "$*" in *polymux.prev*) exit 1 ;; esac
        exec /usr/bin/install "$@"''')
        result = self.deploy(INSTALL_RESULT="1")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        calls = (self.home / "calls").read_text()
        self.assertIn("launchctl bootout", calls)
        self.assertNotIn("launchctl bootstrap", calls)
        self.assertNotIn("--cancel", calls)
        self.assertIn("manual recovery required", result.stdout)

    def test_failure_never_forces_rollback_without_a_drain(self):
        result = self.deploy(INSTALL_RESULT="1")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertEqual((self.bin / "polymux").read_text().strip(), "candidate")
        self.assertEqual((self.state / "sha").read_text().strip(), "old")
        self.assertEqual((self.state / "failed-sha").read_text().strip(), "new-sha")
        calls = (self.home / "calls").read_text()
        self.assertNotIn("launchctl", calls)
        self.assertEqual(self.deploy().returncode, 0)
        self.assertEqual((self.home / "calls").read_text(), calls)


if __name__ == "__main__":
    unittest.main()
