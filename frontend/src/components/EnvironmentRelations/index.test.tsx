/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import EnvironmentRelations from './index';
import request from '@/utils/request';
jest.mock('@/utils/request', () => ({ __esModule: true, default: jest.fn() }));
const mocked = jest.mocked(request);
beforeAll(() => { window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() })); const original = window.getComputedStyle; window.getComputedStyle = (element) => original(element); });
beforeEach(() => mocked.mockReset());
test('fetches only after opening, honors read-only projection and clears stale content on denial', async () => {
  const entity = { ref: 'target:1', kind: 'target', name: 'Sim-App', writable: false, fingerprint: 'v1', attributes: { ident: 'sim-app' } };
  mocked.mockResolvedValueOnce({ dat: { entity, relations: [], native_relations: [], gaps: [], truncated: false }, err: '' });
  render(<EnvironmentRelations objectRef='target:1' />);
  expect(mocked).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: '环境关联' }));
  await screen.findAllByText('Sim-App · 监控对象');
  expect(screen.getByRole('button', { name: /新增关系/ })).toHaveProperty('disabled', true);
  await waitFor(() => expect(screen.getByRole('button', { name: '刷新关联' }).className).not.toContain('ant-btn-loading'));
  mocked.mockResolvedValueOnce({ err: '没有权限查阅' });
  fireEvent.click(screen.getByRole('button', { name: '刷新关联' }));
  await screen.findByText('没有权限查阅');
  await waitFor(() => expect(screen.queryByText('Sim-App · 监控对象')).toBeNull());
  expect(screen.getByRole('button', { name: /重\s*试/ })).toBeTruthy();
});
