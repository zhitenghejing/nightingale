# AI 开发设施验证记录

## 2026-10-21 PR #1 CI 时区依赖修复

GitHub Actions 的 `verify` 在 `pkg/unit` 的秒/毫秒时间戳断言失败：UTC
runner 输出 `2023-05-08 04:00:00`，测试却固定期望东八区的
`2023-05-08 12:00:00`。本地执行
`TZ=UTC go test ./pkg/unit -run TestValueFormatter -count=1` 复现两项子测试失败，
相同命令改为 `TZ=Asia/Shanghai` 则通过。

保留 `ValueFormatter` 按进程本地时区格式化的行为，把时间戳用例移入独立测试，
显式设置 UTC、UTC+08、UTC-05，并在每组测试后恢复 `time.Local`。
两种时间戳精度均断言完整 `FormattedValue`；负偏移用例同时覆盖跨日。
测试顺序执行，避免进程级时区在并行用例之间相互影响。

| 检查 | 本轮结果 |
| --- | --- |
| `TZ=UTC` / `TZ=Asia/Shanghai` / `TZ=America/New_York` 下分别执行 `go test ./pkg/unit -count=1` | 三种宿主时区均通过 |
| `TZ=UTC make verify` | 退出 0；18 项 Python 测试通过，Go 2613 passed、0 failed、39 skipped（包含子测试） |
| `TZ=UTC make verify-all` | 退出 0；相同测试统计，另含 `go vet ./...` 及两个全量集成模板门禁通过 |

39 项 skip 包含未配置的外部服务及其他专项条件，不计作已验证。
本次只修改测试及验证记录，未启动或刷新预览，没有新的 preview build ID；
未执行浏览器或人工产品验收，未发布稳定实例。PR 原有三项 P2 审查待办不在本次修复范围。

## 2026-10-21 pre-push 临时 Git 仓库隔离修复

首次实际推送暴露了 hook 环境污染：Python 测试只通过 `cwd` 选择临时仓库，
却继承了指向真实 worktree 的 Git 仓库变量，导致测试提交、索引和配置写入真实仓库。
此前临时包装 hook 清理环境后推送成功，只是规避，不算原始门禁通过。

本次保留 `.githooks/pre-push` 和 `scripts/verify.py`，在两个测试类的 setup 中按
`git rev-parse --local-env-vars` 清理仓库环境，并在测试结束后恢复调用者环境。
隔离覆盖临时仓库初始化及被测代码切换 ROOT 后的 Git 调用；其他环境设置保留。

新增回归测试创建一次性宿主仓库及 linked worktree，注入指向宿主的 Git 环境变量，
运行原有两个测试套件，核对 HEAD、本地引用、主 checkout/worktree 索引、共享配置
和工作区文件均未变化。修复前已运行并得到 `fixture suites modified the hook's repository`
失败；加入隔离后通过。

| 检查 | 本轮结果 |
| --- | --- |
| `python3 -B -m unittest discover -s scripts -p 'test_*.py'` | 18 项通过，包含宿主仓库状态保持不变的回归检查 |
| `make verify` | 退出 0；Go 2605 passed、0 failed、39 skipped（包含子测试） |
| 一次性 linked worktree 向本地 bare 远端执行普通 `git push` | 原始 pre-push 执行完整 `make verify` 后退出 0；本地 HEAD、分支、索引和配置不变 |
| 在上述一次性 worktree 加入故意失败的测试后再次普通推送 | pre-push 退出 1，显示 `intentional pre-push rejection`；远端仍为上次成功的提交，本地元数据不变 |

两次推送均使用仓库原有 hook 和安装器，没有替换 hooksPath、包装器或跳过检查。
一次性 checkout、worktree 和 bare 远端均已清理，未连接公网预览或稳定实例。
本轮没有执行 `make verify-all`、浏览器或人工产品验收；39 项 skip 不计作已验证。
此修复仅处理 Git 测试隔离，PR 中另外三项 P2 仍待处理。

## 2026-10-21 测试门禁修复补充

以下为 elegant-mole 在 `3bf1533a` 之后的工作区检查结果；后面的 2026-10-20
基线失败表保留历史证据，不代表当前仍有同样失败。

| 检查 | 本轮结果 |
| --- | --- |
| `make verify`，未配置外部服务 | 退出 0；Go 2603 passed、0 failed、39 skipped（包含子测试）；逐项显示 skip 原因 |
| 配置一次性服务并设置 `N9E_REQUIRE_EXTERNAL_TESTS=1` 后 `make verify-all` | 退出 0；Go 2622 passed、0 failed、20 skipped；build、vet、两个全量集成模板门禁通过 |
| 四个外部依赖包专项测试 | MySQL 8.4.7、PostgreSQL 17.7、ClickHouse 23.8、VictoriaLogs 1.53.0；67 passed、0 failed、0 skipped |
| `go test ./pkg/promql ./pushgw/pconf ./pushgw/writer -count=30` | 通过；消除 map 顺序偶发失败并覆盖条件精确/正则/组合/缺失标签/非法配置 |
| 同三个包 `-race -count=1` | 通过 |
| Python 设施测试 | 16 项通过，包括验证入口对 fail/skip 的汇总和退出码保留 |
| 缺少强制配置与已配置服务不可达 | 缺配置强制测试失败；四种服务的不可达配置分别失败，无 skip |

