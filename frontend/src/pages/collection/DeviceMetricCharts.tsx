import React, { useEffect, useState } from 'react';
import { Alert, Card, Empty, Spin } from 'antd';
import { Line } from '@ant-design/plots';
import { useTranslation } from 'react-i18next';
import { collectionRequest, DeviceSubject, getAgentDeviceMetrics } from '@/services/collection';
import { errorText } from './errors';
import { formatAxisTime } from './time';

const order = ['cpu', 'memory', 'disk', 'network_in', 'network_out'];
export default function DeviceMetricCharts({ subject, revision, refresh }: { subject: DeviceSubject; revision?: string; refresh: number }) {
  const { t } = useTranslation('collection');
  const [metrics, setMetrics] = useState<Record<string, any[]>>();
  const [failed, setFailed] = useState<Record<string, boolean>>({});
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    setMetrics(undefined); setError(''); setFailed({});
    const load = () => {
      const query = subject.device_id ? collectionRequest(`devices/${subject.device_id}/metrics`) : getAgentDeviceMetrics(subject);
      query.then((result) => { if (active) { setMetrics(result.metrics); setFailed(result.metric_errors || {}); setError(''); } })
        .catch((e) => { if (active) setError(errorText(e, t('query_failed'))); });
    };
    load(); const timer = window.setInterval(load, 30000);
    return () => { active = false; window.clearInterval(timer); };
  }, [subject.device_id, subject.ident, revision, refresh]);
  const keys = metrics ? [...order.filter((key) => key in metrics), ...Object.keys(metrics).filter((key) => !order.includes(key)).sort()] : order;
  return <>
    {error && <Alert type='error' showIcon message={error} />}
    {metrics && !keys.length && <Empty description={t('no_data')} />}
    <div className='device-detail-metric-grid' style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 16 }}>
      {keys.map((key) => {
        const data = (metrics?.[key] || []).flatMap((series, seriesIndex) => {
          const labels = Object.entries(series.metric || {}).filter(([name]) => !['__name__', 'ident', 'collection_device_id', 'collection_collector_id', 'collection_revision', 'collection_mode', 'collection_template_version'].includes(name));
          const index = labels.map(([name, value]) => `${name}=${value}`).join(', ') || `${t('index')} ${seriesIndex + 1}`;
          return (series.values || []).map(([timestamp, value]) => ({ time: Number(timestamp) * 1000, value: Number(value), index }));
        }).filter((point) => Number.isFinite(point.value) && Number.isFinite(point.time));
        return <Card key={key} title={t(key, { defaultValue: key })}>
          {failed[key] && data.length > 0 && <Alert type='warning' message={t('query_failed')} />}
          {!metrics && !error ? <Spin /> : data.length ? <Line height={220} data={data} xField='time' yField='value' seriesField='index' xAxis={{ type: 'time', label: { formatter: formatAxisTime } }} animation={false} /> : failed[key] ? <Alert type='error' message={t('query_failed')} /> : <Empty description={t('no_data')} />}
        </Card>;
      })}
    </div>
  </>;
}
