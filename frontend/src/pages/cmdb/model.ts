export interface Disk {
  model: string;
  description: string;
  count: number;
}

export interface Device {
  id: string;
  version?: number;
  rackId?: string;
  assetMaterialId?: string;
  customer: string;
  room: string;
  rack: string;
  uStart?: number;
  uEnd?: number;
  type: string;
  vendor: string;
  model: string;
  serial: string;
  hostname: string;
  businessIp: string;
  managementIp: string;
  business: string;
  note: string;
  cpuModel: string;
  cpuCount?: number;
  memory: string;
  memoryNote: string;
  powerCount?: number;
  os: string;
  level: string;
  lifecycle: string;
  warrantyStart: string;
  warrantyEnd: string;
  expire: string;
  warrantyType: string;
  monitored: boolean;
  alertEnabled: boolean;
  monitorState: string;
  disks: Disk[];
}

export interface Field {
  key: keyof Device;
  label: string;
  kind?: 'number' | 'date' | 'boolean';
  options?: string[];
}

export const fieldGroups: { title: string; fields: Field[] }[] = [
  {
    title: '基本信息',
    fields: [
      { key: 'customer', label: '客户信息' },
      { key: 'room', label: '机房位置' },
      { key: 'rack', label: '机柜名称' },
      { key: 'uStart', label: '起始 U 位', kind: 'number' },
      { key: 'uEnd', label: '结束 U 位', kind: 'number' },
      { key: 'type', label: '设备类型', options: ['机架式服务器', '存储', '交换机', '防火墙', '其他'] },
      { key: 'vendor', label: '厂商' },
      { key: 'model', label: '型号' },
      { key: 'serial', label: '序列号' },
      { key: 'hostname', label: '主机名' },
      { key: 'businessIp', label: '业务 IP' },
      { key: 'managementIp', label: '带外管理 IP' },
      { key: 'business', label: '所属业务' },
      { key: 'note', label: '备注' },
      { key: 'level', label: '设备等级' },
      { key: 'lifecycle', label: '生命周期', options: ['使用中', '备用', '待退役', '已退役'] },
    ],
  },
  {
    title: '硬件配置',
    fields: [
      { key: 'cpuModel', label: 'CPU 型号' },
      { key: 'cpuCount', label: 'CPU 数量', kind: 'number' },
      { key: 'memory', label: '内存容量' },
      { key: 'memoryNote', label: '内存描述' },
      { key: 'powerCount', label: '电源数量', kind: 'number' },
      { key: 'os', label: '系统版本' },
    ],
  },
  {
    title: '维保与监控',
    fields: [
      { key: 'warrantyStart', label: '原厂保修开始', kind: 'date' },
      { key: 'warrantyEnd', label: '原厂保修结束', kind: 'date' },
      { key: 'expire', label: '过保时间', kind: 'date' },
      { key: 'warrantyType', label: '保修类型', options: ['官方维保', '第三方维保', '无维保'] },
      { key: 'monitored', label: '是否监控', kind: 'boolean' },
      { key: 'alertEnabled', label: '是否开启报警', kind: 'boolean' },
    ],
  },
];

export const blankDevice: Device = {
  id: '',
  customer: '示例医院',
  room: '',
  rack: '',
  type: '机架式服务器',
  vendor: '',
  model: '',
  serial: '',
  hostname: '',
  businessIp: '',
  managementIp: '',
  business: '',
  note: '',
  cpuModel: '',
  memory: '',
  memoryNote: '',
  os: '',
  level: '',
  lifecycle: '使用中',
  warrantyStart: '',
  warrantyEnd: '',
  expire: '',
  warrantyType: '官方维保',
  monitored: false,
  alertEnabled: false,
  monitorState: '未接入',
  disks: [],
};

