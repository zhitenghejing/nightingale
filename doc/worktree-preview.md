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

首次 `--slot 0` 选择 n9e-dev0.cubbber.dev；开发槽位共 0–9，全部具有各自公网入口。
绑定后不能切换槽位，也不能抢占其他 worktree 的停止/失败绑定；先保留需要的数据
再 destroy。自动分配选择空闲的 dev 槽位；十个都占用时失败，不会占用稳定地址。

稳定主版本 `n9e.cubbber.dev` 独立使用 30299，不属于开发槽位池。命令显式加
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
preview.log、bin/ 和独立 run/ 工作目录（SQLite、TSDB、Skill、配置与日志）。
Go 模块和编译缓存使用 Go 本身的共享机制。

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

首次 SQLite 从仓库初始化 SQL 创建，并在启动监听前替换默认 root 密码，同时写入
实例级 `login-check.pending`。网关首次验证登录和鉴权资料成功后删除该标记；失败或
中断时保留，后续启动继续验证。refresh 不重新生成标记、不重置密码。已有旧实例没有
该标记时直接使用健康检查，不强制用初始化密码重新登录。
status 提供 credentialsFile 路径，其中只保存初始化凭据，UI 改密后不会同步更新；
密码仅本机读取。每次启动及状态探测都验证版本、页面、TSDB 查询，以及匿名访问
`/api/n9e/self/profile` 返回 401，就绪不依赖用户的当前密码。
准备入口 `.../__preview/ready` 显示版本并清理该域名
旧存储/缓存，再点击登录；miniredis 是进程内的，刷新后旧 Token 可能失效。
Ready 不替代告警业务、真实模型和人工验收。

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
独立登记目录和真实双实例，检查数据隔离、槽位所有权、源码/前端漂移、刷新、停止、
销毁和槽位重用，最后清理；不会修改日常预览绑定。此检查包含多次构建，不在每次
make verify 中机械重复。
