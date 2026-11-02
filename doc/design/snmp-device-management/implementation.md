# SNMP implementation record

Branch: `feat/snmp-device-management`; starting commit: `ed462382`.
The source design is [README.md](README.md). Runtime/API details are in
[collection.md](../../api/collection.md).

## Design review

The Center owns credentials, lifecycle, publication and status. Each collector polls
the stock HTTP Provider protocol; no sync daemon, Docker socket or inbound collector
HTTP service is introduced. Web authentication, object/group permissions and dedicated
configuration/writer identities stay separate. Public exported initialization signatures
and commercial Target hooks remain compatible.

The design's persisted-draft wording conflicted with its device/Target/publication
atomicity requirement. This implementation keeps the transaction atomic and retains
failed input in the browser, avoiding an unpublishable registered Target. Native Target
mutations direct managed objects to collection management; force cannot bypass this.

The protocol gate found that stock v0.5.18 logs communities on reload. The controlled
patch removes config-valued reload/debug log arguments. Its binary keeps the same
protocol and SNMP behavior. Source pin and verification are reproducible via the
included build script. Production collector replacement remains a deployment operation.

The active Targets route currently resolves through `frontend/src/pages/hosts/entry.tsx`
and `pages/hosts/pages/List`, rather than the design's estimated `pages/targets` list.
The browser gate caught the inactive initial placement; the active list, filters,
statistics and shared collection drawers are now wired together. The legacy list
also retains the additive integration for its existing consumers.

## Code review

Review uses fixed baseline `ed462382` and covers the full working-tree diff and
new files. Native Target pagination is retained for existing clients and copy-all,
while collection API pages and status query batches remain bounded at 50. It checked Center/CenterApi Target reads, native mutation hooks, shared
credential transactions, row-lock ordering, unique constraints, empty/error Provider
responses, stale/future/revision health, soft-deletion filtering and secret logging.

Review found and fixed an empty-group `NOT IN (NULL)` visibility edge case, reset of
clock warnings and waiting windows when a configuration revision changes, cache
statistics for soft-deleted Targets, and preservation of unchanged CMDB associations
after asset deletion. Disabled-device edits retain the original withdrawal pointer
and retired task's interval, so shortening the desired interval cannot shorten the
observation window or leave an unchanged empty publication permanently pending.
Focused regressions cover each. Permission tests use both native
route middleware and object-level queries. No exported initialization signatures changed.

## Automated evidence

- Original binary: `v0.5.18-3af14ebe568108c22ea7ee78d1c64becd03f1076`, SHA-256
  `ff8e472d2d3ba6a90716aca4ea1dfed447d892acc7e09be4deddc32f46390b61`.
- Patched binary: suffix `provider-redact1`, SHA-256
  `a3dc33d493de85a705a0b08c9eba465bb9b7ea274a195d6d1fdc6b600590e2d3`.
- Both binaries passed actual loopback SNMP + HTTP Provider cases: local/remote
  coexistence, scalar/table/up identity labels, additions, content-key changes,
  the unchanged-key counterexample, partial/last deletion, 401/403/503, empty/bad
  responses, connection reset, initialization failure, endpoint outage, cold restart
  and recovery. The stock log contained the synthetic community; the patched log did not.
- SQLite behavior tests cover transactional rollback, concurrent creates, optimistic
  versions, idempotency, group isolation, secret storage, token rotation, probes,
  native Target mutation guards, revision-specific health and datasource failure.
- Actual PostgreSQL and MySQL tests passed Gorm and SQL initialization, repeated
  migration, independent-Store concurrency, optimistic update conflicts and encrypted
  snapshots larger than MySQL TEXT. An initial MySQL unique-index migration failure
  was reproduced and fixed by using explicit unique constraints.
- Router tests passed web authentication/operation/object checks, configuration gates,
  bare Provider responses, forged request fields and decrypt-error failure protection.

`make verify-all` passed: 60 Python checks, 2718 Go tests/subtests, 212 frontend suites
with 1846 tests, Go build/vet, the 755-rule action gate, and the full integration i18n
gate. The default Go run skipped 50 optional external/service tests; these are not
passes. The collection-specific actual MySQL/PostgreSQL gate was rerun separately
after the withdrawal schema change and passed all four initialization paths.
Pinned Categraf protocol tests were also run separately. Frontend warning output
from existing tests remains visible in the logs.

Automated Chromium checks passed anonymous rejection, bad-password rejection,
successful login, dashboard/rule route reloads and expired-session recovery. The
SNMP browser run uses a synthetic UDP fixture, a private collector writer and the
patched actual Categraf binary against this preview's PostgreSQL and the shared test
Prometheus. Its Provider poll is one second and sampling interval is 15 seconds;
it does not prove the design's Dev01 10/30-second timing target.

The complete UI lifecycle passed on build `20261025T090736Z-a76c20f2`: collector
registration and one-time token, credential creation, successful collector-side probe
without a Target, immediate registration after save, fresh CPU 37.5% / memory 25%,
all five chart datasets, 32-bit interface fallback and continued Linux system metrics.
Conflicting edits retained input; renaming retained the configuration revision;
disable/enable observed withdrawal and a new revision; native force deletion was
rejected; soft deletion hid the default row; probe cleanup and device withdrawal
completed, and metadata audit contained no community. This full run had no browser
page errors. An earlier partial run captured an unlocated `App is not defined`
exception; the full rerun with stack capture and the separate login/route gate did
not reproduce it.

Visual inspection found that G2's default tick text drops the time component. The
formatter now uses the raw epoch retained in the tick ID, with a regression for this
exact date-only text case. The final preview is rebuilt with this correction; its
manifest and the private `collection-e2e/final-smoke.json` identify the delivered
build and the final screenshot check, independently of the earlier lifecycle build.
Runtime evidence is under the private preview instance directory, outside Git.

