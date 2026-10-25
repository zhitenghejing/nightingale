import { Device, warrantyStatus } from './model';

export const textFilters = [
  { key: 'room', label: '机房名称' },
  { key: 'rack', label: '机柜名称' },
  { key: 'hostname', label: '主机名' },
  { key: 'vendor', label: '厂商' },
  { key: 'serial', label: '序列号' },
  { key: 'business', label: '所属业务' },
  { key: 'model', label: '型号' },
  { key: 'businessIp', label: '业务 IP' },
] as const;
export type TextFilterKey = (typeof textFilters)[number]['key'];
export interface TextCondition {
  field: TextFilterKey;
  value: string;
}
export interface InventoryQuery {
  keyword: string;
  types: string[];
  warrantyTypes: string[];
  warrantyStatuses: string[];
  expireFrom: string;
  expireTo: string;
  conditions: TextCondition[];
}
export const emptyQuery = (): InventoryQuery => ({ keyword: '', types: [], warrantyTypes: [], warrantyStatuses: [], expireFrom: '', expireTo: '', conditions: [] });
const normalized = (value: unknown) =>
  String(value ?? '')
    .trim()
    .toLowerCase();

// Keywords and independent conditions intersect; choices in one category are alternatives.
export function matchesInventoryQuery(device: Device, query: InventoryQuery, today: string): boolean {
  const values = textFilters.map(({ key }) => normalized(device[key]));
  if (
    !normalized(query.keyword)
      .split(/\s+/)
      .filter(Boolean)
      .every((word) => values.some((value) => value.includes(word)))
  )
    return false;
  if (!query.conditions.every(({ field, value }) => normalized(device[field]).includes(normalized(value)))) return false;
  if (query.types.length && !query.types.includes(device.type)) return false;
  if (query.warrantyTypes.length && !query.warrantyTypes.includes(device.warrantyType)) return false;
  if (query.warrantyStatuses.length && !query.warrantyStatuses.includes(warrantyStatus(device.expire, today))) return false;
  if (query.expireFrom || query.expireTo) {
    const date = String(device.expire || '');
    const timestamp = Date.parse(date);
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(timestamp) || new Date(timestamp).toISOString().slice(0, 10) !== date) return false;
    if (query.expireFrom && date < query.expireFrom) return false;
    if (query.expireTo && date > query.expireTo) return false;
  }
  return true;
}
