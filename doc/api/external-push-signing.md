# 外部推送签名与独立密钥文件

部署人员请先阅读[1.0平台推送部署与密钥操作手册](../deployment/platform10-push.md)，其中按直接运行和Docker列出操作步骤、升级保留及密钥轮换方法；下文保留签名实现和历史验证记录。

更新：业务已确认UTF-8、POST表单及真实接收地址。新增的1.0平台模式只允许该精确HTTPS地址，预置媒介仍关闭；本轮用户要求只配置、不执行真实推送。见[当前预置与字段来源](platform10-preset.md)。下文签名阶段的本地验证记录保留。

## 规则与明确边界

```text
sign = UPPER_HEX(MD5(UTF8(RemoveASCIISpaces(problemTime) + privateKey)))
```

仅移除最终报文problemTime中的普通半角空格，保留日期连字符、冒号等其他字符；密码原值不做TrimSpace、大小写转换或自动删除换行。输出32位大写十六进制。业务方确认了时间处理、拼接、MD5及大写转换；UTF-8已由业务方补充确认；真实凭据和实际接收结果仍待后续明确指令验证。

恢复先按原有机制继承实际受理故障的problemTime等字段，再签名。同一problemTime及密钥得到相同sign，不同设备甚至不同客户也可能相同，sign绝不作为轮次、派发唯一键或去重键，1.0具体配对算法仍未确认。

## 存在哪里、谁能读取

- 默认版本引用：`platform10-v1`。
- 默认文件：`/etc/n9e/secrets/platform10.key`，位于Git仓库之外，单独在客户服务器配置。
- 渠道只保存`signing_mode=key_file`及`signing_key_ref`（包含版本的标识，不能填写文件路径/密码），通知规则不重复保存密码。
- center进程持有路径注册表，worker在新尝试出网之前读取密钥、生成并持久化最终报文。edge/alert仍只经CenterApi登记，不读取文件；原导出Start入口保留，center使用新增StartWithKeys传入实例自己的注册表。
- 密码及“时间＋密码”的拼接输入不会进入任务快照、数据库、API或日志。任务保存生成后的sign，管理员JSON视图仍统一显示`[REDACTED]`；渠道查询只有引用，密钥状态接口不返回文件路径、内容或长度。
- key_file读取失败只阻止相应任务；center照常启动，其他渠道照旧。空模式继续解释为原来的test_value，旧显式测试签名配置和任务不会自动迁移或回放。新建渠道默认关闭，默认选择key_file；test_value仍只供显式本地模拟。

签名材料只通过独立文件提供。文件必须是可读取的普通文件、1–65536字节有效UTF-8，不含BOM、CR/LF或NUL，不允许空或全空白；包含有效字符的首尾空格原样保留。推荐0400/0600权限；允许受控组只读0440/0640，拒绝执行、组写、其他用户访问。文件目录应仅供部署管理员/服务账号管理，可使用只读Secret挂载；不支持在Git中保存真实文件。Windows权限模型未验证。

## 首次部署与路径覆盖

建议通过自带辅助工具交互输入，密码不出现在命令行参数中：

```bash
# 示例服务账号是n9e，按客户实际运行账号替换。
sudo install -d -m 0700 -o n9e -g n9e /etc/n9e/secrets
sudo -u n9e python3 /opt/nightingale/scripts/external-push-key.py
```

工具默认写上述路径，以0600权限独占创建，拒绝覆盖已有文件，也拒绝在自身Git工作区内创建。终端无法关闭回显时直接失败，两次密码输入均不允许降级为可见输入。自定义路径使用`--path /absolute/private/path/platform10-v1.key`。自动化部署可用`--stdin`读取准确字节，必须由部署端的密钥源供给，不能将密码写成命令参数、仓库脚本常量或带换行的echo输出。没有密码常量或真实密码示例。工具只负责文件创建；既有路径、所有者/组及运行账号由部署端配置。

在center实际使用的配置目录内增加/修改TOML文件，只填写引用和绝对路径，不填写密码：

```toml
[Center.ExternalPushKeyFiles]
platform10-v1 = "/absolute/private/path/platform10-v1.key"
# 版本轮换使用新引用和新文件，不覆盖旧版本：
# platform10-v2 = "/absolute/private/path/platform10-v2.key"
```

未覆盖的默认引用沿用默认路径。修改注册表需重启center；已有注册路径下首次配置文件后，状态查询和后续新任务可以直接读取，不需要重启。路径不在渠道数据库和任务中，升级必须保留部署配置及独立文件，不能随安装包替换。

Docker部署将宿主机私有文件只读挂载到容器内默认路径，宿主机路径可自行选择：

```yaml
services:
  n9e:
    volumes:
      - /srv/n9e-private/platform10-v1.key:/etc/n9e/secrets/platform10.key:ro
```

