# main 合入与功能保留记录

日期：2026-11-27。合入前分支 `59010fe3cbea2fd61e8fe9535e5f03c7717d7e99`；
合入 main `e8aba1563601677cc6dc45667dc3f65fd1ef0c9a`。
用户要求：解决冲突，保留本分支已启用的资料问答、知识维护及环境关系；
保留 main 已发布能力；未启用的处置模块不重构；27B 提速与语义严谨性优化继续延期。
本次是将 main 合入功能分支并更新 PR，不授权将 PR 合入 main 或发布稳定实例。

## 冲突决策

11 个冲突文件按以下方式整合：

- `CLAUDE.md`：保留 main SSH 诊断说明和分支 CMDB/环境关系说明，纠正旧的未绑定描述。
- Center 配置与路由：同时保留 Ops/Library/Environment 和 main RemoteDiagnostics/RemoteAccess。
- `buildToolDeps`：沿用 main 可增补的局部依赖对象，保留 CMDB/RemoteDiagnostics 并加入 Library，聊天及恢复共用。
- MySQL 初始化、迁移及 SQLite 初始化：保留双方独立的表结构，不删表、不改现有记录。
- gorm 注册：保留双方表集合；`ops_` 迁移失败仅追加一次错误，仍阻止启动。
- 设备详情：沿用 main Linux 仪表盘/资产信息布局，将环境关联放入同一区域；保留刷新、编辑和告警行为。
- 设备页测试：合并请求 mock 与测试依赖，保留双方用例并增加环境关联布局断言。
- `front/statik/assets.zip`：从整合后的当前前端源码重新构建，不能选择任一旧包交付。

## Standards

两位只读复核者中的规范审查者确认：未发现明确规范违规或已确认功能退化。
初审的迁移错误重复追加建议已用互斥处理关闭，最终0项明确违规、0项未关闭建议。
公开入口签名、main 远程访问/诊断模块和三方言配套均保留。审查结论基于源码与针对性日志。

## Spec

需求审查者确认：未发现实质需求回归，0项阻断发现。
main 独有的308个变更路径在整合检查时与固定 main 逐字节一致；双方交集32个文件经接缝复核。
普通 Agent 仍能组合两类 CMDB 查询、Target 统计、资料工具、动态 Skill 与 main 远程诊断能力。
处置模块保留现状，不增加故障沉淀功能，不推进延期优化。

## 验证与边界

- 针对性 Go：router、chat、tools、CMDB、知识库、环境关系、migration，以及 remoteaccess、remotediag、collection。
- 针对性前端：DeviceDetail、ChatPanel、event/detail、SSHSettings、EnvironmentRelations；原五组48项通过，新增布局断言通过。
- 新增联动回归实际经 `buildToolDeps` 构造依赖，执行 main CMDB 搜索、分支精确资产查询及资料读取。
- `make verify-all`：Go 3328通过、0失败、65项条件性跳过；前端237组1964项通过，66项预览基础设施测试、vet和两项完整集成规则门禁通过。未配置的外部集成不算通过。
- 当前前端生产构建及statik逐文件配对检查通过；前端源码SHA-256为 `770fcba3970466129cec871ad7e048096900d8ebcab99b89fbaada12072c03d8`。
- 推送前门禁、预览构建及浏览器验证以更新后 PR 中实际结果和 build ID 为准。
- 自动化及 Agent 浏览器验证不代替人工验收；真实设备 SSH/RDP、外部数据库和通知服务只按实际配置的测试范围报告。
- 当前验证不保证消除模型的所有无依据推断；延期事项仍见[待办](deferred-optimizations-20261127.md)。
