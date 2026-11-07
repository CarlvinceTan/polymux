from pathlib import Path
import subprocess
import tempfile
import unittest

from ci import affected, changed, selection


class SelectionTests(unittest.TestCase):
    def test_site_only_does_not_build_native_targets(self):
        self.assertEqual(affected(["apps/site/src/content/docs/example.md"]),
                         {"server": False, "swift": False})

    def test_shared_contracts_and_scripts_check_both(self):
        for path in ("contracts/polymux.schema.json", "scripts/run.sh", ".github/workflows/tests.yml"):
            self.assertEqual(affected([path]), {"server": True, "swift": True})

    def test_component_inputs_include_tests_locks_and_deletions(self):
        for path in ("apps/server/internal/example_test.go", "go.work.sum", "packages/runtime/runtime.go",
                     "packages/memory/memory_test.go", "packages/vault/go.mod", "packages/search/search_test.go"):
            self.assertEqual(affected([path]), {"server": True, "swift": False})
        for path in ("Package.resolved", "apps/Polymux/Info.plist", "packages/AvatarKit/removed.swift", "tools/control/client.mjs"):
            self.assertEqual(affected([path]), {"server": False, "swift": True})

    def test_manual_delivery_runs_every_check(self):
        self.assertIsNone(selection({}, "workflow_dispatch"))
        self.assertIsNone(selection({"before": "0" * 40}, "push"))
        self.assertIsNone(selection({"before": "existing", "ref": "refs/tags/app-v1.0.0"}, "push"))
        self.assertEqual(selection({"pull_request": {"base": {"sha": "base"}}}, "pull_request"), "base")

    def test_moving_a_file_out_checks_where_it_left(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory)
            def git(*args):
                subprocess.run(["git", "-c", "user.name=ci", "-c", "user.email=ci@example.com",
                                "-c", "commit.gpgsign=false", *args], cwd=repo, check=True, capture_output=True)
            git("init")
            (repo / "apps/server").mkdir(parents=True)
            (repo / "docs").mkdir()
            (repo / "apps/server/main.go").write_text("package main\n" * 20)
            git("add", ".")
            git("commit", "-m", "add")
            git("mv", "apps/server/main.go", "docs/main.go")
            git("commit", "-m", "move")
            self.assertTrue(affected(changed("HEAD~1", repo))["server"])


if __name__ == "__main__":
    unittest.main()
