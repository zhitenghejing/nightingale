#!/usr/bin/env python3
"""Linux worktree previews. Python 3.11+ stdlib; no global package installs."""
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import sqlite3
import subprocess
import sys
import time
import urllib.request
from urllib.parse import urlsplit

MANAGER_ROOT = Path(__file__).resolve().parents[1]
ROOT = MANAGER_ROOT
STATE = Path(os.environ.get("N9E_PREVIEW_STATE", Path.home() / ".local/state/n9e-preview")).resolve()
DEFAULT_CONFIG = {
    "stable": {"branch": "main", "port": 30299, "url": "https://n9e.cubbber.dev"},
    "slots": [{"id": n, "port": 30300 + n, "url": f"https://n9e-dev{n}.cubbber.dev"} for n in range(10)],
}


def run(args, **kwargs):
    return subprocess.run(args, cwd=kwargs.pop("cwd", ROOT), check=True, **kwargs)


def git(*args):
    return run(["git", *args], stdout=subprocess.PIPE).stdout.decode().strip()


def digest(path):
    h = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def tree_digest(directory):
    h = hashlib.sha256()
    for path in sorted(Path(directory).rglob("*")):
        if path.is_file():
            h.update(str(path.relative_to(directory)).encode() + b"\0")
            h.update(digest(path).encode())
    return h.hexdigest()


def source_identity():
    """Hash bytes (including dirty/untracked source), not just HEAD or mtimes."""
    files = run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
                stdout=subprocess.PIPE).stdout.split(b"\0")
    h = hashlib.sha256()
    for name in sorted(set(files) - {b""}):
        path = ROOT / os.fsdecode(name)
        h.update(name + b"\0")
        if path.is_symlink():
            h.update(os.readlink(path).encode())
        elif path.is_file():
            h.update(digest(path).encode())
            h.update(str(path.stat().st_mode & 0o111).encode())
        else:
            h.update(b"<deleted>")
    return {"commit": git("rev-parse", "HEAD"), "branch": git("branch", "--show-current"),
            "dirty": bool(git("status", "--porcelain")), "fingerprint": h.hexdigest()}


def write_json(path, obj):
    path = Path(path)
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(obj, indent=2, ensure_ascii=False) + "\n")
    temporary.chmod(0o600)
    temporary.replace(path)


def config():
    path = STATE / "config.json"
    cfg = json.loads(path.read_text()) if path.exists() else DEFAULT_CONFIG
    slots = cfg["slots"]
    ids, ports, urls = set(), set(), set()
    for slot in slots:
        i, port = slot["id"], slot["port"]
        if type(i) is not int or i < 0 or type(port) is not int or not 1024 <= port <= 65535:
            raise ValueError("slot IDs must be nonnegative integers; ports must be 1024..65535")
        url = slot.get("url", "")
        if i in ids or port in ports or (url and url in urls):
            raise ValueError("duplicate slot ID, port or public URL")
        if url and not url.startswith("https://"):
            raise ValueError("public preview URL must use HTTPS")
        stable_url = cfg.get("stable", DEFAULT_CONFIG["stable"])["url"]
        if url and urlsplit(url).hostname == urlsplit(stable_url).hostname:
            raise ValueError("stable hostname is reserved; worktrees must use development hostnames")
        ids.add(i)
        ports.add(port)
        if url:
            urls.add(url)
    if "stable" in cfg:
        stable = cfg["stable"]
        port = stable["port"]
        if type(port) is not int or not 1024 <= port <= 65535 or port in ports:
            raise ValueError("stable port must be valid and separate from worktree slots")
        if not stable["url"].startswith("https://") or not stable.get("branch"):
            raise ValueError("stable requires an HTTPS URL and an expected branch")
    return cfg


def stable_root():
    settings = config().get("stable")
    if not settings:
        raise RuntimeError("stable instance is not enabled in host config")
    if settings.get("worktree"):
        return Path(settings["worktree"]).expanduser().resolve()
    return Path(git("rev-parse", "--path-format=absolute", "--git-common-dir")).parent


def validate_stable_source():
    expected = config()["stable"]["branch"]
    if git("branch", "--show-current") != expected:
        raise RuntimeError(f"stable requires branch {expected}; refusing to publish another branch")
    if git("status", "--porcelain"):
        raise RuntimeError("stable source must be clean; commit/merge changes before publishing")


