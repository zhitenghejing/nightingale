# 告警知识处置闭环

本模块在 Center 内运行。`pkg/ops` 管理处置、证据、审批、任务与恢复验证；`pkg/knowledge` 管理私有来源、草稿、不可变正式版本和适用性。原生告警的触发与恢复规则保持独立。edge/alert 继续通过既有事件持久化路径提供事件，不读取本模块知识。

## 配置和运行边界

`etc/config.toml` 的 `[Center.Ops]`：

| 配置 | 默认 | 作用 |
| --- | --- | --- |
| DataDir | `data/ops` | 私有来源目录，随数据库一致备份；不得放在公开静态资源目录。 |
| ModelEnable | false | 允许使用平台已启用的默认模型。管理员应选择获准处理相应资料的部署。 |
| AllowExecution | false | 生产 Ibex 模板下发开关（只读准备提案仍可使用），同时要求 Ibex 已启用及用户具有作业权限。 |
| DemoEnable | false | 显式开启隔离场景的准备、故障注入、固定动作及测试探测。 |
| DemoURL | 空 | 仅允许显式回环 IP 的 HTTP 地址；禁止代理和重定向。 |

模型关闭时仍可人工处理及按步骤自动采集指标。后台每 5 秒扫描启用的业务组策略，两个 worker 推进持久运行；每轮最多 12 次工具调用、120 秒，租约 150 秒。崩溃最多恢复三次，人工接管使旧 fencing token 失效。模型运行失败保留原因，由人员再次发起或继续处理；不把失败推理自动重试为写操作。

来源仅接受 UTF-8 TXT/Markdown、PDF、DOCX，最大 16 MiB，私有权限 0700/0600，读取时校验大小和 SHA-256。档案导入将确切已保存文档版本的字节保存为本业务组私有副本，记录来源编号与摘要；后续档案编辑不改变副本。在线文档以结构化正文 TXT 保留，附件按原格式保存，不自动提取嵌入对象。PDF/DOCX 首版保留原件，辅助生成要求人工核对的文字摘录，不提供 OCR。

自动生成仅处理所选业务组的 1–5 份文本、合计 64 KB，使用已配置模型且不提供运维执行工具；结构化提交只返回草稿数据，不分发或执行模型返回的调用。输出只存草稿，验证日期清零、可执行动作清空，需人员补齐风险、允许操作与验证依据后发布。没有模型时按表单编写。

## 身份与授权

所有接口位于 `/api/n9e/ops`，要求原生登录。每次调用重新查询账号有效性和业务组权限。读取要求业务组 ro 或 rw；编辑、认领、确认、业务核验及关闭要求 rw。只读人员只能读取正式知识，不能看到未发布草稿内容或标题；来源按本组权限访问。数据库、文件和模型摘要均不向匿名访问开放。

后台 Agent 的身份、处置和运行由服务端绑定，不接受模型提供 user/group/case/target。只开放：`get_ops_context`、`search_ops_knowledge`、`select_ops_knowledge`、`collect_ops_metric`、`collect_ops_eval_logs`、`record_ops_step_result`、`propose_ops_action`、`run_ops_verification`。工具循环在分发内建工具前执行集合校验；不加载 Skill 或外部工具源。工具开始与结束审计以 call_id、run_id 对应，工具层持久化证据。模型摘要排除原始告警快照、作业脚本、附件和审计内容；指标返回限定目标的数据。

指标查询仅支持 Prometheus 类数据源，重新核验 Target 与业务组及数据源权限，PromQL 每个向量选择器强制相同 ident，拒绝 offset、@、标量伪造与跨对象匹配。查询最多 1 小时、10,000 样本，并查询底层 timestamp 校验新鲜度。自动日志目前为当前规则评估日志；应用日志、主机 Agent 诊断、CMDB 绑定没有通用作用域适配时需人工证据，能力清单明确返回不可用，禁止扩大到通用 SQL/HTTP/脚本工具。

