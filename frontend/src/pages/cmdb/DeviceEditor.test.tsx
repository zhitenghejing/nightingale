/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import DeviceEditor from './DeviceEditor';
import { blankDevice } from './model';
import { api, catalog, resetMock, saveAsset } from './testApi';
jest.mock('./api', () => require('./testApi'));
beforeAll(() => {
  window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() }));
  const original = window.getComputedStyle;
  window.getComputedStyle = (e) => original(e);
});
beforeEach(() => {
  resetMock();
  jest.clearAllMocks();
});
it('selects the exact material, fills canonical identity and saves its stable ID with the asset', async () => {
  const draft = { ...blankDevice, customer: '医院', room: '北院7号楼11F', rack: '5号柜', rackId: 'rack', uStart: 1, uEnd: 1, disks: [] };
  render(<DeviceEditor editor={{ mode: 'add', device: draft }} devices={[]} catalog={catalog} onClose={jest.fn()} onSaved={async () => {}} />);
  const dialog = screen.getByRole('dialog');
  await waitFor(() => expect(within(dialog).getByLabelText('客户信息')).toHaveProperty('value', '医院'));
  fireEvent.change(within(dialog).getByLabelText('序列号'), { target: { value: 'MATERIAL-001' } });
  fireEvent.mouseDown(within(dialog).getByLabelText('设备素材型号'));
  fireEvent.click(await screen.findByText('Dell 戴尔 PowerEdge R750 · 2U'));
  expect(within(dialog).getByLabelText('厂商')).toHaveProperty('value', 'Dell');
  expect(within(dialog).getByLabelText('型号')).toHaveProperty('value', 'PowerEdge R750');
  expect(within(dialog).getByLabelText('结束 U 位')).toHaveProperty('value', '2');
  fireEvent.click(within(dialog).getByRole('button', { name: '保存设备' }));
  await waitFor(() =>
    expect(saveAsset).toHaveBeenCalledWith(
      expect.objectContaining({ serial: 'MATERIAL-001', vendor: 'Dell', model: 'PowerEdge R750', assetMaterialId: 'dell-r750', uStart: 1, uEnd: 2 }),
      true,
    ),
  );
});

it('confirms a versioned device deletion and refreshes the inventory', async () => {
  const onClose = jest.fn();
  const onSaved = jest.fn(async () => {});
  render(
    <DeviceEditor
      editor={{ mode: 'view', device: { ...blankDevice, id: 'device', version: 7, serial: 'DELETE-7', disks: [] } }}
      devices={[]}
      catalog={catalog}
      onClose={onClose}
      onSaved={onSaved}
      canDelete
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: '删除设备' }));
  expect(api).not.toHaveBeenCalledWith('delete', 'POST', expect.anything());
  fireEvent.click(await screen.findByRole('button', { name: '确认删除' }));
  await waitFor(() => expect(api).toHaveBeenCalledWith('delete', 'POST', [{ id: 'device', version: 7 }]));
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(onSaved).toHaveBeenCalledTimes(1);
});
it('keeps the device open and shows a stale-version rejection', async () => {
  const onClose = jest.fn();
  render(
    <DeviceEditor
      editor={{ mode: 'view', device: { ...blankDevice, id: 'device', version: 1, disks: [] } }}
      devices={[]}
      catalog={catalog}
      onClose={onClose}
      onSaved={async () => {}}
      canDelete
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: '删除设备' }));
  api.mockRejectedValueOnce(new Error('设备已修改，请刷新'));
  fireEvent.click(await screen.findByRole('button', { name: '确认删除' }));
  await screen.findByText('设备已修改，请刷新');
  expect(onClose).not.toHaveBeenCalled();
});
it('disables deletion for users without the delete grant', () => {
  render(
    <DeviceEditor
      editor={{ mode: 'view', device: { ...blankDevice, id: 'device', version: 1, disks: [] } }}
      devices={[]}
      catalog={catalog}
      onClose={jest.fn()}
      onSaved={async () => {}}
      canDelete={false}
    />,
  );
  expect(screen.getByRole('button', { name: '删除设备' })).toBeDisabled();
});

it('opens API assets whose optional disk list is absent', async () => {
  const device = JSON.parse('{"id":"minimal","serial":"MINIMAL-1","version":1,"customer":"API"}');
  render(<DeviceEditor editor={{ mode: 'view', device }} devices={[]} catalog={catalog} onClose={jest.fn()} onSaved={async () => {}} canDelete />);
  const modal = screen.getByRole('dialog');
  await waitFor(() => expect(within(modal).getByLabelText('序列号')).toHaveValue('MINIMAL-1'));
  expect(within(modal).getByText('暂无硬盘信息')).toBeInTheDocument();
  expect(within(modal).getByRole('button', { name: '删除设备' })).toBeEnabled();
});
