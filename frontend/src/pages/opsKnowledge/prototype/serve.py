#!/usr/bin/env python3
"""THROWAWAY: serve only this static prototype, loopback by default."""
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from functools import partial
from pathlib import Path
import argparse
p=argparse.ArgumentParser();p.add_argument('--port',type=int,default=30418);p.add_argument('--bind',default='127.0.0.1');a=p.parse_args()
print(f'Prototype: http://{a.bind}:{a.port}/knowledge-unified.html',flush=True)
ThreadingHTTPServer((a.bind,a.port),partial(SimpleHTTPRequestHandler,directory=str(Path(__file__).parent))).serve_forever()
