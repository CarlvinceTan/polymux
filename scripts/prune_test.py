"""Check that prune.sh removes only idle agent worktrees' build output."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest

SCRIPT = Path(__file__).resolve().parent / "prune.sh"


class PruneTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.base = Path(temporary.name).resolve()
        self.main = self.base / "repo"
        self.git("init", "-q", "-b", "dev", str(self.main), cwd=self.base)
        self.git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x", cwd=self.main)
        (self.main / "scripts").mkdir()
        shutil.copy(SCRIPT, self.main / "scripts/prune.sh")
        self.build(self.main)

    def git(self, *args, cwd):
        subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True)

    def build(self, tree):
        data = tree / ".build/xcode-test/Build"
        data.mkdir(parents=True)
        (data / "out").write_bytes(b"x" * 4096)
        (tree / ".build/locks").mkdir()
        (tree / ".build/locks/xcode.lock").touch()

    def worktree(self, name, age_days):
        tree = self.base / ".worktrees/repo" / name
        self.git("worktree", "add", "-q", "--detach", str(tree), cwd=self.main)
        self.build(tree)
        old = time.time() - age_days * 86400
        for path in [tree, *tree.rglob("*")]:
            if ".git" not in path.relative_to(tree).parts[:1]:
                os.utime(path, (old, old), follow_symlinks=False)
        return tree

    def prune(self, *args):
        return subprocess.run([str(self.main / "scripts/prune.sh"), *args], text=True,
                              capture_output=True, check=True).stdout

    def test_only_idle_worktrees_lose_build_output(self):
        idle = self.worktree("idle", 5)
        busy = self.worktree("busy", 0)
        dry = self.prune()
        self.assertIn(str(idle), dry)
        self.assertNotIn(str(busy), dry)
        self.assertTrue((idle / ".build/xcode-test").exists())
        self.prune("--apply")
        self.assertFalse((idle / ".build/xcode-test").exists())
        self.assertTrue((idle / ".build/locks").exists())
        self.assertTrue((busy / ".build/xcode-test").exists())
        self.assertTrue((self.main / ".build/xcode-test").exists())


if __name__ == "__main__":
    unittest.main()
