# Windows Server SNMP 采集模板参考

本组件（`Windows_SNMP`）的采集定义参考 Zabbix 官方模板 **"Windows by SNMP"**
（`templates/os/windows_snmp/template_os_windows_snmp.yaml`，Zabbix 7.0-4）整理，
并适配夜莺（Categraf）采集器的 SNMP 插件配置格式。

Windows 只提供 **HOST-RESOURCES-MIB**、**SNMPv2-MIB** 和 **IF-MIB** 三个 MIB。

## 一、指标与 OID 对照表

### 1.1 标量字段

| 指标名 | OID | MIB | Zabbix 对应项 | 说明 |
|---|---|---|---|---|
| `snmp_uptime` | `.1.3.6.1.2.1.1.3.0` | SNMPv2-MIB | `system.net.uptime[sysUpTime.0]` | 网络部分运行时长（单位 1/100 秒），由采集器自动注入 |

### 1.2 cpu 表 — `snmp_cpu_<field>`

| 指标名 | OID | MIB | Zabbix 对应项 | 说明 |
|---|---|---|---|---|
| `snmp_cpu_load` | `.1.3.6.1.2.1.25.3.3.1.2` | HOST-RESOURCES-MIB | `system.cpu.util`（取 avg） | 单个处理器的繁忙百分比，index 对应逻辑核 |

Zabbix 原模板用 `discovery` + JSONPath 求平均，本模板直接给出逐核值，由 PromQL
`avg by (...)` 求平均，两者语义一致。

### 1.3 storage 表 — `snmp_storage_<field>`

| 指标名 | OID | MIB | Zabbix 对应项 | 说明 |
|---|---|---|---|---|
| `snmp_storage_type`（标签） | `.1.3.6.1.2.1.25.2.3.1.2` | HOST-RESOURCES-MIB | `hrStorageType` | `.1.3.6.1.2.1.25.2.1.2` = 内存，`.1.3.6.1.2.1.25.2.1.4` = 固定磁盘 |
| `snmp_storage_allocation_units` | `.1.3.6.1.2.1.25.2.3.1.4` | HOST-RESOURCES-MIB | `hrStorageAllocationUnits` | 每块分配单元的字节数 |
| `snmp_storage_size` | `.1.3.6.1.2.1.25.2.3.1.5` | HOST-RESOURCES-MIB | `hrStorageSize` | 总容量（单位为 allocation units） |
| `snmp_storage_used` | `.1.3.6.1.2.1.25.2.3.1.6` | HOST-RESOURCES-MIB | `hrStorageUsed` | 已用量（单位为 allocation units） |

**已知限制（与 Zabbix 官方说明一致）**：`hrStorageSize` 上限为 2147483647，
实际字节数 = `hrStorageSize × hrStorageAllocationUnits`。当 allocation units 为
512 字节时可监控上限约 1TB，1024 字节约 2TB，2048 字节约 4TB，64KB 约 128TB。

### 1.4 interface 表 — `snmp_interface_<field>`

| 指标名 | OID | MIB | Zabbix 对应项 | 说明 |
|---|---|---|---|---|
| `snmp_interface_type` | `.1.3.6.1.2.1.2.2.1.3` | IF-MIB | `net.if.type[ifType]` | 接口类型（6=ethernetCsmacd, 71=ieee8023adLag） |
| `snmp_interface_oper_status` | `.1.3.6.1.2.1.2.2.1.8` | IF-MIB | `net.if.status[ifOperStatus]` | 运行状态（1=up, 2=down, 6=notPresent） |
| `snmp_interface_admin_status` | `.1.3.6.1.2.1.2.2.1.7` | IF-MIB | — | 管理状态（1=up, 2=down） |
| `snmp_interface_speed` | `.1.3.6.1.2.1.31.1.1.1.15` | IF-MIB | `net.if.speed[ifHighSpeed]` | 标称速率，单位 Mbps |
| `snmp_interface_in_errors` | `.1.3.6.1.2.1.2.2.1.14` | IF-MIB | `net.if.in.errors[ifInErrors]` | 入方向错误包累计数 |
| `snmp_interface_out_errors` | `.1.3.6.1.2.1.2.2.1.20` | IF-MIB | `net.if.out.errors[ifOutErrors]` | 出方向错误包累计数 |
| `snmp_interface_in_discards` | `.1.3.6.1.2.1.2.2.1.13` | IF-MIB | `net.if.in.discards[ifInDiscards]` | 入方向丢弃包累计数 |
| `snmp_interface_out_discards` | `.1.3.6.1.2.1.2.2.1.19` | IF-MIB | `net.if.out.discards[ifOutDiscards]` | 出方向丢弃包累计数 |