## 主要接口

返回沿用 `{err:"",dat:...}`。错误同时提供 `{err:message,error:{code,message}}`，权限 403、缺失 404、并发/状态冲突 409、语义输入错误 422。JSON 请求上限 2 MiB；所有路径 ID 必须为正数。

| 方法与相对路径 | 输入 | 输出或结果 |
| --- | --- | --- |
| GET `/config` | 无 | 当前用户、可访问组及写权限、模型/执行/Demo 开关。 |
| GET/POST `/cases` | 查询 group_id/state；新建 group_id/title/context 或 event_id | 最近 200 条处置；新建/获取告警轮次对应处置。event_id 为历史事件 ID。 |
| GET `/cases/:id` | 无 | 当前处置，以及最近 200 条轮次/证据/步骤/动作/验证/运行/审计。 |
| POST `/cases/:id/assign` | revision, owner_id | 认领/转交，取消活跃运行并提升 fence。 |
| PUT `/cases/:id/context` | revision, context, wait_reason | 补充环境/版本等人工背景，背景改变提升 generation。保留服务端目标和标签。 |
| POST `/cases/:id/merge` | revision, source_id, source_revision, reason | 同组、同对象、同数据源处置合并；转移告警关联，原始证据留在来源处置。 |
| GET `/cases/:id/matches` | 无 | 正式版本、分数及适用条件满足/不满足/未知。 |
| POST `/cases/:id/knowledge` | revision, version_id, reason | 固定采用版本，换版需原因，提升 generation。Agent 不能更换已采用版本。 |
| POST `/cases/:id/collect` | query, window_seconds | 服务端查询并追加指标证据，错误/空/部分数据同样保留状态。 |
| POST `/cases/:id/collect-logs` | 无 | 当前规则的评估日志证据，截断标记 partial。 |
| POST `/cases/:id/evidence/manual` | note | 带操作者、时间的人工证据。 |
| POST `/cases/:id/steps` | key,state,note,evidence_ids | 当前知识步骤结果；completed/failed 需要同处置同 generation 证据，completed 拒绝错误证据，指标步骤须引用相同查询的证据；人工步骤仅人员登记；skipped 需要原因。 |
| POST `/cases/:id/actions` | key | 来自采用版本的固定操作提案及 UUID、目标、参数/模板和现场证据基线、有效期；新证据到达需重新提案，同操作旧提案转为 stale。 |
| POST `/cases/:id/actions/:action/decision` | accept | 人员拒绝或消费一次确认；过期、模板/背景变化停止下发。 |
| POST `/cases/:id/actions/:action/result` | success,note | 人员核实人工/未知/已提交动作结果。 |
| POST `/cases/:id/actions/:action/reconcile` | 无 | 通过 Ibex system_caller 查确切任务和目标结果，不再次下发。 |
| POST `/cases/:id/verify` | 无 | 服务端采集并计算非人工验证项。 |
| POST `/cases/:id/checks/:key` | passed,note | 仅人工检查项，需操作者确认及证据。 |
| POST `/cases/:id/close` | revision,kind,summary,followup | 关闭前验证所有必需项、证据、新鲜度、活动告警及未知动作；temporary 必需整改信息。 |
| POST `/cases/:id/feedback` | note | 形成知识草稿，记录原处置和采用版本，不自动发布。 |
| POST/DELETE `/cases/:id/runs` | POST mode=guided/agent | 入队/人工停止；数据库唯一约束保证每处置一个活跃运行。 |
| GET/PUT `/policies` | PUT group_id,revision,enabled,service_user_id,auto_investigate,rule_ids | 接入策略与健康；服务账号须具有组读取权限，规则限定同组。 |
| POST `/policies/:group/ack-gap` | note | 人员记录超出源保留期中断的核对结果后确认缺口。 |
| GET/POST `/knowledge` | GET group_id；POST group_id,title,spec | 查询/新增草稿。 |
| GET/PUT `/knowledge/:id` | PUT revision,title,spec | 读取/乐观更新草稿。 |
| POST `/knowledge/:id/publish` | revision | 检查来源、步骤、分支、风险、验证规则、验证日期后生成不可变版本。 |
| GET `/knowledge/:id/versions`；GET `/versions/:version` | 无 | 历史与确切版本。 |
| POST `/versions/:version/withdraw` | reason | 停用保留历史，后续步骤/执行/结束重新校验停用状态。 |
| GET/POST `/sources` | GET group_id；POST multipart group_id,file,description | 来源列表/私有保存。 |
| GET `/sources/:source/file` | 无 | 校验摘要后以 attachment/no-store 返回原件。 |
| POST `/sources/from-archive` | group_id,version_id | 固定档案文档版本私有副本。 |
| POST `/knowledge-draft` | group_id,source_ids | 生成未验证草稿，仅模型启用时可用。 |
| POST `/demo/setup`、`/demo/fault` | setup group_id,datasource_id；fault group_id,kind | 仅开启 Demo 的管理员；kind=storage/business/data/clear。 |

