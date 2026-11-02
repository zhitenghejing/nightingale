import React, { useEffect, useRef, useState } from 'react';
import { Alert, Button, Checkbox, Form, Input, InputNumber, Radio, Select, Space, Table, Tag } from 'antd';
import { FormListFieldData } from 'antd/lib/form/FormList';
import { useTranslation } from 'react-i18next';
import request from '@/utils/request';
import { RequestMethod } from '@/store/common';
import { NS } from '@/pages/notificationChannels/constants';
import ExternalPushFields from '@/pages/notificationChannels/ExternalPushFields';
import LegacyEditor from './ExternalPushLegacy';

const VERSION = 'event_labels_v1';
const defaultLevels = { '1': '灾难', '2': '严重', '3': '警告' };
const levels = ['灾难', '严重', '警告', '一般'];
interface LabelSource { label: string; required: boolean }
interface Preview { payload: Record<string, string>; omitted_tags: { label: string; reason: string }[]; recovery_pending: boolean }
function readLabels(value: string): LabelSource[] | undefined {
  try {
    const list = JSON.parse(value);
    if (!Array.isArray(list) || list.some((item) => !item || typeof item.label !== 'string' || typeof item.required !== 'boolean' || Object.keys(item).some((key) => !['label', 'required'].includes(key)))) return undefined;
    return list;
  } catch { return undefined; }
}
function TagLabels({ value = '[]', onChange }: { value?: string; onChange?: (value: string) => void }) {
  const { t } = useTranslation(NS);
  const list = readLabels(value);
  if (!list) return <Alert type='error' message={t('external.labels.invalid_list')} action={<Button onClick={() => onChange?.('[]')}>{t('external.labels.reset_list')}</Button>} />;
  const change = (next: LabelSource[]) => onChange?.(JSON.stringify(next));
  return <div>
    {list.map((item, index) => <div key={index} className='flex gap-1 items-center mb-2'>
      <span className='text-soft'>{index + 1}</span>
      <Input aria-label={`deviceTags ${index + 1}`} value={item.label} placeholder={t('external.labels.label_placeholder')} onChange={(e) => change(list.map((old, i) => i === index ? { ...old, label: e.target.value } : old))} />
      <Checkbox style={{ whiteSpace: 'nowrap', flexShrink: 0 }} aria-label={`deviceTags required ${index + 1}`} checked={item.required} onChange={(e) => change(list.map((old, i) => i === index ? { ...old, required: e.target.checked } : old))}>{t('external.labels.required')}</Checkbox>
      <Button size='small' aria-label={`deviceTags up ${index + 1}`} disabled={!index} onClick={() => { const next = [...list]; [next[index - 1], next[index]] = [next[index], next[index - 1]]; change(next); }}>↑</Button>
      <Button size='small' aria-label={`deviceTags down ${index + 1}`} disabled={index === list.length - 1} onClick={() => { const next = [...list]; [next[index + 1], next[index]] = [next[index], next[index + 1]]; change(next); }}>↓</Button>
      <Button size='small' aria-label={`deviceTags remove ${index + 1}`} onClick={() => change(list.filter((_, i) => i !== index))}>×</Button>
    </div>)}
    <Button size='small' disabled={list.length >= 64} onClick={() => change([...list, { label: '', required: true }])}>{t('external.labels.add_source')}</Button>
    <p className='text-soft text-xs mt-2 mb-0'>{t('external.labels.join_notice')}</p>
  </div>;
}
function LevelMap({ value = '{}', onChange }: { value?: string; onChange?: (value: string) => void }) {
  const { t } = useTranslation(NS);
  let map: Record<string, string>;
  try { map = JSON.parse(value); if (!map || Array.isArray(map) || typeof map !== 'object') throw new Error(); }
  catch { return <Alert type='error' message={t('external.labels.invalid_levels')} />; }
  return <Space wrap>{['1', '2', '3'].map((key) => <span key={key} className='inline-flex gap-1 items-center'>
    {key} → <Select aria-label={`level ${key}`} value={map[key]} style={{ width: 80 }} options={levels.map((label) => ({ value: label, label }))} onChange={(next) => onChange?.(JSON.stringify({ ...map, [key]: next }))} />
  </span>)}</Space>;
}

