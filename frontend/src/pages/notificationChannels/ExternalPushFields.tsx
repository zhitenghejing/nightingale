import React from 'react';
import { Alert, Table } from 'antd';
import { useTranslation } from 'react-i18next';
import { NS } from './constants';

const fields = ['alarmType', 'companyId', 'deviceType', 'deviceName', 'problemTime', 'recoveryTime', 'triggerName', 'level', 'deviceTags', 'sign'];

// This is a source-expression guide, never a request template or send input.
// The authoritative, validated JSON remains the durable worker payload.
export default function ExternalPushFields({ params = {}, showRuleLink = false }: { params?: Record<string, string>; showRuleLink?: boolean }) {
  const { t } = useTranslation(NS);
  const mapped = (key: string) => (params[key + '_label'] ? `tags[${JSON.stringify(params[key + '_label'])}]` : params[key] || t('external.choose_rule_source'));
  const joinedTags = () => {
    try { const list = JSON.parse(params.device_tags_labels || '[]'); return list.map((item: { label: string; required: boolean }) => `tags[${JSON.stringify(item.label)}] (${item.required ? 'required' : 'optional'})`).join(' + "," + '); }
    catch { return t('external.labels.invalid_list'); }
  };
  const structure = {
    alarmType: 'IsRecovered ? "ok" : "problem"',
    companyId: mapped('company_id'),
    deviceType: params.device_source === 'cmdb' ? 'CMDB.type (snapshot)' : mapped('device_type'),
    deviceName: params.device_source === 'cmdb' ? 'CMDB.hostname (snapshot)' : `tags[${JSON.stringify(params.device_name_label || t('external.choose_label'))}]`,
    problemTime: 'FirstTriggerTime → YYYY-MM-DD HH:mm:ss',
    recoveryTime: 'IsRecovered ? RecoverTime → YYYY-MM-DD HH:mm:ss : ""',
    triggerName: 'RuleName',
    level: `level_map[Severity]${params.level_map ? ' · ' + params.level_map : ''}`,
    deviceTags: params.device_tags_source === 'event_labels' ? joinedTags() : params.device_tags_source === 'cmdb_model_business' ? "[CMDB.model, CMDB.business] (single-quoted list text, snapshot)" : params.device_tags_source === 'rule_append_tags' ? 'AlertRule.append_tags (saved field string, snapshot)' : mapped('device_tags'),
    sign: t('external.sign_pending'),
  };
  return (
    <section className='my-4'>
      <h3>{t('external.mapping_title')}</h3>
      <Alert className='mb-3' type='info' message={t('external.mapping_location')} description={t('external.mapping_notice')} />
      {showRuleLink && (
        <p>
          <a href='/notification-rules'>{t('external.open_rules')}</a>
        </p>
      )}
      <Table
        rowKey='field'
        size='small'
        pagination={false}
        dataSource={fields.map((field) => ({ field }))}
        columns={[
          { title: t('external.json_field'), dataIndex: 'field', width: 130, render: (value) => <code>{value}</code> },
          { title: t('external.field_source'), dataIndex: 'field', render: (value) => t(`external.sources.${value}`) },
        ]}
      />
      <h4 className='mt-3'>{t('external.structure_preview')}</h4>
      <pre aria-label={t('external.structure_preview')} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
        {JSON.stringify(structure, null, 2)}
      </pre>
    </section>
  );
}
