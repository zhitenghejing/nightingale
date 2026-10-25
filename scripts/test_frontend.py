"""Frontend build identity must reject stale source and modified assets."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

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

    def test_embedding_rejects_source_changes_without_replacing_statik(self):
        manifest = self.manifest()
        target = self.root / "front/statik/statik.go"
        target.parent.mkdir(parents=True)
        target.write_text("previous statik")
        real_run = subprocess.run

        def run(command, **kwargs):
            if command[0] != "go":
                return real_run(command, **kwargs)
            destination = Path(next(c.removeprefix("-dest=") for c in command if c.startswith("-dest=")))
            (destination / "statik").mkdir()
            (destination / "statik/statik.go").write_text("generated statik")
            (self.fe / "source.ts").write_text("source edited while embedding")
            return subprocess.CompletedProcess(command, 0)

        with patch.object(frontend, "build", return_value=manifest), patch.object(frontend.subprocess, "run", side_effect=run):
            with self.assertRaisesRegex(RuntimeError, "changed while embedding"):
                frontend.embed()
        self.assertEqual(target.read_text(), "previous statik")

    def test_e2e_uses_lan_without_proxy_and_requires_both_entries_healthy(self):
        status = {"role": "development", "running": True, "sourceChanged": False,
                  "frontendChanged": False, "frontend": {"kind": "in-tree"},
                  "local": {"ok": True}, "public": {"ok": True}, "lan": {"ok": True},
                  "lanUrl": "http://192.168.0.152:30300", "url": "https://n9e-dev0.zhiteng.win",
                  "port": 30300, "buildId": "current", "credentialsFile": "/private/credentials.json"}
        with patch.object(frontend, "check_toolchain"), \
                patch.object(frontend.subprocess, "check_output", side_effect=lambda *a, **k: json.dumps(status).encode()), \
                patch.object(frontend, "run") as run, \
                patch.dict(os.environ, {"N9E_E2E_PROXY_SERVER": "http://host-proxy:8080"}):
            frontend.e2e()
            environment = run.call_args.kwargs["env"]
            self.assertEqual(environment["N9E_E2E_BASE_URL"], status["lanUrl"])
            self.assertNotIn("N9E_E2E_PROXY_SERVER", environment)
            frontend.e2e(local=True)
            self.assertEqual(run.call_args.kwargs["env"]["N9E_E2E_BASE_URL"], "http://127.0.0.1:30300")
            self.assertNotIn("N9E_E2E_PROXY_SERVER", run.call_args.kwargs["env"])
            status["lan"]["ok"] = False
            with self.assertRaisesRegex(RuntimeError, "refresh"):
                frontend.e2e()
            status["lan"]["ok"] = True
            status["public"]["ok"] = False
            with self.assertRaisesRegex(RuntimeError, "refresh"):
                frontend.e2e()


class FrontendE2ETest(unittest.TestCase):
    def status(self, public_ok=True):
        return {"role": "development", "running": True, "sourceChanged": False, "frontendChanged": False,
                "frontend": {"kind": "in-tree"}, "local": {"ok": True}, "public": {"ok": public_ok},
                "url": "https://preview.example", "port": 30301, "buildId": "test-build",
                "credentialsFile": "/private/credentials.json"}

    def test_local_e2e_uses_verified_loopback_and_removes_browser_proxy(self):
        with patch.object(frontend, "check_toolchain"), patch.object(frontend.subprocess, "check_output", return_value=json.dumps(self.status()).encode()), \
                patch.object(frontend, "run") as run, patch.dict(os.environ, {"N9E_E2E_PROXY_SERVER": "http://proxy.invalid"}):
            frontend.e2e(local=True)
        environment = run.call_args.kwargs["env"]
        self.assertEqual(environment["N9E_E2E_BASE_URL"], "http://127.0.0.1:30301")
        self.assertEqual(environment["N9E_E2E_BUILD_ID"], "test-build")
        self.assertNotIn("N9E_E2E_PROXY_SERVER", environment)

    def test_local_e2e_still_rejects_failed_public_readiness(self):
        with patch.object(frontend, "check_toolchain"), patch.object(frontend.subprocess, "check_output", return_value=json.dumps(self.status(False)).encode()), \
                patch.object(frontend, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "refresh"):
                frontend.e2e(local=True)
            run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
