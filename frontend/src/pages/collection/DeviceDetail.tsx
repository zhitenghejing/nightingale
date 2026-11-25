import React, { useContext, useEffect, useState } from 'react';
import { Alert, Button, Descriptions, Drawer, Empty, Modal, Select, Space, Spin, Table, Tabs, Tag } from 'antd';
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
import LinuxDashboard, { supportsLinuxDashboard } from './LinuxDashboard';
import useLinuxAssetSummary, { assetNumber, assetBytes, assetMemoryCapacity } from './useLinuxAssetSummary';

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
  const [dashboardType, setDashboardType] = useState<string>();
  const subject = id ? { device_id: id } : { ident: target?.ident };
  const load = () => getDeviceDetail(subject).then((value) => { setData(value); setError(''); }).catch((e) => setError(errorText(e, t('query_failed'))));
  useEffect(() => {
    let active = true;
    setData(undefined); setError(''); setTab('dashboard'); setEdit(false); setDashboardType(undefined);
    const fetch = () => getDeviceDetail(subject).then((value) => { if (active) { setData(value); setError(''); } }).catch((e) => { if (active) setError(errorText(e, t('query_failed'))); });
    fetch(); const timer = window.setInterval(fetch, 10000);
    return () => { active = false; window.clearInterval(timer); };
  }, [id, target?.ident]);
  const device = data?.device;
  const inventory = data?.target.inventory;
  const selectedDashboardType = dashboardType || (data && supportsLinuxDashboard(data) ? 'linux' : 'template');
  const linuxSelected = selectedDashboardType === 'linux';
  const mergedLinuxInfo = !!data && (supportsLinuxDashboard(data) || linuxSelected);
  const asset = useLinuxAssetSummary(data, mergedLinuxInfo, refresh);
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
  const deviceInfo = data ? <>
      <Space className='mb-3'>
        <Button onClick={() => { load(); setRefresh((value) => value + 1); }}>{t('refresh')}</Button>
        {canWrite && !device?.deleted && (device || onEditNative) && <Button onClick={() => device ? setEdit(true) : onEditNative?.()}>{t('edit')}</Button>}
        {canWrite && device && !device.deleted && <Button loading={busy} onClick={() => act(device.enabled ? 'disable' : 'enable')}>{t(device.enabled ? 'disable' : 'enable')}</Button>}
        {canDelete && device && !device.deleted && <Button danger loading={busy} onClick={() => Modal.confirm({ title: t('confirm_delete'), onOk: () => act('delete') })}>{t('delete')}</Button>}
      </Space>
      <Descriptions className='device-detail-summary' bordered column={2} size='small'>
        {mergedLinuxInfo && <Descriptions.Item label='设备名称'>{name}</Descriptions.Item>}
        {mergedLinuxInfo && <Descriptions.Item label='配置信息'>CPU {assetNumber(asset.cores ?? (data.target.cpu_num || undefined), ' 核')} · 内存 <span title={asset.memoryBytes === undefined ? undefined : `采集容量：${assetBytes(asset.memoryBytes)}；配置容量按 1024 进制向上取整估算，并非安装容量的直接采集值。`}>{assetMemoryCapacity(asset.memoryBytes)}</span></Descriptions.Item>}
        <Descriptions.Item label={mergedLinuxInfo ? '稳定标识' : t('ident')}>{data.target.ident}</Descriptions.Item>
        <Descriptions.Item label={t('device_ip')}>{device ? `${device.address}:${device.port}` : data.target.host_ip || '—'}</Descriptions.Item>
        <Descriptions.Item label={t('device_type')}>{inventory?.device_type || t('device_unclassified')}</Descriptions.Item>
        <Descriptions.Item label={t('device_model')}>{inventory?.device_model || data.target.os || '—'}</Descriptions.Item>
        <Descriptions.Item label={t('health')}><Health state={device?.collection.health || (inventory?.collection_ok ? 'healthy' : 'unknown')} /></Descriptions.Item>
        <Descriptions.Item label={t('phase')}>{device ? t(device.collection.configuration_state) : t('device_native_collection')}</Descriptions.Item>
        <Descriptions.Item label={t('device_method')}><Tag color={inventory?.collection_ok ? 'green' : 'default'}>{(inventory?.method || 'agent').toUpperCase()}</Tag></Descriptions.Item>
        <Descriptions.Item label={t('device_reachability')}><Tag color={inventory?.reachability === 'up' ? 'green' : inventory?.reachability === 'down' ? 'red' : undefined}>{t(`hosts:inventory.reachability_${inventory?.reachability || 'unknown'}`)}</Tag></Descriptions.Item>
        <Descriptions.Item label={t('collector')}>{data.collector_name || device?.collector_id || '—'}</Descriptions.Item>
        <Descriptions.Item label={t('last_sample')}>{lastSample ? moment.unix(lastSample).format('YYYY-MM-DD HH:mm:ss') : t('no_sample')}</Descriptions.Item>
        {mergedLinuxInfo && <Descriptions.Item label='所属业务'>{data.target.group_objs?.map((group) => group.name).join('、') || '—'}</Descriptions.Item>}
        {mergedLinuxInfo && <Descriptions.Item label='系统描述'>{data.target.os || data.target.note || '—'}</Descriptions.Item>}
        {mergedLinuxInfo && <Descriptions.Item label='运行时间'>{assetNumber(asset.uptimeSeconds === undefined ? undefined : Math.round(asset.uptimeSeconds / 86400), ' 天')}</Descriptions.Item>}
        {mergedLinuxInfo && <Descriptions.Item label='标签'>{device ? Object.keys(device.labels || {}).length ? Object.entries(device.labels).map(([key, value]) => <Tag key={key}>{key}={value}</Tag>) : '—' : data.target.tags?.join('、') || '—'}</Descriptions.Item>}
        {device?.profile?.catalog && <Descriptions.Item label={t('device_template_version')} span={2}>{device.profile.name} · v{device.profile.catalog.version} · {device.profile.catalog.category_path.join(' → ')}</Descriptions.Item>}
      </Descriptions>
      {mergedLinuxInfo && asset.error && <Alert className='mt-2' type='warning' showIcon message={asset.error} />}
      {device?.collection.counter_bits === 32 && <Alert className='mt-2' type='warning' message={t('counter_32')} />}
  </> : null;
  return <Drawer visible width='85%' className='device-detail' title={name} onClose={onClose} destroyOnClose>
    {error && <Alert type='error' showIcon message={error} />}
    {!data ? !error && <Spin /> : <>
      {!linuxSelected && deviceInfo}
      <Tabs className='device-detail-tabs mt-3' activeKey={tab} onChange={setTab}>
        <Tabs.TabPane key='dashboard' tab={t('profile_dashboard')}>
          <Space className='mb-3'>仪表盘类型<Select aria-label='仪表盘类型' value={selectedDashboardType} onChange={setDashboardType} style={{ width: 200 }} options={[{ value: 'template', label: '模板仪表盘' }, { value: 'linux', label: 'Linux 系统仪表盘' }]} /></Space>
          {linuxSelected ? <LinuxDashboard key={id || target?.ident} data={data} subject={subject} refresh={refresh} assetInfo={deviceInfo} /> : device?.profile?.boards.length ? device.profile.boards.map((board) => <div key={board.id}>
            <a href={`/dashboards/${board.id}?__refresh=10`} target='_blank' rel='noreferrer'>{t('profile_dashboard_edit')}: {board.name}</a>
            <iframe key={`${board.id}:${device.collection.revision}:${refresh}`} title={board.name} src={`/dashboards/${board.id}?viewMode=fullscreen&__readonly=true&__show_header=true&__refresh=10`} style={{ width: '100%', height: 920, border: 0 }} />
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
