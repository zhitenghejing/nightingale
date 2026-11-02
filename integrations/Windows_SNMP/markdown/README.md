# Windows SNMP

在设备列表新增或编辑采集设备时，选择本组件。每台设备会获得独立的采集定义、指标说明、夜莺仪表盘和告警规则。

## 采集前提

启用 Windows SNMP Service，并允许采集器访问 UDP 161。Windows 只提供 HOST-RESOURCES-MIB、SNMPv2-MIB 和 IF-MIB。
凭据在设备中选择，模板不包含设备 IP 或 Community。IP 只校验 IPv4 格式，允许同一采集器监控多个设备，也允许地址重复。

## 采集方式

以下 TOML 区块由设备服务读取；在本页编辑并保存即可修改公共采集定义。只填写数值 OID、字段、表和 required_metrics。uptime 自动采集，端点、凭据、归属标签由系统注入。字符串磁盘名称可能存在编码问题，因此以 storage index 和 HOST-RESOURCES-MIB type 识别内存和磁盘。

```snmp-collection
required_metrics = ["snmp_uptime", "snmp_cpu_load", "snmp_storage_size", "snmp_storage_used"]

[[table]]
name = "cpu"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "load"
oid = ".1.3.6.1.2.1.25.3.3.1.2"

[[table]]
name = "storage"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "type"
oid = ".1.3.6.1.2.1.25.2.3.1.2"
is_tag = true

[[table.field]]
name = "allocation_units"
oid = ".1.3.6.1.2.1.25.2.3.1.4"

[[table.field]]
name = "size"
oid = ".1.3.6.1.2.1.25.2.3.1.5"

[[table.field]]
name = "used"
oid = ".1.3.6.1.2.1.25.2.3.1.6"

[[table]]
name = "interface"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "type"
oid = ".1.3.6.1.2.1.2.2.1.3"

[[table.field]]
name = "oper_status"
oid = ".1.3.6.1.2.1.2.2.1.8"

[[table.field]]
name = "admin_status"
oid = ".1.3.6.1.2.1.2.2.1.7"

[[table.field]]
name = "speed"
oid = ".1.3.6.1.2.1.31.1.1.1.15"

[[table.field]]
name = "in_errors"
oid = ".1.3.6.1.2.1.2.2.1.14"

[[table.field]]
name = "out_errors"
oid = ".1.3.6.1.2.1.2.2.1.20"

[[table.field]]
name = "in_discards"
oid = ".1.3.6.1.2.1.2.2.1.13"

[[table.field]]
name = "out_discards"
oid = ".1.3.6.1.2.1.2.2.1.19"

[[table]]
name = "traffic32"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "in_octets"
oid = ".1.3.6.1.2.1.2.2.1.10"

[[table.field]]
name = "out_octets"
oid = ".1.3.6.1.2.1.2.2.1.16"
```

## 指标与仪表盘

指标说明、仪表盘、告警规则使用本组件另外三个标准页签，可直接编辑。CPU 是所有处理器平均利用率（hrProcessorLoad）；内存使用 hrStorageRam；磁盘使用 hrStorageFixedDisk，各分区独立。

Windows 未实现 IF-MIB 的 64 位计数器（ifHCInOctets / ifHCOutOctets），因此本组件只采集 32 位计数器（ifInOctets / ifOutOctets）。高速链路上的 32 位计数器存在回绕风险，流量读数可能偏低。接口速度取 ifHighSpeed（单位 Mbps）。接口错误与丢弃计数分别为 ifInErrors / ifOutErrors 和 ifInDiscards / ifOutDiscards。

原始 OID 不受支持时，对应图表无数据，请调整设备配置或组件定义。

## 默认告警

CPU、内存、磁盘使用率超过 90% 持续 5 分钟；SNMP 失败或连续 3 分钟无 uptime 样本后持续 1 分钟；接口 down、带宽利用率超过 90%、错误包速率超过 2 pps 各持续 5 分钟。生成的规则已启用并产生夜莺告警事件。通知渠道需要在设备关联的独立告警规则中配置。

## 设备定制

设备编辑中的高级设置可以修改该设备的采集 TOML 和指标说明。设备详情可打开独立仪表盘和告警规则进行编辑。公共模板修改只作用于之后的选择；普通保存不会覆盖已有定制。显式“重新应用模板”会重建该设备的仪表盘和规则，旧规则禁用、旧仪表盘隐藏，历史告警保留。暂停或删除设备会暂停关联规则，恢复设备会恢复原规则的启停状态。
