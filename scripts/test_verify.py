"""Check gate selection against real Git changes without running the gates."""
from contextlib import redirect_stdout
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from git_test_support import git_fixture_environment

spec = importlib.util.spec_from_file_location("verify", Path(__file__).with_name("verify.py"))
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


class VerifyTest(unittest.TestCase):
    def setUp(self):
        # A hook's repository selection must not escape into temporary fixtures.
        self.enterContext(patch.dict(os.environ, git_fixture_environment(), clear=True))
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "-qb", "main")
        self.git("config", "user.name", "Verify Test")
        self.git("config", "user.email", "verify@example.invalid")
        for path, content in [("integrations/Test/i18n/en_US.json", "{}\n"),
                              ("center/integration/removed.go", "package integration\n")]:
            target = self.root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content)
        self.git("add", ".")
        self.git("commit", "-qm", "baseline")

    def git(self, *args):
        return subprocess.run(["git", *args], cwd=self.root, check=True, capture_output=True)

    def assert_deletion_runs_integration_gates(self, path, staged=False, remove=True):
        if remove:
            (self.root / path).unlink()
        if staged:
            self.git("add", "-u")
        real_run = subprocess.run
        commands = []

        def run(command, **kwargs):
            if command[0] == "git":
                return real_run(command, **kwargs)
            commands.append(command)
            return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

        with patch.object(verify, "ROOT", self.root), \
                patch.dict(os.environ, {"N9E_VERIFY_BASE": "HEAD"}), \
                patch("sys.argv", ["verify.py"]), \
                patch.object(verify.subprocess, "run", side_effect=run), \
                redirect_stdout(io.StringIO()):
            self.assertEqual(verify.main(), 0)
        self.assertIn(["go", "run", "./cmd/integrations-alert-action", "check"], commands)
        self.assertIn(["go", "run", "./cmd/integrations-i18n", "check", "-scope", "all"], commands)
        self.assertFalse(any(command[0] == "gofmt" for command in commands))

    def test_unstaged_integration_deletion_runs_both_gates(self):
        self.assert_deletion_runs_integration_gates("integrations/Test/i18n/en_US.json")

    def test_staged_integration_deletion_runs_both_gates(self):
        self.assert_deletion_runs_integration_gates("integrations/Test/i18n/en_US.json", staged=True)

    def test_deleted_go_file_runs_gates_without_gofmt(self):
        self.assert_deletion_runs_integration_gates("center/integration/removed.go")

    def test_rename_out_of_integrations_runs_both_gates(self):
        self.git("mv", "integrations/Test/i18n/en_US.json", "outside.json")
        # The original path must still select the integration gates.
        self.assert_deletion_runs_integration_gates("outside.json", remove=False)

    def test_frontend_change_selects_frontend_gate(self):
        directory = self.root / "frontend"
        directory.mkdir()
        (directory / "page.ts").write_text("export const changed = true;\n")
        real_run = subprocess.run
        commands = []

        def run(command, **kwargs):
            if command[0] == "git":
                return real_run(command, **kwargs)
            commands.append(command)
            return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

        with patch.object(verify, "ROOT", self.root), \
                patch.dict(os.environ, {"N9E_VERIFY_BASE": "HEAD"}), \
                patch("sys.argv", ["verify.py"]), \
                patch.object(verify.subprocess, "run", side_effect=run), \
                redirect_stdout(io.StringIO()):
            self.assertEqual(verify.main(), 0)
        self.assertIn(["python3", "-B", "scripts/frontend.py", "verify"], commands)

    def test_go_test_summary_reports_skips_and_preserves_failure(self):
        events = [
            {"Action": "output", "Package": "example", "Test": "TestExternal",
             "Output": "missing test service configuration\n"},
            {"Action": "skip", "Package": "example", "Test": "TestExternal"},
            {"Action": "pass", "Package": "example", "Test": "TestUnit"},
            {"Action": "output", "Package": "example", "Test": "TestBroken", "Output": "assertion failed\n"},
            {"Action": "fail", "Package": "example", "Test": "TestBroken"},
        ]
        result = subprocess.CompletedProcess([], 1, stdout="\n".join(json.dumps(e) for e in events))
        output = io.StringIO()
        with patch.object(verify.subprocess, "run", return_value=result), redirect_stdout(output):
            self.assertEqual(verify.run_go_tests(["go", "test", "-json", "./..."]), 1)
        self.assertIn("1 passed, 1 failed, 1 skipped", output.getvalue())
        self.assertIn("missing test service configuration", output.getvalue())
        self.assertIn("assertion failed", output.getvalue())


if __name__ == "__main__":
    unittest.main()
