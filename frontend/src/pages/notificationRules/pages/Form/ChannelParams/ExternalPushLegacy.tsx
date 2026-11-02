import React, { useState } from 'react';
import { Alert, Form, Input, Radio, Select } from 'antd';
import { FormListFieldData } from 'antd/lib/form/FormList';
import { useTranslation } from 'react-i18next';
import { NS } from '@/pages/notificationChannels/constants';
import ExternalPushFields from '@/pages/notificationChannels/ExternalPushFields';

function RuleSource({ field, name, target }: { field: FormListFieldData; name: string; target: string }) {
  const { t } = useTranslation(NS);
  const form = Form.useFormInstance();
  const path = ['notify_configs', field.name, 'params'];
  const params = Form.useWatch(path) || {};
  const [source, setSource] = useState(() => (form.getFieldValue([...path, name + '_label']) ? 'label' : 'literal'));
  const key = source === 'label' ? name + '_label' : name;
  return (
    <fieldset aria-label={target} className='mb-3'>
      <legend style={{ fontSize: 14 }}>
        <code>{target}</code>
      </legend>
      {params[name]?.trim() && params[name + '_label']?.trim() && <Alert className='mb-2' type='error' message={t('external.source_conflict')} />}
      <Radio.Group
        className='mb-2'
        value={source}
        onChange={(event) => {
          const next = event.target.value;
          // An explicit source change discards only the inactive mapping. Loading
          // an existing contradictory configuration never silently chooses one.
          form.setFields([{ name: [...path, next === 'label' ? name : name + '_label'], value: undefined }]);
          setSource(next);
        }}
      >
        <Radio value='literal'>{t('external.source_literal')}</Radio>
        <Radio value='label'>{t('external.source_label')}</Radio>
      </Radio.Group>
      <Form.Item name={[field.name, 'params', key]} label={t(`external.params.${key}`)} rules={[{ required: true, whitespace: true }]}>
        <Input.TextArea autoSize={{ minRows: 1, maxRows: 4 }} />
      </Form.Item>
    </fieldset>
  );
}

export default function ExternalPush({ field }: { field: FormListFieldData }) {
  const { t } = useTranslation(NS);
  const form = Form.useFormInstance();
  const params = Form.useWatch(['notify_configs', field.name, 'params']);
  const path = ['notify_configs', field.name, 'params'];
  const deviceSource = params?.device_source || 'event';
  const tagsSource = params?.device_tags_source || 'mapping';
  return (
    <>
      <Alert className='mb-4' type='warning' showIcon message={t('external.recovery_notice')} description={t('external.rule_notice')} />
      <h3>{t('external.edit_mapping')}</h3>
      <RuleSource field={field} name='company_id' target='companyId' />
      <Form.Item name={[field.name, 'params', 'device_source']} label={t('external.device_source')} getValueProps={(value) => ({ value: value || 'event' })}>
        <Select options={['event', 'cmdb'].map((value) => ({ value, label: t('external.device_sources.' + value) }))} onChange={(value) => {
          const inactive = value === 'cmdb' ? ['device_type', 'device_type_label', 'device_name_label'] : tagsSource === 'cmdb_model_business' ? [] : ['cmdb_match_field', 'cmdb_match_label'];
          form.setFields(inactive.map((key) => ({ name: [...path, key], value: undefined })));
        }} />
      </Form.Item>
      {(deviceSource === 'cmdb' || tagsSource === 'cmdb_model_business') && <>
        <Alert className='mb-3' type='info' message={t('external.cmdb_notice')} />
        <Form.Item name={[field.name, 'params', 'cmdb_match_field']} label={t('external.cmdb_match_field')} rules={[{ required: true }]}>
          <Select options={['hostname', 'businessIp', 'managementIp'].map((value) => ({ value, label: t('external.cmdb_fields.' + value) }))} />
        </Form.Item>
        <Form.Item name={[field.name, 'params', 'cmdb_match_label']} label={t('external.cmdb_match_label')} rules={[{ required: true, whitespace: true }]}><Input autoComplete='off' /></Form.Item>
      </>}
      {deviceSource !== 'cmdb' && <>
        <RuleSource field={field} name='device_type' target='deviceType' />
        <Form.Item name={[field.name, 'params', 'device_name_label']} label={t('external.params.device_name_label')} rules={[{ required: true, whitespace: true }]}><Input.TextArea autoSize={{ minRows: 1, maxRows: 4 }} /></Form.Item>
      </>}
      <Form.Item name={[field.name, 'params', 'device_tags_source']} label={t('external.device_tags_source')} getValueProps={(value) => ({ value: value || 'mapping' })}>
        <Select options={['cmdb_model_business', 'mapping', 'rule_append_tags'].map((value) => ({ value, label: t('external.tags_sources.' + value) }))} onChange={(value) => {
          if (value !== 'mapping') form.setFields(['device_tags', 'device_tags_label'].map((key) => ({ name: [...path, key], value: undefined })));
        }} />
      </Form.Item>
      {tagsSource === 'cmdb_model_business' ? <Alert className='mb-3' type='info' message={t('external.cmdb_tags_notice')} /> : tagsSource === 'rule_append_tags' ? <Alert className='mb-3' type='info' message={t('external.append_tags_notice')} /> : <RuleSource field={field} name='device_tags' target='deviceTags' />}
      {['level_map'].map((key) => (
        <Form.Item key={key} name={[field.name, 'params', key]} label={t(`external.params.${key}`)} rules={[{ required: true, whitespace: true }]}>
          <Input.TextArea autoSize={{ minRows: 1, maxRows: 4 }} />
        </Form.Item>
      ))}
      {/* Preserve retired metadata without exposing a second reminder policy. */}
      <Form.Item hidden name={[field.name, 'params', 'repeat_policy']}><Input /></Form.Item>
      <ExternalPushFields params={params} />
    </>
  );
}
