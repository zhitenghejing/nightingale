# 前端源码纳入当前仓库的可行性分析

调查日期：2026-10-21。后端基线：`e4972c82`，分支 `elegant-mole`，
`git describe` 为 `v9.2.0-16-ge4972c82`。本文保存迁入前的研究结论；下文“当前”均指
该调查基线。后续已开始按此方案迁入，实际适配与验证见[迁移验证记录](frontend-migration-validation.md)，
日常操作见[同仓前端开发](frontend-development.md)。

## 结论

可行，且适合当前由 Agent 完成前后端设计、实现、审查和 E2E 的工作方式。
建议将选定提交的公开前端源码作为普通文件放入 `frontend/`，由当前 Git 仓库直接维护。
同一个提交可同时表达页面、接口和测试的修改；一个 worktree 对应一套前后端源码。
前端仍在构建阶段生成静态文件，运行时继续由现有 Go 服务提供，不需要增加 Node 服务。

迁移后的工具链按用户要求统一为 **Node.js 24 + pnpm**，开发、预览构建、CI、
E2E 和发布构建均使用这套工具链。Node 24 和 pnpm 的兼容验证属于迁入完成条件，
不能以旧 Node/npm 环境通过代替；两者的具体版本在实测后精确固定。

但“当前版本对应源码”有两个不同含义，必须区分：

- 与后端 `v9.2.0` 同名的前端版本：有可获得的明确候选提交。
- 精确生成当前已提交 statik 的源码：本轮未能确定，不能用同名 tag 或包内版本号代替证据。

建议以 `n9e/fe v9.2.0` 的提交
`58e8e242e88d834282bbeb8da9a5dd8d38100d17` 为迁入候选，
先做隔离构建与兼容验证，再迁入。它同时构成一次相对现有内嵌 UI 的升级，
需要单独验收；同名版本不能证明每个接口都兼容。
若要求迁入后页面完全保持现状，则应先继续追溯旧产物来源，不能宣称已找到精确源码。

