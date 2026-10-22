#!/usr/bin/env python3
"""Explicit integration check: two worktrees, real builds, isolated disposable data.

Run separately from make verify. Leaves the ordinary host registry untouched.
"""
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import sqlite3
import subprocess
import tempfile
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def main():
    with tempfile.TemporaryDirectory(prefix="n9e-smoke-") as temporary:
        base = Path(temporary)
        second = base / "worktree"
        state = base / "state"
        state.mkdir(mode=0o700)
        ports = []
        for _ in range(2):
            with socket.socket() as sock:
                sock.bind(("127.0.0.1", 0))
                ports.append(sock.getsockname()[1])
        if ports[0] == ports[1]:
            raise RuntimeError("port collision; retry smoke check")
        (state / "config.json").write_text(json.dumps({"slots": [
            {"id": 0, "port": ports[0]}, {"id": 1, "port": ports[1]}]}))
        env = {**os.environ, "N9E_PREVIEW_STATE": str(state), "PYTHONDONTWRITEBYTECODE": "1",
               "CONFIGTYPE_HTTP_APIFORSERVICE_ENABLE": "true",
               "CONFIGTYPE_HTTP_APIFORAGENT_ENABLE": "true",
               "CONFIGTYPE_HTTP_PROXYAUTH_ENABLE": "true",
               "CONFIGTYPE_HTTP_PROXYAUTH_HEADERUSERNAMEKEY": "X-Smoke-User"}

        def command(root, *args, success=True):
            result = subprocess.run(["python3", "-B", str(root / "scripts/preview.py"), *args],
                                    env=env, capture_output=True, text=True)
            if (result.returncode == 0) != success:
                logs = []
                for path in (state / "instances").glob("*/build.log"):
                    logs.append(f"{path}:\n" + path.read_text()[-6000:])
                raise RuntimeError(f"{args}: {result.stdout}\n{result.stderr}\n" + "\n".join(logs))
            return json.loads(result.stdout) if result.stdout.lstrip().startswith("{") else result.stdout

        def read(port, path):
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            with opener.open(f"http://127.0.0.1:{port}{path}", timeout=10) as response:
                return response.read().decode()

        def api(port, path, body=None, token=None, method=None):
            headers = {"Content-Type": "application/json"}
            if token:
                headers["Authorization"] = "Bearer " + token
            request = urllib.request.Request(f"http://127.0.0.1:{port}/api/n9e{path}",
                data=json.dumps(body).encode() if body is not None else None,
                headers=headers, method=method)
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            with opener.open(request, timeout=10) as response:
                return json.load(response)

        def check_access_boundaries(port):
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            for path, expected in (("/v1/n9e/users", 403), ("/v1/n9e/user-token", 403),
                                   ("/prometheus/api/v1/query?query=vector(1)", 403),
                                   ("/api/n9e/self/profile", 401)):
                request = urllib.request.Request(f"http://127.0.0.1:{port}{path}",
                                                 headers={"X-Smoke-User": "root"})
                try:
                    with opener.open(request, timeout=10) as response:
                        status = response.status
                except urllib.error.HTTPError as error:
                    status = error.code
                    error.close()
                assert status == expected, f"{path}: expected {expected}, got {status}"

        subprocess.run(["git", "worktree", "add", "--detach", str(second), "HEAD"], cwd=ROOT, check=True,
                       stdout=subprocess.DEVNULL)
        try:
            # Overlay this task's working changes without committing or touching the base checkout.
            changed = subprocess.check_output(["git", "diff", "--name-only", "-z", "HEAD"], cwd=ROOT)
            changed += subprocess.check_output(["git", "ls-files", "--others", "--exclude-standard", "-z"], cwd=ROOT)
            for name in set(changed.split(b"\0")) - {b""}:
                relative = os.fsdecode(name)
                source, dest = ROOT / relative, second / relative
                if source.is_file():
                    dest.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(source, dest)
                elif not source.exists() and dest.exists():
                    dest.unlink()
            first = command(ROOT, "up", "--slot", "0")
            assert first["local"]["ok"] and not first["sourceChanged"]
            assert first["frontend"]["kind"] == "in-tree" and not first["frontendChanged"]
            check_access_boundaries(ports[0])
            command(second, "up", "--slot", "0", success=False)
            frontend = base / "frontend"
            frontend.mkdir()
            (frontend / "index.html").write_text("<html>smoke frontend one</html>")
            other = command(second, "up", "--slot", "1", "--frontend-dir", str(frontend), "--frontend-ref", "smoke-fixture")
            assert other["local"]["ok"] and first["buildId"] != other["buildId"]
            check_access_boundaries(ports[1])
            first_credentials = json.loads(Path(first["credentialsFile"]).read_text())
            other_credentials = json.loads(Path(other["credentialsFile"]).read_text())
            assert first_credentials != other_credentials
            with sqlite3.connect(Path(first["instanceDir"]) / "run/n9e.db") as db:
                db.execute("CREATE TABLE preview_isolation_probe (value TEXT)")
                db.execute("INSERT INTO preview_isolation_probe VALUES ('only-first')")
            with sqlite3.connect(Path(other["instanceDir"]) / "run/n9e.db") as db:
                assert not db.execute("SELECT name FROM sqlite_master WHERE name='preview_isolation_probe'").fetchall()
            assert "smoke frontend one" in read(ports[1], "/login")
            marker = second / "smoke-untracked.txt"
            marker.write_text("source changed")
            assert command(second, "status")["sourceChanged"]
            command(second, "up", success=False)
            marker.unlink()
            (frontend / "index.html").write_text("<html>smoke frontend two</html>")
            assert command(second, "status")["frontendChanged"]
            assert "smoke frontend one" in read(ports[1], "/login")  # staged, never live-linked
            initial_login = api(ports[1], "/auth/login", other_credentials)
            token = initial_login["dat"]["access_token"]
            original_profile = api(ports[1], "/self/profile", token=token)["dat"]
            new_credentials = {"username": "root", "password": secrets.token_urlsafe(24)}
            changed_password = api(ports[1], "/self/password",
                {"oldpass": other_credentials["password"], "newpass": new_credentials["password"]},
                token=token, method="PUT")
            assert not changed_password.get("err"), "password change failed"
            refreshed = command(second, "refresh")
            assert refreshed["buildId"] != other["buildId"]
            assert not refreshed["frontendChanged"] and not refreshed["sourceChanged"]
            check_access_boundaries(ports[1])
            assert "smoke frontend two" in read(ports[1], "/login")
            assert json.loads(Path(refreshed["credentialsFile"]).read_text()) == other_credentials
            assert not (Path(refreshed["instanceDir"]) / "login-check.pending").exists()
            old_login = api(ports[1], "/auth/login", other_credentials)
            assert not (old_login.get("dat") or {}).get("access_token"), "initial password still accepted"
            new_login = api(ports[1], "/auth/login", new_credentials)
            new_profile = api(ports[1], "/self/profile", token=new_login["dat"]["access_token"])["dat"]
            assert (new_profile["id"], new_profile["roles"]) == (original_profile["id"], original_profile["roles"])
            command(second, "stop")
            stopped = command(second, "status")
            assert not stopped["running"] and not stopped["local"]["ok"]
            assert command(ROOT, "status")["local"]["ok"]
            command(second, "destroy")
            assert not Path(other["instanceDir"]).exists()
            command(second, "destroy", "--if-exists")
            # A released slot can be reused without reusing the old instance/data.
            replacement = command(second, "up", "--slot", "1", "--frontend-dir", str(frontend),
                                  "--frontend-ref", "smoke-fixture")
            assert replacement["instanceDir"] != other["instanceDir"]
            print(json.dumps({"result": "passed", "checks": ["two worktrees", "slot ownership",
                "host auth environment isolation", "internal APIs blocked", "anonymous/spoofed profile rejected",
                "private credentials and DB", "dirty source detection", "in-tree frontend identity", "frontend staging and drift",
                "refresh after password change preserves account and roles", "initial password rejected",
                "stop isolation", "destroy/idempotence", "slot reuse"],
                "firstBuildId": first["buildId"], "secondBuildId": refreshed["buildId"]}, indent=2))
        finally:
            for root in (second, ROOT):
                if (root / "scripts/preview.py").exists():
                    command(root, "destroy", "--if-exists")
            subprocess.run(["git", "worktree", "remove", "--force", str(second)], cwd=ROOT, check=True)


if __name__ == "__main__":
    main()
