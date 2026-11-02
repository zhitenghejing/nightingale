#!/usr/bin/env python3
"""Compatibility entry point; the old publication is retired."""
from pathlib import Path
import runpy

if __name__ == '__main__':
    runpy.run_path(str(Path(__file__).resolve().parent.parent /
                      'template-driven-device-management/publish.py'), run_name='__main__')
