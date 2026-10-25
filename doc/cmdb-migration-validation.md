# CMDB and report migration

> Historical validation of the 2026-10-22 migration. Inspection and incident report
> features have since been removed, including their pages, APIs, conversion worker
> and `Center.CMDB.ReportsDir` configuration. Existing archive files are retained
> on disk; they are no longer served. Report checks below describe the migration
> checkpoint only. Current browser coverage retains CMDB and rack scenarios.

## Scope and source

Target baseline: `jealous-bumblebee`, `a20cd71f`. Source: `violent-dragonfly`
commits `4c1157a3` and `ac1f2bbe`, plus its material working tree as inspected on
2026-10-22 (27 modified tracked files and 103 untracked material/tool files).
The source worktree and its database/files remain untouched.

The source frontend was an overlay onto `/home/zt/Project/nightingale-fe`.
Only `RackViewport.tsx`, its behavior tests, `catalog.ts` and the rack stylesheet
were imported from that checkout's untracked `src/pages/rackViews/` directory.
CMDB and reports now live directly in `frontend/src/pages/`; their tests use the
repository's pinned Node 24/pnpm/Jest toolchain. No external checkout is needed.
The material catalog, original photos, approved vector panels and creation history
are preserved with their source URLs and SHA-256 receipts. Packaging now reads
the in-tree location and validates approved vector checksums. Desktop export remains
an explicit optional command, and was not run during migration.

## Design review

- Keep inventory logic in `pkg/cmdb`; mount its authenticated dispatch under the
  existing center router. Native auth/user middleware supplies the verified user,
  including disabled-user rejection. Do not change exported center/router entry
  point signatures used by n9e-plus; the optional Router.CMDB field is additive.
- Preserve the original global inventory ACL and per-owner report policy. A menu
  entry does not grant data access. No business-group row isolation is claimed.
  CMDB reads are center-only; edge/alert do not consume them.
- Use the existing center DB, including each preview's private PostgreSQL database.
  Preserve UUIDs, payloads and transaction/version behavior. Quote placement column
  identifiers through GORM clauses; use dialect-specific large text types. The
  write-lock row must exist before any inventory mutation.
- Maintain all applicable initialization/upgrade SQL, with a standalone schema
  snapshot for each dialect. Startup creates missing CMDB tables and validates
  columns/unique indexes; it does not infer destructive changes to existing tables.
- Let center own the report worker and stop it before releasing its directory lock.
  The archive defaults to `data/reports`; previews explicitly use `run/reports`.
  Stop/refresh retain data; destroy removes only that preview's data.
- Word conversion keeps the existing fail-closed bubblewrap isolation and limits.
  It clears inherited environment and exposes font/LibreOffice/loader configuration rather than
  the host's entire `/etc`. Missing conversion support leaves the original available.

## Validation and acceptance

Engineering checks executed on 2026-10-22:

| Check | Actual result |
| --- | --- |
| `make verify` | Passed; Go 2634 pass / 0 fail / 44 skip at that checkpoint; frontend 206 suites / 1798 tests passed |
| `make verify-all` | Passed; Go 2635 pass / 0 fail / 44 skip after adding native identity coverage; vet and both full integration gates passed; frontend 206 suites / 1798 tests passed |
| Real PostgreSQL and preview DB tests | Passed with disposable test databases, for GORM/SQL schemas, placement/version concurrency, bootstrap isolation and SQLite import |
| Real isolated DOCX conversion | Passed after explicitly exposing LibreOffice's own configuration; original bytes unchanged |
| Preview configuration tests | 13 passed, including private report-directory configuration |
| Approved material packaging | 10 models / 20 panels validated and packaged; no desktop export |

The default gate's 44 skips include unconfigured external services and opt-in Word
conversion; they are not passes. PostgreSQL and Word were separately enabled and
tested as above. MySQL remains unverified in a real disposable MySQL environment.
The Vite build emits existing Plus-placeholder export warnings; production build
and tests complete successfully. Final source/build identity and browser outcomes
are verified separately during handoff, after regenerating committed statik.