def bindings():
    path = STATE / "bindings.json"
    return json.loads(path.read_text()) if path.exists() else {}


def key(stable=False):
    return "stable" if stable else hashlib.sha256(str(ROOT).encode()).hexdigest()[:16]


def instance(binding):
    return STATE / "instances" / binding["instance"]


def runtime_temp(binding):
    # Keep Unix socket paths below Linux's 108-byte limit, even for long worktree/state paths.
    return Path("/tmp") / ("n9ep-" + binding["instance"])


def proc_identity(pid):
    try:
        # comm may contain spaces or parentheses; starttime is field 22.
        fields = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
        return None if fields[0] == "Z" else fields[19]
    except (FileNotFoundError, ProcessLookupError):
        return None


def alive(binding):
    return bool(binding.get("pid") and binding.get("startTime") and
                proc_identity(binding["pid"]) == binding["startTime"])


def fetch(url):
    # Local readiness must not be sent through a host HTTP proxy.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}) if url.startswith("http://127.")
                                        else urllib.request.ProxyHandler())
    request = urllib.request.Request(url, headers={"User-Agent": "n9e-preview/1.0", "Accept": "application/json"})
    with opener.open(request, timeout=12) as response:
        return json.load(response)


def probe(url, build_id):
    try:
        data = fetch(url.rstrip("/") + "/__preview/info")
        return {"ok": data.get("buildId") == build_id and data.get("ready") is True,
                "buildId": data.get("buildId")}
    except Exception as exc:
        return {"ok": False, "error": str(exc)}


def inspect(binding, public=True):
    directory = instance(binding)
    manifest_path = directory / "manifest.json"
    manifest = json.loads(manifest_path.read_text()) if manifest_path.exists() else {}
    result = {"worktree": str(ROOT), "role": binding.get("role", "development"),
              "slot": binding["slot"], "port": binding["port"],
              "url": binding.get("url") or f"http://127.0.0.1:{binding['port']}",
              "running": alive(binding), "buildId": manifest.get("buildId"),
              "source": manifest.get("source"), "frontend": manifest.get("frontend"),
              "profile": manifest.get("profile"), "appVersion": manifest.get("appVersion"),
              "instanceDir": str(directory),
              "log": str(directory / "preview.log"),
              "credentialsFile": str(directory / "credentials.json")}
    result["sourceChanged"] = manifest.get("source", {}).get("fingerprint") != source_identity()["fingerprint"]
    if binding.get("frontendDir"):
        try:
            result["frontendChanged"] = tree_digest(binding["frontendDir"]) != manifest.get("frontend", {}).get("sha256")
        except OSError:
            result["frontendChanged"] = True
    elif manifest.get("frontend", {}).get("kind") == "in-tree":
        try:
            fingerprint = run([sys.executable, "-B", "scripts/frontend.py", "source"], stdout=subprocess.PIPE).stdout.decode().strip()
            result["frontendChanged"] = (fingerprint != manifest["frontend"]["sourceSha256"]
                or tree_digest(ROOT / "frontend/pub") != manifest["frontend"]["sha256"])
        except (OSError, subprocess.CalledProcessError, KeyError):
            result["frontendChanged"] = True
    result["local"] = probe(f"http://127.0.0.1:{binding['port']}", result["buildId"]) if result["running"] else {"ok": False}
    if public and binding.get("url"):
        result["public"] = probe(binding["url"], result["buildId"])
    return result


def reserve(all_bindings, slot_id, stable=False):
    used = {b["slot"] for b in all_bindings.values()}
    cfg = config()
    resources = [{**cfg["stable"], "id": "stable"}] if stable else cfg["slots"]
    for slot in resources:
        if slot_id is not None and slot["id"] != slot_id:
            continue
        if slot["id"] in used:
            continue
        with socket.socket() as sock:
            # Match Go's listener semantics: TIME_WAIT from a destroyed preview
            # must not make its released slot appear occupied.
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            try:
                sock.bind(("127.0.0.1", slot["port"]))
            except OSError:
                continue
        return {"slot": slot["id"], "port": slot["port"], "url": slot.get("url"),
                "instance": secrets.token_hex(8), "worktree": str(ROOT),
                "role": "stable" if stable else "development"}
    raise RuntimeError("no free slot (stopped/failed previews retain their binding; destroy to release)")


