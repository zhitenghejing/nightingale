import request from '@/utils/request';

export type Group = { id: number; name: string; writable: boolean };
export type Config = { groups: Group[]; user_id: number; model_enabled: boolean; execution_enabled: boolean; demo_enabled: boolean };
export type OpsCase = {
  id: number;
  group_id: number;
  title: string;
  severity: number;
  state: string;
  wait_reason: string;
  owner_id: number;
  target_ident: string;
  datasource_id: number;
  rule_id: number;
  context: Record<string, string>;
  knowledge_version_id: number;
  plan_id: number;
  revision: number;
  generation: number;
  close_type: string;
  summary: string;
  followup: string;
  updated_at: number;
};
export type Evidence = {
  id: number;
  kind: string;
  source: string;
  status: string;
  observed_at: number;
  collected_at: number;
  generation: number;
  payload: unknown;
  sha256: string;
};
export type Step = { key: string; title: string; kind: string; query: string; instruction: string; expected: string; next?: string; on_failure?: string };
export type Check = {
  key: string;
  title: string;
  kind: string;
  query: string;
  operator: string;
  threshold: number;
  window_seconds: number;
  fresh_seconds: number;
  required: boolean;
};
export type Action = { required_steps?: string[]; key: string; title: string; kind: string; template_id: number; instruction: string; risk: string; rollback: string };
export type Spec = {
  summary: string;
  body: string;
  source_ids: number[];
  source_note: string;
  rule_ids: number[];
  conditions: Record<string, string>;
  steps: Step[];
  checks: Check[];
  actions: Action[];
  escalation: string;
  validated_at: number;
  review_at: number;
  test_only: boolean;
};
export type Knowledge = { id: number; group_id: number; title: string; draft: Spec | null; revision: number; current_version_id: number; updated_at: number };
export type Version = { id: number; item_id: number; title: string; sequence: number; payload: Spec; published_at: number; withdrawn_at: number; withdraw_reason: string };
export type Candidate = { version: Version; spec: Spec; applicable: boolean; reason: string; conditions: { key: string; expected: string; actual: string; state: string }[] };
export type CasePlan = {
  id: number;
  case_id: number;
  sequence: number;
  title: string;
  scope: string;
  rationale: string;
  payload: Spec;
  evidence_ids: number[];
  sha256: string;
  generation: number;
  confirmed_generation: number;
  state: string;
  created_by: number;
  created_at: number;
  confirmed_by: number;
  confirmed_at: number;
  decision_note: string;
};
export type Detail = {
  plans: CasePlan[];
  case: OpsCase;
  episodes: { id: number; recovered: boolean; incident_id: string }[];
  evidence: Evidence[];
  steps: { id: number; step_key: string; state: string; note: string; evidence_ids: number[]; version_id: number; plan_id: number; generation: number }[];
  actions: {
    id: number;
    kind: string;
    state: string;
    expires_at: number;
    payload: { definition: Action; target: string; evidence_max_id?: number; template?: unknown };
    result: unknown;
  }[];
  verifications: { id: number; check_key: string; generation: number; state: string; reason: string; evidence_ids: number[]; confirmed_by: number }[];
  runs: { id: number; state: string; mode: string; result: string; attempts: number }[];
  audit: { id: number; action: string; actor_id: number; created_at: number; detail: unknown }[];
};
export async function api<T = unknown>(path: string, method = 'GET', data?: unknown): Promise<T> {
  const res = await request('/api/n9e/ops' + path, { method, data, silence: true });
  if (!res || res.err !== '') throw new Error(res?.err || '请求失败，请重试');
  return res.dat;
}
export const time = (v?: number) => (v ? new Date(v * 1000).toLocaleString() : '—');
export const state = (v: string) =>
  ({
    draft: '草稿待确认',
    confirmed: '已确认',
    pending: '待处理',
    processing: '处理中',
    verifying: '待验证',
    closed: '已结束',
    proposed: '待确认',
    rejected: '已拒绝',
    dispatching: '正在下发',
    submitted: '已下发待核实',
    succeeded: '执行成功',
    unknown: '结果不明',
    awaiting_manual: '待人工执行',
    awaiting_operator: '等待人员接管',
    agent_failed: 'Agent 排查失败，待值班人员接管',
    plan_confirmation: '等待人员确认本次方案',
    action_confirmation: '等待人员确认操作或核实结果',
    manual_investigation: '等待人员完成检查',
    passed: '已通过',
    failed: '失败',
    completed: '已完成',
    queued: '排队中',
    running: '运行中',
    cancelled: '已取消',
    ok: '有效',
    empty: '无数据',
    stale: '已过期',
    partial: '不完整',
    error: '查询失败',
    satisfied: '满足',
    mismatch: '不满足',
    skipped: '已跳过',
  }[v] || v);
export const errorText = (e: unknown) => (e instanceof Error ? e.message : '请求失败');
export const key = () => 'item-' + Math.random().toString(36).slice(2, 12);
export function blankSpec(): Spec {
  const now = Math.floor(Date.now() / 1000);
  return {
    summary: '',
    body: '',
    source_ids: [],
    source_note: '',
    rule_ids: [],
    conditions: {},
    steps: [{ key: key(), title: '确认异常范围', kind: 'manual', query: '', instruction: '', expected: '' }],
    actions: [],
    checks: [{ key: key(), title: '确认业务恢复', kind: 'manual', query: '', operator: '<', threshold: 0, window_seconds: 0, fresh_seconds: 60, required: true }],
    escalation: '',
    validated_at: now,
    review_at: now + 90 * 86400,
    test_only: false,
  };
}
