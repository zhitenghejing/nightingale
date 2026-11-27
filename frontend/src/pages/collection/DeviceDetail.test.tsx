/** @jest-environment jsdom */
import React from 'react';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { CommonStateContext } from '@/App';
import { getDeviceDetail } from '@/services/collection';
import DeviceDetail from './DeviceDetail';
import request from '@/utils/request';

jest.mock('@/utils/request', () => ({ __esModule: true, default: jest.fn() }));
jest.mock('@/App', () => ({ CommonStateContext: require('react').createContext({ profile: { admin: true }, perms: [] }) }));
jest.mock('@/services/collection', () => ({ getDeviceDetail: jest.fn(), setDeviceState: jest.fn() }));
jest.mock('./LinuxTrendChart', () => () => null);
jest.mock('@ant-design/plots', () => ({ Area: () => null, Bar: () => null }));
jest.mock('./DeviceEditor', () => ({ __esModule: true, default: ({ device, onClose }) => <div role='dialog' aria-label='existing-device-editor'>{device.id}<button onClick={onClose}>close editor</button></div> }));
jest.mock('./Health', () => ({ Health: ({ state }) => <span>{state}</span> }));
jest.mock('./DeviceMetricCharts', () => ({ __esModule: true, default: ({ subject }) => <div aria-label='real-device-charts'>{subject.device_id || subject.ident}</div> }));
jest.mock('./DeviceCurrentAlerts', () => ({ __esModule: true, default: ({ subject }) => <div aria-label='device-current-events'>{subject.device_id || subject.ident}</div>, DeviceSeverity: ({ severity }) => <span>{severity}</span> }));
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
const props = { id: 'device-one', onClose: jest.fn(), onChanged: jest.fn() };
const data = {
  target: { ident: 'windows-ident', host_ip: '192.0.2.1', inventory: { device_type: '操作系统', device_model: 'Windows Server 2019', method: 'snmp', name: 'dev01', collection_ok: true, reachability: 'up' } },
  device: { id: 'device-one', name: 'dev01', ident: 'windows-ident', address: '192.0.2.1', port: 161, enabled: true, collection: { health: 'healthy', configuration_state: 'applied', checked_at: Date.now() / 1000 }, profile: { name: 'Windows SNMP', boards: [{ id: 42, name: 'Device dashboard' }], metrics: [], rules: [] } },
  collector_name: 'Collector One', metrics: [{ name: 'CPU', expression: 'real_metric{ident="windows-ident"}', unit: 'percent', note: 'CPU utilization' }], rules: [{ id: 8, name: 'Bound CPU rule', source: 'template', disabled: 0, severity: 2 }],
};
beforeEach(() => { jest.clearAllMocks(); (getDeviceDetail as jest.Mock).mockResolvedValue(data); jest.mocked(request).mockResolvedValue({ dat: { metrics: {} } }); });

test('details show device information and its existing dashboard before editing', async () => {
  render(<DeviceDetail {...props} />);
  expect(await screen.findByText('windows-ident')).toBeTruthy();
  expect(screen.getByText('192.0.2.1:161')).toBeTruthy();
  expect(screen.getByText('操作系统')).toBeTruthy();
  expect(screen.getByText('Windows Server 2019')).toBeTruthy();
  expect(screen.getByRole('button', { name: '环境关联' })).toBeTruthy();
  expect(screen.getAllByRole('tab').map((el) => el.textContent)).toEqual(['profile_dashboard', 'device_current_alerts', 'profile_metrics', 'profile_alerts']);
  expect((await screen.findByTitle('Device dashboard')).getAttribute('src')).toBe('/dashboards/42?viewMode=fullscreen&__readonly=true&__show_header=true&__refresh=10');
  fireEvent.click(screen.getByRole('tab', { name: 'device_current_alerts' }));
  expect(screen.getByLabelText('device-current-events').textContent).toBe('device-one');
  fireEvent.click(screen.getByRole('tab', { name: 'profile_metrics' }));
  expect(screen.getByText('CPU utilization')).toBeTruthy();
  fireEvent.click(screen.getByRole('tab', { name: 'profile_alerts' }));
  expect(screen.getByRole('link', { name: 'Bound CPU rule' }).getAttribute('href')).toBe('/alert-rules/edit/8');
  expect(screen.queryByRole('dialog', { name: 'existing-device-editor' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'edit' }));
  expect(screen.getByRole('dialog', { name: 'existing-device-editor' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'close editor' }));
  expect(screen.queryByRole('dialog', { name: 'existing-device-editor' })).toBeNull();
  expect(screen.getByText('windows-ident')).toBeTruthy();
});