def stop(binding):
    if alive(binding):
        # The gateway is a session/group leader; its n9e child shares this group.
        os.killpg(binding["pid"], signal.SIGTERM)
        until = time.monotonic() + 40
        while alive(binding) and time.monotonic() < until:
            time.sleep(0.2)
        if alive(binding):
            os.killpg(binding["pid"], signal.SIGKILL)
            for _ in range(50):
                if not alive(binding):
                    break
                time.sleep(0.1)
        if alive(binding):
            raise RuntimeError("preview did not stop; keeping binding for recovery")
    binding.pop("pid", None)
    binding.pop("startTime", None)


def seed(directory):
    db_path = directory / "run/n9e.db"
    if db_path.exists():
        # Existing accounts may have changed their password. Never restore the
        # initial credentials or require them for a restart (including legacy instances).
        return
    # Replace the shipped default before the first listener can accept requests.
    credentials = {"username": "root", "password": secrets.token_urlsafe(18)}
    write_json(directory / "credentials.json", credentials)
    staging = db_path.with_suffix(".seed")
    try:
        with sqlite3.connect(staging) as db:
            db.executescript((ROOT / "docker/sqlite.sql").read_text())
            db.execute("UPDATE users SET password = ? WHERE username = 'root'", (credentials["password"],))
        # Publish the pending check before the DB so interrupted initial startup
        # cannot silently skip account verification on the next refresh.
        write_json(directory / "login-check.pending", {})
        staging.replace(db_path)
    finally:
        staging.unlink(missing_ok=True)


def app_config(binding, backend_port):
    # A dedicated minimal profile: never inherit production DSNs, writers or notifications.
    directory = instance(binding)
    return f'''[Global]
RunMode = "release"
[Log]
Dir = "logs"
Output = "stdout"
Level = "INFO"
[HTTP]
Host = "127.0.0.1"
Port = {backend_port}
PProf = false
ExposeMetrics = false
ShutdownTimeout = 20
[HTTP.APIForService]
Enable = false
[HTTP.APIForAgent]
Enable = false
[HTTP.ProxyAuth]
Enable = false
[HTTP.TokenAuth]
Enable = true
[HTTP.JWTAuth]
AccessExpired = 1500
RefreshExpired = 10080
[DB]
DBType = "sqlite"
DSN = "n9e.db"
MaxOpenConns = 1
MaxIdleConns = 1
[Redis]
RedisType = "miniredis"
[Alert.Heartbeat]
IP = "127.0.0.1"
EngineName = "preview-{binding['instance']}"
Interval = 1000
[Center]
UseFileAssets = {str(bool(binding.get('frontendDir') or binding.get('inTreeFrontend'))).lower()}
MetricsYamlFile = {json.dumps(str(ROOT / 'etc/metrics.yaml'))}
BuiltinIntegrationsDir = {json.dumps(str(ROOT / 'integrations'))}
I18NHeaderKey = "X-Language"
[Center.AIAgent]
SkillsPath = "skill"
[Center.Sandbox]
Disabled = {str(binding.get('profile', 'basic') != 'full').lower()}
RequireIsolation = true
DataDir = {json.dumps(str(runtime_temp(binding) / 'sandbox'))}
[EmbeddedTSDB]
Enable = true
Dir = "data/tsdb"
RetentionDuration = "12h"
MaxBytes = "256MiB"
[Ibex]
Enable = false
'''


