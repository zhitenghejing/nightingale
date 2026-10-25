"""Exercise resource ownership, source identity and readiness failure paths."""
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
import sys
import shutil
import subprocess
import tempfile
import tomllib
import unittest
from unittest.mock import patch

from git_test_support import git_fixture_environment

spec = importlib.util.spec_from_file_location("preview", Path(__file__).with_name("preview.py"))
preview = importlib.util.module_from_spec(spec)
spec.loader.exec_module(preview)


class PreviewTest(unittest.TestCase):
    def setUp(self):
        # Cover both fixture setup and preview's Git calls when ROOT is patched.
        self.enterContext(patch.dict(os.environ, git_fixture_environment(), clear=True))
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.state = Path(self.temp.name)
        patcher = patch.object(preview, "STATE", self.state)
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_committed_frontend_identity_tracks_zip_and_legacy_bundles(self):
        root = self.state / "repo"
        directory = root / "front/statik"
        directory.mkdir(parents=True)
        legacy = directory / "statik.go"
        legacy.write_text("legacy embedded data")
        with patch.object(preview, "ROOT", root):
            self.assertEqual(preview.committed_frontend()["sha256"], preview.digest(legacy))
            bundle = directory / "assets.zip"
            bundle.write_bytes(b"new ZIP payload")
            self.assertEqual(preview.committed_frontend()["sha256"], preview.digest(bundle))
            before = preview.committed_frontend()
            bundle.write_bytes(b"changed ZIP payload")
            self.assertNotEqual(before, preview.committed_frontend())

    def test_generated_config_disables_service_agent_and_proxy_auth(self):
        for profile in ("basic", "full"):
            with self.subTest(profile=profile):
                binding = {"instance": "test-instance", "profile": profile}
                directory = preview.instance(binding)
                directory.mkdir(parents=True, exist_ok=True)
                (directory / "postgres.json").write_text(json.dumps({"dsn": "host=127.0.0.1 dbname=test user=test"}))
                config = tomllib.loads(preview.app_config(binding, 30300))
                for section in ("APIForService", "APIForAgent", "ProxyAuth"):
                    self.assertIs(config["HTTP"].get(section, {}).get("Enable"), False, section)
                self.assertIs(config["HTTP"]["TokenAuth"]["Enable"], True)
                self.assertEqual(config["DB"]["DBType"], "postgres")
                self.assertEqual(config["Redis"]["RedisType"], "miniredis")
                self.assertFalse(config["EmbeddedTSDB"]["Enable"])

    def test_private_collector_uses_authenticated_agent_and_opted_in_shared_prometheus(self):
        binding = {"instance": "collector-instance", "role": "development", "profile": "basic"}
        directory = preview.instance(binding)
        directory.mkdir(parents=True)
        (directory / "postgres.json").write_text(json.dumps({"dsn": "isolated-preview", "prometheusURL": "http://127.0.0.1:39090"}))
        auth = {"username": "preview-collector", "password": 'private-secret-"\\with-escapes'}
        (directory / "collector.json").write_text(json.dumps(auth))
        with self.assertRaisesRegex(ValueError, "remote-write opt-in"):
            preview.app_config(binding, 30310)
        shared = self.state / "shared"
        shared.mkdir()
        (shared / "prometheus-remote-write.enabled").touch()
        for port in (30310, 30312):
            config = tomllib.loads(preview.app_config(binding, port))
            self.assertTrue(config["HTTP"]["APIForAgent"]["Enable"])
            self.assertEqual(config["HTTP"]["APIForAgent"]["BasicAuth"], {auth["username"]: auth["password"]})
            self.assertFalse(config["HTTP"]["APIForService"]["Enable"])
            self.assertFalse(config["HTTP"]["ProxyAuth"]["Enable"])
            self.assertEqual(config["HTTP"]["Host"], "127.0.0.1")
            self.assertFalse(config["EmbeddedTSDB"]["Enable"])
            self.assertEqual(config["Pushgw"]["Writers"], [{"Url": "http://127.0.0.1:39090/api/v1/write"}])
            self.assertFalse(config["Ibex"]["Enable"])
        binding["role"] = "stable"
        with self.assertRaisesRegex(ValueError, "private development collector"):
            preview.app_config(binding, 30310)
        binding["role"] = "development"
        (directory / "collector.json").write_text(json.dumps({"username": "collector", "password": ""}))
        with self.assertRaises(ValueError):
            preview.app_config(binding, 30310)

    def test_in_tree_preview_detects_ignored_frontend_configuration_changes(self):
        root = self.state / "repo"
        (root / "scripts").mkdir(parents=True)
        (root / "frontend/pub").mkdir(parents=True)
        shutil.copyfile(Path(__file__).with_name("frontend.py"), root / "scripts/frontend.py")
        (root / "frontend/.gitignore").write_text(".env*\npub/\n")
        (root / "frontend/pub/index.html").write_text("compiled UI")
        for args in (("init", "-qb", "main"), ("add", "."),
                     ("-c", "user.name=Preview Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "baseline")):
            subprocess.run(["git", *args], cwd=root, check=True, capture_output=True)
        binding = {"instance": "config-drift", "slot": 0, "port": 1}
        directory = preview.instance(binding)
        directory.mkdir(parents=True)
        fingerprint = subprocess.check_output(["python3", "-B", "scripts/frontend.py", "source"], cwd=root).decode().strip()
        with patch.object(preview, "ROOT", root):
            manifest = {"source": preview.source_identity(), "frontend": {"kind": "in-tree",
                        "sourceSha256": fingerprint, "sha256": preview.tree_digest(root / "frontend/pub")}}
            (directory / "manifest.json").write_text(json.dumps(manifest))
            self.assertFalse(preview.inspect(binding, public=False)["frontendChanged"])
            (root / "frontend/.env.production").write_text("VITE_PREFIX=/changed")
            status = preview.inspect(binding, public=False)
            self.assertFalse(status["sourceChanged"])
            self.assertTrue(status["frontendChanged"])

    def test_slot_zero_is_valid_and_binding_cannot_be_stolen(self):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        with patch.object(preview, "config", return_value={"slots": [{"id": 0, "port": port}]}):
            binding = preview.reserve({}, 0)
            self.assertEqual(binding["slot"], 0)
            with self.assertRaisesRegex(RuntimeError, "no free slot"):
                preview.reserve({"other-worktree": binding}, 0)

    def test_invalid_host_config_rejected(self):
        (self.state / "config.json").write_text(json.dumps({"slots": [
            {"id": 0, "port": 30300}, {"id": 1, "port": 30300}]}))
        with self.assertRaisesRegex(ValueError, "duplicate"):
            preview.config()

    def test_defaults_reserve_ten_development_hosts_and_a_separate_stable_entry(self):
        cfg = preview.DEFAULT_CONFIG
        self.assertEqual(cfg["stable"]["url"], "https://n9e.zhiteng.win")
        self.assertEqual([s["id"] for s in cfg["slots"]], list(range(10)))
        self.assertEqual([s["url"] for s in cfg["slots"]],
                         [f"https://n9e-dev{i}.zhiteng.win" for i in range(10)])
        self.assertNotIn(cfg["stable"]["port"], [s["port"] for s in cfg["slots"]])
        self.assertNotEqual(preview.key(True), preview.key(False))

    def test_development_slots_cannot_use_stable_hostname(self):
        (self.state / "config.json").write_text(json.dumps({"slots": [
            {"id": 0, "port": 30300, "url": "https://n9e.zhiteng.win/"}]}))
        with self.assertRaisesRegex(ValueError, "stable hostname is reserved"):
            preview.config()

    def test_stable_binding_is_outside_the_development_pool(self):
        sockets = [socket.socket(), socket.socket()]
        for sock in sockets:
            sock.bind(("127.0.0.1", 0))
        ports = [sock.getsockname()[1] for sock in sockets]
        for sock in sockets:
            sock.close()
        cfg = {"stable": {"port": ports[0], "branch": "main", "url": "https://n9e.zhiteng.win"},
               "slots": [{"id": 0, "port": ports[1], "url": "https://n9e-dev0.zhiteng.win"}]}
        with patch.object(preview, "config", return_value=cfg):
            stable = preview.reserve({}, None, stable=True)
            development = preview.reserve({"stable": stable}, 0)
            self.assertEqual(stable["role"], "stable")
            self.assertEqual(development["role"], "development")
            self.assertNotEqual(stable["port"], development["port"])
            with self.assertRaisesRegex(RuntimeError, "no free slot"):
                preview.reserve({"stable": stable}, None, stable=True)

    def test_stable_requires_expected_clean_branch(self):
        repo = self.state / "main-checkout"
        repo.mkdir()
        def git(*args):
            subprocess.run(["git", *args], cwd=repo, check=True, capture_output=True)
        git("init", "-qb", "main")
        git("config", "user.name", "Preview Test")
        git("config", "user.email", "preview@example.invalid")
        (repo / "source.txt").write_text("committed")
        git("add", ".")
        git("commit", "-qm", "stable baseline")
        with patch.object(preview, "ROOT", repo), patch.object(preview, "config", return_value={"stable": {"branch": "main"}}):
            preview.validate_stable_source()
            git("checkout", "-qb", "feature")
            with self.assertRaisesRegex(RuntimeError, "requires branch main"):
                preview.validate_stable_source()
            git("checkout", "-q", "main")
            (repo / "source.txt").write_text("uncommitted")
            with self.assertRaisesRegex(RuntimeError, "must be clean"):
                preview.validate_stable_source()

    def test_listener_owned_outside_registry_is_not_available(self):
        with socket.socket() as sock:
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            sock.bind(("127.0.0.1", 0))
            sock.listen()
            with patch.object(preview, "config", return_value={"slots": [{"id": 0, "port": sock.getsockname()[1]}]}):
                with self.assertRaisesRegex(RuntimeError, "no free slot"):
                    preview.reserve({}, 0)

    def test_lan_host_requires_an_explicit_private_ipv4_address(self):
        for host in ("0.0.0.0", "127.0.0.1", "8.8.8.8", "localhost", "::", "fd00::1", "192.0.2.1", 123, ""):
            with self.subTest(host=host), self.assertRaisesRegex(ValueError, "RFC1918"):
                preview.validate_lan_host(host)
        for host in ("10.0.0.1", "172.16.0.1", "192.168.0.152"):
            self.assertEqual(preview.validate_lan_host(host), host)

    def test_configured_lan_is_persisted_only_for_development(self):
        cfg = {"lan": {"host": "192.168.0.152"},
               "stable": {"port": 31000}, "slots": [{"id": 0, "port": 31001}]}
        with patch.object(preview, "config", return_value=cfg), patch.object(preview, "check_listeners"):
            binding = preview.reserve({}, 0)
            self.assertEqual(binding["lanHost"], cfg["lan"]["host"])
            self.assertNotIn("lanHost", preview.reserve({}, 0, lan_host=""))
            self.assertNotIn("lanHost", preview.reserve({}, None, stable=True))
            with self.assertRaisesRegex(ValueError, "stable"):
                preview.reserve({}, None, stable=True, lan_host=cfg["lan"]["host"])

    def test_lan_port_conflict_and_unavailable_address_block_startup(self):
        # Model the second interface with another loopback address, so this test
        # also runs on CI hosts without a private interface.
        with socket.socket() as occupied:
            occupied.bind(("127.0.0.2", 0))
            occupied.listen()
            port = occupied.getsockname()[1]
            binding = {"port": port, "lanHost": "127.0.0.2"}
            with patch.object(preview, "validate_lan_host", side_effect=lambda host: host):
                with self.assertRaisesRegex(RuntimeError, f"127.0.0.2:{port}"):
                    preview.check_listeners(binding)
                with patch.object(preview, "config", return_value={"slots": [{"id": 0, "port": port}]}):
                    with self.assertRaisesRegex(RuntimeError, "no free slot.*127.0.0.2"):
                        preview.reserve({}, 0, lan_host="127.0.0.2")
            # Failed LAN validation/bind must release the earlier loopback socket.
            preview.check_listeners({"port": port})
        with patch.object(preview.socket, "socket") as factory:
            factory.return_value.__enter__.return_value.bind.side_effect = OSError("Cannot assign requested address")
            with self.assertRaisesRegex(RuntimeError, "Cannot assign requested address"):
                preview.check_listeners({"port": 31001, "lanHost": "192.168.0.152"})

    def test_lan_url_selection_preserves_public_url_and_rejects_missing_entry(self):
        binding = {"port": 30300, "lanHost": "192.168.0.152", "url": "https://n9e-dev0.zhiteng.win"}
        self.assertEqual(preview.entry_urls(binding), ["http://192.168.0.152:30300", binding["url"]])
        self.assertEqual(preview.entry_urls(binding, lan=True), ["http://192.168.0.152:30300"])
        self.assertEqual(preview.entry_urls(binding, public=True), [binding["url"]])
        del binding["lanHost"]
        self.assertEqual(preview.entry_urls(binding), [binding["url"]])
        with self.assertRaisesRegex(RuntimeError, "no LAN entry"):
            preview.entry_urls(binding, lan=True)

    def test_url_cli_prints_both_entries_and_explicit_options_print_one(self):
        binding = {"port": 30300, "lanHost": "192.168.0.152", "url": "https://n9e-dev0.zhiteng.win"}
        lan = "http://192.168.0.152:30300/__preview/ready"
        public = binding["url"] + "/__preview/ready"
        for flags, expected in (([], [lan, public]), (["--lan"], [lan]), (["--public"], [public])):
            with self.subTest(flags=flags), patch.object(sys, "argv", ["preview.py", "url", *flags]), \
                    patch.object(preview, "bindings", return_value={preview.key(): binding}), \
                    patch.object(sys, "stdout", io.StringIO()) as output:
                preview.main()
                self.assertEqual(output.getvalue().splitlines(), expected)
        for other, expected in (({"port": 30300}, ["http://127.0.0.1:30300"]),
                                ({**binding, "role": "stable"}, [binding["url"]]),
                                ({"port": 30300, "lanHost": binding["lanHost"]}, ["http://192.168.0.152:30300"])):
            self.assertEqual(preview.entry_urls(other), expected)

    def test_lan_status_checks_build_identity_even_when_public_probe_is_skipped(self):
        binding = {"instance": "lan-status", "slot": 0, "port": 30300,
                   "url": "https://n9e-dev0.zhiteng.win", "lanHost": "192.168.0.152"}
        directory = preview.instance(binding)
        directory.mkdir(parents=True)
        (directory / "manifest.json").write_text(json.dumps({"buildId": "current", "source": {"fingerprint": "same"}}))
        with patch.object(preview, "source_identity", return_value={"fingerprint": "same"}), \
                patch.object(preview, "alive", return_value=True), \
                patch.object(preview, "probe", side_effect=[{"ok": True}, {"ok": False, "buildId": "old"}]) as probe:
            status = preview.inspect(binding, public=False)
        self.assertTrue(status["local"]["ok"])
        self.assertFalse(status["lan"]["ok"])
        self.assertNotIn("public", status)
        self.assertEqual(probe.call_args_list[1].args, (status["lanUrl"], "current"))

    def test_lan_probe_does_not_use_host_proxy(self):
        with patch.object(preview.urllib.request, "ProxyHandler") as proxy, \
                patch.object(preview.urllib.request, "build_opener") as opener:
            opener.return_value.open.return_value.__enter__.return_value.read.return_value = b'{}'
            preview.fetch("http://192.168.0.152:30300/__preview/info")
            proxy.assert_called_once_with({})

    def test_lan_cli_options_cannot_change_stable_or_apply_to_other_commands(self):
        for args in (("up", "--stable", "--lan-host", "192.168.0.152"),
                     ("status", "--no-lan"), ("refresh", "--lan"), ("stop", "--public")):
            with self.subTest(args=args), patch.object(sys, "argv", ["preview.py", *args]), patch.object(sys, "stderr", io.StringIO()):
                with self.assertRaises(SystemExit) as error:
                    preview.main()
                self.assertEqual(error.exception.code, 2)

    def test_wrong_process_identity_never_signalled(self):
        binding = {"pid": os.getpid(), "startTime": "not-this-process"}
        with patch.object(preview.os, "killpg") as kill:
            preview.stop(binding)
            kill.assert_not_called()

    def test_ready_requires_build_identity_and_health(self):
        for data in [{"buildId": "old", "ready": True}, {"buildId": "new", "ready": False}]:
            with patch.object(preview, "fetch", return_value=data):
                self.assertFalse(preview.probe("http://127.0.0.1:1", "new")["ok"])
        with patch.object(preview, "fetch", return_value={"buildId": "new", "ready": True}):
            self.assertTrue(preview.probe("http://127.0.0.1:1", "new")["ok"])

    def test_legacy_snapshot_is_retained_across_failed_migration_retries(self):
        directory = self.state / "instance"
        (directory / "run").mkdir(parents=True)
        (directory / "run/n9e.db").write_bytes(b"original database")
        (directory / "run/n9e").write_bytes(b"old application")
        (directory / "manifest.json").write_text(json.dumps({"buildId": "old"}))
        preview.archive_legacy(directory)
        (directory / "run/n9e.db").write_bytes(b"later change")
        preview.archive_legacy(directory)
        self.assertEqual((directory / "legacy-sqlite/run/n9e.db").read_bytes(), b"original database")
        self.assertEqual((directory / "legacy-sqlite/run/n9e").read_bytes(), b"old application")
        self.assertEqual(json.loads((directory / "legacy-sqlite/manifest.json").read_text())["buildId"], "old")

    def test_fingerprint_sees_dirty_untracked_and_ignored_but_tracked_files(self):
        root = self.state / "repo"
        root.mkdir()
        def git(*args):
            return subprocess.run(["git", *args], cwd=root, check=True, capture_output=True)
        git("init", "-q")
        git("config", "user.name", "Preview Test")
        git("config", "user.email", "preview@example.invalid")
        (root / ".gitignore").write_text("statik.go\nignored\n")
        (root / "statik.go").write_text("baseline")
        git("add", ".gitignore")
        git("add", "-f", "statik.go")
        git("commit", "-qm", "baseline")
        with patch.object(preview, "ROOT", root):
            before = preview.source_identity()["fingerprint"]
            (root / "ignored").write_text("runtime")
            self.assertEqual(before, preview.source_identity()["fingerprint"])
            (root / "statik.go").write_text("modified")
            changed = preview.source_identity()["fingerprint"]
            self.assertNotEqual(before, changed)
            (root / "new.go").write_text("untracked")
            self.assertNotEqual(changed, preview.source_identity()["fingerprint"])


if __name__ == "__main__":
    unittest.main()
