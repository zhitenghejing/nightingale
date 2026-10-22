#!/usr/bin/env bash
set -euo pipefail
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
existing="$(git -C "$repository_root" config --get core.hooksPath || true)"
if [[ -n "$existing" && "$existing" != ".githooks" ]]; then
  echo "Existing core.hooksPath=$existing; left unchanged. Run make verify before push."
  exit 0
fi
git -C "$repository_root" config --local core.hooksPath .githooks
echo "Configured repository Git hooks from .githooks"
