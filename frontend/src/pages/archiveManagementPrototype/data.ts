// Throwaway UI fixtures. No API calls, uploads or durable storage.
export type Category = 'training' | 'inspection' | 'incident' | 'other';
export type Variant = 'A' | 'B' | 'C';
export type DocumentItem = { id: string; name: string; kind: 'online' | 'pdf' | 'word'; purpose: string; size: string };
export type ArchiveRecord = {
  id: string;
  category: Category;
  title: string;
  status: '草稿' | '已归档';
  department: string;
  person: string;
  date: string;
  updated: string;
  location: string;
  result: string;
  duration: string;
  scope: string;
  level: string;
  type: string;
  summary: string;
  documents: DocumentItem[];
  body?: string;
};

export const categoryLabels: Record<Category, string> = {
  training: '培训服务',
  inspection: '巡检报告',
  incident: '故障报告',
  other: '其他文档',
};
export const variantNames: Record<Variant, string> = { A: '标准列表', B: '列表与详情', C: '资料卡片' };

export const records: ArchiveRecord[] = [
  [
    'tr01',
    'training',
    '网络设备日常运维与故障排查培训',
    '已归档',
    '信息技术部',
    '王明',
    '2026-10-22',
    '2026-10-23 09:42',
    '总部 · 第三会议室',
    '正常',
    '2',
    '网络、交换机',
    '',
    '培训讲义',
    '围绕网络设备日常巡检、故障定位和应急处置展开，结合实际操作完成培训。',
  ],
  [
    'tr02',
    'training',
    '数据库备份与恢复操作培训',
    '已归档',
    '运维保障部',
    '陈静',
    '2026-10-20',
    '2026-10-22 16:35',
    '总部 · 第二会议室',
    '正常',
    '3',
    '数据库',
    '',
    '培训讲义',
    '熟悉备份检查、恢复演练及数据完整性验证的标准流程。',
  ],
  [
    'tr03',
    'training',
    '新员工信息安全与账号管理培训',
    '草稿',
    '综合管理部',
    '李文',
    '2026-10-18',
    '2026-10-21 14:08',
    '线上培训',
    '正常',
    '1.5',
    '安全设备',
    '',
    '培训讲义',
    '介绍账号安全、信息分级、日常安全习惯及事件上报方式。',
  ],
  [
    'tr04',
    'training',
    '机房基础设施操作规范培训',
    '已归档',
    '运维保障部',
    '赵晨',
    '2026-10-16',
    '2026-10-17 11:26',
    '数据中心 · 培训区',
    '正常',
    '2',
    '机房设施',
    '',
    '培训讲义',
    '讲解机房出入、设备操作及值班交接中的关键事项。',
  ],
  [
    'tr05',
    'training',
    '监控告警平台使用与值班交接培训',
    '已归档',
    '信息技术部',
    '王明',
    '2026-10-14',
    '2026-10-15 10:18',
    '总部 · 第一会议室',
    '正常',
    '2',
    '运维平台',
    '',
    '培训讲义',
    '统一监控平台使用方式，明确告警确认、处置记录和交接要求。',
  ],
  [
    'tr06',
    'training',
    '存储系统容量管理与扩容培训',
    '草稿',
    '信息技术部',
    '陈静',
    '2026-10-12',
    '2026-10-13 15:46',
    '线上培训',
    '正常',
    '3',
    '存储',
    '',
    '培训讲义',
    '介绍容量监测、扩容前检查及扩容后的验证步骤。',
  ],
  [
    'in01',
    'inspection',
    '2026 年 10 月网络设备例行巡检',
    '已归档',
    '运维保障部',
    '赵晨',
    '2026-10-22',
    '2026-10-23 10:15',
    '总部数据中心',
    '正常',
    '',
    '网络、交换机',
    '',
    '巡检报告',
    '核心链路和设备状态正常，配置备份完整，未发现异常。',
  ],
  [
    'in02',
    'inspection',
    '服务器与存储运行状态巡检',
    '已归档',
    '信息技术部',
    '陈静',
    '2026-10-21',
    '2026-10-22 17:32',
    '第二数据中心',
    '异常',
    '',
    '服务器、存储',
    '',
    '巡检报告',
    '一台服务器存在磁盘告警，已安排复核与更换，业务暂未受影响。',
  ],
  [
    'in03',
    'inspection',
    '安全设备策略与运行巡检',
    '草稿',
    '信息技术部',
    '李文',
    '2026-10-20',
    '2026-10-21 11:20',
    '总部数据中心',
    '正常',
    '',
    '安全设备',
    '',
    '巡检报告',
    '检查策略变更、设备运行状态及日志留存情况。',
  ],
  [
    'fa01',
    'incident',
    '办公网络间歇中断故障报告',
    '已归档',
    '信息技术部',
    '王明',
    '2026-10-21',
    '2026-10-22 16:50',
    '总部办公区',
    '已恢复',
    '',
    '办公网络',
    'Ⅲ级',
    '故障报告',
    '链路异常导致局部办公网络间歇中断，切换链路后恢复，完成配置复核。',
  ],
  [
    'fa02',
    'incident',
    '业务系统访问超时故障报告',
    '草稿',
    '运维保障部',
    '赵晨',
    '2026-10-19',
    '2026-10-20 09:12',
    '生产业务区',
    '已恢复',
    '',
    '业务访问',
    'Ⅱ级',
    '故障报告',
    '排查连接池和后端资源占用，完成服务恢复与复盘记录。',
  ],
  [
    'ot01',
    'other',
    '数据中心值班交接操作规范',
    '已归档',
    '运维保障部',
    '李文',
    '2026-10-18',
    '2026-10-22 13:18',
    '',
    '有效',
    '',
    '',
    '',
    '操作规范',
    '记录值班检查、交接项目和紧急联络方式，便于日常使用。',
  ],
  [
    'ot02',
    'other',
    '网络变更窗口与回退说明',
    '草稿',
    '信息技术部',
    '王明',
    '2026-10-20',
    '2026-10-21 15:28',
    '',
    '有效',
    '',
    '',
    '',
    '操作手册',
    '梳理变更前检查、执行步骤、观察要点及回退条件。',
  ],
].map((row) => {
  const [id, category, title, status, department, person, date, updated, location, result, duration, scope, level, type, summary] = row;
  return {
    id,
    category: category as Category,
    title,
    status: status as ArchiveRecord['status'],
    department,
    person,
    date,
    updated,
    location,
    result,
    duration,
    scope,
    level,
    type,
    summary,
    documents: [
      {
        id: `${id}-online`,
        name: `${category === 'training' ? '培训讲义' : title} · 在线文档`,
        kind: 'online' as const,
        purpose: category === 'training' ? '培训讲义' : '正文',
        size: '在线文档',
      },
      {
        id: `${id}-pdf`,
        name: category === 'training' ? '培训签到表.pdf' : `${title}.pdf`,
        kind: 'pdf' as const,
        purpose: category === 'training' ? '签到资料' : '报告附件',
        size: '1.8 MiB',
      },
      ...(category === 'training' ? [{ id: `${id}-word`, name: '培训材料原件.docx', kind: 'word' as const, purpose: '上传原件', size: '2.4 MiB' }] : []),
    ],
  };
});

