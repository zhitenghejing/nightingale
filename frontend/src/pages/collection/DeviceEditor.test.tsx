/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import DeviceEditor from './DeviceEditor';
import { CommonStateContext } from '@/App';
import { Device, getCollectorOptions, saveDevice } from '@/services/collection';
import { CatalogEntry, getCatalog, getCatalogEntry } from '@/pages/builtInComponents/catalog';
jest.mock('@/pages/notificationRules/services', () => ({ getItems: jest.fn().mockResolvedValue([]) }));
jest.mock('@/App', () => ({ CommonStateContext: require('react').createContext({ busiGroups: [] }) }));
jest.mock('@/pages/cmdb/api', () => ({ api: jest.fn().mockResolvedValue({ permissions: { read: false } }) }));
jest.mock('@/services/collection', () => ({
  getCollectorOptions: jest.fn().mockResolvedValue([]),
  getCredentials: jest.fn().mockResolvedValue([]),
  saveDevice: jest.fn(),
  requestKey: () => 'fixture-request-key',
}));
jest.mock('@/pages/builtInComponents/catalog', () => ({
  ...jest.requireActual('@/pages/builtInComponents/catalog'),
  getCatalog: jest.fn().mockResolvedValue({ entries: [], categories: [] }),
  getCatalogEntry: jest.fn(),
}));

test('device creation starts with the approved three-stage wizard and cannot save before review', async () => {
  render(<DeviceEditor onClose={jest.fn()} onSaved={jest.fn()} />);
  await waitFor(() => expect(screen.getByText('设备类型与采集配置')).toBeTruthy());
  expect(screen.getByText('仪表盘与告警通知')).toBeTruthy();
  expect(screen.getByText('确认添加')).toBeTruthy();
  expect(screen.getByRole('button', { name: '下一步' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: '保存' })).toBeNull();
});

test.each<[string, Record<string, string>]>([
  ['  Jane Doe  ', { env: 'test', owner: 'Jane Doe' }],
  ['', { env: 'test' }],
])('editing owner %j saves the expected labels', async (owner, labels) => {
  const device: Device = {
    id: 'device-one', name: 'Device one', address: '192.0.2.1', ident: 'device-one', port: 161,
    protocol: 'snmp_v2c', group_id: 1, collector_id: 'collector-one', credential_id: 'credential-one',
    template_version_id: 'version-one', interval_seconds: 30, timeout_seconds: 3, retries: 1,
    labels: { env: 'test', owner: '张三' }, cmdb_id: '', enabled: true, deleted: false, record_version: 2,
    collection: { device_id: 'device-one', kind: 'snmp', configuration_state: '', health: '', collector_id: 'collector-one',
      revision: '', last_sample_at: 0, checked_at: 0, cpu_percent: null, memory_percent: null, missing_metrics: [], counter_bits: 0 },
  };
  jest.mocked(saveDevice).mockClear().mockResolvedValue(device);
  render(<DeviceEditor device={device} onClose={jest.fn()} onSaved={jest.fn()} />);
  await screen.findByText('历史模板（设备当前快照）', { selector: '.ant-select-selection-item' });
  const field = screen.getByLabelText(/^(owner|责任人)$/i) as HTMLInputElement;
  expect(field.value).toBe('张三');
  fireEvent.change(field, { target: { value: owner } });
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  await screen.findByRole('button', { name: '上一步' });
  fireEvent.click(screen.getByRole('button', { name: '下一步' }));
  fireEvent.click(await screen.findByRole('button', { name: '确认保存' }));
  await waitFor(() => expect(saveDevice).toHaveBeenCalledWith(
    device.id, expect.objectContaining({ labels, record_version: 2 }), 'fixture-request-key',
  ));
  expect(jest.mocked(saveDevice).mock.calls[0][1]).not.toHaveProperty('owner');
});

