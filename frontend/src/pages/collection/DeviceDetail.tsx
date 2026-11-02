import React, { useContext, useEffect, useState } from 'react';
import { Alert, Button, Descriptions, Drawer, Empty, Modal, Space, Spin, Table, Tabs, Tag } from 'antd';
import { useTranslation } from 'react-i18next';
import moment from 'moment';
import { CommonStateContext } from '@/App';
import type { Item } from '@/pages/hosts/types';
import { DeviceDetailData, getDeviceDetail, setDeviceState } from '@/services/collection';
import { errorText } from './errors';
import DeviceEditor from './DeviceEditor';
import DeviceMetricCharts from './DeviceMetricCharts';
import DeviceCurrentAlerts, { DeviceSeverity } from './DeviceCurrentAlerts';
import { Health } from './Health';

interface Props { id?: string; target?: Item; onClose: () => void; onChanged: () => void; editable?: boolean; onEditNative?: () => void; }
export default function DeviceDetail({ id, target, onClose, onChanged, editable = true, onEditNative }: Props) {
  const { t } = useTranslation('collection');
  const { profile, perms } = useContext(CommonStateContext);
  const [data, setData] = useState<DeviceDetailData>();
  const [error, setError] = useState('');
  const [edit, setEdit] = useState(false);
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [tab, setTab] = useState('dashboard');
  const subject = id ? { device_id: id } : { ident: target?.ident };
  const load = () => getDeviceDetail(subject).then((value) => { setData(value); setError(''); }).catch((e) => setError(errorText(e, t('query_failed'))));
  useEffect(() => {
    let active = true;
    setData(undefined); setError(''); setTab('dashboard'); setEdit(false);
    const fetch = () => getDeviceDetail(subject).then((value) => { if (active) { setData(value); setError(''); } }).catch((e) => { if (active) setError(errorText(e, t('query_failed'))); });
    fetch(); const timer = window.setInterval(fetch, 10000);
    return () => { active = false; window.clearInterval(timer); };
  }, [id, target?.ident]);
  const device = data?.device;
  const inventory = data?.target.inventory;
  const can = (permission: string) => editable && (profile.admin || perms?.includes(permission));
  const canWrite = can(device ? '/collection/write' : '/targets/put');
  const canDelete = !!device && can('/collection/delete');
  const act = async (action: 'enable' | 'disable' | 'delete') => {
    if (!device) return;
    setBusy(true); setError('');
    try { await setDeviceState(device, action); onChanged(); await load(); setRefresh((value) => value + 1); }
    catch (e) { setError(errorText(e, t('query_failed'))); }
    finally { setBusy(false); }
  };
  const name = inventory?.name || device?.name || target?.inventory?.name || target?.ident || t('device_details');
  const lastSample = inventory?.last_sample_at || device?.collection.last_sample_at || 0;
  return <Drawer visible width='85%' className='device-detail' title={name} onClose={onClose} destroyOnClose>
    {error && <Alert type='error' showIcon message={error} />}
    {!data ? !error && <Spin /> : <>
      <Space className='mb-3'>
        <Button onClick={() => { load(); setRefresh((value) => value + 1); }}>{t('refresh')}</Button>
        {canWrite && !device?.deleted && (device || onEditNative) && <Button onClick={() => device ? setEdit(true) : onEditNative?.()}>{t('edit')}</Button>}
        {canWrite && device && !device.deleted && <Button loading={busy} onClick={() => act(device.enabled ? 'disable' : 'enable')}>{t(device.enabled ? 'disable' : 'enable')}</Button>}
        {canDelete && device && !device.deleted && <Button danger loading={busy} onClick={() => Modal.confirm({ title: t('confirm_delete'), onOk: () => act('delete') })}>{t('delete')}</Button>}
      </Space>
      <Descriptions className='device-detail-summary' bordered column={2} size='small'>
        <Descriptions.Item label={t('ident')}>{data.target.ident}</Descriptions.Item>
        <Descriptions.Item label={t('device_ip')}>{device ? `${device.address}:${device.port}` : data.target.host_ip || '—'}</Descriptions.Item>
        <Descriptions.Item label={t('device_type')}>{inventory?.device_type || t('device_unclassified')}</Descriptions.Item>
        <Descriptions.Item label={t('device_model')}>{inventory?.device_model || data.target.os || '—'}</Descriptions.Item>
        <Descriptions.Item label={t('health')}><Health state={device?.collection.health || (inventory?.collection_ok ? 'healthy' : 'unknown')} /></Descriptions.Item>
        <Descriptions.Item label={t('phase')}>{device ? t(device.collection.configuration_state) : t('device_native_collection')}</Descriptions.Item>
        <Descriptions.Item label={t('device_method')}><Tag color={inventory?.collection_ok ? 'green' : 'default'}>{inventory?.method || 'agent'}</Tag></Descriptions.Item>
        <Descriptions.Item label={t('device_reachability')}><Tag color={inventory?.reachability === 'up' ? 'green' : inventory?.reachability === 'down' ? 'red' : undefined}>{t(`hosts:inventory.reachability_${inventory?.reachability || 'unknown'}`)}</Tag></Descriptions.Item>
        <Descriptions.Item label={t('collector')}>{data.collector_name || device?.collector_id || '—'}</Descriptions.Item>
        <Descriptions.Item label={t('last_sample')}>{lastSample ? moment.unix(lastSample).format('YYYY-MM-DD HH:mm:ss') : t('no_sample')}</Descriptions.Item>
        {device?.profile?.catalog && <Descriptions.Item label={t('device_template_version')} span={2}>{device.profile.name} · v{device.profile.catalog.version} · {device.profile.catalog.category_path.join(' → ')}</Descriptions.Item>}
      </Descriptions>
      {device?.collection.counter_bits === 32 && <Alert className='mt-2' type='warning' message={t('counter_32')} />}
      <Tabs className='device-detail-tabs mt-3' activeKey={tab} onChange={setTab}>
        <Tabs.TabPane key='dashboard' tab={t('profile_dashboard')}>
          {device?.profile?.boards.length ? device.profile.boards.map((board) => <div key={board.id}>
            <a href={`/dashboards/${board.id}`} target='_blank' rel='noreferrer'>{t('profile_dashboard_edit')}: {board.name}</a>
            <iframe title={board.name} src={`/dashboards/${board.id}?viewMode=fullscreen&__readonly=true&__show_header=true`} style={{ width: '100%', height: 920, border: 0 }} />
          </div>) : <DeviceMetricCharts subject={subject} revision={device?.collection.revision} refresh={refresh} />}
        </Tabs.TabPane>
        <Tabs.TabPane key='current-alerts' tab={t('device_current_alerts')}><DeviceCurrentAlerts subject={subject} active={tab === 'current-alerts'} refresh={refresh} /></Tabs.TabPane>
        <Tabs.TabPane key='metrics' tab={t('profile_metrics')}>
          <Table className='device-detail-metrics' rowKey='key' dataSource={data.metrics.map((metric, index) => ({ ...metric, key: index }))} pagination={false} size='small' locale={{ emptyText: <Empty description={t('device_no_metrics')} /> }} columns={[
            { title: t('profile_metric_name'), dataIndex: 'name', render: (value) => t(value, { defaultValue: value }) },
            { title: t('profile_expression'), dataIndex: 'expression' }, { title: t('profile_unit'), dataIndex: 'unit' }, { title: t('profile_note'), dataIndex: 'note' },
          ]} />
        </Tabs.TabPane>
        <Tabs.TabPane key='alerts' tab={t('profile_alerts')}>
          <Alert className='mb-3' type='info' message={t('device_rule_hint')} />
          <Table className='device-detail-rules' rowKey='id' dataSource={data.rules} pagination={false} size='small' locale={{ emptyText: <Empty description={t('device_no_rules')} /> }} columns={[
            { title: t('device_alert_rule'), dataIndex: 'name', render: (value, rule) => <a href={`/alert-rules/edit/${rule.id}`} target='_blank' rel='noreferrer'>{value}</a> },
            { title: t('device_rule_state'), dataIndex: 'disabled', width: 120, render: (value) => <Tag color={value ? 'orange' : 'green'}>{t(value ? 'disable' : 'enable')}</Tag> },
            { title: t('device_alert_severity'), dataIndex: 'severity', width: 120, render: (value) => <DeviceSeverity severity={value} /> },
            { title: t('device_rule_source'), dataIndex: 'source', width: 160, render: (value) => t(`device_rule_source_${value}`) },
          ]} />
        </Tabs.TabPane>
      </Tabs>
      {edit && device && <DeviceEditor device={device} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); onChanged(); load(); setRefresh((value) => value + 1); }} />}
    </>}
  </Drawer>;
}