export function documentBody(record: ArchiveRecord) {
  return (
    record.body ||
    `<div class="paper-kicker">${categoryLabels[record.category]} / ${record.id.toUpperCase()}</div>
    <h1>${record.title}</h1>
    <p class="paper-subtitle">编写：${record.person}　　日期：${record.date}　　版本：V3.0</p>
    <div class="paper-summary"><strong>文档摘要</strong><p>${record.summary}</p></div>
    <h2 id="section-goal">一、${record.category === 'training' ? '培训目标' : '工作概述'}</h2>
    <p>规范日常操作流程，统一关键检查项和记录要求，确保每个步骤可复核、执行过程可追溯。</p>
    <p>本次内容覆盖准备、执行、验证和归档四个环节。参与人员应结合现场情况逐项确认，发现异常及时记录。</p>
    <h2 id="section-plan">二、${record.category === 'training' ? '培训安排与内容' : '工作内容与检查项'}</h2>
    <table><thead><tr><th>阶段</th><th>主要内容</th><th>负责人</th></tr></thead><tbody>
    <tr><td>准备与检查</td><td>核对环境信息、操作范围及前置条件</td><td>${record.person}</td></tr>
    <tr><td>执行与记录</td><td>按流程操作，记录关键结果及异常情况</td><td>${record.person}</td></tr>
    <tr><td>复核与留档</td><td>确认结果，整理文档与补充材料</td><td>${record.person}</td></tr></tbody></table>
    <h2 id="section-steps">三、操作要点</h2>
    <ol><li>操作前确认当前环境与计划内容一致，核对关键参数。</li><li>执行期间做好过程记录，异常情况先确认影响范围。</li><li>完成后复查结果，补充报告及相关资料。</li></ol>
    <blockquote>关键原则：操作有记录，结果可验证，资料可追溯。</blockquote>
    <h2 id="section-result">四、总结与后续安排</h2>
    <p>${record.summary}后续按照统一流程开展复查和资料维护。</p>
    <p>相关讲义、签到资料和补充文件统一保存在本档案中，可从左侧文档列表切换查看。</p>`
  );
}
