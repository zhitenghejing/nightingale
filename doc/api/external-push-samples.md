# 1.0业务样例对照与本地适配验证

本轮只做脱敏样例对照和本地模拟，不开放生产模式、不访问1.0生产接口、不新增人员匹配、短信电话或下游状态查询。业务方明确确认的事件类型为：**problem=故障，ok=恢复**。此确认不等于其他字段、传输格式或后续通知协议已确认。

实施前基线是上一轮完整工作区（预览构建20261027T122553Z-060e1729），不是只比较HEAD；HEAD仍为ed46238250ca41c9ff881a3ffe454d17ac8f07ef，已有实现和修复均未提交。本轮开始时已在仓库外保存源码副本及完整文件哈希，以单独提取本轮diff；不覆盖前轮改动。

## 原始附件观察与脱敏

附件为9条带未加引号键/值的展示对象，不是标准JSON；其中3条problem、6条ok。按字段一致性可以对照出三组样本，另外三条仅有恢复记录：

| 对照组 | 附件行号（故障 / 恢复） | 业务取值结构（已脱敏） | 两时刻间隔 | 结果 |
| --- | --- | --- | --- | --- |
| P1 | R3 / R2 | C1、主机、单独IPv4地址、触发项含括号、9个展示标签 | 722秒 | 同company/device/name/problemTime/trigger/level/tags/sign；恢复补recoveryTime |
| P2 | R4 / R1 | C1、数据库、名称前缀-IPv4、4个展示标签 | 8279秒 | 同上 |
| P3 | R8 / R6 | C2、服务器、名称前缀-IPv4、触发项含连字符、6个展示标签 | 60480秒 | 同上 |
| 仅恢复 | R5 / R7 / R9 | 含服务器、网络交换机；R7设备名含括号、接口路径斜杠及尾部连字符；R9出现C3 | 不推断 | 没有对应problem样本，不补造故障 |

上述“对照”只说明样本字段一致，不是1.0配对算法。尤其R6与R7设备类型、名称和触发项不同却具有相同sign，不能把sign当事件唯一键或配对身份，也不据此猜测其生成规则。

仓库不保存附件原文、真实客户标识、内网地址、原设备/触发项/标签内容或原sign。测试样例手工替换为TEST标识、文档示例地址段192.0.2.0/24、198.51.100.0/24及203.0.113.0/24，日期平移，保留上述三对的时间间隔和字符边界。没有使用原附件自动生成生产配置。

## 字段对照与明确的类型假设

规范化仅用于本地模拟：十个字段均序列化为JSON字符串；deviceTags采用“包含方括号展示内容的字符串”，不是JSON数组。原附件的无引号`[ ... ]`不能证明线上类型。测试仍以已有实现的POST表单字段json承载一个完整JSON对象，不发送事件数组；此本地规范化不能代替客户原始请求抓包或业务程序解析验证。

| 字段 | 样本观察 | 当前映射与本轮验证 | 剩余边界 |
| --- | --- | --- | --- |
| alarmType | problem / ok | IsRecovered=false→problem，true→ok；后端校验确认值，新建配置只读默认 | 已确认；不再列为未知恢复值 |
| companyId | 3种名称文本，匿名为C1/C2/C3 | 现有通知规则Params.company_id字符串；同一私有部署、同一渠道/目的端/地址支持分别取值 | 不硬编码医院，不把本地客户ID当外部标识；总部/分部真正归属规则仍待确认 |
| deviceType | 主机、数据库、服务器、网络交换机 | Params.device_type逐规则配置；同地址不同事件无需全库一致 | 观察值不等于完整生产词典，内部类型如何映射仍待确认 |
| deviceName | IP、前缀-IP、带接口路径的名称 | 指定标签键取完整值，不解析IP/端口或裁剪字符；另测TCP端口、空格、+ & = %、引号和反斜杠 | 缺失/歧义仍校验失败；额外TCP边界是补充测试，不冒充附件中的第四对 |
| problemTime | 三对内完全相同 | 原始持久事件FirstTriggerTime；恢复沿用实际受理故障的不可变problemTime | 时区用显式Asia/Shanghai测试假设，真实时区/精度仍需确认 |
| recoveryTime | problem为空，ok有时间 | RecoverTime；构造历史时依原ToHis契约将LastEvalTime同步为恢复时刻；恢复补值 | 未改变告警生命周期；其他来源若时间缺失仍拒绝猜测 |
| triggerName | 三对内相同；可含括号/连字符 | 原持久事件RuleName；恢复保留原故障字段 | 文本相同不证明接收端用此字段配对 |
| level | 警告 / 严重 | 显式level_map JSON；本地假设内部1→严重、2→警告，逐项核对 | 不据样例确定全量等级映射 |
| deviceTags | 无引号方括号展示，条目数3/4/5/6/9 | 本地JSON字符串假设；表单解码后类型和值不变 | 真实类型、内容语法、顺序/空格语义待确认 |
| sign | 同对相同，且不同对象间也可相同 | 所有脱敏记录显式用同一个TEST_ONLY值；11个派发仍各建独立任务 | 不复制原sign，不推断算法，不把它用作UUID、去重键或生产默认 |

