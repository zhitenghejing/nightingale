#!/usr/bin/env python3
"""Publish only the standalone prototype alongside the current built UI.

Run: python3 doc/design/device-onboarding-prototype/publish.py
The preview manager owns the staging identity and restart. Business sources,
the built frontend/pub directory, credentials and collector configuration are
preserved. This replaces the preview's frontend mode with explicit file assets.
"""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def main():
    status = subprocess.run(
        [sys.executable, '-B', 'scripts/preview.py', 'status', '--local-only'],
        cwd=ROOT, capture_output=True, text=True,
    )
    current = json.loads(status.stdout)
    instance = Path(current['instanceDir']).resolve()
    stage = instance / 'device-onboarding-prototype-assets'
    frontend = ROOT / 'frontend/pub'
    if not (frontend / 'index.html').is_file():
        raise SystemExit('Build the in-tree frontend before publishing this prototype.')
    if stage.exists():
        shutil.rmtree(stage)
    shutil.copytree(frontend, stage)
    destination = stage / 'prototype'
    destination.mkdir(parents=True, exist_ok=True)
    sources = [HERE / 'prototype.html', HERE / 'catalog.json', HERE / 'README.md']
    revision = hashlib.sha256(b''.join(p.read_bytes() for p in sources)).hexdigest()
    html = sources[0].read_text().replace('<head>', '<head><meta name="onboarding-prototype-revision" content="' + revision + '">', 1)
    (destination / 'device-onboarding.html').write_text(html)
    shutil.copyfile(HERE / 'README.md', destination / 'device-onboarding-plan.md')
    shutil.copyfile(HERE / 'catalog.json', destination / 'device-onboarding-catalog.txt')
    subprocess.run([
        sys.executable, '-B', 'scripts/preview.py', 'refresh', '--lan-host',
        '192.168.0.152', '--local-only', '--frontend-dir', str(stage),
        '--frontend-ref', 'device-onboarding-prototype:' + revision,
    ], cwd=ROOT, check=True)
    print('\nPrototype: http://192.168.0.152:' + str(current['port']) + '/prototype/device-onboarding.html')
    print('Prototype revision: ' + revision)


if __name__ == '__main__':
    main()
