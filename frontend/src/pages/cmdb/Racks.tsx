import React, { useMemo, useState } from 'react';
import { Alert, Button, Checkbox, Empty, Form, Input, InputNumber, Modal, Radio, Select, Space, Switch, Tag, Tooltip, message } from 'antd';
import { CloseOutlined, DatabaseOutlined, DeleteOutlined, EditOutlined, InfoCircleOutlined } from '@ant-design/icons';
import PageLayout from '@/components/pageLayout';
import RackViewport from '@/pages/rackViews/RackViewport';
import AssetPanel from './AssetPanel';
import MaterialLibrary from './MaterialLibrary';
import { PanelSide } from './materialCatalog';
import '../rackViews/index.less';
import { api, Rack, RackAlert, Room, saveAsset } from './api';
import { Device, blankDevice } from './model';
import useInventory from './useInventory';
import DeviceEditor, { Editor } from './DeviceEditor';
import RackDeviceAlerts, { rackAlertLevels } from './RackDeviceAlerts';
import './index.less';

export function placement(d: Device, rack: Rack, start: number, devices: Device[]): string | undefined {
  const height = (d.uEnd || 1) - (d.uStart || 1) + 1,
    end = start + height - 1;
  if (!Number.isInteger(start) || !Number.isInteger(end) || start < 1 || end > rack.units) return '目标位置超出机柜容量';
  if (devices.some((x) => x.id !== d.id && x.rackId === rack.id && x.uStart != null && x.uEnd != null && start <= x.uEnd && end >= x.uStart)) return '目标 U 位已被占用';
}
export default function Racks() {
  const { devices, catalog, access, error, loading, reload, rackAlerts, rackAlertsFailed } = useInventory(true);
  const [side, setSide] = useState<PanelSide>('front');
  const [selected, setSelected] = useState<string>();
  const [compare, setCompare] = useState<string[]>([]);
  const [editor, setEditor] = useState<Editor>();
  const [drag, setDrag] = useState<Device>();
  const [hover, setHover] = useState<{ rackId: string; start: number }>();
  const [saving, setSaving] = useState(false);
  const [dialog, setDialog] = useState<'room' | 'rack'>();
  const [editingRoom, setEditingRoom] = useState<Room>();
  const [editingRack, setEditingRack] = useState<Rack>();
  const [form] = Form.useForm();
  const [dialogError, setDialogError] = useState('');
  const [deleting, setDeleting] = useState<{ type: 'room' | 'rack'; item: Room | Rack }>();
  const [deleteError, setDeleteError] = useState('');
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [search, setSearch] = useState('');
  const [showAlertLights, setShowAlertLights] = useState(true);
  const [onlyAlertRacks, setOnlyAlertRacks] = useState(false);
  const canEdit = !!access?.permissions.edit;
  const confirmDelete = (type: 'room' | 'rack', item: Room | Rack) => {
    setDeleteError('');
    setDeleting({ type, item });
  };
  const remove = async () => {
    if (!deleting || deleteBusy) return;
    setDeleteBusy(true);
    try {
      await api(
        `${deleting.type === 'room' ? 'rooms' : 'racks'}/${deleting.item.id}`,
        'DELETE',
        deleting.type === 'rack' ? { version: (deleting.item as Rack).version } : undefined,
      );
      setDeleting(undefined);
      await reload();
      message.success('删除成功');
    } catch (e) {
      setDeleteError(e instanceof Error ? e.message : '删除失败，请刷新重试');
    } finally {
      setDeleteBusy(false);
    }
  };
  const close = () => {
    setDialog(undefined);
    setEditingRack(undefined);
    setEditingRoom(undefined);
    setDialogError('');
    form.resetFields();
  };
  const open = (type: 'room' | 'rack', r?: Rack | Room) => {
    form.resetFields();
    setDialogError('');
    setEditingRack(type === 'rack' ? (r as Rack | undefined) : undefined);
    setEditingRoom(type === 'room' && r ? { id: r.id, name: r.name } : undefined);
    form.setFieldsValue(r || { units: 42, roomId: catalog.rooms[0]?.id });
    setDialog(type);
  };
  const save = async () => {
    try {
      const values = await form.validateFields();
      const existing = dialog === 'room' ? editingRoom : editingRack;
      await api(dialog === 'room' ? 'rooms' : 'racks', existing ? 'PUT' : 'POST', { ...existing, ...values, ...(editingRoom ? { previousName: editingRoom.name } : {}) });
      await reload();
      close();
    } catch (e) {
      if (e instanceof Error) setDialogError(e.message);
    }
  };
  const drop = async (rack: Rack, start: number) => {
    const asset = drag;
    setDrag(undefined);
    setHover(undefined);
    if (!asset || saving || !canEdit) return;
    const invalid = placement(asset, rack, start, devices);
    if (invalid) {
      message.error(invalid);
      return;
    }
    setSaving(true);
    try {
      const room = catalog.rooms.find((x) => x.id === rack.roomId);
      await saveAsset({ ...asset, rackId: rack.id, room: room?.name || '', rack: rack.name, uStart: start, uEnd: start + (asset.uEnd || 1) - (asset.uStart || 1) }, false);
      await reload();
      message.success('设备位置已同步到 CMDB');
    } catch (e) {
      message.error(e instanceof Error ? e.message : '移动失败，位置未改变');
      void reload().catch(() => {});
    } finally {
      setSaving(false);
    }
  };
  const alertRacks = useMemo(() => {
    const result = new Map<string, { severity: RackAlert['severity']; devices: number }>();
    for (const device of devices) {
      const severity = rackAlerts[device.id]?.[0]?.severity;
      if (!severity || !device.rackId || !device.uStart || !device.uEnd) continue;
      const current = result.get(device.rackId);
      if (current) {
        current.devices++;
        if (severity < current.severity) current.severity = severity;
      } else result.set(device.rackId, { severity, devices: 1 });
    }
    return result;
  }, [devices, rackAlerts]);
  const availableRacks = onlyAlertRacks ? catalog.racks.filter((r) => alertRacks.has(r.id)) : catalog.racks;
  const rack = availableRacks.find((r) => r.id === selected) || availableRacks[0];
  const comparisons = compare
    .map((id) => availableRacks.find((r) => r.id === id && r.id !== rack?.id))
    .filter((r): r is Rack => !!r)
    .slice(0, 3);
  const shown = rack ? [rack, ...comparisons] : [];
  const query = search.trim().toLowerCase();
  const rooms = catalog.rooms
    .map((room) => ({
      ...room,
      racks: availableRacks.filter((r) => r.roomId === room.id && `${room.name} ${r.name}`.toLowerCase().includes(query)),
    }))
    .filter((room) => (onlyAlertRacks ? room.racks.length > 0 : !query || room.name.toLowerCase().includes(query) || room.racks.length > 0));
  const rackEmpty = rackAlertsFailed && onlyAlertRacks ? '当前告警暂不可用' : onlyAlertRacks ? '暂无告警机柜' : '暂无机柜';
  const unplaced = devices.filter((d) => !d.uStart || !d.uEnd);
  const renderRack = (r: Rack) => {
    const placed = devices.filter((d) => d.rackId === r.id && d.uStart && d.uEnd);
    const used = placed.reduce((a, d) => a + (d.uEnd || 0) - (d.uStart || 0) + 1, 0);
    const preview =
      drag && hover?.rackId === r.id
        ? { start: hover.start, end: hover.start + (drag.uEnd || 1) - (drag.uStart || 1), error: placement(drag, r, hover.start, devices) }
        : undefined;
    const pointerUnit = (event: React.DragEvent<HTMLDivElement>) => {
      const bounds = event.currentTarget.getBoundingClientRect();
      return Math.max(1, Math.min(r.units, r.units - Math.floor(((event.clientY - bounds.top) / (bounds.height / (r.units * 44 + (r.units - 1) * 2 + 24)) - 12) / 46)));
    };
    return (
      <div key={r.id} className='rack-panel'>
        <div className='rack-heading'>
          <div className='rack-title'>
            <h2 title={r.name}>
              {r.name} · {side === 'front' ? '正视图' : '背视图'}
            </h2>
            <span title={catalog.rooms.find((x) => x.id === r.roomId)?.name}>
              {catalog.rooms.find((x) => x.id === r.roomId)?.name} · {used}/{r.units}U
            </span>
          </div>
          <Space size={4} wrap>
            <Button size='small' disabled={!canEdit || saving} onClick={() => open('rack', r)}>
              编辑机柜
            </Button>
            <Button
              size='small'
              disabled={!access?.permissions.create}
              onClick={() =>
                setEditor({
                  mode: 'add',
                  device: { ...blankDevice, customer: '', rackId: r.id, rack: r.name, room: catalog.rooms.find((x) => x.id === r.roomId)?.name || '', disks: [] },
                })
              }
            >
              添加设备
            </Button>
            <Button
              size='small'
              danger
              type='text'
              aria-label={`删除机柜 ${r.name}`}
              icon={<DeleteOutlined />}
              disabled={!access?.permissions.delete || saving}
              onClick={() => confirmDelete('rack', r)}
            />
            {r.id !== rack?.id && (
              <Button size='small' type='text' aria-label={`关闭并排机柜 ${r.name}`} icon={<CloseOutlined />} onClick={() => setCompare(compare.filter((id) => id !== r.id))} />
            )}
          </Space>
        </div>
        <div className='rack-placement-status' role='status'>
          {preview && (
            <Tag color={preview.error ? 'red' : 'purple'}>
              {r.name} · {preview.start}–{preview.end}U · {preview.error || '松开后保存位置'}
            </Tag>
          )}
        </div>
        <RackViewport>
          <div className='rack-cabinet'>
            <div className='rack-cabinet-header'>
              <span>19″ RACK</span>
              <span>
                {r.name} · {r.units}U
              </span>
            </div>
            <div
              className='rack-chassis'
              style={{ gridTemplateRows: `repeat(${r.units},44px)` }}
              onDragOver={(e) => {
                if (drag && canEdit && !saving) {
                  e.preventDefault();
                  e.dataTransfer.dropEffect = 'move';
                  setHover({ rackId: r.id, start: pointerUnit(e) });
                }
              }}
              onDrop={(e) => {
                e.preventDefault();
                void drop(r, pointerUnit(e));
              }}
              onDragLeave={(e) => {
                if (!(e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget))) setHover(undefined);
              }}
            >
              {Array.from({ length: r.units }, (_, i) => {
                const u = r.units - i,
                  occupied = placed.some((d) => u >= (d.uStart || 0) && u <= (d.uEnd || 0)),
                  highlight = preview && u >= preview.start && u <= preview.end;
                return (
                  <React.Fragment key={u}>
                    <span className='rack-unit' style={{ gridRow: i + 1 }}>
                      {u}U
                    </span>
                    <button
                      className='rack-slot'
                      aria-label={`${r.name} ${u}U`}
                      style={{ gridRow: i + 1, background: highlight ? (preview.error ? 'rgba(255,70,70,.25)' : 'rgba(108,83,177,.35)') : undefined }}
                      disabled={occupied || !access?.permissions.create || saving}
                      onClick={() =>
                        setEditor({
                          mode: 'add',
                          device: {
                            ...blankDevice,
                            customer: '',
                            rackId: r.id,
                            rack: r.name,
                            room: catalog.rooms.find((x) => x.id === r.roomId)?.name || '',
                            uStart: u,
                            uEnd: u,
                            disks: [],
                          },
                        })
                      }
                    >
                      {!occupied && '+'}
                    </button>
                  </React.Fragment>
                );
              })}
              {placed.map((d) => {
                const alerts = rackAlerts[d.id];
                const severity = alerts?.[0]?.severity;
                const label = severity ? ` · ${rackAlertLevels[severity].label} · ${alerts.length} 条当前告警` : '';
                return (
                  <button
                    key={d.id}
                    draggable={canEdit && !saving}
                    className='rack-device rack-device-with-faceplate rack-device-photo'
                    data-rack-alert-level={showAlertLights ? severity : undefined}
                    style={{ gridRow: `${r.units - (d.uEnd || 1) + 1} / span ${(d.uEnd || 1) - (d.uStart || 1) + 1}`, cursor: canEdit ? 'grab' : 'pointer' }}
                    onDragStart={(e) => {
                      e.dataTransfer.setData('text/plain', d.id);
                      e.dataTransfer.effectAllowed = 'move';
                      setDrag({ ...d });
                    }}
                    onDragEnd={() => {
                      setDrag(undefined);
                      setHover(undefined);
                    }}
                    onClick={() => {
                      if (!drag) setEditor({ mode: 'view', device: d });
                    }}
                    title={`${d.hostname || d.serial} · ${d.vendor} ${d.model} · ${d.uStart}–${d.uEnd}U${label}`}
                  >
                    <AssetPanel device={d} side={side} />
                    {showAlertLights && severity && <span className='rack-device-alert-light' aria-hidden='true' />}
                    <span className='rack-device-nameplate'>
                      <strong>{d.hostname || d.serial}</strong>
                      <span>
                        {d.model} · {d.uStart}–{d.uEnd}U
                      </span>
                    </span>
                  </button>
                );
              })}
            </div>
            <div className='rack-cabinet-footer'>
              <span />
              <span />
            </div>
          </div>
        </RackViewport>
      </div>
    );
  };
  return (
    <PageLayout title='机柜图' icon={<DatabaseOutlined />}>
      <div className='rack-views'>
        {error && <Alert type='error' showIcon message={error} />}
        {rackAlertsFailed && <Alert type='warning' showIcon message='机柜告警加载失败，刷新后重试' />}
        <div className='rack-toolbar'>
          <Space size={8} wrap>
            <Radio.Group
              size='small'
              value={side}
              onChange={(e) => {
                setSide(e.target.value);
                setHover(undefined);
                setDrag(undefined);
              }}
              optionType='button'
              options={[
                { value: 'front', label: '正视图' },
                { value: 'rear', label: '背视图' },
              ]}
              disabled={!!drag || saving}
            />
          </Space>
          <Input.Search className='rack-search' size='small' placeholder='搜索机房 / 机柜' allowClear onChange={(e) => setSearch(e.target.value)} />
          <Select
            className='rack-compare'
            size='small'
            mode='multiple'
            maxTagCount={1}
            allowClear
            aria-label='并排显示机柜'
            value={comparisons.map((r) => r.id)}
            placeholder='添加并排机柜（最多4个）'
            onChange={(ids) => setCompare(ids.slice(0, 3))}
            options={availableRacks
              .filter((r) => r.id !== rack?.id)
              .map((r) => ({
                value: r.id,
                label: `${catalog.rooms.find((x) => x.id === r.roomId)?.name} / ${r.name}`,
                disabled: comparisons.length >= 3 && !comparisons.some((x) => x.id === r.id),
              }))}
          />
          <Space size={8} wrap>
            <Checkbox checked={onlyAlertRacks} onChange={(e) => setOnlyAlertRacks(e.target.checked)}>
              仅告警机柜
            </Checkbox>
            <Switch size='small' aria-label='告警亮灯' checked={showAlertLights} onChange={setShowAlertLights} />
            <span>告警亮灯</span>
          </Space>
          <Space size={8} wrap className='rack-management'>
            <MaterialLibrary />
            <Button size='small' disabled={!access?.permissions.edit} onClick={() => open('room')}>
              新增机房
            </Button>
            <Button size='small' disabled={!access?.permissions.edit} onClick={() => open('rack')}>
              新增机柜
            </Button>
            <Button size='small' loading={loading || saving} onClick={() => void reload().catch(() => {})}>
              刷新
            </Button>
            <Tooltip
              placement='bottomRight'
              trigger={['hover', 'focus']}
              title={
                <div className='rack-help-content'>
                  <p>点击设备查看或编辑；拖动时以指针所在 U 位作为起始位置，保留设备高度并自动保存。</p>
                  <p>最多四个机柜并排，向右滚动查看并跨柜拖动。默认适应窗口；滚轮以鼠标位置为中心缩放，也可使用缩放按钮和滚动条查看局部。</p>
                  <p>当前告警按设备 IP 关联。一级红灯、二级橙灯、三级黄灯。点击设备可查看对应告警事件。</p>
                </div>
              }
            >
              <Button size='small' type='text' aria-label='机柜图使用说明' icon={<InfoCircleOutlined />} />
            </Tooltip>
          </Space>
        </div>
        <div className='rack-workspace'>
          <aside className='rack-list'>
            {rooms.map((room) => (
              <details key={room.id} className='rack-room' open={query || onlyAlertRacks ? true : undefined}>
                <summary>
                  <span className='rack-room-name' title={room.name}>
                    {room.name}
                  </span>
                  <span className='rack-room-count'>{room.racks.length}</span>
                  <Button
                    size='small'
                    type='text'
                    aria-label={`编辑机房 ${room.name}`}
                    title='编辑机房'
                    icon={<EditOutlined />}
                    disabled={!canEdit || saving}
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      open('room', room);
                    }}
                  />
                  <Button
                    size='small'
                    type='text'
                    danger
                    aria-label={`删除机房 ${room.name}`}
                    icon={<DeleteOutlined />}
                    disabled={!access?.permissions.delete || saving}
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation();
                      confirmDelete('room', room);
                    }}
                  />
                </summary>
                {room.racks.map((r) => (
                  <div key={r.id} className='rack-list-entry'>
                    <button
                      className={r.id === rack?.id ? 'rack-list-item selected' : 'rack-list-item'}
                      title={`${r.name} · ${room.name}`}
                      onClick={() => {
                        setSelected(r.id);
                        setCompare(comparisons.filter((x) => x.id !== r.id).map((x) => x.id));
                      }}
                    >
                      <strong>{r.name}</strong>
                      <span className='rack-list-meta'>
                        {r.units}U
                        {alertRacks.has(r.id) && (
                          <span className='rack-list-alert-count' title={`${alertRacks.get(r.id)!.devices} 台设备有当前告警`}>
                            {showAlertLights && <i data-rack-alert-level={alertRacks.get(r.id)!.severity} aria-hidden='true' />}
                            {alertRacks.get(r.id)!.devices}
                          </span>
                        )}
                      </span>
                    </button>
                    <Space size={0} className='rack-list-actions'>
                      <Button
                        size='small'
                        type='text'
                        aria-label={`编辑机柜 ${r.name}`}
                        title='编辑机柜'
                        icon={<EditOutlined />}
                        disabled={!canEdit || saving}
                        onClick={() => open('rack', r)}
                      />
                      <Button
                        size='small'
                        type='text'
                        danger
                        aria-label={`删除机柜 ${r.name}`}
                        title='删除机柜'
                        icon={<DeleteOutlined />}
                        disabled={!access?.permissions.delete || saving}
                        onClick={() => confirmDelete('rack', r)}
                      />
                    </Space>
                  </div>
                ))}
                {!room.racks.length && <div className='rack-room-empty'>暂无机柜</div>}
              </details>
            ))}
            {!rooms.length && <Empty description={query ? '未找到机房或机柜' : onlyAlertRacks ? rackEmpty : '请先创建机房和机柜'} />}
          </aside>
          <main className='rack-detail'>
            <div className={`rack-panels${shown.length > 1 ? ' rack-panels-multiple' : ''}`} aria-label='并排机柜区域'>
              {shown.length ? shown.map(renderRack) : <Empty description={rackEmpty} />}
            </div>
            {unplaced.length > 0 && (
              <details className='rack-unplaced'>
                <summary>待上架设备（{unplaced.length}）</summary>
                <Space wrap>
                  {unplaced.map((d) => (
                    <Button key={d.id} onClick={() => setEditor({ mode: 'view', device: d })}>
                      {d.hostname || d.serial}
                    </Button>
                  ))}
                </Space>
              </details>
            )}
          </main>
        </div>
        <Modal
          title={`删除${deleting?.type === 'room' ? '机房' : '机柜'}`}
          visible={!!deleting}
          okText='删除'
          okButtonProps={{ danger: true }}
          confirmLoading={deleteBusy}
          cancelButtonProps={{ disabled: deleteBusy }}
          closable={!deleteBusy}
          maskClosable={!deleteBusy}
          onCancel={() => setDeleting(undefined)}
          onOk={remove}
          destroyOnClose
        >
          <p>确定删除“{deleting?.item.name}”？删除后无法撤销。</p>
          <p>{deleting?.type === 'room' ? '机房下有机柜时无法删除，请先迁移或删除机柜。' : '请先迁移机柜内设备；待上架及回收站中的绑定设备也需要处理。'}</p>
          {deleteError && <Alert type='error' showIcon message={deleteError} />}
        </Modal>
        <DeviceEditor
          editor={editor}
          devices={devices}
          catalog={catalog}
          onClose={() => setEditor(undefined)}
          onSaved={reload}
          canEdit={canEdit}
          canDelete={!!access?.permissions.delete}
          viewContent={editor?.device && <RackDeviceAlerts alerts={rackAlerts[editor.device.id] || []} failed={rackAlertsFailed} />}
        />
        <Modal
          title={dialog === 'room' ? (editingRoom ? '编辑机房' : '新增机房') : editingRack ? '编辑机柜' : '新增机柜'}
          visible={!!dialog}
          okText='保存'
          cancelText='取消'
          onCancel={close}
          onOk={save}
          destroyOnClose
        >
          <Form form={form} layout='vertical'>
            {dialogError && <Alert type='error' message={dialogError} />}
            <Form.Item name='name' label={dialog === 'room' ? '机房名称' : '机柜名称'} rules={[{ required: true, whitespace: true }]}>
              <Input maxLength={60} />
            </Form.Item>
            {dialog === 'rack' && (
              <>
                <Form.Item name='roomId' label='所属机房' rules={[{ required: true }]}>
                  <Select options={catalog.rooms.map((r) => ({ value: r.id, label: r.name }))} />
                </Form.Item>
                <Form.Item name='units' label='容量（U，从底部开始计数）' rules={[{ required: true }]}>
                  <InputNumber min={1} max={60} precision={0} />
                </Form.Item>
              </>
            )}
          </Form>
        </Modal>
      </div>
    </PageLayout>
  );
}
