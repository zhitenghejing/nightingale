/** @jest-environment jsdom */
import React from 'react';
import { createEvent, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import Racks, { placement } from './Racks';
import { blankDevice } from './model';
import { api, catalog, saveAsset, resetMock } from './testApi';
jest.mock('./api', () => require('./testApi'));
jest.mock('@/components/pageLayout', () => ({ __esModule: true, default: ({ children }: any) => <div>{children}</div> }));
jest.mock('@/pages/rackViews/RackViewport', () => ({ __esModule: true, default: ({ children }: any) => <div>{children}</div> }));
beforeAll(() => {
  window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() }));
  const original = window.getComputedStyle;
  window.getComputedStyle = (e) => original(e);
});
beforeEach(() => {
  resetMock();
  jest.clearAllMocks();
});
it('preserves height and checks cabinet bounds and occupancy', () => {
  const d = { ...blankDevice, id: 'x', uStart: 9, uEnd: 10 };
  expect(placement(d, catalog.racks[0], 42, [])).toBeTruthy();
  expect(placement(d, catalog.racks[1], 45, [])).toBeUndefined();
  expect(placement(d, catalog.racks[0], 15, [{ ...blankDevice, id: 'other', rackId: 'rack', uStart: 16, uEnd: 18 }])).toBeTruthy();
});
it('drags a device and sends the same asset ID/version with its new U range', async () => {
  render(<Racks />);
  const button = await screen.findByTitle(/FORUM01 · Lenovo/);
  const transfer = { setData: jest.fn(), effectAllowed: '', dropEffect: '' };
  fireEvent.dragStart(button, { dataTransfer: transfer });
  const chassis = document.querySelector('.rack-chassis')!;
  Object.defineProperty(chassis, 'getBoundingClientRect', { value: () => ({ top: 0, height: 420, bottom: 420, left: 0, right: 500, width: 500 }) });
  // Empty 1–2U is used because the example ledger already occupies 9–20U.
  for (const type of ['dragOver', 'drop'] as const) {
    const event = createEvent[type](chassis, { dataTransfer: transfer });
    Object.defineProperty(event, 'clientY', { value: 415 });
    fireEvent(chassis, event);
  }
  await waitFor(() => expect(saveAsset).toHaveBeenCalled());
  expect(saveAsset).toHaveBeenCalledWith(expect.objectContaining({ id: 'CI-001', version: 1, rackId: 'rack', uStart: 1, uEnd: 2 }), false);
});

it('moves the same asset across cabinets and keeps its two-unit height', async () => {
  render(<Racks />);
  await screen.findByTitle(/FORUM01 · Lenovo/);
  fireEvent.mouseDown(screen.getAllByRole('combobox')[0]);
  fireEvent.click(await screen.findByText('北院7号楼11F / 6号柜'));
  const button = screen.getByTitle(/FORUM01 · Lenovo/);
  const transfer = { setData: jest.fn(), effectAllowed: '', dropEffect: '' };
  fireEvent.dragStart(button, { dataTransfer: transfer });
  const chassis = document.querySelectorAll('.rack-chassis')[1];
  Object.defineProperty(chassis, 'getBoundingClientRect', { value: () => ({ top: 0, height: 480, bottom: 480, left: 0, right: 500, width: 500 }) });
  const drop = createEvent.drop(chassis, { dataTransfer: transfer });
  Object.defineProperty(drop, 'clientY', { value: 475 });
  fireEvent(chassis, drop);
  await waitFor(() => expect(saveAsset).toHaveBeenCalledWith(expect.objectContaining({ id: 'CI-001', version: 1, rackId: 'rack2', rack: '6号柜', uStart: 1, uEnd: 2 }), false));
});

it('switches to the real rear material without changing placement or writing the asset', async () => {
  const { setMockDevices } = jest.requireMock('./api');
  setMockDevices([
    {
      ...blankDevice,
      id: 'real-hpe',
      version: 1,
      customer: '医院',
      hostname: 'HPE01',
      serial: 'HPE001',
      vendor: 'HPE',
      model: 'ProLiant DL380 Gen10',
      assetMaterialId: 'hpe-dl380-gen10',
      rackId: 'rack',
      room: '北院7号楼11F',
      rack: '5号柜',
      uStart: 9,
      uEnd: 10,
    },
  ]);
  render(<Racks />);
  const button = await screen.findByTitle(/HPE01 · HPE/);
  const initialRow = button.style.gridRow;
  expect(screen.getByAltText('HPE ProLiant DL380 Gen10 正面产品素材')).toHaveProperty('dataset.view', 'front');
  fireEvent.click(screen.getByRole('radio', { name: '背视图' }));
  expect(screen.getByAltText('HPE ProLiant DL380 Gen10 背面产品素材').getAttribute('data-material-id')).toBe('hpe-dl380-gen10');
  expect(button.style.gridRow).toBe(initialRow);
  expect(saveAsset).not.toHaveBeenCalled();
});