现有映射按“本次实际适用的通知规则”选择，不按接收地址扫描全库强制companyId/deviceType相同。总部和分部可以配置各自合法业务参数；这只是发送端能力，不代表已确认1.0必然用companyId或其他哪个字段识别总部/分部。没有新增branchId或客户主数据系统。

## 本地模拟用例与证据

- 脱敏输入：[business_samples.sanitized.json](../../pkg/externalpush/testdata/business_samples.sanitized.json)。9条样本按附件顺序保留（恢复在故障前出现）；另有清楚标记的纯合成字符边界故障/恢复一对。
- 公共入口：[business_samples_test.go](../../pkg/externalpush/business_samples_test.go)：保存原始事件/现有通知规则→RegisterDispatch持久登记→Worker.Step→HTTP回环接收端→Get查询任务、attempt。每次路由重复并重入登记，仍一派发一任务；不模拟内部发送模块。
- 相同渠道、相同逻辑目的端、同一个127.0.0.1接收地址，实际收到8次POST：附件3对共6次，补充边界对2次；3个companyId、4个设备类型均有实际请求。每次只有一个json表单字段，Content-Type为application/x-www-form-urlencoded; charset=UTF-8；解码JSON与预期对象全字段相等，deviceTags仍是字符串。
- 三条仅恢复记录保留waiting_predecessor、零次attempt，明确没有实际故障；不因sign相同或problemTime相同误合并，不自动补造故障。
- 所有11个派发使用同一个显式测试sign，但任务ID互不合并；每对使用测试明确分配的incident UUID，故障发送在恢复之前。测试不推断1.0配对算法。
- 模拟响应为HTTP200、业务数字status=200、msg=TEST_ACCEPTED_LOCAL_ONLY；8条任务记录accepted且各一次attempt。只是本地模拟接口受理，不是1.0真实保存或人员通知结果。
- 旧TEST类型配置明确拒绝；以旧pending快照作为隔离DB输入时，worker保存校验失败、零attempt、零请求，不静默把旧任务改成problem/ok后重发。已受理/未知历史状态不回写。

可重复命令：

```bash
N9E_EXTERNAL_PUSH_SAMPLE_EVIDENCE=/tmp/external-samples-http-evidence.json \
  go test ./pkg/externalpush -run '^TestSanitizedBusinessSamples' -count=1 -v
go test -race ./pkg/externalpush \
  -run 'TestSanitizedBusinessSamples|TestLegacySimulationSnapshot' -count=1
```

测试默认不写仓库；仅显式设置证据路径时输出脱敏请求/响应。提交的 [模拟请求响应证据](external-push-sample-evidence.json) 只来自脱敏测试。真实附件不参与运行时解析，也不经过HTTP发送。

## 最小待确认协议清单

1. **原始线上表示**：实际请求的Content-Type、表单json字段内的原始字符格式/编码、deviceTags究竟是数组还是字符串及其内容语法；展示文本不能代替抓包。当前表单+规范JSON+字符串标签只作为本轮模拟约定。
2. **归属及词典**：客户/总部/分部到底由哪些原字段和值识别，companyId与本地规则映射关系，设备类型/名称与等级允许值；不新增branchId、不默认医院或内部客户ID。
3. **sign**：生成算法、覆盖字段/规范化规则及凭据，需可验证签名向量；原sign绝不作为配置默认或唯一键。
4. **生命周期业务**：1.0实际配对规则、重复提醒策略、恢复被本地过滤时是否必须同步，及时间的时区/精度/空值要求。problem/ok已经确认，不再包含在待确认项内。
5. **接收与后续验收**：异常实际落库和重复行为的核对办法；接口保存与后续人员通知分别验收。当前没有可靠下游查单或去重，不据异常自动判未落库，不新增下游查询功能。

## 本轮关键diff及兼容影响

- models/external_push.go：新增已确认problem/ok常量，配置验证拒绝旧/错误类型，测试sign必须显式非空白。只保留simulation和数字回环限制，不开放生产；原字段/导出方法签名和表结构不变，无SQL迁移。
- frontend通知渠道默认/表单/中英文说明：新建默认problem/ok且只读，仍默认关闭，地址、客户映射及测试签名没有生产可用默认。旧配置不自动更新，需核对后新建关闭的模拟配置；旧任务不自动回放。
- pkg/externalpush新增脱敏样例、端到端任务测试及旧快照不发送回归；已有用例和本地引擎脚本改用确认类型。保留“TEST_*”只用于拒绝旧类型的负例。
- pkg/i18nx/var.go：确认值及旧配置处置原因中文说明。
- 当前配置文档更新；前期调查/审查报告注明协议状态已更新，历史日志不被伪装成新协议证据。
- 既有worker顺序、去重唯一约束、未知结果保护、人工审计、中心发送和旧普通渠道均未重构。首次持久任务登记前崩溃窗口、真实edge双进程及生产模式未验边界仍保留，不能宣称端到端恰好一次。

