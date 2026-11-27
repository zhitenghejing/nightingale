// THROWAWAY UI PROTOTYPE: three layouts on /alert-rules?prototype=operations&variant=A.
// Question: can rule configuration and post-alert handling become one clear flow?
// Existing rules/policies are read-only inputs. Every change below is memory-only.
import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Drawer, Input, InputNumber, Modal, Radio, Select, Space, Steps, Table, Tag, Typography, message } from 'antd';
import { ArrowRightOutlined, CheckCircleOutlined, EditOutlined, PlusOutlined, SearchOutlined, SettingOutlined } from '@ant-design/icons';
import { useLocation } from 'react-router-dom';
import request from '@/utils/request';
import PrototypeSwitcher from '@/components/PrototypeSwitcher';
import './style.less';

type Mode = 'monitor' | 'manual' | 'agent';
type Selection = Mode | 'inherit';
type Rule = { id: number; name: string; group_id: number; disabled: number; query: string; duration: number; source: number; selection: Selection };
type Policy = { group_id: number; enabled: boolean; auto_investigate: boolean; rule_ids: number[] };
type Group = { id: number; name: string; writable: boolean };
const modes: { key: Mode; title: string; short: string; detail: string; color: string; icon: string }[] = [
  { key: 'monitor', title: '仅监控告警', short: '仅告警', detail: '产生告警事件，按通知规则发送通知。', color: 'default', icon: '01' },
  { key: 'manual', title: '自动创建处置', short: '自动建单', detail: '事件发生后建立处置记录，由人员启动排查。', color: 'blue', icon: '02' },
  { key: 'agent', title: '建单并启动 Agent', short: '建单 + Agent', detail: '自动建单、只读取证与分析，操作仍需人员确认。', color: 'purple', icon: '03' },
];
const variants = [
  { key: 'A', name: '列表内配置 · 推荐' },
  { key: 'B', name: '分步配置向导' },
  { key: 'C', name: '处置策略工作台' },
];
const modeInfo = (mode: Mode) => modes.find((m) => m.key === mode)!;
const sourceName = (id: number) => (id === 1 ? 'shared-prometheus' : id === 2 ? 'Isolated Ops Demo' : `数据源 #${id}`);

const RuleFields = ({ rule, onChange }: { rule: Rule; onChange: (r: Rule) => void }) => (
  <div className='op-fields'>
    <label>
      规则名称 <b>*</b>
      <Input aria-label='规则名称' value={rule.name} placeholder='例如：应用服务器 CPU 持续高使用率' onChange={(e) => onChange({ ...rule, name: e.target.value })} />
    </label>
    <div className='op-field-pair'>
      <label>
        数据源
        <Input value={sourceName(rule.source)} readOnly />
      </label>
      <label>
        持续时间（秒）
        <InputNumber aria-label='持续时间' min={0} step={15} value={rule.duration} onChange={(v) => onChange({ ...rule, duration: Number(v) })} />
      </label>
    </div>
    <label>
      告警表达式 <b>*</b>
      <Input.TextArea aria-label='告警表达式' className='op-query' rows={3} value={rule.query} onChange={(e) => onChange({ ...rule, query: e.target.value })} />
    </label>
  </div>
);

const ModeChoices = ({ value, onChange, inherit = false, groupMode }: { groupMode: Mode; value: Selection; onChange: (m: Selection) => void; inherit?: boolean }) => (
  <Radio.Group className='op-mode-choices' value={value} onChange={(e) => onChange(e.target.value)}>
    {inherit && (
      <Radio value='inherit'>
        <strong>继承业务组默认</strong>
        <span>当前为「{modeInfo(groupMode).title}」，随业务组默认设置变化。</span>
      </Radio>
    )}
    {modes.map((m) => (
      <Radio key={m.key} value={m.key}>
        <strong>{m.title}</strong>
        <span>{m.detail}</span>
      </Radio>
    ))}
  </Radio.Group>
);