默认环境额外跳过 19 项外部服务测试；配置服务后保留的 20 项 skip 属于真实通知
凭据、sandbox/网络隔离等其他专项条件，本轮没有把它们记为通过。四种服务使用
独立容器、随机回环端口和一次性数据，不接入现有数据库或预览数据。CI 新增强制
外部集成任务；本轮验证了本机同版本镜像和健康命令，尚未在 GitHub 执行该任务。
本轮未做浏览器、人工产品验收或发布。

## 2026-10-20 初始设施验证

验证日期：2026-10-20。Linux amd64，Go 1.25.7，Python 3.12.3。
基础提交：97fe92976009b12a206d145e59a1ad4fddf5fbc3，实施分支 elegant-mole。
验证基于实施时的工作区快照，实际运行版本以 `make preview-status` 的 commit、源码指纹与 build ID 为准。

## 已通过

| 检查 | 结果 |
| --- | --- |
| `python3 -B -m unittest discover -s scripts -p 'test_*.py'` | 11 项通过，另覆盖十个 dev 域名、稳定域名保留、槽位池分离、稳定源码须为干净 main；保留原槽位/端口、PID、密码、指纹和探测测试 |
| `go test ./cmd/preview-gateway -race -count=1` | 通过；启动门禁、后端身份/健康、敏感路径拦截、API 参数/鉴权传递、SSE 首帧流式输出 |
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| 两项集成模板检查 | action、全量 en_US 渲染通过 |
| `python3 -B scripts/preview-smoke.py` | 真实双 worktree、多次构建的完整生命周期通过 |
| 本地与公网身份探测 | 同一 build ID，源码匹配；独立 named tunnel 和用户服务已运行 |
| 公网浏览器 | 准备入口、随机 root 账号登录、指标查询页面及 `vector(1)` 查询通过，返回一条值为 1 的结果 |
| 公网本机接口边界 | 直接访问 `/prometheus/...`、`/dumper/sync` 返回 403；页面通过鉴权代理查询正常 |

生命周期验证使用独立登记目录，检查两实例密码及 SQLite 互相独立；公网槽位不可
抢占；未提交源码变化被检测；前端被复制而不是实时链接；前端源产物变化可检测；
刷新更换 build ID、保留账号；停止不影响另一实例；销毁幂等且槽位能立即重用。
临时 worktree、进程、数据和绑定已清理，日常公网实例保留用于验收。

## 稳定入口与开发入口

`n9e.cubbber.dev` 独立绑定主 checkout（main），使用 30299，不属于 worktree 槽位池。
`n9e-dev0.cubbber.dev` 至 `n9e-dev9.cubbber.dev` 为开发槽位，使用 30300–30309。
当前 elegant-mole 分支迁到 dev0，原实例数据库和账号保留；主版本另建独立实例。
DNS 与专用 Tunnel 已配置全部 11 个域名，空闲 dev 槽位无实例时不返回应用页面。

主实例通过 `--stable` 显式启动/刷新；要求主 checkout 位于 main 且工作区干净。
普通 preview/refresh/归档只操作当前 worktree，不会替换或清理稳定实例。

## 全量门禁的已有失败

`make verify-all` 如实返回失败，原因是 `go test ./...` 的以下 7 个已有包失败：

| 包 | 观察到的原因 |
| --- | --- |
| `dskit/clickhouse` | 未配置测试连接，empty shards |
| `dskit/mysql` | 未配置测试连接，empty shards/empty pgsql shards |
| `dskit/victorialogs` | 127.0.0.1:9428 没有测试服务 |
| `models/migrate` | 连接 127.0.0.1:3306 失败，原测试继续使用空连接后 panic |
| `pkg/ormx` | 原数据库测试使用固定连接信息，连接/认证失败；部分 DSN 与测试调用不匹配 |
| `pkg/promql` | 原变量解析与二元运算相关期望不匹配 |
| `pushgw/writer` | 原条件 relabel 测试期望不匹配 |

已另建**未覆盖本次改动的干净基线 worktree**，在同一基础提交对这 7 个包运行
`go test -buildvcs=false`，全部复现失败。没有跳过测试、放宽断言或修改无关业务代码。
新 CI/pre-push 会继续暴露这些基线问题，预览通过不等于全量门禁通过。

## 尚未执行

- full profile 的 Docker sandbox 资源构建、实际隔离脚本执行和 QA 语料功能验收。
- 真实 LLM 调用、外部 MySQL/PostgreSQL/ClickHouse/VictoriaLogs 的完整功能矩阵。
- 跨架构发布、真实人工产品验收、推送、PR、合并或发布。

浏览器检查由 Agent 执行，不代替 PR 模板要求的人工确认。主机上的私有验证日志、
截图和账号文件位于预览状态目录及任务日志中，不提交仓库。
