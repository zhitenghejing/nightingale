/** @jest-environment jsdom */
import React from 'react';
import { render, screen } from '@testing-library/react';
import { collectionRequest, getAgentDeviceMetrics } from '@/services/collection';
import DeviceMetricCharts from './DeviceMetricCharts';

jest.mock('@/services/collection', () => ({ collectionRequest: jest.fn(), getAgentDeviceMetrics: jest.fn() }));
jest.mock('@ant-design/plots', () => ({ Line: ({ data }) => <div aria-label='measured-chart'>{JSON.stringify(data)}</div> }));
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
beforeEach(() => { jest.clearAllMocks(); });

test('legacy SNMP keeps its device metrics source and actual finite samples', async () => {
  (collectionRequest as jest.Mock).mockResolvedValue({ metrics: { cpu: [{ metric: { ident: 'one', cpu: 'total' }, values: [[1800000000, '25.3'], [1800000001, 'NaN']] }] } });
  render(<DeviceMetricCharts subject={{ device_id: 'snmp-one' }} refresh={0} />);
  const chart = await screen.findByLabelText('measured-chart');
  expect(chart.textContent).toContain('25.3');
  expect(chart.textContent).not.toContain('NaN');
  expect(collectionRequest).toHaveBeenCalledWith('devices/snmp-one/metrics');
  expect(getAgentDeviceMetrics).not.toHaveBeenCalled();
});

test('native charts keep disk series distinct and show partial failures without invented zeroes', async () => {
  (getAgentDeviceMetrics as jest.Mock).mockResolvedValue({ metrics: { disk: [
    { metric: { path: '/data' }, values: [[1800000000, '21']] },
    { metric: { path: '/logs' }, values: [[1800000000, '43']] },
  ], cpu: [] }, metric_errors: { cpu: true } });
  render(<DeviceMetricCharts subject={{ ident: 'native-agent' }} refresh={0} />);
  const chart = await screen.findByLabelText('measured-chart');
  expect(chart.textContent).toContain('path=/data');
  expect(chart.textContent).toContain('path=/logs');
  expect(screen.getByText('query_failed')).toBeTruthy();
  expect(getAgentDeviceMetrics).toHaveBeenCalledWith({ ident: 'native-agent' });
  expect(collectionRequest).not.toHaveBeenCalled();
});