def build(binding, profile):
    directory = instance(binding)
    for name in ("bin", "run", "run/etc"):
        (directory / name).mkdir(parents=True, exist_ok=True)
    binding["profile"] = profile
    tags = []
    if profile == "full":
        # The frontend corpus comes from this checkout; the collector stays pinned.
        if not os.environ.get("CATEGRAF_REF"):
            raise RuntimeError("full profile requires pinned CATEGRAF_REF")
        run(["bash", "scripts/build-sandbox-assets.sh"])
        run(["bash", "scripts/build-qa-code-assets.sh"])
        tags = ["-tags=sandbox_embed,qa_code_embed"]
    binding["inTreeFrontend"] = (ROOT / "frontend/package.json").is_file() and not binding.get("frontendDir")
    frontend_build = None
    if binding["inTreeFrontend"]:
        with (directory / "frontend-build.log").open("w") as log:
            run([sys.executable, "-B", "scripts/frontend.py", "build"], stdout=log, stderr=log)
        frontend_build = json.loads((ROOT / "frontend/.build-manifest.json").read_text())
    source = source_identity()
    build_id = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()) + "-" + secrets.token_hex(4)
    app_version = git("describe", "--tags", "--always") if binding.get("role") == "stable" else "preview-" + build_id
    frontend = {"kind": "committed-statik", "sha256": digest(ROOT / "front/statik/statik.go")}
    if binding.get("frontendDir") or binding["inTreeFrontend"]:
        origin = Path(binding["frontendDir"]) if binding.get("frontendDir") else ROOT / "frontend/pub"
        if not (origin / "index.html").is_file():
            raise RuntimeError("frontend directory must contain the built index.html")
        frontend = frontend_build or {"kind": "file-assets", "ref": binding["frontendRef"], "sha256": tree_digest(origin)}
        dest = directory / "run/pub"
        if dest.exists():
            shutil.rmtree(dest)
        shutil.copytree(origin, dest)
        if tree_digest(dest) != frontend["sha256"]:
            raise RuntimeError("frontend changed while staging assets; refresh after build completes")
    with (directory / "build.log").open("w") as log:
        # Manifest owns VCS identity. Go's automatic stamping can mistake a linked
        # worktree's parent .git directory for the actual repository.
        run(["go", "build", "-buildvcs=false", *tags, "-ldflags", f"-X github.com/ccfos/nightingale/v6/pkg/version.Version={app_version}",
             "-o", str(directory / "run/n9e"), "./cmd/center"], stdout=log, stderr=log)
        run(["go", "build", "-buildvcs=false", "-o", str(directory / "bin/gateway"), "./cmd/preview-gateway"],
            cwd=MANAGER_ROOT, stdout=log, stderr=log)
    if source_identity()["fingerprint"] != source["fingerprint"]:
        raise RuntimeError("source changed during build; stop editing and refresh")
    seed(directory)
    temp = runtime_temp(binding)
    temp.mkdir(mode=0o700, exist_ok=True)
    if temp.is_symlink() or temp.stat().st_uid != os.getuid() or temp.stat().st_mode & 0o077:
        raise RuntimeError("runtime temp directory must be private and owned by this user")
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        backend_port = sock.getsockname()[1]
    (directory / "run/etc/config.toml").write_text(app_config(binding, backend_port))
    # Config assets are private per instance, with no generated files in source.
    for name in ("i18n", "metrics.yaml", "ops.yaml"):
        src = ROOT / "etc" / name
        dest = directory / "run/etc" / name
        if src.is_dir():
            shutil.copytree(src, dest, dirs_exist_ok=True)
        elif src.exists():
            shutil.copy2(src, dest)
    manifest = {"buildId": build_id, "source": source, "frontend": frontend, "profile": profile,
                "role": binding.get("role", "development"),
                "appVersion": app_version,
                "port": binding["port"], "backendPort": backend_port, "instance": binding["instance"],
                "binary": str(directory / "run/n9e"), "runDir": str(directory / "run"),
                "tempDir": str(temp),
                "credentialsFile": str(directory / "credentials.json")}
    if profile == "full":
        manifest["qaCorpus"] = json.loads((ROOT / "aiagent/skill/embedded/codeassets/manifest.json").read_text())
    write_json(directory / "manifest.json", manifest)
    return manifest


@contextlib.contextmanager
def lock():
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (STATE / "manager.lock").open("w") as handle:
        fcntl.flock(handle, fcntl.LOCK_EX)
        yield