本轮相对上述工作区基线的 [关键增量diff](external-push-samples-key.diff) 包含配置常量/校验、表单默认/只读、文案和模拟脚本。新增完整脱敏fixture、行为测试及证据另见前文链接；不是从HEAD累计实施diff中挑选最后一次提交。基线manifest SHA256为`ed1f81ff2494592b0f7129695996fd72b9f9a9eb495b42bbca43cb5b13792d60`，本轮没有新增提交。

## 检查与审查结果

- TDD：后端旧实现接受未知恢复值的新增用例先失败；前端确认类型默认/只读用例先失败；修复后分别通过。日志保留于本地`/tmp/external-samples-types-{red,green}.log`及`external-samples-ui-{red,green}.log`。
- 聚焦行为：models、pkg/externalpush、alert/dispatch和center/router通过；新增样例/旧快照回归及race通过。相同接收地址的多归属/多设备类型、所有字段表单往返、故障恢复顺序、孤立恢复、同sign不合并均有实际请求/任务断言。
- `make verify`通过：Go2753项通过、0失败、48项跳过（含子测试）；前端213套、1851项通过。跳过项未验证，未将未配置的外部服务测试视为通过。
- `sh fe.sh`完成当前源码构建及statik ZIP配对；`N9E_TEST_FRONTEND_DIR="$PWD/frontend/pub" go test ./front/statik -count=1`通过。Node24.22.0、pnpm10.32.1，前端源码SHA256=`fcd67f5a90351b1e80ee769a2abf6c15d2e2bc5bd82dca0d18542706e39ecdf1`。
- 本轮无迁移变化，样例行为测试使用隔离SQLite；不据此声称新验证MySQL/PostgreSQL迁移、真实edge双进程或生产接入。后续预览独立PostgreSQL运行验证单列，不替代全部数据库模式测试。

### Standards

只读规范轴复核未发现新增阻塞问题：保留原导出接口/表结构，模拟回环限制和默认关闭不变，statik与源码配套，脱敏样例及原始信息隔离。不需扩大重构。

### Spec

只读需求轴复核未发现新增阻塞问题：已确认problem/ok，三对完整映射、同sign独立任务、未知协议隔离及旧快照阻止发送均符合范围；第三个companyId实际请求来自明确标注的补充合成对，不冒充原附件中仅恢复的故障。

两轴新增阻塞发现均为0；人工验收尚未执行。

## 更新构建与Agent页面检查

功能及页面测试构建`20261027T133452Z-e8505427`，私有独立PostgreSQL数据库/miniredis，basic预览，未连接生产数据。LAN入口`http://192.168.0.152:30305`可达，公网dev5入口的TLS探测失败；不将本地通过写成公网可用。交付前文档补记后再次刷新，最终build ID及源码一致性在交付消息列出。

- 执行`python3 -B scripts/external-push-preview-smoke.py --local-only --output /tmp/external-samples-engine-evidence.json`退出0。合成表达式经过真实告警引擎、现有通知规则、任务持久化、center worker、回环HTTP和查询：problem→ok→新一轮problem共3次请求/accepted、各一次attempt。前一对同incident且problemTime相同，新故障不同incident。不是1.0真实接口或人员通知验收。
- Playwright经LAN页面实际保存/重新打开模拟配置：默认关闭、空地址/测试sign，problem/ok只读、必填校验正常。临时启用仅限自有回环fixture；同派发多规则矛盾保存validation_failed、0attempt、0请求，页面可查具体原因。
- 任务详情、只读刷新/重载、接口受理措辞检查通过；取消空理由不提交，显式填写后仅一次版本保护POST，终态not_delivered及cancel_unsent操作者/原因留痕，未标为已收到；无页面异常。脚本证据`/tmp/external-samples-ui-evidence.json`。
- 引擎和浏览器自建通知渠道/规则结束后关闭、告警规则disabled，本地模拟服务器停止。最终另做已受理任务无重发/取消按钮、取消状态筛选和只读查询核验；页面检查不代替业务方人工验收。
- 原始信息扫描：本轮25个文件（24个文本、1个配套ZIP）以及ZIP内655个文本资源，针对附件原客户标识、完整设备名、sign及原IP串/URL解码变体，未命中。所有提交的请求/响应证据均为脱敏合成输入；扫描不是对所有可能敏感语义的自动证明。
- 既有构建体积和React/Antd测试警告仍存在，未造成检查失败；初次刷新期间补写文档被源码身份检查明确中止，冻结后刷新成功，没有交付失配构建。

关键位置：`models/external_push.go:18`（确认常量）、`:115`（确认值及显式测试签名校验）、`frontend/src/pages/notificationChannels/constants.ts:81`（新建默认）、`pages/Form/ExternalPush.tsx:19`（确认类型只读）、`pkg/externalpush/business_samples_test.go:40`（三对/边界持久化HTTP验证）、`:235`（旧快照不发送）。具体diff见前文；行号以当前工作区为准。

仍须先取得线上格式/标签类型、归属/设备/等级词典、签名向量及生命周期政策，再另行设计并授权测试环境联调。当前配置验证仍拒绝生产模式和非数字回环地址，未提供生产可用签名或下游通知功能。