## 状态与恢复不变量

处置：pending → processing → verifying → closed。原生告警恢复只更新告警轮次，不自动关闭处置。IncidentID 对应原生异常轮次，DispatchID 为 inbox 幂等键；已恢复轮次不被迟到触发覆盖。下一轮生成新处置，可人工关联同对象告警。缺失旧事件身份拒绝自动关联，需手工建单。

历史采用稳定 ID 页和重叠时间窗，定期从保留历史重新对账，并独立分页全部当前事件。范围是已经持久化且仍在源保留期内的事件；原生进程内队列尚未落库的数据不在保证内。超保留期缺口保持告警，直到人工登记核对。

知识固定正式版本；环境硬条件未知或不符时不采用，test_only 强制 test 环境，停用/过复核日期不继续使用。换版、背景变化和合并提升 generation，原证据保持历史，不充当新验证。按步骤取证遵循显式 next/on_failure，未登记的人工步骤和循环分支停止等待人员；已登记的同 generation 人工结果允许继续。重复采用同版本保持幂等。采集完成不代表根因已确认。

操作：proposed → rejected / awaiting_manual / dispatching → submitted / succeeded / failed / unknown。审批先提交 dispatching，再调用执行端一次。Ibex 接收仅为 submitted，确切目标终态才为成功；超时或崩溃不自动重发。未知、下发中或未确认的结果阻止背景变更、合并和关闭。人工结果登记需有核实说明。固定 Demo 动作按 operation_key 幂等。

指标比较在完整窗口每个样本上执行，时间来自服务器及底层采样，缺值/警告/过期/观察窗口不足保持 pending。成功或失败的最新动作、同对象同规则更新一轮异常之后必须重新观察。指标范围查询使用 5 秒步长，非零窗口的新鲜度至少 5 秒，总样本数仍受限。非指标业务检查默认 300 秒新鲜度（可设置 fresh_seconds）。结束时再次检查最新 generation、版本、证据和新鲜度。关联告警仍活动时不能宣称恢复；取消不宣称恢复，仍不能遗留结果不明的动作。

## 隔离演练与验证

运行 `go run ./cmd/ops-demo --data <私有空目录> --ident <唯一标识>`，只监听显式 loopback。独立 Prometheus 每 5 秒抓取其 `/metrics`，配置成隔离预览内的专用数据源，不修改共享测试 Prometheus。Demo 的 8 MiB 是自身缓存配额，文件为逻辑大小，绝不填满宿主磁盘；测试数据账本及 operation_key 持久化在该目录。

场景主线及完整验证矩阵见 [开发方案](../design/alert-knowledge-operations/development-plan.md)、[验证方案](../design/alert-knowledge-operations/validation-plan.md)、[实际验证记录](../design/alert-knowledge-operations/implementation-verification.md)。脚本化模型端点 `/v1/chat/completions` 用于同一 HTTP 工具循环的确定性联调，不能证明真实模型能力。真实模型、真实 Ibex、医院数据和人工验收分别报告。

