# 档案 API 与文档运行环境

档案模块仅由 Center 提供，入口为 `/api/n9e/archives`。所有接口复用现有登录与账号有效性检查；首版对所有有效登录账号开放，不绑定业务组。文件、草稿及导出产物均不匿名开放。

## 数据与写入规则

类别为 `training / inspection / incident / other`，状态为 `draft / archived`。业务字段定义见 `archives.Metadata`，校验集中在档案包。档案更新、归档、回收站操作携带读取到的 `revision`；冲突返回 HTTP 409 和当前修订信息。字段校验失败返回 422，缺失对象返回 404。

正式档案快照只保存业务信息与不可变文档版本引用。列表中的文档版本省略正文；读取正文需调用单文档接口。恢复历史创建新版本，回收站不物理删除原件或历史。导出固定请求时的版本，不读取私有草稿。

| 方法 | 相对路径 | 用途 |
| --- | --- | --- |
| GET / POST / DELETE | 空路径 | 分页查询、新建、批量移入回收站；批量操作同一事务，最多 100 条 |
| GET / PUT / DELETE | `/:id` | 详情、修改业务信息、移入回收站 |
| POST | `/:id/archive`、`/:id/restore` | 归档、从回收站恢复 |
| POST | `/:id/documents` | 创建在线文档，可关联原 Word 文档与确切版本 |
| PUT / DELETE | `/:id/documents/:document_id` | 修改名称/用途、从当前档案移除 |
| POST | `/:id/files`、`/:id/assets` | multipart 上传/替换附件、受管正文图片 |
| GET | `/files/:file_id` | 登录后读取文件；支持 Range；`download=true` 记录原件下载 |
| GET | `/documents/:document_id` | 当前或 `version_id` 指定版本的正文和文件信息 |
| GET | `/:id/versions`、`/documents/:document_id/versions` | 按版本倒序分页，`offset` 与最多 100 条 |
| POST | `/:id/versions/:version_id/restore`、`/documents/:document_id/versions/:version_id/restore` | 恢复历史，文档正在编辑时拒绝 |
| GET | `/:id/logs` | 真实登录账号操作记录；`offset` 分页 |
| GET / PUT | `/documents/:document_id/bookmark` | 个人阅读位置；写入携带所阅读的 `version_id` 与 `position`，不同版本不复用位置 |
| GET | `/limits`、`/licenses` | 容量约束、编辑/导出组件及传递依赖的许可证声明 |

列表参数：`category,q,state,department,location,result,level,start,end,deleted,page,limit`。`q` 搜索标题及人员；部门和地点按完整字段过滤；时间使用含时区的 ISO 格式。默认按更新时间和 ID 倒序。时间范围对培训按培训开始、故障按发生时间、巡检和其他文档按创建时间过滤；数据库投影统一为 UTC。

上传参数为 `file,purpose,document_id,session_id,epoch`。新附件 `document_id=0`；替换附件和向现有在线文档上传图片需携带该文档的编辑会话。正文图片只存 `archive-asset:<id>` 引用，浏览器临时 Blob URL 与 base64 在保存前转换为受管图片。未发布图片只允许上传者读取和首次绑定。

DOCX 校验压缩体积、路径、CRC/XML、宏及外部图片；PDF 由离线解析器验证页数及可解析性。加密 PDF、宏、SVG 正文图片及嵌入网页不在首版范围。原始文件在转换前后保持不可变。

## 编辑会话

| 方法 | 文档相对路径 | 用途 |
| --- | --- | --- |
| POST | `/documents/:document_id/lease` | 取得独占会话，返回租约、正式版本、本人私有草稿 |
| PUT / DELETE | 同上 | 心跳续租、按准确会话和代次释放 |
| PUT | `/documents/:document_id/draft` | 私有草稿；不产生正式版本或公开修改 |
| POST | `/documents/:document_id/draft/discard` | 在有效会话内明确放弃本人草稿 |
| POST | `/documents/:document_id/save` | 保存正式文档或 PDF 批注，生成新版本 |

写入携带 `session_id,epoch,base_version_id,body,mode,markdown`。刷新时可用准确会话调用 `POST /documents/:document_id/lease/resume`，恢复会轮换会话和代次，旧窗口及晚到释放请求随即失效。普通新窗口仍需独占申请；浏览器会识别克隆窗口正在使用的会话。30 秒心跳、5 分钟过期；同账号第二窗口也不能共享会话。服务端同时验证所有者、会话、代次、过期时间及基础版本，过期窗口不能覆盖新版本。网络失败后页面暂停编辑，先恢复会话验证再继续。

在线正文使用 Canvas Editor 快照，外加 `schema_version=1`。`mode` 为 `visual / markdown`；PDF 批注使用 `annotations`。进入 Markdown 时以 `switch_mode=true` 冻结保留基线；自动保存只更新 Markdown 字符串。返回可视化及正式保存均核对页设置、页眉页脚、复杂表格和浮动图片。占位块损坏时保留原稿并返回错误。系统内支持双模式，首版没有 `.md` 文件上传下载。

