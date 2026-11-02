/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import CatalogBrowser from './CatalogBrowser';
import { getCatalog } from './catalog';
import ComponentFormModal from './components/ComponentFormModal';

jest.mock('@/App', () => ({ CommonStateContext: require('react').createContext({ profile: { admin: true }, perms: [] }) }));
jest.mock('@/services/collection', () => ({ collectionRequest: jest.fn() }));

jest.mock('@/components/AuthorizationWrapper', () => ({ __esModule: true, default: ({ children }) => children }));
jest.mock('./components/ComponentFormModal', () => ({ __esModule: true, default: jest.fn() }));
jest.mock('./catalog', () => ({ ...jest.requireActual('./catalog'), getCatalog: jest.fn() }));
const catalog = {
  categories: [
    { id: 'os', parent_id: '', name: '操作系统', record_version: 1 },
    { id: 'win', parent_id: 'os', name: 'Windows', record_version: 1 },
    { id: 'general', parent_id: 'win', name: '通用', record_version: 1 },
  ],
  entries: [
    {
      component_id: 1,
      ident: 'Windows_SNMP',
      name: 'Windows SNMP',
      logo: '/windows.svg',
      category_id: 'general',
      disabled: false,
      record_version: 1,
      draft: { adapter: 'documentation', fields: [], config: '', notification: 'events_only' },
      published: null,
      published_version_id: '',
    },
  ],
};
test('card opens details and pencil independently opens only the metadata dialog', async () => {
  (getCatalog as jest.Mock).mockResolvedValue(catalog);
  const onOpen = jest.fn();
  render(<CatalogBrowser components={[{ id: 1, ident: 'Windows_SNMP', logo: '', readme: 'instructions', disabled: 0 }]} onOpen={onOpen} reload={jest.fn()} />);
  await waitFor(() => expect(screen.getByRole('button', { name: '配置模板 Windows SNMP' })).toBeTruthy());
  fireEvent.click(screen.getByRole('button', { name: '编辑基本信息 Windows SNMP' }));
  expect(onOpen).not.toHaveBeenCalled();
  expect(ComponentFormModal).toHaveBeenCalledWith(expect.objectContaining({ action: 'edit', catalogEntry: expect.objectContaining({ component_id: 1 }), commonState: expect.objectContaining({ profile: { admin: true } }) }));
  fireEvent.click(screen.getByRole('button', { name: '配置模板 Windows SNMP' }));
  expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ id: 1, display_name: 'Windows SNMP' }));
  expect(screen.queryByRole('button', { name: '添加设备' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '全部折叠' }));
  fireEvent.click(screen.getByRole('button', { name: '列表模式' }));
  expect(screen.getByText('Windows SNMP')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '卡片模式' }));
  expect(screen.getByRole('button', { name: '配置模板 Windows SNMP' })).toBeTruthy();
});