test('refresh reloads the embedded dashboard while background status polling preserves it', async () => {
  jest.useFakeTimers();
  try {
    render(<DeviceDetail {...props} />);
    await act(async () => {});
    const initial = screen.getByTitle('Device dashboard');
    await act(async () => { jest.advanceTimersByTime(10000); });
    expect(getDeviceDetail).toHaveBeenCalledTimes(2);
    expect(screen.getByTitle('Device dashboard')).toBe(initial);
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^refresh$/ })); });
    expect(screen.getByTitle('Device dashboard')).not.toBe(initial);
    expect(getDeviceDetail).toHaveBeenCalledTimes(3);
  } finally {
    jest.useRealTimers();
  }
});

test('a new collection revision reloads a retained dashboard so its query filters are fetched again', async () => {
  jest.useFakeTimers();
  try {
    render(<DeviceDetail {...props} />);
    await act(async () => {});
    const initial = screen.getByTitle('Device dashboard');
    (getDeviceDetail as jest.Mock).mockResolvedValue({ ...data, device: { ...data.device, collection: { ...data.device.collection, revision: 'new-revision' } } });
    await act(async () => { jest.advanceTimersByTime(10000); });
    expect(screen.getByTitle('Device dashboard')).not.toBe(initial);
  } finally {
    jest.useRealTimers();
  }
});

test('read-only users can view information and dashboards without mutation buttons', async () => {
  render(<CommonStateContext.Provider value={{ profile: { admin: false }, perms: ['/collection/read'] } as any}><DeviceDetail {...props} /></CommonStateContext.Provider>);
  expect(await screen.findByText('windows-ident')).toBeTruthy();
  for (const name of ['edit', 'disable', 'delete']) expect(screen.queryByRole('button', { name })).toBeNull();
});

test('an explicitly read-only list also hides mutations from an administrator', async () => {
  render(<DeviceDetail {...props} editable={false} />);
  expect(await screen.findByText('windows-ident')).toBeTruthy();
  for (const name of ['edit', 'disable', 'delete']) expect(screen.queryByRole('button', { name })).toBeNull();
});

test('legacy SNMP uses the same tabs with real charts and explicit empty rules', async () => {
  (getDeviceDetail as jest.Mock).mockResolvedValue({ ...data, device: { ...data.device, profile: undefined }, rules: [] });
  render(<DeviceDetail {...props} />);
  expect(await screen.findByLabelText('real-device-charts')).toBeTruthy();
  expect(screen.getAllByRole('tab')).toHaveLength(4);
  fireEvent.click(screen.getByRole('tab', { name: 'profile_alerts' }));
  expect(screen.getByText('device_no_rules')).toBeTruthy();
});

test('native Agent has the same details and tabs and edits only after explicit action', async () => {
  (getDeviceDetail as jest.Mock).mockResolvedValue({ ...data, device: undefined, target: { ...data.target, ident: 'native-agent', inventory: { ...data.target.inventory, method: 'agent', device_model: 'Red Hat 9' } } });
  const editNative = jest.fn();
  render(<DeviceDetail target={{ ident: 'native-agent' } as any} onClose={props.onClose} onChanged={props.onChanged} onEditNative={editNative} />);
  expect(await screen.findByText('Red Hat 9')).toBeTruthy();
  expect(getDeviceDetail).toHaveBeenCalledWith({ ident: 'native-agent' });
  expect(screen.getAllByRole('tab')).toHaveLength(4);
  expect(screen.getByLabelText('real-device-charts').textContent).toBe('native-agent');
  expect(editNative).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'edit' }));
  expect(editNative).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole('button', { name: 'delete' })).toBeNull();
});