三种 SQL 由 `go run ./cmd/ops-schema` 生成于 `doc/sql/ops.*.sql`，同步 MySQL 初始化/迁移、SQLite 初始化和独立 PostgreSQL 迁移脚本；更改模型后必须同步并在实际三方言核验重复初始化/迁移、大正文、唯一约束和空活跃租约语义。

开发预览可在实例私有 `ops.json` 写入 `{"demoURL":"http://127.0.0.1:39708","modelEnable":true}` 后刷新，脚本固定关闭生产执行。Demo 服务和专用 Prometheus 的生命周期由测试操作者管理，停止/销毁预览前保存证据，之后停止关联服务；不清理主机共享数据库/Prometheus。来源与数据库需一致备份，首版不自动清理证据或历史知识。

## 本次处置方案（无适用知识的新故障）

本次方案独立保存于 `ops_plan`，不进入知识检索。`OpsCase.plan_id` 与 `knowledge_version_id` 互斥；步骤、动作及验证结果同时记录 `plan_id`，通过同一个处理依据入口读取知识或已确认方案。现有知识流程保持兼容，旧记录的新字段默认为 0。

- POST `/cases/:id/plans`：`revision,title,scope,rationale,evidence_ids,spec`。追加不可变草稿，返回方案版本。scope 明确本次目标与范围，rationale 分清事实、假设及未知事项；至少引用一条同处置、当前阶段的成功证据。spec 复用结构化步骤/操作/检查定义，必需有恢复检查和停止升级条件；禁止引用知识来源、规则编号和发布验证日期。草稿保存不改变当前处理依据。
- POST `/cases/:id/plans/:plan/decision`：`revision,accept,note`。仅业务组写权限的人员可确认或拒绝；Agent 无此工具。确认前重验内容摘要、现场条件、背景阶段及最新证据基线。成功后采用确切方案，清空知识引用、提升处置阶段并取消旧 Agent 运行。确认方案不等于批准任何操作，每项操作仍需单独确认。
- GET `/cases/:id`：返回 `plans`，包含历史版本、证据引用、SHA-256、提出人及人员决定。修订追加新版本，拒绝/确认后的版本不可重新决定。当前采用方案即使超出最近 200 条也保留在返回中。
- `propose_ops_plan`：受限 Agent 工具，`plan_json` 为上述请求的 JSON 字符串。只能在服务端绑定的处置/身份/运行范围内提出草稿，要求该用户有业务组写权限；不能确认、切换当前依据、扩大查询或执行范围。`get_ops_context` 包含 revision、方案状态及当前依据。

新证据到达后，未确认草稿必须修订；背景、合并或处理依据变化后，原方案的确认失效，需新版本确认。采用或换版使历史证据保留，但后续步骤与恢复需使用新阶段证据。有未知、下发中或待核实操作时，不能切换依据或背景。正式知识切换到方案需要显式人员确认；方案切回知识需要人员填写理由。

确认后的方案仍沿用已有 Target/数据源/作业权限、工具集合、调用预算、幂等下发及业务验证规则。人工方案可用于没有自动取数入口的报障；没有工具时由人员检查并保存证据，不生成任意 SQL/HTTP/命令执行能力。已恢复但根因未知时，可按真实情况选择临时恢复并填写后续负责人和任务。

“形成知识草稿”从采用方案复制步骤、风险及检查，附原处置/方案编号与内容摘要，清空验证和复核日期；不能直接发布，也不会把一次恢复自动认定为可复用知识。旧表升级由 Center AutoMigrate 完成；独立、仅运行一次的旧表 SQL 在 `docker/migratesql/ops-case-plan.{mysql,postgres,sqlite}.sql`，初始化 SQL 已同步。

