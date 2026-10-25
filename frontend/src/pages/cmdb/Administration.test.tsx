/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import Administration from './Administration';

jest.mock('./api', () => ({ api: jest.fn() }));
const request: jest.Mock = jest.requireMock('./api').api;
const access = { admin: false, permissions: { userId: 1, read: true, create: false, edit: false, delete: true, import: false, export: false } };
let rows: { id: string; version: number; hostname: string; serial: string }[];
beforeAll(() => {
  window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() }));
  const original = window.getComputedStyle;
  window.getComputedStyle = (element) => original(element);
});
beforeEach(() => {
  jest.clearAllMocks();
  rows = [1, 2].map((i) => ({ id: `device-${i}`, version: i + 2, hostname: `host-${i}`, serial: `SERIAL-${i}` }));
  request.mockImplementation(async (path, _method, data) => {
    if (path === 'devices?deleted=true') return [...rows];
    if (path === 'purge') {
      const ids = (data as typeof rows).map(({ id }) => id);
      rows = rows.filter(({ id }) => !ids.includes(id));
      return ids.length;
    }
    return [];
  });
});
const open = async () => {
  fireEvent.click(screen.getByRole('button', { name: '回收站' }));
  await screen.findByText('SERIAL-1');
  return screen.getByRole('dialog');
};
it('requires confirmation, sends only the selected id/version and refreshes the recycle bin', async () => {
  const changed = jest.fn(async () => {});
  render(<Administration access={access} onChanged={changed} />);
  let dialog = await open();
  expect(within(dialog).getAllByRole('button', { name: /恢\s*复/ })[0]).toBeDisabled();
  fireEvent.click(within(dialog).getAllByRole('button', { name: '彻底删除' })[0]);
  expect(request).not.toHaveBeenCalledWith('purge', 'POST', expect.anything());
  expect(screen.getByText(/确认彻底删除这 1 台设备/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /取\s*消/ }));
  await waitFor(() => expect(screen.getAllByRole('dialog')).toHaveLength(1));
  dialog = screen.getByRole('dialog');
  fireEvent.click(within(dialog).getAllByRole('button', { name: '彻底删除' })[0]);
  fireEvent.click(screen.getByRole('button', { name: '确认彻底删除' }));
  await waitFor(() => expect(request).toHaveBeenCalledWith('purge', 'POST', [{ id: 'device-1', version: 3 }]));
  await waitFor(() => expect(screen.queryByText('SERIAL-1')).not.toBeInTheDocument());
  expect(screen.getByText('SERIAL-2')).toBeInTheDocument();
  expect(changed).toHaveBeenCalledTimes(1);
});
it('clears the complete loaded snapshot, including devices beyond the current table page', async () => {
  rows = Array.from({ length: 12 }, (_, i) => ({ id: `d-${i}`, version: 4, hostname: `h-${i}`, serial: `SERIAL-${i + 1}` }));
  const targets = rows.map(({ id, version }) => ({ id, version }));
  render(<Administration access={access} onChanged={async () => {}} />);
  const dialog = await open();
  fireEvent.click(within(dialog).getByRole('button', { name: '一键清空' }));
  expect(screen.getByText(/确认彻底删除这 12 台设备/)).toBeInTheDocument();
  expect(request).not.toHaveBeenCalledWith('purge', 'POST', expect.anything());
  // New arrivals after confirmation are not part of the authorized snapshot.
  rows.push({ id: 'new-arrival', version: 9, hostname: 'late', serial: 'LATE' });
  fireEvent.click(screen.getByRole('button', { name: '确认清空' }));
  await waitFor(() => expect(request).toHaveBeenCalledWith('purge', 'POST', targets));
  await screen.findByText('LATE');
  expect(screen.queryByText('SERIAL-1')).not.toBeInTheDocument();
});
it('preserves the data and displays a failed purge so cancellation and refresh remain possible', async () => {
  const changed = jest.fn(async () => {});
  render(<Administration access={access} onChanged={changed} />);
  const dialog = await open();
  fireEvent.click(within(dialog).getByRole('button', { name: '一键清空' }));
  request.mockRejectedValueOnce(new Error('设备已修改，请刷新回收站后重试'));
  fireEvent.click(screen.getByRole('button', { name: '确认清空' }));
  await screen.findByText('设备已修改，请刷新回收站后重试');
  expect(screen.getByText('SERIAL-1')).toBeInTheDocument();
  expect(changed).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: /取\s*消/ }));
  await waitFor(() => expect(screen.getAllByRole('dialog')).toHaveLength(1));
  fireEvent.click(screen.getByRole('button', { name: '刷新回收站' }));
  await waitFor(() => expect(request.mock.calls.filter(([path]) => path === 'devices?deleted=true')).toHaveLength(2));
});
it('requires delete permission independently of edit permission', async () => {
  render(<Administration access={{ ...access, permissions: { ...access.permissions, edit: true, delete: false } }} onChanged={async () => {}} />);
  const dialog = await open();
  expect(within(dialog).getByRole('button', { name: '一键清空' })).toBeDisabled();
  for (const button of within(dialog).getAllByRole('button', { name: '彻底删除' })) expect(button).toBeDisabled();
  expect(within(dialog).getAllByRole('button', { name: /恢\s*复/ })[0]).toBeEnabled();
});
it('disables empty or failed-to-load clearing', async () => {
  request.mockRejectedValueOnce(new Error('回收站加载失败'));
  render(<Administration access={access} onChanged={async () => {}} />);
  fireEvent.click(screen.getByRole('button', { name: '回收站' }));
  await screen.findByText('回收站加载失败');
  expect(screen.getByRole('button', { name: '一键清空' })).toBeDisabled();
  rows = [];
  fireEvent.click(screen.getByRole('button', { name: '刷新回收站' }));
  await screen.findByText('共 0 台设备');
  expect(screen.getByRole('button', { name: '一键清空' })).toBeDisabled();
});
