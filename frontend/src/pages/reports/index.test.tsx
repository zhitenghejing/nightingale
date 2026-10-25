/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import Reports from './index';
import { listReports, reportBlob, Report } from './api';

jest.mock('@/utils/constant', () => ({ AccessTokenKey: 'access_token' }));
jest.mock('@/components/pageLayout', () => ({ __esModule: true, default: ({ children }: { children: React.ReactNode }) => <main>{children}</main> }));
jest.mock('react-router-dom', () => ({ useLocation: () => ({ pathname: '/reports/inspection' }) }));
jest.mock('./api', () => ({ ...jest.requireActual('./api'), listReports: jest.fn(), reportBlob: jest.fn() }));

const record: Report = {
  id: 'r1',
  kind: 'inspection',
  title: '核心机房巡检',
  engineer: '张工程师',
  company: '运维团队',
  contact: '123',
  reportDate: '2026-10-21',
  summary: '检查交换机和服务器',
  filename: 'October.pdf',
  extension: '.pdf',
  size: 100,
  sha256: 'hash',
  ownerId: 1,
  createdAt: '2026-10-21T10:00:00Z',
  updatedAt: '',
  version: 1,
  previewStatus: 'ready',
};
beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: jest.fn().mockImplementation(() => ({
      matches: false,
      addListener: jest.fn(),
      removeListener: jest.fn(),
      addEventListener: jest.fn(),
      removeEventListener: jest.fn(),
      dispatchEvent: jest.fn(),
    })),
  });
  URL.createObjectURL = jest.fn(() => 'blob:report-preview');
  URL.revokeObjectURL = jest.fn();
});
test('selects reports, shows engineer metadata and loads authenticated PDF preview', async () => {
  jest.mocked(listReports).mockResolvedValue([record, { ...record, id: 'r2', title: '备用机房巡检', engineer: '李工程师' }]);
  jest.mocked(reportBlob).mockResolvedValue(new Blob(['%PDF-1.4'], { type: 'application/pdf' }));
  const view = render(<Reports />);
  await screen.findByTitle('核心机房巡检 在线预览');
  expect(screen.getByText('张工程师')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: /备用机房巡检/ }));
  await screen.findByTitle('备用机房巡检 在线预览');
  expect(screen.getByText('李工程师')).toBeTruthy();
  expect(reportBlob).toHaveBeenLastCalledWith(expect.objectContaining({ id: 'r2' }), true, expect.any(AbortSignal));
  fireEvent.change(screen.getByPlaceholderText('搜索报告、工程师、摘要'), { target: { value: '不存在' } });
  await waitFor(() => expect(screen.getByText('没有匹配的报告')).toBeTruthy());
  view.unmount();
  expect(URL.revokeObjectURL).toHaveBeenCalled();
});
