#!/usr/bin/env python3
"""Build the in-tree frontend with a pinned toolchain and verifiable assets."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def run(command, **kwargs):
    return subprocess.run(command, cwd=ROOT / "frontend", check=True, **kwargs)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def tree_sha(directory):
    result = hashlib.sha256()
    for path in sorted(Path(directory).rglob("*")):
        if path.is_file():
            result.update(str(path.relative_to(directory)).encode() + b"\0" + sha(path).encode())
    return result.hexdigest()


def source_sha():
    # Includes dirty and new files, but never walks node_modules or build output.
    result = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "frontend", "scripts/frontend.py"], cwd=ROOT)
    files = {ROOT / os.fsdecode(name) for name in result.split(b"\0") if name}
    files.update((ROOT / "frontend").glob(".env*"))
    digest = hashlib.sha256()
    for path in sorted(files):
        digest.update(str(path.relative_to(ROOT)).encode() + b"\0")
        digest.update(sha(path).encode() if path.is_file() else b"<deleted>")
    # Vite reads public build settings from the process environment as well.
    environment = {k: v for k, v in os.environ.items() if k.startswith("VITE_")}
    digest.update(json.dumps(environment, sort_keys=True).encode())
    return digest.hexdigest()


def check_toolchain():
    run(["node", "scripts/check-toolchain.cjs"])


def current():
    frontend = ROOT / "frontend"
    try:
        manifest = json.loads((frontend / ".build-manifest.json").read_text())
        if (manifest["sourceSha256"] == source_sha() and (frontend / "pub/index.html").is_file()
                and manifest["sha256"] == tree_sha(frontend / "pub")):
            return manifest
    except (OSError, ValueError, KeyError):
        pass
    return None


def install():
    check_toolchain()
    run(["pnpm", "install", "--frozen-lockfile", "--prod=false"])


def build(force=False):
    check_toolchain()
    frontend = ROOT / "frontend"
    with (frontend / ".build.lock").open("w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        manifest = current()
        if manifest and not force:
            return manifest
        (frontend / ".build-manifest.json").unlink(missing_ok=True)
        install()
        before = source_sha()
        shutil.rmtree(frontend / "pub", ignore_errors=True)
        run(["pnpm", "run", "build"], env={**os.environ, "NODE_ENV": "production"})
        if before != source_sha():
            raise RuntimeError("frontend source changed during build; stop editing and rebuild")
        if not (frontend / "pub/index.html").is_file():
            raise RuntimeError("frontend build did not produce pub/index.html")
        package = json.loads((frontend / "package.json").read_text())
        manifest = {"kind": "in-tree", "ref": before, "sourceSha256": before,
                    "sha256": tree_sha(frontend / "pub"), "lockSha256": sha(frontend / "pnpm-lock.yaml"),
                    "node": (frontend / ".node-version").read_text().strip(), "packageManager": package["packageManager"]}
        with tempfile.NamedTemporaryFile(mode="w", dir=frontend, delete=False, prefix=".build-", suffix=".tmp") as stream:
            json.dump(manifest, stream, indent=2)
            stream.write("\n")
        Path(stream.name).replace(frontend / ".build-manifest.json")
        return manifest


def embed():
    manifest = build()
    with (ROOT / "frontend/.build.lock").open("w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if current() != manifest:
            raise RuntimeError("frontend changed before embedding; rebuild")
        with tempfile.TemporaryDirectory(prefix="n9e-frontend-embed-") as temporary:
            directory = Path(temporary)
            shutil.copytree(ROOT / "frontend/pub", directory / "pub")
            if tree_sha(directory / "pub") != manifest["sha256"]:
                raise RuntimeError("frontend changed while staging statik")
            # statik is pinned in go.mod. -m normalizes resource timestamps.
            subprocess.run(["go", "run", "github.com/rakyll/statik", "-m", "-f", "-include=*",
                            "-src=" + str(directory / "pub"), "-dest=" + str(directory)], cwd=ROOT, check=True)
            if current() != manifest:
                raise RuntimeError("frontend changed while embedding; rebuild")
            destination = ROOT / "front/statik/statik.go"
            destination.parent.mkdir(parents=True, exist_ok=True)
            with tempfile.NamedTemporaryFile(dir=destination.parent, prefix=".statik-", suffix=".tmp", delete=False) as stream:
                stream.write((directory / "statik/statik.go").read_bytes())
            staged = Path(stream.name)
            staged.chmod(0o644)
            staged.replace(destination)
    return manifest


def e2e():
    check_toolchain()
    result = subprocess.check_output([sys.executable, "-B", str(ROOT / "scripts/preview.py"), "status"], cwd=ROOT)
    status = json.loads(result)
    if (status.get("role") != "development" or not status.get("running") or status.get("sourceChanged")
            or status.get("frontendChanged") or status.get("frontend", {}).get("kind") != "in-tree"
            or not status.get("local", {}).get("ok") or not status.get("public", {"ok": True}).get("ok")):
        raise RuntimeError("refresh the in-tree development preview before running E2E")
    environment = {**os.environ, "N9E_E2E_BASE_URL": status["url"], "N9E_E2E_BUILD_ID": status["buildId"],
                   "N9E_E2E_CREDENTIALS_FILE": status["credentialsFile"]}
    run(["pnpm", "exec", "playwright", "test", "--config", "playwright.migration.config.ts"], env=environment)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("install", "build", "verify", "embed", "identity", "source", "e2e"))
    parser.add_argument("--force", action="store_true", help="rebuild even when source and asset digests still match")
    args = parser.parse_args()
    if args.command == "source":
        print(source_sha())
        return
    if args.command == "e2e":
        e2e()
        return
    if args.command == "identity":
        manifest = current()
        if not manifest:
            raise RuntimeError("frontend assets are missing or stale; run make frontend-build")
    elif args.command == "install":
        install()
        return
    elif args.command == "embed":
        manifest = embed()
    else:
        manifest = build(force=args.force)
        if args.command == "verify":
            run(["pnpm", "run", "test", "--runInBand"])
    print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"frontend: {error}", file=sys.stderr)
        sys.exit(1)
