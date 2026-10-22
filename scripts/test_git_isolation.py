"""Run the fixture suites with Git hook variables pointing at a disposable host."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from git_test_support import git_fixture_environment


SCRIPTS = Path(__file__).resolve().parent


class GitIsolationTest(unittest.TestCase):
    def test_hook_repository_is_unchanged_by_fixture_suites(self):
        environment = git_fixture_environment()
        with tempfile.TemporaryDirectory(prefix="n9e-git-isolation-") as directory:
            base = Path(directory)
            repository = base / "repository"
            worktree = base / "worktree"
            repository.mkdir()

            def git(root, *args):
                return subprocess.check_output(
                    ["git", *args], cwd=root, env=environment, stderr=subprocess.PIPE,
                )

            git(repository, "init", "-qb", "main")
            git(repository, "config", "user.name", "Host Repository")
            git(repository, "config", "user.email", "host@example.invalid")
            (repository / "sentinel.txt").write_text("preserve committed content\n")
            git(repository, "add", ".")
            git(repository, "commit", "-qm", "host baseline")
            git(repository, "worktree", "add", "-qb", "feature", str(worktree))
            (worktree / "staged.txt").write_text("preserve staged content\n")
            git(worktree, "add", "staged.txt")
            (worktree / "untracked.txt").write_text("preserve untracked content\n")
            git_dir = Path(git(worktree, "rev-parse", "--absolute-git-dir").decode().strip())
            common_dir = repository / ".git"

            def snapshot():
                # Read status before index bytes, since status can refresh the index.
                status = git(worktree, "status", "--porcelain=v1", "-z")
                return {
                    "head": (git_dir / "HEAD").read_bytes(),
                    "main_head": (common_dir / "HEAD").read_bytes(),
                    "refs": git(repository, "show-ref"),
                    "status": status,
                    "index": (git_dir / "index").read_bytes(),
                    "main_index": (common_dir / "index").read_bytes(),
                    "config": (common_dir / "config").read_bytes(),
                    "files": {p.name: p.read_bytes() for p in worktree.iterdir() if p.is_file()},
                }

            before = snapshot()
            hook_environment = {
                **environment,
                "GIT_DIR": str(git_dir),
                "GIT_COMMON_DIR": str(common_dir),
                "GIT_WORK_TREE": str(worktree),
                "GIT_INDEX_FILE": str(git_dir / "index"),
                "GIT_PREFIX": "",
            }
            result = subprocess.run(
                [sys.executable, "-B", "-m", "unittest", "test_preview", "test_verify"],
                cwd=SCRIPTS, env=hook_environment, capture_output=True, text=True, timeout=60,
            )
            after = snapshot()
            self.assertEqual(before, after, "fixture suites modified the hook's repository")
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