export function sampleDevices(): Device[] {
  const base = {
    ...blankDevice,
    room: '北院7号楼11F',
    rack: '5号柜',
    vendor: 'Lenovo',
    model: 'ThinkServer SR660 V2',
    warrantyStart: '2024-09-29',
    warrantyEnd: '2029-11-27',
    expire: '2029-11-27',
  };
  const rows: Partial<Device>[] = [
    {
      hostname: 'FORUM01',
      serial: 'DEMO-LNV-001',
      business: '论坛系统',
      uStart: 9,
      uEnd: 10,
      disks: [
        { model: '03LD420', description: 'SATA 960G', count: 2 },
        { model: '03HA238', description: 'SAS 2.4T', count: 6 },
      ],
    },
    { hostname: 'HEYEX01', serial: 'DEMO-LNV-002', business: '检验系统', uStart: 11, uEnd: 12 },
    {
      hostname: 'PACS APP01',
      serial: 'DEMO-LNV-003',
      business: 'PACS',
      uStart: 13,
      uEnd: 14,
      businessIp: '192.0.2.11',
      monitored: true,
      alertEnabled: true,
      monitorState: '在线',
      cpuModel: 'Xeon Silver（示例）',
      cpuCount: 2,
      memory: '128 GB',
      os: 'Linux（示例）',
    },
    {
      hostname: 'PACS DB01',
      serial: 'DEMO-LNV-004',
      business: 'PACS',
      uStart: 15,
      uEnd: 16,
      businessIp: '192.0.2.12',
      monitored: true,
      alertEnabled: true,
      monitorState: '心跳中断',
    },
    { hostname: '业务数据存储01', serial: 'DEMO-STG-001', business: '存储平台', uStart: 17, uEnd: 20, type: '存储', model: 'ThinkSystem DE4000H' },
    {
      hostname: 'NAS统一存储',
      serial: 'DEMO-STG-002',
      business: '文件服务',
      rack: '6号柜',
      uStart: 5,
      uEnd: 8,
      type: '存储',
      model: 'DM3010H',
      expire: '2026-11-10',
      warrantyEnd: '2026-11-10',
      warrantyType: '第三方维保',
    },
    {
      hostname: '华为存储',
      serial: 'DEMO-HW-001',
      room: '南院住院部1F机房',
      rack: 'ZY-HX-C03',
      uStart: 21,
      uEnd: 24,
      vendor: 'HUAWEI',
      type: '存储',
      model: 'DAE62435U4',
      business: '存储平台',
      expire: '2029-03-20',
      warrantyEnd: '2029-03-20',
    },
    {
      hostname: '内镜系统服务器',
      serial: 'DEMO-DELL-001',
      room: '南院住院部1F机房',
      rack: 'ZY-HX-B05',
      uStart: 9,
      uEnd: 10,
      vendor: 'DELL',
      model: 'R650',
      business: '内镜系统',
      businessIp: '192.0.2.33',
      expire: '2026-09-01',
      warrantyEnd: '2026-09-01',
      monitored: true,
      alertEnabled: true,
      monitorState: '在线',
    },
    {
      hostname: 'LIS微生物',
      serial: 'DEMO-DELL-002',
      room: '南院住院部1F机房',
      rack: 'ZY-HX-B05',
      uStart: 11,
      uEnd: 12,
      vendor: 'DELL',
      model: 'R650',
      business: 'LIS',
      businessIp: '192.0.2.35',
      expire: '2026-11-05',
      warrantyEnd: '2026-11-05',
      monitored: true,
      monitorState: '在线',
    },
    {
      hostname: 'LIS数据库服务器02',
      serial: 'DEMO-DELL-003',
      room: '南院住院部1F机房',
      rack: 'ZY-HX-B05',
      uStart: 13,
      uEnd: 14,
      vendor: 'DELL',
      model: 'R650',
      business: 'LIS',
      businessIp: '192.0.2.32',
      expire: '2026-08-16',
      warrantyEnd: '2026-08-16',
      lifecycle: '待退役',
    },
  ];
  return rows.map((row, index) => ({
    ...base,
    ...row,
    id: `CI-${String(index + 1).padStart(3, '0')}`,
    note: row.hostname || '',
    disks: row.disks ? row.disks.map((disk) => ({ ...disk })) : [],
  }));
}

export function warrantyStatus(expire: string, today = new Date().toISOString().slice(0, 10)): string {
  if (!expire || !Number.isFinite(Date.parse(expire))) return '未填写';
  const days = (Date.parse(`${expire}T00:00:00Z`) - Date.parse(`${today}T00:00:00Z`)) / 86400000;
  return days < 0 ? '已过保' : days <= 30 ? '即将过保' : '在保';
}

export function deviceError(value: Device, devices: Device[], capacity = 60): string | undefined {
  if (!value.customer.trim() || !value.serial.trim()) return '客户信息和序列号必填';
  if (devices.some((d) => d.id !== value.id && d.serial.toLowerCase() === value.serial.trim().toLowerCase())) return '序列号已存在';
  if (value.warrantyStart && value.warrantyEnd && value.warrantyStart > value.warrantyEnd) return '原厂保修开始不能晚于结束时间';
  const { uStart, uEnd } = value;
  if (uStart != null || uEnd != null) {
    if (!Number.isInteger(uStart) || !Number.isInteger(uEnd) || !uStart || !uEnd || uStart < 1 || uEnd > capacity || uStart > uEnd)
      return `机柜 U 位范围应为 1–${capacity}，且起始不大于结束`;
    if (!value.room.trim() || !value.rack.trim()) return '配置 U 位时请同时填写机房和机柜';
    if (devices.some((d) => d.id !== value.id && d.room === value.room && d.rack === value.rack && d.uStart != null && d.uEnd != null && uStart <= d.uEnd && uEnd >= d.uStart))
      return '机柜 U 位与其他设备冲突';
  }
}

