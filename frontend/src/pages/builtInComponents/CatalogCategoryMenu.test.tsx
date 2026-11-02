/** @jest-environment jsdom */
import React from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import Menu from './CatalogCategoryMenu';
import { Catalog, saveCategory, moveCategory, moveTemplate } from './catalog';
import { TEMPLATE_DRAG_TYPE } from './catalogDrag';
jest.mock('@/services/collection', () => ({ collectionRequest: jest.fn() }));
jest.mock('./catalog', () => ({ ...jest.requireActual('./catalog'), saveCategory: jest.fn(), moveCategory: jest.fn(), moveTemplate: jest.fn() }));
const catalog: Catalog = {
  categories: [
    { id: 'os', parent_id: '', name: '操作系统', record_version: 1 },
    { id: 'ms', parent_id: 'os', name: '微软', record_version: 1 },
    { id: 'windows', parent_id: 'ms', name: 'Windows Server 2019', record_version: 2 },
  ],
  entries: [{ component_id: 1, name: 'Windows SNMP', category_id: 'elsewhere', record_version: 5 } as any],
};
const props = { catalog, selected: '', onSelect: jest.fn(), onChanged: jest.fn().mockResolvedValue(undefined), canEdit: true, canDelete: true };
beforeEach(() => {
  jest.clearAllMocks();
  (saveCategory as jest.Mock).mockResolvedValue(undefined);
  (moveTemplate as jest.Mock).mockResolvedValue(undefined);
});
test.each([
  ['添加类型', '类型名称', '服务器', ''],
  ['添加品牌 操作系统', '品牌名称', 'Red Hat', 'os'],
  ['添加型号 微软', '型号名称', 'Windows Server 2022', 'ms'],
  ['添加同级型号 Windows Server 2019', '型号名称', 'Windows Server 2025', 'ms'],
])('the contextual plus %s creates at the correct level', async (button, label, name, parent_id) => {
  render(<Menu {...props} />);
  fireEvent.click(screen.getByRole('button', { name: button }));
  fireEvent.change(screen.getByLabelText(label), { target: { value: name } });
  fireEvent.click(screen.getByRole('button', { name: /^保\s*存$/ }));
  await waitFor(() => expect(saveCategory).toHaveBeenCalledWith(expect.objectContaining({ name, parent_id })));
  await waitFor(() => expect(props.onChanged).toHaveBeenCalled());
});
test('template drop persists only its location and version, and type/brand are not valid targets', async () => {
  const view = render(<Menu {...props} />);
  const transfer = { types: [TEMPLATE_DRAG_TYPE], getData: () => JSON.stringify({ component_id: 1, record_version: 5 }) };
  fireEvent.drop(view.container.querySelector('[data-category-id="ms"]')!, { dataTransfer: transfer });
  expect(moveTemplate).not.toHaveBeenCalled();
  fireEvent.drop(view.container.querySelector('[data-category-id="windows"]')!, { dataTransfer: transfer });
  await waitFor(() => expect(moveTemplate).toHaveBeenCalledWith(expect.objectContaining({ component_id: 1, record_version: 5 }), 'windows'));
});
test('read-only menus retain selection without add/edit/delete or draggable nodes', () => {
  const view = render(<Menu {...props} canEdit={false} canDelete={false} />);
  expect(screen.queryByRole('button', { name: '添加类型' })).toBeNull();
  expect(screen.queryByRole('button', { name: '编辑目录 微软' })).toBeNull();
  expect(screen.queryByRole('button', { name: '删除目录 微软' })).toBeNull();
  expect(view.container.querySelector('[draggable="true"]')).toBeNull();
  fireEvent.click(screen.getByText('Windows Server 2019'));
  expect(props.onSelect).toHaveBeenCalledWith('windows');
});

test('pending directory save retains native dragend cleanup without the window fallback throwing', async () => {
  let finish: () => void = () => {};
  (moveCategory as jest.Mock).mockReturnValue(
    new Promise<void>((resolve) => {
      finish = resolve;
    }),
  );
  const errors: string[] = [];
  const onError = (event: ErrorEvent) => {
    errors.push(event.error?.message || event.message);
    event.preventDefault();
  };
  window.addEventListener('error', onError);
  const view = render(<Menu {...props} />);
  const source = view.container.querySelector('[data-category-id="windows"]')!.closest('.ant-tree-treenode')!;
  const target = view.container.querySelector('[data-category-id="ms"]')!.closest('.ant-tree-treenode')!;
  const dataTransfer = { types: ['text/plain'], setData: jest.fn() };
  try {
    fireEvent.dragStart(source, { dataTransfer, clientX: 40, clientY: 10 });
    fireEvent.dragEnter(target, { dataTransfer, clientX: 90, clientY: 20 });
    fireEvent.dragOver(target, { dataTransfer, clientX: 90, clientY: 20 });
    fireEvent.drop(target, { dataTransfer });
    expect(moveCategory).toHaveBeenCalled();
    fireEvent.dragEnd(source, { dataTransfer });
    expect(errors).toEqual([]);
    // A second gesture while the save is pending must clean up without saving again.
    fireEvent.dragStart(source, { dataTransfer, clientX: 40, clientY: 10 });
    fireEvent.dragEnter(target, { dataTransfer, clientX: 90, clientY: 20 });
    fireEvent.dragOver(target, { dataTransfer, clientX: 90, clientY: 20 });
    fireEvent.drop(target, { dataTransfer });
    fireEvent.dragEnd(source, { dataTransfer });
    expect(moveCategory).toHaveBeenCalledTimes(1);
    expect(errors).toEqual([]);
  } finally {
    await act(async () => {
      finish();
    });
    window.removeEventListener('error', onError);
  }
});
