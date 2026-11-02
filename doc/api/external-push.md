# 外部告警推送：第一阶段设计与运行约定

更新：媒介显示名称已统一为“1.0平台推送”，现有初始化流程会预置一条默认关闭、可被通知规则选择的保存记录。真实地址、编码和字段来源已补充确认，CMDB关联键及真实密钥仍待配置，本轮不执行真实推送，详见[预置媒介说明](platform10-preset.md)。以下第一阶段说明保留为历史记录。

更新：已按业务提供的转换规则新增center独立文件签名，默认路径可覆盖；仍仅本地模拟，Java编码需对照向量。见[密钥部署与签名约定](external-push-signing.md)。此前“sign未知”的记录为当时状态，未自动改写旧任务。

状态：第一阶段已实现；验证记录见下文（2026-10-27，基线 ed462382）。本阶段仅本地模拟，不支持启用生产协议。调查依据见 [告警链路调查](../告警链路与外部平台推送接入调查.md)。

业务确认更新：故障alarmType=`problem`，恢复alarmType=`ok`。其他协议不因此视为已确认；本轮样例对照与类型假设见 [脱敏样例适配验证](external-push-samples.md)。

最新字段配置与关联说明见 [字段映射与1.0页面对照](external-push-field-mapping.md)。

## 设计决定

- 复用通知规则、渠道配置页面、现有屏蔽/过滤/恢复开关。新增 `external_push` 渠道，不修改其他 Provider。专用分支在 `HandleEventNotify` 的根规则与匹配订阅完成流水线及过滤后封闭路由批次，再由 `RegisterDispatch` 登记任务（直接规则入口 `HandleEventWithNotifyRule` 也封闭自己的批次），不展开本地接收人、不直接发网络请求。渠道创建默认关闭；不种入启用配置。
- center 拥有任务、attempt、人工审计及 stream 锁，只有 center 启动 worker。edge/alert 通过 BasicAuth CenterApi 幂等登记，绝不向目的端发请求。普通渠道测试发送不支持本类型，避免绕过账本。
- 第一阶段配置只允许 `mode=simulation`、数字回环 IP 的 HTTP 地址（必须带端口，无 DNS/代理/重定向）。生产模式拒绝发送，没有生产 URL 默认值。alarmType采用业务已确认的problem/ok；映射、标签类型/字符串和测试签名仍为明确的模拟假设；不实现猜测的生产签名算法。
- 渠道配置保存地址、逻辑目的端键、超时、已确认的problem/ok类型值及显式测试签名、时区；通知规则 Params 承载 `company_id`、`device_type`（二者分别可改用`company_id_label`、`device_type_label`指定原始告警标签）、`level_map`（JSON 字符串）、`device_name_label`、`device_tags`（字符串，或`device_tags_label`指定标签字符串）和 `repeat_policy`。缺失/异常保存为本地校验失败，不能用空值悄悄发送。重复策略只能显式选择 `all_dispatches`（模拟每次提醒）或 `review_repeats`（保留重复提醒并等待人工处置），不默认把重复提醒永久丢弃。
- 在既有入队快照处生成派发 UUID，首次故障生成 incident UUID、每个实际故障快照递增 incident_sequence；恢复继承 UUID、序列递增。当前/历史事件保存这些字段以跨引擎重启继承。不使用 event.Id/hash/秒时间充当可靠身份。旧活跃事件、外来事件没有合法身份时保留失败任务，禁止猜测合并；HTTP 接入重传的业务幂等仍需上游提供同一派发身份。
- task 唯一键是 SHA256(逻辑目的端键 + 派发 UUID)，与本地规则/订阅/用户数无关；这只是本地索引，不是外部 sign。同一派发命中不同配置时校验快照摘要，冲突拒绝且记审计，不能静默覆盖。每个正常重复提醒有新的派发 UUID，分别保留任务。逻辑目的端键须在指向同一外部业务目标的多条配置中一致。
- stream 锁以目的端+incident 为粒度，DB 条件更新领取租约；跨进程 worker 竞争只有一人领取。task、attempt、锁领取同事务。先保存 sending 和 attempt 再出网；成功读完且严格解析响应、HTTP 2xx、数字 status=200 才标 `accepted`（接口报告受理）。其余已发请求均保守标 `unknown`，绝不自动重投。禁用自动重定向。
- 源序列只表示全部快照的相对顺序，允许静默、规则过滤和渠道中途启用造成空洞；投递依赖只读取实际登记的任务。重复故障等待实际在前的待发送故障；恢复不要求未尝试提醒先成功，但须存在实际尝试且已受理或人工确认收到的故障。任何在前的已尝试未知故障仍阻塞。恢复先到保留 waiting_predecessor，不补造故障。不同 incident 可并发。重新故障必须新 incident，不从 hash 推断。
- 租约过期的 sending 转 unknown，包括发送前后进程崩溃；不重新派发。人工确认已收到/授权重发/取消未发送/保留人工处置需管理员 POST、非空理由及已观察的 task version，使用事务 CAS，并保存 actor、时间、旧新状态；GET/刷新无副作用。人工确认使用独立的 confirmed_received，不伪造 accepted 或改写原 unknown attempt；两种已收到状态均可解除前序阻塞且禁止重发。授权重发复用同 task，新增 attempt。
- 原始任务正文只供 worker；管理员详情提供 payload_preview 脱敏 JSON，sign 固定替换为 [REDACTED]，不返回原始 payload/snapshot；响应只存 HTTP/业务码、长度和摘要，不存可能回显的签名。该摘要与审计不依赖现有尽力型 notification_record 队列。登记成功只记 task ID 日志，不写普通通知“发送成功”记录；登记出错沿用通知失败记录。专用渠道的派发与发送结果应在任务页面核对，普通“已通知事件”列表不作为该渠道的受理账本。

