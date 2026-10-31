import './style.less';
import { formError, errorText } from './errors';
import React, { useContext, useEffect, useRef, useState } from 'react';
import { Alert, Button, Checkbox, Collapse, Form, Input, InputNumber, Modal, Select, Space, Switch, Steps, Descriptions, Divider, Table, Tag } from 'antd';
import { useTranslation } from 'react-i18next';
import { CommonStateContext } from '@/App';
import { api as inventory } from '@/pages/cmdb/api';
import type { Device as Asset } from '@/pages/cmdb/model';
import { collectionRequest, Collector, Credential, Device, getCollectors, getCredentials, Probe, requestKey, saveDevice } from '@/services/collection';
import TemplateSelection, { legacyFields, resetTemplateParameters } from './TemplateSelection';
import { activeFields, adapterNames, Catalog, CatalogEntry, entryDefinition, executionNames, getCatalog, getCatalogEntry, NotificationPreset, PackResource } from '@/pages/builtInComponents/catalog';
import TemplateFields from './TemplateFields';
import { getItems as getNotificationRules } from '@/pages/notificationRules/services';
import { RequestMethod } from '@/store/common';

interface Props {
  device?: Device;
  onClose: () => void;
  onSaved: (d: Device) => void;
}
export default function DeviceEditor({ device, onClose, onSaved }: Props) {
  const { t } = useTranslation('collection');
  const { busiGroups, profile: account, perms } = useContext(CommonStateContext);
  const [form] = Form.useForm();
  const [fullEntry, setFullEntry] = useState<CatalogEntry>();
  const [loadingPack, setLoadingPack] = useState(false);
  const [catalog, setCatalog] = useState<Catalog>();
  const [collectors, setCollectors] = useState<Collector[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [step, setStep] = useState(0);
  const [notifyEdited, setNotifyEdited] = useState(false);
  const [notificationRules, setNotificationRules] = useState<{ id: number; name: string }[]>([]);
  const [upgradeDiff, setUpgradeDiff] = useState<{ resources: { kind: string; name: string; changed: string[]; conflicts: string[] }[] }>();
  const [resourceOverrides, setResourceOverrides] = useState<{ dashboards?: PackResource[]; alerts?: PackResource[] }>({});
  const [summary, setSummary] = useState<Record<string, any>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [assets, setAssets] = useState<Asset[]>([]);
  const [canReadAssets, setCanReadAssets] = useState(false);
  const [probe, setProbe] = useState<Probe>();
  const [credentialOpen, setCredentialOpen] = useState(false);
  const [credentialForm] = Form.useForm();
  const [credentialBusy, setCredentialBusy] = useState(false);
  const [customize, setCustomize] = useState(false);
  const key = useRef(requestKey());
  const group = Form.useWatch('group_id', form);
  const selectedVersion = Form.useWatch('template_version_id', form);
  const selection = Form.useWatch('template_selection', form);
  const chosen = catalog?.entries.find((e) => selection === `catalog:${e.component_id}`);
  useEffect(() => {
    let active = true;
    setFullEntry(undefined);
    if (chosen && (!device || selectedVersion !== device.template_version_id)) {
      setLoadingPack(true);
      getCatalogEntry(chosen.component_id)
        .then((value) => {
          if ((value.published_version_id || undefined) !== selectedVersion) throw new Error('模板发布状态已变化，请重新打开设备配置');
          if (active) setFullEntry(value);
        })
        .catch((e) => {
          if (active) setError(errorText(e, '无法加载模板完整内容'));
        })
        .finally(() => {
          if (active) setLoadingPack(false);
        });
    } else setLoadingPack(false);
    return () => {
      active = false;
    };
  }, [chosen?.component_id, selectedVersion]);
  const selectedDefinition =
    device && selectedVersion === device.template_version_id
      ? device.profile?.catalog?.definition
      : fullEntry && fullEntry.component_id === chosen?.component_id && (fullEntry.published_version_id || undefined) === selectedVersion
      ? entryDefinition(fullEntry)
      : chosen && entryDefinition(chosen);
  const pack = selectedDefinition?.pack;
  const fieldSchema = chosen || selectedVersion ? selectedDefinition?.fields || (device && selectedVersion === device.template_version_id ? legacyFields : []) : [];
  const retainedSnapshot = !!device && selectedVersion === device.template_version_id;
  const activeEntry = fullEntry?.component_id === chosen?.component_id ? fullEntry || chosen : chosen;
  const canBind = retainedSnapshot || (!!activeEntry?.published && (activeEntry.onboarding?.ready ?? ['snmp_v2c', 'mysql'].includes(activeEntry.published.definition.adapter)));
  const fetchOptions = () =>
    Promise.all([getCollectors(), getCredentials(), getCatalog()])
      .then(([cs, cr, catalogData]) => {
        setCatalog(catalogData);
        setCollectors(cs);
        setCredentials(cr);
      })
      .catch((e) => setError(e.message || t('query_failed')));
  useEffect(() => {
    fetchOptions();
    if (account?.admin || perms?.includes('/notification-rules'))
      getNotificationRules()
        .then(setNotificationRules)
        .catch(() => {});
    inventory<{ permissions: { read: boolean } }>('access')
      .then(async (access) => {
        if (access.permissions.read) {
          setCanReadAssets(true);
          setAssets(await inventory<Asset[]>('devices'));
        }
      })
      .catch(() => {});
  }, []);
  useEffect(() => {
    if (!probe || probe.state === 'cleaned') return;
    const timer = window.setInterval(
      () =>
        collectionRequest(`probes/${probe.id}`)
          .then(setProbe)
          .catch((e) => setError(e.message || t('query_failed'))),
      3000,
    );
    return () => window.clearInterval(timer);
  }, [probe?.id, probe?.state]);
  useEffect(() => {
    let active = true;
    setUpgradeDiff(undefined);
    if (device && selectedVersion && selectedVersion !== device.template_version_id)
      collectionRequest(`devices/${device.id}/template-diff?version_id=${encodeURIComponent(selectedVersion)}`)
        .then((value) => {
          if (active) setUpgradeDiff(value);
        })
        .catch((e) => {
          if (active) setError(errorText(e, '无法加载版本差异'));
        });
    return () => {
      active = false;
    };
  }, [device?.id, selectedVersion]);
  const input = async () => {
    const values = await form.validateFields();
    if (!canBind || !selectedVersion) throw new Error('此模板尚不具备自动接入条件，请先查看模板接入状态');
    const { custom_config, custom_metrics, custom_dashboards, custom_alerts, keep_conflicts, template_selection, ...fields } = values;
    const resources = { ...resourceOverrides };
    if (custom_dashboards !== undefined && form.isFieldTouched('custom_dashboards')) resources.dashboards = JSON.parse(custom_dashboards);
    if (custom_alerts !== undefined && form.isFieldTouched('custom_alerts')) resources.alerts = JSON.parse(custom_alerts);
    if (step > 0 && upgradeDiff?.resources.some((r) => r.conflicts.length) && !values.keep_conflicts && !values.reset_resources) throw new Error('请先确认模板升级冲突的处理方式');
    const schema = activeFields(fieldSchema, { ...values.parameters, protocol: values.protocol });
    const parameters = Object.fromEntries(
      schema
        .filter((f) => !['address', 'port', 'protocol', 'credential_id'].includes(f.key))
        .filter((f) => values.parameters?.[f.key] !== undefined)
        .map((f) => [f.key, values.parameters[f.key]]),
    );
    return {
      ...fields,
      parameters,
      notify: device && !notifyEdited ? undefined : fields.notify,
      ...(keep_conflicts ? { upgrade_resolution: 'keep_device' } : {}),
      ...(customize || Object.keys(resources).length
        ? { customization: { ...(customize ? { config: custom_config, metrics: JSON.parse(custom_metrics || '[]') } : {}), ...resources } }
        : {}),
      labels: JSON.parse(values.labels || '{}'),
      protocol: selectedDefinition?.adapter || values.protocol || device?.protocol || 'snmp_v2c',
      record_version: device?.record_version || 0,
      cmdb_id: values.cmdb_id || '',
    };
  };
  const save = async () => {
    setBusy(true);
    setError('');
    try {
      const d = await saveDevice(device?.id, await input(), key.current);
      onSaved(d);
    } catch (e) {
      if (!formError(e)) setError(errorText(e, t('query_failed')));
    } finally {
      setBusy(false);
    }
  };
  const test = async () => {
    setBusy(true);
    setError('');
    try {
      const p = await collectionRequest('probes', RequestMethod.Post, { ...(await input()), probe_device_id: device?.id });
      setProbe(p);
    } catch (e) {
      if (!formError(e)) setError(errorText(e, t('query_failed')));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Modal
      visible
      title={t(device ? 'edit' : 'add')}
      width={1120}
      onCancel={onClose}
      destroyOnClose
      footer={
        <Space>
          <Button onClick={onClose}>{t('cancel')}</Button>
          {step > 0 && <Button onClick={() => setStep(step - 1)}>上一步</Button>}
          {step === 0 && canBind && ['snmp_v2c', 'mysql'].includes(selectedDefinition?.adapter || '') && (
            <Button disabled={!!probe && probe.state !== 'cleaned'} loading={busy} onClick={test}>
              {t('test')}
            </Button>
          )}
          {step < 2 ? (
            <Button
              type='primary'
              disabled={loadingPack || !canBind || (!!selectedVersion && !pack && !retainedSnapshot)}
              onClick={async () => {
                try {
                  const values = await input();
                  setSummary({ ...values, notify: values.notify || device?.notify });
                  setError('');
                  setStep(step + 1);
                } catch (e) {
                  if (!formError(e)) setError(errorText(e, '请检查填写内容'));
                }
              }}
            >
              下一步
            </Button>
          ) : (
            <Button type='primary' loading={busy} onClick={save}>
              {device ? '确认保存' : '确认添加设备'}
            </Button>
          )}
        </Space>
      }
    >
      <Steps current={step} className='mb-5'>
        {['设备类型与采集配置', '仪表盘与告警通知', '确认添加'].map((title) => (
          <Steps.Step key={title} title={title} />
        ))}
      </Steps>
      {error && <Alert className='mb-2' type='error' showIcon message={error} />}
      <Form
        form={form}
        layout='vertical'
        onValuesChange={(changed) => {
          if (changed.notify) setNotifyEdited(true);
        }}
        initialValues={
          device
            ? {
                name: device.name,
                address: device.address,
                port: device.port,
                ident: device.ident,
                group_id: device.group_id,
                collector_id: device.collector_id,
                credential_id: device.credential_id,
                interval_seconds: device.interval_seconds,
                timeout_seconds: device.timeout_seconds,
                retries: device.retries,
                cmdb_id: device.cmdb_id,
                enabled: device.enabled,
                template_version_id: device.template_version_id,
                protocol: device.protocol,
                labels: JSON.stringify(device.labels),
                parameters: device.parameters || {},
                notify: device.notify || device.profile?.catalog?.definition.notify || { mode: 'events_only', rule_ids: [], message: '' },
              }
            : {
                port: 161,
                interval_seconds: 30,
                timeout_seconds: 3,
                retries: 1,
                labels: '{}',
                enabled: true,
                parameters: {},
                notify: { mode: 'events_only', rule_ids: [], message: '' },
              }
        }
      >
        <div className='collection-wizard-layout'>
          <div>
            <div style={{ display: step === 0 ? undefined : 'none' }}>
              <TemplateSelection
                catalog={catalog}
                form={form}
                device={device}
                onSelection={() => {
                  setCustomize(false);
                  setResourceOverrides({});
                  setNotifyEdited(false);
                  if (device) form.setFieldsValue({ notify: device.notify || { mode: 'events_only', rule_ids: [], message: '' } });
                  form.setFieldsValue({ custom_dashboards: undefined, custom_alerts: undefined, keep_conflicts: false });
                }}
              />
              {chosen && !retainedSnapshot && !canBind && (
                <Alert
                  className='mb-3'
                  type='warning'
                  showIcon
                  message='模板接入状态：待完成'
                  description={
                    <div>
                      <p>接入方式：{executionNames[chosen.onboarding?.execution || selectedDefinition?.execution || 'collector'] || '按采集说明配置'}</p>
                      <ul>{(chosen.onboarding?.issues || ['尚未发布监控包']).map((issue) => <li key={issue}>{issue}</li>)}</ul>
                      <p>下方展示此模板的表单草稿，尚不能保存或下发采集。请在模板中心补齐并发布监控包。</p>
                    </div>
                  }
                />
              )}
              {chosen && !canBind && (
                <Collapse className='mb-3'>
                  <Collapse.Panel header='查看原模板的采集说明、仪表盘与告警规则' key={`guide-${chosen.component_id}`}>
                    <p>指标说明 {pack?.metrics.length || 0} 项；仪表盘 {pack?.dashboards.length || 0} 份；告警规则 {pack?.alerts.length || 0} 条。</p>
                    <p>仪表盘：{pack?.dashboards.map((r) => r.name).join('、') || '待补齐'}</p>
                    <p>告警规则：{pack?.alerts.map((r) => r.name).join('、') || '待补齐'}</p>
                    <pre style={{ whiteSpace: 'pre-wrap', maxHeight: 420, overflow: 'auto' }}>{loadingPack ? '加载采集说明…' : pack?.readme || '此模板尚未配置采集说明'}</pre>
                  </Collapse.Panel>
                </Collapse>
              )}
              <Form.Item name='name' label={t('name')} rules={[{ required: true, max: 128 }]}>
                <Input />
              </Form.Item>
              <Space align='start'>
                <Form.Item name='address' label={t('address')} rules={[{ required: true }]}>
                  <Input />
                </Form.Item>
                <Form.Item name='ident' label={t('ident')} extra={t('ident_hint')}>
                  <Input disabled={!!device} placeholder={t('address')} />
                </Form.Item>
              </Space>
              <Form.Item name='group_id' label={t('group')} rules={[{ required: true }]}>
                <Select
                  options={busiGroups.map((g) => ({ label: g.name, value: g.id }))}
                  onChange={() => {
                    const parameters = { ...form.getFieldValue('parameters') };
                    fieldSchema
                      .filter((f) => f.secret || f.type === 'password')
                      .forEach((f) => {
                        parameters[f.key] = '';
                      });
                    resetTemplateParameters(form, parameters);
                    form.setFieldsValue({ collector_id: undefined, credential_id: undefined });
                  }}
                />
              </Form.Item>
              <Form.Item name='collector_id' label={t('collector')} rules={[{ required: true }]}>
                <Select options={collectors.filter((c) => c.group_ids.includes(group)).map((c) => ({ label: `${c.name} (${c.agent_ident})`, value: c.id }))} />
              </Form.Item>
              <Form.Item label='指标数据源'>
                <Input readOnly value={collectors.find((c) => c.id === form.getFieldValue('collector_id'))?.datasource_id || ''} placeholder='随执行采集器的数据源绑定' />
              </Form.Item>
              <Divider orientation='left'>连接与采集参数</Divider>
              <TemplateFields
                form={form}
                fields={fieldSchema}
                credentials={credentials}
                group={group}
                disabled={!!chosen && !canBind}
                savedSecrets={
                  device && group === device.group_id && (selectedVersion === device.template_version_id || chosen?.component_id === device.profile?.component_id)
                    ? device.profile?.secret_fields
                    : undefined
                }
              />
              {fieldSchema.some((f) => f.type === 'credential') && (
                <Button
                  className='mb-3'
                  type='link'
                  disabled={!group}
                  onClick={() => {
                    credentialForm.resetFields();
                    setCredentialOpen(true);
                  }}
                >
                  {t('new_credential')}
                </Button>
              )}
              {device && selectedVersion !== device.template_version_id && (
                <Form.Item name='reset_resources' valuePropName='checked'>
                  <Checkbox>重建仪表盘和告警规则，重置该设备的资源自定义（默认保留）</Checkbox>
                </Form.Item>
              )}
              <Collapse className='mb-3'>
                <Collapse.Panel header={t('profile_customize')} key='customize'>
                  <p>{t('profile_customize_hint')}</p>
                  <Checkbox
                    checked={customize}
                    onChange={(e) => {
                      setCustomize(e.target.checked);
                      if (e.target.checked) {
                        const selected = form.getFieldValue('template_version_id');
                        const source =
                          device?.profile && selected === device.template_version_id
                            ? device.profile
                            : { config: chosen?.published?.definition.config, metrics: chosen?.published_metrics || [] };
                        form.setFieldsValue({ custom_config: source?.config || '', custom_metrics: JSON.stringify(source?.metrics || [], null, 2) });
                      }
                    }}
                  >
                    {t('profile_customize_enable')}
                  </Checkbox>
                  {customize && (
                    <>
                      <Form.Item name='custom_config' label={t('template_config')} rules={[{ required: true }]} extra={t('template_config_hint')}>
                        <Input.TextArea rows={12} spellCheck={false} />
                      </Form.Item>
                      <Form.Item
                        name='custom_metrics'
                        label={t('profile_metrics_json')}
                        rules={[
                          {
                            validator: (_, value) => {
                              try {
                                const parsed = JSON.parse(value);
                                if (!Array.isArray(parsed) || parsed.some((m) => !m.name || !m.expression)) throw new Error();
                                return Promise.resolve();
                              } catch {
                                return Promise.reject(new Error(t('json_invalid')));
                              }
                            },
                          },
                        ]}
                      >
                        <Input.TextArea rows={8} spellCheck={false} />
                      </Form.Item>
                    </>
                  )}
                </Collapse.Panel>
              </Collapse>
              <Space align='start'>
                <Form.Item name='interval_seconds' label={t('interval')}>
                  <Select style={{ width: 150 }} options={[15, 30, 60, 120].map((v) => ({ label: v, value: v }))} />
                </Form.Item>
                <Form.Item name='timeout_seconds' label={t('timeout')}>
                  <InputNumber min={1} max={selectedDefinition?.adapter === 'mysql' ? 60 : 10} />
                </Form.Item>
                <Form.Item name='retries' label={t('retries')}>
                  <InputNumber min={0} max={3} />
                </Form.Item>
              </Space>
              <Form.Item
                name='labels'
                label={t('labels')}
                rules={[
                  {
                    validator: (_, value) => {
                      try {
                        const v = JSON.parse(value || '{}');
                        if (!v || Array.isArray(v) || typeof v !== 'object' || Object.values(v).some((n) => typeof n !== 'string')) throw new Error();
                        return Promise.resolve();
                      } catch {
                        return Promise.reject(new Error(t('json_invalid')));
                      }
                    },
                  },
                ]}
              >
                <Input.TextArea rows={2} />
              </Form.Item>
              <Form.Item name='cmdb_id' label={t('asset')}>
                <Select
                  allowClear
                  showSearch
                  optionFilterProp='label'
                  disabled={!canReadAssets && !device?.cmdb_id}
                  options={[
                    ...assets.map((a) => ({ value: a.id, label: `${a.hostname || a.serial} (${a.serial})` })),
                    ...(device?.cmdb_id && !assets.some((a) => a.id === device.cmdb_id) ? [{ value: device.cmdb_id, label: t('asset_unavailable') }] : []),
                  ]}
                />
              </Form.Item>
              <Form.Item name='enabled' label={t('enable')} valuePropName='checked'>
                <Switch />
              </Form.Item>
            </div>
            <div style={{ display: step === 1 ? undefined : 'none' }}>
              <h3>按模板应用完整监控包</h3>
              {upgradeDiff && (
                <>
                  <h4>版本更新差异</h4>
                  <Table
                    rowKey={(row) => `${row.kind}-${row.name}`}
                    size='small'
                    pagination={false}
                    dataSource={upgradeDiff.resources}
                    columns={[
                      { title: '资源', dataIndex: 'name' },
                      { title: '将更新的字段', render: (_, r) => r.changed.join('、') || '保持设备现值' },
                      { title: '冲突 / 设备自定义', render: (_, r) => r.conflicts.join('、') || '无冲突' },
                    ]}
                  />
                  {upgradeDiff.resources.some((r) => r.conflicts.length) && (
                    <Form.Item name='keep_conflicts' valuePropName='checked'>
                      <Checkbox>保留以上冲突字段的设备自定义，应用其余模板更新</Checkbox>
                    </Form.Item>
                  )}
                </>
              )}
              <Alert className='mb-3' type='info' message='采集与指标说明随绑定保存；仪表盘与告警规则创建为本设备的独立实例。' />
              <p>
                <b>仪表盘：</b>
                {(resourceOverrides.dashboards || pack?.dashboards || []).map((r) => r.name).join('、') || '保留设备现有仪表盘'}
              </p>
              <Table
                size='small'
                pagination={false}
                rowKey='id'
                dataSource={resourceOverrides.alerts || pack?.alerts || []}
                columns={[
                  { title: '默认告警规则', dataIndex: 'name' },
                  {
                    title: '持续时间',
                    render: (_, row) => {
                      try {
                        return `${JSON.parse(row.content).prom_for_duration || 0} 秒`;
                      } catch {
                        return '模板格式错误';
                      }
                    },
                  },
                  {
                    title: '条件',
                    render: (_, row) => {
                      try {
                        return (JSON.parse(row.content).rule_config?.queries || []).map((q) => q.prom_ql).join('；');
                      } catch {
                        return '模板格式错误';
                      }
                    },
                  },
                ]}
              />
              {!device && pack && (
                <Collapse className='mt-3 mb-3'>
                  <Collapse.Panel header='仅调整本设备的仪表盘与告警规则' key='resources'>
                    {(['dashboards', 'alerts'] as const).map((kind) => (
                      <Form.Item
                        name={`custom_${kind}`}
                        initialValue={JSON.stringify(pack[kind], null, 2)}
                        label={kind === 'dashboards' ? '独立仪表盘模板' : '独立告警规则模板'}
                        key={`${selectedVersion}-${kind}`}
                        rules={[
                          {
                            validator: (_, value) => {
                              try {
                                if (!Array.isArray(JSON.parse(value))) throw new Error();
                                return Promise.resolve();
                              } catch {
                                return Promise.reject(new Error('请输入有效的夜莺模板数组'));
                              }
                            },
                          },
                        ]}
                      >
                        <Input.TextArea rows={7} />
                      </Form.Item>
                    ))}
                  </Collapse.Panel>
                </Collapse>
              )}
              <Divider orientation='left'>报警通知方案</Divider>
              <Form.Item name={['notify', 'mode']} label='设备通知映射'>
                <Select
                  options={[
                    { value: 'events_only', label: '仅产生告警事件，不发送通知' },
                    { value: 'inherit', label: '继承所选模板的通知方案' },
                    { value: 'rules', label: '为本设备指定通知规则' },
                  ]}
                />
              </Form.Item>
              <Form.Item noStyle shouldUpdate={(previous, next) => previous.notify?.mode !== next.notify?.mode}>
                {() =>
                  form.getFieldValue(['notify', 'mode']) === 'rules' ? (
                    <Form.Item name={['notify', 'rule_ids']} label='夜莺通知规则' rules={[{ required: true, message: '请选择通知规则' }]}>
                      <Select mode='multiple' options={notificationRules.map((r) => ({ value: r.id, label: r.name }))} />
                    </Form.Item>
                  ) : null
                }
              </Form.Item>
              <Form.Item name={['notify', 'message']} label='消息内容' extra='随设备告警发送；投递格式和渠道由所选夜莺通知规则控制。'>
                <Input.TextArea rows={4} maxLength={1024} />
              </Form.Item>
              <Alert type='info' message='没有映射有效通知规则时，只生成告警事件。保存后仍需等待采集器返回必需指标，才显示采集正常。' />
            </div>
            {step === 2 && (
              <>
                <h3>确认设备添加方案</h3>
                <Descriptions column={1} bordered size='small'>
                  <Descriptions.Item label='对象名称'>{summary.name}</Descriptions.Item>
                  <Descriptions.Item label='模板 / 发布版本'>
                    {chosen?.name || device?.profile?.name} · v{chosen?.published?.version || device?.profile?.catalog?.version}
                  </Descriptions.Item>
                  <Descriptions.Item label='执行采集器'>{collectors.find((c) => c.id === summary.collector_id)?.name}</Descriptions.Item>
                  <Descriptions.Item label='采集方式'>{selectedDefinition && adapterNames[selectedDefinition.adapter]}</Descriptions.Item>
                  <Descriptions.Item label='地址 / 周期'>
                    {summary.address} · {summary.interval_seconds} 秒
                  </Descriptions.Item>
                  <Descriptions.Item label='资源'>
                    {(summary.customization?.dashboards || pack?.dashboards || device?.profile?.boards || []).length} 个仪表盘 ·{' '}
                    {(summary.customization?.alerts || pack?.alerts || device?.profile?.rules || []).length} 条告警规则
                  </Descriptions.Item>
                  <Descriptions.Item label='通知'>
                    {summary.notify?.mode === 'events_only' || !summary.notify?.rule_ids?.length
                      ? '仅事件；通知未配置'
                      : `通知规则：${summary.notify.rule_ids.map((id) => notificationRules.find((r) => r.id === id)?.name || id).join('、')}`}
                  </Descriptions.Item>
                </Descriptions>
                <Divider orientation='left'>脱敏参数</Divider>
                <Descriptions column={1} bordered size='small'>
                  {activeFields(fieldSchema, { ...summary.parameters, protocol: summary.protocol }).map((field) => (
                    <Descriptions.Item key={field.key} label={field.label}>
                      {field.secret || field.type === 'password' ? '••••••（加密保存）' : String(summary[field.key] ?? summary.parameters?.[field.key] ?? '')}
                    </Descriptions.Item>
                  ))}
                </Descriptions>
                <Alert className='mt-3' type='info' message='保存后登记设备并等待配置下发。相同 IP 的其他监控对象独立运行。' />
              </>
            )}
          </div>
          <aside className='collection-wizard-summary'>
            <h3>本次设备方案</h3>
            <p>{chosen?.name || device?.profile?.name || '请完成三级分类选择'}</p>
            <p>{selectedDefinition && adapterNames[selectedDefinition.adapter]}</p>
            <p>
              采集配置与指标说明
              <br />
              独立仪表盘
              <br />
              告警规则与通知方案
            </p>
            <Tag>{chosen?.published ? `发布 v${chosen.published.version}` : chosen ? '草稿 · 待接入' : device?.profile?.catalog ? `绑定 v${device.profile.catalog.version}` : '尚未选择模板'}</Tag>
          </aside>
        </div>
      </Form>
      {probe && (
        <Alert
          type='info'
          message={`${t('probe')}: ${t(probe.state)}${probe.result ? ` / ${t(probe.result === 'timeout' ? 'timeout_result' : probe.result)}` : ''}`}
          description={
            <>
              <p>{t('probe_hint')}</p>
              {probe.state !== 'cleaned' && (
                <Button
                  onClick={() =>
                    collectionRequest(`probes/${probe.id}`, RequestMethod.Delete)
                      .then(() => collectionRequest(`probes/${probe.id}`))
                      .then(setProbe)
                      .catch((e) => setError(e.message))
                  }
                >
                  {t('test_cancel')}
                </Button>
              )}
            </>
          }
        />
      )}
      <Modal
        visible={credentialOpen}
        title={t('new_credential')}
        onCancel={() => {
          setCredentialOpen(false);
          credentialForm.resetFields();
        }}
        confirmLoading={credentialBusy}
        onOk={async () => {
          setCredentialBusy(true);
          try {
            const v = await credentialForm.validateFields();
            const c = await collectionRequest('credentials', RequestMethod.Post, { ...v, group_id: group });
            credentialForm.resetFields();
            setCredentialOpen(false);
            await fetchOptions();
            form.setFieldsValue({ credential_id: c.id });
          } catch (e) {
            if (!formError(e)) setError(errorText(e, t('query_failed')));
          } finally {
            setCredentialBusy(false);
          }
        }}
      >
        <Form form={credentialForm} layout='vertical'>
          <Form.Item name='name' label={t('credential_name')} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name='community' label={t('community')} rules={[{ required: true }]}>
            <Input.Password autoComplete='new-password' />
          </Form.Item>
        </Form>
      </Modal>
    </Modal>
  );
}