该片段是部署说明，未修改任何客户或稳定实例。确保文件所有者/受控组与容器实际运行UID/GID匹配；Docker Secret默认0444权限需调整为0400或受控0440。密钥不放入镜像构建上下文或镜像层。单独备份/分发密钥，升级镜像保留宿主机文件。

## 页面与异常处置

通知媒介→1.0平台推送→签名方式选择“center密钥文件”，填写包含版本的引用。点击“检查密钥状态（只读）”查看当前center上文件是否可用；不会发送告警、改变任务、回显密码或上传文件。

`GET /api/n9e/external-push-signing-key?reference=platform10-v1`沿用管理员权限，返回reference/ready/reason。普通用户不能查询。修改引用后丢弃旧查询的迟到响应，不显示其他引用的就绪状态。结果只反映当前center检查时刻，不证明其他center节点已正确配置；worker领取时仍重新检查。

缺失、不可读、格式不合或未知引用的新任务保存validation_failed、0attempt、0请求，不回退到空签名/旧测试签名，也不阻止其他任务处理。已尝试过的任务若此时文件不可用，则保留manual及原尝试，不推断对方未收到。修复文件不会自动重投旧失败任务；零次尝试的旧失败任务可按现有人工操作先保留人工处置，再明确授权发送，或明确取消并等待后续正常派发。

每次网络尝试前，签名和最终JSON先在领取事务中保存。结果不明不自动重投；人工授权重发使用已保存的完整请求字节，同时检查原版本密钥是否仍匹配，绝不使用替换密码重新签一份同任务报文。恢复及正常提醒也核对同轮已受理故障的密钥版本、problemTime与签名；不匹配则阻止，不能任意取新密码。版本轮换应使用新引用/新文件，先关闭渠道并核对在途和存量任务，保留原版本直至全部旧轮次完成。

不记录密码指纹或完整拼接输入。任务登记前至首次签名前尚未固定密钥字节，版本文件必须由部署端保持不可变；首次登记前的崩溃窗口依然存在。没有增加全局outbox或端到端exactly-once承诺。

## 隔离验证与未验证范围

预览脚本为每个实例将platform10-v1覆盖到该实例独立的`secrets/platform10.key`，不读取主机/etc下的真实文件；不自动生成测试密码，不自动启用渠道。stop/refresh保留实例数据，destroy删除该实例一次性数据（含测试密钥），不触碰共享PG/Prometheus或其他实例。运行验证仅显式创建TEST密码，完成后关闭自有渠道并移除测试文件。

- Go真实文件＋隔离SQLite＋本地HTTP：固定独立向量`2026-01-02 03:04:05`、`TEST_ONLY_FIXED_KEY`→`134CD8F89958CBFEED40918C040FCE9D`；恢复沿用problemTime和sign；缺失文件只阻止对应任务；密钥首尾空格有意义；空白/换行/非法UTF8/过大文件/不安全权限拒绝。
- 未知结果及人工重发：替换原版本文件后不能发出新签名；恢复原文件并明确授权后，重发JSON字节与首次完全一致。同轮已发送故障后切换key_file/test_value，提醒和恢复都在新尝试前阻止；旧test_value→test_value兼容。没有继承通用HTTP重试。
- 管理员API：就绪状态不回显路径/密钥，普通用户403，读取不改变任务状态和版本。前端检查只发GET，旧test_value界面和保存字段保留兼容。
- 部署工具：精确字节、权限、不回显、不覆盖、换行及仓库内路径拒绝测试。

当前尚未验证原Java方法真实字符编码、真实密码或生产接口、客户Docker Secret挂载、真实edge双进程/多center及Windows。未改数据库表/SQL迁移；不能用SQLite测试代替其他部署模式。1.0归属解析、完整设备词典、线上deviceTags类型、GET/POST接入地址差异、业务配对/提醒和后续通知仍按前期清单分别确认。

## 本轮改动与复审记录

本轮增量基于开始实施时的完整脏工作区（`/tmp/external-signing-baseline-1793191662981338477/manifest.json`的逐文件SHA256及源码副本）；Git HEAD始终为`ed46238250ca41c9ff881a3ffe454d17ac8f07ef`，没有新增提交。此前外部推送修复均保留。[关键代码增量](external-push-signing-key.diff)对照该工作区，而非把前几轮内容重新算作本轮改动。

