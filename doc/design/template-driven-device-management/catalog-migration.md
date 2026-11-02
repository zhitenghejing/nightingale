# 现有模板分类与接入迁移计划

日期：2026-10-30。配套 [设计方案](README.md) 与 [交互原型](prototype.html)。本清单替换旧的 16 类通用表单方案；不执行真实迁移，也不表示所列组件已经支持自动接入。

## 分类原则与实施批次

每行是当前原生组件的迁移落点。路径中的“待核验版本 / 型号”是盘点队列，**不是可发布的兼容性声明**。完成产品版本、采集能力与指标验证后再拆成可供设备选择的具体包。保留原组件 ID / ident，多个版本包可关联同一原生组件资源，但各自固定发布快照。

- **P0：目录与现有 SNMP 基础。** 分类树、模板包、动态字段、发布校验；Windows_SNMP / Linux_SNMP 的 v2c 兼容迁入，原设备和资源快照保留。自建组件先核验，不自动发布。
- **P1：指定场景。** MySQL 版本包；Windows / Linux 的明确适用范围；新增 Dell R740、HPE DL380 Gen10 和 Lenovo SR650 硬件包。硬件包是新建并验证，不能把通用 Redfish 资料直接改名交付。SNMP v3 也需实际适配和验证。
- **P2：其他远程服务。** 数据库、缓存、网络、应用、拨测、存储；按产品逐项实现和验收，禁止只加一个账号密码表单就标为可用。
- **P3：需要执行位置 / 发现机制的类型。** 本机插件、集群、云产品、推送与关联类型，分别设计执行位置、资源发现和权限模型。

每个包的出队门槛相同：字段 Schema → 适配器与插件能力 → 采集说明与指标契约 → 独立仪表盘 → 告警及通知预设 → 目标版本样本验证 → 草稿发布。缺仪表盘、规则或指标说明时先补资源；通知预设均需新增结构化定义，并引用原生媒介与逻辑接收角色。

原型演示 MySQL 5.7 / 8.0 / 8.4、SQL Server 2019 / 2022、Oracle 19c 等三级路径，这些是分类和表单示例，不是已验证的支持矩阵。没有列入首批的产品版本不被禁止，管理员可在验证后继续添加分类与包。

## 盘点口径

数据来自 2026-10-28 已保留的模板盘点快照：89 个默认组件、2 个自建组件，共 91 项。资源数仅用于安排补齐工作，未重新验证每条查询、采集示例或告警规则。`采集资料` 表示盘点时存在采集说明 / 示例证据，不能证明受管下发、自动重载、凭据保护、设备标签隔离或真实采集已实现。

表中资源列依次为：采集资料 / 指标说明条数 / 仪表盘份数 / 告警条数。独立指标说明为 0 时，Markdown 可能仍含介绍，发布前仍需建立可校验的指标契约。原自建组件名称不规范时保留原名，待归类不丢弃。


## 操作系统（4 项；原盘点分组：主机与操作系统）

已装 Agent 关联 / 远程 SNMP 分成不同包；SNMP v2c 基础为 P0。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [Linux](../../../integrations/Linux/markdown/README.md) | 操作系统 → Linux → Agent 基础包（版本待核验） | P2 | 有 / 132 / 5 / 60 | 核验适配器与版本覆盖 |
| [Linux_SNMP](../../../integrations/Linux_SNMP/markdown/README.md) | 操作系统 → Linux → 通用 SNMP（再拆已验证版本） | P0 → P1 | 有 / 6 / 1 / 4 | 核验适配器与版本覆盖；保留 v2c 设备，明确 v3 待实现 |
| [Windows](../../../integrations/Windows/markdown/README.md) | 操作系统 → Windows → Agent 基础包（版本待核验） | P2 | 待补 / 0 / 6 / 14 | 需补结构化采集定义；缺独立指标说明 |
| [Windows_SNMP](../../../integrations/Windows_SNMP/markdown/README.md) | 操作系统 → Windows → 通用 SNMP（再拆已验证版本） | P0 → P1 | 有 / 6 / 1 / 4 | 核验适配器与版本覆盖；保留 v2c 设备，明确 v3 待实现 |

