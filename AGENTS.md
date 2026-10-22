# Agent development rules

Read [CLAUDE.md](CLAUDE.md) for the architecture and module map, then
[the workflow](doc/agent-development-workflow.md). These rules apply to all agents;
keep shared project architecture in CLAUDE.md rather than duplicating it here.

- Start with Git status, branch and recent commits. Preserve unrelated edits.
  One task/worktree has one primary writer; additional reviewers are read-only.
- Follow the call chain before editing. Preserve exported entry points consumed
  by n9e-plus. Reads consumed by edge/alert need both DB and CenterApi paths.
- Schema changes update gorm and applicable initialization/migration SQL,
  including PostgreSQL where applicable. SQLite tests do not prove other dialects.
- Follow [frontend development](doc/frontend-development.md) for Node 24 + pnpm,
  frontend verification and paired assets. Plain Go builds use committed statik;
  use preview or `make all` for current in-tree UI. Never download a release UI as fallback.
- Run focused behavior tests during implementation, then `make verify`. Use
  `make verify-all` for vet and both full integration gates. Report existing
  failures and skipped tests; never present them as passes.
- Use preview for running-instance verification, refresh after edits, and check
  source/build identity plus local/public probes before handing off an address.
- `n9e.cubbber.dev` is the separate stable/main instance. Worktrees only use
  `n9e-dev0.cubbber.dev` through `n9e-dev9.cubbber.dev`. Never publish a branch
  through the stable hostname. Stable commands require the configured clean main checkout.
- Each preview owns its SQLite, TSDB, runtime Skill files, logs and credentials.
  Never connect a disposable preview to production data. Basic previews disable
  Skill script execution; full previews require actual isolation and pinned inputs.
- Deliver reasons and results, actual checks, preview build ID and acceptance
  steps, plus limitations. Distinguish automated checks, agent browser checks,
  human acceptance and release. The PR template requires human verification;
  agents cannot tick that checkbox on behalf of a person.
- Commit only task files. Follow user authorization for push, PR, merge and
  release. Stop retains data; destroy deletes the instance's disposable data and
  releases its slot. Preserve needed evidence/data before archive teardown.