### 1.5 traffic32 表 — `snmp_traffic32_<field>`

| 指标名 | OID | MIB | Zabbix 对应项 | 说明 |
|---|---|---|---|---|
| `snmp_traffic32_in_octets` | `.1.3.6.1.2.1.2.2.1.10` | IF-MIB | `net.if.in[ifInOctets]` | 入方向累计字节数（32 位） |
| `snmp_traffic32_out_octets` | `.1.3.6.1.2.1.2.2.1.16` | IF-MIB | `net.if.out[ifOutOctets]` | 出方向累计字节数（32 位） |

## 二、为什么没有 64 位流量计数器

Zabbix 官方模板在 Known Issues 中明确记录：

> 64-bit I/O is not supported even though `IfxTable` is present. Currently,
> Windows gets its interface status from MIB-2. Since these 64-bit SNMP counters
> (`ifHCInOctets`, `ifHCOutOctets`, etc.) are defined as an extension to IF-MIB,
> Microsoft has not implemented it.
> — Version: Win2008, Win2012R2

因此本组件**不再采集** `.1.3.6.1.2.1.31.1.1.1.6`（ifHCInOctets）和
`.1.3.6.1.2.1.31.1.1.1.10`（ifHCOutOctets）。

**代价**：32 位计数器在 10GbE 链路上约 34 秒就会回绕一次，长时间窗口的
`rate()` 结果可能偏低甚至为负。Windows 侧如需精确流量，建议改用 WMI/PerfCounter
采集，或缩短 `rate()` 窗口。

其余未实现项：WindowsXP 不支持 `ifXTable`；所有版本均不支持 EtherLike MIB。

## 三、与 Linux_SNMP 组件的差异

| 项目 | Linux_SNMP | Windows_SNMP |
|---|---|---|
| 64 位流量计数器 | 采集（traffic64） | 不采集（Windows 未实现） |
| 接口错误/丢弃计数 | 无 | 有（in_errors / out_errors / in_discards / out_discards） |
| 接口速率 | 无 | 有（ifHighSpeed，Mbps） |
| 网络流量 PromQL | `rate(traffic64) or rate(traffic32)` | 仅 `rate(traffic32)` |

## 四、默认告警规则

| 规则 | 触发条件 | 持续时间 |
|---|---|---|
| CPU usage above 90% | `avg by (...) (snmp_cpu_load) > 90` | 5 分钟 |
| Memory usage above 90% | `100 * snmp_storage_used{mem} / snmp_storage_size{mem} > 90` | 5 分钟 |
| Disk usage above 90% | `100 * snmp_storage_used{disk} / snmp_storage_size{disk} > 90` | 5 分钟 |
| SNMP collection interrupted | `snmp_up == 0` 或 `absent_over_time(snmp_uptime[3m])` | 1 分钟 |
| Interface link down | `snmp_interface_oper_status == 2` | 5 分钟 |
| Interface high bandwidth usage | `8 * rate(traffic32_in/out_octets[5m]) > 0.9 * ifHighSpeed * 1000000` | 5 分钟 |
| Interface high error rate | `rate(in_errors[5m]) > 2 or rate(out_errors[5m]) > 2` | 5 分钟 |

## 五、参考来源

- Zabbix 官方模板：`zabbix/zabbix` → `templates/os/windows_snmp/template_os_windows_snmp.yaml`（release/7.0，vendor version 7.0-4）
- Zabbix 模板说明与已知问题：同目录 `README.md`
- Categraf SNMP 插件配置格式：`categraf` → `inputs/snmp/`（`Table`/`Field` 结构体定义）
