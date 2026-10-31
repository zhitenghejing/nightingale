import { collectionRequest } from '@/services/collection';
import { RequestMethod } from '@/store/common';

export interface Category {
  id: string;
  parent_id: string;
  name: string;
  record_version: number;
  sort?: number;
  disabled?: boolean;
}
export interface DeviceField {
  key: string;
  label: string;
  type: 'text' | 'password' | 'credential' | 'number' | 'switch' | 'select' | 'multiselect' | 'ip' | 'url' | 'endpoints';
  required: boolean;
  default?: number | string | boolean | string[];
  options?: string[];
  help?: string;
  secret?: boolean;
  group?: string;
  min?: number;
  max?: number;
  when?: { key: string; values: string[] }[];
}
export interface NotificationPreset {
  mode: 'events_only' | 'inherit' | 'rules';
  message: string;
  rule_ids: number[];
}
export interface PackResource {
  id: number;
  name: string;
  content: string;
  [key: string]: unknown;
}
export interface TemplatePack {
  component_id: number;
  name: string;
  readme: string;
  metrics: import('@/services/collection').ProfileMetric[];
  dashboards: PackResource[];
  alerts: PackResource[];
  customized?: boolean;
}
export interface Definition {
  reachability?: 'icmp';
  adapter: 'documentation' | 'snmp_v2c' | 'mysql' | 'sqlserver' | 'oracle' | 'pgsql' | 'redfish' | 'redis' | 'http';
  config: string;
  fields: DeviceField[];
  notification: 'events_only' | 'inherit' | 'rules';
  compatibility?: string;
  execution?: string;
  pack?: TemplatePack;
  notify?: NotificationPreset;
}

export function activeFields(fields: DeviceField[], values: Record<string, unknown>): DeviceField[] {
  const active = new Set<string>();
  return fields.filter((field) => {
    const visible = (field.when || []).every((condition) => {
      const value = values[condition.key];
      return active.has(condition.key) && condition.values.some((expected) => (Array.isArray(value) ? value.map(String).includes(expected) : String(value) === expected));
    });
    if (visible) active.add(field.key);
    return visible;
  });
}
export const adapterNames: Record<Definition['adapter'], string> = {
  documentation: '采集说明 / 手动接入',
  snmp_v2c: 'SNMP v2c',
  mysql: 'MySQL',
  sqlserver: 'SQL Server',
  oracle: 'Oracle',
  pgsql: 'PostgreSQL',
  redfish: 'Redfish（iDRAC / iLO / XCC）',
  redis: 'Redis',
  http: 'HTTP',
};
export function connectionFields(adapter: Definition['adapter']): DeviceField[] {
  const port = (value: number): DeviceField => ({ key: 'port', label: '端口', type: 'number', required: true, default: value, min: 1, max: 65535 });
  const account: DeviceField[] = [
    { key: 'username', label: '监控账号', type: 'text', required: true },
    { key: 'password', label: '监控密码', type: 'password', secret: true, required: true },
  ];
  if (adapter === 'documentation') return [];
  if (adapter === 'snmp_v2c')
    return [
      { key: 'protocol', label: 'SNMP 版本', type: 'select', required: true, default: 'snmp_v2c', options: ['snmp_v2c'] },
      port(161),
      { key: 'community', label: '团体字', type: 'password', secret: true, required: true },
    ];
  if (adapter === 'redfish')
    return [
      port(443),
      ...account,
      { key: 'tls', label: '证书校验', type: 'select', default: 'verify', required: true, options: ['verify', 'ca', 'skip'] },
      { key: 'ca_file', label: 'CA 文件路径', type: 'text', required: true, when: [{ key: 'tls', values: ['ca'] }] },
      { key: 'scope', label: '采集范围', type: 'multiselect', required: true, default: ['health'], options: ['health', 'temperature', 'fan', 'power', 'storage'] },
    ];
  if (adapter === 'http') return [{ key: 'url', label: '状态接口 URL', type: 'url', required: true }];
  if (adapter === 'redis') return [port(6379), ...account.map((f) => ({ ...f, required: false }))];
  return [
    port({ mysql: 3306, sqlserver: 1433, oracle: 1521, pgsql: 5432 }[adapter]),
    ...account,
    ...(adapter === 'oracle' ? [{ key: 'service', label: 'Service Name / SID', type: 'text' as const, required: true }] : []),
    { key: 'tls', label: '连接加密', type: 'select', required: true, default: 'off', options: ['off', 'verify'] },
  ];
}
export interface Release {
  version: number;
  category_path: string[];
  definition: Definition;
}
export interface CatalogEntry {
  component_id: number;
  ident: string;
  name: string;
  logo: string;
  category_id: string;
  disabled: boolean;
  record_version: number;
  draft: Definition;
  published_version_id: string;
  published: Release | null;
  published_metrics?: import('@/services/collection').ProfileMetric[];
  onboarding?: { state: 'ready' | 'unpublished' | 'disabled'; ready: boolean; execution: string; issues: string[] };
}
export const executionNames: Record<string, string> = { collector: '采集器远程采集', local: '目标本机 Agent', cloud: '云 API 与资源范围', discovery: '集群资源发现' };
export function entryDefinition(entry: CatalogEntry): Definition {
  return entry.published?.definition || entry.draft;
}
export function entryStatus(entry: CatalogEntry): string {
  if (entry.disabled || entry.onboarding?.state === 'disabled') return '已停用';
  return entry.onboarding?.ready || (!entry.onboarding && entry.published) ? `可自动接入 · v${entry.published?.version}` : '尚未发布 / 待接入';
}
export interface Catalog {
  categories: Category[];
  entries: CatalogEntry[];
}
export const createCatalog = (values: { name: string; category_id: string; base_component_id?: number }) => collectionRequest('catalog', RequestMethod.Post, values);
export const getCatalogEntry = (id: number): Promise<CatalogEntry> => collectionRequest(`catalog/${id}`);
export const getCatalog = (): Promise<Catalog> => collectionRequest('catalog');
export const saveMetadata = (entry: CatalogEntry, values: Record<string, unknown>) =>
  collectionRequest(`catalog/${entry.component_id}/metadata`, RequestMethod.Put, { ...values, record_version: entry.record_version });
