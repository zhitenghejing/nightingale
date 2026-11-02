# 设备添加分类与配置原型

盘点日期：2026-10-28。范围：当前本地模板中心的 91 个组件，其中 89 个来自源码，2 个来自当前数据库自建记录。本轮只增加独立设计资料和原型，业务代码、数据库 Schema、采集配置及现有设备不做功能修改。

## 交互问题与入口

评审问题：选择对象类型后，是否能找到正确的模板，并填写满足该模板的采集参数；应用模板时，是否能明确知道将采集什么、生成哪些仪表盘和规则。

主流程：**选择设备 / 服务类型 → 选择匹配模板 → 填写按采集方式生成的参数 → 校验 → 选择仪表盘与告警 → 预览添加结果**。四步原型把“校验”放在参数页，把“选择应用内容”单独列为一步。

打开 [prototype.html](prototype.html) 即可离线使用。运行 `python3 doc/design/device-onboarding-prototype/publish.py` 可把独立 HTML 随当前业务 UI 一起交给预览管理器发布，原业务页面继续保留；不会改动 `frontend/pub` 或业务源码。发布后入口为 `http://192.168.0.152:30304/prototype/device-onboarding.html`。每一类都有独立 `?view=form&type=<id>` 页面链接，同一类型的不同模板仍会改变表单字段。浏览器内的数据仅保留在内存，按钮不调用业务 API。

## 类型分类与页面

