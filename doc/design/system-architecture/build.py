#!/usr/bin/env python3
"""Generate the editable, dependency-free SVG architecture drawings."""
from html import escape
from pathlib import Path

HERE = Path(__file__).resolve().parent
C = {
    'ink': '#172033', 'muted': '#526074', 'line': '#cbd5e1',
    'purple': '#6d28d9', 'blue': '#0369a1', 'teal': '#0f766e',
    'orange': '#c2410c', 'gray': '#64748b',
}
FILL = {'purple': '#f5f0ff', 'blue': '#eff8ff', 'teal': '#edf9f6',
        'orange': '#fff5ed', 'gray': '#f5f7fa'}


class Diagram:
    def __init__(self, width, height, title, subtitle):
        self.width, self.height, self.parts = width, height, []
        self.parts.append(f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}" role="img" aria-labelledby="title description">')
        self.parts.append(f'<title id="title">{escape(title)}</title><desc id="description">{escape(subtitle)}</desc>')
        self.parts.append('<defs>')
        for name, color in C.items():
            self.parts.append(f'<marker id="arrow-{name}" markerWidth="9" markerHeight="9" refX="8" refY="4.5" orient="auto-start-reverse"><path d="M1 1 L8 4.5 L1 8" fill="none" stroke="{color}" stroke-width="1.5"/></marker>')
        self.parts.append('</defs><style>text{font-family:"Noto Sans CJK SC","Microsoft YaHei",sans-serif} .body{font-size:21px;fill:#334155} .small{font-size:18px;fill:#526074} .heading{font-size:26px;font-weight:600;fill:#172033} .label{font-size:18px;font-weight:500}</style>')
        self.rect(0, 0, width, height, '#ffffff', 'none', 0)
        self.text(48, 64, title, 36, weight=600)
        self.text(48, 105, subtitle, 20, C['muted'])

    def rect(self, x, y, w, h, fill, stroke, radius=14, dash=False):
        self.parts.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{radius}" fill="{fill}" stroke="{stroke}" stroke-width="1.5"'+(' stroke-dasharray="8 6"' if dash else '')+'/>')

    def text(self, x, y, text, size=21, color=None, weight=400, anchor='start'):
        self.parts.append(f'<text x="{x}" y="{y}" font-size="{size}" font-weight="{weight}" fill="{color or C["ink"]}" text-anchor="{anchor}">{escape(str(text))}</text>')

    def lines(self, x, y, lines, size=21, color=None, gap=33):
        for i, line in enumerate(lines):
            self.text(x, y+i*gap, line, size, color)

    def group(self, x, y, w, h, title, subtitle='', kind='gray', dash=False):
        self.rect(x, y, w, h, FILL[kind], C['line'], 20, dash)
        self.text(x+24, y+38, title, 25, C[kind], 600)
        if subtitle:
            self.text(x+24, y+68, subtitle, 18, C['muted'])

    def node(self, x, y, w, h, title, lines, kind='gray', tag='', footer='', dash=False):
        self.rect(x, y, w, h, '#ffffff', C['line'], 13, dash)
        self.rect(x, y+18, 5, h-36, C[kind], 'none', 2)
        self.text(x+22, y+38, title, 25, C['ink'], 600)
        if tag:
            tw = max(72, len(tag)*18+22)
            self.rect(x+w-tw-16, y+15, tw, 28, FILL[kind], 'none', 6)
            self.text(x+w-tw/2-16, y+35, tag, 16, C[kind], 500, 'middle')
        self.lines(x+22, y+78, lines, 21, gap=32)
        if footer:
            self.text(x+22, y+h-20, footer, 17, C[kind])

    def path(self, points, kind='gray', dash=False, start=False):
        path='M'+' L'.join(f'{x} {y}' for x,y in points)
        self.parts.append(f'<path d="{path}" fill="none" stroke="{C[kind]}" stroke-width="2.3" stroke-linejoin="round" marker-end="url(#arrow-{kind})"'+(f' marker-start="url(#arrow-{kind})"' if start else '')+(' stroke-dasharray="7 5"' if dash else '')+'/>')

    def label(self, x, y, label, kind='gray', anchor='middle'):
        w=sum(18 if ord(ch)>127 else 10 for ch in label)+16
        left=x-w/2 if anchor=='middle' else x-8
        self.rect(left, y-19, w, 27, '#ffffff', 'none', 4)
        self.text(x, y, label, 18, C[kind], 500, anchor)

    def legend(self, y):
        for x, kind, name, dash in [(48,'blue','指标写入',False),(280,'teal','查询请求 / 返回',False),(595,'orange','告警事件 / 通知',False),(945,'purple','配置 / 心跳 / 管理',True)]:
            self.path([(x,y-6),(x+58,y-6)],kind,dash)
            self.text(x+72,y,name,19,C['muted'])

    def save(self, name):
        self.parts.append('</svg>')
        (HERE/name).write_text('\n'.join(self.parts)+'\n')


