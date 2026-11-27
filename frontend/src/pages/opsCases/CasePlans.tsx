import React, { useState } from 'react';
import { Alert, Button, Card, Descriptions, Form, Input, Modal, Select, Space, Switch, Tag, message } from 'antd';
import { api, blankSpec, CasePlan, Config, Detail, errorText, Spec, state, time } from '../opsShared/api';
import ProcedureFields from '../opsShared/ProcedureFields';

export default function CasePlans({ detail, config, onChange }: { detail: Detail; config: Config; onChange: () => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const [reading, setReading] = useState<CasePlan>();
  const [decision, setDecision] = useState<{ plan: CasePlan; accept: boolean }>();
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();
  const [decisionForm] = Form.useForm();
  const c = detail.case;
  const plans = detail.plans || [];
  const writable = config.groups.some((g) => g.id === c.group_id && g.writable) && c.state !== 'closed';
  const edit = (p?: CasePlan) => {
    const spec = p?.payload || blankSpec();
    form.resetFields();
    form.setFieldsValue({
      ...spec,
      case_revision: c.revision,
      title: p?.title || '本次异常处置方案',
      scope: p?.scope || `仅本次处置 #${c.id}；对象 ${c.target_ident || '待人员明确'}；业务组 ${c.group_id}`,
      rationale: p?.rationale || '',
      evidence_ids: detail.evidence
        .filter((e) => e.generation === c.generation && e.status === 'ok')
        .map((e) => e.id)
        .slice(0, 30),
      conditions_rows: Object.entries(
        p?.payload.conditions || Object.fromEntries(Object.entries(c.context || {}).filter(([k, v]) => ['environment', 'system_version'].includes(k) && v)),
      ).map(([name, value]) => ({ name, value })),
    });
    setEditing(true);
  };
  const save = async () => {
    const v = await form.validateFields();
    setSaving(true);
    try {
      const spec: Spec = {
        ...blankSpec(),
        summary: v.summary,
        body: v.body || '',
        conditions: Object.fromEntries((v.conditions_rows || []).map((r: { name: string; value: string }) => [r.name, r.value])),
        steps: v.steps || [],
        actions: v.actions || [],
        checks: v.checks || [],
        escalation: v.escalation || '',
        test_only: !!v.test_only,
        validated_at: 0,
        review_at: 0,
        source_ids: [],
        rule_ids: [],
      };
      await api(`/cases/${c.id}/plans`, 'POST', { revision: v.case_revision, title: v.title, scope: v.scope, rationale: v.rationale, evidence_ids: v.evidence_ids, spec });
      setEditing(false);
      await onChange();
      message.success('方案草稿已保存，人员确认后生效');
    } catch (e) {
      message.error(errorText(e));
    } finally {
      setSaving(false);
    }
  };
  const decide = async () => {
    const v = await decisionForm.validateFields();
    if (!decision) return;
    setSaving(true);
    try {
      await api(`/cases/${c.id}/plans/${decision.plan.id}/decision`, 'POST', { revision: v.case_revision, accept: decision.accept, note: v.note });
      setDecision(undefined);
      await onChange();
      message.success(decision.accept ? '本次方案已确认，后续操作仍需逐项确认' : '方案已拒绝');
    } catch (e) {
      message.error(errorText(e));
    } finally {
      setSaving(false);
    }
  };
  return (
    <Card title='本次处置方案' style={{ marginBottom: 16 }} extra={writable && <Button onClick={() => edit()}>编写本次方案</Button>}>
      <p>没有适用知识时，可根据现场证据编写本次方案。仅对当前处置生效，不进入正式知识库。</p>
      {!plans.length && <Alert type='info' message='先查询指标或补充人工证据，再编写排查步骤、允许操作及恢复标准。' />}
      {plans.map((p) => (
        <div className='ops-item' key={p.id}>
          <Space wrap>
            <strong>
              {p.title} · v{p.sequence}
            </strong>
            <Tag>{state(p.state)}</Tag>
            {c.plan_id === p.id && (
              <Tag color={p.confirmed_generation === c.generation ? 'green' : 'orange'}>{p.confirmed_generation === c.generation ? '当前采用' : '背景已变化，需新版本确认'}</Tag>
            )}
          </Space>
          <p className='ops-text'>{p.scope}</p>
          <p className='ops-text'>{p.payload.summary}</p>
          <div className='ops-meta'>
            创建人 #{p.created_by} · {time(p.created_at)}
            {p.confirmed_by > 0 && ` · 决定人 #${p.confirmed_by} · ${time(p.confirmed_at)} · ${p.decision_note}`}
          </div>
          <Space wrap>
            <Button size='small' onClick={() => setReading(p)}>
              查看方案
            </Button>
            {writable && (
              <Button size='small' onClick={() => edit(p)}>
                修订为新版本
              </Button>
            )}
            {writable && p.state === 'draft' && (
              <>
                <Button
                  size='small'
                  type='primary'
                  onClick={() => {
                    decisionForm.resetFields();
                    decisionForm.setFieldsValue({ case_revision: c.revision });
                    setDecision({ plan: p, accept: true });
                  }}
                >
                  确认并采用方案
                </Button>
                <Button
                  size='small'
                  onClick={() => {
                    decisionForm.resetFields();
                    decisionForm.setFieldsValue({ case_revision: c.revision });
                    setDecision({ plan: p, accept: false });
                  }}
                >
                  拒绝方案
                </Button>
              </>
            )}
          </Space>
        </div>
      ))}
      <Modal
        visible={editing}
        title='编写本次处置方案'
        width={1000}
        onCancel={() => setEditing(false)}
        onOk={save}
        okText='保存方案草稿'
        confirmLoading={saving}
        destroyOnClose
        bodyStyle={{ maxHeight: '75vh', overflowY: 'auto' }}
      >
        <Alert type='info' showIcon message='保存只生成本次草稿。确认方案后，仍需为每项操作单独确认；背景或证据变化后，草稿须重新核对。' style={{ marginBottom: 16 }} />
        <Form form={form} layout='vertical' className='ops-editor'>
          <Form.Item name='case_revision' hidden>
            <Input />
          </Form.Item>
          <Form.Item name='title' label='方案标题' rules={[{ required: true }]}>
            <Input maxLength={255} />
          </Form.Item>
          <Form.Item name='scope' label='本次目标与适用范围' rules={[{ required: true }]}>
            <Input.TextArea maxLength={4000} />
          </Form.Item>
          <Form.Item name='summary' label='处理目标' rules={[{ required: true }]}>
            <Input.TextArea />
          </Form.Item>
          <Form.Item name='rationale' label='已确认事实、待验证假设和未知事项' rules={[{ required: true }]}>
            <Input.TextArea rows={4} maxLength={16000} />
          </Form.Item>
          <Form.Item name='evidence_ids' label='方案依据（本次阶段的有效证据）' rules={[{ required: true }]}>
            <Select
              mode='multiple'
              options={detail.evidence
                .filter((e) => e.generation === c.generation && e.status === 'ok')
                .map((e) => ({ value: e.id, label: `#${e.id} ${e.kind} · ${time(e.collected_at)}` }))}
            />
          </Form.Item>
          <Form.Item name='body' label='补充说明'>
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name='test_only' label='仅测试环境' valuePropName='checked'>
            <Switch />
          </Form.Item>
          <ProcedureFields form={form} demoEnabled={config.demo_enabled} />
          <Form.Item name='escalation' label='停止、回退与升级条件' rules={[{ required: true }]}>
            <Input.TextArea />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        visible={!!decision}
        title={decision?.accept ? '确认并采用本次方案' : '拒绝本次方案'}
        onCancel={() => setDecision(undefined)}
        onOk={decide}
        confirmLoading={saving}
        destroyOnClose
      >
        <p>
          {decision?.plan.title} · v{decision?.plan.sequence}
        </p>
        {decision?.accept && <Alert type='warning' message='请先查看完整方案并核对目标、证据、风险及恢复标准。采用后替换当前处理依据，需重新取证与验证。' />}
        <Form form={decisionForm} layout='vertical'>
          <Form.Item name='case_revision' hidden>
            <Input />
          </Form.Item>
          <Form.Item name='note' label='核对依据与决定说明' rules={[{ required: true }]}>
            <Input.TextArea />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        visible={!!reading}
        title={reading && `${reading.title} · v${reading.sequence}`}
        width={900}
        onCancel={() => setReading(undefined)}
        footer={null}
        bodyStyle={{ maxHeight: '75vh', overflowY: 'auto' }}
      >
        {reading && (
          <>
            <Descriptions column={1}>
              <Descriptions.Item label='适用范围'>{reading.scope}</Descriptions.Item>
              <Descriptions.Item label='处理目标'>{reading.payload.summary}</Descriptions.Item>
              <Descriptions.Item label='事实、假设和未知事项'>
                <span className='ops-text'>{reading.rationale}</span>
              </Descriptions.Item>
              <Descriptions.Item label='依据证据'>{reading.evidence_ids.map((id) => `#${id}`).join('、')}</Descriptions.Item>
              <Descriptions.Item label='背景条件'>
                {Object.entries(reading.payload.conditions || {})
                  .map(([k, v]) => `${k}=${v}`)
                  .join('；') || '未声明额外条件'}
              </Descriptions.Item>
            </Descriptions>
            <p className='ops-text'>{reading.payload.body}</p>
            <h4>排查步骤</h4>
            {reading.payload.steps.map((s) => (
              <div className='ops-item' key={s.key}>
                <strong>{s.title}</strong>
                <p className='ops-text'>{s.instruction}</p>
                <p>预期：{s.expected}</p>
                {s.query && <code>{s.query}</code>}
              </div>
            ))}
            <h4>允许操作</h4>
            {reading.payload.actions.map((a) => (
              <div className='ops-item' key={a.key}>
                <strong>{a.title}</strong>
                <p>
                  方式：{a.kind} {a.template_id > 0 && `· 模板 #${a.template_id}`}
                </p>
                <p className='ops-text'>{a.instruction}</p>
                <p>风险与停止条件：{a.risk}</p>
                <p>回退：{a.rollback}</p>
              </div>
            ))}
            <h4>恢复标准</h4>
            {reading.payload.checks.map((c) => (
              <div className='ops-item' key={c.key}>
                <strong>
                  {c.title} · {c.required ? '必需通过' : '可选'}
                </strong>
                <p>{c.kind === 'manual' ? '人员核验' : `${c.query} ${c.operator} ${c.threshold}；观察 ${c.window_seconds} 秒`}</p>
              </div>
            ))}
            <p className='ops-text'>升级条件：{reading.payload.escalation}</p>
            <small>内容摘要 SHA-256：{reading.sha256}</small>
          </>
        )}
      </Modal>
    </Card>
  );
}
