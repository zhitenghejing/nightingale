#!/usr/bin/env python3
"""Linux worktree previews. Python 3.11+ stdlib; no global package installs."""
import argparse
import contextlib
import fcntl
import hashlib
import ipaddress
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

import preview_services

MANAGER_ROOT = Path(__file__).resolve().parents[1]
ROOT = MANAGER_ROOT
STATE = Path(os.environ.get("N9E_PREVIEW_STATE", Path.home() / ".local/state/n9e-preview")).resolve()
DEFAULT_CONFIG = {
    "stable": {"branch": "main", "port": 30299, "url": "https://n9e.zhiteng.win"},
    "slots": [{"id": n, "port": 30300 + n, "url": f"https://n9e-dev{n}.zhiteng.win"} for n in range(10)],
    "services": preview_services.DEFAULT_PORTS,
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
    if "lan" in cfg:
        validate_lan_host(cfg["lan"]["host"])
    services = preview_services.settings(STATE, cfg)
    slots = cfg["slots"]
    ids, ports, urls = set(), set(), set()
    for slot in slots:
        i, port = slot["id"], slot["port"]
        if type(i) is not int or i < 0 or type(port) is not int or not 1024 <= port <= 65535:
            raise ValueError("slot IDs must be nonnegative integers; ports must be 1024..65535")
        if port in (services["postgresPort"], services["prometheusPort"]):
            raise ValueError("preview port conflicts with shared dependencies")
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
        if (type(port) is not int or not 1024 <= port <= 65535 or port in ports
                or port in (services["postgresPort"], services["prometheusPort"])):
            raise ValueError("stable port must be valid and separate from worktree slots")
        if not stable["url"].startswith("https://") or not stable.get("branch"):
            raise ValueError("stable requires an HTTPS URL and an expected branch")
    return cfg


def validate_lan_host(host):
    """Only explicit RFC1918 IPv4 addresses; never expose every host interface."""
    try:
        address = ipaddress.IPv4Address(host)
    except (ipaddress.AddressValueError, TypeError):
        raise ValueError("LAN host must be an explicit RFC1918 IPv4 address") from None
    if not isinstance(host, str) or not any(address in ipaddress.IPv4Network(network)
            for network in ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")):
        raise ValueError("LAN host must be an explicit RFC1918 IPv4 address")
    return host


def lan_url(binding):
    host = binding.get("lanHost")
    if not host or binding.get("role") == "stable":
        return None
    return f"http://{validate_lan_host(host)}:{binding['port']}"


def entry_urls(binding, lan=False, public=False):
    local_lan = lan_url(binding)
    if lan and not local_lan:
        raise RuntimeError("this preview has no LAN entry; use refresh --lan-host <private-IP>")
    fallback = binding.get("url") or f"http://127.0.0.1:{binding['port']}"
    if lan:
        return [local_lan]
    if public:
        return [fallback]
    return [url for url in (local_lan, binding.get("url")) if url] or [fallback]


def check_listeners(binding):
    hosts = ["127.0.0.1"]
    if lan_url(binding):
        hosts.append(binding["lanHost"])
    with contextlib.ExitStack() as stack:
        for host in hosts:
            sock = stack.enter_context(socket.socket())
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            try:
                sock.bind((host, binding["port"]))
            except OSError as error:
                raise RuntimeError(f"preview listener unavailable at {host}:{binding['port']}: {error}") from error


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
    host = urlsplit(url).hostname
    try:
        direct = ipaddress.ip_address(host).is_private
    except ValueError:
        direct = False
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}) if direct
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
    result["storage"] = manifest.get("storage", {"database": "sqlite", "redis": "miniredis", "metrics": "embedded"})
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
    if lan_url(binding):
        result["lanUrl"] = lan_url(binding)
        result["lan"] = probe(result["lanUrl"], result["buildId"]) if result["running"] else {"ok": False}
    if public and binding.get("url"):
        result["public"] = probe(binding["url"], result["buildId"])
    return result


