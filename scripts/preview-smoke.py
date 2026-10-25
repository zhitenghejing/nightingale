#!/usr/bin/env python3
"""Explicit integration check: two worktrees, real builds, isolated disposable data.

Run separately from make verify. Leaves the ordinary host registry untouched.
"""
import json
import argparse
import contextlib
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import urllib.error
import urllib.request

import preview_services
from preview import validate_lan_host

ROOT = Path(__file__).resolve().parents[1]


@contextlib.contextmanager
def smoke_directory():
    directory = tempfile.mkdtemp(prefix="n9e-smoke-")
    try:
        yield directory
    except BaseException:
        print(f"Smoke evidence retained at {directory}; inspect and finish cleanup before deleting it.")
        raise
    else:
        shutil.rmtree(directory)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lan-host", type=validate_lan_host, help="also verify LAN lifecycle on this host's private IPv4")
    args = parser.parse_args()
    with smoke_directory() as temporary:
        base = Path(temporary)
        second = base / "worktree"
        state = base / "state"
        state.mkdir(mode=0o700)
        ports = []
        for _ in range(4):
            with socket.socket() as sock:
                sock.bind(("127.0.0.1", 0))
                ports.append(sock.getsockname()[1])
        if len(set(ports)) != len(ports):
            raise RuntimeError("port collision; retry smoke check")
        config = {"slots": [
            {"id": 0, "port": ports[0]}, {"id": 1, "port": ports[1]}],
            "services": {"postgresPort": ports[2], "prometheusPort": ports[3]}}
        if args.lan_host:
            config["lan"] = {"host": args.lan_host}
        (state / "config.json").write_text(json.dumps(config))
        shared_cfg = preview_services.settings(state, json.loads((state / "config.json").read_text()))
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

        def read(port, path, host="127.0.0.1"):
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            with opener.open(f"http://{host}:{port}{path}", timeout=10) as response:
                return response.read().decode()

        def api(port, path, body=None, token=None, method=None, host="127.0.0.1"):
            headers = {"Content-Type": "application/json"}
            if token:
                headers["Authorization"] = "Bearer " + token
            request = urllib.request.Request(f"http://{host}:{port}/api/n9e{path}",
                data=json.dumps(body).encode() if body is not None else None,
                headers=headers, method=method)
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            with opener.open(request, timeout=10) as response:
                return json.load(response)

        def check_access_boundaries(port, host="127.0.0.1"):
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            for path, expected in (("/v1/n9e/users", 403), ("/v1/n9e/user-token", 403),
                                   ("/prometheus/api/v1/query?query=vector(1)", 403),
                                   ("/api/n9e/self/profile", 401)):
                request = urllib.request.Request(f"http://{host}:{port}{path}",
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
            if args.lan_host:
                assert first["lan"]["ok"]
                assert json.loads(read(ports[0], "/__preview/info", args.lan_host))["buildId"] == first["buildId"]
                check_access_boundaries(ports[0], args.lan_host)
                assert command(ROOT, "url").splitlines() == [first["lanUrl"] + "/__preview/ready"]
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
            first_db, other_db = first["storage"]["databaseName"], other["storage"]["databaseName"]
            assert first_db != other_db
            preview_services.sql(shared_cfg, "CREATE TABLE preview_isolation_probe (value TEXT); "
                                 "INSERT INTO preview_isolation_probe VALUES ('only-first');", database=first_db)
            assert preview_services.sql(shared_cfg, "SELECT count(*) FROM information_schema.tables "
                                        "WHERE table_name='preview_isolation_probe';", database=other_db) == "0"
            assert preview_services.probe_prometheus(shared_cfg)
            for path, expected, explanation in (
                    ("/api/v1/write", 404, "remote write receiver needs to be enabled"),
                    ("/api/v1/admin/tsdb/delete_series", 500, "admin APIs disabled"),
                    ("/-/reload", 403, "Lifecycle API is not enabled")):
                request = urllib.request.Request(f"http://127.0.0.1:{ports[3]}{path}", data=b"", method="POST")
                try:
                    urllib.request.urlopen(request, timeout=5).close()
                    raise AssertionError("shared Prometheus mutation accepted")
                except urllib.error.HTTPError as error:
                    body = error.read().decode()
                    assert error.code == expected and explanation in body, (path, error.code, body)
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
            if args.lan_host:
                check_access_boundaries(ports[1], args.lan_host)
                lan_login = api(ports[1], "/auth/login", other_credentials, host=args.lan_host)
                assert api(ports[1], "/self/profile", token=lan_login["dat"]["access_token"],
                           host=args.lan_host)["dat"]["username"] == "root"
            first_login = api(ports[0], "/auth/login", first_credentials)
            try:
                api(ports[1], "/self/profile", token=first_login["dat"]["access_token"])
                raise AssertionError("token from another environment accepted")
            except urllib.error.HTTPError as error:
                assert error.code == 401, error.code
            listing = api(ports[1], "/datasource/list", {"plugin_type": "prometheus"}, token=token)
            assert not listing["error"], listing["error"]
            datasources = listing["data"]
            shared = next(ds for ds in datasources if ds["identifier"] == "preview-shared-prometheus")
            metrics = api(ports[1], f"/proxy/{shared['id']}/api/v1/query?query=up", token=token)
            assert metrics["status"] == "success" and metrics["data"]["result"]
            original_profile = api(ports[1], "/self/profile", token=token)["dat"]
            new_credentials = {"username": "root", "password": secrets.token_urlsafe(24)}
            changed_password = api(ports[1], "/self/password",
                {"oldpass": other_credentials["password"], "newpass": new_credentials["password"]},
                token=token, method="PUT")
            assert not changed_password.get("err"), "password change failed"
            refreshed = command(second, "refresh")
            assert refreshed["buildId"] != other["buildId"]
            assert not refreshed["frontendChanged"] and not refreshed["sourceChanged"]
            if args.lan_host:
                assert refreshed["lan"]["ok"] and refreshed["lanUrl"] == other["lanUrl"]
                assert json.loads(read(ports[1], "/__preview/info", args.lan_host))["buildId"] == refreshed["buildId"]
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
            if args.lan_host:
                assert not stopped["lan"]["ok"] and stopped["lanUrl"] == other["lanUrl"]
                with socket.socket() as sock:
                    assert sock.connect_ex((args.lan_host, ports[1])) != 0, "stop leaked LAN listener"
            assert command(ROOT, "status")["local"]["ok"]
            command(second, "destroy")
            assert not Path(other["instanceDir"]).exists()
            assert preview_services.sql(shared_cfg, f"SELECT count(*) FROM pg_database WHERE datname='{other_db}';") == "0"
            assert preview_services.sql(shared_cfg, f"SELECT count(*) FROM pg_roles WHERE rolname='{other_db}';") == "0"
            assert preview_services.sql(shared_cfg, "SELECT value FROM preview_isolation_probe;", database=first_db) == "only-first"
            assert preview_services.probe_prometheus(shared_cfg)
            command(second, "destroy", "--if-exists")
            # A released slot can be reused without reusing the old instance/data.
            replacement = command(second, "up", "--slot", "1", "--frontend-dir", str(frontend),
                                  "--frontend-ref", "smoke-fixture")
            assert replacement["instanceDir"] != other["instanceDir"]
            if args.lan_host:
                assert replacement["lan"]["ok"]
                assert json.loads(read(ports[1], "/__preview/info", args.lan_host))["buildId"] == replacement["buildId"]
                disabled = command(second, "refresh", "--no-lan")
                assert disabled["local"]["ok"] and "lanUrl" not in disabled
                with socket.socket() as sock:
                    assert sock.connect_ex((args.lan_host, ports[1])) != 0, "disabling LAN leaked listener"
                command(second, "up", "--lan-host", args.lan_host, success=False)
                assert "lanUrl" not in command(second, "status"), "up changed a running preview"
            print(json.dumps({"result": "passed", "checks": ["two worktrees", "slot ownership",
                "host auth environment isolation", "internal APIs blocked", "anonymous/spoofed profile rejected",
                "private credentials and PostgreSQL databases", "cross-environment token rejection",
                "authenticated shared Prometheus query", "shared read-only Prometheus", "dirty source detection", "in-tree frontend identity", "frontend staging and drift",
                "refresh after password change preserves account and roles", "initial password rejected",
                "stop isolation", "destroy/idempotence", "slot reuse"],
                "lanLifecycleVerified": bool(args.lan_host),
                "firstBuildId": first["buildId"], "secondBuildId": refreshed["buildId"]}, indent=2))
        finally:
            for root in (second, ROOT):
                if (root / "scripts/preview.py").exists():
                    command(root, "destroy", "--if-exists")
            subprocess.run(["git", "worktree", "remove", "--force", str(second)], cwd=ROOT, check=True)
            # Only this temporary registry's explicitly owned services/volumes are disposable.
            for name in (shared_cfg["postgres"], shared_cfg["prometheus"]):
                info = preview_services.container(name)
                if info and (info["Config"].get("Labels") or {}).get("n9e.preview.owner") == shared_cfg["owner"]:
                    preview_services.docker("rm", "-f", name)
                    preview_services.docker("volume", "rm", name + "-data")


if __name__ == "__main__":
    main()
