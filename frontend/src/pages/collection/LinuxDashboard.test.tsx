/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import request from '@/utils/request';
import LinuxDashboard, { supportsLinuxDashboard, linuxStorageMetrics, LinuxGauge } from './LinuxDashboard';
jest.mock('@/utils/request', () => jest.fn());
jest.mock('@ant-design/plots', () => ({ Area: (props) => <div aria-label='area-chart'>{JSON.stringify(props.data)}</div>, Bar: (props) => <div aria-label='bar-chart'>{JSON.stringify(props.data)}</div> }));
jest.mock('./LinuxTrendChart', () => (props) => <div aria-label='native-timeseries'>{JSON.stringify(props.points)}</div>);
const data: any = { target: { ident: 'linux-test', os: '', group_objs: [{ name: 'Linux业务' }] }, collector_name: 'SNMP采集', device: { id: 'device-1', name: 'Linux 主机', address: '192.0.2.1', interval_seconds: 30, labels: {}, collection: { revision: 'rev-1' }, profile: { name: 'SNMP采集', catalog: { category_path: ['主机系统', 'Linux', 'X86'] } } }, metrics: [{ name: '1 分钟平均负载' }, { name: 'CPU 使用率' }, { name: '物理内存总量' }, { name: '磁盘读取 IOPS' }] };
beforeEach(() => { jest.clearAllMocks(); jest.mocked(request).mockResolvedValue({ dat: { metrics: {} } }); });
test('Linux classification uses device taxonomy or actual OS', () => {
  expect(supportsLinuxDashboard(data)).toBe(true);
  expect(supportsLinuxDashboard({ ...data, metrics: [{ name: 'CPU usage' }] })).toBe(false);
  expect(supportsLinuxDashboard({ target: { os: 'Linux 6.8' } } as any)).toBe(true);
  expect(supportsLinuxDashboard({ target: { os: 'Windows' } } as any)).toBe(false);
});
test('storage joins by index and timestamp and excludes memory and invalid samples', () => {
  const series = (index, description, values) => ({ metric: { index, storage_description: description }, values });
  const result = linuxStorageMetrics({ snmp_storage_size: [series('1', '/', [[10, '100'], [20, '0']]), series('2', 'Physical memory', [[10, '100']])], snmp_storage_used: [series('2', 'Physical memory', [[10, '25']]), series('1', '/', [[10, '40'], [20, '20']])], snmp_storage_allocation_units: [series('1', '/', [[10, '4096'], [20, '4096']])] });
  expect(result['磁盘使用率']).toEqual([series('1', '/', [[10, '40']])]);
  expect(result['磁盘空闲量'][0].values).toEqual([[10, '245760']]);
});
test('nine categories query saved metrics and distinguish unconfigured collection', async () => {
  render(<LinuxDashboard data={data} subject={{ device_id: 'device-1' }} refresh={0} />);
  expect(screen.getAllByRole('tab')).toHaveLength(9);
  expect(screen.queryByRole('tab', { name: '历史信息' })).toBeNull();
  expect(request).not.toHaveBeenCalled();
  expect(screen.queryByText('系统负载趋势')).toBeNull();
  fireEvent.click(screen.getByRole('tab', { name: 'CPU信息' }));
  await waitFor(() => expect(jest.mocked(request).mock.calls.some(([url]) => url.includes('profile_metric=CPU'))).toBe(true));
  expect(jest.mocked(request).mock.calls.slice(-1)[0][0]).toContain('/devices/device-1/metrics');
  expect(jest.mocked(request).mock.calls.slice(-1)[0][0]).toContain('step_seconds=30');
  fireEvent.click(screen.getByRole('tab', { name: '进程' }));
  expect(screen.getAllByText(/当前模板未配置此项采集/)).toHaveLength(2);

});
test('query failures are visible and refresh retries successfully', async () => {
  jest.mocked(request).mockRejectedValueOnce(new Error('datasource unavailable'));
  const props = { data, subject: { device_id: 'device-1' }, refresh: 0 };
  const view = render(<LinuxDashboard {...props} />);
  fireEvent.click(screen.getByRole('tab', { name: 'CPU信息' }));
  expect(await screen.findByText('datasource unavailable')).toBeTruthy();
  view.rerender(<LinuxDashboard {...props} refresh={1} />);
  await waitFor(() => expect(screen.queryByText('datasource unavailable')).toBeNull());
});


