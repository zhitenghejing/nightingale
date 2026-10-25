"""Frontend build identity must reject stale source and modified assets."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import frontend
from git_test_support import git_fixture_environment


class FrontendTest(unittest.TestCase):
    def setUp(self):
        self.enterContext(patch.dict(os.environ, git_fixture_environment(), clear=True))
        temporary = self.enterContext(tempfile.TemporaryDirectory())
        self.root = Path(temporary)
        self.enterContext(patch.object(frontend, "ROOT", self.root))
        self.fe = self.root / "frontend"
        self.fe.mkdir()
        (self.fe / ".gitignore").write_text("pub/\nnode_modules/\n.build*\n.env*\n")
        (self.fe / "source.ts").write_text("export const version = 1;\n")
        for command in (("init", "-qb", "main"), ("add", "."),
                        ("-c", "user.name=Frontend Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "baseline")):
            subprocess.run(["git", *command], cwd=self.root, check=True, capture_output=True)

    def manifest(self):
        (self.fe / "pub").mkdir(exist_ok=True)
        (self.fe / "pub/index.html").write_text("built version one")
        value = {"sourceSha256": frontend.source_sha(), "sha256": frontend.tree_sha(self.fe / "pub")}
        (self.fe / ".build-manifest.json").write_text(json.dumps(value))
        return value

    def test_source_identity_ignores_install_outputs_but_detects_local_inputs(self):
        before = frontend.source_sha()
        (self.fe / "node_modules").mkdir()
        (self.fe / "node_modules/dependency.js").write_text("installed")
        self.assertEqual(before, frontend.source_sha())
        (self.fe / ".env.production").write_text("VITE_PREFIX=/preview")
        self.assertNotEqual(before, frontend.source_sha())
        (self.fe / ".env.production").unlink()
        (self.fe / "new.ts").write_text("new source")
        self.assertNotEqual(before, frontend.source_sha())
        (self.fe / "new.ts").unlink()
        (self.fe / "source.ts").unlink()
        self.assertNotEqual(before, frontend.source_sha())

    def test_stale_source_or_modified_assets_cannot_reuse_a_build(self):
        value = self.manifest()
        self.assertEqual(value, frontend.current())
        (self.fe / "pub/index.html").write_text("tampered")
        self.assertIsNone(frontend.current())
        self.manifest()
        (self.fe / "source.ts").write_text("changed source")
        self.assertIsNone(frontend.current())

    def test_failed_rebuild_invalidates_previous_manifest(self):
        self.manifest()
        with patch.object(frontend, "check_toolchain"), \
                patch.object(frontend, "install", side_effect=RuntimeError("install failed")):
            with self.assertRaisesRegex(RuntimeError, "install failed"):
                frontend.build(force=True)
        self.assertIsNone(frontend.current())
        self.assertFalse((self.fe / ".build-manifest.json").exists())

    def previous_archive(self):
        target = self.root / "front/statik/assets.zip"
        target.parent.mkdir(parents=True)
        target.write_bytes(b"previous archive")
        return target

    def test_embedding_preserves_all_assets_and_is_reproducible(self):
        self.manifest()
        nested = self.fe / "pub/subdir"
        nested.mkdir()
        (nested / "说明").write_bytes(b"\x00\xffextensionless asset")
        (nested / ".hidden").write_text("hidden asset")
        manifest = self.manifest()
        target = self.root / "front/statik/assets.zip"
        with patch.object(frontend, "build", return_value=manifest):
            frontend.embed()
            before = target.read_bytes()
            with zipfile.ZipFile(target) as archive:
                expected = {p.relative_to(self.fe / "pub").as_posix(): p.read_bytes()
                            for p in (self.fe / "pub").rglob("*") if p.is_file()}
                self.assertEqual({name: archive.read(name) for name in archive.namelist()}, expected)
                self.assertIsNone(archive.testzip())
            for path in (self.fe / "pub").rglob("*"):
                os.utime(path, (1700000000, 1700000000))
                path.chmod(0o700)
            frontend.embed()
        self.assertEqual(before, target.read_bytes())

    def test_embedding_rejects_source_changes_without_replacing_archive(self):
        manifest = self.manifest()
        target = self.previous_archive()
        real_pack = frontend.pack_assets

        def pack(directory, destination):
            real_pack(directory, destination)
            (self.fe / "source.ts").write_text("source edited while embedding")

        with patch.object(frontend, "build", return_value=manifest), patch.object(frontend, "pack_assets", side_effect=pack):
            with self.assertRaisesRegex(RuntimeError, "changed while embedding"):
                frontend.embed()
        self.assertEqual(target.read_bytes(), b"previous archive")

    def test_embedding_rejects_asset_changes_without_replacing_archive(self):
        manifest = self.manifest()
        target = self.previous_archive()
        real_pack = frontend.pack_assets

        def pack(directory, destination):
            real_pack(directory, destination)
            (self.fe / "pub/index.html").write_text("assets changed while embedding")

        with patch.object(frontend, "build", return_value=manifest), patch.object(frontend, "pack_assets", side_effect=pack):
            with self.assertRaisesRegex(RuntimeError, "changed while embedding"):
                frontend.embed()
        self.assertEqual(target.read_bytes(), b"previous archive")

    def test_failed_packing_keeps_the_committed_archive(self):
        manifest = self.manifest()
        target = self.previous_archive()
        with patch.object(frontend, "build", return_value=manifest), \
                patch.object(frontend, "pack_assets", side_effect=OSError("packing failed")):
            with self.assertRaisesRegex(OSError, "packing failed"):
                frontend.embed()
        self.assertEqual(target.read_bytes(), b"previous archive")
        self.assertEqual(list(target.parent.iterdir()), [target])

    def test_oversized_archive_keeps_the_committed_archive(self):
        manifest = self.manifest()
        target = self.previous_archive()

        def pack(directory, destination):
            with destination.open("wb") as stream:
                stream.truncate(100 * 1024 * 1024 + 1)

        with patch.object(frontend, "build", return_value=manifest), patch.object(frontend, "pack_assets", side_effect=pack):
            with self.assertRaisesRegex(RuntimeError, "100 MiB"):
                frontend.embed()
        self.assertEqual(target.read_bytes(), b"previous archive")


if __name__ == "__main__":
    unittest.main()
