import request from '@/utils/request';
import { RequestMethod } from '@/store/common';

export interface CollectionState {
  device_id: string;
  kind: 'snmp' | 'mysql';
  configuration_state: string;
  health: string;
  collector_id: string;
  revision: string;
  last_sample_at: number;
  checked_at: number;
  cpu_percent: number | null;
  memory_percent: number | null;
  missing_metrics: string[];
  counter_bits: number;
}
export interface Device {
  parameters?: Record<string, unknown>;
  notify?: import('@/pages/builtInComponents/catalog').NotificationPreset;
  profile?: CollectionProfile | null;
  id: string;
  name: string;
  address: string;
  ident: string;
  port: number;
  protocol: string;
  group_id: number;
  collector_id: string;
  credential_id: string;
  template_version_id: string;
  interval_seconds: number;
  timeout_seconds: number;
  retries: number;
  labels: Record<string, string>;
  cmdb_id: string;
  enabled: boolean;
  deleted: boolean;
  record_version: number;
  collection: CollectionState;
}
export interface Collector {
  id: string;
  name: string;
  agent_ident: string;
  datasource_id: number;
  group_ids: number[];
  networks: string[];
  ports: number[];
  writer_user: string;
  record_version: number;
  last_fetched_at: number;
  desired_version: string;
  last_client_version: string;
}
export interface Credential {
  id: string;
  name: string;
  group_id: number;
  kind: string;
  record_version: number;
}
export interface Probe {
  id: string;
  state: string;
  result: string;
  deadline: number;
  cleanup_version: string;
}
export const collectionRequest = (path: string, method = RequestMethod.Get, data?: unknown, headers?: Record<string, string>): Promise<any> =>
  request(`/api/n9e/collection/${path}`, { method, data, headers, silence: true }).then((res) => res.dat);
export const collectionEnabled = (): Promise<boolean> => collectionRequest('features').then((r) => r.enabled);
export const getCollectors = (): Promise<Collector[]> => collectionRequest('collectors');
export const getCredentials = (): Promise<Credential[]> => collectionRequest('credentials');
export const getDevice = (id: string): Promise<Device> => collectionRequest(`devices/${id}`);
export const saveDevice = (id: string | undefined, values: any, key: string): Promise<Device> =>
  collectionRequest(id ? `devices/${id}` : 'devices', id ? RequestMethod.Put : RequestMethod.Post, values, { 'Idempotency-Key': key });
export const setDeviceState = (d: Device, action: 'enable' | 'disable' | 'delete') =>
  collectionRequest(action === 'delete' ? `devices/${d.id}` : `devices/${d.id}/${action}`, action === 'delete' ? RequestMethod.Delete : RequestMethod.Post, {
    record_version: d.record_version,
  });
export function requestKey(): string {
  return Array.from(crypto.getRandomValues(new Uint32Array(4)), (n) => n.toString(16).padStart(8, '0')).join('');
}

export interface CollectionTemplate {
  id: string;
  template_key: string;
  version: number;
  name: string;
  component: string;
  description: string;
  config: string;
}
export const getTemplates = (): Promise<CollectionTemplate[]> => collectionRequest('templates');
export const publishTemplate = (values: Omit<CollectionTemplate, 'id' | 'version' | 'template_key'> & { base_version_id?: string }): Promise<CollectionTemplate> =>
  collectionRequest('templates', RequestMethod.Post, values);

export interface ProfileMetric {
  name: string;
  note: string;
  expression: string;
  unit: string;
  [key: string]: unknown;
}
export interface CollectionProfile {
  secret_fields?: Record<string, boolean>;
  catalog?: import('@/pages/builtInComponents/catalog').Release;
  component_id: number;
  name: string;
  config: string;
  metrics: ProfileMetric[];
  boards: { id: number; name: string }[];
  rules: { id: number; name: string }[];
  customized: boolean;
}
export interface ProfileOption extends Pick<CollectionProfile, 'component_id' | 'name' | 'config' | 'metrics'> {
  dashboard_count: number;
  alert_count: number;
}
export const getProfiles = (): Promise<ProfileOption[]> => collectionRequest('profiles');

export interface DeviceDetailData {
  target: import('@/pages/hosts/types').Item;
  device?: Device;
  collector_name: string;
  metrics: ProfileMetric[];
  rules: { id: number; name: string; disabled: number; severity: number; source: 'template' | 'event' }[];
}
export interface DeviceSubject { device_id?: string; ident?: string; }
export interface DeviceAlert {
  id: number; rule_id: number; rule_name: string; severity: number;
  trigger_time: number; trigger_value: string; target_ident: string;
}
const deviceDetailRequest = (path: string, params: DeviceSubject & { p?: number; limit?: number }) =>
  request(`/api/n9e/targets/${path}`, { method: RequestMethod.Get, params, silence: true }).then((res) => res.dat);
export const getDeviceDetail = (subject: DeviceSubject): Promise<DeviceDetailData> => deviceDetailRequest('device-detail', subject);
export const getDeviceAlerts = (subject: DeviceSubject, page: number): Promise<{ list: DeviceAlert[]; total: number }> => deviceDetailRequest('device-alerts', { ...subject, p: page, limit: 20 });
export const getAgentDeviceMetrics = (subject: DeviceSubject): Promise<{ metrics: Record<string, any[]>; metric_errors?: Record<string, boolean> }> => deviceDetailRequest('device-metrics', subject);
