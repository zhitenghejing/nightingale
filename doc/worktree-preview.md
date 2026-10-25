# Worktree 预览运行手册

## 命令

在任务 worktree 根目录执行，Make/Paseo 提供同名封装：

| Python 入口：`python3 -B scripts/preview.py …` | 行为 |
| --- | --- |
| `up` | 分配槽位、构建、启动；存活实例只复验 |
| `refresh` | 停止、重建、启动；保留槽位、数据库与账号 |
| `status` | JSON 状态、源码/前端变化、本地/公网探测和私有路径 |
| `url` | 准备入口地址，不检查可用性 |
| `stop --if-exists` | 停止，保留绑定和数据 |
| `destroy --if-exists` | 停止、删除本实例数据和日志、释放绑定 |
| `list` | 查看本机登记目录中的所有绑定 |
| `services` | 准备/启动主机登记目录共用的一个 PG 实例和一个 Prometheus |

首次 `--slot 0` 选择 n9e-dev0.zhiteng.win；开发槽位共 0–9，全部具有各自公网入口。
绑定后不能切换槽位，也不能抢占其他 worktree 的停止/失败绑定；先保留需要的数据
再 destroy。自动分配选择空闲的 dev 槽位；十个都占用时失败，不会占用稳定地址。

稳定主版本 `n9e.zhiteng.win` 独立使用 30299，不属于开发槽位池。命令显式加
`--stable`（如 `up --stable`、`refresh --stable`、`status --stable`），或使用
`make stable`、`make stable-refresh`、`make stable-status`、`make stable-url`。
稳定实例应用源码来自主 checkout，只接受配置中的预期分支（默认 main）和干净源码；
应用版本使用 Git describe，开发实例版本使用 preview build ID。两者都有独立 build ID
以及独立的数据库和凭据。普通 stop/destroy 和 Paseo teardown 仅作用于当前 worktree。
稳定实例的删除需要显式 `destroy --stable`，它也会删除稳定实例的数据。

up/refresh 默认要求已配置的公网探测通过，初次配置 Tunnel 或排查本地时可加
`--local-only`；它不改变绑定，也不能作为公网验收结果。status 只报告状态，不以
退出码代表全部健康；交付检查 running、local.ok、public.ok（若存在）、
sourceChanged/frontendChanged。源码变化后必须 refresh。

## 隔离与身份

