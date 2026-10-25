"""One host-owned PostgreSQL/Prometheus pair; databases belong to preview IDs."""
import hashlib
import json
from pathlib import Path
import re
import secrets
import subprocess
import time
import urllib.request


POSTGRES_IMAGE = "postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24"
PROMETHEUS_IMAGE = "prom/prometheus@sha256:fa3302279710be2f21a8301ec17a8b4f2301fd829bd113839b3b6b02b6f191a2"
DEFAULT_PORTS = {"postgresPort": 35432, "prometheusPort": 39090}


def write_private(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.chmod(0o600)
    temporary.replace(path)


def docker(*args, input=None):
    # SQL and credentials stay off argv and public logs, including failure output.
    result = subprocess.run(["docker", *args], input=input, text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError("preview dependency command failed; check Docker availability and owned service logs")
    return result.stdout.strip()


def identity(state):
    return hashlib.sha256(str(Path(state).resolve()).encode()).hexdigest()[:12]


def settings(state, config):
    configured = config.get("services", {})
    if not isinstance(configured, dict) or set(configured) - set(DEFAULT_PORTS):
        raise ValueError("services accepts only postgresPort and prometheusPort")
    ports = {**DEFAULT_PORTS, **configured}
    for name in DEFAULT_PORTS:
        port = ports[name]
        if type(port) is not int or not 1024 <= port <= 65535:
            raise ValueError("invalid preview shared-service port")
    if ports["postgresPort"] == ports["prometheusPort"]:
        raise ValueError("preview shared-service ports must differ")
    owner = identity(state)
    return {**ports, "owner": owner, "postgres": f"n9e-shared-pg-{owner}",
            "prometheus": f"n9e-shared-prom-{owner}"}


def container(name):
    result = subprocess.run(["docker", "inspect", name], capture_output=True, text=True)
    if result.returncode:
        return None
    return json.loads(result.stdout)[0]


def ensure_container(name, owner, args):
    current = container(name)
    if current:
        if (current["Config"].get("Labels") or {}).get("n9e.preview.owner") != owner:
            raise RuntimeError("refusing to manage a dependency owned by another environment")
        if not current["State"]["Running"]:
            docker("start", name)
    else:
        docker("run", "-d", "--name", name, "--label", f"n9e.preview.owner={owner}",
               "--restart", "unless-stopped", *args)


def register_services(directory, cfg):
    """Rename legacy containers in place; keep their persistent volume identities."""
    marker = directory / "services.json"
    previous = json.loads(marker.read_text()) if marker.exists() else cfg.copy()
    recorded = {key: value for key, value in previous.items() if key != "volumes"}
    legacy = {**cfg, "postgres": f"n9e-preview-pg-{cfg['owner']}",
              "prometheus": f"n9e-preview-prom-{cfg['owner']}"}
    if recorded != cfg and recorded != legacy:
        raise RuntimeError("shared service settings changed; migrate dependencies explicitly before restarting")
    volumes = previous.get("volumes", {key: f"{recorded[key]}-data"
                                      for key in ("postgres", "prometheus")})
    if (not isinstance(volumes, dict) or set(volumes) != {"postgres", "prometheus"}
            or any(volumes[key] not in (f"{cfg[key]}-data", f"{legacy[key]}-data")
                   for key in ("postgres", "prometheus"))):
        raise RuntimeError("shared service volume ownership mismatch")
    renames = []
    # Validate the whole pair before changing anything. A partially completed rename
    # can be retried: the old marker stays until both containers have been renamed.
    for key in ("postgres", "prometheus"):
        old_name, new_name = recorded[key], cfg[key]
        target = container(new_name)
        old = container(old_name) if old_name != new_name else None
        for current in (old, target):
            if current and (current["Config"].get("Labels") or {}).get("n9e.preview.owner") != cfg["owner"]:
                raise RuntimeError("refusing to rename a dependency owned by another environment")
        if old and target:
            raise RuntimeError("both legacy and shared containers exist; resolve the naming collision")
        if old:
            renames.append((old_name, new_name))
    for old_name, new_name in renames:
        docker("rename", old_name, new_name)
    write_private(marker, {**cfg, "volumes": volumes})
    return volumes


def prometheus_write_flags(directory):
    # Host-owned opt-in; default disposable test registries remain read-only.
    return ["--web.enable-remote-write-receiver"] if (Path(directory) / "prometheus-remote-write.enabled").is_file() else []


def ensure(state, config):
    state = Path(state)
    cfg = settings(state, config)
    directory = state / "shared"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    volumes = register_services(directory, cfg)
    password_file = directory / "postgres-password"
    if not password_file.exists():
        if container(cfg["postgres"]):
            raise RuntimeError("shared PostgreSQL management credential is missing; recover it before restarting")
        password_file.write_text(secrets.token_hex(24))
        password_file.chmod(0o600)
    env_file = directory / "postgres.env"
    env_file.write_text("POSTGRES_PASSWORD=" + password_file.read_text().strip() + "\n")
    env_file.chmod(0o600)
    ensure_container(cfg["postgres"], cfg["owner"], ["-p", f"127.0.0.1:{cfg['postgresPort']}:5432",
                     "--env-file", str(env_file), "-v", f"{volumes['postgres']}:/var/lib/postgresql/data",
                     POSTGRES_IMAGE])
    prom_config = directory / "prometheus.yml"
    # Default fixtures are read-only; collector ingestion requires a host-owned opt-in.
    prom_config.write_text("global:\n  scrape_interval: 5s\nscrape_configs:\n"
                           "  - job_name: prometheus\n    static_configs:\n"
                           "      - targets: ['localhost:9090']\n")
    prom_config.chmod(0o644)
    ensure_container(cfg["prometheus"], cfg["owner"], ["-p", f"127.0.0.1:{cfg['prometheusPort']}:9090",
                     "-v", f"{prom_config}:/etc/prometheus/prometheus.yml:ro",
                     "-v", f"{volumes['prometheus']}:/prometheus", PROMETHEUS_IMAGE,
                     "--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.retention.time=3d",
                     "--storage.tsdb.retention.size=1GB", *prometheus_write_flags(directory)])
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        result = subprocess.run(["docker", "exec", cfg["postgres"], "pg_isready", "-U", "postgres"],
                                capture_output=True)
        if result.returncode == 0:
            break
        time.sleep(1)
    else:
        raise RuntimeError("shared PostgreSQL did not become ready")
    return cfg


def sql(cfg, statement, database="postgres"):
    current = container(cfg["postgres"])
    if not current or (current["Config"].get("Labels") or {}).get("n9e.preview.owner") != cfg["owner"]:
        raise RuntimeError("PostgreSQL service ownership mismatch")
    return docker("exec", "-i", cfg["postgres"], "psql", "-X", "-v", "ON_ERROR_STOP=1",
                  "-U", "postgres", "-d", database, "-At", input=statement)


def resource_name(binding):
    ident = binding["instance"]
    if not re.fullmatch(r"[0-9a-f]{16}", ident):
        raise ValueError("invalid preview instance ID")
    return "n9ep_" + ident


def prepare(state, config, binding, directory):
    cfg = ensure(state, config)
    name = resource_name(binding)
    path = Path(directory) / "postgres.json"
    if path.exists():
        resource = json.loads(path.read_text())
        if resource["name"] != name or resource["owner"] != cfg["owner"]:
            raise RuntimeError("PostgreSQL resource ownership mismatch")
    else:
        resource = {"name": name, "owner": cfg["owner"], "password": secrets.token_hex(24),
                    "initialized": False, "provisioned": False, "roleOwned": False}
        write_private(path, resource)
    # Never recreate an initialized database that disappeared: surface data loss.
    existing = sql(cfg, f"SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname='{name}';")
    if existing and existing != name:
        raise RuntimeError("database is owned by another role")
    if existing and not resource.get("provisioned"):
        raise RuntimeError("existing database has no provisioning receipt; recover its metadata before proceeding")
    if not existing:
        if resource["initialized"]:
            raise RuntimeError("initialized preview database is missing; refusing to silently reset it")
        role = sql(cfg, f"SELECT rolname FROM pg_roles WHERE rolname='{name}';")
        if role and not resource.get("roleOwned"):
            raise RuntimeError("existing role has no provisioning receipt; recover its metadata before proceeding")
        if not role:
            sql(cfg, f"CREATE ROLE {name} LOGIN PASSWORD '{resource['password']}' "
                     "NOSUPERUSER NOCREATEDB NOCREATEROLE CONNECTION LIMIT 8;")
            resource["roleOwned"] = True
            write_private(path, resource)
        sql(cfg, f"CREATE DATABASE {name} OWNER {name};\nREVOKE ALL ON DATABASE {name} FROM PUBLIC;")
        resource["provisioned"] = True
        write_private(path, resource)
    # Legacy main checkouts check pg_database through the postgres maintenance DB.
    # This grants connection only, never ownership/CREATE or other environment DB access.
    sql(cfg, f"GRANT CONNECT ON DATABASE postgres TO {name};\n"
             f"ALTER ROLE {name} SET statement_timeout='30s';")
    resource["dsn"] = (f"host=127.0.0.1 port={cfg['postgresPort']} user={name} "
                       f"password={resource['password']} dbname={name} sslmode=disable connect_timeout=5")
    resource["prometheusURL"] = f"http://127.0.0.1:{cfg['prometheusPort']}"
    write_private(path, resource)
    return resource


def destroy(state, config, binding, directory):
    path = Path(directory) / "postgres.json"
    if not path.exists():
        return
    cfg = settings(state, config)
    resource = json.loads(path.read_text())
    name = resource_name(binding)
    if resource["name"] != name or resource["owner"] != cfg["owner"]:
        raise RuntimeError("refusing to delete PostgreSQL resources with mismatched ownership")
    existing = sql(cfg, f"SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname='{name}';")
    if existing and existing != name:
        raise RuntimeError("refusing to delete another owner's database")
    if existing and not resource.get("provisioned"):
        raise RuntimeError("refusing to delete a database without its provisioning receipt")
    sql(cfg, f"DROP DATABASE IF EXISTS {name} WITH (FORCE);")
    if sql(cfg, f"SELECT rolname FROM pg_roles WHERE rolname='{name}';"):
        if not resource.get("roleOwned"):
            raise RuntimeError("refusing to delete a role without its provisioning receipt")
        sql(cfg, f"REVOKE ALL ON DATABASE postgres FROM {name};\nDROP ROLE {name};")


def probe_prometheus(cfg):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    url = f"http://127.0.0.1:{cfg['prometheusPort']}/api/v1/query?query=up"
    with opener.open(url, timeout=5) as response:
        result = json.load(response)
    return result.get("status") == "success" and bool(result.get("data", {}).get("result"))
