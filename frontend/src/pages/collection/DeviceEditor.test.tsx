/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import DeviceEditor from './DeviceEditor';
import { CatalogEntry, getCatalog, getCatalogEntry } from '@/pages/builtInComponents/catalog';
jest.mock('@/pages/notificationRules/services', () => ({ getItems: jest.fn().mockResolvedValue([]) }));
jest.mock('@/App', () => ({ CommonStateContext: require('react').createContext({ busiGroups: [] }) }));
jest.mock('@/pages/cmdb/api', () => ({ api: jest.fn().mockResolvedValue({ permissions: { read: false } }) }));
jest.mock('@/services/collection', () => ({
  getCollectors: jest.fn().mockResolvedValue([]),
  getCredentials: jest.fn().mockResolvedValue([]),
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
  for (const [label, option] of [['设备类型', '缓存、文档与搜索'], ['产品 / 厂商', 'Redis'], ['版本 / 型号', '待核验版本']]) {
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
