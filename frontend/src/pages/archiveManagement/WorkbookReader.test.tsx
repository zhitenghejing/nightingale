/** @jest-environment jsdom */
import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import WorkbookReader from './WorkbookReader';
import { api } from './services';
jest.mock('./services', () => ({ api: jest.fn(), errorText: (e: Error) => e.message }));
const request = jest.mocked(api);
const sheet = { name: '巡检报告', rows: 122, columns: 2, hidden: false, hidden_rows: [3], hidden_columns: [], merges: ['A1:B1'] };
beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: jest.fn(() => ({
      matches: false,
      addListener: jest.fn(),
      removeListener: jest.fn(),
      addEventListener: jest.fn(),
      removeEventListener: jest.fn(),
      dispatchEvent: jest.fn(),
    })),
  });
});
beforeEach(() => {
  jest.clearAllMocks();
  request.mockImplementation(async (path: string) => {
    if (!path.includes('operation=read')) return { sheets: [sheet], rows: [], warnings: [] };
    const offset = Number(new URLSearchParams(path.split('?')[1]).get('offset'));
    return {
      rows: [[{ address: `A${offset + 2}`, row: offset + 2, column: 1, value: '0007', raw: '0007', type: 's' }]],
      warnings: [],
      matched_rows: 121,
      has_more: offset === 0,
      range: 'A1:B122',
    };
  });
});
test('reads the exact saved version and paginates without offering editing', async () => {
  render(<WorkbookReader documentID={7} versionID={23} />);
  await screen.findByText('0007');
  expect(screen.getByText(/Excel 只读预览/)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: '保存' })).not.toBeInTheDocument();
  expect(request).toHaveBeenCalledWith('/documents/7/workbook?version_id=23');
  expect(screen.getByLabelText('单元格范围')).toHaveValue('A1:B122');
  fireEvent.click(screen.getByRole('button', { name: '下一页' }));
  await waitFor(() => expect(request).toHaveBeenCalledWith(expect.stringContaining('offset=1&limit=50')));
});
test('a failed range read is an error rather than an empty workbook', async () => {
  request.mockImplementation(async (path: string) => {
    if (path.includes('operation=read')) throw new Error('Excel 原件校验失败');
    return { sheets: [sheet], rows: [] };
  });
  render(<WorkbookReader documentID={7} versionID={23} />);
  await screen.findByText('Excel 原件校验失败');
  expect(screen.getByRole('button', { name: /重\s*试/ })).toBeInTheDocument();
});