def reserve(all_bindings, slot_id, stable=False, lan_host=None):
    used = {b["slot"] for b in all_bindings.values()}
    cfg = config()
    resources = [{**cfg["stable"], "id": "stable"}] if stable else cfg["slots"]
    if lan_host is None:
        lan_host = cfg.get("lan", {}).get("host", "") if not stable else ""
    if stable and lan_host:
        raise ValueError("stable does not support the development LAN entry")
    if lan_host:
        validate_lan_host(lan_host)
    last_error = ""
    for slot in resources:
        if slot_id is not None and slot["id"] != slot_id:
            continue
        if slot["id"] in used:
            continue
        binding = {"slot": slot["id"], "port": slot["port"], "url": slot.get("url"),
                "instance": secrets.token_hex(8), "worktree": str(ROOT),
                "role": "stable" if stable else "development"}
        if lan_host:
            binding["lanHost"] = lan_host
        try:
            check_listeners(binding)
        except RuntimeError as error:
            last_error = f"; {error}"
            continue
        return binding
    raise RuntimeError("no free slot (stopped/failed previews retain their binding; destroy to release)" + last_error)


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


def archive_legacy(directory):
    """Called after stopping the child and before overwriting its binaries/config."""
    manifest = directory / "manifest.json"
    archive = directory / "legacy-sqlite"
    if not manifest.exists() or archive.exists():
        return
    previous = json.loads(manifest.read_text())
    if previous.get("storage", {}).get("database") == "postgres":
        return
    staging = directory / "legacy-sqlite.partial"
    if staging.exists():
        shutil.rmtree(staging)
    staging.mkdir(mode=0o700)
    for name in ("run", "bin"):
        if (directory / name).exists():
            shutil.copytree(directory / name, staging / name)
    for name in ("manifest.json", "credentials.json"):
        if (directory / name).exists():
            shutil.copy2(directory / name, staging / name)
    staging.replace(archive)