def overview():
    d=Diagram(2540,1780,'夜莺系统全景 · 组件职责与数据流','依据当前 feat/snmp-device-management 源码绘制 · 2026-10-29 · 中央为同一 n9e 进程内的逻辑模块')
    d.legend(145)
    d.group(470,190,1510,1120,'夜莺服务端 n9e / Center','Web、管理 API、接收转发、查询和告警共用一个进程；下列方框不代表独立微服务。','purple')
    d.node(510,285,1430,105,'WebUI + HTTP API',['浏览器管理入口 · 登录 / 业务组授权 · /api/n9e 与 /api/cmdb · /v1/n9e 服务 API'], 'purple')
    d.node(510,430,440,205,'设备与采集管理',['Target 列表、分组、标签、设备详情','SNMP：采集器、加密凭据、探测','启停 / 删除、版本发布、采集健康'], 'blue', '原生＋分支',footer='pkg/collection · pages/hosts')
    d.node(1005,430,440,205,'数据查询与仪表盘',['指标即时 / 区间查询、日志查询','指标视图、快捷视图、记录规则','可编辑仪表盘、变量和图表'], 'teal','原生',footer='datasource · prom · dscache')
    d.node(1500,430,440,205,'规则与事件管理',['告警规则、屏蔽、订阅、通知规则','活跃 / 历史事件、判定执行记录','工作流编辑与执行记录'], 'orange','原生',footer='alert · models · center/router')
    d.node(510,680,440,210,'集成中心',['数据源连接、系统集成 / 嵌入入口','模板：采集说明 / 指标说明','仪表盘 / 告警规则；SNMP 可选包'], 'purple','原生＋分支',footer='其他类型自动接入：仅原型 / 规划')
    d.node(1005,680,440,210,'资产与业务档案',['CMDB：资产、权限、审计、回收站','机柜图：机房 / 机柜、U 位与上架','档案：文档、版本、附件、离线导出'], 'gray','本地扩展',footer='CMDB 尚未自动关联 Target')
    d.node(1500,680,440,210,'Nightingale AI',['Agent 对话、指标 / 日志分析工具','LLM、Skills、任务与确认流程','MCP / A2A 对接外部助手'], 'purple','原生',footer='需配置模型；脚本执行受运行策略限制')
    d.node(510,935,1430,105,'平台治理与运行支撑',['人员 / 团队 / 角色、业务组、SSO / Token、站点与变量配置、操作审计；memsto 配置缓存、dscache 客户端缓存'], 'gray')
    d.node(510,1100,320,165,'Pushgw',['接收 Remote Write 等协议','校验受管设备归属与版本','Writers 转发到指标库'], 'blue')
    d.node(880,1100,320,165,'数据源查询适配',['PromQL / 日志 / SQL','查询代理与客户端缓存','供页面、健康检查和告警用'], 'teal')
    d.node(1250,1100,320,165,'AlertEngine',['规则调度、分片、持续时长','触发 / 恢复、屏蔽、去重','产生事件并进入进程内队列'], 'orange')
    d.node(1620,1100,320,165,'事件处理与发送',['订阅 / 通知规则匹配','可选 Pipeline、消息模板','发送、重试与通知记录'], 'orange')
    d.path([(1200,1180),(1250,1180)],'teal')
    d.path([(1570,1180),(1620,1180)],'orange')
    d.node(48,285,360,105,'用户 / 管理员',['浏览器、HTTP API 客户端'],'gray')
    d.path([(408,337),(510,337)],'purple',True)
    d.node(48,430,360,205,'被监控对象',['Windows / Linux SNMP Agent','交换机等 SNMP 端点（按模板）','本机 OS、数据库、中间件等'], 'blue', footer='受管主机模板：Windows / Linux SNMP')
    d.node(48,700,360,190,'既有采集体系',['Categraf 本地插件 / Exporter','Telegraf、Datadog 等兼容协议','也可直接写已有时序库'], 'blue',footer='并非均由设备新增页自动配置')
    d.node(48,1100,360,165,'Categraf 采集器',['本机插件或远程 SNMP 采集','一个采集器可管理多个 Agent','配置来源：本地 / HTTP Provider'], 'blue')
    d.path([(228,635),(228,700)],'blue')
    d.label(240,672,'实际采集','blue')
    d.path([(228,890),(228,1100)],'blue')
    d.path([(408,1180),(510,1180)],'blue')
    d.label(456,1148,'样本','blue')
    d.path([(510,510),(450,510),(450,1060),(325,1060),(325,1100)],'purple',True)
    d.label(449,1017,'配置 / 心跳','purple')
    d.node(2050,285,442,195,'外部 AI / 自动化客户端',['MCP：调用夜莺工具及现有权限','A2A：委派给内置 AI Agent','接口位于 Center；无需独立 MCP 进程'], 'purple',footer='接口 / 写工具是否启用由配置决定')
    d.path([(2050,337),(1940,337)],'purple',True)
    d.node(2050,680,442,210,'外部依赖 · 模型与身份',['LLM API：推理、工具调用决策','身份提供商：SSO / OAuth（按配置）','模型故障不替代或停止核心告警'], 'gray',footer='AI 模型连接情况不等于功能上线验收')
    d.path([(1940,785),(2050,785)],'purple',True,start=True)
    d.node(2050,1100,442,205,'通知接收方',['钉钉 / 飞书 / 企微 / 邮件等','Webhook、HTTP / 脚本媒介','Flashduty / 第三方事件平台'], 'orange',footer='实际通知渠道需另行配置')
    d.path([(1940,1180),(2050,1180)],'orange')
    d.label(1995,1148,'通知','orange')
    d.group(470,1390,1510,240,'存储边界','指标样本、业务元数据、运行状态和文件分别存储。','gray')
    d.node(510,1478,320,125,'元数据库',['当前 PostgreSQL；用户、规则、','模板、Target、资产、档案元数据'], 'purple')
    d.node(880,1478,320,125,'Redis / 运行缓存',['当前为进程内 miniredis','心跳、会话 / 流状态等'], 'gray')
    d.node(1250,1478,320,125,'指标 / 查询数据存储',['当前 Prometheus；保存时序样本','VM、ES、Loki、CK 等按需接入'], 'teal')
    d.node(1620,1478,320,125,'私有持久化文件',['档案原件 / 导出、Skill 文件','评估日志、密钥文件等'], 'gray')
    d.path([(670,1265),(670,1338),(1410,1338),(1410,1478)],'blue')
    d.label(1020,1338,'Pushgw.Writers：指标写到哪里','blue')
    d.path([(1040,1265),(1040,1374),(1550,1374),(1550,1478)],'teal',start=True)
    d.label(1295,1374,'数据源配置：查询哪里','teal')
    d.path([(705,1310),(705,1478)],'purple',True)
    d.path([(1080,1310),(1080,1478)],'purple',True)
    d.path([(1820,1310),(1820,1478)],'purple',True)
    d.node(48,1390,360,240,'边界说明',['SNMP Agent 不必安装 Categraf','设备表有记录 ≠ 已有指标','收到配置 ≠ 当前采集健康','CMDB 开关不创建告警或采集','告警事件队列位于进程内'], 'gray')
    d.node(2050,1390,442,240,'可选部署能力',['n9e-edge：边缘接收与本地告警','n9e-alert：独立告警执行进程','n9e-pushgw：独立接收转发进程','Ibex：自愈任务 / Agent 执行','本预览未运行这些独立服务'], 'gray',dash=True)
    d.text(48,1690,'原生：夜莺已有功能  ｜  本地扩展：资产 / 档案等  ｜  分支：受管 SNMP 采集  ｜  虚线框：可选部署',21,C['muted'])
    d.text(48,1730,'参考：夜莺官方系统架构与采集数据流图；功能落点以当前工作树源码为准。',20,C['muted'])
    d.save('01-system-overview.svg')