## 数据库（7 项；原盘点分组：关系型数据库）

数据库地址、端口、认证、实例范围；每个产品单独定义适配器。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [Dameng](../../../integrations/Dameng/markdown/dameng.md) | 数据库 → Dameng → 待核验版本 | P2 | 有 / 11 / 1 / 4 | 核验适配器与版本覆盖 |
| [Greenplum](../../../integrations/Greenplum/markdown/README.md) | 数据库 → Greenplum → 待核验版本 | P2 | 有 / 0 / 0 / 2 | 缺独立指标说明；无仪表盘；采集文件仅有间隔注释，连接契约需补齐 |
| [Kingbase](../../../integrations/Kingbase/markdown/kingbase.md) | 数据库 → Kingbase → 待核验版本 | P2 | 待补 / 0 / 1 / 0 | 需补结构化采集定义；缺独立指标说明；无告警规则；文档指向 Prometheus，端点及指标契约待确认 |
| [MySQL](../../../integrations/MySQL/markdown/README.md) | 数据库 → MySQL → 先验证 8.0，再扩展其他版本 | P1 | 有 / 31 / 7 / 20 | 核验适配器与版本覆盖 |
| [Oracle](../../../integrations/Oracle/markdown/README.md) | 数据库 → Oracle → 待核验版本 | P2 | 有 / 0 / 1 / 5 | 缺独立指标说明 |
| [PostgreSQL](../../../integrations/PostgreSQL/markdown/README.md) | 数据库 → PostgreSQL → 待核验版本 | P2 | 有 / 0 / 1 / 5 | 缺独立指标说明 |
| [SQLServer](../../../integrations/SQLServer/markdown/README.md) | 数据库 → SQLServer → 待核验版本 | P2 | 有 / 0 / 1 / 11 | 缺独立指标说明 |

## 数据库（4 项；原盘点分组：分析与分布式数据库）

区分单实例与集群，逐角色配置 SQL / 指标端点。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [ClickHouse](../../../integrations/ClickHouse/markdown/README.md) | 数据库 → ClickHouse → 待核验版本与部署方式 | P2 | 有 / 81 / 2 / 10 | 核验适配器与版本覆盖 |
| [Doris](../../../integrations/Doris/markdown/README.md) | 数据库 → Doris → 待核验版本与部署方式 | P2 | 有 / 0 / 1 / 21 | 缺独立指标说明 |
| [TDEngine](../../../integrations/TDEngine/markdown/README.md) | 数据库 → TDEngine → 待核验版本与部署方式 | P2 | 有 / 0 / 1 / 0 | 缺独立指标说明；无告警规则 |
| [TiDB](../../../integrations/TiDB/markdown/README.md) | 数据库 → TiDB → 待核验版本与部署方式 | P2 | 有 / 0 / 1 / 28 | 缺独立指标说明 |

## 缓存、文档与搜索（3 项；原盘点分组：缓存、文档与搜索）

按产品定义协议、认证库 / ACL、TLS 与资源范围。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [Elasticsearch](../../../integrations/Elasticsearch/markdown/README.md) | 缓存、文档与搜索 → Elasticsearch → 待核验版本 | P2 | 有 / 30 / 5 / 22 | 核验适配器与版本覆盖 |
| [MongoDB](../../../integrations/MongoDB/markdown/README.md) | 缓存、文档与搜索 → MongoDB → 待核验版本 | P2 | 有 / 0 / 1 / 8 | 缺独立指标说明 |
| [Redis](../../../integrations/Redis/markdown/README.md) | 缓存、文档与搜索 → Redis → 待核验版本 | P2 | 有 / 0 / 3 / 13 | 缺独立指标说明 |

