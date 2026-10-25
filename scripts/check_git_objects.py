#!/usr/bin/env python3
"""Reject oversized Git blobs, including versions deleted by later commits."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
MAX_BLOB_BYTES = 100 * 1024 * 1024


def git(*args, input=None):
    return subprocess.check_output(["git", *args], cwd=ROOT, input=input)


def oversized_blobs(revisions, *, include_index=False, limit=MAX_BLOB_BYTES):
    objects = set(git("rev-list", "--objects", "--no-object-names", *revisions).splitlines())
    paths = {}
    if include_index:
        for entry in git("ls-files", "--stage", "-z").split(b"\0"):
            if entry:
                metadata, path = entry.split(b"\t", 1)
                mode, oid, _ = metadata.split()
                if mode == b"160000":
                    continue  # Submodule commits are not blobs in this repository.
                objects.add(oid)
                paths[oid] = os.fsdecode(path)
    if not objects:
        return []
    result = git("cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)",
                 input=b"\n".join(sorted(objects)) + b"\n")
    oversized = []
    for line in result.splitlines():
        oid, kind, size = line.split()
        if kind == b"blob" and int(size) > limit:
            oversized.append((oid.decode(), int(size), paths.get(oid)))
    return oversized


def push_revisions(remote, updates):
    tips, previous = set(), set()
    for line in updates.splitlines():
        _, local_oid, _, remote_oid = line.split()
        if not local_oid.strip("0"):
            continue  # Deleting a remote ref transfers no objects.
        tips.add(local_oid)
        if remote_oid.strip("0"):
            available = subprocess.run(["git", "cat-file", "-e", remote_oid], cwd=ROOT,
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if available.returncode == 0:
                previous.add(remote_oid)
    if not tips:
        return []
    # Exclude only objects already known on this remote, never local backup refs.
    return sorted(tips) + ["--not", *sorted(previous), "--remotes=" + remote]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--base", help="check HEAD's new history and the index against this revision")
    mode.add_argument("--pre-push", metavar="REMOTE", help="read Git pre-push ref updates from stdin")
    args = parser.parse_args()
    if args.pre_push is not None:
        revisions = push_revisions(args.pre_push, sys.stdin.read())
        if not revisions:
            return 0
    else:
        revisions = ["HEAD", "--not", args.base]
    oversized = oversized_blobs(revisions, include_index=args.pre_push is None)
    if oversized:
        print("Git blobs exceed GitHub's 100 MiB file limit:", file=sys.stderr)
        for oid, size, path in oversized:
            print(f"  {size / 1024 ** 2:.2f} MiB  {path or '(historical blob)'}  {oid}", file=sys.stderr)
        print("Replace these blobs in the unpushed commits; deleting them in a later commit is insufficient.",
              file=sys.stderr)
        return 1
    print("Git blob size check passed (100 MiB limit).")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"Git blob size check failed: {error}", file=sys.stderr)
        sys.exit(1)