def snmp():
    d=Diagram(2300,1490,'SNMP 设备管理闭环 · 模板、采集、指标与告警','已实现的分支能力 · Windows_SNMP / Linux_SNMP · 配置流与指标数据流分开标示')
    d.legend(145)
    d.group(48,195,2204,330,'① 中心端：模板选择与设备登记','新增 / 编辑设备时绑定模板、采集器和凭据；模板不直接充当每台设备的实时实例。','purple')
    d.node(78,280,460,200,'原生模板中心 · 四部分',['采集说明：SNMP OID / TOML 定义','指标说明：字段及指标名称','仪表盘、告警规则资源'], 'purple',footer='可编辑的原生组件记录')
    d.node(648,280,460,200,'新增 / 编辑采集设备',['IPv4、端口、团体字凭据、采集器','模板、业务组、ident、间隔 / 超时','只校验 IP 格式；不限制网段或 IP 重复'], 'purple',footer='ident 保持对象身份；一个采集器可采多个目标')
    d.node(1218,280,460,200,'事务与模板快照',['加密凭据、不可变模板版本','monitor_device + 原生 Target 登记','独立仪表盘 / 规则实例、发布当前版本'], 'purple',footer='保存失败整体回滚；不留下半成品设备')
    d.node(1788,280,434,200,'设备专属资源',['采集配置 / 指标说明可定制','原生仪表盘 / 告警规则可独立编辑','普通保存保留定制；明确重应用再替换'], 'purple')
    for a,b in [(538,648),(1108,1218),(1678,1788)]: d.path([(a,380),(b,380)],'purple',True)
    d.group(48,590,2204,360,'② 执行与写入：由采集器远程访问 Agent','HTTP Provider 接口只负责发布配置；Categraf 执行 SNMP 请求，Pushgw 负责接收与转发指标。','blue')
    d.node(78,680,460,210,'Center HTTP Provider',['按采集器身份返回版本化配置','独立认证、加密存储、权威撤回','失败不伪装成空配置成功'], 'purple',footer='/v1/n9e/collection/configs')
    d.node(648,680,460,210,'Categraf · 受管任务',['主动拉取 Provider 配置并重载','同一采集器运行多台设备任务','每台设备携带身份与版本标签'], 'blue',footer='由分支兼容的采集器执行配置')
    d.node(1218,680,460,210,'SNMP Agent 设备',['Windows SNMP / Linux Net-SNMP','提供 HOST-RESOURCES / IF MIB','CPU、内存、磁盘、接口、uptime'], 'blue',footer='远程 UDP SNMP v2c；目标机可不安装 Categraf')
    d.node(1788,680,434,210,'Pushgw → Prometheus',['Categraf 通过 Remote Write 上报','校验设备 / 采集器 / 修订号归属','按 Writers 配置转发到真实时序库'], 'blue',footer='样本不是从设备记录或模板生成的模拟数值')
    d.path([(1448,480),(1448,555),(308,555),(308,680)],'purple',True)
    d.label(900,555,'发布完整配置快照','purple')
    d.path([(538,780),(648,780)],'purple',True)
    d.label(594,744,'拉取 / 返回','purple')
    d.path([(1108,760),(1218,760)],'blue',start=True)
    d.label(1162,727,'SNMP','blue')
    d.path([(878,890),(878,922),(2005,922),(2005,890)],'blue')
    d.label(1448,922,'Categraf 将采得的指标写入（不是 Agent 直接 Remote Write）','blue')
    d.group(48,1020,2204,340,'③ 读数据：健康、查询、仪表盘与告警','模板定义如何采；实时查询和告警均读取时序库。仅收到配置不能证明采集正常。','teal')
    d.node(78,1110,460,205,'采集健康与设备详情',['按设备 UUID / 采集器 / 修订过滤','检查样本时间、缺失、时钟及查询错误','设备详情展示真实查询值 / 曲线'], 'teal',footer='旧修订 / 旧采集器样本不能证明当前健康')
    d.node(648,1110,460,205,'指标查询与设备仪表盘',['ident：查询该身份下所有指标','collection_device_id：精确定位设备','独立面板继续读取同一数据源'], 'teal',footer='同 IP 可有多条设备记录，应按设备身份区分')
    d.node(1218,1110,460,205,'原生告警引擎',['CPU / 内存 / 磁盘 >90% 持续5分钟','SNMP 采集中断规则、恢复与去重','按设备及当前修订限定指标'], 'orange',footer='阈值、规则和单设备覆盖均可再调整')
    d.node(1788,1110,434,205,'通知规则与媒介',['告警事件进入原生分发链路','按通知规则、订阅和模板投递','通知渠道 / 接收人需单独配置'], 'orange',footer='生成告警事件不等于已经配置通知接收人')
    d.path([(2005,950),(2005,1097),(308,1097),(308,1110)],'teal')
    d.label(1560,1075,'Prometheus 查询结果 / 当前时间序列','teal')
    d.path([(878,1097),(878,1110)],'teal')
    d.path([(1448,1097),(1448,1110)],'teal')
    d.path([(1678,1210),(1788,1210)],'orange')
    d.label(1732,1177,'事件','orange')
    d.text(48,1410,'生命周期：启用 → 发布 → 观察当前样本；停用 / 删除 → 发布撤回 → 观察旧任务退出。断连时不承诺强制停止。',22,C['muted'])
    d.text(48,1450,'其他数据库 / 云 / 容器等“按类型动态生成接入表单”的能力：已做分类与交互原型，尚未接入真实业务执行链。',22,C['purple'])
    d.save('02-snmp-collection.svg')


