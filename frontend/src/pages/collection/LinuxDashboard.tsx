import React, { useEffect, useState } from 'react';
import { Alert, Card, Empty, Select, Space, Spin, Table, Tabs, Tag, Typography } from 'antd';
import { Bar } from '@ant-design/plots';
import moment from 'moment';
import request from '@/utils/request';
import { RequestMethod } from '@/store/common';
import { DeviceDetailData, DeviceSubject, getAgentDeviceMetrics } from '@/services/collection';
import LinuxTrendChart from './LinuxTrendChart';
import { errorText } from './errors';
import './LinuxDashboard.less';

const colors = ['#5b8ff9', '#61d9b5', '#f6bd16', '#9270ca', '#ef6d6d'];
const memoryMetrics = ['有效内存使用率（扣除 buffers/cache）', '物理内存占用率（含 buffers/cache）', 'Swap 使用率', '物理内存总量', '物理内存空闲量', '有效内存已用量', '内存 buffers', '内存 cache'];
const groups: Record<string, string[]> = {
  cpu: ['CPU 使用率', '1 分钟平均负载', '5 分钟平均负载', '15 分钟平均负载'],
  memory: memoryMetrics,
  network: ['network_in', 'network_out', '网络接收速率', '网络发送速率', '网络延迟', '丢包率'],
  disk: ['disk', '磁盘使用率', '磁盘总量', '磁盘空闲量', '磁盘已用量'],
  io: ['磁盘读取 IOPS', '磁盘写入 IOPS', '磁盘读取速率', '磁盘写入速率'],
  process: ['进程 CPU 使用率', '进程内存使用量'],
  ports: ['监听端口'],
};
const tabs = [['asset', '资产信息'], ['cpu', 'CPU信息'], ['memory', '内存信息'], ['network', '网络信息'], ['disk', '硬盘'], ['io', '硬盘IO'], ['process', '进程'], ['ports', '监听端口'], ['topology', '拓扑']];
interface Series { metric: Record<string, string>; values: [number, string][]; }
type Metrics = Record<string, Series[]>;
export function supportsLinuxDashboard(data: DeviceDetailData): boolean {
  const linux = (data.device?.profile?.catalog?.category_path || data.target.inventory?.template_path || []).some((part) => /^linux$/i.test(part)) || /^linux/i.test(data.target.os || '');
  // Older Linux packs use different metric names; keep their existing boards by default.
  return linux && (!data.device || data.metrics.some((metric) => metric.name === 'CPU 使用率'));
}
export function linuxPoints(metrics: Metrics, names: string[], disk = '') {
  return names.flatMap((name) => (metrics[name] || []).filter((s) => !disk || s.metric.disk_name === disk).flatMap((s) => {
    const identity = ['disk_name', 'storage_description', 'mountpoint', 'path', 'process', 'pid', 'cpu'].filter((key) => s.metric[key]);
    const labels = (identity.length ? identity : ['index']).map((key) => s.metric[key]).filter(Boolean).join(' / ');
    return (s.values || []).map(([time, value]) => ({ time: Number(time) * 1000, value: Number(value), series: `${name}${labels ? ` · ${labels}` : ''}` }));
  })).filter((p) => Number.isFinite(p.time) && Number.isFinite(p.value));
}
// Join HOST-RESOURCES rows by their SNMP index, never by array position.
export function linuxStorageMetrics(raw: Metrics): Metrics {
  const result: Metrics = { 磁盘使用率: [], 磁盘总量: [], 磁盘空闲量: [], 磁盘已用量: [] };
  for (const size of raw.snmp_storage_size || []) {
    if (!/^\//.test(size.metric.storage_description || '')) continue;
    const match = (name: string) => (raw[name] || []).find((s) => s.metric.index === size.metric.index && s.metric.storage_description === size.metric.storage_description);
    const used = match('snmp_storage_used'); const units = match('snmp_storage_allocation_units');
    if (!used || !units) continue;
    const usedValues = new Map(used.values); const unitValues = new Map(units.values);
    const values: Record<string, [number, string][]> = { 磁盘使用率: [], 磁盘总量: [], 磁盘空闲量: [], 磁盘已用量: [] };
    for (const [time, blocks] of size.values) {
      const total = Number(blocks) * Number(unitValues.get(time)); const occupied = Number(usedValues.get(time)) * Number(unitValues.get(time));
      if (!Number.isFinite(total) || !Number.isFinite(occupied) || total <= 0 || occupied < 0 || occupied > total) continue;
      for (const [name, value] of Object.entries({ 磁盘使用率: 100 * occupied / total, 磁盘总量: total, 磁盘空闲量: total - occupied, 磁盘已用量: occupied })) values[name].push([time, String(value)]);
    }
    for (const name of Object.keys(values)) if (values[name].length) result[name].push({ metric: size.metric, values: values[name] });
  }
  return result;
}
// The full scale remains visible while the pointer alone follows the sample.
export function LinuxGauge({ value, title }: { value: number; title: string }) {
  const percent = Math.round(Math.max(0, Math.min(100, value)));
  const point = (n: number, radius: number) => {
    const angle = (157.5 + n * 2.25) * Math.PI / 180;
    return { x: 180 + Math.cos(angle) * radius, y: 180 + Math.sin(angle) * radius };
  };
  const bands: [number, number, string][] = [[0, 20, '#B8E986'], [20, 40, '#7ED321'], [40, 60, '#00CD00'], [60, 80, '#ffe58f'], [80, 90, '#fa8c16'], [90, 100, '#ff4d4f']];
  const needle = point(percent, 78);
  return <svg className='linux-dashboard-dial' viewBox='0 0 360 270' role='meter' aria-label={title} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent} aria-valuetext={`${percent}%`}>
    {bands.map(([start, end, color]) => {
      const from = point(start, 145); const to = point(end, 145);
      return <path key={start} className='linux-dashboard-dial-band' d={`M ${from.x} ${from.y} A 145 145 0 ${end - start > 180 / 2.25 ? 1 : 0} 1 ${to.x} ${to.y}`} fill='none' stroke={color} strokeWidth={8} />;
    })}
    {Array.from({ length: 26 }, (_, i) => i * 4).map((n) => {
      const from = point(n, 140); const to = point(n, n % 20 === 0 ? 134 : 137);
      return <line key={n} className='linux-dashboard-dial-mark' x1={from.x} y1={from.y} x2={to.x} y2={to.y} stroke='#8c9baa' strokeWidth={0.7} />;
    })}
    {[0, 20, 40, 60, 80, 100].map((n) => {
      const pos = point(n, 124);
      return <text key={n} x={pos.x} y={pos.y} textAnchor='middle' dominantBaseline='middle' className='linux-dashboard-dial-tick'>{n}</text>;
    })}
    <line className='linux-dashboard-dial-needle' x1={180} y1={180} x2={needle.x} y2={needle.y} stroke='#4b4b4b' strokeWidth={2.5} strokeLinecap='round' />
    <circle cx={180} cy={180} r={4} fill='white' stroke='#4b4b4b' strokeWidth={2} />
    <text x={180} y={213} textAnchor='middle' className='linux-dashboard-dial-value'>{percent}%</text>
  </svg>;
}
export default function LinuxDashboard({ data, subject, refresh, assetInfo }: { data: DeviceDetailData; subject: DeviceSubject; refresh: number; assetInfo?: React.ReactNode }) {
  const [tab, setTab] = useState('asset');
  const [metrics, setMetrics] = useState<Metrics>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [disk, setDisk] = useState('');
  const [checked, setChecked] = useState(0);
  const device = data.device;
  const available = data.metrics.map((m) => m.name);
  const wanted = (groups[tab] || []).filter((name) => available.includes(name));
  if (!device) {
    if (tab === 'cpu') wanted.push('cpu');
    if (tab === 'memory') wanted.push('memory');
    if (tab === 'disk') wanted.push('disk');
    if (tab === 'network') wanted.push('network_in', 'network_out');
  }
  const key = device && tab === 'disk' && available.includes('物理内存总量') ? '__storage' : [...new Set(wanted)].join('|');
  useEffect(() => {
    let active = true;
    setMetrics({}); setError(''); setChecked(0); setDisk('');
    const names = key ? key.split('|') : [];
    if (!names.length) { setLoading(false); return; }
    const load = async () => {
      setLoading(true);
      try {
        const result = device ? await request(`/api/n9e/collection/devices/${device.id}/metrics?step_seconds=30&${key === '__storage' ? '' : new URLSearchParams(names.map((name) => ['profile_metric', name])).toString()}`, { method: RequestMethod.Get, silence: true }).then((r) => r.dat) : await getAgentDeviceMetrics(subject);
        if (active) {
          setMetrics(key === '__storage' ? linuxStorageMetrics(result.metrics) : result.metrics); setChecked(Date.now());
          setError(Object.values(result.metric_errors || {}).some(Boolean) ? '部分指标查询失败，请检查数据源后重试。' : '');
        }
      } catch (e) { if (active) { setMetrics({}); setError(errorText(e, '指标查询失败')); } }
      finally { if (active) setLoading(false); }
    };
    load(); const timer = window.setInterval(load, 30000);
    return () => { active = false; window.clearInterval(timer); };
  }, [key, subject.device_id, subject.ident, device?.collection.revision, refresh]);
  const latest = (name: string) => {
    const points = linuxPoints(metrics, [name]).filter((p) => p.time >= Date.now() - Math.max(120, (device?.interval_seconds || 30) * 3) * 1000);
    return points.length === 1 ? points[0].value : points.length ? points.reduce((a, b) => a.time > b.time ? a : b).value : undefined;
  };
  const display = (value?: number, unit = '') => value === undefined ? '—' : `${value.toLocaleString(undefined, { maximumFractionDigits: 2 })}${unit}`;
  const bytes = (value?: number) => value === undefined || !Number.isFinite(value) ? '—' : value >= 1024 ** 3 ? display(value / 1024 ** 3, ' GiB') : display(value / 1024 ** 2, ' MiB');
  const empty = (names: string[]) => <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={(key === '__storage' || names.some((n) => available.includes(n))) ? '当前时间范围暂无样本，请检查采集状态和设备 SNMP 视图。' : '当前模板未配置此项采集，请在模板中心补充采集项并更新设备。'} />;
  const chart = (title: string, names: string[], unit = '', color = colors, selectedDisk = '', bar = false) => {
    const points = linuxPoints(metrics, names, selectedDisk);
    const rows = names.flatMap((name) => (metrics[name] || []).flatMap((s) => { const point = linuxPoints({ [name]: [s] }, [name]).slice(-1)[0]; return point ? [point] : []; })).sort((a, b) => b.value - a.value).slice(0, 10);
    return <Card title={title} size='small' className='linux-dashboard-chart'>
      {points.length ? bar ? <Bar height={280} data={rows} xField='value' yField='series' yAxis={{ label: { formatter: (text) => { const label = text.split(' · ').slice(1).join(' · ') || text; return label.length > 28 ? `…${label.slice(-27)}` : label; } } }} color={color[0]} legend={false} animation={false} /> : <LinuxTrendChart points={points} unit={unit} /> : loading ? <Spin /> : empty(names)}
    </Card>;
  };
  const gauge = (title: string, name: string) => {
    const value = latest(name);
    return <Card title={title} size='small' className='linux-dashboard-gauge'>{value === undefined ? loading ? <Spin /> : empty([name]) : <LinuxGauge value={value} title={title} />}</Card>;
  };
  const cpu = device ? 'CPU 使用率' : 'cpu';
  const memory = device ? '有效内存使用率（扣除 buffers/cache）' : 'memory';
  const ioDisks = [...new Set(Object.values(metrics).flatMap((series) => series.map((s) => s.metric.disk_name).filter(Boolean)))].sort();
  const diskNames = ['disk', '磁盘使用率'];
  const diskRows = diskNames.flatMap((name) => (metrics[name] || []).map((series, index) => ({ key: `${name}:${index}`, name: series.metric.storage_description || series.metric.mountpoint || series.metric.path || series.metric.index || '—', value: linuxPoints({ [name]: [series] }, [name]).slice(-1)[0]?.value, total: Number(metrics['磁盘总量']?.find((s) => s.metric.storage_description === series.metric.storage_description)?.values.slice(-1)[0]?.[1]), free: Number(metrics['磁盘空闲量']?.find((s) => s.metric.storage_description === series.metric.storage_description)?.values.slice(-1)[0]?.[1]), used: Number(metrics['磁盘已用量']?.find((s) => s.metric.storage_description === series.metric.storage_description)?.values.slice(-1)[0]?.[1]) })));
  return <div className='linux-dashboard'>
    <div className='linux-dashboard-heading'><Typography.Title level={4}>Linux 系统仪表盘</Typography.Title><Space><Tag color='purple'>{device ? 'SNMP' : 'Agent'}</Tag><span>最近 1 小时 · 每 30 秒刷新</span><span>{checked ? `更新于 ${moment(checked).format('HH:mm:ss')}` : ''}</span></Space></div>
    {error && <Alert type='error' showIcon message={error} />}
    <Tabs activeKey={tab} onChange={setTab} destroyInactiveTabPane>
      {tabs.map(([id, title]) => <Tabs.TabPane key={id} tab={title} />)}
    </Tabs>
    {tab === 'asset' && <>
      {assetInfo && <div className='linux-dashboard-asset-info mb-3'>{assetInfo}</div>}
    </>}
    {tab === 'cpu' && <>
      <div className='linux-dashboard-gauge-row'>{gauge('CPU 平均使用率', cpu)}{chart('CPU 使用趋势', [cpu], '%')}</div>
      {chart('系统负载趋势', ['1 分钟平均负载', '5 分钟平均负载', '15 分钟平均负载'], '负载')}
    </>}
    {tab === 'memory' && <>
      <div className='linux-dashboard-gauge-row'>{gauge('内存使用率', memory)}{chart('内存使用趋势', [memory, '物理内存占用率（含 buffers/cache）'], '%', ['#78cf26', colors[0]])}</div>
      <div className='linux-dashboard-gauge-row'>{gauge('Swap 使用率', 'Swap 使用率')}{chart('Swap 使用趋势', ['Swap 使用率'], '%', [colors[4]])}</div>
      {chart('内存容量趋势', ['物理内存总量', '物理内存空闲量', '有效内存已用量', '内存 buffers', '内存 cache'], 'bytes')}
    </>}
    {tab === 'network' && <>{chart('延迟 / 丢包趋势', ['网络延迟', '丢包率'], 'ms / %', ['#f6b447', '#a45ee5'])}{chart('网络流量趋势', ['network_in', 'network_out', '网络接收速率', '网络发送速率'], 'bit/s')}</>}
    {tab === 'disk' && <><div className='linux-dashboard-two-col'>{chart('硬盘使用率', diskNames, '%', colors, '', true)}{chart('主机磁盘使用率趋势', diskNames, '%')}</div><Card title='主机分区列表' size='small'><Table rowKey='key' dataSource={diskRows} pagination={false} columns={[{ title: '分区', dataIndex: 'name', width: '40%', ellipsis: true }, { title: '总大小', dataIndex: 'total', render: bytes }, { title: '空闲大小', dataIndex: 'free', render: bytes }, { title: '空闲率', dataIndex: 'value', render: (v) => display(v === undefined ? undefined : 100 - v, '%') }, { title: '使用率', dataIndex: 'value', render: (v) => display(v, '%') }, { title: '已用大小', dataIndex: 'used', render: bytes }]} locale={{ emptyText: empty(diskNames) }} /></Card></>}
    {tab === 'io' && <><Space className='mb-3'>硬盘<Select aria-label='硬盘' value={disk} onChange={setDisk} style={{ minWidth: 180 }} options={[{ value: '', label: '全部硬盘' }, ...ioDisks.map((value) => ({ value, label: value }))]} /></Space>{chart('磁盘 I/O 每秒读写次数', ['磁盘读取 IOPS', '磁盘写入 IOPS'], '次/秒', colors, disk)}{chart('磁盘 I/O 每秒读写大小', ['磁盘读取速率', '磁盘写入速率'], 'bytes/s', colors, disk)}</>}
    {tab === 'process' && <div className='linux-dashboard-two-col'>{chart('CPU 使用情况 TOP10', ['进程 CPU 使用率'], '%', colors, '', true)}{chart('内存使用情况 TOP10', ['进程内存使用量'], 'bytes', colors, '', true)}</div>}
    {tab === 'ports' && <Card title='监听端口' size='small'><Table dataSource={(metrics['监听端口'] || []).map((s, i) => ({ ...s.metric, key: i }))} pagination={false} columns={[{ title: '监听端口', dataIndex: 'port' }, { title: '监听协议', dataIndex: 'protocol' }, { title: '监听 IP', dataIndex: 'address' }, { title: '进程名', dataIndex: 'process' }]} locale={{ emptyText: empty(['监听端口']) }} /></Card>}
    {tab === 'topology' && <Card title='端口下联设备拓扑' size='small'><Empty description='当前设备尚未关联拓扑数据。需要配置端口邻居发现及资产关系后展示。' /></Card>}
  </div>;
}
