/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { CommonStateContext } from '@/App';
import { getDeviceDetail } from '@/services/collection';
import DeviceDetail from './DeviceDetail';

jest.mock('@/App', () => ({ CommonStateContext: require('react').createContext({ profile: { admin: true }, perms: [] }) }));
jest.mock('@/services/collection', () => ({ getDeviceDetail: jest.fn(), setDeviceState: jest.fn() }));
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
beforeEach(() => { jest.clearAllMocks(); (getDeviceDetail as jest.Mock).mockResolvedValue(data); });

test('details show device information and its existing dashboard before editing', async () => {
  render(<DeviceDetail {...props} />);
  expect(await screen.findByText('windows-ident')).toBeTruthy();
  expect(screen.getByText('192.0.2.1:161')).toBeTruthy();
  expect(screen.getByText('操作系统')).toBeTruthy();
  expect(screen.getByText('Windows Server 2019')).toBeTruthy();
  expect(screen.getAllByRole('tab').map((el) => el.textContent)).toEqual(['profile_dashboard', 'device_current_alerts', 'profile_metrics', 'profile_alerts']);
  expect((await screen.findByTitle('Device dashboard')).getAttribute('src')).toBe('/dashboards/42?viewMode=fullscreen&__readonly=true&__show_header=true');
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
