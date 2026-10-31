# 原型一致性修正

2026-10-30。验收依据是本目录已经确认的 README 和 prototype.html；不修改原型来迎合已有实现。

上一版的问题是把通用设备监控包缩成 SNMP 接入特例：新增设备仍是一页表单，表单只允许协议/端口/凭据三项，草稿不包含原生资源正文，升级保留整份旧资源。它不能代表“按原型执行”。本次修正在现有生命周期和原生资源引擎上完成，保持原有设备、不可变版本、业务组权限和独立实例。

| 原型要求 | 对应实现与验证入口 |
|---|---|
| 卡片白色简洁、悬停铅笔只改基本信息、主体改详细配置 | CatalogBrowser、ComponentFormModal、CatalogEditor；卡片行为测试与生产浏览器脚本 |
| 同一完整草稿管理四部分资源与表单/通知 | CatalogDefinition.pack/notify；TestPrototypePublicationUsesWholeDraft 验证草稿外原生库修改不混入发布 |
| 通用 Schema 驱动不同设备字段 | definition.go、TemplateFields；数据库账号/密码、条件字段、秘密默认值拒绝、隐藏值排除测试 |
| 设备内三级选择与三步向导 | TemplateSelection、DeviceEditor：配置 → 仪表盘/告警通知 → 脱敏确认；生产浏览器验收 |
| 密码保密并支持编辑保留 | parameters.go 的 active-field 校验和加密边界；读取只返回 is-set，保存留空不回显秘密 |
| 发布固定版本，普通保存保持设备覆盖 | monitor_template_version；旧版本只读/编辑兼容、新绑定要求当前可用发布版本 |
| 升级只改未覆盖字段 | profile_upgrade.go 比较旧包/设备现值/新包；冲突列出并显式确认，原生实例 ID 保留，新增资源单独创建 |
| 首批指标完成后启用告警 | arming.go 的后台检查；Provider 已读取、历史样本、仅 up 和查询失败均不能解除等待 |
| 通知采用夜莺现有引擎 | 原生 NotifyRule/NotifyChannel/NotifyTpl 校验，RuleNote 与附加信息承载消息；无映射仅事件 |
| 同一采集器管理多个不同类型对象 | Provider 按 snmp/mysql 插件分组，设备/修订标签隔离，IP 仅格式校验 |
| 保留资料包手动接入能力 | 仪表盘/规则仍可通过原生导入弹窗导入业务组；发布和设备实例均与原生库草稿编辑隔离 |

实际执行目前包括 Windows/Linux SNMP v2c 和 MySQL 8.4 LTS。MySQL_84_Remote 是在原 91 项盘点之外新增的完整包，测试服务为 MySQL 8.4.11；固定 Categraf v0.5.18 完成真实 Provider → 数据库查询 → Remote Write → 必需指标验证，临时连接测试不登记 Target。MySQL 使用类型化开关和精确 metrics_pass，不通过字符串替换拼接密码到配置。

完整草稿支持数据库、硬件等类型的条件表单。SQL Server、Oracle、PostgreSQL、SNMP v3、Redfish、Redis、HTTP 等的执行编译器、具体型号/版本与采集器能力验证仍属于迁移队列；未完成者不能发布为可自动采集模板。本次没有真实 Dell/HPE/Lenovo 硬件或通知渠道投递验收，不能据表单或事件创建声称完成这些验证。

规则升级按属性比较；数组结构变动和同字段双向修改需要冲突确认，删除的模板资源保留为设备覆盖。原生规则在等待首批样本期间发生人工编辑时，后台恢复采取保守策略，不覆盖该规则的人工启停状态。特殊自定义媒介模板需要保留原生 RuleNote 或引用 collection_message。

自动化结果、实际预览 build ID、浏览器截图和探测记录在本次交付时记录；人工验收由使用者完成。预览沿用本地 192.168.0.152 端口，原型、原型归档和架构图保持原文件内容。