## 可靠性边界与本阶段取舍

持久登记提交后，任务/attempt/人工操作可跨重启查询，多路由重入可幂等；这不等于事件生成到对方恰好一次。现有事件队列仍是内存，事件持久化后到异步通知规则筛选/首次登记前存在崩溃窗口；edge 到 center 登记暂时失败可重试同派发，但进程退出仍可能丢登记。本阶段不重构全局事件持久化事务、不自动用最新配置回放历史事件。失败记录日志并暴露通知失败，不声称绝不丢失。后续完整解决需在事件持久化事务保存可重放的路由/配置快照 outbox，而非靠新任务表推断可靠。

incident UUID 仅随当前事件快照持久化；首次事件在保存前崩溃无法继承，存量事件不伪造轮次。DB 事件持久化和派发顺序的既有竞态未全面重构；专用 worker 遇到缺身份或没有实际送过故障的恢复保守阻塞。每次登记保留 incident_sequence 的事实，但不要求序列连续，不补发被规则过滤的事件，不绕过本地通知开关。

故障已推送后若恢复被本地规则/静默/恢复开关过滤，对方可能保留未恢复故障。是否必须无条件同步恢复尚未决定，本阶段不改变现有开关。

无法从超时/连接中断/非 JSON/业务失败证明对方未落库。对方无查单/去重时，人工重发有重复风险，必须先核对（包括迟到可能）。本端租约不能阻止被操作系统长时间暂停后恢复的旧进程：人工重发前必须确认旧 worker 已退出且请求结束。系统不承诺端到端 exactly-once。

## 验证计划

本地模拟服务器断言 form round-trip（中文/特殊字符、标签字符串）、多规则订阅同派发幂等、正常重复独立、新轮次、恢复先到及前序 unknown 阻塞、保存后超时只一次、sending 重启不重投、HTTP/数字业务状态/截断/非 JSON/空体/重定向、CAS 人工操作审计、center/edge 登记、权限和旧渠道回归。数据库实际验证分别报告，不以 SQLite 代替 PostgreSQL/MySQL。执行 focused tests、make verify、前端检查、独立 code review、隔离 preview 与浏览器验收。

## 生产协议待确认