def main():
    global ROOT
    if sys.platform != "linux":
        raise RuntimeError("preview lifecycle management requires Linux")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["up", "refresh", "status", "url", "stop", "destroy", "list"])
    parser.add_argument("--slot", type=int)
    parser.add_argument("--stable", action="store_true", help="manage the separate clean main-checkout instance")
    parser.add_argument("--if-exists", action="store_true")
    parser.add_argument("--local-only", action="store_true", help="skip public probe (never changes binding)")
    parser.add_argument("--profile", choices=["basic", "full"], default=None)
    parser.add_argument("--frontend-dir", type=Path, help="built frontend directory containing index.html")
    parser.add_argument("--frontend-ref", help="frontend commit/tag corresponding to --frontend-dir")
    args = parser.parse_args()
    if args.stable:
        if args.slot is not None or args.frontend_dir:
            parser.error("stable has its own binding and committed frontend; --slot/--frontend-dir are not allowed")
        ROOT = stable_root()
    binding_key = key(args.stable)
    if bool(args.frontend_dir) != bool(args.frontend_ref):
        parser.error("--frontend-dir and --frontend-ref must be supplied together")
    if args.frontend_dir and args.command not in ("up", "refresh"):
        parser.error("frontend options apply only to up/refresh")
    os.umask(0o077)
    if args.command == "list":
        print(json.dumps(bindings(), indent=2))
        return
    if args.command in ("status", "url"):
        binding = bindings().get(binding_key)
        if not binding:
            if args.if_exists:
                return
            raise RuntimeError("this worktree has no preview binding")
        if args.command == "url":
            print((binding.get("url") or f"http://127.0.0.1:{binding['port']}") + "/__preview/ready")
            return
        result = inspect(binding, public=not args.local_only)
        print(json.dumps(result, indent=2, ensure_ascii=False))
        return
    with lock():
        if args.stable and args.command in ("up", "refresh"):
            validate_stable_source()
        all_bindings = bindings()
        binding = all_bindings.get(binding_key)
        if not binding and args.command in ("stop", "destroy"):
            if args.if_exists:
                return
            raise RuntimeError("this worktree has no preview binding")
        if binding and args.slot is not None and args.slot != binding["slot"]:
            raise RuntimeError("worktree already bound to another slot; destroy before changing slots")
        if args.command in ("stop", "destroy"):
            stop(binding)
            if args.command == "destroy":
                directory = instance(binding)
                # Validate generated identifier before recursive deletion.
                if directory.parent != STATE / "instances" or not all(c in "0123456789abcdef" for c in binding["instance"]):
                    raise RuntimeError("invalid instance directory")
                if directory.exists():
                    shutil.rmtree(directory)
                temp = runtime_temp(binding)
                if temp.exists():
                    shutil.rmtree(temp)
                del all_bindings[binding_key]
            write_json(STATE / "bindings.json", all_bindings)
            print(args.command + " complete")
            return
        if binding and alive(binding) and args.command == "up":
            if args.frontend_dir:
                raise RuntimeError("use refresh to change frontend assets")
            if args.profile and args.profile != binding.get("profile"):
                raise RuntimeError("use refresh to change preview profile")
        else:
            if binding:
                stop(binding)
            else:
                binding = reserve(all_bindings, args.slot, stable=args.stable)
                all_bindings[binding_key] = binding
            # Persist binding before build: failures remain inspectable/cleanable.
            write_json(STATE / "bindings.json", all_bindings)
            directory = instance(binding)
            directory.mkdir(parents=True, exist_ok=True)
            if args.frontend_dir:
                binding.update(frontendDir=str(args.frontend_dir.resolve()), frontendRef=args.frontend_ref)
                write_json(STATE / "bindings.json", all_bindings)
            try:
                manifest = build(binding, args.profile or binding.get("profile", "basic"))
                if args.stable:
                    validate_stable_source()
            except Exception:
                print(f"build failed; inspect {directory / 'build.log'}", file=sys.stderr)
                raise
            with (directory / "preview.log").open("a") as log:
                process = subprocess.Popen([str(directory / "bin/gateway"), str(directory / "manifest.json")],
                                           cwd=directory, stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                           start_new_session=True)
            binding.update(pid=process.pid, startTime=proc_identity(process.pid))
            write_json(STATE / "bindings.json", all_bindings)
            deadline = time.monotonic() + 180
            while time.monotonic() < deadline:
                if not alive(binding):
                    raise RuntimeError(f"gateway exited; inspect {directory / 'preview.log'}")
                if probe(f"http://127.0.0.1:{binding['port']}", manifest["buildId"])["ok"]:
                    break
                time.sleep(1)
            else:
                stop(binding)
                write_json(STATE / "bindings.json", all_bindings)
                raise RuntimeError(f"readiness timed out; inspect {directory / 'preview.log'}")
        result = inspect(binding, public=not args.local_only)
        print(json.dumps(result, indent=2, ensure_ascii=False))
        if (not result["local"]["ok"] or not result.get("public", {"ok": True})["ok"]
                or result["sourceChanged"] or result.get("frontendChanged")):
            raise RuntimeError("preview not ready for handoff; inspect status (use --local-only before tunnel setup)")


if __name__ == "__main__":
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        print(f"preview: {error}", file=sys.stderr)
        sys.exit(1)