## 消息中间件（5 项；原盘点分组：消息队列与 MQTT）

按实际插件区分 Broker、管理 API、SASL 与指标端点。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [AutoMQ](../../../integrations/AutoMQ/markdown/overview.md) | 消息中间件 → AutoMQ → 待核验版本 | P2 | 有 / 59 / 5 / 12 | 核验适配器与版本覆盖 |
| [EMQX](../../../integrations/EMQX/markdown/README.md) | 消息中间件 → EMQX → 待核验版本 | P2 | 有 / 0 / 0 / 9 | 缺独立指标说明；无仪表盘 |
| [Kafka](../../../integrations/Kafka/markdown/README.md) | 消息中间件 → Kafka → 待核验版本 | P2 | 有 / 6 / 2 / 5 | 核验适配器与版本覆盖 |
| [NSQ](../../../integrations/NSQ/markdown/README.md) | 消息中间件 → NSQ → 待核验版本 | P2 | 有 / 0 / 0 / 5 | 缺独立指标说明；无仪表盘 |
| [RabbitMQ](../../../integrations/RabbitMQ/markdown/README.md) | 消息中间件 → RabbitMQ → 待核验版本 | P2 | 有 / 0 / 4 / 6 | 缺独立指标说明 |

## 应用与 Web 服务（8 项；原盘点分组：Web 与应用服务）

状态页 / 指标接口 / 本机模式分别建包，不共用空白账号密码表单。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [Apache](../../../integrations/Apache/markdown/README.md) | 应用与 Web 服务 → Apache → 待核验版本 | P2 | 有 / 0 / 0 / 5 | 缺独立指标说明；无仪表盘 |
| [Gitlab](../../../integrations/Gitlab/markdown/README.md) | 应用与 Web 服务 → Gitlab → 待核验版本 | P2 | 有 / 0 / 5 / 9 | 缺独立指标说明 |
| [HAProxy](../../../integrations/HAProxy/markdown/README.md) | 应用与 Web 服务 → HAProxy → 待核验版本 | P2 | 有 / 0 / 1 / 8 | 缺独立指标说明 |
| [IIS](../../../integrations/IIS/markdown/README.md) | 应用与 Web 服务 → IIS → 待核验版本 | P2 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| [Jenkins](../../../integrations/Jenkins/markdown/README.md) | 应用与 Web 服务 → Jenkins → 待核验版本 | P2 | 有 / 0 / 0 / 5 | 缺独立指标说明；无仪表盘 |
| [Nginx](../../../integrations/Nginx/markdown/README.md) | 应用与 Web 服务 → Nginx → 待核验版本 | P2 | 有 / 7 / 3 / 3 | 核验适配器与版本覆盖 |
| [PHP](../../../integrations/PHP/markdown/README.md) | 应用与 Web 服务 → PHP → 待核验版本 | P2 | 有 / 0 / 0 / 5 | 缺独立指标说明；无仪表盘 |
| [Tomcat](../../../integrations/Tomcat/markdown/README.md) | 应用与 Web 服务 → Tomcat → 待核验版本 | P2 | 有 / 0 / 1 / 6 | 缺独立指标说明 |

## 应用运行时（3 项；原盘点分组：Java 与 JMX）

JMX Exporter、Jolokia、Actuator 分开；检查目标侧依赖。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [Java](../../../integrations/Java/markdown/README.md) | 应用运行时 → Java → 待核验接入方式 | P2 | 有 / 0 / 3 / 0 | 缺独立指标说明；无告警规则 |
| [Jolokia_Agent](../../../integrations/Jolokia_Agent/markdown/README.md) | 应用运行时 → Jolokia_Agent → 待核验接入方式 | P2 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| [SpringBoot](../../../integrations/SpringBoot/markdown/README.md) | 应用运行时 → SpringBoot → 待核验接入方式 | P2 | 有 / 0 / 2 / 5 | 缺独立指标说明 |