客户/总部/分部归属及设备/等级映射、原始传输格式及deviceTags类型/字符串格式、sign 算法与凭据、故障恢复配对/重复提醒业务、接口异常的真实保存行为、人员匹配及通知结果。下一阶段先取得可验证样例与测试账号，再设计生产 signer、目的端许可与联调；本阶段不调用 admin.ztservice.cn。

最终验收分别记：接口接收且保存；下游按既有规则完成业务消息与人员通知。`accepted` 不代表人员/短信/电话成功。

## 实施后的配置与查询

`external_push` 的创建预设为关闭，不写入启用的种子渠道。现有通知媒介页面选择“外部告警推送（模拟）”，在现有通知规则中引用它。该类型不选择本地人员和消息模板，也不提供绕开任务账本的普通“测试发送”。

渠道 `request_config.external_push_config` 示例（problem/ok已确认，其余为显式模拟假设，不能投入生产）：

```json
{
  "mode": "simulation",
  "endpoint": "http://127.0.0.1:18091/mock",
  "destination": "simulation-one-receiver",
  "timeout_ms": 1000,
  "problem_type": "problem",
  "recovery_type": "ok",
  "test_sign": "TEST_ONLY_NOT_A_PRODUCTION_SIGNATURE",
  "time_zone": "Asia/Shanghai"
}
```

规则 `notify_configs[].params` 示例：

```json
{
  "company_id": "测试客户 A",
  "device_type": "TEST_virtualization",
  "device_name_label": "ident",
  "device_tags": "['测试标签', '模拟业务等级']",
  "level_map": "{\"1\":\"TEST_严重\",\"2\":\"TEST_警告\",\"3\":\"TEST_提示\"}",
  "repeat_policy": "all_dispatches"
}
```

目的端键是本阶段显式配置契约，不是客户主数据。同一独立部署的总部、分部及不同设备可共用同一接口和逻辑目的端，分别由各次实际命中的规则提供业务参数；不再扫描全库要求不同事件映射一致。同一派发同一目的端命中矛盾参数时保存校验失败，不任意选第一条；封闭批次若同一地址使用不同目的端键，也阻止所有这些别名任务发送，不能改键绕过检查。跨独立重入批次修改目的端仍不能由当前约束完全防御，管理员须保持同一真实目标的键稳定，变更前关闭渠道并核对在途任务。

总部/分部通过哪些原协议字段区分尚待确认，不新增 branchId，不假定 companyId 是分部字段。模拟测试在 deviceTags 放 TEST_HEADQUARTERS/TEST_BRANCH 标记、使用不同测试设备类型，只证明发送端允许各规则合法参数，不证明1.0真实归属协议。

新建模拟配置使用已确认的problem/ok，页面只读显示；后端拒绝旧TEST类型。保留原配置字段以兼容存储结构，不自动改写或重投旧任务；旧配置需核对后新建模拟配置，原记录仍可查询。生产模式、地址和签名算法没有因此开放。

专用报文读取持久化历史中的原始派发事实，以主键定位并再次核对派发 UUID、轮次 UUID 和序列；主键仅是索引定位手段，不是幂等身份。不按无索引的 dispatch_id 扫描整张历史表。通知规则/订阅的 drop、匹配、恢复开关仍生效；它们对副本字段的改写不会重写专用报文中的历史事实。原始历史不存在或身份不一致时保存本地校验失败，不猜测身份，也不触发发送。

worker 每 500ms 扫描最多 100 条候选，按最后检查时间和 ID 轮转；等待任务不会永久饿死其他 incident。每个 center 启动两个 worker，stream 租约为 30 秒，请求超时显式限制为 1–10000ms。正常重启不立即清理仍有效租约；到期后发送中任务转结果不明。配置变更前先关闭渠道并核对在途任务；已经开始的请求无法撤回。渠道关闭/删除将尚未发送的任务保留为人工处置，不自动切到新地址。

界面入口：“通知媒介 → 外部推送任务（管理员）”，或 `/notification-channels/external-tasks`。查询只读，最新/更早页以 ID 游标每页最多 100 条；GET 不触发发送或重发。

