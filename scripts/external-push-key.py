#!/usr/bin/env python3
"""Provision a versioned center signing key outside Git. Never overwrite a key."""
import argparse
import getpass
import os
from pathlib import Path
import sys
import warnings


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--path', type=Path, default=Path('/etc/n9e/secrets/platform10.key'))
    parser.add_argument('--stdin', action='store_true', help='Read exact UTF-8 bytes from stdin, without a trailing newline')
    args = parser.parse_args()
    if not args.path.is_absolute():
        parser.error('--path must be absolute')
    checkout = Path(__file__).resolve().parents[1]
    if (checkout / '.git').exists() and args.path.resolve().is_relative_to(checkout):
        parser.error('key files must be outside the Git checkout')
    if args.stdin:
        raw = sys.stdin.buffer.read(65537)
    else:
        with warnings.catch_warnings():
            warnings.simplefilter('error', getpass.GetPassWarning)
            value = getpass.getpass('1.0 signing password (not echoed): ')
            if value != getpass.getpass('Confirm signing password: '):
                raise ValueError('password confirmation differs')
        raw = value.encode('utf-8')
    text = raw.decode('utf-8')
    if not raw or len(raw) > 65536 or not text.strip() or text.startswith('\ufeff') or any(c in text for c in '\r\n\0'):
        raise ValueError('key must be nonempty UTF-8 without BOM, line breaks or NUL; no automatic trimming')
    args.path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    descriptor = os.open(args.path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(descriptor, 'wb') as target:
            target.write(raw)
            target.flush()
            os.fsync(target.fileno())
    except BaseException:
        args.path.unlink(missing_ok=True)
        raise
    print('Signing key file created; existing keys are never overwritten.')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, UnicodeError, getpass.GetPassWarning):
        print('Key creation failed: check content, permissions and whether the file already exists. No key value is logged.', file=sys.stderr)
        sys.exit(1)