| 类型 | 当前模板 | 主要配置内容 | 阶段 |
|---|---|---|---|
| [主机与操作系统](prototype.html?view=form&type=host) | `Linux`, `Linux_SNMP`, `Windows`, `Windows_SNMP` | Agent：目标主机 Agent、采集模块；SNMP：IPv4、端口、v2c、团体字、超时。Windows / Linux 按 OS 匹配。 | P1 |
| [关系型数据库](prototype.html?view=form&type=database) | `Dameng`, `Greenplum`, `Kingbase`, `MySQL`, `Oracle`, `PostgreSQL`, `SQLServer` | MySQL / PostgreSQL / SQLServer / Oracle / Dameng：地址、端口、监控账号、密码、数据库或 Service / SID、TLS。Greenplum 为待补齐表单；Kingbase 按现有说明走 Prometheus 端点。 | P1 |
| [分析与分布式数据库](prototype.html?view=form&type=analytics) | `ClickHouse`, `Doris`, `TDEngine`, `TiDB` | ClickHouse 为 HTTP/SQL 接口和账号；TiDB / Doris 为角色 + 多节点指标端点 + 集群标签；TDEngine 为 taosKeeper。 | P2 |
| [缓存、文档与搜索](prototype.html?view=form&type=data-services) | `Elasticsearch`, `MongoDB`, `Redis` | Redis：单机 / Sentinel、ACL 用户、可选密码；MongoDB：节点、副本集、认证库、账号；ES：HTTP API、认证、节点 / 索引范围。 | P1 |
| [消息队列与 MQTT](prototype.html?view=form&type=messaging) | `AutoMQ`, `EMQX`, `Kafka`, `NSQ`, `RabbitMQ` | Kafka：Broker 列表、SASL、TLS、消费组；RabbitMQ 区分 Management API / Prometheus；EMQX 为版本和 API 凭据；AutoMQ / NSQ 各自端点。 | P2 |
| [Web 与应用服务](prototype.html?view=form&type=web) | `Apache`, `Gitlab`, `HAProxy`, `IIS`, `Jenkins`, `Nginx`, `PHP`, `Tomcat` | Nginx / Apache / HAProxy / PHP：状态接口；Tomcat / Jenkins：管理监控接口和账号 / Token；Gitlab：指标端点；IIS：Windows 本机 Agent + 站点。 | P1 |
| [Java 与 JMX](prototype.html?view=form&type=java) | `Java`, `Jolokia_Agent`, `SpringBoot` | Java：JMX Exporter；Jolokia：URL、认证、MBean 子模板；SpringBoot：Actuator Prometheus 端点和应用标签。 | P2 |
| [容器与 Kubernetes](prototype.html?view=form&type=container) | `Docker`, `Kubernetes`, `cAdvisor` | Docker：本机 Agent + socket / 受保护 API；cAdvisor：kubelet / 独立端点和 Token；Kubernetes：API Server、SA Token、CA、组件端点与 cluster 标签。 | P2 |
| [存储与虚拟化](prototype.html?view=form&type=storage) | `Ceph`, `MinIO`, `XSKYApi`, `vSphere` | Ceph / MinIO：指标端点与所需监控凭据；XSKY：XMS 地址、Token、存储类型；vSphere：vCenter、只读账号、数据中心和对象范围。 | P2 |
| [网络设备与带外硬件](prototype.html?view=form&type=network) | `IPMI`, `Redfish`, `SNMP`, `Switch_Legacy` | SNMP：厂商 / 型号 OID 方案 + 团体字；Switch_Legacy：v2c + 范围；IPMI：BMC、账号、driver、工具路径；Redfish：管理 API + 只读凭据。 | P2 |
| [云平台与 SaaS](prototype.html?view=form&type=cloud) | `Akamai`, `AliYun`, `Azure`, `CloudWatch`, `Cloudflare`, `GoogleCloud`, `Huawei`, `Meraki`, `OpenStack`, `Tencent` | 云账户凭据、Region / Project / Subscription、Namespace / 产品、资源范围；不同云分别渲染 IAM、AK/SK、OAuth Client 或服务账号文件。Cloudflare / Meraki / Akamai 使用自身 API 凭据。 | P3 |
| [网络拨测、域名与证书](prototype.html?view=form&type=probes) | `CDN`, `Dns_Query`, `HTTP_Response`, `Net_Response`, `Ping`, `Whois`, `X509_Cert` | 按探测方式填写 URL、域名、DNS 服务器、IP / 主机名、TCP/UDP 端口、TLS/SNI、预期响应；不统一强制密码。 | P1 |
| [主机专项与本地插件](prototype.html?view=form&type=local) | `AMD_ROCm_SMI`, `Exec`, `Filecount`, `HUATUO`, `IPVS`, `Mtail`, `NFSClient`, `NVIDIA`, `Netstat_Filter`, `Procstat`, `SMART`, `Systemd` | 先绑定目标机器 Agent，再填 GPU 工具路径、进程匹配、systemd unit、目录、日志程序、磁盘设备或挂载点。Exec 需要另行审批和执行隔离。 | P3 |
| [基础中间件与服务](prototype.html?view=form&type=infra) | `Bind`, `Canal`, `Consul`, `Ldap`, `Radius`, `ZooKeeper` | Consul ACL Token、LDAP Bind DN / 密码、RADIUS shared secret、BIND XML 统计端点、ZooKeeper 原生节点和 Canal metrics。 | P2 |
| [指标平台与推送接入](prototype.html?view=form&type=observability) | `AppDynamics`, `LLM`, `Logstash`, `N9E`, `Prometheus`, `RocketMQ`, `Skywalking`, `VictoriaMetrics`, `Zabbix` | 抓取自身指标、关联已有数据源、接收推送三种路径。RocketMQ 为 4317 接收通道；Zabbix 为 HTTP receiver + API 元信息；LLM 不收模型厂商 Key；Skywalking 为空组件。 | P3 |
| [自建模板与待归类](prototype.html?view=form&type=custom) | `jjj`, `Windows (snmp)` | 保留当前 jjj、Windows (snmp)；先核对原内容，再映射对象类别、采集协议和参数，不根据名称直接覆盖或迁移。 | P0 |

## 为什么不把所有模板都变成 IP + 密码

- 同一个 Linux 对象可以使用 Agent 或 SNMP；“Linux”是对象类型，“SNMP”是采集方式，二者不是同一层。类型匹配的是一组可用模板，模板再决定凭据和采集参数。
- 数据库实例与所在主机应关联，不能因为增加同一 IP 的 MySQL、Redis 就重复创造物理资产。IP 不作为唯一标识，单一采集器可以采多个目标。
- 本地插件必须绑定目标主机自己的 Agent。中央采集器的 `/var/log`、GPU、Docker socket 不是远程主机的数据。
- 云账户、Kubernetes 集群、存储集群应先确认资源范围或节点角色，不假定一个 IP 等于整个对象。
- 推送接收端属于采集通道。RocketMQ / Zabbix 的监听端口不能被当成远程 Broker 的抓取端口；多个来源应复用接收通道并按标签绑定对象。
- 关联既有数据源时先确认已有指标和标签；添加数据源本身不会自动产生指标。