## 处置可靠性修复（2026-11-12）

- 操作定义增加 JSON 字段 `required_steps: string[]`，只引用本处理依据中存在且不重复的步骤。空或未填写时保守要求全部步骤完成；分支操作应明确选择前置步骤。准备和批准均核查同处置、同阶段、同知识/方案的最新检查结果，必须 completed，跳过/失败不算完成。支持证据必须有效且最近 300 秒取得，指标底层采样必须在最近 60 秒；条件变化需重新检查并提案。既有知识、方案和操作的 JSON 保留，读取时执行新门禁，不重写已发布版本。不增加表字段或 SQL 迁移。
- 没有已采用依据且没有可适用正式知识时，后台走独立草稿阶段，不让模型自由猜测指标。优先使用告警快照的表达式（缺失时明确标记当前规则回退），从命名向量选择器中提取最多两个限定目标的诊断查询，各查询 60 秒。平台上下文证据明确标注其不证明根因或恢复；空、错误或过期指标继续保留原状态。
- 草稿模型不开放运维执行工具，使用不注册到工具执行器的结构化提交格式（函数参数仅作为 JSON 数据解析），使用同一 85 秒期限、最多两次结构化生成。修订提示不带入原始思考或无效输出。生成或内容校验失败时只保存无修复动作的“待人工完善排查提纲”，来源审计为 `system_outline`；正常模型生成记为 `model`。这两种均为未确认方案。自动草稿不能建议 Ibex/Demo 下发，必须有人工作为必要项的业务/完整性恢复核验。
- 当前处置已经有仍有效的待确认方案/操作时，再发起 Agent 不继续取证使其失效。受限工具成功提出方案或操作后，使用已有执行器中断机制立即停轮，包括同一批次后续工具；人员确认与结果登记后可从页面再次启动排查。采集指标工具向模型返回证据编号、查询、状态、首末采样和数量摘要；原始证据仍由工作台保存。
- 运行新增终态 `awaiting_operator`，释放活动唯一占位，不会被 worker 自动重跑。处置等待原因区分 `plan_confirmation`、`action_confirmation`、`manual_investigation` 和 `agent_failed`。失败保留最近证据引用、未确认事项与本业务组值班接手角色；不把失败推理重试为写操作。人员修改背景时取消旧运行并提升 fence，旧结果不得覆盖人工输入；权限撤销后也会释放运行占位。
- 匹配属性由服务端合并主字段 `target_ident`、`datasource_id`、`rule_id`、`group_id`，这些值覆盖背景中的同名输入。环境/版本缺失仍为未知，不通过匹配。方案适用性、知识采用与后续执行共用这一属性定义。
- 方案、知识草稿和用户可见运行结果分离模型的 `<think>` 包裹；不完整的思考块不得成为结论。JSON 仅接受思考包裹后单一完整对象或 JSON 代码围栏，不猜测拼接无效正文。已有运行记录不改写，读取投影去掉思考内容；格式错误不保存为有效草稿。

仍未覆盖：CMDB/应用变更自动背景、真实业务探测和 Ibex 联调、带标签的多对象证据完整性、独立整改任务；上述能力不能由本次修复推定通过。

步骤结果工具声明 completed/failed/skipped 枚举；成功回执返回确切状态、证据和 ready_action_keys，与准备/批准使用同一前置门禁，避免模型反复尝试其他状态名。人工确认方案、采用依据、核实操作或开始验证后会清除已满足的等待原因。

草稿输出声明 `submit_ops_plan_draft` / `submit_ops_knowledge_draft` 作为数据格式。这两个名字不注册到运维工具执行器；只解析唯一匹配名字的 JSON 参数，其他调用、多调用均拒绝，不分发执行。优先使用结构化参数，仍兼容完整 JSON 文本。自动初稿限定简短人工排查步骤和业务检查，避免用长自由文本思考替代可审核内容。