it('starts with collapsed rooms and expands matching racks when searching', async () => {
  render(<Racks />);
  await screen.findByTitle(/FORUM01 · Lenovo/);
  const room = document.querySelector('details.rack-room')!;
  expect(room).not.toHaveAttribute('open');
  fireEvent.change(screen.getByPlaceholderText('搜索机房 / 机柜'), { target: { value: '6号柜' } });
  expect(room).toHaveAttribute('open');
  expect(room.querySelectorAll('.rack-list-item')).toHaveLength(1);
  expect(room).toHaveTextContent('6号柜');
});

it('requires confirmation for room deletion and displays a server rejection', async () => {
  render(<Racks />);
  await screen.findByTitle(/FORUM01 · Lenovo/);
  fireEvent.click(screen.getByRole('button', { name: '删除机房 北院7号楼11F' }));
  expect(api).not.toHaveBeenCalledWith('rooms/room', 'DELETE', undefined);
  api.mockRejectedValueOnce(new Error('机房下还有机柜，请先迁移或删除机柜'));
  fireEvent.click(screen.getByRole('button', { name: /^删\s*除$/ }));
  await screen.findByText('机房下还有机柜，请先迁移或删除机柜');
  expect(api).toHaveBeenCalledWith('rooms/room', 'DELETE', undefined);
});

it('submits the rack version when deleting an empty cabinet', async () => {
  render(<Racks />);
  await screen.findByTitle(/FORUM01 · Lenovo/);
  fireEvent.click(within(document.querySelector('.rack-panel') as HTMLElement).getByRole('button', { name: '删除机柜 5号柜' }));
  fireEvent.click(screen.getByRole('button', { name: /^删\s*除$/ }));
  await waitFor(() => expect(api).toHaveBeenCalledWith('racks/rack', 'DELETE', { version: 1 }));
});

it('limits comparison to four cabinets and can remove a comparison', async () => {
  const previous = catalog.racks.length;
  catalog.racks.push(...['3', '4', '5'].map((n) => ({ id: `rack${n}`, roomId: 'room', name: `${n}测试柜`, units: 42, version: 1 })));
  try {
    render(<Racks />);
    await screen.findByTitle(/FORUM01 · Lenovo/);
    const select = screen.getAllByRole('combobox')[0];
    fireEvent.mouseDown(select);
    for (const name of ['6号柜', '3测试柜', '4测试柜']) {
      fireEvent.click(await screen.findByText(`北院7号楼11F / ${name}`));
    }
    expect(document.querySelectorAll('.rack-panel')).toHaveLength(4);
    expect(screen.getByText('北院7号楼11F / 5测试柜').closest('.ant-select-item')).toHaveClass('ant-select-item-option-disabled');
    fireEvent.click(screen.getByRole('button', { name: '关闭并排机柜 4测试柜' }));
    expect(document.querySelectorAll('.rack-panel')).toHaveLength(3);
  } finally {
    catalog.racks.splice(previous);
  }
});

it('edits a room directly in the hierarchy with its previous name', async () => {
  render(<Racks />);
  fireEvent.click(await screen.findByRole('button', { name: '编辑机房 北院7号楼11F' }));
  const modal = screen.getByRole('dialog');
  expect(within(modal).getByLabelText('机房名称')).toHaveValue('北院7号楼11F');
  fireEvent.change(within(modal).getByLabelText('机房名称'), { target: { value: '重命名机房' } });
  fireEvent.click(within(modal).getByRole('button', { name: /保\s*存/ }));
  await waitFor(() => expect(api).toHaveBeenCalledWith('rooms', 'PUT', expect.objectContaining({ id: 'room', previousName: '北院7号楼11F', name: '重命名机房' })));
});
it('edits a rack directly in the hierarchy without selecting that rack', async () => {
  render(<Racks />);
  await screen.findByTitle(/FORUM01 · Lenovo/);
  fireEvent.click(document.querySelector('.rack-room summary')!);
  fireEvent.click(screen.getByRole('button', { name: '编辑机柜 6号柜' }));
  const modal = screen.getByRole('dialog');
  fireEvent.change(within(modal).getByLabelText('机柜名称'), { target: { value: '更名6号柜' } });
  fireEvent.click(within(modal).getByRole('button', { name: /保\s*存/ }));
  await waitFor(() => expect(api).toHaveBeenCalledWith('racks', 'PUT', expect.objectContaining({ id: 'rack2', version: 1, name: '更名6号柜' })));
  expect(document.querySelector('.rack-title h2')).toHaveTextContent('5号柜');
});
