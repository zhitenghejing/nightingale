import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, Descriptions, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, Tabs, message } from 'antd';
import { Link, useHistory, useParams } from 'react-router-dom';
import PageLayout from '@/components/pageLayout';
import { api, Candidate, Config, Detail, errorText, OpsCase, Spec, state, time } from '../opsShared/api';
import '../opsShared/style.less';
import CasePlans from './CasePlans';

export default function OpsCases() {
  const { id } = useParams<{ id?: string }>();
  const history = useHistory();
  const [config, setConfig] = useState<Config>();
  const [group, setGroup] = useState<number>();
  const [filter, setFilter] = useState('');
  const [rows, setRows] = useState<OpsCase[]>([]);
  const [detail, setDetail] = useState<Detail>();
  const [matches, setMatches] = useState<Candidate[]>([]);
  const [adopted, setAdopted] = useState<Spec>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [policies, setPolicies] = useState<
    { group_id: number; revision: number; enabled: boolean; auto_investigate: boolean; service_user_id: number; rule_ids: number[]; health: string; gap_since: number }[]
  >([]);
  const [dialog, setDialog] = useState<{ kind: string; title: string; target?: string | number }>();
  const [form] = Form.useForm();
  const epoch = useRef(0);
  const load = useCallback(async () => {
    const seq = ++epoch.current;
    try {
      const cfg = await api<Config>('/config');
      if (seq !== epoch.current) return;
      setConfig(cfg);
      const policyRows = await api<typeof policies>('/policies');
      if (seq !== epoch.current) return;
      setPolicies(policyRows);
      if (id) {
        const d = await api<Detail>(`/cases/${id}`);
        const m = await api<Candidate[]>(`/cases/${id}/matches`);
        const version = d.case.knowledge_version_id ? await api<{ spec: Spec }>(`/versions/${d.case.knowledge_version_id}`) : undefined;
        if (seq !== epoch.current) return;
        setDetail(d);
        setMatches(m);
        setAdopted(d.case.plan_id ? d.plans.find((p) => p.id === d.case.plan_id)?.payload : version?.spec);
      } else {
        const data = await api<OpsCase[]>(`/cases?${new URLSearchParams({ ...(group ? { group_id: String(group) } : {}), state: filter })}`);
        if (seq === epoch.current) setRows(data);
      }
      if (seq === epoch.current) setError('');
    } catch (e) {
      if (seq === epoch.current) setError(errorText(e));
    }
  }, [id, group, filter]);
  useEffect(() => {
    load();
    const timer = setInterval(load, 10000);
    return () => {
      clearInterval(timer);
      epoch.current++;
    };
  }, [load]);
  const act = async (path: string, data?: unknown, method = 'POST') => {
    setBusy(true);
    try {
      const result = await api<unknown>(path, method, data);
      await load();
      return result;
    } catch (e) {
      message.error(errorText(e));
      throw e;
    } finally {
      setBusy(false);
    }
  };
  const run = (path: string, data?: unknown, method?: string) => {
    act(path, data, method).catch(() => {});
  };
  const open = (kind: string, title: string, target?: string | number, initial: Record<string, unknown> = {}) => {
    form.resetFields();
    form.setFieldsValue(initial);
    setDialog({ kind, title, target });
  };
  const submit = async () => {
    const value = await form.validateFields();
    if (!dialog) return;
    const base = `/cases/${id}`;
    try {
      if (dialog.kind === 'create') {
        const created = await api<OpsCase>('/cases', 'POST', { ...value, context: {} });
        setDialog(undefined);
        history.push(`/ops/cases/${created.id}`);
        return;
      }
      if (dialog.kind === 'policy') {
        const previous = policies.find((p) => p.group_id === value.group_id);
        await act(
          '/policies',
          {
            ...value,
            revision: previous?.revision || 0,
            rule_ids: String(value.rule_ids_text || '')
              .split(/[\s,]+/)
              .filter(Boolean)
              .map(Number),
          },
          'PUT',
        );
      }
      if (dialog.kind === 'ack-gap') await act(`/policies/${dialog.target}/ack-gap`, value);
      if (dialog.kind === 'merge') {
        const source = await api<Detail>(`/cases/${value.source_id}`);
        await act(base + '/merge', { ...value, revision: detail?.case.revision, source_revision: source.case.revision });
      }
      if (dialog.kind === 'demo-setup') await act('/demo/setup', value);
      if (dialog.kind === 'demo-fault') await act('/demo/fault', value);
      if (dialog.kind === 'context')
        await act(
          base + '/context',
          {
            revision: detail?.case.revision,
            context: { system_version: value.system_version || '', environment: value.environment || '', business: value.business || '' },
            wait_reason: value.wait_reason || '',
          },
          'PUT',
        );
      if (dialog.kind === 'manual') await act(base + '/evidence/manual', value);
      if (dialog.kind === 'check') await act(base + `/checks/${dialog.target}`, value);
      if (dialog.kind === 'action-result') await act(base + `/actions/${dialog.target}/result`, value);
      if (dialog.kind === 'close') await act(base + '/close', { ...value, revision: detail?.case.revision });
      if (dialog.kind === 'feedback') await act(base + '/feedback', value);
      if (dialog.kind === 'collect') await act(base + '/collect', value);
      if (dialog.kind === 'step') await act(base + '/steps', { ...value, key: dialog.target });
      if (dialog.kind === 'assign') await act(base + '/assign', { ...value, revision: detail?.case.revision });
      if (dialog.kind === 'select') await act(base + '/knowledge', { ...value, version_id: dialog.target, revision: detail?.case.revision });
      setDialog(undefined);
    } catch (e) {
      if (dialog.kind === 'create') message.error(errorText(e));
    }
  };
  const c = detail?.case;
  const writable = !!config?.groups.find((g) => g.id === c?.group_id)?.writable;
  const closed = c?.state === 'closed';
  const base = `/cases/${id}`;
  return (
    <PageLayout title='告警处置' showBack={!!id} backPath='/ops/cases'>
      <div className='ops-page'>
        {error && <Alert type='error' showIcon message={error} action={<Button onClick={load}>重试</Button>} style={{ marginBottom: 16 }} />}
        <div className='ops-toolbar'>
          <Link to='/ops/knowledge'>管理处置知识</Link>
          <Button onClick={load}>刷新</Button>
          {!id && (
            <>
              <Select
                allowClear
                placeholder='全部可访问业务组'
                style={{ minWidth: 220 }}
                value={group}
                onChange={setGroup}
                options={config?.groups.map((g) => ({ value: g.id, label: g.name }))}
              />
              <Select
                value={filter}
                onChange={setFilter}
                style={{ minWidth: 140 }}
                options={['', 'pending', 'processing', 'verifying', 'closed'].map((v) => ({ value: v, label: v ? state(v) : '全部状态' }))}
              />
              <Button type='primary' onClick={() => open('create', '新建处置', undefined, { group_id: group || config?.groups.find((g) => g.writable)?.id })}>
                新建处置
              </Button>
            </>
          )}
          {!id && (
            <Button
              onClick={() => {
                const selected = group || config?.groups.find((g) => g.writable)?.id;
                const p = policies.find((item) => item.group_id === selected);
                open('policy', '自动告警接入', undefined, {
                  ...p,
                  group_id: selected,
                  rule_ids_text: p?.rule_ids?.join(', ') || '',
                  enabled: !!p?.enabled,
                  auto_investigate: !!p?.auto_investigate,
                });
              }}
            >
              自动接入设置
            </Button>
          )}
          {!id && config?.demo_enabled && (
            <>
              <Button onClick={() => open('demo-setup', '准备隔离演练场景', undefined, { group_id: group || config.groups[0]?.id })}>准备演练</Button>
              <Button onClick={() => open('demo-fault', '注入或解除测试故障', undefined, { group_id: group || config.groups[0]?.id, kind: 'storage' })}>演练故障</Button>
            </>
          )}
          {c && (
            <>
              <Tag>{state(c.state)}</Tag>
              <span>处置 #{c.id}</span>
              <span>负责人：{c.owner_id || '待分派'}</span>
              {writable && !closed && (
                <>
                  <Button onClick={() => run(base + '/assign', { revision: c.revision, owner_id: config?.user_id })}>认领并接管</Button>
                  <Button onClick={() => open('assign', '转交处置', undefined, { owner_id: c.owner_id })}>转交</Button>
                  <Button onClick={() => open('merge', '关联同一对象的其他处置')}>关联告警</Button>
                </>
              )}
            </>
          )}
        </div>
        {!id &&
          policies
            .filter((p) => p.health && p.health !== 'ok')
            .map((p) => (
              <Alert
                key={p.group_id}
                type='warning'
                showIcon
                message={`业务组 ${p.group_id} 接入状态：${p.health}`}
                action={
                  p.gap_since > 0 && config?.groups.find((g) => g.id === p.group_id)?.writable ? (
                    <Button onClick={() => open('ack-gap', '登记源事件缺口核对', p.group_id)}>登记人工对账</Button>
                  ) : undefined
                }
                style={{ marginBottom: 12 }}
              />
            ))}
        {!id && (
          <Table
            rowKey='id'
            dataSource={rows}
            pagination={{ pageSize: 20 }}
            columns={[
              { title: '处置', dataIndex: 'title', render: (v, row) => <Link to={`/ops/cases/${row.id}`}>{v}</Link> },
              { title: '级别', dataIndex: 'severity', render: (v) => (v ? `P${v}` : '待确认') },
              { title: '状态', dataIndex: 'state', render: (v) => <Tag>{state(v)}</Tag> },
              { title: '监控对象', dataIndex: 'target_ident', render: (v) => v || '未关联' },
              { title: '负责人', dataIndex: 'owner_id', render: (v) => v || '待分派' },
              { title: '等待原因', dataIndex: 'wait_reason', render: (v) => state(v) },
              { title: '更新时间', dataIndex: 'updated_at', render: time },
            ]}
          />
        )}
        {c && (
          <>
            <Card
              title={c.title}
              style={{ marginBottom: 16 }}
              extra={writable && !closed && <Button onClick={() => open('context', '补充现场背景', undefined, { ...c.context, wait_reason: c.wait_reason })}>补充背景</Button>}
            >
              <Descriptions column={{ xs: 1, sm: 1, md: 2, lg: 3 }} size='small'>
                <Descriptions.Item label='监控对象'>{c.target_ident || '未知，需核实'}</Descriptions.Item>
                <Descriptions.Item label='关联告警规则'>{c.rule_id || '未关联'}</Descriptions.Item>
                <Descriptions.Item label='数据源'>{c.datasource_id || '未关联'}</Descriptions.Item>
                <Descriptions.Item label='当前处理依据'>{c.plan_id ? `本次方案 #${c.plan_id}` : c.knowledge_version_id || '待选择'}</Descriptions.Item>
                <Descriptions.Item label='业务范围'>{c.context?.business || '尚未确认'}</Descriptions.Item>
                <Descriptions.Item label='等待原因'>{c.wait_reason ? state(c.wait_reason) : '—'}</Descriptions.Item>
              </Descriptions>
              <div className='ops-meta'>
                {detail.episodes.map((e) => (
                  <Tag key={e.id}>
                    {e.recovered ? '监控已恢复，仍需完成处置验证' : '监控异常'} · {e.incident_id}
                  </Tag>
                ))}
              </div>
              {closed && <Alert type='success' message={c.summary} description={c.followup && `后续跟进：${c.followup}`} />}
            </Card>
            {config && <CasePlans detail={detail} config={config} onChange={load} />}
            <div className='ops-grid'>
              <div className='ops-stack'>
                <Card
                  title='处理依据与排查步骤'
                  extra={
                    !closed && (
                      <Space>
                        <Button disabled={busy} onClick={() => run(base + '/runs', { mode: 'guided' })}>
                          按步骤自动取证
                        </Button>
                        <Button disabled={!config?.model_enabled || busy} onClick={() => run(base + '/runs', { mode: 'agent' })}>
                          Agent 排查
                        </Button>
                      </Space>
                    )
                  }
                >
                  {!c.knowledge_version_id && !c.plan_id && <Alert type='info' message='先核对现场条件，再采用正式知识版本。没有适用知识时可补充证据，编写并确认本次处置方案。' />}
                  {matches.map((m) => (
                    <div key={m.version.id} className='ops-item'>
                      <strong>
                        {m.version.title} · v{m.version.sequence}
                      </strong>
                      <p>{m.spec.summary}</p>
                      <div>
                        {m.conditions.map((condition) => (
                          <Tag key={condition.key} color={condition.state === 'satisfied' ? 'green' : 'orange'}>
                            {condition.key}：{condition.actual || '未知'} / 需要 {condition.expected} · {state(condition.state)}
                          </Tag>
                        ))}
                      </div>
                      {writable && !closed && c.knowledge_version_id !== m.version.id && (
                        <Button disabled={!m.applicable} onClick={() => open('select', '采用知识版本', m.version.id)}>
                          采用此版本
                        </Button>
                      )}
                    </div>
                  ))}
                  {matches.length === 0 && <p>没有找到可用正式知识。可补充背景、查询证据并编写本次处置方案。</p>}
                  {adopted?.steps.map((step, i) => (
                    <div key={step.key} className='ops-item'>
                      <strong>
                        {i + 1}. {step.title}
                      </strong>
                      <p className='ops-text'>{step.instruction}</p>
                      {step.expected && <p>预期：{step.expected}</p>}
                      {(step.next || step.on_failure) && (
                        <p>
                          取得证据后：{step.next || '下一步骤'}；失败转：{step.on_failure || '停止并交由人员处理'}
                        </p>
                      )}
                      {step.kind === 'metric' && (
                        <>
                          <code>{step.query}</code>
                          <br />
                          <Button disabled={closed || busy} onClick={() => run(base + '/collect', { query: step.query, window_seconds: 300 })}>
                            查询并保存证据
                          </Button>
                        </>
                      )}
                      {writable && !closed && (
                        <Button style={{ marginLeft: 8 }} onClick={() => open('step', `登记：${step.title}`, step.key, { state: 'completed', evidence_ids: [] })}>
                          登记检查结果
                        </Button>
                      )}
                    </div>
                  ))}
                  {adopted?.escalation && <Alert type='info' message='升级路径' description={adopted.escalation} />}
                </Card>
                <Card title='操作与确认'>
                  {adopted?.actions.map((a) => (
                    <div className='ops-item' key={a.key}>
                      <strong>{a.title}</strong>
                      <p>{a.instruction}</p>
                      <p>执行前必需检查：{(a.required_steps?.length ? a.required_steps : (adopted?.steps || []).map((s) => s.key)).map((key) => adopted?.steps.find((s) => s.key === key)?.title || key).join('、')}。后端会重新核对完成状态和证据新鲜度。</p>
                      <p>风险：{a.risk}</p>
                      <p>回退：{a.rollback}</p>
                      {writable && !closed && <Button onClick={() => run(base + '/actions', { key: a.key })}>准备操作提案</Button>}
                    </div>
                  ))}
                  {detail.actions.map((a) => (
                    <div className='ops-item' key={a.id}>
                      <Space>
                        <strong>
                          操作 #{a.id} · {a.payload.definition.title}
                        </strong>
                        <Tag>{state(a.state)}</Tag>
                      </Space>
                      <p>
                        目标：{a.payload.target || '人工处理范围'} · 确认有效期：{time(a.expires_at)}
                        <br />
                        证据基线：{a.payload.evidence_max_id ? `截至证据 #${a.payload.evidence_max_id}` : '尚无现场证据，请先采集并重新准备提案'}
                      </p>
                      {Boolean(a.payload.template) && (
                        <details>
                          <summary>查看固定作业内容</summary>
                          <pre className='ops-code'>{JSON.stringify(a.payload.template, null, 2)}</pre>
                        </details>
                      )}
                      {writable && !closed && a.state === 'proposed' && (
                        <Space>
                          <Button
                            type='primary'
                            disabled={a.kind === 'ibex_template' && !config?.execution_enabled}
                            onClick={() =>
                              Modal.confirm({
                                title: '确认执行此操作？',
                                content: (
                                  <div>
                                    <p>目标：{a.payload.target || '请按人工规程核实'}</p>
                                    <p>风险：{a.payload.definition.risk}</p>
                                    <p>回退：{a.payload.definition.rollback}</p>
                                  </div>
                                ),
                                onOk: () => act(base + `/actions/${a.id}/decision`, { accept: true }).then(() => undefined),
                              })
                            }
                          >
                            确认操作
                          </Button>
                          <Button onClick={() => run(base + `/actions/${a.id}/decision`, { accept: false })}>拒绝</Button>
                        </Space>
                      )}
                      {Boolean(a.result) && <pre className='ops-code'>{JSON.stringify(a.result, null, 2)}</pre>}
                      {writable && !closed && ['unknown', 'dispatching', 'awaiting_manual', 'submitted'].includes(a.state) && (
                        <Button onClick={() => open('action-result', '登记核实后的操作结果', a.id, { success: false })}>核实操作结果</Button>
                      )}
                      {a.kind === 'ibex_template' && ['unknown', 'dispatching', 'submitted'].includes(a.state) && (
                        <Button style={{ marginLeft: 8 }} onClick={() => run(base + `/actions/${a.id}/reconcile`)}>
                          查询作业结果
                        </Button>
                      )}
                    </div>
                  ))}
                  {!adopted && <p>采用适用知识或确认本次方案后显示受控操作。</p>}
                </Card>
              </div>
              <div className='ops-stack'>
                <Card
                  title='恢复验证'
                  extra={
                    !closed && (
                      <Button type='primary' disabled={!writable} loading={busy} onClick={() => run(base + '/verify')}>
                        运行恢复检查
                      </Button>
                    )
                  }
                >
                  <p>监控恢复后，还需要完成本场景要求的业务与数据核对。</p>
                  {adopted?.checks.map((check) => {
                    const result = detail.verifications.find((v) => v.check_key === check.key && v.generation === c.generation);
                    return (
                      <div className='ops-item' key={check.key}>
                        <Space>
                          <strong>{check.title}</strong>
                          {check.required && <Tag>必需</Tag>}
                          <Tag color={result?.state === 'passed' ? 'green' : 'orange'}>{state(result?.state || 'pending')}</Tag>
                        </Space>
                        <p>{result?.reason || '尚未验证'}</p>
                        {check.kind === 'manual' && writable && !closed && (
                          <Button onClick={() => open('check', `确认：${check.title}`, check.key, { passed: false })}>登记业务验证</Button>
                        )}
                      </div>
                    );
                  })}
                  {writable && !closed && <Button onClick={() => open('close', '结束本次处置', undefined, { kind: 'resolved' })}>填写结论并结束</Button>}
                  {writable && (
                    <Button style={{ marginLeft: 8 }} onClick={() => open('feedback', '提交知识反馈')}>
                      形成知识草稿
                    </Button>
                  )}
                </Card>
                <Card title='证据与交接'>
                  <Space wrap>
                    {writable && !closed && <Button onClick={() => open('manual', '补充人工证据')}>补充人工证据</Button>}
                    {!closed && <Button onClick={() => open('collect', '查询当前对象指标', undefined, { window_seconds: 300 })}>查询指标</Button>}
                    {!closed && c.rule_id > 0 && <Button onClick={() => run(base + '/collect-logs')}>采集规则评估日志</Button>}
                    {writable && !closed && <Button onClick={() => run(base + '/runs', undefined, 'DELETE')}>停止 Agent 并接管</Button>}
                  </Space>
                  <Tabs defaultActiveKey='evidence'>
                    <Tabs.TabPane tab='现场证据' key='evidence'>
                      {detail.evidence.map((e) => (
                        <div className='ops-item' key={e.id}>
                          <Space>
                            <strong>证据 #{e.id}</strong>
                            <Tag>{state(e.status)}</Tag>
                            <span>
                              {e.kind} · {e.source}
                            </span>
                          </Space>
                          <div className='ops-meta'>
                            观测：{time(e.observed_at)} · 获取：{time(e.collected_at)}
                            {e.generation !== c.generation && ' · 属于先前处置阶段'}
                          </div>
                          <details>
                            <summary>查看原始结果</summary>
                            <pre className='ops-code'>{JSON.stringify(e.payload, null, 2)}</pre>
                          </details>
                        </div>
                      ))}
                    </Tabs.TabPane>
                    <Tabs.TabPane tab='检查记录' key='steps'>
                      {detail.steps.map((s) => (
                        <div className='ops-item' key={s.id}>
                          <Tag>{state(s.state)}</Tag>
                          {adopted?.steps.find((item) => item.key === s.step_key)?.title || s.step_key}
                          <p>{s.note}</p>
                          <span>证据：{s.evidence_ids?.join(', ') || '—'}</span>
                        </div>
                      ))}
                    </Tabs.TabPane>
                    <Tabs.TabPane tab='Agent 运行' key='runs'>
                      {detail.runs.map((r) => (
                        <div key={r.id} className='ops-item'>
                          <Tag>{state(r.state)}</Tag>
                          <span>{r.mode === 'guided' ? '按步骤取证' : 'Agent 排查'}</span>
                          <p className='ops-text'>{r.result || '等待运行结果'}</p>
                        </div>
                      ))}
                    </Tabs.TabPane>
                    <Tabs.TabPane tab='操作时间线' key='audit'>
                      {detail.audit.map((a) => (
                        <div className='ops-item' key={a.id}>
                          <strong>{a.action}</strong>
                          <div className='ops-meta'>
                            人员 #{a.actor_id} · {time(a.created_at)}
                          </div>
                          <pre className='ops-code'>{JSON.stringify(a.detail, null, 2)}</pre>
                        </div>
                      ))}
                    </Tabs.TabPane>
                  </Tabs>
                </Card>
              </div>
            </div>
          </>
        )}
        <Modal title={dialog?.title} visible={!!dialog} onCancel={() => setDialog(undefined)} onOk={submit} confirmLoading={busy} destroyOnClose>
          <Form form={form} layout='vertical'>
            {dialog && ['policy', 'demo-setup', 'demo-fault'].includes(dialog.kind) && (
              <Form.Item name='group_id' label='业务组' rules={[{ required: true }]}>
                <Select
                  options={config?.groups.filter((g) => g.writable).map((g) => ({ value: g.id, label: g.name }))}
                  onChange={(value) => {
                    if (dialog.kind === 'policy') {
                      const p = policies.find((item) => item.group_id === value);
                      form.setFieldsValue({
                        enabled: !!p?.enabled,
                        auto_investigate: !!p?.auto_investigate,
                        service_user_id: p?.service_user_id,
                        rule_ids_text: p?.rule_ids?.join(', ') || '',
                      });
                    }
                  }}
                />
              </Form.Item>
            )}
            {dialog?.kind === 'policy' && (
              <>
                <Form.Item name='enabled' label='是否接入' rules={[{ required: true }]}>
                  <Select
                    options={[
                      { value: true, label: '启用' },
                      { value: false, label: '暂停新接入' },
                    ]}
                  />
                </Form.Item>
                <Form.Item name='service_user_id' label='接入服务账号 ID（需要本组读取权限）' rules={[{ required: true }]}>
                  <InputNumber min={1} />
                </Form.Item>
                <Form.Item name='rule_ids_text' label='允许接入的告警规则 ID（逗号分隔，留空为本组全部）'>
                  <Input />
                </Form.Item>
                <Form.Item name='auto_investigate' label='新告警是否自动发起 Agent 排查'>
                  <Select
                    options={[
                      { value: false, label: '仅创建处置，人员发起排查' },
                      { value: true, label: '自动只读排查（需启用模型）' },
                    ]}
                  />
                </Form.Item>
                <Alert type='info' message='建议使用专用服务账号；自动排查不会批准操作或结束处置。' />
              </>
            )}
            {dialog?.kind === 'demo-setup' && (
              <>
                <Form.Item name='datasource_id' label='隔离演练 Prometheus 数据源 ID' rules={[{ required: true }]}>
                  <InputNumber min={1} />
                </Form.Item>
                <Alert type='info' message='创建测试目标、测试告警与测试知识。仅测试管理员可操作；不会写入共享 Prometheus。' />
              </>
            )}
            {dialog?.kind === 'demo-fault' && (
              <Form.Item name='kind' label='测试故障'>
                <Select
                  options={[
                    { value: 'storage', label: '目录配额水位升高并积压测试记录' },
                    { value: 'business', label: '保持业务探测失败' },
                    { value: 'data', label: '保持数据核对失败' },
                    { value: 'clear', label: '解除业务与数据测试故障' },
                  ]}
                />
              </Form.Item>
            )}
            {dialog?.kind === 'create' && (
              <>
                <Form.Item name='group_id' label='业务组' rules={[{ required: true }]}>
                  <Select options={config?.groups.filter((g) => g.writable).map((g) => ({ value: g.id, label: g.name }))} />
                </Form.Item>
                <Form.Item name='title' label='异常描述' rules={[{ required: true }]}>
                  <Input maxLength={80} />
                </Form.Item>
              </>
            )}
            {dialog?.kind === 'context' && (
              <>
                <Form.Item name='system_version' label='系统版本'>
                  <Input />
                </Form.Item>
                <Form.Item name='environment' label='运行环境'>
                  <Select
                    allowClear
                    options={[
                      { value: 'production', label: '生产' },
                      { value: 'test', label: '测试' },
                    ]}
                  />
                </Form.Item>
                <Form.Item name='business' label='业务影响范围'>
                  <Input.TextArea />
                </Form.Item>
                <Form.Item name='wait_reason' label='当前等待事项'>
                  <Input maxLength={64} />
                </Form.Item>
              </>
            )}
            {dialog?.kind === 'merge' && (
              <>
                <Form.Item name='source_id' label='关联进来的处置编号（须为相同业务组、对象、数据源）' rules={[{ required: true }]}>
                  <InputNumber min={1} />
                </Form.Item>
                <Form.Item name='reason' label='关联依据' rules={[{ required: true }]}>
                  <Input.TextArea />
                </Form.Item>
                <Alert type='info' message='将该处置的告警关联到当前处置；原处置以“合并”结束，原证据仍留在原记录。当前恢复检查需重新验证。' />
              </>
            )}
            {dialog?.kind === 'ack-gap' && (
              <Form.Item name='note' label='已核对的时间范围、来源和未恢复事项' rules={[{ required: true }]}>
                <Input.TextArea />
              </Form.Item>
            )}
            {dialog?.kind === 'assign' && (
              <Form.Item name='owner_id' label='接手人员 ID（须有本组操作权限）' rules={[{ required: true }]}>
                <InputNumber min={1} />
              </Form.Item>
            )}
            {dialog?.kind === 'select' && (
              <Form.Item name='reason' label='采用或更换版本的依据' rules={[{ required: !!c?.knowledge_version_id || !!c?.plan_id }]}>
                <Input.TextArea />
              </Form.Item>
            )}
            {dialog?.kind === 'collect' && (
              <>
                <Form.Item name='query' label='PromQL（自动限定为当前对象）' rules={[{ required: true }]}>
                  <Input.TextArea />
                </Form.Item>
                <Form.Item name='window_seconds' label='查询窗口（秒）'>
                  <InputNumber min={0} max={3600} />
                </Form.Item>
              </>
            )}
            {dialog?.kind === 'step' && (
              <>
                <Form.Item name='state' label='检查结果'>
                  <Select options={['completed', 'failed', 'skipped'].map((v) => ({ value: v, label: state(v) }))} />
                </Form.Item>
                <Form.Item name='evidence_ids' label='引用现场证据'>
                  <Select
                    mode='multiple'
                    options={detail?.evidence.filter((e) => e.generation === c?.generation).map((e) => ({ value: e.id, label: `#${e.id} ${e.kind} · ${state(e.status)}` }))}
                  />
                </Form.Item>
              </>
            )}
            {dialog?.kind === 'check' && (
              <Form.Item name='passed' label='业务验证结果'>
                <Select
                  options={[
                    { value: true, label: '已核实通过' },
                    { value: false, label: '未通过' },
                  ]}
                />
              </Form.Item>
            )}
            {dialog?.kind === 'action-result' && (
              <Form.Item name='success' label='外部执行结果'>
                <Select
                  options={[
                    { value: true, label: '已核实执行成功' },
                    { value: false, label: '已核实执行失败' },
                  ]}
                />
              </Form.Item>
            )}
            {dialog?.kind === 'close' && (
              <>
                <Form.Item name='kind' label='结束类型'>
                  <Select
                    options={[
                      { value: 'resolved', label: '已修复' },
                      { value: 'temporary', label: '临时恢复待整改' },
                      { value: 'self_recovered', label: '自行恢复已核查' },
                      { value: 'monitoring_issue', label: '监控配置问题已核实' },
                      { value: 'false_positive', label: '误报已确认' },
                      { value: 'cancelled', label: '取消处置' },
                    ]}
                  />
                </Form.Item>
                <Form.Item name='summary' label='处置结论与恢复依据' rules={[{ required: true }]}>
                  <Input.TextArea rows={4} />
                </Form.Item>
                <Form.Item name='followup' label='遗留问题、责任人与跟进任务'>
                  <Input.TextArea rows={3} />
                </Form.Item>
              </>
            )}
            {dialog && ['manual', 'step', 'check', 'action-result', 'feedback'].includes(dialog.kind) && (
              <Form.Item name='note' label='事实、结果或反馈依据' rules={[{ required: true }]}>
                <Input.TextArea rows={4} maxLength={8000} />
              </Form.Item>
            )}
          </Form>
        </Modal>
      </div>
    </PageLayout>
  );
}
