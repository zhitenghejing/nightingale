/** @jest-environment jsdom */
import React, { useState } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import InventoryFilters from './InventoryFilters';
import { emptyQuery, matchesInventoryQuery } from './inventoryQuery';
import { sampleDevices } from './model';
beforeAll(() => {
  window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() }));
});
function Harness() {
  const [query, setQuery] = useState(emptyQuery);
  return (
    <>
      <InventoryFilters value={query} onChange={setQuery} />
      <ul>
        {sampleDevices()
          .filter((d) => matchesInventoryQuery(d, query, '2026-10-23'))
          .map((d) => (
            <li key={d.id}>{d.serial}</li>
          ))}
      </ul>
    </>
  );
}
it('combines a single keyword input with field conditions and can remove/reset them', async () => {
  render(<Harness />);
  fireEvent.change(screen.getByRole('textbox', { name: '综合搜索设备' }), { target: { value: 'DELL LIS' } });
  expect(screen.getAllByRole('listitem')).toHaveLength(2);
  fireEvent.click(screen.getByRole('button', { name: /添加条件/ }));
  fireEvent.mouseDown(screen.getByRole('combobox', { name: '筛选字段' }));
  fireEvent.click(await screen.findByText('业务 IP', { exact: true }));
  fireEvent.change(screen.getByRole('textbox', { name: '筛选内容' }), { target: { value: '192.0.2.35' } });
  fireEvent.click(screen.getByRole('button', { name: '添加筛选条件' }));
  await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(1));
  expect(screen.getByRole('listitem')).toHaveTextContent('DEMO-DELL-002');
  fireEvent.click(screen.getByRole('button', { name: '移除筛选 业务 IP：192.0.2.35' }));
  expect(screen.getAllByRole('listitem')).toHaveLength(2);
  fireEvent.click(screen.getByRole('button', { name: '重置筛选' }));
  expect(screen.getByRole('textbox', { name: '综合搜索设备' })).toHaveValue('');
  expect(screen.getAllByRole('listitem')).toHaveLength(10);
});
it('uses text date-picker inputs with clear start/end placeholders', () => {
  render(<Harness />);
  expect(screen.getByPlaceholderText('过保开始日期')).toHaveProperty('type', 'text');
  expect(screen.getByPlaceholderText('过保结束日期')).toHaveProperty('type', 'text');
  expect(screen.queryByPlaceholderText(/yyyy|mm|dd/i)).not.toBeInTheDocument();
});
