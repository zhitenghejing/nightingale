# deviceTags：CMDB型号与所属业务

用户确认将当前配置从告警规则附加标签改为同一CMDB资产的型号(model)＋所属业务(business)，顺序固定为型号、业务。请求中的deviceTags为一个字符串，例如：

```json
{"deviceTags":"['R650', 'HIS']"}
```

复用通知规则已有device_tags_source参数，新增可选值cmdb_model_business；原rule_append_tags、固定字符串及标签来源不自动修改。预览中仅将既有停用配置草稿改为新来源，媒介与草稿保持关闭；本轮未配置或读取真实密钥；CMDB关联条件保留实例现有值，尚未用真实告警验证匹配。

## 取值及边界

- center在首次登记任务时，沿现有唯一资产精确匹配，一次读取type、hostname、model、business，冻结在同一来源快照；不按人员复制，不新增资产匹配默认值。
- 找不到、匹配多台、缺少型号/所属业务时保留校验原因，不发请求。恢复及重复登记继续复用既有冻结值，不在发送时读取最新CMDB。
- 单引号、反斜杠及控制字符进行列表文本转义，再由结构体序列化和url.Values.Encode编码，保留deviceTags字符串类型。特殊字符的列表文本在1.0的展示/解析仍需后续联调验证；本轮只验证本地序列化。
- 未修改签名、事件配对、去重、worker重试、人工操作、其他通知渠道或数据库schema。旧任务保持原报文，不批量重算。故障轮次中途更换字段来源不能视为对方配对已验证。

## 关键源码

- pkg/cmdb/alert_device.go：AlertDevice及LookupAlertDevice读取model、business。
- pkg/externalpush/sources.go：ResolvedSources及resolveSources冻结同一资产字段。
- pkg/externalpush/protocol.go：Build构造字符串，singleQuotedTag转义列表元素；缺值/混合来源阻止发送。
- frontend/src/pages/notificationRules/pages/Form/ChannelParams/ExternalPush.tsx：新增来源选项，共用CMDB关联配置，切换清理无效手动字段。
- frontend/src/pages/notificationChannels/ExternalPushFields.tsx及中英文locale：页面显示字段来源与字符串格式。

## 验证记录

先添加同样公开登记路径的回归，修复前新来源报validation_failed（/tmp/cmdb-tags-red.log）；修复后本地模拟接收器收到两组成对故障/恢复，覆盖来源冻结、重复登记、特殊设备名称、中文及&+字符、空字段和资产歧义。原附加标签来源一并回归；无真实1.0请求。

已对照本轮完整工作区基线进行静态复核：新增可选来源，既有来源缺省语义不变；无schema、公开函数签名和edge读路径变更；仍由center匹配资产并发送。另一个同工作区问答会话补充的CMDB字段说明已合并核对，该会话已停止写入。


最终检查：make verify由本会话执行并返回0，基础设施63项、Go 2786项、前端214套件/1860项通过，48项外部服务环境用例跳过；make verify-all未执行。定向race、内嵌资源与当前pub逐文件比对通过；原始附件28个受保护值检查无泄露。之前同工作区会话启动的重复检查/构建已终止，以本会话最终返回0为准。

Agent浏览器在构建20261029T083622Z-453b8284上保存停用草稿13的新来源并重开核对；保留其当时已有CMDB关联值，没有自动填入或清空。预置媒介25及草稿均关闭，任务前后均为30条且ID集合一致，无任务创建或发送，无页面错误。local/LAN及源码/前端身份通过，公网仍TLS EOF失败，仅代表本地/LAN验收。文档补录后再次刷新，最终build ID以交付消息为准。人工验收和真实接口调用均未进行。
