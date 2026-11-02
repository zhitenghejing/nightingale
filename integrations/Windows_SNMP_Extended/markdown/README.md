# Windows SNMP 增强

基于用户提供的 base.py 采集意图重新制作，使用 Categraf 数值 OID 采集，不执行 Python 脚本。适用于提供 HOST-RESOURCES-MIB、IF-MIB 与 TCP/IP MIB 的 Windows SNMP v2c 代理。具体型号与版本以实测为准。

## 设备添加与名称标签
在设备列表新增采集设备，依次选择“操作系统 → 微软 → Windows 通用 → Windows SNMP 增强”。填写设备名称、IP、采集器、SNMP 端口及团体字凭据。允许同一采集器采集多台设备，也允许 IP 重复，ident/设备 ID 独立区分。
所有受管 SNMP、MySQL 和 ICMP 样本，在夜莺通过设备归属校验后，自动添加 device_name=设备名称。模板不用填写这个标签，自定义标签和 OID 标签不能覆盖它。设备改名后，新写入样本使用新名称；历史数据保留原标签。ident 和 collection_device_id 始终作为设备标识。原生 Agent 自身的本机指标不在本模板的修改范围，授权的 Agent ICMP 以其列表名称 ident 标记。

## 指标范围
基础项：每核/整机 CPU、系统运行时间、核心数、物理内存总量/已用/剩余/使用率、分区容量与使用率、网卡收发流量/速率/带宽利用率、运行/管理状态、错误/丢弃计数和速率。分区按 index 和 storage type 区分，不把可能乱码的 Windows 分区描述写入标签。
扩展项：CPU 型号、网卡名称/别名/MAC、接口数量、双工状态、进程名称/PID/内存/CPU、IPv4 路由表。
TCP 可选项：IPv4 各连接状态计数、监听地址和端口、部分常用服务连接关系；现代 TCP/IPv6 表与独立监听表保留索引和 PID，取决于代理支持。

## 可选采集与兼容性
本模板初始停用 tcp、tcp6、listener 三张明细表，避免默认产生大量短寿命连接时序。启用方式：在模板采集配置或设备高级配置中，从 disabled_tables 删除需要启用的表名。进程、路由、网卡说明也可通过将 process、route、interface_info 加入 disabled_tables 单独停用。修改设备配置只影响该设备。
所有表使用 partial 错误策略，必需指标只包含 uptime、CPU、内存/磁盘容量；可选 OID 缺失不影响整机基础采集判断。可选面板未开启或 OID 不受支持时显示无数据，不用假零值代替。
部分 Windows 原生代理不提供 64 位网卡计数器、双工 OID 或现代监听表。本模板优先使用已提供的 64 位流量，缺失时退回 32 位。32 位计数器在高速链路可能多次回绕，需缩短周期或更换支持 64 位计数的采集方式。
网卡说明与数值采集分开；名称/别名编码不合法只影响说明表，不影响流量、CPU、内存和分区采集。CPU 平均值只计算存在真实 load 的行。

## 单位与计算口径
流量累计值保持 bytes，速率在 PromQL 中求导；bit/s 只在图表表达式中乘 8。接口速度为 ifHighSpeed×1000000 bit/s。内存/分区字节数是 size/used×allocation_units，比例按原始分配单位计算。
hrSystemUptime 按 0.01 秒换算，和自动 SNMP uptime 的原始 TimeTicks 分开。进程 CPU 原值为累计百分之一秒；CPU 秒=原值/100，整机 CPU 占比=rate(原值)/逻辑处理器数。进程按 SNMP index（通常是 PID）和名称区分，不沿用原脚本按进程名缓存的相对占比算法。
原脚本的 ext4 使用率偏移、手工大内存补偿和“缓存未发现即磁盘故障”不沿用。表状态代表所采集对象的枚举值，不代表 RAID/SMART 硬件健康。
TCP 监听优先遵循实际代理能力：旧 IPv4 表使用 tcpConnState=2，现代监听使用独立 tcpListenerTable，不从现代 tcpConnectionState=2 推测监听。连接关系只按目标端口推断服务名称，不代表服务健康，也不是 NetFlow。