| 位置 | 本轮行为 |
| --- | --- |
| `models/external_push.go:125` | 验证key_file/test_value及版本引用；旧空模式仍解释为test_value |
| `pkg/externalpush/signing.go:36`、`:92` | 文件权限/字节校验与确认的MD5转换；不读取任意渠道路径 |
| `pkg/externalpush/worker.go:261` | 同轮模式/版本/原时间核对，事务内冻结签名后才创建attempt和出网 |
| `center/cconf/conf.go:13`、`center/center.go:234` | center路径注册表及兼容旧Start入口的实例注入 |
| `center/router/router_external_push.go:85` | 管理员只读就绪查询，不改任务或回传密码/路径 |
| `frontend/.../Form/ExternalPush.tsx`、`constants.ts` | 默认关闭、文件引用与只读检查、旧显式测试签名兼容 |
| `scripts/external-push-key.py:11` | 仓库外独占0600创建、隐藏输入、不可隐藏则拒绝，不覆盖 |
| `scripts/preview.py:410` | 预览独立路径，不共享生产密钥文件 |

Standards复审：原发现终端getpass可能警告后降级回显，以及UI旧引用的迟到响应。分别补CLI两次输入边界和Form引用切换回归，先复现失败，再修复并复审通过。无扩大重构建议。

Spec复审：原发现key_file切为test_value会绕过原故障签名核对。新增两种方向、提醒/恢复四个子场景，复现前向两例错误出网；修复后统一在attempt前验证模式，保留旧test_value路径，复审通过。复审者只读，测试由主要writer执行。

独立聚焦检查：`go test ./pkg/externalpush ./models ./center/router ./center ./alert/dispatch -count=1`、文件签名专项`-race`、页面专项5项、部署工具4项均通过。修复前后证据分别在本机`/tmp/external-signing-{config,worker,ui,noecho,mode}*`日志；不将私有运行凭据写入报告。全仓门禁与运行实例结果另行记录，外部服务跳过不算通过。

## 完整门禁、页面验收与交接

- `make verify`通过：基础设施63项；Go包含子测试2777项通过、48项跳过；前端214套/1857项通过。跳过主要涉及外部通知服务、外部数据源、MySQL/PostgreSQL专项、真实sandbox等未提供环境的测试，不能算通过。`make verify-all`未执行，未宣称全量vet和两个完整集成门禁通过。
- `sh fe.sh`及`N9E_TEST_FRONTEND_DIR="$PWD/frontend/pub" go test ./front/statik -count=1`通过，当前同仓资源一致。构建存在上游PlusPlaceholder导出/大包等既有警告，退出码为0，不隐藏为无警告。
- Agent浏览器验收build `20261028T132139Z-e590f303`：隔离PG/basic预览，实际登录、媒介默认关闭、引用保存、缺失/就绪按钮、规则字段映射保存后重开、真实引擎合成故障/恢复、持久任务29/30、本地POST表单、独立计算核对实际32位大写MD5均通过；共两请求，恢复同problemTime/sign，deviceTags为字符串，设备名称保留IP/端口/斜杠/括号。详情sign隐藏、受理状态只说明1.0接口报告接收，查询/刷新/重载零任务写操作、无页面异常。见[脱敏模拟证据](external-push-signing-evidence.json)。
- 测试结束已停用自有渠道、通知规则及合成告警规则，并删除预览独立TEST密钥；主机默认路径没有放真实或测试密码。人工验收仍待进行，未发布稳定环境。
- 本地及LAN入口正常：`http://192.168.0.152:30305`。公网`https://n9e-dev5.zhiteng.win`探测为TLS EOF失败，仅交付局域网预览，不能称公网验收通过。补充本报告后刷新构建并核验源码/前端未漂移；最终build ID见交付消息及预览status。
- 原附件中28个客户/设备/标签/sign/IP取值在本轮变更文本和statik文本成员中扫描无命中；真实客户信息、原报文和密码不加入仓库。

人工验收：打开通知媒介→打开预置的“1.0平台推送”，确认默认关闭，选择center密钥文件，仅输入版本引用；检查状态应显示未配置（临时文件已删）。查看任务29/30可核对一次故障和一次恢复的受理、尝试和脱敏报文，查询不会重发。需要再次模拟时按本文在实例私有路径创建显式TEST文件并使用本地模拟端，切勿填生产地址或真实凭据。

下一阶段最小待确认清单：

1. 用非真实密码向量核对原Java MD5的字符编码及准确字节规则；真实密码由客户部署管理员通过安全渠道单独配置，不贴到聊天、Git或规则参数。
2. 确认真实HTTP接入地址/POST表单约定、原始JSON表示及deviceTags的真实类型；其他平台的GET日志不自动改变本端约定。
3. 确认总部/分部归属使用原协议哪个字段/取值、companyId可识别文本、设备类型词典及夜莺1–3到目标四等级的实际映射；不新增branchId，不硬编码样例客户。
4. 确认1.0配对字段、重复提醒策略及本地恢复被过滤后的处置；接口报告接收与后续人员通知分别联调验收。当前不增加人员匹配、短信电话或下游状态查询。
