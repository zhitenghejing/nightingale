# 巡检报告与故障报告

## 页面与接口

新增与集成中心同级、排列在其下方的两个一级菜单：`/reports/inspection`（巡检报告，审核清单图标）、
`/reports/incident`（故障报告，维修工具图标）。沿用夜莺主题、主菜单和登录。
桌面为主菜单、报告清单、详情与预览三片布局；窄屏上下排列后两片。

上传必填标题、工程师、报告日期、摘要；单位和联系方式选填。
支持一个 PDF、DOC 或 DOCX 原件，每份最多 30 MiB。修改报告信息保留元数据历史；
文档更新作为新报告上传，避免覆盖归档原件。当前没有删除或定时清理接口。

接口由 center 进程原生提供，**不依赖 CMDB 资产权限**：

| 接口 | 作用 |
| --- | --- |
| GET `/api/cmdb/reports?kind=inspection\|incident` | 按类别列出本人报告；夜莺管理员可见全部 |
| POST `/api/cmdb/reports` | multipart：`metadata` JSON 和 `file`，返回归档记录 |
| PUT `/api/cmdb/reports/:id` | 更新外部报告信息，必须携带当前 version |
| GET `/api/cmdb/reports/:id/file` | 认证后下载原件 |
| GET `/api/cmdb/reports/:id/preview` | 认证后读取 PDF，支持 Range |

所有操作使用 center 的原生登录鉴权与禁用账号检查。仅本人及管理员可读取、下载、预览和编辑；
无权访问的报告返回 404。无匿名附件地址，不把凭证写进文件 URL。
前端用认证请求获得 Blob，再交浏览器 PDF 阅读器预览。

## 已实现的持久保存

配置为 `[Center.CMDB] ReportsDir`，默认 `data/reports`；相对路径按应用工作目录解析。
生产环境应指向受备份的持久数据卷。preview 显式使用实例私有 `run/reports`，
refresh/stop 保留，destroy 随实例删除。不会自动读取原独立服务的报告目录。
服务重启会重新读取报告。目录使用操作系统文件锁，第二个写进程会启动失败，
崩溃退出会释放锁。多副本部署必须把报告写入路由到唯一存储服务后再启用。

每份报告以服务生成的 UUID 为目录名：

```
<ReportsDir>/<UUID>/
  original.docx          # 或 original.doc / original.pdf，原始字节不改写
  metadata.json          # 当前信息，含来源文件名、大小、SHA-256、ownerId、版本等
  revision-000001.json   # 归档时的信息
  revision-000002.json   # 后续信息或预览状态变更，保留历史
  preview.pdf           # Word 转换的派生文件；PDF 原件直接预览
```

上传先写同盘临时目录、同步文件，再原子改名提交，列表不显示半份上传。
元数据更新有进程内锁、版本检查和原子替换。当前仅支持**单个报告服务写入此目录**。
原件校验扩展名与文件头；DOCX 检查文档 ZIP 结构与解压体积、拒绝 VBA 宏文件。
服务内一条转换任务串行运行，限制时间、CPU、地址空间及输出文件大小；
使用 bubblewrap 禁网、隔离进程/目录，只读挂载当前报告、系统运行库、字体及 LibreOffice 配置，清空继承环境，不能修改原件。
启动恢复 pending 任务。转换失败保留原件，页面显示下载入口。
转换需要 Linux user namespace、`bwrap`、`prlimit` 和 LibreOffice；不支持时不降级为无隔离执行。
真实转换通过 `CMDB_REPORT_CONVERSION_TEST=1` 专项测试验证。加密、损坏或不兼容的 Word 文件可能无法预览。
浏览器需要支持内置 PDF 阅读器，不支持时可下载本地打开。

报告索引是各报告目录的 metadata.json；资产数据使用 center 的数据库。
迁入会新增七张 CMDB 表；不会自动迁移既有资产或报告，也不会更新稳定实例。

## 长期保存及备份计划

“放在服务器持久目录”只能保证应用重启/更新后保留，不能抵御磁盘故障或误删。
正式运维应完成下面的备份配置，当前未擅自安装定时任务或接入外部存储：

1. 将 ReportsDir 固定为数据盘目录；容器部署必须 bind mount / persistent volume，
   不能放容器可写层。保留当前目录迁移副本并验证校验值。
2. 每日异机增量备份**整个 ReportsDir**（元数据和原件必须一起备份），加密、限制权限，
   建议保留每日 30 份、每月 12 份。可以使用 restic/borg 备份到 NAS 或对象存储。
   如需严格同一时点快照，暂停相应 center 进程后复制，或使用文件系统快照；报告与 center 同进程，需安排该实例的维护窗口。
3. 同时备份身份库的用户 ID 对应关系及系统配置，避免重建账号后旧 ownerId 错配。
   做一次恢复演练：隔离目录恢复、比对原件 SHA-256、读取元数据和打开 PDF。
4. 监控数据盘可用空间、备份结果及转换失败；明确保存年限和后续授权删除流程。
5. 规模扩大或多实例部署时，原件与预览迁往 S3/MinIO，使用稳定 object key，
   开启版本控制、跨机副本，必要时启用 Object Lock；报告信息迁入独立业务库。
   下载经权限校验后流式传输或发短期签名链接，数据库不存临时预览 URL。
   迁移校验后切换存储实现，现有 UUID 和原件 SHA-256 可作为迁移标识。

## 验证

```
go test ./pkg/cmdb ./center/router
# 需要正常的本机监听与 user namespace 权限；仅使用测试临时目录
CMDB_REPORT_CONVERSION_TEST=1 go test ./pkg/cmdb -run Report
pnpm --dir frontend exec jest --runInBand --testPathPattern='src/pages/reports'
```