def deployment():
    d=Diagram(2140,1550,'当前本地部署与可选扩展','核对日期 2026-10-29 · dev4 隔离预览 · 实线范围为当前部署，虚线框为可选能力')
    d.legend(145)
    d.node(48,200,420,150,'浏览器 / API 客户端',['本地访问 192.168.0.152:30304','同一入口查看业务页面及设计资料'], 'purple')
    d.group(570,190,1520,920,'本地主机 · 192.168.0.152','槽位 4；本实例拥有独立 PostgreSQL 库 / 角色、运行目录、日志、凭据和密钥。','purple')
    d.node(610,290,1440,130,'Preview Gateway · 本地入口 30304',['生命周期管理、健康与构建身份检查；反向代理到实例内部 n9e 端口，提供本地预览访问。'], 'purple')
    d.path([(468,275),(530,275),(530,355),(610,355)],'purple',True)
    d.group(610,480,920,370,'n9e / Center · 单进程','Web API、Provider、Pushgw、查询和告警在同一进程。','purple')
    d.node(640,575,400,145,'管理 / 采集 / AI / 档案',['模板与 Target、受管 SNMP Provider','CMDB、机柜图、文档管理 API'], 'purple')
    d.node(1070,575,430,145,'接收 / 查询 / 告警',['Pushgw → Writers → Prometheus','查询适配 → 判定 → 事件 / 通知'], 'teal')
    d.node(640,748,860,85,'进程内 miniredis',[], 'gray',footer='当前预览运行状态缓存；不是独立 Redis 服务，也不是指标库')
    d.node(1590,480,460,175,'宿主机 PostgreSQL（共享服务）',['每个预览使用独立数据库 / 角色','当前库存模板、规则、Target、资产','不连接稳定实例的生产数据'], 'purple')
    d.node(1590,720,460,175,'宿主机 Prometheus（共享服务）',['存放真实时间序列，供查询 / 告警','本分支显式授权采集写入','内置 EmbeddedTSDB 已禁用'], 'teal')
    d.node(610,925,920,140,'实例专有私有目录',['档案原件 / 导出、Skill 文件、评估日志、配置、凭据和集合密钥','Node CLI / LibreOffice：按档案导出任务启动子进程，无独立文档 HTTP 服务'], 'gray')
    d.path([(1068,420),(1068,480)],'purple',True)
    d.path([(1530,550),(1590,550)],'purple',True)
    d.path([(1530,770),(1590,770)],'teal',start=True)
    d.path([(1500,680),(1560,680),(1560,840),(1590,840)],'blue')
    d.path([(1068,850),(1068,925)],'purple',True)
    d.node(48,480,420,235,'独立部署的 Categraf',['受管 SNMP：拉取当前 Provider','一台采集器访问多台 SNMP Agent','采集样本经本地入口写入 Pushgw','本机插件可继续使用本地配置'], 'blue',footer='配置与指标共用入口，权限分别校验')
    d.path([(640,610),(510,610),(510,545),(468,545)],'purple',True)
    d.label(534,582,'配置','purple')
    d.path([(468,650),(545,650),(545,445),(1285,445),(1285,575)],'blue')
    d.label(887,445,'经 30304 入口进入 Pushgw 的指标写入','blue')
    d.node(48,810,420,150,'SNMP Agent 设备',['现有 Windows / Linux SNMP 目标','只需暴露所选模板需要的 OID'], 'blue')
    d.path([(258,715),(258,810)],'blue',start=True)
    d.label(258,767,'UDP SNMP 请求 / 返回','blue')
    d.node(48,1180,420,245,'外部服务 · 按配置连接',['通知：IM / 邮件 / Webhook 等','模型：LLM API，供内置 AI 使用','身份：SSO / OAuth 提供商','接口可用不代表已配置实际接收方','本次未验证外部投递或模型连通'], 'gray')
    d.group(570,1180,1520,245,'可选部署 · 当前未运行','不把代码支持的部署方式误画成本机已经启动的服务。','gray',dash=True)
    d.node(610,1275,440,120,'n9e-edge',['CenterApi 同步规则 / 配置','无 DB；边缘时序库旁判定与通知'], 'gray',dash=True)
    d.node(1100,1275,440,120,'n9e-alert / n9e-pushgw',['可拆分告警、接收转发职责','通过中心服务接口读取所需配置'], 'gray',dash=True)
    d.node(1590,1275,460,120,'Ibex / 内置时序库',['Ibex 自愈执行本预览未启用','EmbeddedTSDB 本预览未启用'], 'gray',dash=True)
    d.text(48,1490,'受管 SNMP Provider 及归属校验目前在 Center；尚未增加对 edge / 独立 Pushgw 的同等受管采集路径。',22,C['muted'])
    d.save('03-current-deployment.svg')


if __name__ == '__main__':
    overview()
    snmp()
    deployment()
    print('Generated 3 SVG drawings.')