test.each([[0, 0], [14.06, 14], [70.75, 71], [72.47, 72], [100, 100]])('dial %s shows a rounded value of %s with a complete scale', (value, percent) => {
  const view = render(<LinuxGauge title='CPU' value={value} />);
  const meter = screen.getByRole('meter', { name: 'CPU' });
  expect(meter.getAttribute('aria-valuenow')).toBe(String(percent));
  expect(screen.getByText(`${percent}%`)).toBeTruthy();
  expect([...meter.querySelectorAll('.linux-dashboard-dial-band')].map((el) => el.getAttribute('stroke'))).toEqual(['#B8E986', '#7ED321', '#00CD00', '#ffe58f', '#fa8c16', '#ff4d4f']);
  expect([...meter.querySelectorAll('.linux-dashboard-dial-tick')].map((el) => el.textContent)).toEqual(['0', '20', '40', '60', '80', '100']);
  const needle = meter.querySelector('.linux-dashboard-dial-needle')!;
  // Zero points left/down, 50 points up, 100 points right/down.
  expect(Number(needle.getAttribute('x2')) < 180).toBe(percent < 50);
  view.unmount();
});
test('CPU memory and Swap share rounded gauges and the shortened memory title', async () => {
  const names = ['CPU 使用率', '有效内存使用率（扣除 buffers/cache）', 'Swap 使用率'];
  const metrics = Object.fromEntries(names.map((name, i) => [name, [{ metric: {}, values: [[Date.now() / 1000, String([14, 70.75, 72.47][i])]] }]]));
  jest.mocked(request).mockResolvedValue({ dat: { metrics } });
  const view = render(<LinuxDashboard data={{ ...data, metrics: names.map((name) => ({ name, expression: '', unit: '', note: '' })) }} subject={{ device_id: 'device-1' }} refresh={0} />);
  fireEvent.click(screen.getByRole('tab', { name: 'CPU信息' }));
  expect(await screen.findByText('14%')).toBeTruthy();
  fireEvent.click(screen.getByRole('tab', { name: '内存信息' }));
  expect(await screen.findByText('71%')).toBeTruthy();
  expect(screen.getByText('72%')).toBeTruthy();
  expect(screen.getByText('内存使用率', { selector: '.ant-card-head-title' })).toBeTruthy();
  expect(screen.queryByText('内存使用率（扣除 buffers/cache）', { selector: '.ant-card-head-title' })).toBeNull();
  expect(screen.getAllByRole('meter')).toHaveLength(2);
  jest.mocked(request).mockResolvedValue({ dat: { metrics: { ...metrics, 'Swap 使用率': [{ metric: {}, values: [[Date.now() / 1000, '100']] }] } } });
  view.rerender(<LinuxDashboard data={{ ...data, metrics: names.map((name) => ({ name, expression: '', unit: '', note: '' })) }} subject={{ device_id: 'device-1' }} refresh={1} />);
  expect(await screen.findByText('100%')).toBeTruthy();
  const swap = view.container.querySelectorAll('.linux-dashboard-gauge')[1];
  expect(swap.querySelector('[role="meter"]')!.getAttribute('aria-valuenow')).toBe('100');
});

test('memory information retains memory and Swap charts without CPU charts or queries', async () => {
  const names = ['CPU 使用率', '有效内存使用率（扣除 buffers/cache）', 'Swap 使用率', '物理内存总量'];
  const metrics = Object.fromEntries(names.map((name) => [name, [{ metric: {}, values: [[Date.now() / 1000, '40']] }]]));
  jest.mocked(request).mockResolvedValue({ dat: { metrics } });
  render(<LinuxDashboard data={{ ...data, metrics: names.map((name) => ({ name, expression: '', unit: '', note: '' })) }} subject={{ device_id: 'device-1' }} refresh={0} />);
  fireEvent.click(screen.getByRole('tab', { name: '内存信息' }));
  expect(await screen.findByRole('meter', { name: '内存使用率' })).toBeTruthy();
  expect(screen.getByRole('meter', { name: 'Swap 使用率' })).toBeTruthy();
  for (const title of ['内存使用趋势', 'Swap 使用趋势', '内存容量趋势']) expect(screen.getByText(title)).toBeTruthy();
  expect(screen.queryByText('CPU 平均使用率')).toBeNull();
  expect(screen.queryByText('CPU 使用趋势')).toBeNull();
  const url = jest.mocked(request).mock.calls.slice(-1)[0][0];
  expect(new URL(url, 'http://localhost').searchParams.getAll('profile_metric')).toEqual(names.slice(1));
  fireEvent.click(screen.getByRole('tab', { name: 'CPU信息' }));
  expect(await screen.findByRole('meter', { name: 'CPU 平均使用率' })).toBeTruthy();
  expect(screen.getByText('CPU 使用趋势')).toBeTruthy();
  expect(screen.getByText('系统负载趋势')).toBeTruthy();
  for (const title of ['内存使用率', '内存使用趋势', 'Swap 使用率', 'Swap 使用趋势', '内存容量趋势']) expect(screen.queryByText(title)).toBeNull();
  expect(new URL(jest.mocked(request).mock.calls.slice(-1)[0][0], 'http://localhost').searchParams.getAll('profile_metric')).toEqual(['CPU 使用率']);
});
