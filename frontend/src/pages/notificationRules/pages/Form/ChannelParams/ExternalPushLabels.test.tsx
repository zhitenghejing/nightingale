/** @jest-environment jsdom */
import React from 'react';
import { Form, Button } from 'antd';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import ExternalPush from './ExternalPush';
import request from '@/utils/request';
jest.mock('@/utils/request', () => ({ __esModule: true, default: jest.fn() }));
jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
jest.mock('@/pages/notificationChannels/constants', () => ({ NS: 'notification-channels' }));
beforeAll(() => { window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() })); });
const initial = () => ({ mapping_version: 'event_labels_v1', company_id: 'TEST_COMPANY', device_source: 'event', device_type_label: 'type', device_name_label: 'ident', device_tags_source: 'event_labels', device_tags_labels: '[{"label":"model","required":true},{"label":"business","required":false}]', level_map: '{"1":"灾难","2":"严重","3":"警告"}', repeat_policy: 'once_per_incident' });
function mount(params: Record<string, string>, saved = jest.fn()) {
  render(<Form initialValues={{ notify_configs: [{ channel_id: 25, params }] }} onFinish={saved}><Form.List name='notify_configs'>{(fields) => <>{fields.map((field) => <ExternalPush key={field.key} field={field} />)}</>}</Form.List><Button htmlType='submit'>save mapping</Button></Form>);
  return saved;
}
it('saves the A table with ordered required/optional labels and keeps company/Severity sources', async () => {
  const saved = mount(initial());
  expect(screen.getByLabelText('deviceType source')).toHaveProperty('value', 'type');
  expect(screen.getByLabelText('deviceName source')).toHaveProperty('value', 'ident');
  expect(screen.queryByLabelText('level source')).toBeNull();
  fireEvent.click(screen.getByLabelText('deviceTags up 2'));
  expect(screen.getByLabelText('deviceTags 1')).toHaveProperty('value', 'business');
  expect(screen.getByLabelText('deviceTags required 1')).toHaveProperty('checked', false);
  fireEvent.click(within(screen.getByRole('group', { name: 'companyId' })).getByRole('radio', { name: 'external.source_label' }));
  fireEvent.change(await screen.findByLabelText('company_id_label'), { target: { value: 'external_company' } });
  fireEvent.click(screen.getByText('save mapping'));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  const params = saved.mock.calls[0][0].notify_configs[0].params;
  expect(params.company_id).toBeUndefined(); expect(params.company_id_label).toBe('external_company');
  expect(params.mapping_version).toBe('event_labels_v1'); expect(params.repeat_policy).toBe('once_per_incident');
  expect(JSON.parse(params.device_tags_labels)).toEqual([{ label: 'business', required: false }, { label: 'model', required: true }]);
  expect(JSON.parse(params.level_map)).toEqual({ 1: '灾难', 2: '严重', 3: '警告' });
});
it('retains saved CMDB mapping until explicit conversion and then leaves unknown keys empty', async () => {
  mount({ company_id: 'TEST_COMPANY', device_source: 'cmdb', cmdb_match_field: 'hostname', cmdb_match_label: 'ident', device_tags_source: 'cmdb_model_business', level_map: '{"1":"灾难","2":"严重","3":"警告"}', repeat_policy: 'all_dispatches' });
  expect(screen.queryByLabelText('deviceType source')).toBeNull();
  expect(screen.getByText('CMDB.type')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'external.labels.switch_mapping' }));
  expect(await screen.findByLabelText('deviceType source')).toHaveProperty('value', '');
  expect(screen.getByLabelText('deviceName source')).toHaveProperty('value', '');
  expect(screen.getByLabelText('company_id')).toHaveProperty('value', 'TEST_COMPANY');
  expect(screen.queryByText('CMDB.type')).toBeNull();
});
it('checks a draft via the read-only preview endpoint and invalidates stale results after edits', async () => {
  jest.mocked(request).mockResolvedValue({ dat: { payload: { deviceTags: 'R650', sign: '[REDACTED]' }, omitted_tags: [{ label: 'business', reason: 'missing' }], recovery_pending: false } });
  mount(initial());
  fireEvent.change(screen.getByLabelText('external.labels.event_id'), { target: { value: '34' } });
  fireEvent.click(screen.getByRole('button', { name: 'external.labels.check_mapping' }));
  expect(await screen.findByLabelText('external.labels.preview_payload')).toBeTruthy();
  expect(screen.getByText('business: external.labels.omitted_missing')).toBeTruthy();
  expect(request).toHaveBeenCalledWith('/api/n9e/external-push-preview', expect.objectContaining({ data: expect.objectContaining({ channel_id: 25, event_id: 34 }) }));
  fireEvent.change(screen.getByLabelText('deviceName source'), { target: { value: 'node' } });
  await waitFor(() => expect(screen.queryByLabelText('external.labels.preview_payload')).toBeNull());
});

it('new mappings default to following alert-rule dispatches without an extra reminder selector', async () => {
  const saved = mount({ company_id: 'TEST_COMPANY', level_map: '{"1":"灾难","2":"严重","3":"警告"}' });
  fireEvent.click(screen.getByRole('button', { name: 'external.labels.switch_mapping' }));
  expect(await screen.findByLabelText('deviceType source')).toHaveProperty('value', '');
  expect(screen.queryByText('external.params.repeat_policy')).toBeNull();
  fireEvent.change(screen.getByLabelText('deviceType source'), { target: { value: 'kind' } });
  fireEvent.change(screen.getByLabelText('deviceName source'), { target: { value: 'node' } });
  fireEvent.click(screen.getByRole('button', { name: 'external.labels.add_source' }));
  fireEvent.change(screen.getByLabelText('deviceTags 1'), { target: { value: 'model' } });
  fireEvent.click(screen.getByText('save mapping'));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  expect(saved.mock.calls[0][0].notify_configs[0].params.repeat_policy).toBe('all_dispatches');
});
