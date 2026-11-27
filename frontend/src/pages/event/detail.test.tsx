/** @jest-environment jsdom */
import React from 'react';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route } from 'react-router-dom';
import { getAlertEventsById, getHistoryEventsById } from '@/services/warning';
import EventDetail from './detail';

jest.mock('@/services/warning', () => ({ getAlertEventsById: jest.fn(), getHistoryEventsById: jest.fn() }));
jest.mock('@/components/pageLayout', () => ({ __esModule: true, default: ({ children }) => children }));
jest.mock('./DetailNG', () => ({ __esModule: true, default: ({ data }) => <div>{data?.rule_name}</div> }));
jest.mock('./DetailNG/Actions', () => ({ __esModule: true, default: () => null }));
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key, options) => options?.defaultValue || key }) }));

beforeAll(() => {
  window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() }));
});

it.each([
  ['cur', getAlertEventsById, getHistoryEventsById],
  ['his', getHistoryEventsById, getAlertEventsById],
])('reads the matching event table from the %s detail URL', async (source, active, inactive) => {
  jest.clearAllMocks();
  (active as jest.Mock).mockResolvedValue({ dat: { rule_name: '测试告警详情' } });
  render(
    <MemoryRouter initialEntries={[`/alert-${source}-events/38`]}>
      <Route exact path={['/alert-cur-events/:eventId', '/alert-his-events/:eventId']} component={EventDetail} />
    </MemoryRouter>,
  );
  expect(await screen.findByText('测试告警详情')).toBeInTheDocument();
  expect(active).toHaveBeenCalledWith('38');
  expect(inactive).not.toHaveBeenCalled();
});
