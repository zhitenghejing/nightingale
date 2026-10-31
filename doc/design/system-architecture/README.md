# 当前系统架构与模块职责

核对日期：2026-10-29。基线：`feat/snmp-device-management`，`ed462382` 及当前未提交实现。范围是本工作树和 dev4 本地预览，不代表稳定实例或商业版功能清单。本轮只增加说明和图形，不改变业务实现。

## 图形入口

- [01 系统全景与组件功能](01-system-overview.svg)：沿官方“采集 / 数据源—夜莺—通知”结构，展开当前功能。
- [02 SNMP 模板到采集与告警闭环](02-snmp-collection.svg)：区分配置发布、实际采集、时序查询和告警。
- [03 当前部署与可选扩展](03-current-deployment.svg)：标出 PostgreSQL、进程内 miniredis、共享 Prometheus 及尚未启用的服务。
- [可缩放阅读页](architecture.html)：切换图形、查看原图和 PNG，适合在浏览器阅读。

`build.py` 是无第三方依赖的 SVG 生成源，执行 `python3 doc/design/system-architecture/build.py` 可重建三张图；可编辑关系源另存于同目录 `.mmd` 文件。PNG 来自浏览器对 SVG 的实际渲染。

执行 `python3 doc/design/system-architecture/publish.py` 可将图形随当前预览 UI 发布到 `http://192.168.0.152:30304/architecture/architecture.html`。发布使用显式文件资源快照，保留原业务页面和此前的设备添加原型，不修改 `frontend/pub` 或业务源码。验收时依次切换三张图、调整缩放、打开 SVG 原图并下载 PNG / Mermaid 源；检查内容是否覆盖实际模块。

## 参考方式

