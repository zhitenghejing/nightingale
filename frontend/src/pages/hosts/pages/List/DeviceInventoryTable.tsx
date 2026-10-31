import React, { lazy, Suspense, useContext, useState } from 'react';
import { Alert, Button, message, Modal, Space, Spin, Table, Tag, Tooltip } from 'antd';
import type { ColumnType, TableProps } from 'antd/es/table';
import { useTranslation } from 'react-i18next';
import { CommonStateContext } from '@/App';
import { Device, getDevice, setDeviceState } from '@/services/collection';
import { deleteTargets } from '@/services/targets';
import TargetMetaDrawer from '@/pages/targets/TargetMetaDrawer';
import { errorText } from '@/pages/collection/errors';
import { NS } from '../../constants';
import { Item, OperateType } from '../../types';
import Tags from './Tags';
import OperationModal from './OperationModal';
import ResizableHeaderCell, { clampColumnWidth } from './ResizableHeaderCell';
import './deviceInventory.less';

const DeviceEditor = lazy(() => import('@/pages/collection/DeviceEditor'));
const DeviceDetail = lazy(() => import('@/pages/collection/DeviceDetail'));
export const COLUMN_WIDTHS_KEY = 'hosts-device-inventory-columns-v1';
const defaultWidths = { name: 210, type: 135, model: 210, ip: 170, method: 145, reachability: 135, alerts: 210, groups: 170, tags: 250 };
type ColumnKey = keyof typeof defaultWidths;
function readWidths() {
  try {
    const saved = JSON.parse(localStorage.getItem(COLUMN_WIDTHS_KEY) || '{}');
    return Object.fromEntries(Object.entries(defaultWidths).map(([key, value]) => [key, typeof saved[key] === 'number' && Number.isFinite(saved[key]) ? clampColumnWidth(saved[key]) : value])) as typeof defaultWidths;
  } catch {
    return defaultWidths;
  }
}
export const deviceName = (row: Item) => row.inventory?.name || (row.collection ? row.note : '') || row.ident;

interface Props {
  tableProps: TableProps<Item>;
  selectedRows: Item[];
  setSelectedRows: (rows: Item[]) => void;
  onChanged: () => void;
  onTagClick: (tag: string) => void;
  editable: boolean;
}