const mergedLinuxData = {
  ...data,
  target: { ...data.target, os: 'Linux', note: 'Linux system', cpu_num: 8, group_objs: [{ id: 3, name: 'zc采集' }], inventory: { ...data.target.inventory, device_model: 'X86' } },
  device: { ...data.device, interval_seconds: 60, labels: { env: 'test' }, profile: { ...data.device.profile, name: 'SNMP采集', catalog: { category_path: ['主机系统', 'Linux', 'X86'], version: 2 } } },
  metrics: ['CPU 使用率', 'CPU 核数', '物理内存总量', '设备运行时间', '1 分钟平均负载'].map((name) => ({ name, expression: '', unit: '', note: '' })),
};
test('Linux actions and merged information live only inside the asset category', async () => {
  (getDeviceDetail as jest.Mock).mockResolvedValue(mergedLinuxData);
  const values = { 'CPU 核数': 8, '物理内存总量': 15.49 * 1024 ** 3, '设备运行时间': 1740367.85 };
  jest.mocked(request).mockResolvedValue({ dat: { metrics: Object.fromEntries(Object.entries(values).map(([name, value]) => [name, [{ metric: {}, values: [[Date.now() / 1000, String(value)]] }]])) } });
  render(<DeviceDetail {...props} />);
  const summary = (await screen.findByText('设备名称')).closest('.device-detail-summary');
  expect(summary!.className).toContain('ant-descriptions-bordered');
  expect(within(summary as HTMLElement).getByText('稳定标识')).toBeTruthy();
  expect(within(summary as HTMLElement).getByText('SNMP', { selector: '.ant-tag' })).toBeTruthy();
  expect(await within(summary as HTMLElement).findByText((_, el) => el?.className === 'ant-descriptions-item-content' && el.textContent === 'CPU 8 核 · 内存 16 GB')).toBeTruthy();
  expect(within(summary as HTMLElement).getByText('20 天')).toBeTruthy();
  expect(within(summary as HTMLElement).getByText('16 GB').getAttribute('title')).toContain('采集容量：15.49 GiB');
  expect(within(summary as HTMLElement).getByText('zc采集')).toBeTruthy();
  expect(within(summary as HTMLElement).getByText('env=test')).toBeTruthy();
  expect(within(summary as HTMLElement).getAllByText('Collector One')).toHaveLength(1);
  expect(screen.queryByText('资产描述')).toBeNull();
  expect(summary!.closest('.linux-dashboard-asset-info')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'edit' }).closest('.linux-dashboard-asset-info')).toBeTruthy();
  expect(screen.getByRole('button', { name: '环境关联' }).closest('.linux-dashboard-asset-info')).toBeTruthy();
  fireEvent.click(screen.getByRole('tab', { name: 'CPU信息' }));
  expect(document.querySelector('.device-detail-summary')).toBeNull();
  expect(screen.queryByRole('button', { name: 'edit' })).toBeNull();
  expect(screen.queryByRole('button', { name: '环境关联' })).toBeNull();
  fireEvent.click(screen.getByRole('tab', { name: '资产信息' }));
  expect(await screen.findByText((_, el) => el?.className === 'ant-descriptions-item-content' && el.textContent === 'CPU 8 核 · 内存 16 GB')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'edit' }));
  expect(screen.getByRole('dialog', { name: 'existing-device-editor' })).toBeTruthy();
});
test('failed merged asset queries show an error rather than a stale runtime or memory value', async () => {
  (getDeviceDetail as jest.Mock).mockResolvedValue(mergedLinuxData);
  jest.mocked(request).mockImplementation((url): Promise<any> => new URL(String(url), 'http://test').searchParams.getAll('profile_metric').includes('CPU 核数') ? Promise.reject(new Error('Asset datasource unavailable')) : Promise.resolve({ dat: { metrics: {} } }));
  render(<DeviceDetail {...props} />);
  expect(await screen.findByText('Asset datasource unavailable')).toBeTruthy();
  expect(screen.getByText((_, el) => el?.className === 'ant-descriptions-item-content' && el.textContent === 'CPU 8 核 · 内存 —')).toBeTruthy();
});