export default function ExternalPush({ field }: { field: FormListFieldData }) {
  const { t } = useTranslation(NS);
  const form = Form.useFormInstance();
  const path = ['notify_configs', field.name, 'params'];
  const watched = Form.useWatch(path);
  const params: Record<string, string> = watched || form.getFieldValue(path) || {};
  const isNew = params.mapping_version === VERSION;
  const hasExisting = Object.keys(params).length > 0;
  const [editLegacy, setEditLegacy] = useState(false);
  const [companyMode, setCompanyMode] = useState(() => params.company_id_label ? 'label' : 'literal');
  const [eventID, setEventID] = useState<number | null>();
  const [preview, setPreview] = useState<Preview>();
  const [previewError, setPreviewError] = useState<string>();
  const [checking, setChecking] = useState(false);
  const generation = useRef(0);
  const signature = JSON.stringify(params);
  const setParam = (key: string, value: string | undefined) => form.setFields([{ name: [...path, key], value }]);
  const switchMapping = () => {
    const keep = Object.fromEntries(Object.entries(params).filter(([key]) => !['device_type', 'device_type_label', 'device_name_label', 'device_tags', 'device_tags_label', 'device_tags_labels', 'cmdb_match_field', 'cmdb_match_label'].includes(key)));
    form.setFields([{ name: path, value: { ...keep, mapping_version: VERSION, device_source: 'event', device_tags_source: 'event_labels', device_type_label: '', device_name_label: '', device_tags_labels: '[]', level_map: params.level_map || JSON.stringify(defaultLevels), repeat_policy: params.repeat_policy || 'all_dispatches' } }]);
    setEditLegacy(false);
  };
  useEffect(() => { if (!hasExisting) switchMapping(); }, [hasExisting]);
  useEffect(() => { generation.current++; setPreview(undefined); setPreviewError(undefined); setChecking(false); }, [signature, eventID]);
  useEffect(() => () => { generation.current++; }, []);

  if (editLegacy && !isNew) return <><Button className='mb-3' onClick={() => setEditLegacy(false)}>{t('external.labels.back_table')}</Button><Alert className='mb-3' type='info' message={t('external.labels.once_notice')} /><LegacyEditor field={field} /></>;
  const companyLabel = companyMode === 'label';
  const input = (key: string, target: string) => <Form.Item className='mb-0' name={[field.name, 'params', key]} rules={[{ required: true, whitespace: true }]}><Input aria-label={target} placeholder={t('external.labels.label_placeholder')} autoComplete='off' /></Form.Item>;
  const oldValue = (target: string) => {
    if (target === 'deviceType') return params.device_source === 'cmdb' ? 'CMDB.type' : params.device_type_label ? `tags[${params.device_type_label}]` : params.device_type;
    if (target === 'deviceName') return params.device_source === 'cmdb' ? 'CMDB.hostname' : `tags[${params.device_name_label || ''}]`;
    return params.device_tags_source === 'cmdb_model_business' ? '[CMDB.model, CMDB.business]' : params.device_tags_source === 'rule_append_tags' ? 'AlertRule.append_tags' : params.device_tags_label ? `tags[${params.device_tags_label}]` : params.device_tags;
  };
  const automatic = (key: string) => <Input readOnly aria-label={key} value={t(`external.labels.auto_values.${key}`)} />;
  const rows = [
    { key: 'alarmType', auto: true, mode: t('external.labels.auto'), editor: <Space><Tag>problem</Tag><Tag>ok</Tag></Space> },
    { key: 'companyId', mode: t('external.labels.company_mode'), editor: <div role='group' aria-label='companyId'>
      {params.company_id?.trim() && params.company_id_label?.trim() && <Alert type='error' message={t('external.source_conflict')} />}
      <Radio.Group className='mb-2' value={companyLabel ? 'label' : 'literal'} onChange={(e) => { const next = e.target.value; setCompanyMode(next); setParam(next === 'label' ? 'company_id' : 'company_id_label', undefined); if (next === 'label' && !params.company_id_label) setParam('company_id_label', ''); }}>
        <Radio value='literal'>{t('external.source_literal')}</Radio><Radio value='label'>{t('external.source_label')}</Radio>
      </Radio.Group>
      <Form.Item className='mb-0' name={[field.name, 'params', companyLabel ? 'company_id_label' : 'company_id']} rules={[{ required: true, whitespace: true }]}><Input aria-label={companyLabel ? 'company_id_label' : 'company_id'} /></Form.Item>
    </div> },
    { key: 'deviceType', mode: isNew ? t('external.source_label') : t('external.labels.legacy_source'), editor: isNew ? input('device_type_label', 'deviceType source') : <code>{oldValue('deviceType')}</code> },
    { key: 'deviceName', mode: isNew ? t('external.source_label') : t('external.labels.legacy_source'), editor: isNew ? input('device_name_label', 'deviceName source') : <code>{oldValue('deviceName')}</code> },
    { key: 'problemTime', auto: true, mode: t('external.labels.auto'), editor: automatic('problemTime') },
    { key: 'recoveryTime', auto: true, mode: t('external.labels.auto'), editor: automatic('recoveryTime') },
    { key: 'triggerName', auto: true, mode: t('external.labels.auto'), editor: automatic('triggerName') },
    { key: 'level', mode: t('external.labels.level_mode'), editor: <><p className='text-soft text-xs'>{t('external.labels.level_notice')}</p><Form.Item className='mb-0' name={[field.name, 'params', 'level_map']} rules={[{ required: true }, { validator: async (_, value) => { if (!isNew) return; let map; try { map = JSON.parse(value); } catch { throw Error(t('external.labels.invalid_levels')); } if (!map || ['1', '2', '3'].some((key) => !levels.includes(map[key]))) throw Error(t('external.labels.invalid_levels')); } }]}><LevelMap /></Form.Item></> },
    { key: 'deviceTags', mode: isNew ? t('external.labels.tags_mode') : t('external.labels.legacy_source'), editor: isNew ? <Form.Item className='mb-0' name={[field.name, 'params', 'device_tags_labels']} rules={[{ validator: async (_, value) => { const list = readLabels(value); if (!list?.length || list.length > 64 || list.some((x) => !x.label.trim() || x.label !== x.label.trim()) || new Set(list.map((x) => x.label)).size !== list.length) throw Error(t('external.labels.invalid_list')); } }]}><TagLabels /></Form.Item> : <code>{oldValue('deviceTags')}</code> },
    { key: 'sign', auto: true, mode: t('external.labels.auto'), editor: automatic('sign') },
  ];
  const visibleNames = new Set([companyLabel ? 'company_id_label' : 'company_id', 'level_map', ...(isNew ? ['device_type_label', 'device_name_label', 'device_tags_labels'] : [])]);
  // Preserve saved legacy parameters and source-version metadata when the form
  // submits. Loading never silently converts CMDB values to guessed label keys.
  return <section>
    {Object.keys(params).filter((key) => !visibleNames.has(key)).map((key) => <Form.Item hidden key={key} name={[field.name, 'params', key]}><Input /></Form.Item>)}
    {!isNew && <Alert className='mb-3' type='info' showIcon message={t('external.labels.legacy_notice')} action={<Space><Button size='small' onClick={switchMapping}>{t('external.labels.switch_mapping')}</Button><Button size='small' onClick={() => setEditLegacy(true)}>{t('external.labels.edit_legacy')}</Button></Space>} />}
    <Alert className='mb-3' type='info' showIcon message={t('external.labels.instructions')} description={t('external.labels.source_boundary')} />
    <Table size='small' pagination={false} rowKey='key' scroll={{ x: 1050 }} dataSource={rows} columns={[
      { title: t('external.labels.field'), width: 160, render: (_, row) => <><strong>{t(`external.labels.names.${row.key}`)}</strong><br /><code>{row.key}</code></> },
      { title: t('external.labels.mode'), width: 160, render: (_, row) => row.auto ? <Tag>{row.mode}</Tag> : row.mode },
      { title: t('external.labels.configuration'), width: 400, render: (_, row) => row.editor },
      { title: t('external.labels.rules'), render: (_, row) => <span className='text-soft'>{t(`external.labels.rules_text.${row.key}`)}</span> },
    ]} />
    <Alert className='my-3' type='info' message={t('external.labels.once_notice')} description={t('external.recovery_notice')} />
    {isNew && <div className='p-3 rounded fc-border mb-3'>
      <h4>{t('external.labels.preview_title')}</h4><p className='text-soft'>{t('external.labels.preview_notice')}</p>
      <Space><InputNumber aria-label={t('external.labels.event_id')} placeholder={t('external.labels.event_id')} min={1} precision={0} value={eventID} onChange={(id) => setEventID(typeof id === 'number' ? id : undefined)} />
        <Button disabled={!eventID} loading={checking} onClick={async () => {
          const current = ++generation.current; setChecking(true); setPreview(undefined); setPreviewError(undefined);
          try { const result = await request('/api/n9e/external-push-preview', { method: RequestMethod.Post, silence: true, data: { channel_id: form.getFieldValue(['notify_configs', field.name, 'channel_id']), event_id: eventID, params: form.getFieldValue(path) } }); if (current === generation.current) { if (result.err) setPreviewError(String(result.err)); else setPreview(result.dat); } }
          catch (error) { if (current === generation.current) setPreviewError(error instanceof Error ? error.message : t('external.labels.preview_failed')); }
          finally { if (current === generation.current) setChecking(false); }
        }}>{t('external.labels.check_mapping')}</Button></Space>
      {previewError && <Alert className='mt-3' type='warning' message={previewError} />}
      {preview && <div className='mt-3'>{preview.recovery_pending && <Alert type='info' message={t('external.labels.recovery_preview')} />}{!!preview.omitted_tags?.length && <Alert type='info' message={t('external.labels.omitted_title')} description={preview.omitted_tags.map((item) => `${item.label}: ${t(`external.labels.omitted_${item.reason}`)}`).join('；')} />}<pre aria-label={t('external.labels.preview_payload')} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{JSON.stringify(preview.payload, null, 2)}</pre></div>}
    </div>}
    <details><summary>{t('external.structure_preview')}</summary><ExternalPushFields params={params} /></details>
  </section>;
}
