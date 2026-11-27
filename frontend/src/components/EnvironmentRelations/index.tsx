import React, { useEffect, useRef, useState } from 'react';
import { Alert, Button, Descriptions, Form, Input, Modal, Popconfirm, Select, Space, Table, Tabs, Tag, message } from 'antd';
import { ApartmentOutlined, PlusOutlined } from '@ant-design/icons';
import request from '@/utils/request';

type Entity = { ref: string; kind: string; name: string; url: string; fingerprint: string; writable: boolean; attributes: Record<string, any> };
type Relation = { id: number; revision: number; kind: string; effective: string; status: string; from: Entity; to: Entity; source: string; note: string; valid_until: number; reason?: string };
type Context = { entity: Entity; relations: Relation[]; native_relations: { from: Entity; to: Entity; kind: string; source: string }[]; gaps: string[]; truncated: boolean; discovery?: { state: string; note: string; updated_at: number } };
const kinds: Record<string, string> = { depends_on: '依赖于', runs_on: '部署于', belongs_to: '属于业务', maps_to: '监控映射', describes: '资料描述' };
const states: Record<string, string> = { confirmed: '已确认', candidate: '待确认', rejected: '已拒绝', withdrawn: '已撤回', stale: '对象已变更', expired: '已过期' };
const types: Record<string, string> = { target: '监控对象', asset: '资产', device: '采集设备', document: '资料', service: '业务对象' };
async function api<T,>(path: string, body?: any): Promise<T> {
  const r = await request('/api/n9e/environment' + path, { method: body ? 'POST' : 'GET', ...(body ? { data: body } : {}) });
  if (r.err) throw new Error(r.err);
  return r.dat;
}
const fieldNames: Record<string, string> = { ident: '监控标识', host_ip: 'IP 地址', address: '采集地址', note: '备注', tags: '标签', os: '操作系统', group_id: '业务组编号', type: '类型', description: '用途与环境', serial: '序列号', hostname: '主机名', businessIp: '业务 IP', managementIp: '管理 IP', business: '所属业务', customer: '客户', model: '型号', lifecycle: '生命周期', u_start: '起始 U 位', u_end: '结束 U 位', sequence: '资料版本', enabled: '采集启用' };
const displayValue = (key: string, value: any) => {
  if (value == null || value === '') return '未提供';
  if ((key === 'u_start' || key === 'u_end') && value === 0) return '未上架';
  if (key === 'sequence') return `V${value}`;
  if (key === 'type') return ({ service: '业务系统', application: '应用', database: '数据库', vip: '服务入口 VIP' } as Record<string, string>)[value] || String(value);
  if (typeof value === 'boolean') return value ? '是' : '否';
  if (Array.isArray(value)) return value.length ? value.join('，') : '未提供';
  return String(value);
};
const date = (n: number) => new Date(n * 1000).toLocaleString();
const error = (e: any) => e?.message || '查询失败，请重试';
export default function EnvironmentRelations({ objectRef }: { objectRef: string }) {
  const [visible, setVisible] = useState(false);
  const [ref, setRef] = useState(objectRef);
  const [context, setContext] = useState<Context>();
  const [options, setOptions] = useState<Entity[]>([]);
  const [failure, setFailure] = useState('');
  const [loading, setLoading] = useState(false);
  const [editing, setEditing] = useState(false);
  const [creating, setCreating] = useState(false);
  const [editingObject, setEditingObject] = useState(false);
  const [selectedOther, setSelectedOther] = useState<Entity>();
  const [groups, setGroups] = useState<{ id: number; name: string }[]>([]);
  const [graph, setGraph] = useState<{ nodes: Entity[]; relations: Relation[]; note: string; truncated: boolean }>();
  const [timeline, setTimeline] = useState<{ items: Entity[]; note: string; truncated: boolean }>();
  const [tab, setTab] = useState('relations');
  const [form] = Form.useForm();
  const [objectForm] = Form.useForm();
  const generation = useRef(0);
  const searchGeneration = useRef(0);
  const reload = async () => {
    const gen = ++generation.current;
    setLoading(true); setFailure(''); setContext(undefined); setOptions([]); searchGeneration.current++; setGraph(undefined); setTimeline(undefined);
    try { const value = await api<Context>('/context?ref=' + encodeURIComponent(ref)); if (gen === generation.current) setContext(value); }
    catch (e) { if (gen === generation.current) setFailure(error(e)); }
    finally { if (gen === generation.current) setLoading(false); }
  };
  useEffect(() => { if (visible) reload(); return () => { generation.current++; }; }, [visible, ref]);
  const search = async (q = '') => {
    const gen = ++searchGeneration.current;
    try { const r = await api<{ items: Entity[]; truncated: boolean }>('/search?q=' + encodeURIComponent(q)); if (gen === searchGeneration.current) setOptions(r.items); }
    catch (e) { message.error(error(e)); }
  };
  const change = async (r: Relation, action: string) => {
    try {
      await api('/relations/' + r.id, { revision: r.revision, action, from_version: r.from.fingerprint, to_version: r.to.fingerprint, note: r.note, valid_until: Math.floor(Date.now() / 1000) + 90 * 86400 });
      message.success('关系已更新'); reload();
    } catch (e) { message.error(error(e)); }
  };
  const entityLink = (e: Entity) => <Button size='small' type='link' onClick={() => { setRef(e.ref); setTab('relations'); }}>{e.name} · {types[e.kind] || e.kind}</Button>;
  const columns = [
    { title: '来源对象', key: 'from', render: (_: any, r: Relation) => entityLink(r.from) },
    { title: '关系', dataIndex: 'kind', render: (v: string) => kinds[v] || v },
    { title: '目标对象', key: 'to', render: (_: any, r: Relation) => entityLink(r.to) },
    { title: '状态', dataIndex: 'effective', render: (v: string) => <Tag color={v === 'confirmed' ? 'green' : v === 'candidate' ? 'blue' : 'orange'}>{states[v] || v}</Tag> },
    { title: '来源与有效期', key: 'source', render: (_: any, r: Relation) => <><div>{r.source === 'manual' ? '人工核实' : '资料标识提取'}</div><div>{r.note}</div><div>{r.reason || date(r.valid_until)}</div></> },
    { title: '操作', key: 'actions', render: (_: any, r: Relation) => r.from.writable && r.to.writable && <Space wrap>
      {r.effective === 'candidate' && <><Button size='small' onClick={() => change(r, 'confirm')}>确认关联</Button><Button size='small' onClick={() => change(r, 'reject')}>拒绝</Button></>}
      {['stale', 'expired', 'withdrawn'].includes(r.effective) && r.status !== 'candidate' && r.status !== 'rejected' && <Popconfirm zIndex={1300} title='已重新核实双方对象及来源，有效期续为90天？' onConfirm={() => change(r, 'renew')}><Button size='small'>重新核实</Button></Popconfirm>}
      {['confirmed', 'candidate'].includes(r.status) && <Popconfirm zIndex={1300} title='确认撤回这条关系？' onConfirm={() => change(r, 'withdraw')}><Button size='small'>撤回</Button></Popconfirm>}
    </Space> },
  ];
  return <>
    <Button aria-label='环境关联' icon={<ApartmentOutlined aria-hidden />} onClick={() => { setRef(objectRef); setVisible(true); setTab('relations'); }}>环境关联</Button>
    <Modal zIndex={1200} bodyStyle={{maxHeight: '70vh', overflowY: 'auto'}} title='环境关联' visible={visible} onCancel={() => setVisible(false)} width='min(1100px, 96vw)' footer={<Button onClick={() => setVisible(false)}>关闭</Button>} destroyOnClose>
      <Space wrap style={{ marginBottom: 16 }}>
        <Select getPopupContainer={(node) => node.parentElement!} key={context?.entity.fingerprint || `${ref}:unresolved`} aria-label='查找环境对象' placeholder='查找设备、业务或资料' showSearch filterOption={false} style={{ width: 280, maxWidth: '75vw' }} onSearch={search} onFocus={() => search()} value={ref} onChange={(value) => { setRef(value); setTab('relations'); }} options={[...(context && !options.some((v) => v.ref === context.entity.ref) ? [context.entity] : []), ...options].map((v) => ({ value: v.ref, label: `${v.name} · ${types[v.kind] || v.kind}` }))} />
        <Button aria-label='刷新关联' loading={loading} onClick={reload}>刷新关联</Button>
        <Button aria-label='新增关系' icon={<PlusOutlined aria-hidden />} disabled={!context?.entity.writable} onClick={() => { setEditing(true); setSelectedOther(undefined); form.resetFields(); form.setFieldsValue({ kind: context?.entity.kind === 'document' ? 'describes' : 'depends_on', reverse: 0 }); search(); }}>新增关系</Button>
        <Button onClick={async () => { try { const r = await request('/api/n9e/busi-groups'); if (r.err) throw new Error(r.err); setGroups(r.dat || []); objectForm.resetFields(); setEditingObject(false); setCreating(true); } catch (e) { message.error(error(e)); } }}>新建业务对象</Button>
        {context?.entity.kind === 'service' && context.entity.writable && <Button onClick={() => { objectForm.setFieldsValue({ name: context.entity.name, kind: context.entity.attributes.type, group_id: context.entity.attributes.group_id, description: context.entity.attributes.description }); setGroups([{id: context.entity.attributes.group_id,name: `业务组 ${context.entity.attributes.group_id}`}]); setEditingObject(true); setCreating(true); }}>编辑业务对象</Button>}
        {context?.entity.kind === 'service' && context.entity.writable && <Popconfirm zIndex={1300} title='停用后相关关系将失效，确认停用？' onConfirm={async () => { try { await api('/objects', {id: Number(ref.split(':')[1]),revision: context.entity.attributes.revision,name: context.entity.name,kind: context.entity.attributes.type,group_id: context.entity.attributes.group_id,description: context.entity.attributes.description,deleted: true}); reload(); } catch(e) {message.error(error(e));} }}><Button>停用业务对象</Button></Popconfirm>}
        {context?.entity.kind === 'document'  && context.entity.writable && <Button onClick={async () => { try { const r = await api<{ candidates: any[]; scanned: number }>('/discover', { ref }); message.success(`已检查${r.scanned}个对象，取得${r.candidates.length}条候选，请人工核实`); reload(); } catch (e) { message.error(error(e)); } }}>提取关联候选</Button>}
      </Space>
      {failure && <Alert type='error' showIcon message={failure} action={<Button size='small' onClick={reload}>重试</Button>} />}
      {context && <>
        <Descriptions bordered size='small' column={1} style={{ marginBottom: 12 }}>
          <Descriptions.Item label='当前对象'>{context.entity.name} · {types[context.entity.kind] || context.entity.kind}</Descriptions.Item>
          <Descriptions.Item label='标识与背景'>{Object.entries(context.entity.attributes).filter(([key]) => !['revision', 'source_version_id', 'rack_id'].includes(key)).map(([key, value]) => <Tag key={key} style={{whiteSpace: 'normal', overflowWrap: 'anywhere', maxWidth: '100%'}}>{fieldNames[key] || key}：{displayValue(key, value)}</Tag>)}</Descriptions.Item>
        </Descriptions>
        {context.discovery && <Alert style={{marginBottom: 12}} type={context.discovery.state === 'ready' ? 'info' : 'warning'} message={`自动关联检查：${date(context.discovery.updated_at)} · ${context.discovery.note}`} />}
        <Tabs activeKey={tab} onChange={(key) => { setTab(key); const gen = generation.current; if (key === 'trace') api<any>(`/trace?ref=${encodeURIComponent(ref)}&depth=3&direction=out`).then((value) => { if (gen === generation.current) setGraph(value); }).catch((e) => message.error(error(e))); if (key === 'timeline') api<any>('/timeline?ref=' + encodeURIComponent(ref)).then((value) => { if (gen === generation.current) setTimeline(value); }).catch((e) => message.error(error(e))); }}>
          <Tabs.TabPane key='relations' tab='关联与待确认项'>
            <Table size='small' rowKey='id' dataSource={context.relations} columns={columns} pagination={{ pageSize: 5 }} scroll={{ x: 900 }} locale={{ emptyText: '尚未维护关联；不能据此判断没有依赖' }} />
            {context.truncated && <Alert message='关联较多，结果已截断，请缩小查询范围' type='warning' />}
            {!!context.native_relations.length && <Descriptions title='已有配置关联' column={1} size='small'>{context.native_relations.map((r, i) => <Descriptions.Item key={i} label={r.source}>{entityLink(r.from)} → {entityLink(r.to)}</Descriptions.Item>)}</Descriptions>}
          </Tabs.TabPane>
          <Tabs.TabPane key='trace' tab='下游依赖（3层）'>
            {graph && <><Alert message={graph.note + (graph.truncated ? ' 本次结果已截断。' : '')} type='info' /><Table size='small' rowKey='id' dataSource={graph.relations} columns={columns.slice(0, 5)} scroll={{ x: 800 }} /></>}
          </Tabs.TabPane>
          <Tabs.TabPane key='timeline' tab='相关告警与台账变更'>
            {timeline && <><Alert message={timeline.note + (timeline.truncated ? ' 本次结果已截断。' : '')} type='info' /><Table size='small' rowKey='ref' dataSource={timeline.items} columns={[{ title: '记录', dataIndex: 'name', render: (v, r) => <a href={r.url} target='_blank' rel='noreferrer'>{v}</a> }, { title: '发生时间', render: (_, r) => date(r.attributes.trigger_time || r.attributes.created_at) }, { title: '摘要', render: (_, r) => JSON.stringify(r.attributes) }]} scroll={{ x: 700 }} /></>}
          </Tabs.TabPane>
        </Tabs>
      </>}
    </Modal>
    <Modal zIndex={1210} title='新增环境关系' visible={editing} onCancel={() => setEditing(false)} onOk={async () => {
      try { const v = await form.validateFields(); const other = selectedOther; if (!other || !context) return; const a = v.reverse ? other : context.entity; const b = v.reverse ? context.entity : other; await api('/relations', { from_ref: a.ref, to_ref: b.ref, from_version: a.fingerprint, to_version: b.fingerprint, kind: v.kind, note: v.note, valid_until: Math.floor(Date.now() / 1000) + 90 * 86400 }); setEditing(false); reload(); } catch (e) { if (!(e as any)?.errorFields) message.error(error(e)); }
    }} destroyOnClose>
      <Form form={form} layout='vertical' initialValues={{ kind: context?.entity.kind === 'document' ? 'describes' : 'depends_on', reverse: 0 }}>
        <Form.Item label='关联对象' name='other' rules={[{ required: true }]}><Select getPopupContainer={(node) => node.parentElement!} showSearch filterOption={false} onSearch={search} onChange={(value) => setSelectedOther(options.find((o) => o.ref === value))} options={options.filter((o) => o.ref !== ref && o.writable).map((o) => ({ value: o.ref, label: `${o.name} · ${types[o.kind] || o.kind}` }))} /></Form.Item>
        <Form.Item label='关系方向' name='reverse'><Select getPopupContainer={(node) => node.parentElement!} options={[{ value: 0, label: '当前对象 → 关联对象' }, { value: 1, label: '关联对象 → 当前对象' }]} /></Form.Item>
        <Form.Item label='关系类型' name='kind' rules={[{ required: true }]}><Select getPopupContainer={(node) => node.parentElement!} options={Object.entries(kinds).map(([value, label]) => ({ value, label }))} /></Form.Item>
        <Form.Item label='来源与核实说明（有效期90天）' name='note' rules={[{ required: true, whitespace: true }]}><Input.TextArea maxLength={2000} rows={3} /></Form.Item>
      </Form>
    </Modal>
    <Modal zIndex={1210} title={editingObject ? '编辑业务对象' : '新建业务对象'} visible={creating} onCancel={() => setCreating(false)} onOk={async () => { try { const v = await objectForm.validateFields(); const r = await api<{ id: number }>('/objects', {...v, ...(editingObject && context ? { id: Number(ref.split(':')[1]), revision: context.entity.attributes.revision } : {})}); setCreating(false); setRef(`service:${r.id}`); setTab('relations'); if (editingObject) reload(); } catch (e) { if (!(e as any)?.errorFields) message.error(error(e)); } }} destroyOnClose>
      <Form form={objectForm} layout='vertical' initialValues={{ kind: 'application' }}>
        <Form.Item name='name' label='名称' rules={[{ required: true, whitespace: true }]}><Input maxLength={160} /></Form.Item>
        <Form.Item name='kind' label='类型' rules={[{ required: true }]}><Select getPopupContainer={(node) => node.parentElement!} options={[{ value: 'service', label: '业务系统' }, { value: 'application', label: '应用' }, { value: 'database', label: '数据库' }, { value: 'vip', label: '服务入口 VIP' }]} /></Form.Item>
        <Form.Item name='group_id' label='业务组' rules={[{ required: true }]}><Select getPopupContainer={(node) => node.parentElement!} options={groups.map((g) => ({ value: g.id, label: g.name }))} /></Form.Item>
        <Form.Item name='description' label='用途与环境'><Input.TextArea maxLength={4000} /></Form.Item>
      </Form>
    </Modal>
  </>;
}
