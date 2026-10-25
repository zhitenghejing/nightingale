"""Exercise size checks against real commits, an index and push ref updates."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import check_git_objects as check
from git_test_support import git_fixture_environment


class GitObjectsTest(unittest.TestCase):
    def setUp(self):
        self.enterContext(patch.dict(os.environ, git_fixture_environment(), clear=True))
        self.root = Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.enterContext(patch.object(check, "ROOT", self.root))
        self.git("init", "-qb", "main")
        self.git("config", "user.name", "Object Check Test")
        self.git("config", "user.email", "test@example.invalid")
        (self.root / "baseline").write_text("baseline")
        self.commit("baseline")
        self.base = self.git("rev-parse", "HEAD").decode().strip()

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.root, stderr=subprocess.PIPE)

    def commit(self, message):
        self.git("add", "-A")
        self.git("commit", "-qm", message)
        return self.git("rev-parse", "HEAD").decode().strip()

    def oversized(self, revisions=None, **kwargs):
        return check.oversized_blobs(revisions or ["HEAD", "--not", self.base], limit=64, **kwargs)

    def test_boundary_and_index_check_uses_staged_bytes(self):
        target = self.root / "asset with spaces"
        target.write_bytes(b"a" * 64)
        self.git("add", ".")
        self.assertEqual(self.oversized(include_index=True), [])
        target.write_bytes(b"b" * 65)
        self.git("add", ".")
        target.write_text("smaller worktree version")
        result = self.oversized(include_index=True)
        self.assertEqual([(size, path) for _, size, path in result], [(65, "asset with spaces")])

    def test_deleted_historical_blob_is_still_rejected(self):
        target = self.root / "asset"
        target.write_bytes(b"a" * 65)
        self.commit("add oversized asset")
        oid = self.git("rev-parse", "HEAD:asset").decode().strip()
        target.unlink()
        self.commit("delete asset")
        self.assertEqual(self.oversized(), [(oid, 65, None)])

    def test_index_submodule_without_local_objects_is_ignored(self):
        self.git("update-index", "--add", "--cacheinfo", "160000", "1" * 40, "external")
        self.assertEqual(self.oversized(include_index=True), [])

    def test_new_branch_push_checks_history_despite_a_local_backup(self):
        (self.root / "asset").write_bytes(b"a" * 65)
        tip = self.commit("oversized asset")
        self.git("branch", "backup")
        self.git("update-ref", "refs/remotes/origin/main", self.base)
        revisions = check.push_revisions("origin", f"refs/heads/main {tip} refs/heads/feature {'0' * 40}\n")
        self.assertEqual(len(self.oversized(revisions)), 1)

    def test_update_excludes_known_remote_history(self):
        (self.root / "existing-asset").write_bytes(b"a" * 65)
        previous = self.commit("asset already on remote")
        (self.root / "new-asset").write_text("small")
        tip = self.commit("new change")
        revisions = check.push_revisions("origin", f"refs/heads/main {tip} refs/heads/main {previous}\n")
        self.assertEqual(self.oversized(revisions), [])

    def test_deletion_only_push_has_no_objects(self):
        self.assertEqual(check.push_revisions("origin", f"(delete) {'0' * 40} refs/heads/main {self.base}\n"), [])

    def test_unknown_remote_base_checks_all_local_history(self):
        (self.root / "asset").write_bytes(b"a" * 65)
        tip = self.commit("oversized asset")
        revisions = check.push_revisions("origin", f"refs/heads/main {tip} refs/heads/main {'1' * 40}\n")
        self.assertEqual(len(self.oversized(revisions)), 1)


if __name__ == "__main__":
    unittest.main()