export default function DeviceInventoryTable({ tableProps, selectedRows, setSelectedRows, onChanged, onTagClick, editable }: Props) {
  const { t } = useTranslation(NS);
  const { profile, perms } = useContext(CommonStateContext);
  const [widths, setWidths] = useState(readWidths);
  const [editing, setEditing] = useState<Device>();
  const [detail, setDetail] = useState<Item>();
  const [agent, setAgent] = useState<Item>();
  const [agentRevision, setAgentRevision] = useState(0);
  const [operation, setOperation] = useState(OperateType.None);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const can = (permission: string) => editable && (!!profile?.admin || !!perms?.includes(permission));
  const canEdit = (row: Item) => can(row.collection ? '/collection/write' : '/targets/put');
  const canDelete = (row: Item) => can(row.collection ? '/collection/delete' : '/targets/del');
  const edit = async (row: Item) => {
    if (!canEdit(row)) return;
    setError('');
    if (!row.collection) { setAgent(row); return; }
    setBusy(row.ident);
    try { setEditing(await getDevice(row.collection.device_id)); }
    catch (e) { setError(errorText(e, t('inventory.load_failed'))); }
    finally { setBusy(''); }
  };
  const remove = async (row: Item) => {
    if (!canDelete(row)) return;
    setError('');
    setBusy(row.ident);
    try {
      const device = row.collection ? await getDevice(row.collection.device_id) : undefined;
      Modal.confirm({
        title: t('inventory.delete_confirm'),
        content: `${device?.name || deviceName(row)} · ${row.host_ip || row.ident}`,
        okText: t('inventory.delete'), cancelText: t('inventory.cancel'), okButtonProps: { danger: true },
        onOk: async () => {
          try {
            if (device) await setDeviceState(device, 'delete');
            else {
              const result = await deleteTargets({ idents: [row.ident] });
              if (result?.dat && Object.keys(result.dat).length) throw new Error(Object.values(result.dat).join('; '));
            }
            setSelectedRows(selectedRows.filter((selected) => selected.ident !== row.ident));
            onChanged();
          } catch (e) { message.error(errorText(e, t('inventory.delete_failed'))); throw e; }
        },
      });
    } catch (e) { setError(errorText(e, t('inventory.load_failed'))); }
    finally { setBusy(''); }
  };
  const column = (key: ColumnKey, title: string, render: ColumnType<Item>['render']): ColumnType<Item> => ({
    key, title, width: widths[key], ellipsis: true, render,
    onHeaderCell: () => ({
      width: widths[key], resizeLabel: t('inventory.resize', { column: title }),
      onColumnResize: (width: number) => setWidths((previous) => {
        const next = { ...previous, [key]: width };
        try { localStorage.setItem(COLUMN_WIDTHS_KEY, JSON.stringify(next)); } catch { /* Layout remains usable without browser storage. */ }
        return next;
      }),
    }) as React.ThHTMLAttributes<HTMLTableCellElement>,
  });
  const columns: ColumnType<Item>[] = [
    column('name', t('inventory.name'), (_, row) => (
      <Tooltip title={row.ident}>
        <Button className='device-inventory-name' type='link' onClick={() => setDetail(row)}>{deviceName(row)}</Button>
      </Tooltip>
    )),
    column('type', t('inventory.type'), (_, row) => <span>{row.inventory?.device_type || t('inventory.unclassified')}</span>),
    column('model', t('inventory.model'), (_, row) => <span>{row.inventory?.device_model || row.os || '—'}</span>),
    column('ip', t('inventory.ip'), (_, row) => <span className='device-inventory-ip'>{row.host_ip || '—'}</span>),
    column('method', t('inventory.method'), (_, row) => {
      const method = row.inventory?.method || row.collection?.kind || 'agent';
      return <Tag color={row.inventory?.collection_ok ? 'green' : 'default'} className={`device-inventory-method${row.inventory?.collection_ok ? ' is-success' : ''}`}>{method}</Tag>;
    }),
    column('reachability', t('inventory.reachability'), (_, row) => {
      const state = row.inventory?.reachability || 'unknown';
      const color = state === 'up' ? 'green' : state === 'down' ? 'red' : state === 'error' ? 'orange' : undefined;
      return <Tooltip title={t(`inventory.reachability_${state}_tip`)}><Tag color={color}>{t(`inventory.reachability_${state}`)}</Tag></Tooltip>;
    }),
    column('alerts', t('inventory.alerts'), (_, row) => {
      const counts = row.inventory?.current_alerts;
      if (!counts) return <span className='text-secondary'>{t('inventory.unavailable')}</span>;
      return <Space size={3} wrap className='device-inventory-alerts'>
        {!!counts.critical && <Tag color='red'>{t('inventory.critical')} {counts.critical}</Tag>}
        {!!counts.warning && <Tag color='orange'>{t('inventory.warning')} {counts.warning}</Tag>}
        {!!counts.info && <Tag color='blue'>{t('inventory.info')} {counts.info}</Tag>}
      </Space>;
    }),
    column('groups', t('inventory.groups'), (_, row) => row.group_objs?.length ? <Tags type='fill' data={row.group_objs.map((g) => g.name)} /> : '—'),
    column('tags', t('inventory.tags'), (_, row) => {
      const tags = [...new Set([...(row.tags || []), ...(row.host_tags || [])])];
      return tags.length ? <Tags type='outline' data={tags} onTagClick={onTagClick} /> : '—';
    }),
    {
      key: 'actions', title: t('inventory.actions'), width: 144, fixed: 'right', className: 'device-inventory-actions',
      render: (_, row) => <Space size={4}>
        <Button type='link' size='small' aria-label={`${t('inventory.edit')} ${deviceName(row)}`} disabled={!canEdit(row)} loading={busy === row.ident} onClick={() => edit(row)}>{t('inventory.edit')}</Button>
        <Button type='link' size='small' danger aria-label={`${t('inventory.delete')} ${deviceName(row)}`} disabled={!canDelete(row)} onClick={() => remove(row)}>{t('inventory.delete')}</Button>
      </Space>,
    },
  ];
  return <>
    {error && <Alert type='error' showIcon closable onClose={() => setError('')} message={error} />}
    <Table<Item>
      {...tableProps}
      className='device-inventory-table'
      rowKey='id' size='small' tableLayout='fixed'
      components={{ header: { cell: ResizableHeaderCell } }}
      columns={columns}
      scroll={{ x: Object.values(widths).reduce((sum, width) => sum + width, 184), y: 'calc(100% - 38px)' }}
      rowSelection={{ columnWidth: 40, preserveSelectedRowKeys: true, selectedRowKeys: selectedRows.map((row) => row.id), onChange: (_, rows) => setSelectedRows(rows) }}
    />
    <Suspense fallback={<Spin />}>
      {detail && <DeviceDetail id={detail.collection?.device_id} target={detail} editable={editable} onClose={() => setDetail(undefined)} onChanged={onChanged} onEditNative={() => { setDetail(undefined); edit(detail); }} />}
      {editing && <DeviceEditor device={editing} onClose={() => setEditing(undefined)} onSaved={() => { setEditing(undefined); onChanged(); }} />}
    </Suspense>
    {agent && <TargetMetaDrawer key={agentRevision} ident={agent.ident} drawerOnly drawerOpen onDrawerOpenChange={(open) => { if (!open) setAgent(undefined); }} extraActions={
      <Space wrap>
        <Button onClick={() => setOperation(OperateType.BindTag)}>{t('bind_tag.title')}</Button>
        <Button onClick={() => setOperation(OperateType.UnbindTag)}>{t('unbind_tag.title')}</Button>
        <Button onClick={() => setOperation(OperateType.UpdateBusi)}>{t('update_busi.title')}</Button>
        <Button onClick={() => setOperation(OperateType.UpdateNote)}>{t('update_note.title')}</Button>
      </Space>
    } />}
    {agent && operation !== OperateType.None && <OperationModal operateType={operation} setOperateType={setOperation} idents={[agent.ident]} reloadList={() => { setAgentRevision((value) => value + 1); onChanged(); }} />}
  </>;
}