来源：[前端 v9.2.0 tag](https://github.com/n9e/fe/releases/tag/v9.2.0)、
[候选源码提交](https://github.com/n9e/fe/tree/58e8e242e88d834282bbeb8da9a5dd8d38100d17)。

## 当前 UI 的实际来源状态

对本地 `front/statik/statik.go` 中的 ZIP 数据进行只读解码，得到：

| 项目 | 结果 |
| --- | --- |
| statik Go 文件 SHA256 | `38fb423c02df5e2d256930144041d62c155dc4536bca784371a1aeb060c721c9` |
| ZIP 文件数 / 解压总大小 | 142 / 16,699,093 字节 |
| ZIP 内文件时间 | 均为 2025-03-31 12:26:36；仅是产物元数据，不证明源码时间 |
| 主 JS | `assets/index.c1b0af66.js` |
| JS 内嵌 package version | `v8.0.0-beta.8` |
| Git 最近一次修改该文件 | `7664c151`，2025-04-02 |
| 后端 `v9.2.0` tag 内的 statik | 与当前文件 SHA256 相同 |

当前 worktree 的已有预览 manifest 也记录相同的 committed-statik 摘要。
本轮只读已有 manifest，没有刷新实例或访问凭据。

下载上游 beta.8、beta.10 发布包，按相对路径和文件内容 SHA256 对比：

| 上游发布包 | 包内普通文件数 | 与本地 142 个文件完全匹配数 |
| --- | --- | --- |
| v8.0.0-beta.8 | 149 | 131 |
| v8.0.0-beta.10 | 149 | 132 |

差异包含主 JS 和 HTML 等关键文件，不能认定任一发布包就是当前 UI 的来源。
包内版本字符串可能没有随构建更新；时间和相似静态资源只能缩小调查范围。
本轮没有通过逐个重建历史提交来反推精确来源。

来源：[beta.8 发布包](https://github.com/n9e/fe/releases/tag/v8.0.0-beta.8)、
[beta.10 发布包](https://github.com/n9e/fe/releases/tag/v8.0.0-beta.10)、
本地 [fe.sh](../fe.sh)、[架构说明](../CLAUDE.md)。

## 候选源码是否具备独立构建条件

对固定的 v9.2.0 提交核查结果如下：

| 项目 | 已确认的事实及含义 |
| --- | --- |
| 规模 | Git tree 未截断，3,826 个普通文件，合计 56,059,027 字节，约 53.5 MiB；不含 npm 依赖和 Git 历史。三个中文字体约占 31.7 MB，是体积主要来源 |
| 技术栈 | React 18、TypeScript 4.9.4、Vite 4；仍可作为独立前端目录构建 |
| 安装与构建入口 | 上游 CI 使用 `npm ci`、`npm run build`；build 执行 TypeScript 检查和 Vite 打包，Node 堆上限设置为 8192 MiB，不等于已测得实际峰值 |
| 输出位置 | `pub/`，不是 `dist/`；在建议布局下是 `frontend/pub/` |
| 依赖来源 | lockfileVersion 2，packages 共 2,523 项（含根）；resolved 域名只有 npm 官方 registry 和 npmmirror。未发现 file/git 或私有域依赖，也无 Git 子模块 |
| 专用 scope 包 | 抽查 6 个 fc/flashcatcloud scope 包的精确锁定版本，公网 npm 元数据均可访问；未逐个下载全部依赖，不能据此保证完整安装成功 |
| 商业扩展 | 公开 Git tree 无 `src/plus/`。普通 build 的 plus resolver 使用公开占位实现；advanced/prefix 模式才选择商业扩展，迁入先限定普通构建 |
| 测试入口 | 有 Jest、Playwright 命令和 60 个 `e2e/` 文件；不代表已具备当前隔离预览的数据、账号和业务闭环，不能直接视为迁入后的 E2E 已通过 |

来源：[固定提交 Git tree](https://api.github.com/repos/n9e/fe/git/trees/58e8e242e88d834282bbeb8da9a5dd8d38100d17?recursive=1)、
[package.json](https://github.com/n9e/fe/blob/58e8e242e88d834282bbeb8da9a5dd8d38100d17/package.json)、
[锁文件](https://github.com/n9e/fe/blob/58e8e242e88d834282bbeb8da9a5dd8d38100d17/package-lock.json)、
[Vite 配置](https://github.com/n9e/fe/blob/58e8e242e88d834282bbeb8da9a5dd8d38100d17/vite.config.ts)、
[plus resolver](https://github.com/n9e/fe/blob/58e8e242e88d834282bbeb8da9a5dd8d38100d17/plugins/plusResolve.ts)、
[发布 workflow](https://github.com/n9e/fe/blob/58e8e242e88d834282bbeb8da9a5dd8d38100d17/.github/workflows/release.yml)。

迁入时保留整个前端目录结构，并在该目录运行安装、测试和构建；配置使用工作目录
加载环境文件，不能只复制 `src/` 或直接在后端根目录执行原命令。
部分上游 E2E 依赖 Midscene 和模型配置，还需浏览器、测试数据及隔离后端，
也会创建和清理规则。可复用用例，但应为基础门禁保留不依赖真实模型的确定性检查。
来源：[E2E 说明](https://github.com/n9e/fe/blob/58e8e242e88d834282bbeb8da9a5dd8d38100d17/e2e/README.md)、
[E2E fixture](https://github.com/n9e/fe/blob/58e8e242e88d834282bbeb8da9a5dd8d38100d17/e2e/fixture.ts)。

工具链有明确矛盾：上游 workflow 固定 Node 16.15.0，但锁文件中的 Playwright 要求
Node >=18，Midscene 要求 >=18.19，部分传递依赖要求 >=20 或 >=20.18.1。
因此不能照搬旧 Node 配置。第一阶段须在一次性目录/容器中验证完整安装、构建及所选测试，
目标工具链统一采用 Node 24 + pnpm，按下节方式验证并固定构建环境；
依赖仅作兼容所需的调整，不同时开展无关的大范围升级。

## Node 24 与 pnpm 的迁移要求

pnpm 官方兼容表列出了 Node 24 支持，且提供从 `package-lock.json` 生成
`pnpm-lock.yaml` 的 `pnpm import`。这证明有正式迁移入口，不证明当前 Vite 4、
TypeScript、Jest、Playwright 及全部依赖已经在目标环境下可用。
来源：[pnpm 安装与兼容性](https://pnpm.io/installation)、
[pnpm import](https://pnpm.io/cli/import)。

迁移须覆盖以下内容：

1. **固定版本并强制核验。** 前端 `package.json` 声明 Node `>=24 <25`，在
   `.node-version` 等统一版本文件中固定具体的 24.x 补丁版本，`packageManager`
   固定实测通过的 pnpm 精确版本。开发脚本、CI 和发布读取同一来源，运行时核对实际版本，
   错误版本直接失败；不能只写一个不会被强制执行的 engines 提示。
2. **转换锁文件。** 从固定上游提交的 npm 锁文件执行 `pnpm import`，审查转换前后的
   直接依赖、传递依赖和 peer 解析差异，记录必要调整。完成验证后移除工作树中的
   `package-lock.json`，只维护 `pnpm-lock.yaml`；原始来源仍可从导入提交追溯。
   不通过删除旧锁文件后直接解析所有版本来完成迁移。
3. **修复依赖声明和安装脚本。** 实测 pnpm 的依赖布局是否暴露隐式依赖、peer 冲突、
   类型解析或测试模块解析问题；优先补齐正确的依赖声明。按选定 pnpm 版本明确记录
   确需执行的依赖构建脚本；不能仅因为安装退出码为零就认定 esbuild 等工具可运行。
   不默认使用全局 hoist 或全面放行脚本来掩盖问题。
4. **统一入口与缓存。** 将本地、Paseo、pre-push、CI、preview 和发布中的前端入口
   改为 pnpm。后续安装统一用 `pnpm install --frozen-lockfile`；每个 worktree
   保留独立 node_modules 和输出目录，可共享 pnpm store，不共享可变运行目录。
   检查原脚本对 `npm_config_*`、lifecycle 和工作目录的依赖，不能只替换命令名称。
5. **用最终产物验收。** 在 Node 24 下从干净安装执行 `pnpm run build`、所选
   Jest 检查及 E2E，确认资源加载、动态 import、字体、权限和关键页面可用。
   同时验证锁文件和工具版本未被命令悄悄改写。构建出 `frontend/pub/` 后继续
   使用该产物进行隔离预览、验收和发布，不回退到上游发布包。

前端迁入仍然保持单个 Node 工程：可以直接在 `frontend/` 执行 pnpm，
不因后端和前端同仓就强制引入多包 workspace。需要的 pnpm 配置随选定版本落在
前端目录；后续确有多个 JS 包时再设计 workspace。

Node 24 是开发与构建环境要求，不改变 Go 服务运行时，也不自动提高浏览器兼容目标。
旧构建工具若无法在 Node 24 下可靠运行，应做必要的兼容修复或定向升级，并重跑测试；
不能通过切回 Node 16/18 来满足此次迁移验收。

公开前端根许可证为 Apache-2.0，与本仓库的许可证类型一致。迁入需保留原许可证、
版权和适用的归属声明，并记录本地修改；第三方依赖、字体等仍按各自声明处理。
公开下载不等于所有依赖都采用 Apache-2.0。抽查的 `@fc-components/es-query` 和
`@fc-plot/ts-graph` 未在 npm metadata 或所查 tarball 中找到许可证声明，需补核实。
不要把本机额外取得的商业扩展混入公开源码迁入。
来源：[前端 LICENSE](https://github.com/n9e/fe/blob/58e8e242e88d834282bbeb8da9a5dd8d38100d17/LICENSE)、
[es-query 版本元数据](https://registry.npmjs.org/@fc-components%2fes-query/0.0.3)、
[ts-graph 版本元数据](https://registry.npmjs.org/@fc-plot%2fts-graph/0.27.2)。

## 推荐的维护方式

| 方式 | 对当前工作流的影响 | 判断 |
| --- | --- | --- |
| 普通目录导入固定源码快照 | 一次 checkout、一次提交和 PR，前后端可原子修改；通过来源记录和独立同步提交跟进上游 | 推荐；当前自主开发优先 |
| Git subtree | 源码仍在当前仓库，可建立更明确的上游导入/同步关系；同步路径和历史管理复杂度更高 | 若频繁同步整批上游改动，可再选择 |
| Git submodule | 根仓库只记录外部提交，仍有两套提交、推送和权限；worktree/CI 需额外初始化 | 不符合这次减少跨仓库维护成本的目标 |

建议布局：

```text
frontend/                 # 前端源码、public、plugins、锁文件和测试
frontend/.node-version    # 精确固定 Node 24.x 补丁版本
frontend/pnpm-lock.yaml   # 唯一维护的前端依赖锁文件
frontend/UPSTREAM.md      # 来源仓库、tag、完整 SHA、导入范围、同步记录
front/statik/statik.go    # 保留现有 Go import 路径；生成资源的存放点
scripts/                 # 统一构建、验证、预览和证据入口
```

不复制前端仓库的 `.git`、node_modules、构建产物、凭据和机器环境文件。
上游的 AGENTS/CLAUDE、hooks 和 CI 要与本仓库规则协调，不能直接叠加执行。
例如上游 AGENTS 默认禁止未经明确请求运行 build/tsc，与这里的强制工程验证要求不一致。
来源：[上游 AGENTS](https://github.com/n9e/fe/blob/58e8e242e88d834282bbeb8da9a5dd8d38100d17/AGENTS.md)。

## 必须配套的工程改造

仅复制源码不能解决发布一致性。建议按以下范围接通完整链路：

| 范围 | 必须达成的行为 |
| --- | --- |
| 构建 | 提供统一前端构建入口，固定 Node 24 与 pnpm 精确版本，使用 pnpm 锁文件并清理旧输出；明确构建环境和 mode，产生源码指纹、lock 摘要及产物摘要。源码为权威输入，statik 只作为派生产物 |
| 普通 Go 开发 | 可阶段性保留 committed statik 以支持无 Node 的后端快速构建；不得把这个快速路径当成前端源码验证。明确缓存更新规则，避免长期双源漂移 |
| 预览 | 默认接入本 worktree 构建的 `frontend/pub/`，继承现有复制、摘要和漂移检查；构建失败不能退回旧 UI。每个 worktree 独立 node_modules、构建输出和浏览器状态，可共享下载缓存 |
| 稳定实例 | 当前 stable 禁止外部 frontend-dir；需要让它从干净 main 的同仓源码构建或使用可核验的配套产物，继续保持独立实例规则 |
| 验证与 CI | 前端变动触发类型/构建/相关测试；强制核验 Node 24 和固定 pnpm，安装使用 frozen lockfile，缺少工具或安装失败不能默默跳过。保留统一 `make verify` 入口，处理公共入口和前后端契约变化的联动检查 |
| E2E | 将上游测试适配到隔离 preview 的地址、随机账号、fixture 和清理规则；优先覆盖登录、权限、仪表盘、规则编辑和相关告警路径，直接访问最终产物，不能只测 Vite dev server |
| 发布 | 去掉正常发布对上游 latest 的依赖；前端构建一次，将同一份静态资源用于各架构打包，最终产物再冒烟。记录后端提交、前端来源/本地指纹、环境、依赖摘要和资源摘要 |
| QA 代码语料 | 从同仓 `frontend/src/` 取当前源码，保留商业目录过滤，更新来源信息，避免 UI 已变而代码问答仍取上游旧版本 |
| 文档与规则 | 更新 AGENTS、CLAUDE、工作流、环境和预览手册；同步忽略规则，防止 node_modules/产物进入源码指纹或提交 |

现有可复用基础：后端已有文件前端和 statik 两条读取路径；preview 已能复制本地产物并
核对摘要。需要重点修改的是 [preview](../scripts/preview.py)、[verify](../scripts/verify.py)、
[Makefile](../Makefile)、[fe.sh](../fe.sh)、[GoReleaser](../.goreleaser.yaml)、
[发布 CI](../.github/workflows/n9e.yml)、[验证 CI](../.github/workflows/verify.yml)、
[QA 语料脚本](../scripts/build-qa-code-assets.sh)。无需为迁入改变现有 Go 包的公开入口。

前端兼容验收要特别覆盖权限：候选 Vite 配置含仅开发服务器生效的 n9e-plus 权限路径重写，
生产静态页面不会经过它。必须以普通构建产物验证当前 Go 后端，不能把开发代理成功视为
发布成功。这是应验的风险点，本轮未证明它在普通构建下构成实际缺陷。

## 建议实施顺序与完成条件

1. **候选与工具链验证。** 在一次性环境固定源码 SHA，迁移至 Node 24 + pnpm，
   转换并审查锁文件，完成 frozen 安装、普通 build、所选 Jest 测试；
   确认依赖可获取性、兼容性和资源消耗。明确这次是否接受 v9.2.0 UI 升级。
2. **源码与链路迁入。** 单独记录原样源码导入，再接入统一构建、preview、verify 和发布；
   来源同步与本地功能修改分开提交，便于以后识别差异。
3. **闭环验收。** 从全新 checkout 构建前后端，在隔离实例跑真实浏览器 E2E；
   做一次小页面改动验证“源码变化 → 门禁 → 预览 → 版本证据 → 发布产物”全部对应。
   再验证构建失败不能复用旧包、跨 worktree 不串产物，以及普通 pre-push 仍有效。

整体为中等规模基础设施改造。源码导入本身简单；主要不确定性在工具链复现、
旧 UI 到候选 UI 的兼容性和测试环境适配，不能据文件复制量估算完成。

## 本轮验证边界

已完成本地代码、构建脚本和 CI 的只读调查，statik 解码与历史产物内容比较，
固定前端提交的 Git tree、配置、锁文件和部分 npm 元数据核查。
未安装 npm 依赖、未编译前端、未执行上游 Jest/Playwright、未运行迁入后的业务 E2E，
未修改预览实例、发布配置或远端规则。

首次分析时文档相对链接和 `git diff --check` 通过；`make verify` 通过，18 项 Python 测试通过，
Go（含子测试）2,613 passed、0 failed、39 skipped。未执行 `make verify-all`。
追加 Node 24 + pnpm 要求时只做文档与链接检查，未执行锁文件转换或目标工具链构建。
这些检查针对当前仓库，不能作为候选前端已安装、编译、兼容或迁入成功的证据。
