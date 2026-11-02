import React, { useContext, useEffect, useState } from 'react';
import { Alert, Button, Checkbox, Form, Input, InputNumber, Menu, message, Modal, Select, Space, Switch, Table, Tag } from 'antd';
import ImportDashboard from './Dashboards/Import';
import ImportAlerts from './AlertRules/Import';
import { CommonStateContext } from '@/App';
import AuthorizationWrapper from '@/components/AuthorizationWrapper';
import { adapterNames, CatalogEntry, connectionFields, Definition, DeviceField, executionNames, getCatalogEntry, PackResource, saveDraft, TemplatePack } from './catalog';
import { errorText } from '@/pages/collection/errors';
import { getItems as getNotifyRules } from '@/pages/notificationRules/services';

const sections = ['采集方式与兼容性', '设备添加表单', '采集与指标说明', '仪表盘模板', '告警规则与通知'];
const controls: DeviceField['type'][] = ['text', 'password', 'credential', 'number', 'switch', 'select', 'multiselect', 'ip', 'url', 'endpoints'];
const emptyPack = (id: number): TemplatePack => ({ component_id: id, name: '', readme: '', metrics: [], dashboards: [], alerts: [] });

// All five sections edit one draft value. No resource-library writes happen here.
export default function CatalogEditor({ componentId }: { componentId: number }) {
  const commonState = useContext(CommonStateContext);
  const { profile: account, perms, busiGroups } = commonState;
  const [entry, setEntry] = useState<CatalogEntry>();
  const [draft, setDraft] = useState<Definition>();
  const [section, setSection] = useState(0);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [fieldIndex, setFieldIndex] = useState<number>();
  const [fieldForm] = Form.useForm();
  const [resource, setResource] = useState<{ kind: 'dashboards' | 'alerts'; index: number }>();
  const [resourceForm] = Form.useForm();
  const [notificationRules, setNotificationRules] = useState<{ id: number; name: string }[]>([]);
  const load = async () => {
    const current = await getCatalogEntry(componentId);
    setEntry(current);
    setDraft(current ? JSON.parse(JSON.stringify(current.draft)) : undefined);
  };
  useEffect(() => {
    load().catch((e) => setError(errorText(e, '加载失败')));
    if (account?.admin || perms?.includes('/notification-rules'))
      getNotifyRules()
        .then((r) => setNotificationRules(r || []))
        .catch(() => {});
  }, [componentId]);
  const patch = (values: Partial<Definition>) => setDraft((value) => value && { ...value, ...values });
  const pack = draft?.pack || emptyPack(componentId);
  const patchPack = (values: Partial<TemplatePack>) => patch({ pack: { ...pack, ...values } });
  const editField = (index: number) => {
    const field = draft?.fields[index] || { key: '', label: '', type: 'text', required: true };
    fieldForm.resetFields();
    fieldForm.setFieldsValue({
      ...field,
      default_json: field.default === undefined ? '' : JSON.stringify(field.default),
      options_text: (field.options || []).join('\n'),
      conditions: field.when || [],
    });
    setFieldIndex(index);
  };
  const editResource = (kind: 'dashboards' | 'alerts', index: number) => {
    const value = pack[kind][index];
    resourceForm.resetFields();
    resourceForm.setFieldsValue({
      name: value?.name || '',
      content: value
        ? JSON.stringify(JSON.parse(value.content), null, 2)
        : kind === 'dashboards'
        ? '{"name":"设备总览","configs":{"panels":[]}}'
        : '{"name":"设备告警","cate":"prometheus","prod":"metric","severity":2,"prom_for_duration":300,"rule_config":{"queries":[],"triggers":[]},"annotations":{"action":"检查设备与采集器连接"}}',
    });
    setResource({ kind, index });
  };
  const importResource = (row: PackResource, kind: 'dashboards' | 'alerts') => {
    try {
      const content = { ...JSON.parse(row.content), name: row.name };
      if (kind === 'dashboards') ImportDashboard({ data: JSON.stringify(content), busiGroups });
      else
        ImportAlerts({
          data: JSON.stringify([content]),
          busiGroups,
          groupedDatasourceList: commonState.groupedDatasourceList,
          reloadGroupedDatasourceList: commonState.reloadGroupedDatasourceList,
          datasourceCateOptions: commonState.datasourceCateOptions,
          notificationRulesAuthorized: !!account?.admin || !!perms?.includes('/notification-rules'),
          commonState,
        });
    } catch {
      message.error('模板内容不是有效的夜莺 JSON');
    }
  };
  const save = async (publish: boolean) => {
    if (!entry || !draft) return;
    setBusy(true);
    setError('');
    try {
      await saveDraft(entry, draft, publish);
      message.success(publish ? '完整监控包已发布；已有设备保持绑定版本' : '完整草稿已保存');
      await load();
    } catch (e) {
      setError(errorText(e, '保存失败'));
    } finally {
      setBusy(false);
    }
  };
  if (!draft) return error ? <Alert type='error' message={error} /> : <div>加载模板草稿…</div>;
  const notify = draft.notify || { mode: draft.notification || 'events_only', message: '', rule_ids: [] };
  const patchNotify = (values: Partial<typeof notify>) => patch({ notify: { ...notify, ...values } });
  return (
    <div className='template-package-editor'>
      <div className='template-package-toolbar'>
        <Space>
          <Tag color={entry?.published ? 'green' : 'default'}>{entry?.published ? `已发布 v${entry.published.version}` : '草稿'}</Tag>
          <span>所有内容共同保存、共同发布；设备绑定不可变版本。</span>
        </Space>
        <AuthorizationWrapper allowedPerms={['/components/put']}>
          <Space>
            <Button loading={busy} onClick={() => save(false)}>
              保存草稿
            </Button>
            <Button type='primary' loading={busy} onClick={() => save(true)}>
              发布新版本
            </Button>
          </Space>
        </AuthorizationWrapper>
      </div>
      {error && <Alert className='mb-3' type='error' message={error} showIcon />}
      <div className='template-package-layout'>
        <Menu selectedKeys={[String(section)]} onClick={({ key }) => setSection(Number(key))} items={sections.map((label, key) => ({ key: String(key), label }))} />
        <div className='template-package-content'>
          <h3>{sections[section]}</h3>
          {section === 0 && (
            <Form layout='vertical'>
              <Alert className='mb-4' type='info' message='名称、图标、分类和启用状态由卡片右上角的基本信息按钮编辑。这里配置完整监控包。' />
              <Form.Item label='采集方式'>
                <Select
                  value={draft.adapter}
                  options={Object.entries(adapterNames).map(([value, label]) => ({ value, label }))}
                  onChange={(adapter) => patch({ adapter, ...(draft.fields.length === 0 ? { fields: connectionFields(adapter) } : {}) })}
                />
              </Form.Item>
              <Alert className='mb-4' type='info' message='默认基础采集：ICMP 可达性' description='启用设备后，采集器独立执行 ping，并将结果码、丢包率和延迟写入时序数据库。结果码 ping_result_code：0 可达、1 不可达、2 探测异常；丢包率 ping_percent_packet_loss，平均延迟 ping_average_response_ms。ICMP 不代替本模板的协议采集状态。' />
              <Form.Item label='执行位置'>
                <Select
                  value={draft.execution || 'collector'}
                  options={Object.entries(executionNames).map(([value, label]) => ({ value, label }))}
                  onChange={(execution) => patch({ execution })}
                />
              </Form.Item>
              <Form.Item label='兼容性与权限要求'>
                <Input.TextArea rows={5} value={draft.compatibility} onChange={(e) => patch({ compatibility: e.target.value })} />
              </Form.Item>
              {!['snmp_v2c', 'mysql'].includes(draft.adapter) && (
                <Alert type='warning' message='可编辑和保存完整草稿。此采集方式尚未通过执行编译器验证，不能发布为自动接入模板。' />
              )}
            </Form>
          )}
          {section === 1 && (
            <>
              <p>新增设备按照本表生成控件、条件字段和校验。密码加密保存；隐藏字段不会参与校验或下发。</p>
              <Space className='mb-3'>
                <Button onClick={() => editField(draft.fields.length)}>添加字段</Button>
                <Button onClick={() => Modal.confirm({ title: '替换为此采集方式的连接字段？', onOk: () => patch({ fields: connectionFields(draft.adapter) }) })}>
                  装入标准连接字段
                </Button>
              </Space>
              <Table
                rowKey='key'
                pagination={false}
                dataSource={draft.fields}
                columns={[
                  {
                    title: '字段',
                    render: (_, field: DeviceField) => (
                      <>
                        <b>{field.label}</b>
                        <div>
                          <code>{field.key}</code>
                        </div>
                      </>
                    ),
                  },
                  { title: '控件', dataIndex: 'type' },
                  { title: '必填 / 凭据', render: (_, field: DeviceField) => `${field.required ? '必填' : '可选'}${field.secret || field.type === 'password' ? ' · 加密' : ''}` },
                  { title: '显示条件', render: (_, field: DeviceField) => field.when?.map((c) => `${c.key} ∈ ${c.values.join(', ')}`).join(' 且 ') || '始终显示' },
                  {
                    title: '操作',
                    render: (_, field: DeviceField, index) => (
                      <Space>
                        <Button size='small' onClick={() => editField(index)}>
                          编辑
                        </Button>
                        <Button size='small' danger onClick={() => patch({ fields: draft.fields.filter((_, i) => i !== index) })}>
                          删除
                        </Button>
                      </Space>
                    ),
                  },
                ]}
              />
            </>
          )}
          {section === 2 && (
            <Form layout='vertical'>
              <Form.Item label='采集说明'>
                <Input.TextArea rows={8} value={pack.readme} onChange={(e) => patchPack({ readme: e.target.value })} />
              </Form.Item>
              <Form.Item label='采集配置' extra='使用对应适配器的类型化采集项；目标地址、凭据和归属标签由设备参数绑定。'>
                <Input.TextArea rows={10} value={draft.config} spellCheck={false} onChange={(e) => patch({ config: e.target.value })} />
              </Form.Item>
              <Space className='mb-2'>
                <b>指标说明</b>
                <Button size='small' onClick={() => patchPack({ metrics: [...pack.metrics, { name: '', expression: '', unit: '', note: '' }] })}>
                  添加指标
                </Button>
              </Space>
              <Table
                rowKey={(_, i) => String(i)}
                pagination={false}
                dataSource={pack.metrics}
                columns={[
                  ...(['name', 'expression', 'unit', 'note'] as const).map((key, i) => ({
                    title: ['指标名称', '查询表达式', '单位', '说明'][i],
                    render: (_, row, index) => (
                      <Input
                        aria-label={`${key}-${index}`}
                        value={row[key]}
                        onChange={(e) => patchPack({ metrics: pack.metrics.map((m, n) => (n === index ? { ...m, [key]: e.target.value } : m)) })}
                      />
                    ),
                  })),
                  {
                    title: '',
                    render: (_, row, index) => (
                      <Button danger size='small' onClick={() => patchPack({ metrics: pack.metrics.filter((_, i) => i !== index) })}>
                        删除
                      </Button>
                    ),
                  },
                ]}
              />
            </Form>
          )}
          {(section === 3 || section === 4) && (
            <>
              <Alert
                className='mb-3'
                type='info'
                message={section === 3 ? '发布后，每台设备创建独立仪表盘。编辑此草稿不会修改已有设备。' : '告警使用夜莺现有规则与通知引擎。未映射有效通知规则时只保留事件。'}
              />
              <Button className='mb-3' onClick={() => editResource(section === 3 ? 'dashboards' : 'alerts', pack[section === 3 ? 'dashboards' : 'alerts'].length)}>
                添加{section === 3 ? '仪表盘' : '告警规则'}
              </Button>
              <Table
                rowKey='id'
                pagination={false}
                dataSource={pack[section === 3 ? 'dashboards' : 'alerts']}
                columns={[
                  { title: '名称', dataIndex: 'name' },
                  {
                    title: '操作',
                    render: (_, row, index) => (
                      <Space>
                        <Button size='small' onClick={() => importResource(row, section === 3 ? 'dashboards' : 'alerts')}>
                          导入到业务组
                        </Button>
                        <Button size='small' onClick={() => editResource(section === 3 ? 'dashboards' : 'alerts', index)}>
                          编辑模板内容
                        </Button>
                        <Button
                          danger
                          size='small'
                          onClick={() => {
                            const kind = section === 3 ? 'dashboards' : 'alerts';
                            patchPack({ [kind]: pack[kind].filter((_, i) => i !== index) });
                          }}
                        >
                          删除
                        </Button>
                      </Space>
                    ),
                  },
                ]}
              />
              {section === 4 && (
                <Form layout='vertical' className='mt-4'>
                  <Form.Item label='默认通知方案'>
                    <Select
                      value={notify.mode}
                      options={[
                        { value: 'events_only', label: '仅产生告警事件（不发送通知）' },
                        { value: 'inherit', label: '继承模板指定的通知规则' },
                        { value: 'rules', label: '使用指定通知规则' },
                      ]}
                      onChange={(mode) => patchNotify({ mode })}
                    />
                  </Form.Item>
                  {notify.mode !== 'events_only' && (
                    <Form.Item label='通知规则'>
                      <Select
                        mode='multiple'
                        value={notify.rule_ids}
                        options={notificationRules.map((r) => ({ value: r.id, label: r.name }))}
                        onChange={(rule_ids) => patchNotify({ rule_ids })}
                      />
                    </Form.Item>
                  )}
                  <Form.Item label='消息内容' extra='消息随设备告警发送；投递格式和渠道由所选夜莺通知规则控制。'>
                    <Input.TextArea rows={5} value={notify.message} onChange={(e) => patchNotify({ message: e.target.value })} />
                  </Form.Item>
                </Form>
              )}
            </>
          )}
        </div>
      </div>
      <Modal
        title={fieldIndex === draft.fields.length ? '添加设备字段' : '编辑设备字段'}
        visible={fieldIndex !== undefined}
        width={760}
        onCancel={() => setFieldIndex(undefined)}
        onOk={async () => {
          try {
            const { default_json, options_text, conditions, ...values } = await fieldForm.validateFields();
            const field: DeviceField = {
              ...values,
              options: options_text?.split('\n').filter(Boolean),
              when: conditions || [],
              ...(default_json ? { default: JSON.parse(default_json) } : {}),
            };
            if (field.secret || field.type === 'password' || field.type === 'credential') delete field.default;
            const fields = [...draft.fields];
            fields[fieldIndex!] = field;
            patch({ fields });
            setFieldIndex(undefined);
          } catch (e) {
            if (!(e as any).errorFields) message.error('默认值必须是有效 JSON，如 3306、"off" 或 true');
          }
        }}
      >
        <Form form={fieldForm} layout='vertical'>
          <Space align='start'>
            <Form.Item name='key' label='参数键' rules={[{ required: true, pattern: /^[a-zA-Z_][a-zA-Z0-9_]*$/ }]}>
              <Input />
            </Form.Item>
            <Form.Item name='label' label='字段名称' rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name='type' label='控件类型'>
              <Select style={{ width: 150 }} options={controls.map((value) => ({ value, label: value }))} />
            </Form.Item>
          </Space>
          <Space>
            <Form.Item name='required' valuePropName='checked'>
              <Checkbox>必填</Checkbox>
            </Form.Item>
            <Form.Item name='secret' valuePropName='checked'>
              <Checkbox>凭据（加密保存）</Checkbox>
            </Form.Item>
          </Space>
          <Space align='start'>
            <Form.Item name='min' label='最小值'>
              <InputNumber />
            </Form.Item>
            <Form.Item name='max' label='最大值'>
              <InputNumber />
            </Form.Item>
            <Form.Item name='group' label='字段分组'>
              <Input />
            </Form.Item>
          </Space>
          <Form.Item name='default_json' label='默认值（JSON；凭据不能设置默认值）'>
            <Input />
          </Form.Item>
          <Form.Item name='options_text' label='可选值（每行一个）'>
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name='help' label='填写提示'>
            <Input />
          </Form.Item>
          <b>显示条件（全部满足）</b>
          <Form.List name='conditions'>
            {(fields, { add, remove }) => (
              <>
                {fields.map(({ key, name }) => (
                  <Space key={key} align='start'>
                    <Form.Item name={[name, 'key']} rules={[{ required: true }]}>
                      <Select
                        placeholder='上游字段'
                        style={{ width: 190 }}
                        options={draft.fields
                          .slice(0, fieldIndex)
                          .filter((f) => !f.secret && f.type !== 'password')
                          .map((f) => ({ value: f.key, label: f.label }))}
                      />
                    </Form.Item>
                    <Form.Item name={[name, 'values']} rules={[{ required: true }]}>
                      <Select mode='tags' placeholder='匹配任一值' style={{ width: 240 }} />
                    </Form.Item>
                    <Button onClick={() => remove(name)}>移除</Button>
                  </Space>
                ))}
                <Button onClick={() => add()}>添加条件</Button>
              </>
            )}
          </Form.List>
        </Form>
      </Modal>
      <Modal
        visible={!!resource}
        title={resource?.kind === 'dashboards' ? '编辑仪表盘模板' : '编辑告警规则模板'}
        width={960}
        onCancel={() => setResource(undefined)}
        onOk={async () => {
          try {
            const values = await resourceForm.validateFields();
            JSON.parse(values.content);
            const items = [...pack[resource!.kind]];
            items[resource!.index] = { ...items[resource!.index], ...values, id: items[resource!.index]?.id || -Date.now() } as PackResource;
            patchPack({ [resource!.kind]: items });
            setResource(undefined);
          } catch (e) {
            if (!(e as any).errorFields) message.error('模板内容必须是有效的夜莺 JSON');
          }
        }}
      >
        <Form form={resourceForm} layout='vertical'>
          <Form.Item name='name' label='模板名称' rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name='content' label='夜莺原生模板内容（JSON）' rules={[{ required: true }]}>
            <Input.TextArea rows={22} spellCheck={false} style={{ fontFamily: 'monospace' }} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
