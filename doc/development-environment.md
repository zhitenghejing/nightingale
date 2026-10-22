# 开发环境准备

开发预览以 Linux 为目标。准备 Git、make、Go 1.25（或满足 go.mod 的版本）、Python
3.11+。部分测试用 cgo SQLite，需要 C 编译器，Ubuntu 可安装 build-essential。
普通预览需要固定的 Node 24 + pnpm 来构建同仓前端，不需要 Docker、外部数据库或下载上游 UI。
前端工具链与安装见[前端开发](frontend-development.md)。

```bash
go mod download
make frontend-install
bash scripts/install-git-hooks.sh
make verify
```

下载失败先检查网络/GOPROXY，再重试，不改依赖版本规避网络问题。已有失败如实记录。
安装器遇到已有其他 hooksPath 时保留它；worktree 共享 Git 配置。

## 主机登记

状态目录默认 `~/.local/state/n9e-preview`，可用 N9E_PREVIEW_STATE 指定测试用的
独立目录。日常 worktree 共用同一个目录才能协调槽位。可选 config.json：

```json
{
  "stable": {
    "worktree": "/absolute/path/to/main-checkout",
    "branch": "main",
    "port": 30299,
    "url": "https://n9e.cubbber.dev"
  },
  "slots": [
    {"id": 0, "port": 30300, "url": "https://n9e-dev0.cubbber.dev"},
    {"id": 1, "port": 30301, "url": "https://n9e-dev1.cubbber.dev"},
    {"id": 2, "port": 30302, "url": "https://n9e-dev2.cubbber.dev"},
    {"id": 3, "port": 30303, "url": "https://n9e-dev3.cubbber.dev"},
    {"id": 4, "port": 30304, "url": "https://n9e-dev4.cubbber.dev"},
    {"id": 5, "port": 30305, "url": "https://n9e-dev5.cubbber.dev"},
    {"id": 6, "port": 30306, "url": "https://n9e-dev6.cubbber.dev"},
    {"id": 7, "port": 30307, "url": "https://n9e-dev7.cubbber.dev"},
    {"id": 8, "port": 30308, "url": "https://n9e-dev8.cubbber.dev"},
    {"id": 9, "port": 30309, "url": "https://n9e-dev9.cubbber.dev"}
  ]
}
```

没有文件时使用这些默认值；stable.worktree 未配置时通过 Git common-dir 定位主 checkout。
ID、端口、公网 URL 不得重复，稳定域名禁止出现在开发槽位中；修改前先清理受影响
绑定。已有绑定保留原配置，不会自动漂移。目录、配置和凭据仅本机保存。
本版不导入生产 DSN、模型密钥或通知配置；按需在预览 UI 配置测试模型和数据源。
刷新保留它们，销毁会删除实例数据。

## 公网预览

先用 `python3 -B scripts/preview.py up --slot 0 --local-only` 跑通本地环境。
安装 cloudflared，用 cubbber.dev 所属账号的证书创建项目专用 named tunnel，
独立保存配置与用户服务，不修改其他项目正在使用的 Tunnel：

```yaml
tunnel: <n9e-preview-uuid>
credentials-file: /absolute/private/path/<uuid>.json
ingress:
  - hostname: n9e.cubbber.dev
    service: http://127.0.0.1:30299
  - hostname: n9e-dev0.cubbber.dev
    service: http://127.0.0.1:30300
  - hostname: n9e-dev1.cubbber.dev
    service: http://127.0.0.1:30301
  # dev2–dev9 同样分别转发到 30302–30309
  - service: http_status:404
```

保存为状态目录下 cloudflared.yml，先验证 ingress，再使用**该配置及 UUID**建立 DNS：

```text
cloudflared tunnel --config <config> --origincert <account-cert> route dns <uuid> n9e.cubbber.dev
cloudflared tunnel --config <config> --origincert <account-cert> route dns <uuid> n9e-dev0.cubbber.dev
cloudflared tunnel --config <config> --no-autoupdate --protocol http2 run <uuid>
```

dev1–dev9 也逐个建立 DNS 和 ingress，不用改 worktree 就能分配槽位。
由 systemd --user 服务运行最后一条命令，设置 Restart=on-failure、UMask=0077。
无人登录仍需运行时核对该用户 linger 设置。本机已建立 n9e-preview-tunnel.service：

```bash
systemctl --user status n9e-preview-tunnel.service
cloudflared tunnel --config ~/.local/state/n9e-preview/cloudflared.yml ingress validate
make preview-status
```

Tunnel 转发回环网关端口；应用内部端口不直接发布。公网账号使用随机密码，普通
profile 不暴露 pprof/metrics、不启动 Ibex 和 Skill 脚本。

## 专项环境

MySQL/PostgreSQL/外部 Redis 测试另用隔离库和独立命名空间。现有 docker/compose-*
含固定容器名和端口，不能原样并行运行；需独立 Compose project、容器名、端口和
volume。普通预览管理器只管理 SQLite，不会自动连接外部服务。

外部集成测试只在明确配置下运行；未配置时仅跳过相应测试，包内单元测试照常执行。
配置后连接、认证、SQL 或断言失败仍会失败，不会自动降级为 skip。

| 环境变量 | 测试依赖 |
| --- | --- |
| `N9E_TEST_MYSQL_DSN` | Go MySQL TCP DSN，包含专用测试库名；账号可创建/删除临时数据库和表 |
| `N9E_TEST_POSTGRES_DSN` | `host=... port=... user=... password=... dbname=postgres sslmode=disable` 格式；账号可创建/删除临时数据库 |
| `N9E_TEST_CLICKHOUSE_ADDR` | HTTP 协议的 `host:port`；可选 `N9E_TEST_CLICKHOUSE_USER`（默认 default）、`N9E_TEST_CLICKHOUSE_PASSWORD` |
| `N9E_TEST_VICTORIALOGS_URL` | 一次性 VictoriaLogs 的完整 HTTP URL |
| `N9E_REQUIRE_EXTERNAL_TESTS=1` | 缺少上述任一必需配置时失败，用于强制外部集成门禁 |

DSN 仅指向一次性测试服务，不使用生产配置。MySQL 数据源测试自建临时表；ormx
测试使用随机数据库名并清理；SQLite 使用临时目录，不需要外部环境变量。原迁移
目录的手工权限写入脚本已改为 SQLite 下真实权限绑定操作的幂等测试。

`make verify` / `make verify-all` 显示 Go 测试的通过、失败与跳过数量（包含子测试），
并列出 skip 原因。默认检查通过不表示跳过的外部服务已验证。GitHub verify workflow
另有必跑的 `external-tests` 任务：使用固定版本的一次性 MySQL、PostgreSQL、
ClickHouse、VictoriaLogs，设置强制开关后运行四个相关包，避免 CI 静默跳过。
该开关仅约束上述四类服务，不会启用需要真实通知凭据或 sandbox 能力的其他专项测试。

full profile 需要 Docker、构建脚本要求的网络和 Linux 隔离能力，构建本架构 sandbox
资源与 QA 语料，显式固定 CATEGRAF_REF；前端语料来自同仓源码。常规检查不构建跨架构发布资源，
发布验证仍由现有 GoReleaser workflow 承担。前端版本配对见[运行手册](worktree-preview.md)。