## 仪表盘与默认告警
指标说明和仪表盘提供基础资源、接口、进程、TCP 与路由视图。默认告警：CPU、内存、磁盘使用率 >90% 持续 5 分钟；SNMP 采集失败/中断。告警事件与通知复用夜莺原生机制，通知渠道在设备或告警规则中配置。ICMP 仍由公共基础探测独立采集，不因 ping 不通阻断 SNMP。

## 设备定制与版本
可编辑公共模板，也可在设备高级配置、独立仪表盘和告警规则中定制。发布产生新版本；原有设备继续绑定原版本，不自动升级，不覆盖原 Windows_SNMP 模板。

## 采集配置
只填写数值 OID，不含设备 IP、Community 或固定 device_name。采集器参数由设备绑定统一提供。

```snmp-collection
required_metrics = ["snmp_uptime", "snmp_cpu_load", "snmp_storage_size", "snmp_storage_used"]
disabled_tables = ["tcp", "tcp6", "listener"]

[[field]]
name = "system_uptime"
oid = ".1.3.6.1.2.1.25.1.1.0"

[[field]]
name = "memory_total_kib"
oid = ".1.3.6.1.2.1.25.2.2.0"

[[field]]
name = "interface_count"
oid = ".1.3.6.1.2.1.2.1.0"

[[table]]
name = "cpu"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "load"
oid = ".1.3.6.1.2.1.25.3.3.1.2"

[[table.field]]
name = "model"
oid = ".1.3.6.1.2.1.25.3.2.1.3"
is_tag = true

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

[[table.field]]
name = "duplex"
oid = ".1.3.6.1.2.1.10.7.2.1.19"

[[table]]
name = "interface_info"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "type"
oid = ".1.3.6.1.2.1.2.2.1.3"

[[table.field]]
name = "name"
oid = ".1.3.6.1.2.1.31.1.1.1.1"
is_tag = true

[[table.field]]
name = "alias"
oid = ".1.3.6.1.2.1.31.1.1.1.18"
is_tag = true

[[table.field]]
name = "mac"
oid = ".1.3.6.1.2.1.2.2.1.6"
is_tag = true
conversion = "hwaddr"

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

[[table]]
name = "traffic64"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "in_octets"
oid = ".1.3.6.1.2.1.31.1.1.1.6"

[[table.field]]
name = "out_octets"
oid = ".1.3.6.1.2.1.31.1.1.1.10"

[[table]]
name = "process"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "name"
oid = ".1.3.6.1.2.1.25.4.2.1.2"
is_tag = true

[[table.field]]
name = "process_type"
oid = ".1.3.6.1.2.1.25.4.2.1.6"

[[table.field]]
name = "cpu_time"
oid = ".1.3.6.1.2.1.25.5.1.1.1"

[[table.field]]
name = "memory_kib"
oid = ".1.3.6.1.2.1.25.5.1.1.2"

[[table]]
name = "tcp"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "state"
oid = ".1.3.6.1.2.1.6.13.1.1"

[[table.field]]
name = "local_address"
oid = ".1.3.6.1.2.1.6.13.1.2"
is_tag = true

[[table.field]]
name = "local_port"
oid = ".1.3.6.1.2.1.6.13.1.3"
is_tag = true

[[table.field]]
name = "remote_address"
oid = ".1.3.6.1.2.1.6.13.1.4"
is_tag = true

[[table.field]]
name = "remote_port"
oid = ".1.3.6.1.2.1.6.13.1.5"
is_tag = true

[[table]]
name = "tcp6"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "state"
oid = ".1.3.6.1.2.1.6.19.1.7"

[[table.field]]
name = "pid"
oid = ".1.3.6.1.2.1.6.19.1.8"

[[table]]
name = "listener"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "pid"
oid = ".1.3.6.1.2.1.6.20.1.4"

[[table]]
name = "route"
index_as_tag = true
error_policy = "partial"

[[table.field]]
name = "route_type"
oid = ".1.3.6.1.2.1.4.21.1.8"

[[table.field]]
name = "destination"
oid = ".1.3.6.1.2.1.4.21.1.1"
is_tag = true

[[table.field]]
name = "gateway"
oid = ".1.3.6.1.2.1.4.21.1.7"
is_tag = true

[[table.field]]
name = "mask"
oid = ".1.3.6.1.2.1.4.21.1.11"
is_tag = true
```

