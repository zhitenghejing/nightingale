/** @jest-environment jsdom */
import React from 'react';
import { render } from '@testing-library/react';
import { TabMenu } from './TabMenu';
import { getMenuList } from '@/components/SideMenu/menu';
import { findMenuByPath, getMenuItemPath, getStorageKey, isInventoryMenuPath } from '@/components/SideMenu/utils';

const push = jest.fn();
jest.mock('react-router-dom', () => ({ useHistory: () => ({ push }) }));
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
jest.mock('@/components/IconFont', () => ({ __esModule: true, default: () => null }));
jest.mock('@/utils/constant', () => ({ IS_PLUS: false, IS_ENT: false }));
jest.mock('@/App', () => ({ CommonStateContext: require('react').createContext({ perms: ['/targets'], darkMode: false }) }));

test('infrastructure pages are direct sidebar links with no remembered device tabs', () => {
  const group = getMenuList().find((item) => item.key === 'infrastructure')!;
  expect(group.children.map((item) => item.key)).toEqual(['/targets', '/targets/cmdb', '/targets/racks']);
  localStorage.setItem(getStorageKey('devices'), '/targets/racks');
  for (const item of group.children) {
    expect(item.type).not.toBe('tabs');
    expect(getMenuItemPath(item)).toBe(item.key);
    const view = render(<TabMenu currentMenu={findMenuByPath(item.key, [group])} />);
    expect(view.container).toBeEmptyDOMElement();
    view.unmount();
  }
  expect(isInventoryMenuPath('/targets/cmdb')).toBe(true);
  expect(isInventoryMenuPath('/targets/racks')).toBe(true);
  expect(isInventoryMenuPath('/targets')).toBe(false);
  localStorage.clear();
});