| 状态 | 含义 |
| --- | --- |
| pending | 等待领取；仍须通过前序及配置一致性检查 |
| sending | 已在事务中记录 attempt 和租约，可能正发送或已发送 |
| accepted | HTTP 2xx、完整 JSON 对象、唯一数字 status 精确等于 200；页面显示“1.0接口报告接收成功”，不代表人员通知 |
| confirmed_received | 管理员核对后明确确认已收到；审计记录依据，原 attempt 结果不变；不等同于接口报告受理或人员通知完成 |
| validation_failed | 缺少协议参数、映射歧义、身份或配置异常；未发送 |
| unknown | 请求可能已被保存，包括超时、断连、解析/读取异常、业务失败、重定向、租约过期；不自动重发 |
| waiting_predecessor | 实际在前请求未确认，或恢复没有实际已收到故障；源序列空洞不阻塞、不补造故障 |
| not_delivered | 管理员明确取消从未尝试的任务并留痕；不是对方已收到 |
| manual | 重复策略未决定、渠道关闭或人工保留；等待明确处置 |

HTTP 4xx/5xx 或业务非 200 也保守视为结果不明，因为对方异常响应不保证未落库。数值比较不经过 float64，200.0 与 2e2 按精确数值接受；近似200、重复status键（包括相同值）、非数字、截断、空体、非JSON、尾随内容均不能记受理。响应摘要只保留状态码、长度及 SHA256，查询不返回原始签名或snapshot，管理员详情仅返回sign已隐藏的payload_preview。摘要哈希只用于本地核对，不是对方 sign。测试签名在模拟快照/载荷内持久保存，但不会进入查询响应。

接口：

- `POST /v1/n9e/external-push-tasks`：现有服务 BasicAuth，edge/alert 登记；参数为 `{"routes":[{"channel_id":1,"notify_rule_id":2,"params":{},"event":{}}]}` 的完整实际路由集合，响应为任务 ID 数组。center 重读启用配置、已保存规则及原始事件，edge 最多三轮重试**登记**，不会重试对外网络请求。该接口不在 preview 公网网关开放。
- `GET /api/n9e/external-push-tasks?state=unknown&before=123`：管理员查询，可选 channel_id；最多 100 条。
- `GET /api/n9e/external-push-tasks/:id`：管理员查询任务、attempt、人工/系统审计及payload_preview（实际保存JSON，sign已隐藏）。
- `POST /api/n9e/external-push-tasks/:id/actions`：管理员明确操作，`{"action":"confirm_received|authorize_resend|cancel_unsent|hold","reason":"核对依据","version":当前版本}`。空理由/旧版本拒绝。确认已收到必须确有请求尝试；已受理或人工确认已收到的任务不能授权重发。

普通刷新不会调用 actions；人工核对也不自动查询对方。授权发送/重发只把同一个任务改回 pending，worker 仍检查本地模拟许可、渠道状态和前序，不旁路安全检查。`hold` 不等于“对方已收到”。`cancel_unsent` 只允许从未尝试的 pending/waiting/manual/validation_failed，独立记为 not_delivered；不能取消 unknown/sending。恢复忽略尚未尝试或已取消的提醒，但仍须有实际已收到的故障锚点，任何已尝试未知故障不能靠取消绕过。

## 迁移、审查与验证记录

新增四张表：external_push_task（派发唯一键）、external_push_stream（跨 worker 租约）、external_push_attempt（每次请求）、external_push_operation（人工/系统审计）。当前和历史事件新增 dispatch_id、incident_id、incident_sequence；ID 列在启动时同步补齐，保留原有大表其他迁移流程。当前 center 使用 MigrateChecked 汇总必需身份/账本迁移错误并在启动服务前失败；身份列失败继续尝试其他同步模块，失败后不再启动异步事件表迁移。CheckSchema 再次验证必需结构。既有无关迁移保留原日志策略。原 void Migrate 导出签名兼容，但外部调用方须采用有返回错误的入口才能自行 fail-fast。渠道关闭也不能在缺身份列时正常运行，因为旧告警写入也会带新列；修复权限/空间等DDL原因并重启，确认迁移和schema检查通过后再启动。初始化 SQL 与升级 SQL 同步维护；升级文件为 `docker/migratesql/migrate.sql`、`external-push.postgres.sql`、`external-push.sqlite.sql`。不提供数据自动删除/清理任务，避免本阶段误删核对证据，容量与保留期为后续运维事项。

