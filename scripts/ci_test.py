from pathlib import Path
import subprocess
import tempfile
import unittest

import os
from unittest.mock import patch

from ci import affected, changed, plan, reused, selection


class SelectionTests(unittest.TestCase):
    def test_site_only_does_not_build_native_targets(self):
        self.assertEqual(affected(["apps/site/src/content/docs/example.md"]),
                         {"server": False, "swift": False, "plugins": False, "site": True})

    def test_shared_contracts_and_scripts_check_both(self):
        for path in ("contracts/polymux.schema.json", "scripts/run.sh", ".github/workflows/tests.yml"):
            self.assertTrue(all(affected([path])[name] for name in ("server", "swift", "plugins")))

    def test_component_inputs_include_tests_locks_and_deletions(self):
        for path in ("apps/server/internal/example_test.go", "go.work.sum", "packages/runtime/runtime.go",
                     "packages/memory/memory_test.go", "packages/vault/go.mod", "packages/search/search_test.go"):
            self.assertEqual(affected([path]), {"server": True, "swift": False, "plugins": False, "site": False})
        for path in ("Package.resolved", "apps/Polymux/Info.plist", "packages/AvatarKit/removed.swift"):
            self.assertEqual(affected([path]), {"server": False, "swift": True, "plugins": False, "site": False})
        for path in ("plugins/hub/main.go", "plugins/go.sum", "plugins/scripts/package.sh"):
            self.assertEqual(affected([path]), {"server": False, "swift": False, "plugins": True, "site": False})

    def test_dev_combines_promotion_and_latest_push_changes(self):
        event = {"ref": "refs/heads/dev", "before": "prev"}
        cases = [
            (["apps/server/main.go"], [], {"server"}),  # A revert still needs dev deployment checks.
            ([], ["apps/server/main.go"], {"server"}),  # An earlier push is still part of the promotion.
            (["apps/site/src/main.ts"], ["apps/Polymux/Info.plist"], {"swift", "site"}),
            (["plugins/index.json"], [], {"plugins", "site"}),
            (["plugins/hub/internal/hub/bridges.go"], [], {"plugins", "site"}),
            ([], ["apps/server/go.mod"], {"server", "plugins"}),
            ([], ["docs/development.md"], set()),
        ]
        for before, main, checks in cases:
            with self.subTest(before=before, main=main):
                result, base = plan(event, "push", "prod", {"prev": before, "prod": main}.get)
                self.assertEqual({name for name, selected in result.items() if selected}, checks)
                self.assertEqual(base, "prod")

    def test_missing_diff_and_manual_release_run_full_coverage(self):
        event = {"ref": "refs/heads/dev", "before": "absent"}
        for result, base in (plan(event, "push", "prod", {"prod": []}.get),
                             plan({}, "workflow_dispatch", "prod", {}.get)):
            self.assertEqual(base, "full")
            self.assertTrue(all(result.values()))

    def test_manual_delivery_runs_every_check(self):
        self.assertIsNone(selection({}, "workflow_dispatch"))
        self.assertIsNone(selection({"before": "0" * 40}, "push"))
        self.assertIsNone(selection({"before": "existing", "ref": "refs/tags/stable-v1.0.0"}, "push"))
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
            event = {"ref": "refs/heads/dev", "before": "HEAD~1"}
            result, _ = plan(event, "push", "HEAD", lambda base: changed(base, repo))
            self.assertTrue(result["server"])

    def test_main_push_asks_for_reuse_only_with_a_token(self):
        main = {"ref": "refs/heads/main", "before": "old"}
        calls = []
        def find(*args):
            calls.append(args)
            return "candidate"
        with patch.dict(os.environ, {"GH_TOKEN": "t", "GITHUB_REPOSITORY": "o/r", "GITHUB_SHA": "merge"}):
            self.assertEqual(reused(main, "push", "tree", find), "candidate")
            self.assertEqual(calls, [("o/r", "merge", "old", "tree")])
            for event, kind in (({"ref": "refs/heads/dev"}, "push"), (main, "workflow_dispatch"),
                                ({"ref": "refs/tags/v1"}, "push")):
                self.assertIsNone(reused(event, kind, "tree", find))
        with patch.dict(os.environ, {}, clear=True):
            self.assertIsNone(reused(main, "push", "tree", find))
        self.assertEqual(len(calls), 1)


if __name__ == "__main__":
    unittest.main()