export default function AlertOperationsPrototype({ gids }: { gids?: string }) {
  const location = useLocation();
  const requestedVariant = new URLSearchParams(location.search).get('variant');
  const variant = variants.some((v) => v.key === requestedVariant) ? requestedVariant! : 'A';
  const [rules, setRules] = useState<Rule[]>([]);
  const [groupMode, setGroupMode] = useState<Mode>('manual');
  const [groupName, setGroupName] = useState('');
  const [groupId, setGroupId] = useState(0);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<React.Key[]>([]);
  const [editing, setEditing] = useState<Rule>();
  const [batch, setBatch] = useState(false);
  const [batchMode, setBatchMode] = useState<Selection>('inherit');
  const [defaults, setDefaults] = useState(false);
  const [draftDefault, setDraftDefault] = useState<Mode>('manual');
  const [scenario, setScenario] = useState('ready');
  const [changed, setChanged] = useState(0);
  const [step, setStep] = useState(0);
  const [wizard, setWizard] = useState<Rule>();
  const [stateOpen, setStateOpen] = useState(false);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let active = true;
    setLoading(true);
    Promise.all([request('/api/n9e/ops/config'), request('/api/n9e/ops/policies')])
      .then(async ([cfg, policies]) => {
        if (cfg.err || policies.err) throw Error(cfg.err || policies.err);
        const groups: Group[] = cfg.dat.groups;
        const first = Number(gids?.split(',')[0]);
        const group = groups.find((g) => g.id === first) || groups.find((g) => g.name === '共享指标验证') || groups[0];
        if (!group) return;
        const response = await request(`/api/n9e/busi-group/${group.id}/alert-rules`);
        if (response.err) throw Error(response.err);
        if (!active) return;
        const policy: Policy | undefined = policies.dat.find((p: Policy) => p.group_id === group.id);
        const mode: Mode = policy?.enabled ? (policy.auto_investigate ? 'agent' : 'manual') : 'monitor';
        const rows: Rule[] = response.dat.map((r) => ({
          id: r.id,
          name: r.name,
          group_id: r.group_id,
          disabled: r.disabled,
          query: r.rule_config?.queries?.[0]?.prom_ql || r.prom_ql || '',
          duration: r.prom_for_duration || 0,
          source: r.datasource_ids?.[0] || Number(r.datasource_queries?.[0]?.values?.[0]) || 1,
          selection: policy?.enabled && policy.rule_ids.length && !policy.rule_ids.includes(r.id) ? 'monitor' : 'inherit',
        }));
        setRules(rows);
        setGroupMode(mode);
        setGroupName(group.name);
        setGroupId(group.id);
        setChanged(0);
        setSelected([]);
        setWizard(undefined);
        setStep(0);
      })
      .catch((e) => {
        if (active) message.error(e.message || '读取规则失败');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [gids, reload]);
  const effective = (r: Rule): Mode => (r.selection === 'inherit' ? groupMode : r.selection);
  const filtered = rules.filter((r) => `${r.id} ${r.name}`.toLowerCase().includes(search.toLowerCase()));
  const activeRules = rules.filter((r) => !r.disabled);
  const stats = modes.map((mode) => ({ ...mode, count: activeRules.filter((r) => effective(r) === mode.key).length }));
  const inherited = rules.filter((r) => r.selection === 'inherit').length;
  const bump = () => setChanged((n) => n + 1);
  const update = (rule: Rule) => {
    setRules((rows) => (rule.id < 0 && !rows.some((r) => r.id === rule.id) ? [...rows, rule] : rows.map((r) => (r.id === rule.id ? rule : r))));
    bump();
    setEditing(undefined);
    message.success('已保存到原型演示状态，真实规则未修改');
  };
  const create = () =>
    setEditing({
      id: -Date.now(),
      name: '',
      group_id: groupId,
      disabled: 0,
      query: 'cpu_usage_active{ident="categraf01",cpu="cpu-total"} > 85',
      duration: 300,
      source: 1,
      selection: 'inherit',
    });
  const readiness = (r?: Rule) => {
    const mode = r ? effective(r) : groupMode;
    const items = [
      { label: '告警条件', detail: r?.query ? '已填写指标表达式' : '请选择或填写规则', ok: !!r?.query },
      { label: '处置接入', detail: mode === 'monitor' ? '本规则仅产生告警事件' : `归属 ${groupName}，使用本组接入账号`, ok: true },
      {
        label: 'Agent 排查',
        detail:
          mode !== 'agent'
            ? '由人员按需启动'
            : scenario === 'model'
            ? '模型连接不可用，请先处理模型配置'
            : scenario === 'scope'
            ? '接入账号缺少对象读取权限'
            : '模型与工具范围检查通过（演示）',
        ok: mode !== 'agent' || scenario === 'ready',
      },
    ];
    return items;
  };
  const blocked = (r?: Rule) => !r?.name.trim() || !r?.query.trim() || readiness(r).some((x) => !x.ok);
  const Flow = ({ mode }: { mode: Mode }) => (
    <div className='op-flow'>
      {['监控指标', '告警事件', ...(mode !== 'monitor' ? ['自动创建处置'] : []), ...(mode === 'agent' ? ['Agent 只读排查'] : mode === 'manual' ? ['人员启动排查'] : [])].map(
        (text, i) => (
          <React.Fragment key={text}>
            {i > 0 && <ArrowRightOutlined />}
            <span className={i === 2 ? 'accent' : ''}>{text}</span>
          </React.Fragment>
        ),
      )}
    </div>
  );
  const Checks = ({ rule }: { rule?: Rule }) => (
    <div className='op-checks'>
      {readiness(rule).map((c) => (
        <div key={c.label}>
          <CheckCircleOutlined className={c.ok ? 'ok' : 'not-ready'} />
          <span>
            <strong>{c.label}</strong>
            <small>{c.detail}</small>
          </span>
        </div>
      ))}
      <p>检查状态供页面演示；正式实现将由后端核验模型、账号及工具权限。</p>
    </div>
  );
  const columns = [
    {
      title: 'ID',
      key: 'id',
      width: 80,
      render: (_, r: Rule) =>
        r.id < 0 ? (
          <Tag>待生成</Tag>
        ) : (
          <Typography.Text copyable={{ text: String(r.id) }} className='op-id'>
            #{r.id}
          </Typography.Text>
        ),
    },
    {
      title: '告警规则',
      key: 'name',
      render: (_, r: Rule) => (
        <div className='op-rule-name'>
          <button onClick={() => setEditing({ ...r })}>{r.name.replace('共享指标验证 / categraf01 / ', '')}</button>
          <small>
            {sourceName(r.source)} · {r.duration ? `持续 ${r.duration / 60} 分钟` : '满足条件即触发'}
          </small>
        </div>
      ),
    },
    { title: '状态', width: 82, render: (_, r: Rule) => <Tag color={r.disabled ? undefined : 'green'}>{r.disabled ? '停用' : '启用'}</Tag> },
    {
      title: '处置方式',
      width: 185,
      render: (_, r: Rule) => (
        <div>
          <Tag color={modeInfo(effective(r)).color}>{modeInfo(effective(r)).short}</Tag>
          <small className='op-origin'>{r.selection === 'inherit' ? '继承业务组' : '本规则单独设置'}</small>
        </div>
      ),
    },
    {
      title: '配置状态',
      width: 108,
      render: (_, r: Rule) =>
        r.disabled ? (
          <span className='op-muted'>未启用</span>
        ) : effective(r) === 'agent' && scenario !== 'ready' ? (
          <span className='op-warning'>待处理</span>
        ) : (
          <span className='op-ready'>
            <span />
            已就绪
          </span>
        ),
    },
    {
      title: '操作',
      width: 105,
      render: (_, r: Rule) => (
        <Button type='link' icon={<EditOutlined />} onClick={() => setEditing({ ...r })}>
          配置
        </Button>
      ),
    },
  ];
  const groupDefaults = () => {
    setDraftDefault(groupMode);
    setDefaults(true);
  };
  const BulkButton = () => (
    <Button
      disabled={!selected.length}
      onClick={() => {
        setBatchMode('inherit');
        setBatch(true);
      }}
    >
      批量设置处置方式{selected.length ? ` (${selected.length})` : ''}
    </Button>
  );
  const chosenWizard = wizard || rules.find((r) => !r.disabled);
  const relevantState = useMemo(
    () => ({
      business_group: groupName,
      group_default: modeInfo(groupMode).title,
      simulated_changes: changed,
      rules: rules.map((r) => ({ id: r.id < 0 ? '待生成' : r.id, name: r.name, setting: r.selection, effective_mode: modeInfo(effective(r)).title, enabled: !r.disabled })),
    }),
    [groupName, groupMode, rules, changed],
  );
  return (
    <section className={'ops-rule-prototype variant-' + variant}>
      <div className='op-prototype-notice'>
        <Tag color='purple'>交互原型</Tag>
        <span>读取现有规则，修改仅在当前页面演示。刷新后恢复真实配置。</span>
        <a href='/alert-rules'>返回真实规则</a>
      </div>
      <header className='op-page-heading'>
        <div>
          <div className='op-eyebrow'>告警配置 / 处置联动</div>
          <h2>{variant === 'B' ? '一次配置，从告警到处置' : variant === 'C' ? '处置策略工作台' : '告警规则与处置'}</h2>
          <p>
            {groupName || '读取业务组…'} · {activeRules.length} 条启用规则 · 将告警后的处理方式一起配置清楚
          </p>
        </div>
        <Space>
          <Button icon={<SettingOutlined />} onClick={groupDefaults}>
            业务组默认
          </Button>
          <Button type='primary' icon={<PlusOutlined />} onClick={create}>
            新建规则
          </Button>
        </Space>
      </header>
      <div className='op-default-strip'>
        <span className='op-group-icon'>组</span>
        <div>
          <strong>业务组默认：{modeInfo(groupMode).title}</strong>
          <span>{inherited} 条规则继承，单独设置的规则保持自己的处理方式。</span>
        </div>
        <Button type='link' onClick={groupDefaults}>
          调整默认 <ArrowRightOutlined />
        </Button>
      </div>
      {variant !== 'B' && (
        <div className='op-metrics'>
          {stats.map((m) => (
            <div key={m.key}>
              <span>{m.title}</span>
              <strong>
                {m.count}
                <small> 条启用规则</small>
              </strong>
            </div>
          ))}
          <div className='op-metrics-note'>
            <span>操作与恢复</span>
            <strong>
              人员确认<small>Agent 排查后继续按权限推进</small>
            </strong>
          </div>
        </div>
      )}
      {variant === 'A' && (
        <div className='op-table-card'>
          <div className='op-table-toolbar'>
            <Input aria-label='搜索规则' prefix={<SearchOutlined />} placeholder='搜索规则名称或 ID' value={search} onChange={(e) => setSearch(e.target.value)} />
            <Space>
              <BulkButton />
              <Button
                onClick={() => {
                  setReload((n) => n + 1);
                  message.info('正在重新读取真实配置');
                }}
              >
                重置演示
              </Button>
            </Space>
          </div>
          <Table
            rowKey='id'
            loading={loading}
            columns={columns}
            dataSource={filtered}
            pagination={false}
            scroll={{ x: 850 }}
            rowSelection={{ selectedRowKeys: selected, onChange: setSelected }}
          />
          <div className='op-table-foot'>
            <CheckCircleOutlined />
            规则保存时一并确定告警后的处理方式，ID 由系统关联。
          </div>
        </div>
      )}
      {variant === 'B' && (
        <div className='op-wizard'>
          <div className='op-wizard-rail'>
            <div className='op-eyebrow'>配置一条规则</div>
            <Steps direction='vertical' current={step}>
              <Steps.Step title='监控什么' description='对象、指标和触发条件' />
              <Steps.Step title='告警后如何处理' description='自动建单与 Agent 排查' />
              <Steps.Step title='检查并保存' description='核对最终生效结果' />
            </Steps>
            <div className='op-wizard-helper'>在一个流程里完成配置，不用跳转页面抄写规则 ID。</div>
          </div>
          <div className='op-wizard-content'>
            <label className='op-select-rule'>
              选择一条现有规则
              <Select
                aria-label='选择现有规则'
                value={chosenWizard?.id}
                options={rules.map((r) => ({ value: r.id, label: r.name }))}
                onChange={(id) => {
                  setWizard({ ...rules.find((r) => r.id === id)! });
                  setStep(0);
                }}
              />
            </label>
            {chosenWizard && (
              <>
                <h3>{['设置监控条件', '选择告警后的处理方式', '核对这条规则的完整流程'][step]}</h3>
                {step === 0 && <RuleFields rule={chosenWizard} onChange={setWizard} />}
                {step === 1 && <ModeChoices groupMode={groupMode} inherit value={chosenWizard.selection} onChange={(selection) => setWizard({ ...chosenWizard, selection })} />}
                {step === 2 && (
                  <>
                    <Flow mode={effective(chosenWizard)} />
                    <Checks rule={chosenWizard} />
                    <div className='op-summary-line'>
                      <strong>最终方式</strong>
                      <span>
                        {modeInfo(effective(chosenWizard)).title} · {chosenWizard.selection === 'inherit' ? '继承业务组' : '本规则设置'}
                      </span>
                    </div>
                  </>
                )}
                <div className='op-wizard-actions'>
                  <Button disabled={step === 0} onClick={() => setStep(step - 1)}>
                    上一步
                  </Button>
                  {step < 2 ? (
                    <Button type='primary' onClick={() => setStep(step + 1)}>
                      下一步 <ArrowRightOutlined />
                    </Button>
                  ) : (
                    <Button type='primary' disabled={blocked(chosenWizard)} onClick={() => update(chosenWizard)}>
                      保存规则与处置方式（演示）
                    </Button>
                  )}
                </div>
              </>
            )}
          </div>
          <aside className='op-wizard-summary'>
            <span className='op-eyebrow'>配置结果预览</span>
            <h3>{chosenWizard?.name.replace('共享指标验证 / categraf01 / ', '')}</h3>
            <div>
              数据源<strong>{sourceName(chosenWizard?.source || 1)}</strong>
            </div>
            <div>
              告警后<strong>{modeInfo(chosenWizard ? effective(chosenWizard) : groupMode).title}</strong>
            </div>
            <div>
              操作执行<strong>人员单独确认</strong>
            </div>
            <div>
              恢复结束<strong>依据证据核验</strong>
            </div>
            <p>原型保存不会修改真实规则，也不会触发模型或作业。</p>
          </aside>
        </div>
      )}
      {variant === 'C' && (
        <div className='op-board'>
          {modes.map((m) => (
            <section className={'op-lane lane-' + m.key} key={m.key}>
              <header>
                <span>{m.icon}</span>
                <h3>{m.title}</h3>
                <Tag>{rules.filter((r) => effective(r) === m.key).length}</Tag>
              </header>
              <p>{m.detail}</p>
              {rules
                .filter((r) => effective(r) === m.key)
                .map((r) => (
                  <article key={r.id}>
                    <div>
                      <small>#{r.id < 0 ? '待生成' : r.id}</small>
                      <Tag color={r.disabled ? undefined : 'green'}>{r.disabled ? '停用' : '启用'}</Tag>
                    </div>
                    <h4>{r.name.replace('共享指标验证 / categraf01 / ', '')}</h4>
                    <p>{sourceName(r.source)}</p>
                    <span className='op-origin'>{r.selection === 'inherit' ? '继承业务组默认' : '本规则单独设置'}</span>
                    <div className='op-lane-actions'>
                      <Select
                        aria-label={'移动规则 ' + r.id}
                        value={r.selection}
                        options={[{ value: 'inherit', label: '继承业务组' }, ...modes.map((v) => ({ value: v.key, label: v.title }))]}
                        onChange={(selection) => update({ ...r, selection })}
                      />
                      <Button type='text' icon={<EditOutlined />} aria-label={'编辑规则 ' + r.id} onClick={() => setEditing({ ...r })} />
                    </div>
                  </article>
                ))}
              {!rules.some((r) => effective(r) === m.key) && (
                <div className='op-lane-empty'>
                  暂无规则
                  <br />
                  <small>可从其他列调整规则的处理方式</small>
                </div>
              )}
            </section>
          ))}
        </div>
      )}
      <div className='op-state-line'>
        <span>
          <Tag>{changed ? `${changed} 次演示修改` : '真实配置快照'}</Tag>当前布局：{variants.find((v) => v.key === variant)?.name}
        </span>
        <Space>
          <span className='op-muted'>演示检查</span>
          <Select
            aria-label='演示检查场景'
            value={scenario}
            onChange={setScenario}
            options={[
              { value: 'ready', label: '配置就绪' },
              { value: 'model', label: '模型不可用' },
              { value: 'scope', label: '账号缺少权限' },
            ]}
          />
          <Button type='link' onClick={() => setStateOpen(!stateOpen)}>
            {stateOpen ? '收起' : '查看'}当前演示配置
          </Button>
        </Space>
      </div>
      {stateOpen && <pre className='op-state-json'>{JSON.stringify(relevantState, null, 2)}</pre>}
      <Drawer
        title={editing?.id && editing.id > 0 ? `配置规则 · #${editing.id}` : '新建规则'}
        width={720}
        visible={!!editing}
        onClose={() => setEditing(undefined)}
        footer={
          editing && (
            <div className='op-drawer-footer'>
              <span>仅保存演示状态</span>
              <Space>
                <Button onClick={() => setEditing(undefined)}>取消</Button>
                <Button type='primary' disabled={blocked(editing)} onClick={() => update(editing)}>
                  保存规则与处置方式
                </Button>
              </Space>
            </div>
          )
        }
        className='op-prototype-drawer'
      >
        {editing && (
          <>
            <div className='op-form-section'>
              <span>01</span>
              <h3>监控条件</h3>
            </div>
            <RuleFields rule={editing} onChange={setEditing} />
            <div className='op-form-section'>
              <span>02</span>
              <h3>告警后如何处理</h3>
            </div>
            <ModeChoices groupMode={groupMode} inherit value={editing.selection} onChange={(selection) => setEditing({ ...editing, selection })} />
            <div className='op-effective'>
              <strong>生效结果：{modeInfo(effective(editing)).title}</strong>
              <Flow mode={effective(editing)} />
            </div>
            <div className='op-form-section'>
              <span>03</span>
              <h3>配置检查（演示）</h3>
            </div>
            <Checks rule={editing} />
            {blocked(editing) && <Alert type='warning' showIcon message='请补齐规则信息或处理未就绪的检查项后保存。' />}
          </>
        )}
      </Drawer>
      <Modal
        title='业务组默认处理方式'
        okButtonProps={{ disabled: draftDefault === 'agent' && scenario !== 'ready' }}
        visible={defaults}
        width={650}
        onCancel={() => setDefaults(false)}
        okText='保存默认方式（演示）'
        onOk={() => {
          setGroupMode(draftDefault);
          bump();
          setDefaults(false);
          message.success(`已更新演示默认方式；${inherited} 条继承规则随之变化`);
        }}
      >
        <p className='op-modal-description'>
          {groupName} · 修改后影响 {inherited} 条继承规则，单独设置的规则保持不变。
        </p>
        <ModeChoices groupMode={groupMode} value={draftDefault} onChange={(m) => setDraftDefault(m as Mode)} />
        <Alert type='info' showIcon message='业务组统一设置接入身份，规则自动关联，无需输入规则 ID。' />
      </Modal>
      <Modal
        title={`批量设置 ${selected.length} 条规则`}
        okButtonProps={{ disabled: (batchMode === 'agent' || (batchMode === 'inherit' && groupMode === 'agent')) && scenario !== 'ready' }}
        visible={batch}
        width={650}
        onCancel={() => setBatch(false)}
        okText='应用到所选规则（演示）'
        onOk={() => {
          setRules((rows) => rows.map((r) => (selected.includes(r.id) ? { ...r, selection: batchMode } : r)));
          bump();
          setBatch(false);
          setSelected([]);
          message.success('批量演示设置已应用，真实配置未修改');
        }}
      >
        <div className='op-selected-list'>
          {rules
            .filter((r) => selected.includes(r.id))
            .map((r) => (
              <span key={r.id}>
                #{r.id} {r.name.replace('共享指标验证 / categraf01 / ', '')}
              </span>
            ))}
        </div>
        <ModeChoices groupMode={groupMode} inherit value={batchMode} onChange={setBatchMode} />
      </Modal>
      <PrototypeSwitcher current={variant} variants={variants} />
    </section>
  );
}