## Acceptance

Use the branch's isolated preview and private credentials. Add an SNMP fixture via
Targets, check immediate registration, wait for a current-revision sample and inspect
CPU/memory/volume/interface charts. Exercise edits, disable/enable, deletion, old samples,
probe cancellation and read-only users. Existing Agent rows keep their original behavior.
Do not run failure cases against Dev01 or change the system clock during verification.

The development slot is `https://n9e-dev4.zhiteng.win`, instance
`6b8729690e336daf`; credentials are in its private `credentials.json`. Open
Infrastructure → Hosts → Collection settings to inspect the retained fixture records.
The test devices are soft-deleted and their collectors stopped after withdrawal;
historical charts remain available from Pending withdrawals. The agent's Chromium
checks are engineering evidence. Human acceptance is pending; no human PR checkbox,
stable release, push, or Dev01 migration has been performed.

## Remaining deployment work

No stable instance or existing collector is changed by development. The Dev01 cutover
requires the design's controlled backup, patched binary rollout, exact collection-entry
routing, writer registration and maintenance window; verify Linux continuity and the
host time source there. This is not implied by a green worktree build. Capacity at the
1000-device/8-MiB limits has not been load-tested. Non-Chinese additional UI locales
currently use the complete English fallback dictionary.

## 2026-10-27 add-device error regression

Saving and probing an address outside the collector policy returned HTTP 400 with
`address is outside collector policy`, but both actions displayed only the generic
collection-data failure. Silent Umi requests reject plain objects carrying `message`;
the collection helper previously accepted only native `Error` instances. It now
preserves a nonempty string message from either shape and retains the fallback for
unknown errors. Regression cases cover HTTP 400/403/409/503 and form validation.

Collector address authorization remains explicit. The local collector originally
allowed only Dev01's `/32`; its policy was expanded to the requested LAN subnet
`192.168.0.0/24` so one collector can monitor multiple SNMP agents without individual
policy changes. Device credentials remain separate. Error display does not bypass
policy validation. Running-instance
checks use the branch's [LAN testing policy](../../worktree-preview.md#snmp-分支本地测试约定).
Private reproduction and onboarding evidence is retained in the preview instance's
`diagnostics/add-device-error/` directory.

## 2026-10-27 unrestricted device addresses

The revised requirement removes collector network/port allowlists and address/endpoint
deduplication. Addresses require IPv4 syntax only. The collector UI no longer asks
for allowlists; legacy API fields remain accepted without enforcing them. Repeated
addresses reserve independent Target identities atomically while existing devices
and independent Agents retain their identities. Explicit custom identifiers remain
unique; idempotency and group/credential permissions are unchanged.

Endpoint uniqueness was removed from the Gorm model and all three dialects' SQL.
An idempotent startup migration drops existing endpoint constraints before Gorm
schema synchronization. Regression coverage includes repeated addresses, updates,
independent deletion, Agent coexistence, probes outside old ranges, concurrent
creation and upgrades preserving existing records on SQLite, PostgreSQL and MySQL.
Running-instance acceptance uses the LAN endpoint and disabled test records, so
validation does not add polling tasks to arbitrary addresses.


### 2026-10-27 可编辑模板与 SNMP 勾选

- 根因：设备列表通过 `getCheckboxProps` 显式禁用了所有 managed SNMP 行。
  LAN Playwright 复现断言 `toBeEnabled` 失败，原始复选框有 disabled 属性。
- 历史实现（已被下方原生组件方案替换）：不可变版本表承载采集定义，曾提供独立入口及组件页签。
  保存时验证数字 OID、字段、指标名和受控 TOML 形状，服务端仍负责凭据和目标渲染。
- 模板发布与设备生效分开：设备绑定具体版本，新版本不隐式改写正在采集的配置。
  完整快照记录模板版本，历史回滚同时还原模板版本；旧快照按 Windows v1 兼容。
- 模板必需指标参与状态判断，详情图表补齐自定义指标；SNMP 批量动作独立调用设备生命周期。
- 此历史实现无新增数据库列；原生组件方案另增 profile_state。真实 MySQL/PostgreSQL 的 Gorm/SQL 初始化路径均验证版本发布、
  并发冲突、编译和回滚。浏览器及最终构建证据记录在实例私有 diagnostics/snmp-templates。

- 页面回归补充：Ant Design Dropdown 会覆盖子 Button 的 `disabled`。只禁用子按钮
  无法阻止 Agent 菜单；需在 Dropdown 上同时设置选择包含 SNMP 时的禁用条件。
  LAN 浏览器断言原先显示 enabled，修复后从真实入口重新验证。

## Native component adoption (2026-10-27)

Design review: the previous parallel template catalog duplicated Nightingale's
component model. The replacement uses native four-part DB resources, seeded once
from standard integration files. Collection instructions carry the numeric-OID
TOML contract; a selected component becomes an immutable runtime snapshot.
Device ownership of independent native boards/rules is stored in one dialect-aware
column. Native rule caches and CenterApi paths already serve the alert engine.
This avoids a second alert evaluator or dashboard format.

Default CPU/memory/disk alerts: >90% for 300 seconds; interruption: failed SNMP or
3-minute uptime absence, sustained 60 seconds. Notifications are configured on
independent rule instances. Ordinary edits preserve device customizations;
explicit template reapplication resets them and retires prior instances.

Acceptance coverage includes native four-tab editing, Windows/Linux selection,
transaction rollback on invalid resources, revision-scoped queries, per-device
collection/metric overrides, preservation of native edits, pause/resume/deletion,
credential rotation, both server SQL dialects and LAN browser verification.
Automated checks, browser evidence and human acceptance are recorded separately.