def app_config(binding, backend_port):
    # A dedicated minimal profile: never inherit production DSNs, writers or notifications.
    directory = instance(binding)
    resource = json.loads((directory / "postgres.json").read_text())
    collector_file = directory / "collector.json"
    collector = json.loads(collector_file.read_text()) if collector_file.exists() else None
    agent_auth = ""
    collector_writer = ""
    if collector is not None:
        if (binding.get("role", "development") != "development"
                or set(collector) != {"username", "password"}
                or not all(isinstance(v, str) and len(v) >= 8 for v in collector.values())):
            raise ValueError("invalid private development collector configuration")
        agent_auth = "BasicAuth = {" + json.dumps(collector["username"]) + " = " + json.dumps(collector["password"]) + "}"
        if not preview_services.prometheus_write_flags(STATE / "shared"):
            raise ValueError("collector requires the host-owned shared Prometheus remote-write opt-in")
        collector_writer = "[[Pushgw.Writers]]\nUrl = " + json.dumps(resource["prometheusURL"].rstrip("/") + "/api/v1/write")
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
Enable = {str(collector is not None).lower()}
{agent_auth}
[HTTP.ProxyAuth]
Enable = false
[HTTP.TokenAuth]
Enable = true
[HTTP.JWTAuth]
AccessExpired = 1500
RefreshExpired = 10080
[DB]
DBType = "postgres"
DSN = {json.dumps(resource['dsn'])}
MaxOpenConns = 5
MaxIdleConns = 2
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
Enable = false
[Ibex]
Enable = false
{collector_writer}
'''


def committed_frontend():
    bundle = ROOT / "front/statik/assets.zip"
    return {"kind": "committed-statik", "sha256": digest(
        bundle if bundle.is_file() else ROOT / "front/statik/statik.go")}


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
    frontend = committed_frontend()
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
        run(["go", "build", "-buildvcs=false", "-o", str(directory / "bin/preview-db"), "./cmd/preview-db"],
            cwd=MANAGER_ROOT, stdout=log, stderr=log)
    if source_identity()["fingerprint"] != source["fingerprint"]:
        raise RuntimeError("source changed during build; stop editing and refresh")
    resource = preview_services.prepare(STATE, config(), binding, directory)
    if not resource["initialized"]:
        old_database = directory / "run/n9e.db"
        if old_database.exists():
            backup = directory / "sqlite-backup.db"
            if not backup.exists():
                with contextlib.closing(sqlite3.connect(old_database)) as original, contextlib.closing(sqlite3.connect(backup)) as saved:
                    original.backup(saved)
                backup.chmod(0o600)
            resource["sqlite"] = str(backup)
        else:
            credentials = {"username": "root", "password": secrets.token_urlsafe(18)}
            write_json(directory / "credentials.json", credentials)
            write_json(directory / "login-check.pending", {})
            resource["rootPassword"] = credentials["password"]
    resource["engineName"] = "preview-" + binding["instance"]
    write_json(directory / "postgres.json", resource)
    with (directory / "database-bootstrap.log").open("a") as log:
        run([str(directory / "bin/preview-db"), str(directory / "postgres.json")], stdout=log, stderr=log)
    resource["initialized"] = True
    resource.pop("rootPassword", None)
    write_json(directory / "postgres.json", resource)
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
    if lan_url(binding):
        manifest["lanHost"] = binding["lanHost"]
    manifest.update(databaseDsn=resource["dsn"], prometheusURL=resource["prometheusURL"],
                    storage={"database": "postgres", "databaseName": resource["name"],
                             "redis": "miniredis", "metrics": "shared-prometheus"})
    if (directory / "collector.json").exists():
        manifest["storage"]["collectorMetrics"] = "shared-prometheus"
    if profile == "full":
        manifest["qaCorpus"] = json.loads((ROOT / "aiagent/skill/embedded/codeassets/manifest.json").read_text())
    if source_identity()["fingerprint"] != source["fingerprint"]:
        raise RuntimeError("source changed during database bootstrap; stop editing and refresh")
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
    parser.add_argument("command", choices=["up", "refresh", "status", "url", "stop", "destroy", "list", "services"])
    parser.add_argument("--slot", type=int)
    parser.add_argument("--stable", action="store_true", help="manage the separate clean main-checkout instance")
    parser.add_argument("--if-exists", action="store_true")
    parser.add_argument("--local-only", action="store_true", help="skip public probe (never changes binding)")
    lan_options = parser.add_mutually_exclusive_group()
    lan_options.add_argument("--lan-host", type=validate_lan_host, help="up/refresh: bind this private IPv4 address too")
    lan_options.add_argument("--no-lan", action="store_true", help="up/refresh: disable LAN access")
    url_options = parser.add_mutually_exclusive_group()
    url_options.add_argument("--lan", action="store_true", help="url: require a LAN entry")
    url_options.add_argument("--public", action="store_true", help="url: select the existing public entry")
    parser.add_argument("--profile", choices=["basic", "full"], default=None)
    parser.add_argument("--frontend-dir", type=Path, help="built frontend directory containing index.html")
    parser.add_argument("--frontend-ref", help="frontend commit/tag corresponding to --frontend-dir")
    args = parser.parse_args()
    if (args.lan_host or args.no_lan) and (args.stable or args.command not in ("up", "refresh")):
        parser.error("--lan-host/--no-lan apply only to development up/refresh")
    if (args.lan or args.public) and args.command != "url":
        parser.error("--lan/--public apply only to url")
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
            for url in entry_urls(binding, lan=args.lan, public=args.public):
                print(url + "/__preview/ready")
            return
        result = inspect(binding, public=not args.local_only)
        print(json.dumps(result, indent=2, ensure_ascii=False))
        return
    with lock():
        if args.command == "services":
            print(json.dumps(preview_services.ensure(STATE, config()), indent=2))
            return
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
                preview_services.destroy(STATE, config(), binding, directory)
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
            if (args.lan_host and args.lan_host != binding.get("lanHost")) or (args.no_lan and binding.get("lanHost")):
                raise RuntimeError("use refresh to change the LAN entry")
            if args.frontend_dir:
                raise RuntimeError("use refresh to change frontend assets")
            if args.profile and args.profile != binding.get("profile"):
                raise RuntimeError("use refresh to change preview profile")
        else:
            if binding:
                stop(binding)
                archive_legacy(instance(binding))
            else:
                binding = reserve(all_bindings, args.slot, stable=args.stable,
                                  lan_host="" if args.no_lan else args.lan_host)
                all_bindings[binding_key] = binding
            if args.lan_host:
                binding["lanHost"] = args.lan_host
            elif args.no_lan:
                binding.pop("lanHost", None)
            # Persist binding before build: failures remain inspectable/cleanable.
            write_json(STATE / "bindings.json", all_bindings)
            check_listeners(binding)
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
                print(f"preview setup failed; inspect {directory}/build.log, frontend-build.log and database-bootstrap.log", file=sys.stderr)
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
                or not result.get("lan", {"ok": True})["ok"]
                or result["sourceChanged"] or result.get("frontendChanged")):
            raise RuntimeError("preview not ready for handoff; inspect status (use --local-only before tunnel setup)")


if __name__ == "__main__":
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        print(f"preview: {error}", file=sys.stderr)
        sys.exit(1)
