# 环境上下文与关联

`pkg/environment` 是 Center-only 模块。原生 Target、模板采集设备、CMDB 资产和档案知识版本仍由原模块维护；本模块持久化业务对象、人工关系、审计及候选提取任务。关系不影响采集、规则评估、告警恢复或操作审批。

## 已实现范围

首期：精确来源引用、业务对象维护、人工所属/部署/依赖/监控映射/资料描述关系、统一弹窗和只读会话工具。

二期：正式资料精确标识提及候选、定期对账、当前关系有效性核验、三层有向遍历、最近七天精确对象告警及资产台账变更。自动提取不创建业务依赖，不等价于网络发现；网络链路、变更平台及业务调用链需要未来适配。

引用格式：`target:<数值ID>`、`device:<采集设备UUID>`、`asset:<CMDB UUID>`、`service:<数值ID>`、`document:<知识库版本ID>`。后两种查询记录为 `alert:<历史事件ID>`、`asset_change:<CMDB审计ID>`，只读且不允许人工关联。不同来源即使名称/IP相同，也不自动合并。

## 权限与有效性

每次查阅校验登录有效性、来源原生业务组/采集读取权限/CMDB ACL。关系双方都必须可读；禁止通过不可见边泄露名称与关系计数。写入需要双方可写；文档关联限管理员或该正式源版本的保存人。业务对象按业务组读写权限管理。档案资料继续沿用有效登录用户可读的既有权限，不新增客户隔离 ACL。

对象指纹包含身份/归属及相关背景，不包含心跳。对象变化、删除、文档换版和撤权阻止使用旧证据。关系核实期最多366天，前端默认90天。过期/变化关系可显示待核实，但不参与依赖遍历。关系修改采用 revision 乐观锁并记录 environment_change。原生 device→asset/Target 绑定在上下文动态读取，不复制为人工确认关系。

自动任务每10秒取最多5份到期正式资料，按其原始保存人权限搜索可见对象，仅用明确 ident/序列号生成 describes 候选；每份最多扫描可见目录前100个对象，每类源读取有500条预算。任务成功或失败5分钟后对账重试，120秒租约可恢复。拒绝的候选不会自动重新开启。资料中的任意指令都不执行。未匹配不证明没有相关对象。

## 原生接口

均位于 `/api/n9e/environment`，使用既有 auth/user，返回 `{dat,err}`。

| 方法和路径 | 输入 | 输出 |
|---|---|---|
| GET /search | q，最多200字节 | 授权来源候选，最多100个、截断说明 |
| GET /context | ref | 当前对象、关系状态、已有配置绑定、覆盖缺口、自动任务状态 |
| GET /trace | ref，direction=out/in/both，depth=1..3 | 有向已确认关系，最多100节点/100边 |
| GET /timeline | ref | 最近7天、最多50条历史告警或CMDB审计 |
| POST /objects | id/revision（修改时）、group_id、kind、name、description、deleted | 业务对象；类型service/application/database/vip |
| POST /relations | from_ref/to_ref、from_version/to_version指纹、kind、note、valid_until | 人工关系；重复相同确认不新增 |
| POST /relations/:id | revision、action=confirm/reject/withdraw/renew；renew须双方指纹和来源说明/有效期 | 更新后的关系 |
| POST /discover | ref=document:… | 幂等提取候选；须有文档和端点维护权限 |

`query_environment` 是 Agent 的只读工具，只开放 search/overview/context/trace/timeline，不接受用户指定账号、SQL或写操作。context返回关系状态；trace只遍历当前已确认人工关系。既有绑定另列，不能当作网络邻接。最终回答和历史重放复核服务器生成的环境证据范围。

## 页面

设备详情工具栏、CMDB设备查看/编辑弹窗底部、知识库资料阅读抽屉的“环境关联”打开同一 Ant Design 弹窗。弹窗提供查找对象、新建/编辑/停用业务对象、增添/撤回/重新核实关系、查看依赖及事件。资料可提取并确认/拒绝候选。不新增菜单，不替换原页面框架。

## 数据库和测试

四张 environment_* 表；`models.EnvironmentTables()`、gorm迁移与 `doc/sql/environment.{mysql,postgres,sqlite}.sql` 一致。生成：`go run ./cmd/environment-schema`。初始化SQL已包含，升级SQL在docker/migratesql。

行为：`go test ./pkg/environment ./aiagent/tools ./center/router -run 'Test(Environment|Identity|Relation|Graph|DocumentCandidates|Timeline|AutomaticReconciliation|NativeBindings|Knowledge)' -count=1`。

外部测试：`N9E_TEST_ENV_POSTGRES`、`N9E_TEST_ENV_MYSQL` 仅连接以 `env_test_` 开头的一次性数据库；无配置时明确跳过。模拟用例和真实模型/监控查询分别报告，不能用模拟指标证明医院状态。
