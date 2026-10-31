/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import DeviceInventoryTable, { COLUMN_WIDTHS_KEY } from './DeviceInventoryTable';
import type { Item } from '../../types';
import { getDevice } from '@/services/collection';

jest.mock('@/App', () => ({ CommonStateContext: require('react').createContext({ profile: { admin: true }, perms: [] }) }));
jest.mock('@/utils/constant', () => ({ IS_ENT: false, IS_PLUS: false }));
jest.mock('@/services/collection', () => ({ getDevice: jest.fn().mockResolvedValue({ id: 'device-one', name: 'dev01' }), setDeviceState: jest.fn() }));
jest.mock('@/services/targets', () => ({ deleteTargets: jest.fn() }));
jest.mock('@/pages/collection/DeviceEditor', () => ({ __esModule: true, default: ({ device, onClose }) => <div role='dialog' aria-label='existing-device-editor'>{device.id}<button onClick={onClose}>close editor</button></div> }));
jest.mock('@/pages/collection/DeviceDetail', () => ({ __esModule: true, default: ({ id, target, onClose, editable, onEditNative }) => <div role='dialog' aria-label='existing-device-detail'>{id || target.ident}<span>device information and dashboard</span>{editable && <button onClick={onEditNative}>detail edit</button>}<button onClick={onClose}>close detail</button></div> }));
jest.mock('@/pages/targets/TargetMetaDrawer', () => ({ ident, extraActions, onDrawerOpenChange }) => <div role='dialog' aria-label='native-device-detail'>{ident}{extraActions}<button onClick={() => onDrawerOpenChange(false)}>close native detail</button></div>);
jest.mock('@/pages/targets/components/Explorer', () => ({ selectedIdents }) => <div aria-label='native metrics'>{selectedIdents.join(',')}</div>);
jest.mock('./OperationModal', () => () => null);
jest.mock('./Tags', () => ({ data }) => <span>{data.join(', ')}</span>);
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string, args?: { column?: string }) => args?.column ? `${key} ${args.column}` : key }) }));

const row = {
  id: 1, ident: 'windows-ident', note: 'dev01', host_ip: '192.0.2.1', host_tags: ['os=windows'], tags: ['env=test'], group_objs: [{ id: 1, name: '业务组一' }],
  collection: { device_id: 'device-one', kind: 'snmp', health: 'healthy' },
  inventory: { name: 'dev01', device_type: '操作系统', device_model: 'Windows Server 2019', collection_ok: true, template_path: ['操作系统', 'Windows', '通用 SNMP'], method: 'snmp', reachability: 'unknown', reachability_at: 0, current_alerts: { critical: 2, warning: 1, info: 0 } },
} as Item;
const props = { tableProps: { dataSource: [row], pagination: false as const }, selectedRows: [], setSelectedRows: jest.fn(), onChanged: jest.fn(), onTagClick: jest.fn(), editable: true };

beforeEach(() => { localStorage.clear(); jest.clearAllMocks(); });

test('inventory has the requested fields, real alert counts, and a fixed pair of persistent actions', () => {
  const { container } = render(<DeviceInventoryTable {...props} />);
  for (const key of ['name', 'type', 'model', 'ip', 'method', 'reachability', 'alerts', 'groups', 'tags']) expect(screen.getByText(`inventory.${key}`)).toBeTruthy();
  expect(screen.getByText('Windows Server 2019')).toBeTruthy();
  expect(container.querySelector('.device-inventory-method.is-success')?.textContent).toBe('snmp');
  expect(container.querySelector('.device-inventory-method.ant-tag-green')).toBeTruthy();
  expect(screen.queryByText(/Agent \/|操作系统 \/|inventory.critical 0|inventory.warning 0/)).toBeNull();
  expect(screen.getByText('inventory.critical 2')).toBeTruthy();
  expect(screen.getByText('inventory.warning 1')).toBeTruthy();
  expect(screen.getByText('inventory.reachability_unknown')).toBeTruthy();
  expect(screen.queryByText('inventory.reachability_up')).toBeNull();
  expect(container.querySelector('td.device-inventory-actions.ant-table-cell-fix-right')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'inventory.edit dev01' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'inventory.delete dev01' })).toBeTruthy();
  expect(screen.getByText('env=test, os=windows')).toBeTruthy();
});

