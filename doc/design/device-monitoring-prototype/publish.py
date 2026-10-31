#!/usr/bin/env python3
"""Run with python3 -B doc/design/device-monitoring-prototype/publish.py.

Publish a standalone, memory-only design study alongside the existing preview.
Preserve the business UI and other design publications byte for byte.
"""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def hashes(directory):
    return {
        p.relative_to(directory).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in directory.rglob('*') if p.is_file()
    }


def main():
    page = (HERE / 'prototype.html').read_text()
    script = page.split('<script>', 1)[1].split('</script>', 1)[0]
    subprocess.run(['node', '--check'], input=script, text=True, check=True)
    result = subprocess.run(
        [sys.executable, '-B', 'scripts/preview.py', 'status', '--local-only'],
        cwd=ROOT, check=True, capture_output=True, text=True,
    )
    current = json.loads(result.stdout)
    if current.get('role') != 'development':
        raise SystemExit('Only a development preview may host this prototype.')
    published = Path(current['instanceDir']) / 'run/pub'
    if not (published / 'index.html').is_file():
        raise SystemExit('Existing preview UI assets are required for additive publication.')
    original = hashes(published)
    revision = hashlib.sha256(b''.join(
        (HERE / name).read_bytes() for name in ('prototype.html', 'publish.py')
    )).hexdigest()
    stage = Path(tempfile.mkdtemp(prefix='n9e-monitoring-prototype-'))
    shutil.copytree(published, stage, dirs_exist_ok=True)
    target = stage / 'prototype/device-monitoring.html'
    target.parent.mkdir(exist_ok=True)
    target.write_text(page.replace('<head>', '<head><meta name="monitoring-prototype-revision" '
                                 'content="' + revision + '">', 1))
    after = hashes(stage)
    for name, digest in original.items():
        if name != 'prototype/device-monitoring.html' and after.get(name) != digest:
            raise SystemExit('Unexpected asset change: ' + name)
    # Keep this branch's established LAN-only acceptance policy.
    subprocess.run([
        sys.executable, '-B', 'scripts/preview.py', 'refresh',
        '--lan-host', '192.168.0.152', '--local-only',
        '--frontend-dir', str(stage),
        '--frontend-ref', 'monitoring-prototype:' + revision,
    ], cwd=ROOT, check=True)
    print('\nPrototype: http://192.168.0.152:' + str(current['port'])
          + '/prototype/device-monitoring.html?variant=A')
    print('Prototype revision: ' + revision)
    print('Preserved existing assets:', len(original))
    print('Staged assets (retain while preview is active):', stage)


if __name__ == '__main__':
    main()
