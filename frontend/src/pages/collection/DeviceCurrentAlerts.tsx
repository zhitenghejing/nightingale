import React, { useEffect, useState } from 'react';
import { Alert, Empty, Table, Tag } from 'antd';
import moment from 'moment';
import { useTranslation } from 'react-i18next';
import { DeviceAlert, DeviceSubject, getDeviceAlerts } from '@/services/collection';
import { errorText } from './errors';

export function DeviceSeverity({ severity }: { severity: number }) {
  const { t } = useTranslation('collection');
  return severity >= 1 && severity <= 3 ? <Tag color={severity === 1 ? 'red' : severity === 2 ? 'orange' : 'blue'}>{t(`device_severity_${severity}`)}</Tag> : <span>{t('device_rule_severity')}</span>;
}
export default function DeviceCurrentAlerts({ subject, active, refresh }: { subject: DeviceSubject; active: boolean; refresh: number }) {
  const { t } = useTranslation('collection');
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<{ list: DeviceAlert[]; total: number }>({ list: [], total: 0 });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => { setPage(1); setResult({ list: [], total: 0 }); setError(''); }, [subject.device_id, subject.ident]);
  useEffect(() => {
    if (!active) return;
    let mounted = true;
    const load = () => { setLoading(true); getDeviceAlerts(subject, page)
      .then((data) => { if (mounted) { setResult(data); setError(''); if ((page - 1) * 20 >= data.total && page > 1) setPage(Math.max(1, Math.ceil(data.total / 20))); } })
      .catch((e) => { if (mounted) setError(errorText(e, t('query_failed'))); })
      .finally(() => { if (mounted) setLoading(false); }); };
    load(); const timer = window.setInterval(load, 10000);
    return () => { mounted = false; window.clearInterval(timer); };
  }, [subject.device_id, subject.ident, active, page, refresh]);
  return <>
    {error && <Alert type='error' showIcon message={error} />}
    <Table<DeviceAlert> className='device-current-alerts' rowKey='id' size='small' loading={loading} dataSource={error ? [] : result.list}
      locale={{ emptyText: <Empty description={error ? t('query_failed') : t('device_no_current_alerts')} /> }}
      pagination={{ current: page, pageSize: 20, total: error ? 0 : result.total, onChange: setPage, showSizeChanger: false }}
      columns={[
        { title: t('device_alert_severity'), dataIndex: 'severity', width: 100, render: (severity) => <DeviceSeverity severity={severity} /> },
        { title: t('device_alert_rule'), dataIndex: 'rule_name' },
        { title: t('device_alert_time'), dataIndex: 'trigger_time', width: 200, render: (value) => value ? moment.unix(value).format('YYYY-MM-DD HH:mm:ss') : '—' },
        { title: t('device_alert_value'), dataIndex: 'trigger_value' },
      ]} />
  </>;
}
