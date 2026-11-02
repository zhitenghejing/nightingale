/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import ExternalTasks from './ExternalTasks';

const mockRequest = jest.fn();
jest.mock('@/utils/request', () => ({ __esModule: true, default: (...args: unknown[]) => mockRequest(...args) }));
jest.mock('@/components/pageLayout', () => ({ __esModule: true, default: ({ children }) => <div>{children}</div> }));
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
jest.mock('../constants', () => ({ NS: 'notification-channels' }));

beforeAll(() => {
  window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() }));
  const original = window.getComputedStyle;
  window.getComputedStyle = (element) => original(element);
});
beforeEach(() => {
  mockRequest.mockReset();
  const task = { id: 5, destination: 'test-only', state: 'unknown', reason: 'timeout', attempts: 1, version: 7, dispatch_id: 'dispatch', incident_id: 'incident', sequence: 1 };
  mockRequest.mockImplementation(async (path) => ({ dat: path.endsWith('/5') ? { task, attempts: [], operations: [] } : [task] }));
});
it('queries and refreshes without authorizing a retry; manual action needs an explicit reason and observed version', async () => {
  render(<ExternalTasks />);
  await screen.findByText('test-only');
  fireEvent.click(screen.getByRole('button', { name: 'external.refresh' }));
  await waitFor(() => expect(mockRequest.mock.calls.length).toBeGreaterThanOrEqual(2));
  expect(mockRequest.mock.calls.every(([, options]) => options.method === 'Get')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'external.detail' }));
  await screen.findByRole('button', { name: 'external.authorize_resend' });
  fireEvent.click(screen.getByRole('button', { name: 'external.authorize_resend' }));
  expect(mockRequest.mock.calls.every(([, options]) => options.method === 'Get')).toBe(true);
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'TEST ONLY: receiver checked, old worker stopped' } });
  fireEvent.click(screen.getByRole('button', { name: /OK/ }));
  await waitFor(() =>
    expect(mockRequest).toHaveBeenCalledWith('/api/n9e/external-push-tasks/5/actions', {
      method: 'Post',
      data: { action: 'authorize_resend', version: 7, reason: 'TEST ONLY: receiver checked, old worker stopped' },
    }),
  );
});

it('distinguishes manually confirmed receipt and offers no hold or resend action', async () => {
  const task = { id: 5, destination: 'test-only', state: 'confirmed_received', reason: 'manual confirmation', attempts: 1, version: 8 };
  mockRequest.mockImplementation(async (path) => ({ dat: path.endsWith('/5') ? { task, attempts: [], operations: [] } : [task] }));
  render(<ExternalTasks />);
  await screen.findByText('test-only');
  fireEvent.click(screen.getByRole('button', { name: 'external.detail' }));
  await screen.findByText(/ID: 5/);
  expect(screen.queryByRole('button', { name: 'external.authorize_resend' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'external.hold' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'external.confirm_received' })).toBeNull();
  expect(screen.getAllByText(/external.states.confirmed_received/).length).toBeGreaterThan(0);
});

it('cancels only an unattempted task with an explicit reason; cancellation is not receipt', async () => {
  const task = { id: 5, destination: 'test-only', state: 'pending', reason: 'awaiting worker', attempts: 0, version: 9 };
  mockRequest.mockImplementation(async (path) => ({ dat: path.endsWith('/5') ? { task, attempts: [], operations: [] } : [task] }));
  render(<ExternalTasks />);
  await screen.findByText('test-only');
  fireEvent.click(screen.getByRole('button', { name: 'external.detail' }));
  fireEvent.click(await screen.findByRole('button', { name: 'external.cancel_unsent' }));
  expect(mockRequest.mock.calls.every(([, options]) => options.method === 'Get')).toBe(true);
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'TEST ONLY: explicitly do not deliver this reminder' } });
  fireEvent.click(screen.getByRole('button', { name: /OK/ }));
  await waitFor(() =>
    expect(mockRequest).toHaveBeenCalledWith('/api/n9e/external-push-tasks/5/actions', {
      method: 'Post',
      data: { action: 'cancel_unsent', version: 9, reason: 'TEST ONLY: explicitly do not deliver this reminder' },
    }),
  );
});

it('keeps an empty manual reason in the modal without an unhandled rejection or write', async () => {
  render(<ExternalTasks />);
  await screen.findByText('test-only');
  fireEvent.click(screen.getByRole('button', { name: 'external.detail' }));
  fireEvent.click(await screen.findByRole('button', { name: 'external.authorize_resend' }));
  fireEvent.click(screen.getByRole('button', { name: /OK/ }));
  await screen.findByText(/required/);
  expect(mockRequest.mock.calls.every(([, options]) => options.method === 'Get')).toBe(true);
  expect(screen.getByRole('dialog')).toBeTruthy();
});

it('shows the saved redacted JSON and local pairing explanation using read-only queries', async () => {
  const task = { id: 5, destination: 'test-only', state: 'accepted', attempts: 1, version: 2, incident_id: 'TEST_INCIDENT', sequence: 2 };
  mockRequest.mockImplementation(async (path) => ({
    dat: path.endsWith('/5')
      ? {
          task,
          attempts: [],
          operations: [],
          payload_preview: {
            alarmType: 'ok',
            companyId: 'TEST_COMPANY',
            deviceName: 'TEST_NODE_192.0.2.8:1443',
            problemTime: '2026-01-02 01:02:03',
            recoveryTime: '2026-01-02 01:14:05',
            sign: '[REDACTED]',
          },
        }
      : [task],
  }));
  render(<ExternalTasks />);
  await screen.findByText('test-only');
  fireEvent.click(screen.getByRole('button', { name: 'external.detail' }));
  await screen.findByText('external.payload_title');
  expect(screen.getByText(/TEST_NODE_192.0.2.8:1443/)).toBeTruthy();
  expect(screen.getByText('external.pairing_notice')).toBeTruthy();
  expect(mockRequest.mock.calls.every(([, options]) => options.method === 'Get')).toBe(true);
});