export const saveDraft = (entry: CatalogEntry, definition: Definition, publish: boolean) =>
  collectionRequest(`catalog/${entry.component_id}/${publish ? 'publish' : 'draft'}`, RequestMethod.Put, { definition, record_version: entry.record_version });
export const removeCatalog = (entry: CatalogEntry) => collectionRequest(`catalog/${entry.component_id}`, RequestMethod.Delete, { record_version: entry.record_version });
export const saveCategory = (category: Partial<Category>) =>
  collectionRequest(`categories${category.id ? `/${category.id}` : ''}`, RequestMethod.Put, {
    id: category.id || '',
    parent_id: category.parent_id || '',
    name: category.name,
    sort: category.sort || 0,
    disabled: !!category.disabled,
    record_version: category.record_version || 0,
  });
export const removeCategory = (category: Category) => collectionRequest(`categories/${category.id}`, RequestMethod.Delete, { record_version: category.record_version });

export function categoryPath(categories: Category[], id: string): Category[] {
  const out: Category[] = [];
  const seen = new Set<string>();
  while (id && !seen.has(id)) {
    seen.add(id);
    const category = categories.find((c) => c.id === id);
    if (!category) break;
    out.unshift(category);
    id = category.parent_id;
  }
  return out;
}
export function categoryTree(categories: Category[], parent = '', depth = 1): any[] {
  return categories
    .filter((c) => c.parent_id === parent)
    .map((c) => ({ title: c.name, value: c.id, key: c.id, selectable: depth === 3, children: depth < 3 ? categoryTree(categories, c.id, depth + 1) : undefined }));
}
export function matchingEntries(entries: CatalogEntry[], categories: Category[], query: string, category: string, status: string) {
  return entries.filter((entry) => {
    const path = categoryPath(categories, entry.category_id);
    return (
      (!category || path.some((c) => c.id === category)) &&
      `${entry.name} ${entry.ident} ${path.map((c) => c.name).join(' ')}`.toLowerCase().includes(query.trim().toLowerCase()) &&
      (status === 'all' || (status === 'ready' ? !!entry.published && !entry.disabled : status === 'draft' ? !entry.published : entry.disabled))
    );
  });
}
