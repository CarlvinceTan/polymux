import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


CHECK = Path(__file__).with_name("check.py").resolve()


class CheckTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="polymux-audit-tests-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = {
            **os.environ,
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_OPTIONAL_LOCKS": "0",
            "GIT_AUTHOR_NAME": "Audit test",
            "GIT_AUTHOR_EMAIL": "audit@example.invalid",
            "GIT_COMMITTER_NAME": "Audit test",
            "GIT_COMMITTER_EMAIL": "audit@example.invalid",
        }
        self.git("init", "-q")
        self.put("go.work", "go 1.26.0\n")
        self.put("apps/server/go.mod", "module example.invalid/audit\n\ngo 1.26.0\n")
        self.put("apps/site/package.json", '{"dependencies": {}}\n')
        self.put("apps/site/package-lock.json", "{}\n")
        self.put("src/example.ts", "let value = 1\n")
        self.put("tracked.txt", "original\n")
        self.put(".gitignore", "ignored/\n")
        self.save()

    def git(self, *args, check=True):
        return subprocess.run(
            ["git", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", *args],
            cwd=self.root, env=self.env, capture_output=True, check=check,
        )

    def put(self, path, text):
        file = self.root / path
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(text)

    def save(self):
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")
        return self.git("rev-parse", "HEAD").stdout.decode().strip()

    def run_check(self, *args, code=0):
        result = subprocess.run(
            [sys.executable, "-B", str(CHECK), *args],
            cwd=self.root, env=self.env, capture_output=True, text=True,
        )
        self.assertEqual(result.returncode, code, result.stdout + result.stderr)
        return result.stdout + result.stderr

    def test_clean_and_empty_staged_scope(self):
        self.put("untracked.txt", "bad \n")
        output = self.run_check("--staged")
        self.assertIn("Changed paths: 0", output)
        self.assertNotIn("untracked.txt", output)

    def test_first_commit_has_staged_checks_but_no_worktree_baseline(self):
        self.git("checkout", "--orphan", "first")
        self.run_check("--staged")
        self.assertIn("UNVERIFIED", self.run_check("--worktree", code=2))

    def test_staged_content_not_unstaged_fix(self):
        self.put("tracked.txt", "bad \n")
        self.git("add", "tracked.txt")
        self.put("tracked.txt", "fixed\n")
        self.assertIn('[whitespace]', self.run_check("--staged", code=1))
        self.run_check("--worktree")

    def test_unstaged_failure_does_not_taint_staged_scope(self):
        self.put("tracked.txt", "staged\n")
        self.git("add", "tracked.txt")
        self.put("tracked.txt", "bad \n")
        self.run_check("--staged")
        self.run_check("--worktree", code=1)

    def test_worktree_includes_staged_unstaged_and_untracked(self):
        self.put("tracked.txt", "bad \n")
        self.git("add", "tracked.txt")
        self.put("new.txt", "bad \n")
        self.put("ignored/skip.txt", "bad \n")
        output = self.run_check(code=1)
        self.assertIn('"tracked.txt":1 [whitespace]', output)
        self.assertIn('"new.txt":1 [whitespace]', output)
        self.assertNotIn("skip.txt", output)

    def test_range_uses_committed_endpoint_only(self):
        base = self.git("rev-parse", "HEAD").stdout.decode().strip()
        self.put("src/example.ts", "bad \n")
        head = self.save()
        self.put("src/example.ts", "fixed\n")
        self.put("new.txt", "bad \n")
        output = self.run_check("--base", base, "--head", head, code=1)
        self.assertIn("[whitespace]", output)
        self.assertNotIn("new.txt", output)

    def test_existing_matches_are_not_new_findings(self):
        self.put("src/example.ts", "bad \nlet value = 1\n")
        self.save()
        self.put("src/example.ts", "bad \nlet value = 2\n")
        self.assertNotIn("[whitespace]", self.run_check())

    def test_path_filters_apply_to_tracked_and_untracked_files(self):
        self.put("src/example.ts", "bad \n")
        self.put("new.txt", "bad \n")
        self.assertNotIn("new.txt", self.run_check("--worktree", "--", "src/example.ts", code=1))
        self.assertIn('"new.txt"', self.run_check("--worktree", "--", "new.txt", code=1))
        self.put("nested/other.txt", "bad \n")
        output = self.run_check("--", "nested", code=1)
        self.assertIn("nested/other.txt", output)
        self.assertNotIn('"new.txt"', output)

    def test_go_versions_use_index_and_range_snapshots(self):
        base = self.git("rev-parse", "HEAD").stdout.decode().strip()
        self.put("go.work", "go 1.27.0\n")
        self.git("add", "go.work")
        self.put("apps/server/go.mod", "module example.invalid/audit\n\ngo 1.27.0\n")
        self.assertIn("Go directives differ", self.run_check("--staged", code=1))
        self.run_check("--worktree")
        self.git("commit", "-qm", "mismatched staged fixture")
        self.assertIn("Go directives differ", self.run_check("--base", base, code=1))

    def test_lagging_toolchain_fails(self):
        self.put("go.work", "go 1.26.0\ntoolchain go1.25.0\n")
        self.assertIn("Lagging toolchain", self.run_check(code=1))
        self.put("go.work", "go 1.26.0\ntoolchain go1.26.0\n")
        self.assertIn("REVIEW", self.run_check())

    def test_lockfile_and_permissions_are_review_candidates(self):
        self.put("apps/site/package.json", '{"dependencies": {"example": "1.0.0"}}\n')
        self.put("README.md", "Changed overview\n")
        output = self.run_check()
        self.assertIn("[lockfile]", output)
        self.assertIn("[permission]", output)
        self.assertNotIn("FAIL", output)
        self.put("apps/site/package-lock.json", '{"changed": true}\n')
        self.assertNotIn("[lockfile]", self.run_check())

    def test_tauri_manifest_requires_its_lockfile(self):
        self.put("apps/tauri/package.json", '{"dependencies": {"example": "1.0.0"}}\n')
        self.put("apps/tauri/package-lock.json", "{}\n")
        self.save()
        self.put("apps/tauri/package.json", '{"dependencies": {"example": "2.0.0"}}\n')
        self.assertIn("[lockfile]", self.run_check())
        self.put("apps/tauri/package-lock.json", '{"changed": true}\n')
        self.assertNotIn("[lockfile]", self.run_check())

    def test_secrets_are_not_printed(self):
        secret = "sensitive-example-do-not-print"
        marker = "-----BEGIN " + "PRIVATE KEY-----"
        self.put("key.txt", marker + " " + secret + "\n")
        self.put("tracked.txt", secret + " \n")
        output = self.run_check(code=1)
        self.assertIn("[private-key]", output)
        self.assertNotIn(secret, output)
        self.assertNotIn(marker, output)

    def test_binary_and_symlinks_are_not_followed(self):
        self.put("tracked.bin", "baseline\n")
        self.save()
        (self.root / "tracked.bin").write_bytes(b"\0bad \n")
        (self.root / "new.bin").write_bytes(b"\0bad \n")
        self.put("ignored/target.txt", "bad \n")
        (self.root / "link.txt").symlink_to(self.root / "ignored/target.txt")
        output = self.run_check()
        self.assertIn("2 untracked binary/symlink skip(s)", output)
        self.assertNotIn("[whitespace]", output)

    def test_quoted_unicode_and_control_character_paths(self):
        names = ('space file.txt', 'quote"file.txt', "back\\slash.txt", "café.txt", "tab\tfile.txt", "line\nbreak.txt")
        for path in names:
            self.put(path, "clean\n")
        self.save()
        for path in names:
            self.put(path, "bad \n")
        output = self.run_check(code=1)
        for path in names:
            self.assertIn(json.dumps(path) + ":1 [whitespace]", output)

    def test_added_source_cannot_be_mistaken_for_patch_header(self):
        self.put("tracked.txt", "++ not-a-path \n")
        self.assertIn('"tracked.txt":1 [whitespace]', self.run_check(code=1))

    def test_deletions_do_not_scan_deleted_content(self):
        self.put("src/example.ts", "bad \n")
        self.save()
        (self.root / "src/example.ts").unlink()
        self.assertNotIn("[whitespace]", self.run_check())

    def test_conflicts_and_unmerged_entries_fail(self):
        self.put("tracked.txt", "<<<<<<< HEAD\noriginal\n=======\nother\n>>>>>>> branch\n")
        self.assertIn("[conflict]", self.run_check(code=1))
        self.git("checkout", "--", "tracked.txt")
        branch = self.git("branch", "--show-current").stdout.decode().strip()
        self.git("checkout", "-qb", "other")
        self.put("tracked.txt", "other\n")
        self.save()
        self.git("checkout", "-q", branch)
        self.put("tracked.txt", "current\n")
        self.save()
        self.git("merge", "other", check=False)
        self.assertIn("[unmerged]", self.run_check("--staged", code=1))

    def test_check_does_not_modify_source_or_index_or_run_external_diff(self):
        marker = self.root / "external-ran"
        self.git("config", "diff.external", "touch " + str(marker))
        self.put("tracked.txt", "bad \n")
        self.git("add", "tracked.txt")
        self.put("new.txt", "new\n")
        before = {str(path): path.read_bytes() for path in self.root.rglob("*")
                  if path.is_file() and ".git" not in path.relative_to(self.root).parts}
        index = (self.root / ".git/index").read_bytes()
        self.run_check("--staged", code=1)
        self.run_check("--worktree", code=1)
        after = {str(path): path.read_bytes() for path in self.root.rglob("*")
                 if path.is_file() and ".git" not in path.relative_to(self.root).parts}
        self.assertEqual(before, after)
        self.assertEqual(index, (self.root / ".git/index").read_bytes())
        self.assertFalse(marker.exists())

    def test_invalid_scopes_are_unverified_not_passed(self):
        self.assertIn("UNVERIFIED", self.run_check("--base", "missing-ref", code=2))
        self.assertIn("UNVERIFIED", self.run_check("--base", "", code=2))
        self.assertIn("UNVERIFIED", self.run_check("--base", "HEAD", "--head", "missing-ref", code=2))
        self.assertIn("UNVERIFIED", self.run_check("--base", "HEAD", "--head", "", code=2))
        self.assertIn("requires --base", self.run_check("--head", "HEAD", code=2))
        self.assertIn("UNVERIFIED", self.run_check("--", "../outside", code=2))
        self.assertIn("UNVERIFIED", self.run_check("--", str(self.root), code=2))


if __name__ == "__main__":
    unittest.main()
