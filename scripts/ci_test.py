import unittest

from ci import affected, selection


class SelectionTests(unittest.TestCase):
    def test_site_only_does_not_build_native_targets(self):
        self.assertEqual(affected(["apps/site/src/content/docs/example.md"]), {"server": False, "tauri": False})

    def test_shared_contracts_and_scripts_check_both(self):
        for path in ("contracts/polymux.schema.json", "scripts/run.sh", ".github/workflows/tauri.yml"):
            self.assertEqual(affected([path]), {"server": True, "tauri": True})

    def test_component_inputs_include_tests_locks_and_deletions(self):
        self.assertEqual(affected(["apps/server/internal/example_test.go"]), {"server": True, "tauri": True})
        for path in ("go.work.sum", "packages/runtime/runtime.go",
                     "packages/memory/memory_test.go", "packages/vault/go.mod", "packages/search/search_test.go"):
            self.assertEqual(affected([path]), {"server": True, "tauri": False})
        for path in ("apps/tauri/src/App.svelte", "apps/tauri/package-lock.json",
                     "apps/tauri/src-tauri/Cargo.lock", "e2e/tauri/create.js"):
            self.assertEqual(affected([path]), {"server": False, "tauri": True})

    def test_manual_delivery_runs_every_check(self):
        self.assertIsNone(selection({}, "workflow_dispatch"))
        self.assertIsNone(selection({"before": "0" * 40}, "push"))
        self.assertIsNone(selection({"before": "existing", "ref": "refs/tags/app-v1.0.0"}, "push"))
        self.assertEqual(selection({"pull_request": {"base": {"sha": "base"}}}, "pull_request"), "base")


if __name__ == "__main__":
    unittest.main()