设计审查：采用局部渠道分支及 center 持久模块；明确没有消除源事件内存队列/异步派发到首次登记的窗口。代码审查使用基线 ed462382 与工作区 diff，两个只读 Agent 分别核对规范和本轮用户需求；发现并修复等待队列饥饿、异步映射冲突窗口，补对应回归；查询文案通过 i18nx；持久事实查询采用主键+UUID核对，避免全表扫历史。本轮已移除全库单映射限制；配置变更在途边界仍存在。交付前另只读复核人工确认独立状态及登记成功不写通用通知成功记录，相关回归同步。

### 第一阶段历史自动化结果（修复前）

以下为上一阶段证据，不能替代本轮验证；本轮修复、最新构建及证据以 [缺陷修复记录](external-push-fixes.md) 为准。

`make verify`：退出码 0；59 项预览脚本测试通过；Go 2725 项（包含子测试）通过、0 失败、48 跳过；前端 213 套件、1848 项通过。跳过项包括真实通知、外部数据源/归档及未配置 DSN 的数据库测试，不能视为这些集成已通过。完整逐项原因保留于本机 `/tmp/dirty-peacock-phase1-acceptance-verify.log`；外部推送的 PostgreSQL/MySQL 测试另外显式配置临时 DSN 执行并通过，不能用它代替其他模块跳过的数据库测试。前端构建和同仓 statik 打包通过，并用 `N9E_TEST_FRONTEND_DIR="$PWD/frontend/pub" go test ./front/statik -count=1` 比较内嵌资源。

| 用户验收场景 | 对应验证及结果 |
| --- | --- |
| 表单、中文/特殊字符、deviceTags 字符串 | `TestSimulationFormAndLifecycle` 对模拟接收端解析后的完整结构体做相等断言；通过 |
| 多规则/订阅副本/并发登记与多 worker | `TestDedicatedDispatchUsesOnePersistentTaskNotRecipients`、`TestConcurrentDuplicateRegistrationAndWorkers`；通过；不展开本地人员 |
| 故障/恢复/新轮次及正常重复提醒 | 生命周期测试、`TestDispatchAndIncidentIdentitySurviveSnapshots`；通过；复用相同 hash/秒时间仍区分新 incident |
| 先保存后超时、恢复等前序 | `TestAcceptedThenTimeoutUnknownAndManualCAS`；只发一次并 unknown，人工确认留痕后恢复才可发；通过 |
| sending 重启 | `TestRestartDoesNotResendSending` 实际关闭并重开本地 DB；租约到期只转 unknown；通过 |
| HTTP 200 业务失败/字符串状态、非 JSON、空体、截断、重定向等 | `TestResponsesNeverAssumeDownstreamSuccess`；不误标受理、不跟随重定向；通过 |
| 持久化和迁移 | SQLite 完整初始化与旧表升级通过；真实临时 PostgreSQL、MySQL 8 上 GORM/专用升级 SQL、唯一约束与并发 worker 通过；测试库和自建容器已删除。PG/MySQL 的完整 Docker 初始化脚本本轮只同步修改，未单独从头执行全部脚本 |
| center/edge 与旧渠道 | `TestEdgeRegistersAtCenterWithoutSending`、管理员权限与 CAS 测试及全仓回归通过；实际双进程 edge 部署未验证。真实短信/电话等按约束跳过 |
| 保守阻塞及配置冲突 | 100 条等待任务不饿死其他轮次、当时的全库配置共识发现保存配置冲突（该限制已在本轮移除）、等级 JSON 重复键拒绝；通过 |
| 界面 | Jest 验证默认关闭、测试签名无默认值、刷新只读、人工操作明确携带版本与理由、人工确认状态独立且不可重发/hold；本地浏览器补验登录、任务/attempt 展示、无已受理重发按钮、新渠道默认关闭且地址和签名为空 |