## 容器与编排（3 项；原盘点分组：容器与 Kubernetes）

区分本机 Agent 与集群发现；补全身份、范围和 RBAC。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [Docker](../../../integrations/Docker/markdown/README.md) | 容器与编排 → Docker → 待核验版本与部署方式 | P3 | 有 / 0 / 1 / 6 | 缺独立指标说明 |
| [Kubernetes](../../../integrations/Kubernetes/markdown/README.md) | 容器与编排 → Kubernetes → 待核验版本与部署方式 | P3 | 有 / 305 / 17 / 154 | 核验适配器与版本覆盖 |
| [cAdvisor](../../../integrations/cAdvisor/markdown/README.md) | 容器与编排 → cAdvisor → 待核验版本与部署方式 | P3 | 有 / 45 / 1 / 6 | 核验适配器与版本覆盖 |

## 存储与虚拟化（4 项；原盘点分组：存储与虚拟化）

服务端点、凭据、资源范围；集群 / 实例粒度需显式定义。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [Ceph](../../../integrations/Ceph/markdown/README.md) | 存储与虚拟化 → Ceph → 待核验版本 | P2 | 有 / 0 / 1 / 15 | 缺独立指标说明 |
| [MinIO](../../../integrations/MinIO/markdown/README.md) | 存储与虚拟化 → MinIO → 待核验版本 | P2 | 有 / 0 / 2 / 2 | 缺独立指标说明 |
| [XSKYApi](../../../integrations/XSKYApi/markdown/README.md) | 存储与虚拟化 → XSKYApi → 待核验版本 | P2 | 有 / 0 / 0 / 3 | 缺独立指标说明；无仪表盘 |
| [vSphere](../../../integrations/vSphere/markdown/README.md) | 存储与虚拟化 → vSphere → 待核验版本 | P2 | 有 / 0 / 2 / 5 | 缺独立指标说明 |

## 网络设备（4 项；原盘点分组：网络设备与带外硬件）

按设备系列验证协议、OID / 资源路径与指标单位。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [IPMI](../../../integrations/IPMI/markdown/README.md) | 服务器 → 通用 BMC → IPMI 协议资料（待按厂商 / 型号拆包） | P1 → P2 | 有 / 19 / 3 / 4 | 核验适配器与版本覆盖；资源发现、固件兼容、厂商指标映射 |
| [Redfish](../../../integrations/Redfish/markdown/README.md) | 服务器 → 通用 BMC → Redfish 协议资料（待按厂商 / 型号拆包） | P1 → P2 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则；资源发现、固件兼容、厂商指标映射 |
| [SNMP](../../../integrations/SNMP/markdown/README.md) | 网络设备 → 通用设备 → 标准 MIB（待按厂商 / 型号拆包） | P2 | 有 / 5 / 9 / 37 | 核验适配器与版本覆盖 |
| [Switch_Legacy](../../../integrations/Switch_Legacy/markdown/README.md) | 网络设备 → 原交换机资料 → 厂商 / 型号待核验 | P2 | 有 / 0 / 1 / 7 | 缺独立指标说明 |

## 云平台与 SaaS（10 项；原盘点分组：云平台与 SaaS）

