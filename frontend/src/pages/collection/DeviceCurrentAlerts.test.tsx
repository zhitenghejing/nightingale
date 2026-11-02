/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { getDeviceAlerts } from '@/services/collection';
import DeviceCurrentAlerts from './DeviceCurrentAlerts';

jest.mock('@/services/collection', () => ({ getDeviceAlerts: jest.fn() }));
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
beforeEach(() => { jest.clearAllMocks(); });

test('loads and pages real device events only when its tab is active', async () => {
  (getDeviceAlerts as jest.Mock).mockImplementation((_, page) => Promise.resolve({ list: [{ id: page, rule_name: `Real event page ${page}`, severity: 2, trigger_time: 1800000000, trigger_value: '92' }], total: 21 }));
  const props = { subject: { ident: 'native-agent' }, active: false, refresh: 0 };
  const view = render(<DeviceCurrentAlerts {...props} />);
  expect(getDeviceAlerts).not.toHaveBeenCalled();
  view.rerender(<DeviceCurrentAlerts {...props} active />);
  expect(await screen.findByText('Real event page 1')).toBeTruthy();
  expect(getDeviceAlerts).toHaveBeenLastCalledWith({ ident: 'native-agent' }, 1);
  fireEvent.click(screen.getByTitle('2'));
  expect(await screen.findByText('Real event page 2')).toBeTruthy();
  expect(getDeviceAlerts).toHaveBeenLastCalledWith({ ident: 'native-agent' }, 2);
});

test('empty results and failed queries remain distinguishable', async () => {
  (getDeviceAlerts as jest.Mock).mockResolvedValue({ list: [], total: 0 });
  const props = { subject: { device_id: 'snmp-one' }, active: true, refresh: 0 };
  const view = render(<DeviceCurrentAlerts {...props} />);
  await waitFor(() => expect(getDeviceAlerts).toHaveBeenCalledWith({ device_id: 'snmp-one' }, 1));
  expect(await screen.findByText('device_no_current_alerts')).toBeTruthy();
  (getDeviceAlerts as jest.Mock).mockRejectedValue(new Error('Datasource permission denied'));
  view.rerender(<DeviceCurrentAlerts {...props} refresh={1} />);
  expect(await screen.findByText('Datasource permission denied')).toBeTruthy();
  expect(screen.queryByText('device_no_current_alerts')).toBeNull();
});