## 模板中心需要增加的接入描述（仅规划）

继续沿用原生采集说明、指标说明、仪表盘和告警规则四部分。未来在组件编辑的基础信息中描述：对象类别、支持的采集方式、字段 Schema、凭据种类、执行位置（本机 / 远程 / 监听 / 云）、前置依赖、预期指标与标签、仪表盘和规则适用范围。无需恢复独立 SNMP 目录或页签。

这些描述决定设备添加表单。字段包含必填、默认值、秘密属性、格式、依赖条件与帮助说明；账号密码存凭据引用，前端普通读接口不回显明文。采集器插件编译按允许的适配器执行，不能把任意 collect TOML 当成可直接下发的执行内容。

## 分阶段接入计划

1. **P0：盘点与契约。** 建立本分类映射，补齐采集参数、执行位置、凭据类型、指标名和必需标签；区分已有资源与空组件。核对 2 个自建组件；保留原内容。
2. **P1：高频单实例。** 扩展主机 Agent 与现有 SNMP，再接 MySQL / PostgreSQL / SQLServer、Redis / MongoDB / Elasticsearch、Web 状态接口与主动拨测。建立非 SNMP 凭据与受控插件编译路径，复用独立实例和回滚机制。Greenplum / Kingbase 需先完成各自缺失契约，不能因归属此类就标成已支持。
3. **P2：多节点与协议差异。** TiDB / Doris、消息队列、Kubernetes、存储 / vSphere、JMX、网络和带外硬件。加入组件角色、节点列表、资源选择以及按指标名 / 标签过滤的仪表盘和规则绑定。
4. **P3：资源发现和特殊执行。** 云 API 发现、监听推送、已有指标关联与本机专项。补齐云产品级模板；命令和本地权限类采集单独实现审核、隔离及运行前检查。

每一阶段的完成条件都是：参数合法 → 从选定执行点校验权限和前置条件 → 真实指标匹配模板契约 → 生成可编辑独立仪表盘 / 告警 → 验证告警触发、恢复及停用。保存成功与采集生效分开显示。

## 当前资源完整性

下表的仪表盘按 JSON 文件计数、告警按规则条目计数、指标说明按条目计数。仅反映源码资料，不代表运行时已支持自动接入。自建内容待专门核对，不把未知资源误判为不存在。完整机器可读清单见 [catalog.json](catalog.json)。