先按云产品 / API 建包，再按区域与资源范围配置，不假设为硬件型号。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [Akamai](../../../integrations/Akamai/markdown/akamai.md) | 云平台与 SaaS → Akamai → 待拆分产品包 | P3 | 有 / 0 / 1 / 4 | 缺独立指标说明 |
| [AliYun](../../../integrations/AliYun/markdown/README.md) | 云平台与 SaaS → AliYun → 待拆分产品包 | P3 | 有 / 0 / 44 / 73 | 缺独立指标说明 |
| [Azure](../../../integrations/Azure/markdown/azure.md) | 云平台与 SaaS → Azure → 待拆分产品包 | P3 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| [CloudWatch](../../../integrations/CloudWatch/markdown/README.md) | 云平台与 SaaS → CloudWatch → 待拆分产品包 | P3 | 有 / 0 / 1 / 8 | 缺独立指标说明 |
| [Cloudflare](../../../integrations/Cloudflare/markdown/cloudflare.md) | 云平台与 SaaS → Cloudflare → 待拆分产品包 | P3 | 有 / 0 / 1 / 3 | 缺独立指标说明 |
| [GoogleCloud](../../../integrations/GoogleCloud/markdown/README.md) | 云平台与 SaaS → GoogleCloud → 待拆分产品包 | P3 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| Huawei | 云平台与 SaaS → Huawei → 待拆分产品包 | P3 | 待补 / 286 / 14 / 35 | 缺采集说明；需补结构化采集定义 |
| [Meraki](../../../integrations/Meraki/markdown/README.md) | 云平台与 SaaS → Meraki → 待拆分产品包 | P3 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| [OpenStack](../../../integrations/OpenStack/markdown/README.md) | 云平台与 SaaS → OpenStack → 待拆分产品包 | P3 | 有 / 0 / 0 / 5 | 缺独立指标说明；无仪表盘 |
| [Tencent](../../../integrations/Tencent/markdown/readme.md) | 云平台与 SaaS → Tencent → 待拆分产品包 | P3 | 待补 / 0 / 7 / 0 | 需补结构化采集定义；缺独立指标说明；无告警规则；需补云监控采集凭据与调用定义 |

## 拨测与外部目标（7 项；原盘点分组：网络拨测、域名与证书）

分别使用 URL、域名、IP、端口、证书参数，不强制账户密码。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [CDN](../../../integrations/CDN/markdown/README.md) | 拨测与外部目标 → CDN → 协议基础包 | P2 | 有 / 0 / 0 / 4 | 缺独立指标说明；无仪表盘 |
| [Dns_Query](../../../integrations/Dns_Query/markdown/README.md) | 拨测与外部目标 → Dns_Query → 协议基础包 | P2 | 有 / 5 / 0 / 4 | 无仪表盘 |
| [HTTP_Response](../../../integrations/HTTP_Response/markdown/http.md) | 拨测与外部目标 → HTTP_Response → 协议基础包 | P2 | 有 / 11 / 1 / 2 | 核验适配器与版本覆盖 |
| [Net_Response](../../../integrations/Net_Response/markdown/README.md) | 拨测与外部目标 → Net_Response → 协议基础包 | P2 | 有 / 2 / 2 / 1 | 核验适配器与版本覆盖 |
| [Ping](../../../integrations/Ping/markdown/README.md) | 拨测与外部目标 → Ping → 协议基础包 | P2 | 有 / 6 / 2 / 1 | 核验适配器与版本覆盖 |
| [Whois](../../../integrations/Whois/markdown/README.md) | 拨测与外部目标 → Whois → 协议基础包 | P2 | 有 / 0 / 0 / 3 | 缺独立指标说明；无仪表盘 |
| [X509_Cert](../../../integrations/X509_Cert/markdown/README.md) | 拨测与外部目标 → X509_Cert → 协议基础包 | P2 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |

## 主机专项（12 项；原盘点分组：主机专项与本地插件）

