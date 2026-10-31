import React, { useContext, useState } from 'react';
import { Alert, Button, Modal, Select, Space } from 'antd';
import { useTranslation } from 'react-i18next';
import { CommonStateContext } from '@/App';
import { ProfileOption, getDevice, getProfiles, saveDevice, setDeviceState } from '@/services/collection';
import type { Item } from '@/pages/hosts/types';
import { errorText } from './errors';

export default function BatchActions({ rows, onChanged }: { rows: Item[]; onChanged: (failed: Item[]) => void }) {
  const { t } = useTranslation('collection');
  const { profile, perms } = useContext(CommonStateContext);
  const [open, setOpen] = useState(false);
  const [action, setAction] = useState('disable');
  const [templates, setTemplates] = useState<ProfileOption[]>([]);
  const [template, setTemplate] = useState<number>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [finished, setFinished] = useState(false);
  const allSnmp = rows.length > 0 && rows.every((r) => !!r.collection);
  const canWrite = profile.admin || perms?.includes('/collection/write');
  const canDelete = profile.admin || perms?.includes('/collection/delete');
  return (
    <>
      <Button
        disabled={!allSnmp || (!canWrite && !canDelete)}
        onClick={() => {
          setError('');
          setFinished(false);
          setAction(canWrite ? 'disable' : 'delete');
          setTemplate(undefined);
          setOpen(true);
          getProfiles()
            .then(setTemplates)
            .catch((e) => setError(errorText(e, t('query_failed'))));
        }}
      >
        {t('snmp_batch')}
      </Button>
      <Modal
        visible={open}
        title={`${t('snmp_batch')} (${rows.length})`}
        onCancel={() => {
          if (!busy) setOpen(false);
        }}
        confirmLoading={busy}
        okText={t('batch_execute')}
        okButtonProps={{ disabled: finished || (action === 'template' && !template) }}
        onOk={async () => {
          setBusy(true);
          setError('');
          const failures: string[] = [];
          const failedRows: Item[] = [];
          for (const row of rows) {
            try {
              const d = await getDevice(row.collection!.device_id);
              if (action === 'template') {
                const fields = [
                  'name',
                  'address',
                  'port',
                  'ident',
                  'group_id',
                  'collector_id',
                  'credential_id',
                  'protocol',
                  'interval_seconds',
                  'timeout_seconds',
                  'retries',
                  'labels',
                  'cmdb_id',
                  'enabled',
                  'record_version',
                ] as const;
                const values = Object.fromEntries(fields.map((key) => [key, d[key]]));
                await saveDevice(d.id, { ...values, template_component_id: template, refresh_template: true }, '');
              } else {
                await setDeviceState(d, action as 'enable' | 'disable' | 'delete');
              }
            } catch (e) {
              failedRows.push(row);
              failures.push(`${row.note || row.ident}: ${errorText(e, t('query_failed'))}`);
            }
          }
          setBusy(false);
          setFinished(true);
          onChanged(failedRows);
          if (failures.length) setError(failures.join('\n'));
          else setOpen(false);
        }}
      >
        <Alert className='mb-3' type='info' message={t('batch_hint')} />
        {error && <Alert className='mb-3' type='error' message={<span style={{ whiteSpace: 'pre-wrap' }}>{error}</span>} />}
        <Space direction='vertical' style={{ width: '100%' }}>
          <Select
            style={{ width: '100%' }}
            aria-label={t('actions')}
            value={action}
            disabled={busy || finished}
            onChange={setAction}
            options={[
              ...(canWrite
                ? [
                    { value: 'enable', label: t('enable') },
                    { value: 'disable', label: t('disable') },
                    { value: 'template', label: t('batch_template') },
                  ]
                : []),
              ...(canDelete ? [{ value: 'delete', label: t('delete') }] : []),
            ]}
          />
          {action === 'template' && (
            <Select
              style={{ width: '100%' }}
              aria-label={t('template')}
              showSearch
              optionFilterProp='label'
              disabled={busy || finished}
              value={template}
              onChange={setTemplate}
              options={templates.map((tpl) => ({ value: tpl.component_id, label: tpl.name }))}
            />
          )}
          {action === 'delete' && <Alert type='warning' message={t('batch_delete_hint')} />}
          <div>{rows.map((r) => r.note || r.ident).join(', ')}</div>
        </Space>
      </Modal>
    </>
  );
}
