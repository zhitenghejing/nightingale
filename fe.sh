#!/bin/sh
# Build only the frontend source in this checkout; never download a release UI.
set -eu
cd "$(dirname "$0")"
cp -f ./docker/initsql/a-n9e.sql n9e.sql
exec python3 -B scripts/frontend.py embed
