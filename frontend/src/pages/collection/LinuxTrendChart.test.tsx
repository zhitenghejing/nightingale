/** @jest-environment jsdom */
import React from 'react';
import { render } from '@testing-library/react';
import Timeseries from '@/pages/dashboard/Renderer/Renderer/TimeSeriesNG';
import LinuxTrendChart from './LinuxTrendChart';
jest.mock('@/pages/dashboard/Renderer/Renderer/TimeSeriesNG', () => jest.fn(() => <div data-testid='native-timeseries' />));
jest.mock('@/pages/dashboard/Editor/config', () => ({ defaultCustomValuesMap: { timeseries: { drawStyle: 'lines', lineInterpolation: 'smooth', lineWidth: 2, fillOpacity: 0.01 } } }));
beforeEach(() => jest.clearAllMocks());
test('uses the template renderer and native palette while preserving data and timestamp units', () => {
  render(<LinuxTrendChart unit='%' points={[{ time: 1700000000000, value: 0, series: 'CPU' }, { time: 1700000060000, value: 30, series: 'CPU' }, { time: 1700000000000, value: 70, series: 'Memory' }]} />);
  const props = jest.mocked(Timeseries).mock.calls.slice(-1)[0][0];
  expect(props.colors).toBeUndefined();
  expect(props.series.map((s) => ({ name: s.name, data: s.data }))).toEqual([{ name: 'CPU', data: [[1700000000, 0], [1700000060, 30]] }, { name: 'Memory', data: [[1700000000, 70]] }]);
  expect(props.values.custom).toMatchObject({ drawStyle: 'lines', lineInterpolation: 'smooth', lineWidth: 2, fillOpacity: 0.03, spanNulls: false });
  expect(props.values.options.standardOptions?.unit).toBe('percent');
  expect(props.values.options.legend).toMatchObject({ displayMode: 'list', placement: 'bottom' });
  expect(props.values.options.tooltip).toEqual({ mode: 'all', sort: 'desc' });
  expect(props.isPreview).toBe(true);
});
test('single series and empty series remain distinct and byte formatting uses the native unit', () => {
  const view = render(<LinuxTrendChart unit='bytes' points={[{ time: 1700000000000, value: 1024, series: 'Memory' }]} />);
  let props = jest.mocked(Timeseries).mock.calls.slice(-1)[0][0];
  expect(props.values.options.standardOptions?.unit).toBe('bytesIEC');
  expect(props.values.options.legend?.displayMode).toBe('hidden');
  view.rerender(<LinuxTrendChart unit='bytes' points={[]} />);
  props = jest.mocked(Timeseries).mock.calls.slice(-1)[0][0];
  expect(props.series).toEqual([]);
});