Self-review against `a20cd71f` included new/untracked module files as well as the
tracked diff. It resolved the missing external frontend dependencies, PostgreSQL
reserved identifiers/large text, missing placement-lock detection, report directory
collisions, native disabled-user checks, middleware ordering, and sandbox runtime
configuration. API errors pass through `pkg/i18nx`; imported Chinese text remains
the fallback where a translation is absent.

Focused checks cover persistence, atomic imports, placement bounds/overlaps,
optimistic edits, delete/restore, ACL denial, report ownership and file validation.
The migration adds SQL-initialized SQLite coverage, missing-lock rejection, exclusive
report-writer ownership, and real PostgreSQL GORM/SQL initialization plus concurrent
placement/version tests. PostgreSQL bootstrap/import tests cover the preview DB path.
The isolated Word conversion integration exercises an actual DOCX and checks that
the original remains byte-identical. External MySQL checks require their explicitly
configured disposable database and must be reported as skipped when absent.

The production browser scenario in `frontend/migration-e2e/cmdb.test.ts` covers native
login, asset creation, atomic import rejection, real panel loading, rack dragging,
stale-write rejection, delete/restore, report upload/download/refresh, a real Standard
user's ACL denial/grant, report ownership denial and disabled-session rejection.
The existing migration scenarios retain bad-password, route-refresh and expired-token
coverage. `make frontend-e2e` first verifies the current preview's build/source identity.
Generated Playwright reports are local ignored evidence; the final handoff records
the actual build ID and executed check results.

Human acceptance remains separate: open the preview preparation URL, sign in using
its private credentials file, inspect the CMDB ledger and rack fronts/rears, move an
asset, and upload/preview an inspection or incident report. Agent checks do not mark
the PR template's human verification checkbox.

The first public browser run timed out transferring the main JavaScript bundle;
the unchanged baseline bundle is 19,200,949 bytes, versus 19,271,388 bytes after
migration (about 0.37% larger). A public curl probe also timed out on the baseline
bundle. The same production rack page became ready through loopback in about four
seconds. Public health/source probes pass; public cold-load E2E is not claimed as a
pass. `make frontend-e2e-local` retains the identity and public-readiness preflight
while explicitly selecting the same gateway over loopback for business validation.

Local browser validation additionally found that the page-header tab filter only
recognized Nightingale operation permissions, hiding CMDB/rack tabs despite their
separate inventory ACL. The tab regression fails with the original filter and passes
with the migration fix. The report upload locator also needed to account for the
button's accessible icon name. These fixes are included before final browser retest.

The native-user scenario exposed an existing PostgreSQL initialization defect:
explicit seed IDs left the users/team/business-group sequences at their starting
values, so the first new user failed with `users_pkey` duplication. A real PostgreSQL
initialization regression reproduces this failure. The initializer and PostgreSQL
initialization SQL now advance those three sequences after inserting seeds, before
the database is served. No running production database is repaired or reset by this task.

Report preview/reload is tested with full Chromium (`channel: chromium`). The default
headless shell's PDF navigation limitation caused reload to hang; the same original,
archive, ACL and disabled-session scenario passes with full Chromium. This is a browser
test capability requirement, not a fallback to unisolated document conversion.

## Operational limits and data cutover

No original MySQL inventory or persistent reports are copied by this source migration.
An authorized cutover must back up both source stores, map ACL/report owner user IDs,
preserve asset/report UUIDs and versions, compare counts and original SHA-256 values,
and rehearse restoration before traffic moves. Never connect preview to production data.

Reports use a single writer and filesystem index, with no deletion/retention API.
Production needs an explicit persistent volume and off-machine backups. Multi-replica
report writes need a separate storage service/object-store design. Inventory filtering
is client-side; monitor/alarm switches remain management flags without actual Target,
collector or rule integration. Localization of the imported feature body text remains
Chinese; menu labels are Chinese for Chinese locales and English otherwise.

This task does not push, merge, release, or change the stable instance.
