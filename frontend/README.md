# Nightingale frontend

This directory is the frontend source maintained together with the Go backend.
See [UPSTREAM.md](UPSTREAM.md) for the imported source commit and license notices.

Use the exact Node 24 version in `.node-version` and pnpm version in
`package.json`. From the repository root:

```sh
make frontend-install
make frontend-build
make frontend-verify
make preview-refresh
```

Production assets are generated in `frontend/pub/`. `make all` embeds these
assets in the Go binary. A plain Go build uses the committed statik cache.
Do not install with npm or download a release UI as a build fallback.

See [frontend development](../doc/frontend-development.md) for preview, E2E,
build identity and release requirements. The original upstream instructions are
retained in [UPSTREAM-README.md](UPSTREAM-README.md) for reference only.