### 第一阶段历史隔离实例证据（修复前）

本地完整链路使用 `scripts/external-push-preview-smoke.py`，经普通登录后的既有渠道/规则 API 建立**仅回环模拟**配置，并通过只读测试 Prometheus 的 `vector(1)>0 → vector(0)>0 → vector(1)>0` 让真实告警引擎产生故障、恢复和新故障，不直接伪造任务。该阶段功能构建 `20261027T080734Z-d2804657` 再次实测新增 3 条 accepted、各 1 次 attempt、接收端共 3 次：前两条 incident 相同且序列 1/2，新故障 incident 不同且序列 1。测试后关闭自己的模拟渠道、通知规则和告警规则，保留任务证据。首次试验的 3 条记录也保留，共有两次独立模拟的 6 条受理记录。本段证据补写后仅刷新文档对应的源码标识，功能及前端内容没有再改变。

本地浏览器检查同一该阶段功能构建 `20261027T080734Z-d2804657`：任务查询及详情可见；刷新没有产生写请求；已受理无授权重发按钮；新渠道默认关闭、endpoint/test_sign 为空；页面运行错误 0。页面明确显示恢复过滤风险与“接口受理不等于人员通知”。本机证据 `/tmp/dirty-peacock-external-preview-evidence-final.json`（含完整源码指纹与任务 ID）、`/tmp/dirty-peacock-external-ui-final.log`、`/tmp/dirty-peacock-external-tasks.png`、`/tmp/dirty-peacock-external-channel.png`；凭据单独留在 preview 私有目录，不写入文档或截图。

隔离 preview 在槽位 5、本地 `http://127.0.0.1:30305` 与 LAN 探测通过；公网开发域名 TLS 探测 EOF/握手超时。`--local-only` 明确只用于本地诊断，保留公网失败事实，**不算公网交付通过**；未把标准 `make frontend-e2e-local` 公网就绪门禁改为通过。人工运行实例验收及生产联调仍待办。本轮没有启用生产模式，没有访问外部生产接收接口，没有验证或宣称下游人员、短信或电话通知。

复现：先 `make preview-refresh`，确认 sourceChanged/frontendChanged 均为 false、构建 ID 和探测一致；再执行 `python3 -B scripts/external-push-preview-smoke.py --output /tmp/external-push-evidence.json`。公网不可用时可显式加 `--local-only`，该结果只能用于本地验证。脚本会建立并关闭自己的模拟规则/渠道，保留任务和审计，不启用 service API。仅在本机私有目录读取随机登录凭据。

人工验收可从本地实例“通知媒介 → 外部推送任务（管理员）”查看保留的两次模拟受理记录及 attempt，再打开新建外部媒介核对默认关闭与模拟限制。不要启用已结束测试的回环地址（接收服务器已关闭），不要把 accepted 解释为外部真实平台已保存或已完成人员通知。需要重测时重新运行模拟脚本。

## 下一阶段联调步骤

1. 取得对方完整字段/映射/恢复/签名向量、重复提醒与恢复同步决策，并确认异常落库语义和核对入口。
2. 独立设计并审查生产模式、签名凭据/脱敏、目标许可及多映射规则。当前实现故意不允许把 endpoint 改成生产地址。
3. 用本地模拟继续验证已确认协议与故障注入，按需实现持久路由 outbox 消除登记前窗口，再迁移并验证中心/边缘部署。
4. 获得明确联调授权后，只对测试客户/测试人员请求。先核对原始入库条数和原文，再单独核对业务消息、人员匹配、短信/电话证据；接口受理不能代替后者。
5. 切换前关闭本地试验渠道、核对所有 unknown/sending，在途结束后明确本地与对方的通知责任；不得自动补发历史活跃告警或批量重投结果不明任务。
