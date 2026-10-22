# Frontend source provenance

- Repository: https://github.com/n9e/fe
- Release: `v9.2.0`
- Commit: `58e8e242e88d834282bbeb8da9a5dd8d38100d17`
- Source archive: https://codeload.github.com/n9e/fe/tar.gz/58e8e242e88d834282bbeb8da9a5dd8d38100d17
- Archive SHA256: `42cbd598ac5cb6bcf857ee65a6982dd530a687bfaaf8620991925a6118815102`
- Imported: 2026-10-21

The source files are maintained directly in this repository. Preserve LICENSE,
copyright headers and the notices in source subdirectories when updating upstream.
Commercial `src/plus` is not part of the public snapshot or this import.

Excluded upstream automation: `.github`, `.husky`, `.claude`, `.codex`, `.cursor`,
`.agents`, root `AGENTS.md`/`CLAUDE.md`, environment files and registry configuration.
This repository's development rules, hooks and CI apply instead. The original
README is kept as `UPSTREAM-README.md`; current commands are in README.md.

Local changes are reviewed as part of this repository:

- Node 24.22.0 and pnpm 10.32.1 replace upstream's Node/npm setup.
- Convert the upstream npm lockfile with `pnpm import`, pin original direct
  dependency versions and explicitly declare dependencies imported by source
  instead of relying on npm's flattened install. Maintain only pnpm-lock.yaml.
- Use the public npm registry consistently. Approve esbuild/sharp install scripts
  explicitly; optional SWC/watcher and core-js scripts are disabled.
- Integrate production assets with the preview manager and the Go statik bundle;
  builds include source, dependency and toolchain identities.
- Adapt regression tests and build compatibility as recorded in the migration
  validation document.
- Apply a declaration-only Ant Design patch for Segmented's obsolete React
  event keys. The installed runtime implementation is unchanged.
- Fix missing `options` handling in legacy Table-to-TableNG conversion, using
  its existing failing tests as regression coverage.
- Adapt Jest's ESM transform to pnpm paths, replace private Plus test imports
  with a public contract fixture, update the schema-migration expectation and
  retain QueryForm's separately tested first-render initialization exception.

The old committed statik predates this source version and could not be traced to
an exact source commit. This import is a frontend upgrade, not a reconstruction
of that old bundle. See ../doc/frontend-source-integration-feasibility.md.
