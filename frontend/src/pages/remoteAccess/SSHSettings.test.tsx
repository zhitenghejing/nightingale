/** @jest-environment jsdom */
import React, { createRef } from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import SSHSettings, { endpointInput, SSHSettingsHandle } from './SSHSettings';
import { api } from './api';

jest.mock('./api', () => ({ ...jest.requireActual('./api'), api: jest.fn() }));

const features = { enabled: true, configure: true, manage_credentials: true, execute: true, rdp: true };
const connection = {
  configured: true,
  ident: 'web-01',
  enabled: true,
  protocol: 'ssh',
  group_id: 1,
  target: { host: '192.0.2.10', port: 22, fingerprint: 'SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', credential_id: 'credential-id' },
  log_paths: ['/var/log/test.log'],
  record_version: 4,
};

const openSettings = async () => fireEvent.click(await screen.findByText('设备控制台（可选）'));

beforeEach(() => {
  jest.clearAllMocks();
  (api as jest.Mock).mockImplementation((path: string) => Promise.resolve(path === 'features' ? features : connection));
});

test('existing account fields stay blank and only that device can reuse its saved binding', async () => {
  const ref = createRef<SSHSettingsHandle>();
  render(<SSHSettings ref={ref} ident='web-01' group={1} />);
  await openSettings();
  expect(((await screen.findByLabelText('SSH 账号')) as HTMLInputElement).value).toBe('');
  expect((screen.getByLabelText('SSH 密码') as HTMLInputElement).value).toBe('');
  expect(screen.queryByText('已保存凭据')).toBeNull();
  await expect(ref.current!.value(true)).resolves.toBeUndefined();
  fireEvent.change(screen.getByLabelText('允许读取的日志文件（每行一个绝对路径）'), { target: { value: '/var/log/app.log' } });
  await expect(ref.current!.value()).resolves.toMatchObject({ target: connection.target, log_paths: ['/var/log/app.log'], record_version: 4 });
});

test('a new Windows device defaults to RDP and the optional settings need an inline account only if enabled', async () => {
  const ref = createRef<SSHSettingsHandle>();
  render(<SSHSettings ref={ref} group={1} host='192.0.2.20' defaultProtocol='rdp' />);
  await openSettings();
  await expect(ref.current!.value()).resolves.toBeUndefined();
  fireEvent.click(screen.getByRole('switch'));
  expect(((await screen.findByLabelText('RDP 地址（固定 IP）')) as HTMLInputElement).value).toBe('192.0.2.20');
  expect((screen.getByLabelText('端口') as HTMLInputElement).value).toBe('3389');
  expect(screen.queryByText('测试 SSH 连接')).toBeNull();
  await expect(ref.current!.value()).rejects.toThrow('请手工填写此连接的账号和密码');
  fireEvent.change(screen.getByLabelText('RDP 账号'), { target: { value: 'fixture-user' } });
  fireEvent.change(screen.getByLabelText('RDP 密码'), { target: { value: 'fixture-password' } });
  await expect(ref.current!.value(true)).resolves.toMatchObject({
    protocol: 'rdp',
    target: { host: '192.0.2.20', port: 3389, new_credential: { username: 'fixture-user', password: 'fixture-password' } },
  });
  expect(api).toHaveBeenCalledTimes(1);
});

test('a changed address or port requires fresh credentials and removes the SSH key pin', () => {
  const target = { ...connection.target, port: 2200 };
  expect(() => endpointInput(target, 1, connection.target)).toThrow('请手工填写此连接的账号和密码');
  expect(endpointInput({ ...target, username: 'readonly', password: 'new-password' }, 1, connection.target)).toEqual({
    host: target.host,
    port: 2200,
    fingerprint: '',
    credential_id: '',
    new_credential: { name: '', group_id: 1, username: 'readonly', password: 'new-password' },
  });
});

test.each([true, false])('SSH connection test reports authentication result without saving: %s', async (ok) => {
  (api as jest.Mock).mockImplementation((path: string) => {
    if (path === 'connection/test') return ok ? Promise.resolve({ ok: true, via_jump: false, duration_ms: 5 }) : Promise.reject(new Error('认证失败'));
    if (path === 'host-key') return Promise.resolve({ ...connection.target, algorithm: 'ssh-ed25519' });
    return Promise.resolve(path === 'features' ? features : connection);
  });
  render(<SSHSettings ident='web-01' group={1} />);
  await openSettings();
  await act(async () => fireEvent.click(await screen.findByRole('button', { name: '测试 SSH 连接' })));
  expect(await screen.findByText(ok ? /SSH 连接成功/ : /SSH 连接测试失败：认证失败/)).toBeTruthy();
  expect((api as jest.Mock).mock.calls.filter(([path]) => path === 'connection/test')).toHaveLength(1);
});