参考：RFC 2790、RFC 2863、RFC 4022。该配置不采集 Windows 服务状态、事件日志、磁盘 IOPS 或 RAID 健康。

## Onboarding declaration

```device-onboarding
{
  "adapter": "snmp_v2c",
  "execution": "collector",
  "reachability": "icmp",
  "config": "required_metrics = [\"snmp_uptime\", \"snmp_cpu_load\", \"snmp_storage_size\", \"snmp_storage_used\"]\ndisabled_tables = [\"tcp\", \"tcp6\", \"listener\"]\n\n[[field]]\nname = \"system_uptime\"\noid = \".1.3.6.1.2.1.25.1.1.0\"\n\n[[field]]\nname = \"memory_total_kib\"\noid = \".1.3.6.1.2.1.25.2.2.0\"\n\n[[field]]\nname = \"interface_count\"\noid = \".1.3.6.1.2.1.2.1.0\"\n\n[[table]]\nname = \"cpu\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"load\"\noid = \".1.3.6.1.2.1.25.3.3.1.2\"\n\n[[table.field]]\nname = \"model\"\noid = \".1.3.6.1.2.1.25.3.2.1.3\"\nis_tag = true\n\n[[table]]\nname = \"storage\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"type\"\noid = \".1.3.6.1.2.1.25.2.3.1.2\"\nis_tag = true\n\n[[table.field]]\nname = \"allocation_units\"\noid = \".1.3.6.1.2.1.25.2.3.1.4\"\n\n[[table.field]]\nname = \"size\"\noid = \".1.3.6.1.2.1.25.2.3.1.5\"\n\n[[table.field]]\nname = \"used\"\noid = \".1.3.6.1.2.1.25.2.3.1.6\"\n\n[[table]]\nname = \"interface\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"type\"\noid = \".1.3.6.1.2.1.2.2.1.3\"\n\n[[table.field]]\nname = \"oper_status\"\noid = \".1.3.6.1.2.1.2.2.1.8\"\n\n[[table.field]]\nname = \"admin_status\"\noid = \".1.3.6.1.2.1.2.2.1.7\"\n\n[[table.field]]\nname = \"speed\"\noid = \".1.3.6.1.2.1.31.1.1.1.15\"\n\n[[table.field]]\nname = \"in_errors\"\noid = \".1.3.6.1.2.1.2.2.1.14\"\n\n[[table.field]]\nname = \"out_errors\"\noid = \".1.3.6.1.2.1.2.2.1.20\"\n\n[[table.field]]\nname = \"in_discards\"\noid = \".1.3.6.1.2.1.2.2.1.13\"\n\n[[table.field]]\nname = \"out_discards\"\noid = \".1.3.6.1.2.1.2.2.1.19\"\n\n[[table.field]]\nname = \"duplex\"\noid = \".1.3.6.1.2.1.10.7.2.1.19\"\n\n[[table]]\nname = \"interface_info\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"type\"\noid = \".1.3.6.1.2.1.2.2.1.3\"\n\n[[table.field]]\nname = \"name\"\noid = \".1.3.6.1.2.1.31.1.1.1.1\"\nis_tag = true\n\n[[table.field]]\nname = \"alias\"\noid = \".1.3.6.1.2.1.31.1.1.1.18\"\nis_tag = true\n\n[[table.field]]\nname = \"mac\"\noid = \".1.3.6.1.2.1.2.2.1.6\"\nis_tag = true\nconversion = \"hwaddr\"\n\n[[table]]\nname = \"traffic32\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"in_octets\"\noid = \".1.3.6.1.2.1.2.2.1.10\"\n\n[[table.field]]\nname = \"out_octets\"\noid = \".1.3.6.1.2.1.2.2.1.16\"\n\n[[table]]\nname = \"traffic64\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"in_octets\"\noid = \".1.3.6.1.2.1.31.1.1.1.6\"\n\n[[table.field]]\nname = \"out_octets\"\noid = \".1.3.6.1.2.1.31.1.1.1.10\"\n\n[[table]]\nname = \"process\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"name\"\noid = \".1.3.6.1.2.1.25.4.2.1.2\"\nis_tag = true\n\n[[table.field]]\nname = \"process_type\"\noid = \".1.3.6.1.2.1.25.4.2.1.6\"\n\n[[table.field]]\nname = \"cpu_time\"\noid = \".1.3.6.1.2.1.25.5.1.1.1\"\n\n[[table.field]]\nname = \"memory_kib\"\noid = \".1.3.6.1.2.1.25.5.1.1.2\"\n\n[[table]]\nname = \"tcp\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"state\"\noid = \".1.3.6.1.2.1.6.13.1.1\"\n\n[[table.field]]\nname = \"local_address\"\noid = \".1.3.6.1.2.1.6.13.1.2\"\nis_tag = true\n\n[[table.field]]\nname = \"local_port\"\noid = \".1.3.6.1.2.1.6.13.1.3\"\nis_tag = true\n\n[[table.field]]\nname = \"remote_address\"\noid = \".1.3.6.1.2.1.6.13.1.4\"\nis_tag = true\n\n[[table.field]]\nname = \"remote_port\"\noid = \".1.3.6.1.2.1.6.13.1.5\"\nis_tag = true\n\n[[table]]\nname = \"tcp6\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"state\"\noid = \".1.3.6.1.2.1.6.19.1.7\"\n\n[[table.field]]\nname = \"pid\"\noid = \".1.3.6.1.2.1.6.19.1.8\"\n\n[[table]]\nname = \"listener\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"pid\"\noid = \".1.3.6.1.2.1.6.20.1.4\"\n\n[[table]]\nname = \"route\"\nindex_as_tag = true\nerror_policy = \"partial\"\n\n[[table.field]]\nname = \"route_type\"\noid = \".1.3.6.1.2.1.4.21.1.8\"\n\n[[table.field]]\nname = \"destination\"\noid = \".1.3.6.1.2.1.4.21.1.1\"\nis_tag = true\n\n[[table.field]]\nname = \"gateway\"\noid = \".1.3.6.1.2.1.4.21.1.7\"\nis_tag = true\n\n[[table.field]]\nname = \"mask\"\noid = \".1.3.6.1.2.1.4.21.1.11\"\nis_tag = true\n",
  "notification": "events_only",
  "compatibility": "Windows SNMP v2c; numeric HOST-RESOURCES, IF and TCP/IP OIDs. Optional OIDs depend on the agent. Validate on the target OS version.",
  "fields": [
    {
      "key": "protocol",
      "label": "SNMP 版本",
      "type": "select",
      "required": true,
      "default": "snmp_v2c",
      "options": [
        "snmp_v2c"
      ]
    },
    {
      "key": "port",
      "label": "SNMP 端口",
      "type": "number",
      "required": true,
      "default": 161,
      "min": 1,
      "max": 65535
    },
    {
      "key": "credential_id",
      "label": "SNMP 团体字凭据",
      "type": "credential",
      "required": true,
      "help": "凭据加密保存，一个采集器可采集多台设备。"
    }
  ],
  "notify": {
    "mode": "events_only",
    "rule_ids": [],
    "message": "【{{告警级别}}】{{设备名称}}：{{规则名称}}\n当前值：{{当前值}}；发生时间：{{发生时间}}"
  }
}
```
