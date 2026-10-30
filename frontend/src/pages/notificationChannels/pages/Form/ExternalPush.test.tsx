/** @jest-environment jsdom */
import React from 'react';
import i18next from 'i18next';
import { Form, Input } from 'antd';
import { act, fireEvent, render, screen } from '@testing-library/react';
import ExternalPush from './ExternalPush';
import { getNotificationChannelTypes, NS } from '../../constants';
import zhCN from '../../locale/zh_CN';

const mockRequest = jest.fn();
jest.mock('@/utils/request', () => ({ __esModule: true, default: (...args: unknown[]) => mockRequest(...args) }));
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
beforeAll(async () => {
  await i18next.init({ lng: 'zh_CN', resources: { zh_CN: { [NS]: zhCN } } });
  window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() }));
});
beforeEach(() => mockRequest.mockReset());
it('defaults to the confirmed platform 1.0 endpoint, disabled and without a signature', async () => {
  const preset = getNotificationChannelTypes().external_push.default_values;
  expect(preset.enable).toBe(false);
  expect(preset.name).toBe('1.0平台推送');
  expect(preset.param_config.user_info).toBeUndefined();
  expect(preset.request_config.external_push_config).toMatchObject({
    mode: 'platform10',
    endpoint: 'https://admin.ztservice.cn/messageWarn!acceptWarn.act',
    destination: 'platform10',
    test_sign: '',
    signing_mode: 'key_file',
    signing_key_ref: 'platform10-v1',
    problem_type: 'problem',
    recovery_type: 'ok',
  });
  render(
    <Form initialValues={preset}>
      <Form.Item name='request_type' hidden>
        <Input />
      </Form.Item>
      <ExternalPush />
    </Form>,
  );
  expect(await screen.findByText('external.recovery_notice')).toBeTruthy();
  expect(screen.queryByLabelText('external.test_sign')).toBeNull();
  expect(screen.getByLabelText('external.signing_key_ref')).toHaveProperty('value', 'platform10-v1');
  expect(screen.getByLabelText('external.endpoint')).toHaveProperty('readOnly', true);
  expect(screen.getByText('external.platform10_notice')).toBeTruthy();
});

it('shows confirmed event types as read-only values while keeping signing explicitly test-only', async () => {
  const preset = getNotificationChannelTypes().external_push.default_values;
  render(
    <Form initialValues={preset}>
      <Form.Item name='request_type' hidden>
        <Input />
      </Form.Item>
      <ExternalPush />
    </Form>,
  );
  const problem = await screen.findByLabelText('external.problem_type');
  const recovery = screen.getByLabelText('external.recovery_type');
  expect(problem).toHaveProperty('value', 'problem');
  expect(recovery).toHaveProperty('value', 'ok');
  expect(problem).toHaveProperty('readOnly', true);
  expect(recovery).toHaveProperty('readOnly', true);
  expect(screen.queryByLabelText('external.test_sign')).toBeNull();
  expect(screen.getByLabelText('external.signing_key_ref')).toHaveProperty('value', 'platform10-v1');
});

it('reads only center readiness on explicit check and never accepts key material in the form', async () => {
  mockRequest.mockResolvedValue({ dat: { reference: 'platform10-v1', ready: true, reason: 'TEST_READY' } });
  const preset = getNotificationChannelTypes().external_push.default_values;
  render(
    <Form initialValues={preset}>
      <Form.Item name='request_type' hidden>
        <Input />
      </Form.Item>
      <ExternalPush />
    </Form>,
  );
  fireEvent.click(await screen.findByRole('button', { name: 'external.check_key' }));
  await screen.findByText('TEST_READY');
  expect(mockRequest).toHaveBeenCalledWith('/api/n9e/external-push-signing-key', { method: 'Get', params: { reference: 'platform10-v1' } });
  expect(screen.queryByLabelText('external.test_sign')).toBeNull();
});

it('keeps an existing explicit test signature usable without silently selecting a file key', async () => {
  const preset = getNotificationChannelTypes().external_push.default_values;
  const initial = {
    ...preset,
    request_config: { external_push_config: { ...preset.request_config.external_push_config, mode: 'simulation', endpoint: 'http://127.0.0.1:19999/mock', signing_mode: undefined, signing_key_ref: undefined, test_sign: 'TEST_LEGACY_ONLY' } },
  };
  render(
    <Form initialValues={initial}>
      <Form.Item name='request_type' hidden>
        <Input />
      </Form.Item>
      <ExternalPush />
    </Form>,
  );
  expect(await screen.findByLabelText('external.test_sign')).toHaveProperty('value', 'TEST_LEGACY_ONLY');
  expect(screen.queryByLabelText('external.signing_key_ref')).toBeNull();
});

it('discards readiness for an old reference when a request completes after the reference changes', async () => {
  let complete: (value: unknown) => void = () => {};
  mockRequest.mockReturnValue(new Promise((resolve) => (complete = resolve)));
  const preset = getNotificationChannelTypes().external_push.default_values;
  render(
    <Form initialValues={preset}>
      <Form.Item name='request_type' hidden><Input /></Form.Item>
      <ExternalPush />
    </Form>,
  );
  fireEvent.click(await screen.findByRole('button', { name: 'external.check_key' }));
  fireEvent.change(screen.getByLabelText('external.signing_key_ref'), { target: { value: 'platform10-v2' } });
  await act(async () => complete({ dat: { reference: 'platform10-v1', ready: true, reason: 'OLD_REFERENCE_READY' } }));
  expect(screen.getByLabelText('external.signing_key_ref')).toHaveProperty('value', 'platform10-v2');
  expect(screen.queryByText('OLD_REFERENCE_READY')).toBeNull();
  expect(screen.queryByText('external.key_ready')).toBeNull();
});

it('clears the real endpoint for simulation and restores file-only signing on returning to real mode', async () => {
  const preset = getNotificationChannelTypes().external_push.default_values;
  render(
    <Form initialValues={preset}>
      <Form.Item name='request_type' hidden><Input /></Form.Item>
      <ExternalPush />
    </Form>,
  );
  fireEvent.mouseDown(await screen.findByLabelText('external.mode'));
  fireEvent.click(await screen.findByText('external.simulation', { selector: '.ant-select-item-option-content' }));
  expect(screen.getByLabelText('external.endpoint')).toHaveProperty('value', '');
  expect(screen.getByLabelText('external.endpoint')).toHaveProperty('readOnly', false);
  fireEvent.mouseDown(screen.getByLabelText('external.signing_mode'));
  fireEvent.click(await screen.findByText('external.signing_modes.test_value', { selector: '.ant-select-item-option-content' }));
  fireEvent.change(await screen.findByLabelText('external.test_sign'), { target: { value: 'TEST_ONLY_SIMULATION' } });
  fireEvent.mouseDown(screen.getByLabelText('external.mode'));
  fireEvent.click(await screen.findByText('external.platform10', { selector: '.ant-select-item-option-content' }));
  expect(screen.getByLabelText('external.endpoint')).toHaveProperty('value', 'https://admin.ztservice.cn/messageWarn!acceptWarn.act');
  expect(screen.getByLabelText('external.endpoint')).toHaveProperty('readOnly', true);
  expect(screen.queryByLabelText('external.test_sign')).toBeNull();
  expect(screen.getByLabelText('external.signing_key_ref')).toHaveProperty('value', 'platform10-v1');
});