绑定设备本机采集器、目标路径 / 进程 / 硬件；不能远程中央执行代替。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [AMD_ROCm_SMI](../../../integrations/AMD_ROCm_SMI/markdown/README.md) | 主机专项 → AMD_ROCm_SMI → 本机 Agent 模式 | P3 | 有 / 0 / 0 / 4 | 缺独立指标说明；无仪表盘 |
| [Exec](../../../integrations/Exec/markdown/README.md) | 主机专项 → Exec → 本机 Agent 模式 | P3 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| [Filecount](../../../integrations/Filecount/markdown/README.md) | 主机专项 → Filecount → 本机 Agent 模式 | P3 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| HUATUO | 主机专项 → HUATUO → 本机 Agent 模式 | P3 | 有 / 251 / 3 / 7 | 缺采集说明 |
| [IPVS](../../../integrations/IPVS/markdown/README.md) | 主机专项 → IPVS → 本机 Agent 模式 | P3 | 有 / 0 / 0 / 1 | 缺独立指标说明；无仪表盘 |
| [Mtail](../../../integrations/Mtail/markdown/README.md) | 主机专项 → Mtail → 本机 Agent 模式 | P3 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| [NFSClient](../../../integrations/NFSClient/markdown/README.md) | 主机专项 → NFSClient → 本机 Agent 模式 | P3 | 有 / 0 / 0 / 3 | 缺独立指标说明；无仪表盘 |
| [NVIDIA](../../../integrations/NVIDIA/markdown/README.md) | 主机专项 → NVIDIA → 本机 Agent 模式 | P3 | 有 / 38 / 3 / 2 | 核验适配器与版本覆盖 |
| [Netstat_Filter](../../../integrations/Netstat_Filter/markdown/README.md) | 主机专项 → Netstat_Filter → 本机 Agent 模式 | P3 | 有 / 0 / 0 / 3 | 缺独立指标说明；无仪表盘 |
| [Procstat](../../../integrations/Procstat/markdown/readme.md) | 主机专项 → Procstat → 本机 Agent 模式 | P3 | 有 / 21 / 1 / 2 | 核验适配器与版本覆盖 |
| [SMART](../../../integrations/SMART/markdown/README.md) | 主机专项 → SMART → 本机 Agent 模式 | P3 | 有 / 14 / 1 / 6 | 核验适配器与版本覆盖 |
| [Systemd](../../../integrations/Systemd/markdown/README.md) | 主机专项 → Systemd → 本机 Agent 模式 | P3 | 有 / 12 / 1 / 3 | 核验适配器与版本覆盖 |

## 基础服务（6 项；原盘点分组：基础中间件与服务）

按照服务协议与采集实现生成字段，认证和权限单独校验。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [Bind](../../../integrations/Bind/markdown/README.md) | 基础服务 → Bind → 待核验版本 | P2 | 有 / 0 / 0 / 5 | 缺独立指标说明；无仪表盘 |
| [Canal](../../../integrations/Canal/markdown/README.md) | 基础服务 → Canal → 待核验版本 | P2 | 有 / 0 / 1 / 0 | 缺独立指标说明；无告警规则 |
| [Consul](../../../integrations/Consul/markdown/README.md) | 基础服务 → Consul → 待核验版本 | P2 | 有 / 0 / 0 / 7 | 缺独立指标说明；无仪表盘 |
| [Ldap](../../../integrations/Ldap/markdown/README.md) | 基础服务 → Ldap → 待核验版本 | P2 | 有 / 0 / 0 / 5 | 缺独立指标说明；无仪表盘 |
| [Radius](../../../integrations/Radius/markdown/README.md) | 基础服务 → Radius → 待核验版本 | P2 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| [ZooKeeper](../../../integrations/ZooKeeper/markdown/README.md) | 基础服务 → ZooKeeper → 待核验版本 | P2 | 有 / 0 / 1 / 4 | 缺独立指标说明 |

## 可观测平台（9 项；原盘点分组：指标平台与推送接入）