test('name opens details while the persistent Edit opens the existing editor, and widths persist', async () => {
  render(<DeviceInventoryTable {...props} />);
  fireEvent.click(screen.getByRole('button', { name: 'dev01' }));
  expect(await screen.findByRole('dialog', { name: 'existing-device-detail' })).toBeTruthy();
  expect(screen.queryByRole('dialog', { name: 'existing-device-editor' })).toBeNull();
  expect(getDevice).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'close detail' }));
  fireEvent.click(screen.getByRole('button', { name: 'inventory.edit dev01' }));
  expect(await screen.findByRole('dialog', { name: 'existing-device-editor' })).toBeTruthy();
  expect(getDevice).toHaveBeenCalledTimes(1);
  expect(getDevice).toHaveBeenCalledWith('device-one');
  fireEvent.click(screen.getByRole('button', { name: 'close editor' }));
  // rc-table keeps its header hidden until real layout measurements exist in a browser.
  const handle = screen.getByLabelText('inventory.resize inventory.name');
  fireEvent.keyDown(handle, { key: 'ArrowRight' });
  await waitFor(() => expect(JSON.parse(localStorage.getItem(COLUMN_WIDTHS_KEY) || '{}').name).toBe(226));
});


test('zero alert severities stay blank and unsuccessful collection is black', () => {
  const inactive = { ...row, inventory: { ...row.inventory!, method: 'mysql', collection_ok: false, current_alerts: { critical: 0, warning: 2, info: 0 } } };
  const { container } = render(<DeviceInventoryTable {...props} tableProps={{ dataSource: [inactive], pagination: false }} />);
  expect(screen.queryByText('inventory.critical 0')).toBeNull();
  expect(screen.getByText('inventory.warning 2')).toBeTruthy();
  expect(container.querySelector('.device-inventory-method')?.textContent).toBe('mysql');
  expect(container.querySelector('.device-inventory-method.is-success')).toBeNull();
  expect(container.querySelector('.device-inventory-method.ant-tag')).toBeTruthy();
});

test('native Agent collection uses the same framed success tag', () => {
  const agent = { ...row, collection: undefined, inventory: { ...row.inventory!, method: 'agent', collection_ok: true } };
  const { container } = render(<DeviceInventoryTable {...props} tableProps={{ dataSource: [agent], pagination: false }} />);
  expect(container.querySelector('.device-inventory-method.ant-tag-green')?.textContent).toBe('agent');
});

test('no current events leaves the alert cell empty', () => {
  const clear = { ...row, inventory: { ...row.inventory!, current_alerts: { critical: 0, warning: 0, info: 0 } } };
  render(<DeviceInventoryTable {...props} tableProps={{ dataSource: [clear], pagination: false }} />);
  expect(screen.queryByText(/^inventory\.(critical|warning|info) /)).toBeNull();
  expect(screen.queryByText('inventory.unavailable')).toBeNull();
});


test('read-only lists retain name-to-details without exposing edit actions', async () => {
  render(<DeviceInventoryTable {...props} editable={false} />);
  const name = screen.getByRole('button', { name: 'dev01' });
  expect(name.hasAttribute('disabled')).toBe(false);
  expect(screen.getByRole('button', { name: 'inventory.edit dev01' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(name);
  expect(await screen.findByRole('dialog', { name: 'existing-device-detail' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'detail edit' })).toBeNull();
});

test('native names open the shared details before explicit Edit opens existing native controls', async () => {
  const native = { ...row, ident: 'native-agent', collection: undefined, inventory: { ...row.inventory!, name: 'native-agent', method: 'agent' } };
  render(<DeviceInventoryTable {...props} tableProps={{ dataSource: [native], pagination: false }} />);
  fireEvent.click(screen.getByRole('button', { name: 'native-agent' }));
  expect(await screen.findByRole('dialog', { name: 'existing-device-detail' })).toBeTruthy();
  expect(screen.queryByRole('dialog', { name: 'native-device-detail' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'bind_tag.title' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'detail edit' }));
  expect(await screen.findByRole('dialog', { name: 'native-device-detail' })).toBeTruthy();
  expect(screen.queryByRole('dialog', { name: 'existing-device-detail' })).toBeNull();
  expect(screen.getByRole('button', { name: 'bind_tag.title' })).toBeTruthy();
});
