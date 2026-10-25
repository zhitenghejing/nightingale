/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import CMDB from './index';

jest.setTimeout(30000);
jest.mock('./api', () => require('./testApi'));
beforeEach(() => {
  jest.requireMock('./api').resetMock();
  jest.clearAllMocks();
});

jest.mock('@/components/pageLayout', () => ({
  __esModule: true,
  default: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

beforeAll(() => {
  window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() }));
  const getComputedStyle = window.getComputedStyle;
  window.getComputedStyle = (element) => getComputedStyle(element);
});

describe('CMDB device modals', () => {
  it('opens addition in a modal and clears cancelled values before reopening', async () => {
    render(<CMDB />);
    await waitFor(() => expect(screen.getByRole('button', { name: /添加设备/ })).toHaveProperty('disabled', false));
    fireEvent.click(screen.getByRole('button', { name: /添加设备/ }));
    let dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('序列号'), { target: { value: 'CANCELLED-001' } });
    fireEvent.click(within(dialog).getByRole('button', { name: /取\s*消/ }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    await waitFor(() => expect(screen.getByRole('button', { name: /添加设备/ })).toHaveProperty('disabled', false));
    fireEvent.click(screen.getByRole('button', { name: /添加设备/ }));
    dialog = screen.getByRole('dialog');
    expect((within(dialog).getByLabelText('序列号') as HTMLInputElement).value).toBe('');
    expect(within(dialog).getByLabelText('客户信息')).toHaveProperty('value', '');
  });

  it('keeps viewing read-only, switches to editing, and reflects saved values in the table', async () => {
    render(<CMDB />);
    await waitFor(() => expect(screen.getAllByRole('button', { name: /查\s*看/ }).length).toBeGreaterThan(0));
    fireEvent.click(screen.getAllByRole('button', { name: /查\s*看/ })[0]);
    let dialog = screen.getByRole('dialog');
    expect(within(dialog).getByLabelText('序列号')).toHaveProperty('disabled', true);
    expect(within(dialog).getByLabelText('序列号')).toHaveProperty('value', 'DEMO-LNV-001');
    fireEvent.click(within(dialog).getByRole('button', { name: '编辑设备' }));
    dialog = screen.getByRole('dialog');
    expect(within(dialog).getByLabelText('序列号')).toHaveProperty('disabled', false);
    expect(within(dialog).getAllByLabelText('数量')[0]).toHaveProperty('value', '2');
    fireEvent.change(within(dialog).getByLabelText('备注'), { target: { value: '弹窗编辑成功' } });
    fireEvent.click(within(dialog).getByRole('button', { name: '保存设备' }));
    await waitFor(
      () => {
        if (screen.queryByRole('dialog')) {
          const errors = Array.from(document.querySelectorAll('.ant-form-item-explain-error, .ant-alert-message')).map((element) => element.textContent);
          throw new Error('Save has not closed the dialog: ' + errors.join('; '));
        }
      },
      { timeout: 5000 },
    );
    expect(screen.getByText('弹窗编辑成功')).toBeTruthy();
  });

  it('does not retain disk groups when editing another device', async () => {
    render(<CMDB />);
    await waitFor(() => expect(screen.getAllByRole('button', { name: /^编\s*辑$/ }).length).toBeGreaterThan(0));
    fireEvent.click(screen.getAllByRole('button', { name: /^编\s*辑$/ })[0]);
    let dialog = screen.getByRole('dialog');
    expect(within(dialog).getByLabelText('硬盘1型号')).toHaveProperty('value', '03LD420');
    fireEvent.click(within(dialog).getByRole('button', { name: /取\s*消/ }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    fireEvent.click(screen.getAllByRole('button', { name: /^编\s*辑$/ })[1]);
    dialog = screen.getByRole('dialog');
    expect(within(dialog).getByLabelText('序列号')).toHaveProperty('value', 'DEMO-LNV-002');
    expect(within(dialog).queryByLabelText('硬盘1型号')).toBeNull();
  });
});

it('intersects keywords, clears hidden selections and restores results on reset', async () => {
  render(<CMDB />);
  await screen.findByRole('cell', { name: 'DEMO-LNV-001' });
  const tbody = document.querySelector('.ant-table-tbody') as HTMLElement;
  fireEvent.click(within(tbody).getAllByRole('checkbox')[0]);
  expect(screen.getByRole('button', { name: /删除所选/ })).toBeEnabled();
  fireEvent.change(screen.getByRole('textbox', { name: '综合搜索设备' }), { target: { value: 'PACS 192.0.2.11' } });
  await screen.findByRole('cell', { name: 'DEMO-LNV-003' });
  expect(screen.queryByRole('cell', { name: 'DEMO-LNV-001' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: /删除所选/ })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: '重置筛选' }));
  await screen.findByRole('cell', { name: 'DEMO-LNV-001' });
});
