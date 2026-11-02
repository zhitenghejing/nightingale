# Template-driven collection management

The Center owns the `monitor_*` records. Edge and alert processes retain the
existing Target service API; they do not decrypt collection credentials or serve
Provider snapshots. The UI stays under Infrastructure → Targets.

## Deployment

`Center.Collection.Enable` defaults to false. Set `KeyFile` to a private (`0600`)
JSON AES-256 keyring before enabling it:

```json
{"active":"key-v1","keys":{"key-v1":"BASE64_ENCODED_32_RANDOM_BYTES"}}
```

Generate random key material locally, retain it with the controlled database backup,
and never commit it. To rotate encryption, retain the previous key entries and select
a new active key. Existing encrypted publications still need their original key.
Credentials, snapshots and temporary probe TOML are AES-GCM sealed with object-bound
associated data. Missing keys and authentication failures never produce empty snapshots.

Also set `Center.Collection.ProviderEnable=true` and `HTTP.APIForAgent.Enable=true`
to expose the exact `GET /v1/n9e/collection/configs` route. `APIForService` can remain
disabled. Expose that path through the deployment's authenticated HTTPS collection
entry only; the public worktree preview gateway continues blocking service/agent routes.
The development port 30311 is an external deployment adapter, not an application default.

Every collector selects one dedicated username already configured in
`HTTP.APIForAgent.BasicAuth`. That username can be registered to only one collector and cannot also be a service-API account.
Its writer password remains in deployment configuration. The separately generated
configuration token is displayed once, hashed in the DB, and used with the collector
UUID as the Provider BasicAuth username. Token rotation retains the previous token
for five minutes; it does not stop an unreachable collector's already running tasks.
Managed samples are sent to their managing Center at `/prometheus/v1/write` (the proxy endpoint delegates to the same
validated path). Other ingestion formats cannot submit managed collection labels.
Unmanaged Linux samples keep their existing ingestion behavior.

After validating collector, device, mode, address (for ping) and current revision,
the Center adds the reserved `device_name` label from `MonitorDevice.Name` to all
accepted managed SNMP, MySQL and ICMP samples. It replaces any incoming value;
custom device labels and SNMP tag fields cannot set this name. This common ingest
step also applies to old template bindings without republishing collector tasks.
A metadata-only rename affects subsequent samples without a collector reload;
historical samples retain their original label set. Query history using stable
`ident` or `collection_device_id`. Authorized native Agent ICMP uses its list name
(`ident`); native Agent CPU/memory ingestion remains unchanged.

Use the fixed Categraf source commit `3af14ebe568108c22ea7ee78d1c64becd03f1076`
(v0.5.18) with the supplied [Provider logging patch](../../third_party/categraf/v0.5.18-provider-redaction.patch).
The stock binary logs SNMP communities during reload even with `print_configs=false`.
Build without changing an existing collector checkout:

```bash
python3 scripts/build-collection-categraf.py --source /path/to/categraf-git --output /private/path/categraf
N9E_TEST_CATEGRAF_BINARY=/private/path/categraf N9E_TEST_CATEGRAF_REDACTED=1 \
  go test ./pkg/collection -run TestCategrafHTTPProvider -v -count=1
```