区分拉取、推送接收和已有数据关联，不能都执行远程抓取。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| [AppDynamics](../../../integrations/AppDynamics/markdown/README.md) | 可观测平台 → AppDynamics → 待核验接入模式 | P3 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| [LLM](../../../integrations/LLM/markdown/README.md) | 可观测平台 → LLM → 待核验接入模式 | P3 | 待补 / 0 / 2 / 0 | 需补结构化采集定义；缺独立指标说明；无告警规则 |
| [Logstash](../../../integrations/Logstash/markdown/README.md) | 可观测平台 → Logstash → 待核验接入模式 | P3 | 有 / 0 / 1 / 6 | 缺独立指标说明 |
| [N9E](../../../integrations/N9E/markdown/README.md) | 可观测平台 → N9E → 待核验接入模式 | P3 | 有 / 0 / 3 / 0 | 缺独立指标说明；无告警规则 |
| [Prometheus](../../../integrations/Prometheus/markdown/README.md) | 可观测平台 → Prometheus → 待核验接入模式 | P3 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| [RocketMQ](../../../integrations/RocketMQ/markdown/README.md) | 消息中间件 → RocketMQ → 版本 / 接入方式待核验 | P2 | 有 / 0 / 0 / 0 | 缺独立指标说明；无仪表盘；无告警规则 |
| Skywalking | 可观测平台 → Skywalking → 待核验接入模式 | P3 | 待补 / 0 / 0 / 0 | 缺采集说明；需补结构化采集定义；缺独立指标说明；无仪表盘；无告警规则 |
| [VictoriaMetrics](../../../integrations/VictoriaMetrics/markdown/README.md) | 可观测平台 → VictoriaMetrics → 待核验接入模式 | P3 | 有 / 0 / 2 / 9 | 缺独立指标说明 |
| [Zabbix](../../../integrations/Zabbix/markdown/zabbix.md) | 可观测平台 → Zabbix → 待核验接入模式 | P3 | 有 / 0 / 1 / 0 | 缺独立指标说明；无告警规则 |

## 待归类（2 项；原盘点分组：自建模板与待归类）

保留 ID 与原内容，人工确认类型、适配器、资源及凭据引用后再发布。

| 原组件 | 拟分类路径 | 批次 | 资源：采集 / 指标 / 盘 / 规则 | 补齐重点 |
|---|---|---|---|---|
| jjj | 待归类 → jjj → 原自建组件 | P0 | 待补 / 0 / 0 / 0 | 自建内容待单独核对；未定义通用添加参数映射 |
| Windows (snmp) | 待归类 → Windows (snmp) → 原自建组件 | P0 | 待补 / 0 / 0 / 0 | 自建内容待单独核对；未定义通用添加参数映射 |

## 新增硬件型号包与验收

| 新包路径 | 字段来源 | 必须验证的内容 |
|---|---|---|
| 服务器 → 戴尔 → PowerEdge R740 | iDRAC 地址、端口、账号密码、证书与采集范围 | 实际 iDRAC 型号 / 固件；系统、机箱、热、电源、存储资源发现；指标契约及硬件规则 |
| 服务器 → 惠普 → ProLiant DL380 Gen10 | iLO 地址、端口、账号密码、证书与采集范围 | 实际 iLO 代际 / 固件；HPE 资源发现和传感器映射；部件健康与冗余 |
| 服务器 → 联想 → ThinkSystem SR650 | XCC 地址、端口、账号密码、证书与采集范围 | 实际控制器代际 / 固件；Lenovo 资源发现与指标；硬件选件差异 |

这些型号包单独维护采集定义、仪表盘与告警通知方案。新增设备表单由包的 Schema 生成，不以厂商名切换写死前端组件。

## 迁移约束

先以资料模式展示未适配组件，补齐并发布后才进入“新增设备”的可选模板。不要通过给旧 91 项批量套用一个通用表单来完成迁移。现有设备继续使用旧快照；公共模板发布后只提示有新版本，不自动覆盖设备专属采集参数、仪表盘、规则与通知。

旧清单源文件保存在 [归档目录](../device-onboarding-prototype/archive/2026-10-28/catalog.json)，旧界面和部署脚本一并归档作为历史证据。当前设计文档、原型和迁移计划以本目录为准。