| 默认组件 | 采集说明 | collect 文件 | 指标说明 | 仪表盘 | 告警条目 | 需要处理的缺口 |
|---|---|---:|---:|---:|---:|---|
| `AMD_ROCm_SMI` | [有](../../../integrations/AMD_ROCm_SMI/markdown/README.md) | 1 | 0 | 0 | 4 | 缺独立指标说明; 无仪表盘 |
| `Akamai` | [有](../../../integrations/Akamai/markdown/akamai.md) | 1 | 0 | 1 | 4 | 缺独立指标说明 |
| `AliYun` | [有](../../../integrations/AliYun/markdown/README.md) | 1 | 0 | 44 | 73 | 缺独立指标说明 |
| `Apache` | [有](../../../integrations/Apache/markdown/README.md) | 1 | 0 | 0 | 5 | 缺独立指标说明; 无仪表盘 |
| `AppDynamics` | [有](../../../integrations/AppDynamics/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `AutoMQ` | [有](../../../integrations/AutoMQ/markdown/overview.md) | 1 | 59 | 5 | 12 | 现有资源待结构化映射 |
| `Azure` | [有](../../../integrations/Azure/markdown/azure.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `Bind` | [有](../../../integrations/Bind/markdown/README.md) | 1 | 0 | 0 | 5 | 缺独立指标说明; 无仪表盘 |
| `CDN` | [有](../../../integrations/CDN/markdown/README.md) | 1 | 0 | 0 | 4 | 缺独立指标说明; 无仪表盘 |
| `Canal` | [有](../../../integrations/Canal/markdown/README.md) | 0 | 0 | 1 | 0 | 缺独立指标说明; 无告警规则 |
| `Ceph` | [有](../../../integrations/Ceph/markdown/README.md) | 0 | 0 | 1 | 15 | 缺独立指标说明 |
| `ClickHouse` | [有](../../../integrations/ClickHouse/markdown/README.md) | 1 | 81 | 2 | 10 | 现有资源待结构化映射 |
| `CloudWatch` | [有](../../../integrations/CloudWatch/markdown/README.md) | 1 | 0 | 1 | 8 | 缺独立指标说明 |
| `Cloudflare` | [有](../../../integrations/Cloudflare/markdown/cloudflare.md) | 1 | 0 | 1 | 3 | 缺独立指标说明 |
| `Consul` | [有](../../../integrations/Consul/markdown/README.md) | 1 | 0 | 0 | 7 | 缺独立指标说明; 无仪表盘 |
| `Dameng` | [有](../../../integrations/Dameng/markdown/dameng.md) | 2 | 11 | 1 | 4 | 现有资源待结构化映射 |
| `Dns_Query` | [有](../../../integrations/Dns_Query/markdown/README.md) | 1 | 5 | 0 | 4 | 无仪表盘 |
| `Docker` | [有](../../../integrations/Docker/markdown/README.md) | 1 | 0 | 1 | 6 | 缺独立指标说明 |
| `Doris` | [有](../../../integrations/Doris/markdown/README.md) | 1 | 0 | 1 | 21 | 缺独立指标说明 |
| `EMQX` | [有](../../../integrations/EMQX/markdown/README.md) | 1 | 0 | 0 | 9 | 缺独立指标说明; 无仪表盘 |
| `Elasticsearch` | [有](../../../integrations/Elasticsearch/markdown/README.md) | 1 | 30 | 5 | 22 | 现有资源待结构化映射 |
| `Exec` | [有](../../../integrations/Exec/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `Filecount` | [有](../../../integrations/Filecount/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `Gitlab` | [有](../../../integrations/Gitlab/markdown/README.md) | 0 | 0 | 5 | 9 | 缺独立指标说明 |
| `GoogleCloud` | [有](../../../integrations/GoogleCloud/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `Greenplum` | [有](../../../integrations/Greenplum/markdown/README.md) | 1 | 0 | 0 | 2 | 缺独立指标说明; 无仪表盘; 采集文件仅有间隔注释，连接契约需补齐 |
| `HAProxy` | [有](../../../integrations/HAProxy/markdown/README.md) | 1 | 0 | 1 | 8 | 缺独立指标说明 |
| `HTTP_Response` | [有](../../../integrations/HTTP_Response/markdown/http.md) | 1 | 11 | 1 | 2 | 现有资源待结构化映射 |
| `HUATUO` | 缺 | 1 | 251 | 3 | 7 | 缺采集说明 |
| `Huawei` | 缺 | 0 | 286 | 14 | 35 | 缺采集说明; 需补结构化采集定义 |
| `IIS` | [有](../../../integrations/IIS/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `IPMI` | [有](../../../integrations/IPMI/markdown/README.md) | 1 | 19 | 3 | 4 | 现有资源待结构化映射 |
| `IPVS` | [有](../../../integrations/IPVS/markdown/README.md) | 1 | 0 | 0 | 1 | 缺独立指标说明; 无仪表盘 |
| `Java` | [有](../../../integrations/Java/markdown/README.md) | 0 | 0 | 3 | 0 | 缺独立指标说明; 无告警规则 |
| `Jenkins` | [有](../../../integrations/Jenkins/markdown/README.md) | 1 | 0 | 0 | 5 | 缺独立指标说明; 无仪表盘 |
| `Jolokia_Agent` | [有](../../../integrations/Jolokia_Agent/markdown/README.md) | 11 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `Kafka` | [有](../../../integrations/Kafka/markdown/README.md) | 1 | 6 | 2 | 5 | 现有资源待结构化映射 |
| `Kingbase` | [有](../../../integrations/Kingbase/markdown/kingbase.md) | 0 | 0 | 1 | 0 | 需补结构化采集定义; 缺独立指标说明; 无告警规则; 文档指向 Prometheus，端点及指标契约待确认 |
| `Kubernetes` | [有](../../../integrations/Kubernetes/markdown/README.md) | 0 | 305 | 17 | 154 | 现有资源待结构化映射 |
| `LLM` | [有](../../../integrations/LLM/markdown/README.md) | 0 | 0 | 2 | 0 | 需补结构化采集定义; 缺独立指标说明; 无告警规则 |
| `Ldap` | [有](../../../integrations/Ldap/markdown/README.md) | 1 | 0 | 0 | 5 | 缺独立指标说明; 无仪表盘 |
| `Linux` | [有](../../../integrations/Linux/markdown/README.md) | 9 | 132 | 5 | 60 | 现有资源待结构化映射 |
| `Linux_SNMP` | [有](../../../integrations/Linux_SNMP/markdown/README.md) | 0 | 6 | 1 | 4 | 现有资源待结构化映射 |
| `Logstash` | [有](../../../integrations/Logstash/markdown/README.md) | 1 | 0 | 1 | 6 | 缺独立指标说明 |
| `Meraki` | [有](../../../integrations/Meraki/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `MinIO` | [有](../../../integrations/MinIO/markdown/README.md) | 0 | 0 | 2 | 2 | 缺独立指标说明 |
| `MongoDB` | [有](../../../integrations/MongoDB/markdown/README.md) | 2 | 0 | 1 | 8 | 缺独立指标说明 |
| `Mtail` | [有](../../../integrations/Mtail/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `MySQL` | [有](../../../integrations/MySQL/markdown/README.md) | 1 | 31 | 7 | 20 | 现有资源待结构化映射 |
| `N9E` | [有](../../../integrations/N9E/markdown/README.md) | 0 | 0 | 3 | 0 | 缺独立指标说明; 无告警规则 |
| `NFSClient` | [有](../../../integrations/NFSClient/markdown/README.md) | 1 | 0 | 0 | 3 | 缺独立指标说明; 无仪表盘 |
| `NSQ` | [有](../../../integrations/NSQ/markdown/README.md) | 1 | 0 | 0 | 5 | 缺独立指标说明; 无仪表盘 |
| `NVIDIA` | [有](../../../integrations/NVIDIA/markdown/README.md) | 2 | 38 | 3 | 2 | 现有资源待结构化映射 |
| `Net_Response` | [有](../../../integrations/Net_Response/markdown/README.md) | 1 | 2 | 2 | 1 | 现有资源待结构化映射 |
| `Netstat_Filter` | [有](../../../integrations/Netstat_Filter/markdown/README.md) | 1 | 0 | 0 | 3 | 缺独立指标说明; 无仪表盘 |
| `Nginx` | [有](../../../integrations/Nginx/markdown/README.md) | 2 | 7 | 3 | 3 | 现有资源待结构化映射 |
| `OpenStack` | [有](../../../integrations/OpenStack/markdown/README.md) | 1 | 0 | 0 | 5 | 缺独立指标说明; 无仪表盘 |
| `Oracle` | [有](../../../integrations/Oracle/markdown/README.md) | 2 | 0 | 1 | 5 | 缺独立指标说明 |
| `PHP` | [有](../../../integrations/PHP/markdown/README.md) | 1 | 0 | 0 | 5 | 缺独立指标说明; 无仪表盘 |
| `Ping` | [有](../../../integrations/Ping/markdown/README.md) | 1 | 6 | 2 | 1 | 现有资源待结构化映射 |
| `PostgreSQL` | [有](../../../integrations/PostgreSQL/markdown/README.md) | 1 | 0 | 1 | 5 | 缺独立指标说明 |
| `Procstat` | [有](../../../integrations/Procstat/markdown/readme.md) | 2 | 21 | 1 | 2 | 现有资源待结构化映射 |
| `Prometheus` | [有](../../../integrations/Prometheus/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `RabbitMQ` | [有](../../../integrations/RabbitMQ/markdown/README.md) | 2 | 0 | 4 | 6 | 缺独立指标说明 |
| `Radius` | [有](../../../integrations/Radius/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `Redfish` | [有](../../../integrations/Redfish/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `Redis` | [有](../../../integrations/Redis/markdown/README.md) | 2 | 0 | 3 | 13 | 缺独立指标说明 |
| `RocketMQ` | [有](../../../integrations/RocketMQ/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `SMART` | [有](../../../integrations/SMART/markdown/README.md) | 1 | 14 | 1 | 6 | 现有资源待结构化映射 |
| `SNMP` | [有](../../../integrations/SNMP/markdown/README.md) | 3 | 5 | 9 | 37 | 现有资源待结构化映射 |
| `SQLServer` | [有](../../../integrations/SQLServer/markdown/README.md) | 1 | 0 | 1 | 11 | 缺独立指标说明 |
| `Skywalking` | 缺 | 0 | 0 | 0 | 0 | 缺采集说明; 需补结构化采集定义; 缺独立指标说明; 无仪表盘; 无告警规则 |
| `SpringBoot` | [有](../../../integrations/SpringBoot/markdown/README.md) | 0 | 0 | 2 | 5 | 缺独立指标说明 |
| `Switch_Legacy` | [有](../../../integrations/Switch_Legacy/markdown/README.md) | 1 | 0 | 1 | 7 | 缺独立指标说明 |
| `Systemd` | [有](../../../integrations/Systemd/markdown/README.md) | 1 | 12 | 1 | 3 | 现有资源待结构化映射 |
| `TDEngine` | [有](../../../integrations/TDEngine/markdown/README.md) | 0 | 0 | 1 | 0 | 缺独立指标说明; 无告警规则 |
| `Tencent` | [有](../../../integrations/Tencent/markdown/readme.md) | 0 | 0 | 7 | 0 | 需补结构化采集定义; 缺独立指标说明; 无告警规则; 需补云监控采集凭据与调用定义 |
| `TiDB` | [有](../../../integrations/TiDB/markdown/README.md) | 0 | 0 | 1 | 28 | 缺独立指标说明 |
| `Tomcat` | [有](../../../integrations/Tomcat/markdown/README.md) | 1 | 0 | 1 | 6 | 缺独立指标说明 |
| `VictoriaMetrics` | [有](../../../integrations/VictoriaMetrics/markdown/README.md) | 0 | 0 | 2 | 9 | 缺独立指标说明 |
| `Whois` | [有](../../../integrations/Whois/markdown/README.md) | 1 | 0 | 0 | 3 | 缺独立指标说明; 无仪表盘 |
| `Windows` | [有](../../../integrations/Windows/markdown/README.md) | 0 | 0 | 6 | 14 | 需补结构化采集定义; 缺独立指标说明 |
| `Windows_SNMP` | [有](../../../integrations/Windows_SNMP/markdown/README.md) | 0 | 6 | 1 | 4 | 现有资源待结构化映射 |
| `X509_Cert` | [有](../../../integrations/X509_Cert/markdown/README.md) | 1 | 0 | 0 | 0 | 缺独立指标说明; 无仪表盘; 无告警规则 |
| `XSKYApi` | [有](../../../integrations/XSKYApi/markdown/README.md) | 1 | 0 | 0 | 3 | 缺独立指标说明; 无仪表盘 |
| `Zabbix` | [有](../../../integrations/Zabbix/markdown/zabbix.md) | 1 | 0 | 1 | 0 | 缺独立指标说明; 无告警规则 |
| `ZooKeeper` | [有](../../../integrations/ZooKeeper/markdown/README.md) | 1 | 0 | 1 | 4 | 缺独立指标说明 |
| `cAdvisor` | [有](../../../integrations/cAdvisor/markdown/README.md) | 1 | 45 | 1 | 6 | 现有资源待结构化映射 |
| `vSphere` | [有](../../../integrations/vSphere/markdown/README.md) | 1 | 0 | 2 | 5 | 缺独立指标说明 |

## 重要边界与待确认项

- 当前业务实现仍是受管 SNMP v2c，并非本原型列出的全部类型已经接入。页面“模拟通过”只表示示例参数齐全。
- Greenplum 的 collect 文件只有间隔注释；Skywalking 源目录没有可用四部分资源；Kingbase 仅简要指向 Prometheus。它们都不能被一律当作成熟数据库接入。
- RabbitMQ、ZooKeeper、Jolokia、GPU 多采集器等存在指标命名差异。更换采集方式时必须同步匹配模板，不允许“连接成功但图全空”仍显示接入完成。
- 多产品 / 多角色模板默认只预选 1 个仪表盘，告警需按实际范围匹配，不把云平台全部资源一次性生成。
- 自建组件的原文只在本地私有盘点记录中读取，独立 HTML 不嵌入其原文或任何运行凭据。
- 本轮产物是设计评审资料。业务功能、真实连接测试、数据存储、权限、秘密管理和采集适配器均留待方案确认后的实现阶段。