参考 [夜莺官方系统架构](https://n9e.github.io/zh/docs/concepts/architecture)、[官方架构设计及数据流图](https://flashcat.cloud/docs/content/flashcat-monitor/nightingale-v8/prologue/architecture/) 和仓库现有 [官方产品架构图](../../img/readme/20240221152601.png)、[当前 README 架构图](../../img/readme/architecture-overseas.svg)。沿用中心进程与外部采集、数据存储、通知的边界，增加本地资产、档案及本分支的受管 SNMP 链路；没有直接把官方版本的功能数量和默认部署照搬为本机状态。

颜色与线型表示链路：蓝色为指标写入，绿色为查询请求和返回，橙色为事件和通知，紫色虚线为配置 / 心跳 / 管理。节点上的“原生 / 本地扩展 / 分支”表示功能来源；可选部署使用虚线框。

## 模块功能与实现落点

| 组件 / 模块 | 当前职责 | 主要实现 / 入口 | 状态与边界 |
|---|---|---|---|
| WebUI / Center API | 管理界面、页面 API、服务 API、统一登录 | `frontend/src`、`center/router`；`/api/n9e`、`/v1/n9e` | 与后端在同一 `n9e` 进程提供服务 |
| 用户、团队、角色与业务组 | 身份认证、菜单操作点、业务组读写授权 | `center/router` 认证 / 权限中间件、`models` 等 | CMDB 使用自身库存 ACL；档案首版只要求有效登录 |
| SSO / Token / OAuth | 单点登录、个人 Token、外部工具授权 | `center/sso`、HTTP 认证中间件、MCP OAuth 接口 | 按运行配置连接外部身份提供商 |
| 系统配置 | 站点、变量、引擎注册、版本信息、操作审计 | `center/cconf`、`center/router`、`pkg/aop` | 不替代底层服务的部署配置 |
| 设备列表 / Target | 机器身份、心跳、业务组、标签、详情 | `models/target*`、`center/metas`、`pushgw/idents`；`/targets` | Agent 上报与受管 SNMP 登记共存；有 Target 不等于有指标 |
| 受管 SNMP 设备 | 新增 / 编辑、凭据、连接探测、启停 / 删除、版本健康 | `pkg/collection`、`models/monitor*`、`center/router/router_collection.go` | 本分支；当前仅 SNMP v2c 真实自动接入路径 |
| Provider | 按采集器发布完整版本配置、撤回任务 | `pkg/collection`、`center/router/router_collection.go`；`/v1/n9e/collection/configs` | Center 专属独立认证；收到版本不是采集成功凭证 |
| Categraf | 本机插件采集、远程 SNMP、Provider 拉取、指标上报 | 外部采集器；分支兼容实现位于 `third_party` | 一个采集器可采多个 Agent；目标 SNMP 设备不必安装 Categraf |
| SNMP Agent | 响应 SNMP 请求、暴露 OID | Windows SNMP / Linux Net-SNMP 等目标服务 | 团体字与 Agent view 决定可读指标；模板语法正确不保证 OID 可用 |
| Pushgw | 接收多种上报协议、Target / ident 处理、Remote Write 转发 | `pushgw/router`、`pushgw/writer` | 受管样本额外校验设备、采集器及修订归属；不是长期指标存储 |
| 数据源管理与查询适配 | 保存查询连接，代理指标 / 日志 / SQL 请求，缓存客户端 | `datasource`、`dscache`、`prom`、`dskit`；`/datasources` | “数据库作为查询源”与“采集数据库运行指标”是两种用途 |
| 数据查询 | 即时 / 区间指标查询、日志查询、指标视图与快捷视图 | `pages/explorer` 等、`center/router/router_query*` | 从选择的数据源取数；不会从设备登记表生成指标 |
| 仪表盘 | 图表、变量、查询配置与编辑 | `pages/dashboard`、`models/board*` | SNMP 模板应用后生成独立原生实例，可单设备调整 |
| 记录规则 | 定时查询并产生派生指标 | `models/recording_rule.go`、`alert/eval` | 派生指标写入由规则及数据源配置决定 |
| 模板中心 | 原生组件的采集说明、指标说明、仪表盘、告警规则 | `integrations`、`center/integration`、`pages/builtInComponents` | Windows_SNMP / Linux_SNMP 是原生四部分包；启动保留已编辑内容 |
| 集成与系统嵌入 | 数据源连接与嵌入产品入口 | `/datasources`、`/embedded-products` | 不代表嵌入产品已安装或成为夜莺内置模块 |
| AlertEngine | 周期查询、规则分片、持续时长、触发和恢复 | `alert/eval`、`alert/naming`、`alert/process` | 中心默认可运行；同引擎集群规则按一致性哈希分摊 |
| 事件与降噪 | 屏蔽、去重、当前 / 历史事件、事件队列 | `alert/process`、`alert/queue`、`models/alert_*` | 事件队列在进程内，不应误画成 Redis 持久消息队列 |
| 通知与订阅 | 通知规则、订阅匹配、媒介、消息模板、发送记录 | `alert/dispatch`、`alert/sender`、`memsto` | 告警事件已生成不等于接收人和通知媒介已经配置 |
| 事件 Pipeline | 条件、转换、分支、HTTP / 脚本等处理与执行记录 | `alert/pipeline`、`models/event_pipeline*` | 在规则或通知流程启用时参与，不是每个事件都必经的独立进程 |
| 自愈 / Ibex | 自愈脚本、任务下发、目标 Agent 执行及记录 | 外部 Ibex 包、`job-tpls` / `job-tasks` 页面 | 代码支持；本地预览 `Ibex.Enable=false` |
| Nightingale AI | 对话、工具调用、Agent / LLM、Skills、任务协作 | `aiagent`、`center/router/router_ai*`；`/nightingale-ai` | 需要配置模型；基本预览禁用 Skill 脚本执行，模型连接未在本轮验收 |
| MCP / A2A | 向外部助手开放工具、向内置 Agent 委派任务 | `aiagent/a2a`；`/mcp`、`/a2a` | 位于同一 Center；写工具和身份模式受配置控制 |
| CMDB | 资产、配置、导入导出、权限、变更审计、回收站 | `pkg/cmdb`；`/api/cmdb`、`/targets/cmdb` | 本地扩展；资产记录及监控开关没有自动创建 Target / 采集 / 告警 |
| 机柜图 | 机房 / 机柜目录、U 位、资产上架、跨机柜拖拽 | `pages/rackViews`、CMDB Store；`/targets/racks` | 与 CMDB 共用资产数据与权限 |
| 档案管理 | 培训 / 巡检 / 故障等档案、原件、在线编辑、版本与导出 | `archives`、`pages/archiveManagement`；`/archives` | 本地扩展；DB 存元数据，私有目录存文件，导出使用 Node CLI / LibreOffice |
| 元数据库 | 配置、规则、模板、用户、Target、资产、档案元数据 | `storage`、`models`、`models/migrate` | 当前 PostgreSQL；代码另支持 MySQL / SQLite，三者不会同时作为本实例主库 |
| Redis 与配置缓存 | 心跳、会话 / 流状态等；配置热路径读取内存缓存 | `storage`、`memsto`、`dscache` | 当前 miniredis 位于进程内；它不是 Prometheus，也不是 `memsto` 本身 |
| 指标 / 日志数据存储 | 指标样本长期保存、PromQL / 日志 / SQL 查询 | 外部 Prometheus / VM / ES / Loki / CK 等 | 当前指标是共享 Prometheus；其他后端是可选连接能力 |
| 文件、日志、后台清理 | 档案文件、Skills、判定日志、密钥、事件 / 通知记录保留 | `archives`、`aiagent/skill`、`evallog`、`cron` | 备份应包含所需 DB、文件、密钥；文件不是公开静态目录 |
| Preview Gateway | 隔离实例生命周期、入口代理、构建身份与探测 | `scripts/preview.py`、`cmd/preview-gateway` | 开发环境设施，不是生产夜莺必需组件 |

## 实际数据流

1. **指标写入**：SNMP Agent → Categraf → 夜莺 Pushgw → `Pushgw.Writers` → Prometheus。箭头表示数据返回方向；SNMP 请求由 Categraf 主动发起。
2. **配置下发**：模板中心 / 设备表单 → 模板快照与加密发布 → HTTP Provider → Categraf 主动拉取。采集器也可保留与受管任务不冲突的本地插件配置。
3. **页面查询**：设备详情 / 仪表盘 / 数据查询 → 数据源查询适配 → Prometheus → 返回真实指标。设备身份、数据源和指标标签必须匹配。
4. **告警通知**：规则调度 → 查询数据源 → 触发 / 恢复判定 → 屏蔽 / 去重 → 事件队列 → 通知规则 / 订阅及可选 Pipeline → 媒介 / 消息模板 → 接收方。部分 Pipeline 也可在规则处理阶段执行，图中合并为事件处理能力。
5. **业务文档**：浏览器 → 档案 API → 元数据库及私有文件；需要导出时启动离线转换子进程。这条路径不经过 Prometheus。

`Pushgw.Writers` 决定写入地址，页面的数据源决定读取地址；两个配置必须各自正确，写入成功不能替代查询数据源配置。

## 本地预览核对与未实现部分

核对时预览入口为 `http://192.168.0.152:30304`，槽位 4。实际运行配置：PostgreSQL、miniredis、一个 Remote Write Writer、受管采集开启、`EmbeddedTSDB.Enable=false`、`Ibex.Enable=false`。元数据库 / Redis 的选择与官方通用生产图不同，已按实际配置绘制。未把凭据、密钥、DSN 或 Token 放入图中。

独立 `n9e-edge`、`n9e-alert`、`n9e-pushgw` 属于代码支持的可选部署；本预览没有运行它们。edge / alert 通过 CenterApi 读取配置，不能直接画成连接中心数据库。受管 Provider 和受管样本归属服务当前仅在 Center 接入，不代表这些独立进程已拥有相同能力。

其他 89 个默认模板的分类、动态接入表单仍是[设计原型](../device-onboarding-prototype/README.md)，尚未成为自动配置采集的通用实现。SNMP 的 CPU、内存、磁盘 >90% 持续 5 分钟及采集中断默认规则不能照搬为数据库、云平台或集群的通用规则。

核对依据包括 `center/center.go`、`cmd/edge/edge.go`、`alert/alert.go`、`pushgw`、前端实际菜单 `components/SideMenu/menu.tsx`，以及 [采集 API](../../api/collection.md)、[CMDB API](../../api/cmdb.md)、[档案 API](../../api/archives.md)、[MCP API](../../api/mcp-server.md)、[事件 Pipeline API](../../api/event-pipeline.md)。本轮为架构说明与图形检查，没有重新验证外部设备、真实通知投递、外部 LLM 或全部模块的业务行为。