Python 管理器用主机文件锁串行变更和重型构建。每个绑定具有随机实例 ID，位于
`STATE/instances/<id>/`，包含 manifest.json、credentials.json、build.log、
preview.log、postgres.json、database-bootstrap.log、bin/ 和独立 run/ 工作目录
（Skill、配置与日志）。每个实例的业务数据存入共享 PG 服务中独立的
`n9ep_<instanceID>` database，由同名受限账号访问；miniredis 仍在本实例进程内。
所有实例查询同一个只读测试 Prometheus，内嵌 TSDB 关闭。共享服务配置、管理凭据
位于 `STATE/shared/`，服务数据位于独立 Docker 数据卷；详见[环境准备](development-environment.md#共享测试服务)。
Go 模块和编译缓存使用 Go 本身的共享机制。

共享容器命名为 `n9e-shared-pg-<registryID>` 和 `n9e-shared-prom-<registryID>`，
main 与 preview 接入相同主机登记目录时共用这一组服务。后缀来自登记目录的哈希，用于区分日常服务
与自动化测试的临时服务，不是 preview 实例 ID。旧 `n9e-preview-*` 容器在下一次
执行 `services` 或启动/刷新实例时原地改名，端口、凭据和数据卷保持不变；旧数据卷
名称记录在 `STATE/shared/services.json` 中，不因容器改名而创建空卷。

网关启动 n9e 时只继承 PATH、LANG、LC_ALL、LC_CTYPE、TZ、SSL_CERT_FILE 和
SSL_CERT_DIR；HOME 指向实例 run/，TMPDIR 指向实例私有临时目录。不继承宿主的
CONFIGTYPE_*、N9E_CONFIGS、代理变量或模型/云服务凭据。构建脚本仍使用调用者的
构建环境；运行时所需的模型和数据源配置应在预览 UI 中显式设置。
生成配置固定关闭 HTTP.APIForService、HTTP.APIForAgent 和 HTTP.ProxyAuth，
两个 profile 都适用。网关同时拒绝 `/v1/n9e` 及其子路径，无论后端是否开启接口；
网页登录、鉴权 UI API、MCP/A2A 和流式响应继续使用应用自身认证。
这些措施用于阻断宿主配置泄漏与内部 API 公开，不提供容器或系统用户隔离。
basic 只禁用 Skill 脚本，脚本通知等其他命令执行能力不属于该沙箱保护范围；
预览管理员仍需可信，不能将实例目录独立等同于宿主机安全隔离。

需要接入现有采集器的开发实例，可在私有实例目录创建权限为 `0600` 的
`collector.json`，内容为 `username`、`password` 两个字符串（各至少 8 字符）。
主机管理员明确允许共享指标写入后，创建 `STATE/shared/prometheus-remote-write.enabled`，
并在保留原数据卷的前提下，为已有共享 Prometheus 重建容器、添加
`--web.enable-remote-write-receiver`。后续 services/refresh 会保留该启动选项；
默认无标记的独立测试登记目录仍保持只读，admin/lifecycle API 继续关闭。
刷新后，该开发实例启用带 BasicAuth 的 APIForAgent，并将 Pushgw 指标转发至共享
Prometheus 的 `/api/v1/write`；内置 TSDB 保持关闭，UI 直接使用已有 shared-prometheus。
公共预览网关继续阻断全部采集接口。采集入口通过限定路径、校验上述凭据的本地
Docker 网桥代理提供；代理读取 manifest 中的 backendPort，刷新后地址保持不变。
APIForService、ProxyAuth 和 Ibex 保持关闭。代理和现有采集器属于外部接入配置，
stop/destroy 前应停止代理并切换或停止采集器；销毁预览不删除共享指标。
删除 collector.json 并刷新可关闭本实例采集入口；stable 实例拒绝该设置。

应用二进制放在 run/n9e，网关放在 bin/。toolkits 的 runner.Cwd 使用二进制所在
目录，文件前端从该目录的 pub/ 读取，因此应用位置必须与实例工作目录一致。

临时文件与 sandbox socket 使用 `/tmp/n9ep-<instance-id>` 私有目录，避免长 worktree
路径超出 Unix socket 长度限制。destroy 同时清理该目录。

网关独立会话后台运行，并管理同组 n9e 子进程。Ctrl-C 或关闭 Agent 不停止实例。
stop 校验 PID 与 Linux starttime，防止误杀复用 PID；先 SIGTERM 优雅关闭，再按需
强制退出。网关被杀时子进程也会结束。销毁只删除 manager 所属实例数据，不删源码。

源码指纹包含 Git 跟踪文件（包括已跟踪但被 ignore 的 statik）及未跟踪、未 ignore
文件；不包含运行数据和依赖缓存。构建中源码变化会失败，停止编辑后 refresh。
manifest 同时记录 commit、branch、dirty、前端摘要与 build ID。
`/__preview/info` 返回构建身份与健康，不公开凭据、PID 或主机路径。

首次 PG 初始化由当前管理器的 `cmd/preview-db` 完成，开放监听前写入随机 root 密码，
同时写入实例级 `login-check.pending`。网关首次验证登录和鉴权资料成功后删除该标记；失败或
中断时保留，后续启动继续验证。refresh 不重新生成标记、不重置密码。已有旧实例没有
该标记时直接使用健康检查，不强制用初始化密码重新登录。
status 提供 credentialsFile 路径，其中只保存初始化凭据，UI 改密后不会同步更新；
密码仅本机读取。每次启动及状态探测都验证版本、页面、PG 的 root 记录、共享
Prometheus 的 `up{job="prometheus"}` 实际采集值，以及匿名访问
`/api/n9e/self/profile` 返回 401，就绪不依赖用户的当前密码。
准备入口 `.../__preview/ready` 显示版本并清理该域名
旧存储/缓存，再点击登录；miniredis 是进程内的，刷新后旧 Token 可能失效。
Ready 不替代告警业务、真实模型和人工验收。

## 数据保留与旧实例切换

`refresh` 保留 PG database/账号、应用账号与数据；`stop` 也保留这些资源。
`destroy` 先停本实例，核对资源归属，然后删除该实例 database 和角色，再删私有
文件并释放槽位；PG/Prometheus 共享服务、其他环境数据库和共享指标不会被删除。
任一步失败都保留绑定供重试，不能直接删除 worktree 代替资源清理。

旧 SQLite 实例首次 refresh 自动备份并导入到本实例的 PG database。
`sqlite-backup.db`、原 `run/n9e.db` 和旧 `run/data/tsdb` 保留用于恢复，成功后不会
自动合并后续 SQLite 改动。SQLite 的非空表或列若无法对应 PG schema，导入事务
回滚并拒绝启动，不会静默跳过。失败原因记录在私有 `database-bootstrap.log`。
PG 标记 initialized 后，即使数据库丢失也只报错，不会悄悄新建空库。

切换前需保存旧 manifest、二进制及数据备份；恢复旧版本时停止当前实例，使用原
SQLite/TSDB 和对应旧版本管理器重建，不能把 PG 初始化失败当作允许覆盖备份的理由。
PG 初始化阶段发生中断可以直接重试 refresh；成功运行后的 PG 数据应单独备份，
不能用切换前的 SQLite 备份替代当前 PG 数据。

## 前端与完整能力

普通模式默认用固定 Node 24 + pnpm 构建同仓 `frontend/`，复制 `frontend/pub/` 到实例，
记录源码、锁文件、工具链及资源摘要。没有同仓前端的旧分支才使用 committed statik；
构建失败不能退回旧包。详见[前端开发](frontend-development.md)。
外部 UI 联调需先构建，再显式提供产物和 revision：

```bash
python3 -B scripts/preview.py refresh --frontend-dir /absolute/path/to/fe/pub --frontend-ref <commit-or-tag>
```

管理器将产物复制到实例，记录摘要；status 检查源产物是否变化。refresh 保留选择，
恢复 statik 模式需销毁后重新启动。源目录须包含 index.html。

完整能力模式构建 sandbox、QA 代码语料：

```bash
CATEGRAF_REF=<collector-tag> \
  python3 -B scripts/preview.py refresh --profile full
```

QA 前端语料固定来自同仓 `frontend/src/`，不再通过 FE_REF/FE_LOCAL 取外部源码。
显式外部 UI 的产物与同仓 QA 语料需分别记录，不能宣称天然同版。full 设置 RequireIsolation=true，主机
没有隔离能力时拒绝脚本运行，不能降级 unsafe；实际脚本执行仍需专项验证。
切回轻量模式用 `refresh --profile basic`。

## 排障

| 现象 | 处理 |
| --- | --- |
| 槽位耗尽 | list 找停止/失败绑定，在对应 worktree destroy |
| 页面没更新 | 看 build ID/sourceChanged，refresh 后从准备入口打开 |
| 构建失败 | 看 build.log，修复源码/网络/依赖后 refresh，绑定仍保留 |
| 本地通、公网不通 | 查 DNS、Tunnel ingress、用户服务和公网返回的实例身份 |
| 网关退出 | 看 preview.log，排查端口、启动、迁移与健康检查 |
| 登录失败 | 未改密时用该实例 credentialsFile；改密后用新密码重新登录，初始化凭据不会同步更新 |
| verify 失败 | 区分基线、缺失外部服务及新增回归，不能用预览通过替代门禁 |

日志可能包含用户内容或诊断信息，分享前处理敏感内容。归档 hook 应在源码删除前清理，
失败时查日志重试。测试可使用独立 N9E_PREVIEW_STATE，端口仍需避开日常实例。

完整生命周期验证可运行 `python3 -B scripts/preview-smoke.py`。它建立临时 worktree、
独立登记目录、一套临时 PG/Prometheus 和真实双应用实例，检查分库与账号隔离、
共享指标查询与写入拒绝、槽位所有权、源码/前端漂移、刷新、停止、销毁和槽位重用，
最后删除测试实例、共享测试容器和数据卷；不会修改日常预览绑定。此检查包含多次构建，不在每次
make verify 中机械重复。