PDF 批注坐标以原始未旋转页面的左上角归一化到 0..1；页码从 1 开始。批注包括 `highlight / strikeout / note`，作者与修改者由服务端写入，替换原 PDF 时不复用旧坐标。

## 导出与打印

`POST /:id/exports` 请求体为 `format,document_id,version_id`。格式 `docx / pdf / annotated_pdf / information_pdf / zip`。轮询 `GET /exports/:export_id` 的 `queued / running / done / error`，完成后通过登录接口 `GET /exports/:export_id/file` 下载。中断任务不会发布半成品，重启后标记为可重试的失败。

每个服务进程最多同时执行一份转换，同一账号最多 5 份待处理任务。任务使用独立临时目录与 LibreOffice profile，超时终止整个 Linux 子进程组。导出错误日志写服务日志；前端只显示可操作的错误信息。

在线文档先生成 DOCX，再以独立 LibreOffice profile 生成 PDF；系统阅读页与打印使用这份 PDF。上传 Word 先展示可编辑转换结果，确认后形成在线副本，保留原件；不承诺与原 Word 的逐页版式一致。批注 PDF 保留原文件，使用标准 PDF 批注对象生成新文件。

整份 ZIP 包含档案信息 PDF/JSON、当前原始附件、在线文档 PDF/DOCX 及适用的批注 PDF；不包含全部历史与私有草稿。`POST /:id/print` 记录“发起打印”，不代表物理打印完成。

## 本机运行与交付

前端构建同时打包离线 Node CLI 到 `archives/renderer/worker.cjs`。Go 将其及 NOTICE 内嵌，客户运行时不依赖源码或 node_modules，不启动额外 Node HTTP 服务。单改导出 worker、依赖锁或构建脚本后执行 `pnpm archive:worker`；Go 测试会核对嵌入内容与来源摘要。

运行依赖 Node 24、LibreOffice 和中文字体。验证环境为 Node 24.22.0、LibreOffice 26.2.6.3；固定字体后再做客户部署验收。复用本机已安装的 `Noto Sans CJK SC` 字体。PDF.js worker、CMaps、ICC 色彩配置、标准字体及 WASM 随前端本地打包，不使用 CDN。标准拉丁字体固定为 Liberation Sans 2.1.5（SIL OFL 1.1），源文件、SHA-256 与许可放在 `frontend/public/archive-fonts/`；构建核对摘要，并替换 PDF.js 原先随包携带的旧字体。中文字体声明放在 `frontend/public/font/OFL-NotoSansSC.txt`。字体/编解码器各自声明合入 NOTICE；字体许可和编辑器代码许可分开保留。

```toml
[Center.Archives]
DataDir = "data/archives"
NodePath = "/absolute/path/to/node"
LibreOfficePath = "/usr/bin/soffice"
ExportTimeoutSeconds = 180
```

默认数据目录相对于 Center 的运行目录。预览的默认位置在该实例独立 run 目录下；客户环境应使用服务账号专有的持久化目录。目录包含 `objects,temporary,exports`，不得映射为静态公共目录。备份需要数据库与文件目录保持同一时点；恢复时同时恢复两者。历史不计入当前 500 MB 配额，磁盘使用需由部署方监控，首版没有永久删除历史的页面。

直接依赖与传递依赖声明在 `archives/renderer/NOTICE`；双许可证依赖选择 MIT 路线。交付时保留该声明以及单独安装的 LibreOffice、字体和 PDF.js 字体/编解码器自身声明。选用路线未引入 ONLYOFFICE 或收费分页插件；源码和部署授权仍按实际交付包逐项核对。

## 验证入口

`go test ./archives ./center/router -run TestArchive` 覆盖草稿、编辑锁、历史恢复、归档材料、上传、配额与认证。设置 `N9E_TEST_ARCHIVE_RENDERER=1` 并提供 Node/LibreOffice/Poppler 后运行真实 DOCX/PDF/批注/ZIP 导出测试，`N9E_ARCHIVE_NODE` 可指定 Node 路径。

数据库专项测试使用 `N9E_TEST_ARCHIVE_POSTGRES` 与 `N9E_TEST_ARCHIVE_MYSQL`。PostgreSQL 在指定测试数据库中创建并清理随机 `archive_test_` schema；MySQL 必须指定独立的 `archive_test_` 数据库。测试执行对应初始化 SQL、再次 AutoMigrate，并验证大正文、并发会话、保存及恢复。SQLite 通过不能替代这两项验证。

正式交接包括 `make verify`、`make verify-all` 与隔离预览浏览器验证；工程记录见[实施验证](../design/unified-business-documents/implementation-verification.md)。人工验收和生产发布保持独立，Agent 不代填人工验收结论。