export function csvRows(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [],
    cell = '',
    quoted = false;
  const source = text.replace(/^\uFEFF/, '');
  for (let i = 0; i < source.length; i++) {
    const c = source[i];
    if (c === '"') {
      if (quoted && source[i + 1] === '"') {
        cell += '"';
        i++;
      } else if (!quoted && cell.length) throw new Error('CSV 引号格式不正确');
      else quoted = !quoted;
    } else if (c === ',' && !quoted) {
      row.push(cell);
      cell = '';
    } else if ((c === '\n' || c === '\r') && !quoted) {
      if (c === '\r' && source[i + 1] === '\n') i++;
      row.push(cell);
      if (row.some((value) => value.trim())) rows.push(row);
      row = [];
      cell = '';
    } else cell += c;
  }
  if (quoted) throw new Error('CSV 引号未闭合');
  row.push(cell);
  if (row.some((value) => value.trim())) rows.push(row);
  return rows;
}

export const deviceFields: Field[] = [...fieldGroups.flatMap((group) => group.fields), { key: 'assetMaterialId', label: '素材编号' }];
export const diskHeaders = Array.from({ length: 4 }, (_, i) => [`硬盘${i + 1}型号`, `硬盘${i + 1}描述`, `硬盘${i + 1}数量`]).flat();

export function exportCsv(devices: Device[]): string {
  const groups = Math.max(4, ...devices.map((d) => d.disks.length));
  const header = [...deviceFields.map((field) => field.label), ...Array.from({ length: groups }, (_, i) => [`硬盘${i + 1}型号`, `硬盘${i + 1}描述`, `硬盘${i + 1}数量`]).flat()];
  const rows = devices.map((d) => [
    ...deviceFields.map((field) => (field.kind === 'boolean' ? (d[field.key] ? '是' : '否') : String(d[field.key] ?? ''))),
    ...Array.from({ length: groups }, (_, i) => [d.disks[i]?.model || '', d.disks[i]?.description || '', String(d.disks[i]?.count ?? '')]).flat(),
  ]);
  return '\uFEFF' + [header, ...rows].map((row) => row.map((value) => `"${String(value).replace(/"/g, '""')}"`).join(',')).join('\r\n');
}

export function importCsv(text: string, existing: Device[]): Device[] {
  const [header, ...rows] = csvRows(text);
  if (!header || !['客户信息', '序列号', '主机名'].every((label) => header.includes(label))) throw new Error('请使用设备档案 CSV 模板（需包含客户信息、序列号、主机名）');
  if (!rows.length) throw new Error('文件中没有设备数据');
  if (rows.length > 1000) throw new Error('单次最多导入 1000 台设备');
  const result: Device[] = [];
  rows.forEach((row, index) => {
    const record: Record<string, unknown> = { ...blankDevice, id: `CI-${Date.now()}-${index}`, disks: [] };
    deviceFields.forEach((field) => {
      const pos = header.indexOf(field.label);
      if (pos < 0) return;
      const value = (row[pos] || '').trim();
      if (field.kind === 'boolean') {
        if (value && !['是', '否'].includes(value)) throw new Error(`第 ${index + 2} 行：${field.label}只能为是或否`);
        record[field.key] = value === '是';
      } else if (field.kind === 'number') record[field.key] = value ? Number(value) : undefined;
      else record[field.key] = value;
      if (field.options && value && !field.options.includes(value)) throw new Error(`第 ${index + 2} 行：${field.label}选项无效`);
      if (field.kind === 'date' && value && (!/^\d{4}-\d{2}-\d{2}$/.test(value) || !Number.isFinite(Date.parse(value)) || new Date(value).toISOString().slice(0, 10) !== value))
        throw new Error(`第 ${index + 2} 行：${field.label}日期无效`);
      if (field.kind === 'number' && value && (!Number.isInteger(record[field.key]) || Number(record[field.key]) < 0))
        throw new Error(`第 ${index + 2} 行：${field.label}应为非负整数`);
    });
    const disks: Disk[] = [];
    for (let i = 1; i <= 100; i++) {
      const at = header.indexOf(`硬盘${i}型号`);
      if (at < 0) break;
      const model = row[at] || '',
        description = row[header.indexOf(`硬盘${i}描述`)] || '',
        count = row[header.indexOf(`硬盘${i}数量`)] || '';
      if (model || description || count) {
        if (!Number.isInteger(Number(count)) || Number(count) < 1) throw new Error(`第 ${index + 2} 行：硬盘数量应为正整数`);
        disks.push({ model, description, count: Number(count) });
      }
    }
    const device = { ...record, disks } as unknown as Device;
    const error = deviceError(device, [...existing, ...result]);
    if (error) throw new Error(`第 ${index + 2} 行：${error}`);
    device.monitorState = device.monitored ? '等待心跳' : '未接入';
    if (!device.monitored) device.alertEnabled = false;
    result.push(device);
  });
  return result;
}
