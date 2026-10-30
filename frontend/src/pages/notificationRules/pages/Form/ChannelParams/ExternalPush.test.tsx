/** @jest-environment jsdom */
import React from 'react';
import { Form, Button } from 'antd';
import { fireEvent, render, screen, within, waitFor } from '@testing-library/react';
import ExternalPush from './ExternalPush';

jest.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
jest.mock('@/pages/notificationChannels/constants', () => ({ NS: 'notification-channels' }));
beforeAll(() => {
  window.matchMedia = jest.fn().mockImplementation(() => ({ matches: false, addListener: jest.fn(), removeListener: jest.fn() }));
});
it('shows all JSON fields and lets a rule explicitly select a company label instead of a literal without retaining both sources', async () => {
  const saved = jest.fn();
  render(
    <Form
      initialValues={{
        notify_configs: [
          {
            params: {
              company_id: 'TEST_FIXED',
              device_type: 'TEST_SERVER',
              device_tags: "['TEST_TAG']",
              device_name_label: 'ident',
              level_map: '{"1":"TEST_LEVEL"}',
              repeat_policy: 'all_dispatches',
            },
          },
        ],
      }}
      onFinish={saved}
    >
      <Form.List name='notify_configs'>
        {(fields) => (
          <>
            {fields.map((field) => (
              <ExternalPush key={field.key} field={field} />
            ))}
          </>
        )}
      </Form.List>
      <Button htmlType='submit'>save mapping</Button>
    </Form>,
  );
  await screen.findByText('external.mapping_title');
  for (const key of ['alarmType', 'companyId', 'deviceType', 'deviceName', 'problemTime', 'recoveryTime', 'triggerName', 'level', 'deviceTags', 'sign'])
    expect(screen.getAllByText(key).length).toBeGreaterThan(0);
  expect(screen.getByLabelText('external.params.company_id')).toHaveProperty('value', 'TEST_FIXED');
  fireEvent.click(within(screen.getByRole('group', { name: 'companyId' })).getByRole('radio', { name: 'external.source_label' }));
  fireEvent.change(await screen.findByLabelText('external.params.company_id_label'), { target: { value: 'external_company' } });
  fireEvent.click(screen.getByRole('button', { name: 'save mapping' }));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  expect(saved.mock.calls[0][0].notify_configs[0].params.company_id).toBeUndefined();
  expect(saved.mock.calls[0][0].notify_configs[0].params.company_id_label).toBe('external_company');
  expect(JSON.parse(screen.getByLabelText('external.structure_preview').textContent || '{}').companyId).toBe('tags["external_company"]');
});

it('reopens a saved label mapping without replacing it with a literal', async () => {
  render(
    <Form
      initialValues={{
        notify_configs: [
          {
            params: {
              company_id_label: 'external_company',
              device_type: 'TEST_TYPE',
              device_tags: 'TEST_TAG',
              device_name_label: 'ident',
              level_map: '{"1":"TEST_LEVEL"}',
              repeat_policy: 'all_dispatches',
            },
          },
        ],
      }}
    >
      <Form.List name='notify_configs'>
        {(fields) => (
          <>
            {fields.map((field) => (
              <ExternalPush key={field.key} field={field} />
            ))}
          </>
        )}
      </Form.List>
    </Form>,
  );
  expect(await screen.findByLabelText('external.params.company_id_label')).toHaveProperty('value', 'external_company');
  expect(screen.queryByLabelText('external.params.company_id')).toBeNull();
  expect(within(screen.getByRole('group', { name: 'companyId' })).getByRole('radio', { name: 'external.source_label' })).toHaveProperty('checked', true);
});

it.each(['rule_append_tags', 'cmdb_model_business'])('saves explicit CMDB matching and tags source %s while clearing inactive mappings', async (source) => {
  const saved = jest.fn();
  render(
    <Form initialValues={{ notify_configs: [{ params: { company_id: 'TEST_COMPANY', device_type: 'TEST_TYPE', device_name_label: 'old_label', device_tags: 'TEST_TAGS', level_map: '{"1":"灾难","2":"严重","3":"警告"}', repeat_policy: 'all_dispatches' } }] }} onFinish={saved}>
      <Form.List name='notify_configs'>{(fields) => <>{fields.map((field) => <ExternalPush key={field.key} field={field} />)}</>}</Form.List>
      <Button htmlType='submit'>save CMDB mapping</Button>
    </Form>,
  );
  fireEvent.mouseDown(await screen.findByLabelText('external.device_source'));
  fireEvent.click(await screen.findByText('external.device_sources.cmdb', { selector: '.ant-select-item-option-content' }));
  fireEvent.change(await screen.findByLabelText('external.cmdb_match_label'), { target: { value: 'ident' } });
  fireEvent.mouseDown(screen.getByLabelText('external.cmdb_match_field'));
  fireEvent.click(await screen.findByText('external.cmdb_fields.hostname', { selector: '.ant-select-item-option-content' }));
  fireEvent.mouseDown(screen.getByLabelText('external.device_tags_source'));
  fireEvent.click(await screen.findByText('external.tags_sources.' + source, { selector: '.ant-select-item-option-content' }));
  expect(screen.queryByLabelText('external.params.device_tags')).toBeNull();
  expect(screen.queryByLabelText('external.params.device_name_label')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'save CMDB mapping' }));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  expect(saved.mock.calls[0][0].notify_configs[0].params).toMatchObject({ device_source: 'cmdb', cmdb_match_label: 'ident', cmdb_match_field: 'hostname', device_tags_source: source, device_type: undefined, device_name_label: undefined, device_tags: undefined });
  const guide = JSON.parse(screen.getByLabelText('external.structure_preview').textContent || '{}');
  expect(guide.deviceType).toBe('CMDB.type (snapshot)');
  expect(guide.deviceName).toBe('CMDB.hostname (snapshot)');
  expect(guide.deviceTags).toContain(source === 'rule_append_tags' ? 'AlertRule.append_tags' : 'CMDB.model');
});