Keep Categraf `print_configs=false` and debug mode off. Preserve local providers and
existing writers when adding the HTTP provider described in the
[design](../design/snmp-device-management/README.md#71-categraf-启动配置).
The patch and build script do not replace or restart a deployed collector.

## Web API

All paths below have prefix `/api/n9e/collection`. They use native authenticated
users, `/collection/read`, `/collection/write`, `/collection/delete` operations,
and business-group read/write checks. Collector registration, policy edits, token
rotation, deletion, and whole-collector rollback additionally require an administrator.
Single-object reads enforce the same group checks as list reads.

| Path | Methods | Behavior |
| --- | --- | --- |
| `/features` | GET | Feature flag for the Targets UI, available when disabled |
| `/collectors` | GET, POST | Scoped list; register collector and return its token once |
| `/collectors/:id` | PUT, DELETE | Versioned policy edit or reference-checked deletion |
| `/collectors/:id/rotate` | POST | Versioned configuration-token rotation |
| `/templates` | GET, POST | Read published SNMP template versions; administrator publishes a new immutable version |
| `/credentials` | GET, POST | Scoped secret-free list; encrypted credential creation |
| `/credentials/:id` | PUT, DELETE | Versioned update or reference-checked deletion |
| `/credentials/:id/references` | GET | Affected-device count before shared credential changes |
| `/devices` | GET, POST | Page up to 50 devices; atomic registration and publication |
| `/devices/:id` | GET, PUT, DELETE | Details, versioned edits, or soft deletion and withdrawal |
| `/devices/:id/enable` | POST | Enable; also explicitly retry an enabled device with a new revision |
| `/devices/:id/disable` | POST | Withdraw while preserving Target, audit and historical metrics |
| `/devices/:id/metrics` | GET | Current-revision CPU, memory, volumes and interface-rate charts |
| `/probes` | POST | Publish a temporary collector-side SNMP/MySQL probe; creates no Target |
| `/probes/:id` | GET, DELETE | Inspect or cancel; cleanup remains visible until observed |
| `/publications` | GET | Secret-free, authorized publication history |
| `/audits` | GET | Scoped metadata differences; optional `object_id` filter |
| `/publications/:id/rollback` | POST | Administrator restores historical device settings with new revisions |

Device creation requires an `Idempotency-Key` of 8–128 characters. Repeating the
same normalized request returns the same object; changing the request under that
key returns 409. Full edits and state actions require `record_version`; conflicts
return 409. DELETE and enable/disable bodies are `{"record_version":1}`.

Device addresses require valid IPv4 or IPv6 syntax only; collector network/port allowlists
and endpoint deduplication are not enforced. Multiple records may share an address,
port and protocol within the same group and collector. An empty or IP-valued `ident`
first tries the address; if already reserved, a `snmp-<device UUID>` identity is
allocated atomically. Explicit custom idents remain unique. Existing idents are
immutable and independent Agent Targets are never taken over. Query a specific
record by `collection_device_id`; the SNMP input also supplies `snmp_target`. MySQL bindings use the same device ownership labels.
Allowed intervals are 15, 30, 60 and 120 seconds; SNMP timeouts are 1–10 seconds, MySQL timeouts 1–60 seconds; retries 0–3.
Credential ownership and group permissions are rechecked in the transaction.
Legacy collector `networks`/`ports` fields are accepted for compatibility but ignored
for endpoint eligibility. Custom labels cannot replace identity, index or storage-type tags.
Raw Windows storage descriptions are excluded from the template; volume labels use
SNMP indices, so malformed legacy encodings cannot corrupt a Remote Write request.

Writes lock group, credential and collector rows before publishing. Transactions use
READ COMMITTED on MySQL/PostgreSQL, with database unique constraints for idents
and idempotency keys; endpoint columns are no longer unique. Metadata-only name edits retain the configuration
revision. The device, Target ownership, encrypted publication and current pointer
commit together. On publication failure the transaction rolls back and the browser
keeps the form input; there is no separately persisted draft object.

Native Target edit/delete APIs reject managed objects, including force deletion.
Use the collection details for coordinated changes. Default Target lists, filtered
lists, statistics and CenterApi cache reads exclude soft-deleted managed Targets;
the retained physical Target row reserves its ident. Neither CMDB assets nor
historical time series are deleted. Group/collector/credential deletion rejects references.

Probe limits are two outstanding per user, ten per collector/group, and five new
probes per user per minute. Probes expire after 90 seconds. Cancellation, expiry or
an observed result publishes removal; cleanup requires a current client version
and 90 seconds without additional probe samples. A disconnected collector keeps
`cleanup_pending`. Wrong credentials and packet loss are not assigned invented causes.

## Provider and health

A successful Provider response is bare JSON, never a web `{dat,err}` envelope:

```json
{"version":"NONEMPTY_PUBLICATION_UUID","configs":{}}
```

That response is an authoritative withdrawal only after a successful authenticated
read/decrypt of a published empty set. Each nonempty `configs.snmp` entry is keyed by
SHA-256 of its exact UTF-8 TOML bytes. A request carrying the current version gets
200 with only `version`; unknown/history versions get the complete current snapshot.
401, 403 and 503 errors contain only an `error` key. All responses use `no-store`.

The device's `collection` field separates configuration progress from health. Serving
or seeing a client version cannot establish health. The existing Target API retains its pagination and copy-all compatibility. Status
queries batch up to 50 devices by datasource and cache results for ten seconds; only matching UUID,
collector, revision and `managed` samples qualify. Explicit timestamps determine
freshness (`max(3 × interval, StaleSeconds)`). Query failures remain `query_error`,
future samples remain clock errors, and missing values stay null. Zero-size storage
is excluded. CPU cores and storage indices are preserved. Interface charts prefer
64-bit counters per index and fall back to 32-bit counters.

Rejected samples from previous revisions/collectors still extend withdrawal
observation. The absence of accepted samples is not proof a remote task stopped.
The withdrawal window retains the retired task's interval even when a disabled
device's desired parameters are subsequently edited.
This version provides observation, not a lease or a forced stop. Reassignment requires
an already withdrawn device and a completed observation window. Automatic failover,
configuration disk caches, reliable plugin acknowledgments, background automatic
retry, and hard-stop guarantees are not provided. Use an explicit enable request to
retry a repaired configuration.

## Schema and verification

Initialization and explicit upgrades are in `doc/sql/collection.{mysql,postgres,sqlite}.sql`
and `docker/migratesql/collection.*.sql`. MySQL/SQLite full initialization files and
the MySQL aggregate migration include these tables. Gorm migration uses the same
constraints and unbounded text (LONGTEXT on MySQL). Startup migration removes legacy
endpoint unique constraints before AutoMigrate on MySQL, PostgreSQL and SQLite,
preserving device records, credentials, revisions and existing Target identities.

```bash
go test ./pkg/collection ./center/router ./models ./pushgw/router -count=1
# Administrative DSNs create/drop only uniquely named disposable test databases:
N9E_TEST_MYSQL_DSN=... N9E_TEST_POSTGRES_DSN=... \
  go test ./pkg/collection -run TestCollectionSQLDialects -v -count=1
make verify
```

For a worktree preview, create private `collection.json` containing `{"enable":true}`
in its instance directory. Refresh retains `collection-keys.json`; destroy removes
that instance's private key with its data. A private `collector.json` and the existing
host-owned shared-Prometheus write opt-in are also required for real collection.
Stable previews reject this development override. Never replace stable credentials
or migrate Dev01 merely to run fixture tests.

## Recovery

Restore a selected publication through the administrator rollback endpoint, then check
current-revision samples again. A rollback is a new publication with new device content
keys; it does not reuse an envelope version to try to force a reload. Deleted objects
must be resolved explicitly, and a rollback never resurrects probes.

Before turning the feature off or destroying its preview, withdraw managed tasks and
observe cleanup while the configuration endpoint is still available. Turning the flag
off or revoking its token cannot stop an isolated collector. If the configuration API
cannot be recovered, stop the remote provider on the collector host before restoring
local SNMP files. Restore database and keyring together when recovering a backup.

## Native four-part SNMP components (legacy selection compatibility)

This section describes compatibility for historical component selection and
`refresh_template` callers. The current UI, full draft model, generic form,
notification presets and upgrade behavior are specified in the catalog section
below; new clients use immutable catalog release IDs.

**Integration center → Template center** contains **Windows_SNMP** and
**Linux_SNMP** components with the existing Instructions, Metrics, Dashboards
and Alert rules tabs. There is no separate SNMP template manager or tab.
The native component, metric and payload editors own these records and their
existing `/components/*` and `/builtin-metrics/*` permissions apply.

`integrations/<component>` uses the standard markdown/metrics/dashboards/alerts
file formats. A single fenced `snmp-collection` TOML block in the instructions
identifies a device-selectable component. On first initialization the complete
pack is seeded transactionally into native DB tables with author `template`.
Subsequent starts preserve native edits and resource deletions. A native custom
component with this block and all three other resources is selectable too.

The TOML block accepts numeric OIDs, `field`, `table`, nested `table.field` and
`required_metrics`. `snmp_uptime` is injected and required. Scalar names become
`snmp_<name>`; numeric table fields become `snmp_<table>_<field>`; `is_tag` adds a
label. Endpoints, credentials and managed ownership labels come from the device.
Windows SNMP Service and Linux Net-SNMP must expose HOST-RESOURCES-MIB/IF-MIB.
Unsupported OIDs require an agent view or template adjustment; numeric syntax
alone does not establish device support.

`GET /api/n9e/collection/profiles` returns available components with
`component_id`, `name`, `config`, `metrics`, `dashboard_count` and `alert_count`.
Device POST/PUT accepts `template_component_id`. The device response includes
`profile` with the saved component, collection TOML, metric documentation and
independent native `boards`/`rules` IDs. Historical devices may keep their legacy
`template_version_id`; the original immutable version remains supported.

Selection snapshots all four parts in an immutable `monitor_template_version`.
The same transaction creates native board/board_payload/board_busigroup and
alert_rule records and publishes collection configuration. Native rule caches,
CenterApi reads and the existing alert engine process these rules normally.
Every PromQL vector selector is restricted to device ID, collector, managed mode
and current revision. Native board/rule query scopes follow revision changes,
credential rotation and rollback. The device detail renders its native board and
links to native dashboard/rule editors. It displays its own metric documentation with valid PromQL expressions bound
to the device revision, so copied queries retain the device scope.

Default rules are enabled: CPU average, physical memory and each fixed disk
above 90% for 300 seconds; SNMP failure or absence of uptime samples for three
minutes, sustained for 60 seconds. Adoption clears notification recipients,
pipelines and callbacks. Users configure notification routing on device rules.
Rules create native alert events without automatically choosing a notification
channel.

Device-only overrides use optional `customization: { config, metrics, dashboards, alerts }` in POST/
PUT; the editor exposes these as advanced TOML and metric JSON. Native instances
remain independently editable. Ordinary saves preserve the saved pack and native
customizations even when the public component changes. `refresh_template: true`
with a component selection explicitly reapplies the latest public resources,
resets local collection/metric overrides, retires prior boards/rules and creates
new instances. Old rules are disabled, old boards hidden, alert history retained.
Stopping/deleting a device suspends its current rules. Re-enabling restores their
previous enabled/disabled states. `profile_state` uses dialect-aware document
storage and is included in gorm plus all initialization/migration schemas.

SNMP batch actions apply components explicitly and reset customizations, or
perform enable/disable/delete. Devices are updated independently; failed rows
remain selected with individual errors. Agent-only actions are disabled when
SNMP devices are selected. AI task selections still require Agent devices.

The historical `/collection/templates` version endpoints remain compatible for
existing callers and runtime history; the UI catalog is the native template
center. Existing devices are not silently rebound to new packs or given new
alerts until a pack is selected explicitly.


## 分类模板与完整监控包

`/components` 的白色卡片只显示图标、名称。悬停铅笔编辑名称、图标、三级分类和启停；卡片本体进入完整监控包编辑器。卡片/列表切换、分类全部展开/折叠保留筛选状态。图标沿用原生图库、URL/Base64、本地 PNG/JPG/SVG/WebP，原图上限 2 MiB。模板中心没有新增设备入口。

编辑器的五部分共用一份草稿：采集方式与兼容性、设备表单、采集与指标说明、仪表盘、告警规则与通知。采集说明、指标、仪表盘和规则都包含在 `definition.pack`；通知预设位于 `definition.notify`。发布只消费这份保存的完整值，原生资源库的并行编辑不会混入发布。原生组件 ID/ident 和源语言文件保持独立。未登记组件只导入一次；旧版不完整草稿在首次读取时补齐一次，之后不重新抓取原生资源。旧发布版本保持原始字节和绑定关系。

以下接口均需要用户认证。全局写入需要管理员及对应原生组件权限；目录读沿用模板中心原有登录可读范围。乐观锁冲突返回 409。

| 方法 | `/api/n9e/collection` 下的路径 | 内容 |
|---|---|---|
| GET | `/catalog` | 轻量分类、元数据、发布状态、表单 Schema；不返回大型资源正文 |
| GET | `/catalog/:component_id` | 单个完整草稿和发布包 |
| POST | `/catalog` | `{name,category_id,logo?,base_component_id?}`；空白新建或独立复制完整草稿，默认未发布 |
| PUT | `/catalog/:component_id/metadata` | `{name,logo,category_id,disabled,record_version}`，仅基本信息 |
| PUT | `/catalog/:component_id/draft` | `{definition,record_version}`，不修改发布版本或设备 |
| PUT | `/catalog/:component_id/publish` | 原子校验并发布整个监控包，递增发布版本 |
| DELETE | `/catalog/:component_id` | 目录墓碑；存在设备引用时拒绝删除 |
| PUT | `/categories` 或 `/categories/:id` | `{name,parent_id,sort,disabled,record_version}`；三级、无循环、同级名称唯一 |
| DELETE | `/categories/:id` | 先移出模板并删除子分类 |
| PUT | `/categories/:id/move` | `{parent_id,before_id,record_version}`；保持层级，事务内移动并重排目标同级目录 |
| PUT | `/catalog/:component_id/category` | `{category_id,record_version}`；仅移动模板位置，必须选择第三级型号 |
| GET | `/devices/:id/template-diff?version_id=...` | 旧包、当前实例、新包之间的更新字段和冲突 |

目录的三级含义固定为“类型 → 品牌 → 型号”，如“操作系统 → 微软 → Windows Server 2019”“服务器 → 戴尔 → R640”。通用软件/协议包使用明确的通用型号，不假定已有精确版本或硬件适配。初始化包含服务器、数据库、操作系统、安全设备、网络设备、存储等类型，其余软件和云服务类型继续保留。型号下可以有多个采集模板。

根目录加号添加类型；类型旁加号添加品牌；品牌旁加号添加型号；型号旁加号添加同级型号，不生成第四级。每一级旁均可编辑名称/属性或删除，已有子目录/模板时拒绝直接删除并提示先移出。保留展开、折叠、选择、搜索与列表/卡片切换。

目录通过拖拽调整同级顺序，或拖入另一个上级目录（例如跨品牌移动型号、跨类型移动整个品牌）。后台锁定目录并验证深度、同级重名和版本；不允许改变类型/品牌/型号的层级。模板卡片、列表行均可拖到型号目录；只更新位置及版本锁，不把客户端旧名称、图标或草稿一并覆盖。只读用户不显示写操作或拖拽入口，服务端仍要求管理员及原生组件权限。失败时保留原位置并提示重试。

首次升级仅整理仍在原始位置、且整条目录路径未经用户编辑的默认模板；保留自建/改动目录和主动移动过的模板。一次性完成标记保存在现有采集审计记录中，后续读取或重启不会重建已删除目录，不重复覆盖分类。清理无引用的未改动旧目录。此过程不修改设备、凭据、采集发布、仪表盘、告警规则、模板正文或已有不可变发布版本。新发布的分类快照带 `taxonomy:"type_brand_model"`，型号取第三级；旧发布保留原有解析，已有设备不会因菜单整理改变型号或模板绑定。无需新增 SQL 字段。

分类禁用阻止新发布/新绑定，不中断已有设备。发布版本保存分类路径快照，后续改名/移动不改变历史路径。

### 声明式设备表单

`definition` 包括 `adapter, execution, compatibility, config, fields, pack, notification, notify`。字段包括 `key,label,type,required,default,options,help,secret,group,min,max,when`。控件支持 text/password/credential/number/switch/select/multiselect/ip/url/endpoints。条件为 `when:[{key,values:[...]}]`，全部满足才显示；依赖只能引用前面的非秘密字段，排除循环。隐藏字段不校验、不入库、不下发。密码/凭据禁止默认值。

设备的名称、IP、ident、业务组、采集器、周期和资产关联是通用配置；模板参数在 `parameters` 中。`address/port/protocol/credential_id` 绑定既有类型化输入，其他参数按 Schema 校验。SNMP 社区字段和数据库密码由服务端写入专属加密凭据；设备 `parameters` 只存非秘密参数，读取只提供 `profile.secret_fields` 的已设置状态。编辑时密码留空保留同一模板族/业务组内的原值；改变设备类型/业务组后不得沿用旧秘密。

已有的 `credential_id` SNMP 调用保留兼容。新客户端直接提交例如：

```json
{"protocol":"mysql","port":3306,"parameters":{"username":"monitor","password":"<entered privately>","tls":"off"}}
```

该片段还需常规设备字段、当前 `template_version_id`、版本锁和幂等键。秘密不会出现在读取响应、审计详情或请求日志中。

### 设备三步流程与资源覆盖

设备选择器展示完整目录，不按 `published_version_id` 过滤分类或模板。`template_selection` 是前端目录选择键（组件 ID 或设备当前快照），不提交给设备写入 API；仅已发布且可执行的版本才提交为 `template_version_id`。因此多个未发布模板不会因为版本 ID 都为空而相互混淆。停用模板/分类仍可查看，但不能新绑定；历史绑定保持原来的编辑规则。

目录摘要和详情均返回 `onboarding:{state,ready,execution,issues}`，在剥离大型资源之前计算缺项。未发布模板可以在三级选择器内选中，查看设备表单草稿、原采集说明和资源清单；不可执行的模板明确阻止测试、下一步和保存，不套用默认 SNMP。原生模板的精确空白旧草稿会按已有采集说明一次性初始化部分连接字段和执行位置；不替换作者配置，不发布版本、不修改设备或下发配置。不明确的接入类型仍提示补齐表单，不能用统一账号密码表单冒充适配。

新增/编辑设备在设备列表内打开：①类型、品牌、型号三级分类与采集参数；②本设备的仪表盘、告警和通知；③脱敏确认。单方案自动匹配，多方案追加模板选择。返回保留输入，改变上级清空旧模板参数与秘密，通用对象信息保留。不强制测试连接；保存成功也不代表采集正常。

保存事务写入绑定、非秘密参数、加密凭据、资源快照、独立原生仪表盘/规则和本地加密下发版本；真实网络采集在事务外由采集器执行。只更改设备显示名称不产生采集修订。同 IP 绑定保持独立。

`customization:{config,metrics,dashboards,alerts}` 为本设备覆盖，不修改全局包。已有独立仪表盘/规则仍可从设备详情进入夜莺原生编辑器。普通保存保持设备修改；显式选择同源新版本时，对旧包/设备现值/新包逐字段合并，只更新未覆盖字段。新增模板资源创建新实例。数组结构或同字段双向修改产生冲突；模板删除的设备资源保留并列为冲突。差异接口供第二步展示；确认保留冲突值时提交 `upgrade_resolution:"keep_device"`，未确认返回 409。`reset_resources:true` 是显式重建，旧规则停用、旧仪表盘隐藏、事件历史保留。旧 `refresh_template` 命令仍兼容。

新包规则先暂停，完整的当前修订必需指标从数据源查询成功后，后台 worker 才恢复期望的启停状态。Provider 已读取、仅收到 up、历史修订样本、查询失败都不足以启用规则。暂停/删除设备暂停规则，独立停用状态保留。

### 设备总表投影

`GET /api/n9e/targets` 及 hostfilter 在已授权、已分页的 Target 上附加非持久化 `inventory`：`name,device_type,device_model,template_path,method,collection_ok,last_sample_at,reachability,reachability_at,current_alerts`。设备类型取绑定版本的第一级分类，型号取具体产品与版本，以文字显示；通用模板未声明的系统版本不推测。历史 Windows 基础包仍识别为操作系统和 Windows。Agent 优先显示上报的系统名称与版本，无详细信息时显示 OS。

采集方式显示明确的 `snmp`、`ipmi`、`agent`、`mysql` 等单一协议，使用与可达性相同的标签框。`collection_ok` 为 true 时标签变绿，否则为黑字中性框。托管设备须对应协议的当前修订指标完整、实时且健康；原生 Agent 须在时序库有最近 3 分钟的实际指标，不以心跳或 ping 代替采集成功。查询在 `timestamp` 前将指标名保留到临时标签，避免多个同维度指标去掉名称后发生标签冲突。当前告警沿用未恢复事件及其权限；零数量不显示，有对应级别告警才显示数量，查询失败仍显示暂不可用。

可达性使用授权 Prometheus 数据源近 3 分钟 `ping_result_code` 的实际样本：0 可达、1 不可达、2 探测异常，无新样本为未知。托管样本还须匹配当前设备、采集器、修订和数据源，避免重复 IP 或旧任务串数据。清单含设备名称、设备类型、设备型号、IP、采集方式、可达性、当前告警、业务组和设备标签；列宽可拖拽，右侧编辑与删除固定，沿用原编辑页与权限。

点击所有设备名称统一打开详情抽屉，不要求写权限。顶部采用相同的信息表，包含设备类型、设备型号、采集方式、可达性、采集器和真实最近样本时间；下方固定依次为“仪表盘、当前告警、指标说明、告警规则”。绑定模板的设备保留原有独立仪表盘；历史 SNMP 和原生 Agent 使用实际 CPU、内存、磁盘和网络时序曲线，无数据或查询失败分别显示，不生成模拟值。详情中的编辑按钮和列表右侧编辑按钮都进入原有配置编辑器；原生 Agent 编辑保留原主机元信息操作。只读列表隐藏详情内的写操作。

三个新增 Center UI 只读接口均经原生登录与用户中间件：`GET /api/n9e/targets/device-detail`、`device-alerts`、`device-metrics`。以 `device_id` 或精确 `ident` 定位，服务端校验 Target 业务组可见性；托管设备还要求采集管理读取权限及设备业务组权限。响应不含团体字、密码、完整告警配置或通知目的地，不创建或接管设备，不修改发布配置。它们不供 edge/alert 使用，不改变其原有接口。

`device-detail` 返回 `{target,device?,collector_name,metrics,rules}`。`inventory.last_sample_at` 是实际采集样本时间，不以 Agent 心跳代替。指标说明保留模板定义；历史 SNMP 和原生 Agent 展示其实际查询表达式。规则页仅展示显式模板绑定以及当前事件关联的授权规则，并标明来源，不推测全局 PromQL 规则的静态适用范围。

`device-alerts` 返回 `{list,total}`，参数 `p` 默认 1，`limit` 默认 20、最大 100。事件与列表计数共用精确 ident、设备拥有的无 ident 中断规则、业务组和数据源过滤，重复 IP 不混用；仅返回规则名、级别、触发时间和值等展示字段。`device-metrics` 只供原生 Agent，查询授权 Prometheus 的近一小时 CPU、内存、磁盘和网络数据，保留维度，部分失败通过 `metric_errors` 标明；托管设备继续使用原有设备指标接口及修订隔离。

### 模板默认 ICMP 基础采集

所有目录模板和发布包都继承 `reachability:"icmp"` 的基础采集；旧草稿和不可变发布字节保持原样。目录 API 返回有效默认值，模板编辑页说明探测方式和指标。尚不能执行的模板仍保持原接入状态，不因默认 ICMP 变为可自动接入。

每台启用的托管设备在其绑定采集器上独立下发 `configs.ping`：每个设备周期探测 3 个 ICMP 包，包间隔 0.2 秒，超时 3 秒，原生模式，支持 IPv4 和 IPv6。任务带 `ident,target,collection_device_id,collection_collector_id,collection_revision,collection_mode`；`ping_result_code`、`ping_percent_packet_loss`、`ping_average_response_ms` 等经原有 Remote Write 写入同一时序库，查询例子为 `ping_result_code{ident="设备标识",collection_mode="managed"}`。网络/权限失败也保留真实结果码，不阻止 SNMP 或数据库采集。

启动时按内容摘要为现有启用设备补齐任务；保持原协议配置、设备修订、凭据、资源和模板绑定。停用/删除撤回协议和 ping；回滚从恢复的设备参数重新生成 ping。写入校验采集器身份、设备归属、地址与当前修订；ping 不更新协议采集回执或触发协议探针成功。

原生 Agent 主机也自动下发 ICMP：优先选用 `agent_ident` 对应的已启用采集器，其他主机选用同业务组内授权采集器（按 ID 稳定选择一个），避免重复探测。无对应采集器且无业务组授权时保持未知。原生任务周期 30 秒，使用 `collection_mode="agent"`，带主机 ident、当前地址、采集器和由这些字段计算的修订，不创建托管设备或修改 Agent 的本地 CPU、内存等配置。查询示例：`ping_result_code{ident="categraf01",collection_mode="agent"}`。结果、丢包率和延迟均进入原时序库。

采集器认证轮询时自动同步新主机、地址、业务组或删除变化，内容不变不产生新版本；旧地址、旧归属和已删除主机的 ping 样本在写入时拒绝，不能重新注册主机。页面只使用当前归属、修订及数据源的样本。历史回滚重新生成当前原生主机的 ping，不恢复旧地址。删除仅有原生 ICMP 任务的采集器时先发布撤回版本，待采集器确认且观察 90 秒后重试删除，避免撤销认证后遗留旧任务。

### 告警通知

`notify:{mode,message,rule_ids}` 支持 `events_only`、`inherit`（所选发布包预设）和 `rules`（本设备指定原生通知规则）。校验规则启用状态、接收用户组授权、媒介和消息模板；无有效映射明确显示仅事件。模板重新发布不改已有设备通知映射。

消息支持 `{{设备名称}}`、`{{设备分类}}`、`{{规则名称}}`、`{{当前值}}`、`{{发生时间}}`、`{{告警级别}}`，最多 1024 字符。写入独立规则附加信息，并通过原生 RuleNote 引用供现有媒介消息模板呈现；自定义媒介模板需要保留 RuleNote 或引用 `AnnotationsJSON.collection_message`。不复制媒介密码/Token，不增加第二套投递引擎。通知配置不等于通知已投递，浏览器验收不会发送真实通知。

### 当前执行支持与迁移边界

- Windows_SNMP / Linux_SNMP：SNMP v2c、类型化 OID 配置、原生独立资源。
- Windows_SNMP_Extended（Windows SNMP 增强）：独立新增的 Windows 包，位于“操作系统 → 微软 → Windows 通用”，不替换原模板或设备绑定。包含 48 项指标说明、36 个仪表盘面板，以及 CPU/内存/磁盘 >90% 持续 5 分钟和 SNMP 中断共 4 条默认规则。配置使用数值 OID；进程、网卡说明、路由为可选扩展，TCP 明细三张表初始停用。实际兼容性取决于 Windows SNMP 代理。
- MySQL_84_Remote：数据库 → MySQL → 8.4 LTS。已用 MySQL 8.4.11 与固定 Categraf 验证完整 Provider/Remote Write/指标契约。`metrics_pass` 声明精确指标名称，`required_metrics` 必须是其子集；类型化采集开关控制 process list、schema/table size、replication 等项。地址、密码、归属标签由设备参数绑定。
- 其他数据库、SNMP v3、Redfish、Redis、HTTP 等可以定义完整草稿和条件表单，但执行编译器/能力验证未完成的类型不允许发布为自动接入包。硬件型号与固件的真实兼容性仍需逐项验收，不能用表单测试代替。

保留原生仪表盘/规则导入到业务组的入口供手动接入资料包使用。当前目录在原 91 项盘点基础上增加上述 MySQL 版本包；原盘点和原型不因这次修正而改写。后续适配按[迁移清单](../design/template-driven-device-management/catalog-migration.md)推进。

SNMP 类型化配置的 `disabled_tables` 是表名列表。编译时仅省略这些表；未知名、重复名和包含必需指标的表均拒绝。停用可选表不删除指标说明或可选面板，缺失的数据保持无数据。MAC 标签仅开放 `conversion="hwaddr"`；任意其他转换或数值字段上的转换均拒绝。新字段在空值时不参与序列化，保持旧发布版本的摘要及已下发配置字节不变。

SQL 的十张 Center-only 表在 gorm、`doc/sql/collection.*.sql`、对应迁移及适用总脚本中同步维护。本次增加设备参数/通知文档、分类排序/禁用，并扩展 IPv6 地址宽度；不改变 edge/alert 的原生读取接口。完整包写入上限 8 MiB，发布快照上限 2 MiB，采集器配置上限沿用部署设置。

## Reusable template icon library

The metadata dialog's **Select logo** picker retains the built-in artwork and adds
an **Add icon material (+)** button below it. Administrators can upload PNG, JPEG,
SVG or WebP (up to 2 MiB; raster dimensions up to 4096 pixels per side), then select
the uploaded icon and save the template metadata. Importing alone does not modify
a template or publish collection configuration. Icons are reusable across templates
and survive page refreshes and deployment refreshes in the Center database.

* `GET /api/n9e/collection/icons`: authenticated list of `{id,name,url,created_at}`.
* `POST /api/n9e/collection/icons`: administrator plus `/components/put`; JSON
  `{name,data_url}`. Validates actual image content and deduplicates by content hash.
* `GET /api/n9e/collection/icons/:id/content`: anonymous immutable artwork, like
  existing built-in icon URLs; restrictive CSP sandbox and `nosniff` headers.

Custom images live in `monitor_icon` (dialect-aware large text for base64 content),
not the frontend build directory. Include this table in database backups. No edge
or alert read path is introduced. The additive initialization/migration SQL is in
`doc/sql/collection.{mysql,postgres,sqlite}.sql` and `docker/migratesql/collection.*.sql`.
Built-in source icons remain in `integrations/<Component>/icon/<file>` (or
`Center.BuiltinIntegrationsDir`) and are served at
`/api/n9e/integrations/icon/<Component>/<file>`.
