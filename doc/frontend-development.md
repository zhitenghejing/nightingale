# 同仓前端开发

前端源码在 `frontend/`，上游来源及导入范围见
[UPSTREAM.md](../frontend/UPSTREAM.md)。前端与后端在同一任务分支修改和审查。
构建后由现有 Go 服务提供页面；部署时不需要 Node 服务。

## 工具链和命令

使用 `frontend/.node-version` 固定的 Node 24 补丁版本，以及
`frontend/package.json` 的 `packageManager` 固定的 pnpm 版本。
安装相应工具后执行：

```bash
make frontend-install    # pnpm install --frozen-lockfile
make frontend-build      # 生产构建，输出 frontend/pub/ 和构建身份
make frontend-verify     # 核验/构建当前产物，并执行 Jest
make all                 # 构建当前 UI，嵌入 statik，再构建 Go 二进制
```

后端快速编译可继续 `go build ./...` / `make build`，使用已提交的 statik，
不代表当前前端源码已编译。交付 UI 时必须使用 preview 或重新生成 statik；
`sh fe.sh` 只构建本仓库前端，不下载上游发布包。

仅维护 `frontend/pnpm-lock.yaml`，不混用 npm/yarn 锁文件。
修改依赖后审查锁文件，再验证冻结安装、构建和相关测试。
只允许配置中明确批准的依赖安装脚本；新增需求须查明用途再配置。
每个 worktree 的 node_modules、构建输出和浏览器状态独立，可共享 pnpm store。

`make verify` 在前端或相关路由/models 改动时包含前端检查；
`make verify-all` 强制包含前端。CI 使用相同工具链和验证入口。
不能以安装成功替代编译成功，不能跳过类型检查来取得绿色结果。
只有源码、配置和资源摘要均匹配时才复用已成功编译的缓存，Jest 每次执行；
全新 CI checkout 没有缓存产物，会完整编译。需要强制重建时执行
`python3 -B scripts/frontend.py verify --force`。

## 预览与构建身份

```bash
make preview-refresh
make preview-status
make preview-url
```

迁入后默认从当前 worktree 的 `frontend/` 构建页面，再复制到隔离实例。
构建失败会阻止启动，不会退回旧 statik。稳定实例仍只从配置的干净 main 构建；
分支只使用 dev 槽位。旧分支没有前端源码时保留 committed-statik 兼容路径。

`frontend/.build-manifest.json` 记录前端源码指纹、pnpm 锁文件摘要、工具链版本和
资源摘要；preview manifest 同时记录后端源码指纹及 build ID。
源码、配置或资源变化使旧构建失效。生成文件、node_modules 和测试报告不进入 Git。
状态必须满足源码和资源未漂移、本地及公网探测通过，才可交给人验收。

外部 `--frontend-dir` / `--frontend-ref` 仍保留为显式联调入口，不能假称为同仓构建。
完整 QA 代码语料从同仓源码提取，不接受 FE_LOCAL/FE_REF 替换；
full 模式仍需固定 CATEGRAF_REF 和实际 sandbox 隔离能力。

## 浏览器验收

在隔离 preview 上验证生产构建，不只验证 Vite dev server。
使用实例私有凭据文件，不把密码写进代码、日志或公开报告。
基线至少覆盖错误密码拒绝、正确登录、权限菜单、页面路由、静态资源加载和刷新恢复；
功能改动另外覆盖受影响的业务场景。

```bash
(cd frontend && pnpm exec playwright install chromium)  # 首次安装浏览器
make preview-refresh
make frontend-e2e
```

无模型的迁入用例验证匿名拒绝、错误密码、真实登录、仪表盘/规则页刷新与失效会话。
入口先核验运行实例版本并从私有文件读取凭据；报告不录制含登录请求的 trace/video。
这些用例验证迁入基线，不替代告警触发、通知与恢复等业务链路的专项测试。

上游 Midscene E2E 需要模型配置和业务 fixture，不属于无模型的默认门禁，
不得连接生产实例执行创建或清理操作。人工验收保持独立，由人确认。

## 发布

GoReleaser 在多架构构建前只构建、嵌入一次同仓 UI；各架构消费同一份静态资源。
归档及容器保存前端构建 manifest，QA 代码语料使用同仓源码。
发布产物仍须做冒烟和部署核验；本地构建通过不代表已经发布。
