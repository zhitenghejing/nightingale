/** @jest-environment jsdom */
import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { Modal } from 'antd';
import { api, Connection } from './api';
import { confirmHostKeys, HostKeyConfirmationCancelled } from './hostKeys';
jest.mock('./api', () => ({ api: jest.fn() }));
jest.mock('antd', () => ({ ...jest.requireActual('antd'), Modal: { confirm: jest.fn() } }));
const fp = 'SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA';
const changed = 'SHA256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA';
const connection: Connection = { enabled: true, group_id: 1, target: { host: '192.0.2.1', port: 22, fingerprint: '', credential_id: 'cred' }, log_paths: [], record_version: 0 };
beforeEach(() => {
  jest.clearAllMocks();
  (Modal.confirm as jest.Mock).mockReturnValue({ destroy: jest.fn() });
  (api as jest.Mock).mockImplementation((_, __, f) =>
    Promise.resolve({ host: f.connection[f.endpoint].host, port: f.connection[f.endpoint].port, algorithm: 'ssh-ed25519', fingerprint: fp }),
  );
});
test('first save waits for explicit acceptance without exposing a manual input', async () => {
  const pending = confirmHostKeys(connection, 'web-01', undefined, new AbortController().signal);
  await waitFor(() => expect(Modal.confirm).toHaveBeenCalledTimes(1));
  const popup = (Modal.confirm as jest.Mock).mock.calls[0][0];
  render(popup.content);
  expect(screen.getByText(fp)).toBeTruthy();
  expect(screen.queryByRole('textbox')).toBeNull();
  popup.onOk();
  expect((await pending).target.fingerprint).toBe(fp);
  expect(connection.target.fingerprint).toBe('');
});
test('cancelling fingerprint confirmation rejects the save', async () => {
  const pending = confirmHostKeys(connection, undefined, undefined, new AbortController().signal);
  const rejected = expect(pending).rejects.toBeInstanceOf(HostKeyConfirmationCancelled);
  await waitFor(() => expect(Modal.confirm).toHaveBeenCalledTimes(1));
  (Modal.confirm as jest.Mock).mock.calls[0][0].onCancel();
  await rejected;
  expect(api).toHaveBeenCalledTimes(1);
});
test('unchanged trusted keys need no popup, but changed keys show old and new values', async () => {
  const previous = { ...connection, target: { ...connection.target, fingerprint: fp } };
  await confirmHostKeys(connection, 'web-01', previous, new AbortController().signal);
  expect(Modal.confirm).not.toHaveBeenCalled();
  (api as jest.Mock).mockResolvedValue({ host: connection.target.host, port: 22, algorithm: 'ssh-ed25519', fingerprint: changed });
  const pending = confirmHostKeys(connection, 'web-01', previous, new AbortController().signal);
  await waitFor(() => expect(Modal.confirm).toHaveBeenCalledTimes(1));
  const popup = (Modal.confirm as jest.Mock).mock.calls[0][0];
  expect(popup.okButtonProps.danger).toBe(true);
  render(popup.content);
  expect(screen.getByText(fp)).toBeTruthy();
  expect(screen.getByText(changed)).toBeTruthy();
  popup.onOk();
  expect((await pending).target.fingerprint).toBe(changed);
});
test('jump acceptance precedes target discovery and supplies its pinned key', async () => {
  const f = { ...connection, jump: { host: '192.0.2.2', port: 22, fingerprint: '', credential_id: 'jump-cred' } };
  const pending = confirmHostKeys(f, 'web-01', undefined, new AbortController().signal);
  await waitFor(() => expect(Modal.confirm).toHaveBeenCalledTimes(1));
  expect(api).toHaveBeenCalledTimes(1);
  (Modal.confirm as jest.Mock).mock.calls[0][0].onOk();
  await waitFor(() => expect(Modal.confirm).toHaveBeenCalledTimes(2));
  expect((api as jest.Mock).mock.calls[1][2].connection.jump.fingerprint).toBe(fp);
  (Modal.confirm as jest.Mock).mock.calls[1][0].onOk();
  expect((await pending).jump?.fingerprint).toBe(fp);
});
test('closing the editor destroys pending confirmation and cancels saving', async () => {
  const controller = new AbortController();
  const pending = confirmHostKeys(connection, undefined, undefined, controller.signal);
  const rejected = expect(pending).rejects.toBeInstanceOf(HostKeyConfirmationCancelled);
  await waitFor(() => expect(Modal.confirm).toHaveBeenCalledTimes(1));
  controller.abort();
  await rejected;
  expect((Modal.confirm as jest.Mock).mock.results[0].value.destroy).toHaveBeenCalledTimes(1);
});
