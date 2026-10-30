import React, { useEffect, useRef, useState } from 'react';
import { Alert, Button, Form, Input, InputNumber, Select } from 'antd';
import { useTranslation } from 'react-i18next';
import request from '@/utils/request';
import { RequestMethod } from '@/store/common';
import { NS } from '../../constants';
import ExternalPushFields from '../../ExternalPushFields';

const names = ['request_config', 'external_push_config'];
function FileKeyStatus() {
  const { t } = useTranslation(NS);
  const reference = Form.useWatch([...names, 'signing_key_ref']) || 'platform10-v1';
  const [status, setStatus] = useState<{ ready: boolean; reason: string }>();
  const [busy, setBusy] = useState(false);
  const latestRequest = useRef(0);
  useEffect(() => {
    setStatus(undefined);
    setBusy(false);
    return () => { latestRequest.current++; };
  }, [reference]);
  return (
    <>
      <Alert className='mb-3' type='info' message={t('external.key_file_notice')} />
      <Form.Item name={[...names, 'signing_key_ref']} label={t('external.signing_key_ref')} rules={[{ required: true, whitespace: true }]}>
        <Input autoComplete='off' />
      </Form.Item>
      <Button
        className='mb-3'
        loading={busy}
        onClick={async () => {
          const requestID = ++latestRequest.current;
          setBusy(true);
          try {
            const result = await request('/api/n9e/external-push-signing-key', { method: RequestMethod.Get, params: { reference } });
            if (latestRequest.current === requestID) {
              setStatus(result.dat.reference === reference ? result.dat : { ready: false, reason: t('external.key_check_failed') });
            }
          } catch {
            if (latestRequest.current === requestID) setStatus({ ready: false, reason: t('external.key_check_failed') });
          } finally {
            if (latestRequest.current === requestID) setBusy(false);
          }
        }}
      >
        {t('external.check_key')}
      </Button>
      {status && (
        <Alert className='mb-3' type={status.ready ? 'success' : 'warning'} message={t(status.ready ? 'external.key_ready' : 'external.key_unready')} description={status.reason} />
      )}
    </>
  );
}
export default function ExternalPush() {
  const { t } = useTranslation(NS);
  const form = Form.useFormInstance();
  const requestType = Form.useWatch('request_type');
  const signingMode = Form.useWatch([...names, 'signing_mode']) || 'test_value';
  const mode = Form.useWatch([...names, 'mode']) || 'simulation';
  if (requestType !== 'external_push') return null;
  return (
    <>
      <Alert className='mb-4' type='warning' showIcon message={t(mode === 'platform10' ? 'external.platform10_notice' : 'external.simulation_notice')} description={t('external.recovery_notice')} />
      <Alert className='mb-3' type='info' message={t('external.transport_notice')} />
      <Form.Item name={[...names, 'mode']} label={t('external.mode')} rules={[{ required: true }]}>
        <Select
          options={['platform10', 'simulation'].map((value) => ({ value, label: t('external.' + value) }))}
          onChange={(value) => {
            form.setFields([{ name: [...names, 'endpoint'], value: value === 'platform10' ? 'https://admin.ztservice.cn/messageWarn!acceptWarn.act' : '' }]);
            if (value === 'platform10') form.setFields([
              { name: [...names, 'signing_mode'], value: 'key_file' },
              { name: [...names, 'signing_key_ref'], value: form.getFieldValue([...names, 'signing_key_ref']) || 'platform10-v1' },
              { name: [...names, 'test_sign'], value: '' },
            ]);
          }}
        />
      </Form.Item>
      {['endpoint', 'destination', 'problem_type', 'recovery_type', 'time_zone'].map((key) => (
        <Form.Item key={key} name={[...names, key]} label={t(`external.${key}`)} rules={[{ required: true, whitespace: true }]}>
          <Input autoComplete='off' readOnly={key === 'problem_type' || key === 'recovery_type' || (key === 'endpoint' && mode === 'platform10')} />
        </Form.Item>
      ))}
      <Form.Item name={[...names, 'signing_mode']} label={t('external.signing_mode')} getValueProps={(value) => ({ value: value || 'test_value' })}>
        <Select
          options={(mode === 'platform10' ? ['key_file'] : ['key_file', 'test_value']).map((value) => ({ value, label: t('external.signing_modes.' + value) }))}
          onChange={(value) => {
            if (value === 'key_file')
              form.setFields([
                { name: [...names, 'test_sign'], value: '' },
                { name: [...names, 'signing_key_ref'], value: form.getFieldValue([...names, 'signing_key_ref']) || 'platform10-v1' },
              ]);
            else form.setFields([{ name: [...names, 'signing_key_ref'], value: undefined }]);
          }}
        />
      </Form.Item>
      {signingMode === 'key_file' ? (
        <FileKeyStatus />
      ) : (
        <Form.Item name={[...names, 'test_sign']} label={t('external.test_sign')} rules={[{ required: true, whitespace: true }]}>
          <Input autoComplete='off' />
        </Form.Item>
      )}
      <Form.Item name={[...names, 'timeout_ms']} label={t('external.timeout')} rules={[{ required: true }]}>
        <InputNumber min={1} max={10000} />
      </Form.Item>
      <Alert className='mb-4' type='info' message={t('external.protocol_notice')} />
      <ExternalPushFields showRuleLink />
    </>
  );
}
