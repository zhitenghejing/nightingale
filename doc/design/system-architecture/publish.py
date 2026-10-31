#!/usr/bin/env python3
"""Publish architecture drawings without modifying the existing application UI."""
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
        cwd=ROOT, capture_output=True, text=True, check=True,
    )
    current = json.loads(status.stdout)
    if current['role'] != 'development' or not current['running']:
        raise SystemExit('A running development preview is required.')
    instance = Path(current['instanceDir']).resolve()
    origin = instance / 'run/pub'
    stage = instance / 'system-architecture-assets'
    if not (origin / 'index.html').is_file():
        raise SystemExit('The current preview has no built application UI.')
    sources = sorted(p for p in HERE.iterdir() if p.suffix in ('.svg', '.png', '.mmd', '.md', '.html'))
    revision = hashlib.sha256(b''.join(p.name.encode()+b'\0'+p.read_bytes() for p in sources)).hexdigest()
    if stage.exists():
        shutil.rmtree(stage)
    shutil.copytree(origin, stage)
    destination = stage / 'architecture'
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir()
    for path in sources:
        shutil.copyfile(path, destination / path.name)
        if path.suffix == '.mmd':
            # Center serves only its existing static suffix allowlist. Preserve
            # business routing and offer the editable text via an allowed suffix.
            shutil.copyfile(path, destination / (path.name+'.txt'))
    html = (destination / 'architecture.html').read_text().replace(
        '<head>', '<head><meta name="architecture-revision" content="'+revision+'">', 1,
    )
    (destination / 'architecture.html').write_text(html)
    subprocess.run([
        sys.executable, '-B', 'scripts/preview.py', 'refresh', '--lan-host',
        '192.168.0.152', '--local-only', '--frontend-dir', str(stage),
        '--frontend-ref', 'system-architecture:'+revision,
    ], cwd=ROOT, check=True)
    print('\nArchitecture: http://192.168.0.152:'+str(current['port'])+'/architecture/architecture.html')
    print('Revision: '+revision)


if __name__ == '__main__':
    main()