test('unpublished native templates load their own form and full instructions without enabling a false SNMP save', async () => {
  const entry: CatalogEntry = {
    component_id: 42, ident: 'Redis', name: 'Redis', logo: '', category_id: 'redis-general', disabled: false, record_version: 2,
    draft: { adapter: 'redis', config: '', notification: 'events_only', fields: [{ key: 'port', label: 'Redis 端口', type: 'number', required: true, default: 6379 }, { key: 'password', label: 'Redis 密码', type: 'password', secret: true, required: false }] },
    published: null, published_version_id: '', onboarding: { ready: false, state: 'unpublished', execution: 'collector', issues: ['尚未发布监控包', '此采集方式尚未接通自动下发'] },
  };
  (getCatalog as jest.Mock).mockResolvedValue({ entries: [entry], categories: [
    { id: 'cache', parent_id: '', name: '缓存、文档与搜索', record_version: 1 },
    { id: 'redis', parent_id: 'cache', name: 'Redis', record_version: 1 },
    { id: 'redis-general', parent_id: 'redis', name: '待核验版本', record_version: 1 },
  ] });
  (getCatalogEntry as jest.Mock).mockResolvedValue({ ...entry, draft: { ...entry.draft, pack: { component_id: 42, name: 'Redis', readme: 'Redis INFO 采集说明测试', metrics: [], dashboards: [], alerts: [] } } });
  render(<DeviceEditor onClose={jest.fn()} onSaved={jest.fn()} />);
  await screen.findByText(/全部 1 个模板/);
  for (const [label, option] of [['设备类型', '缓存、文档与搜索'], ['品牌', 'Redis'], ['设备型号', '待核验版本']]) {
    fireEvent.mouseDown(screen.getByRole('combobox', { name: label }).closest('.ant-select')!.querySelector('.ant-select-selector')!);
    fireEvent.click(await screen.findByText(option, { selector: '.ant-select-item-option-content' }));
  }
  await waitFor(() => expect(getCatalogEntry).toHaveBeenCalledWith(42));
  expect((screen.getByLabelText('Redis 端口') as HTMLInputElement).value).toBe('6379');
  expect((screen.getByLabelText('Redis 密码') as HTMLInputElement).disabled).toBe(true);
  expect((screen.getByRole('button', { name: '下一步' }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByLabelText('SNMP 版本')).toBeNull();
  expect(screen.getByText('草稿 · 待接入', { selector: '.ant-tag' })).toBeTruthy();
  expect(screen.queryByText('尚未选择模板')).toBeNull();
  fireEvent.click(screen.getByText('查看原模板的采集说明、仪表盘与告警规则'));
  expect(await screen.findByText('Redis INFO 采集说明测试')).toBeTruthy();
});


test('any registered collector is selectable and survives a business-group change', async () => {
  jest.mocked(getCollectorOptions).mockResolvedValue([
    { id: 'foreign', name: 'Other group Categraf', agent_ident: 'other-agent', datasource_id: 2, enabled: true },
    { id: 'offline', name: 'Disabled Categraf', agent_ident: 'offline-agent', datasource_id: 3, enabled: false },
  ]);
  render(<CommonStateContext.Provider value={{ busiGroups: [{ id: 1, name: 'Group one' }, { id: 2, name: 'Group two' }], profile: { admin: true }, perms: [] } as any}><DeviceEditor onClose={jest.fn()} onSaved={jest.fn()} /></CommonStateContext.Provider>);
  await waitFor(() => expect(getCollectorOptions).toHaveBeenCalled());
  const select = (id: string) => fireEvent.mouseDown(document.getElementById(id)!.closest('.ant-select')!.querySelector('.ant-select-selector')!);
  select('group_id');
  fireEvent.click(await screen.findByText('Group one', { selector: '.ant-select-item-option-content' }));
  select('collector_id');
  const disabled = await screen.findByText(/Disabled Categraf/, { selector: '.ant-select-item-option-content' });
  expect(disabled.closest('.ant-select-item-option')!.className).toContain('ant-select-item-option-disabled');
  fireEvent.click(await screen.findByText('Other group Categraf (other-agent)', { selector: '.ant-select-item-option-content' }));
  await waitFor(() => expect((screen.getByPlaceholderText('随执行采集器的数据源绑定') as HTMLInputElement).value).toBe('2'));
  select('group_id');
  fireEvent.click(await screen.findByText('Group two', { selector: '.ant-select-item-option-content' }));
  expect((screen.getByPlaceholderText('随执行采集器的数据源绑定') as HTMLInputElement).value).toBe('2');
  expect(document.getElementById('collector_id')!.closest('.ant-select')!.textContent).toContain('Other group Categraf');
});
