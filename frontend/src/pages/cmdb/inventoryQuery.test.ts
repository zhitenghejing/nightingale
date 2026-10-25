import { blankDevice, Device } from './model';
import { emptyQuery, matchesInventoryQuery, textFilters } from './inventoryQuery';
const today = '2026-10-23';
const device: Device = {
  ...blankDevice,
  id: 'asset',
  room: '北院机房',
  rack: 'A-01',
  hostname: 'WEB ALPHA',
  vendor: 'Dell',
  model: 'R650',
  serial: 'SN-001',
  business: '业务平台',
  businessIp: '192.0.2.11',
  expire: '2026-11-05',
};

it('combines case-insensitive keywords across fields and requires every keyword', () => {
  expect(matchesInventoryQuery(device, { ...emptyQuery(), keyword: ' 北院　dELL 192.0.2.11 ' }, today)).toBe(true);
  expect(matchesInventoryQuery(device, { ...emptyQuery(), keyword: 'Dell missing' }, today)).toBe(false);
  expect(matchesInventoryQuery(device, emptyQuery(), today)).toBe(true);
});
it.each(textFilters)('retains field-specific matching for $label', ({ key }) => {
  const query = { ...emptyQuery(), conditions: [{ field: key, value: String(device[key]).toUpperCase() }] };
  expect(matchesInventoryQuery(device, query, today)).toBe(true);
  expect(matchesInventoryQuery({ ...device, [key]: '' }, query, today)).toBe(false);
});
it('intersects text conditions, including two required substrings in the same field', () => {
  const conditions = [
    { field: 'hostname' as const, value: 'web' },
    { field: 'hostname' as const, value: 'alpha' },
    { field: 'vendor' as const, value: 'dell' },
  ];
  expect(matchesInventoryQuery(device, { ...emptyQuery(), conditions }, today)).toBe(true);
  expect(matchesInventoryQuery({ ...device, hostname: 'WEB beta' }, { ...emptyQuery(), conditions }, today)).toBe(false);
  expect(matchesInventoryQuery(device, { ...emptyQuery(), conditions: [{ field: 'vendor', value: 'R650' }] }, today)).toBe(false);
});
it('allows alternatives within categories and intersects categories with text and dates', () => {
  const query = {
    ...emptyQuery(),
    keyword: 'Dell',
    types: ['存储', '机架式服务器'],
    warrantyTypes: ['第三方维保', '官方维保'],
    warrantyStatuses: ['在保', '即将过保'],
    expireFrom: '2026-11-01',
    expireTo: '2026-11-30',
  };
  expect(matchesInventoryQuery(device, query, today)).toBe(true);
  expect(matchesInventoryQuery({ ...device, type: '交换机' }, query, today)).toBe(false);
  expect(matchesInventoryQuery({ ...device, warrantyType: '无维保' }, query, today)).toBe(false);
  expect(matchesInventoryQuery({ ...device, expire: '2026-10-01' }, query, today)).toBe(false);
  expect(matchesInventoryQuery({ ...device, expire: '2026-12-01' }, query, today)).toBe(false);
});
it('includes both date boundaries and supports either open end', () => {
  expect(matchesInventoryQuery(device, { ...emptyQuery(), expireFrom: device.expire, expireTo: device.expire }, today)).toBe(true);
  expect(matchesInventoryQuery(device, { ...emptyQuery(), expireFrom: '2026-11-06' }, today)).toBe(false);
  expect(matchesInventoryQuery(device, { ...emptyQuery(), expireTo: '2026-11-04' }, today)).toBe(false);
  expect(matchesInventoryQuery(device, { ...emptyQuery(), expireFrom: device.expire }, today)).toBe(true);
  expect(matchesInventoryQuery(device, { ...emptyQuery(), expireTo: device.expire }, today)).toBe(true);
});
it.each(['', 'invalid', '2026-02-30'])('excludes invalid expiry %s from a bounded search', (expire) => {
  expect(matchesInventoryQuery({ ...device, expire }, { ...emptyQuery(), expireFrom: '2026-01-01' }, today)).toBe(false);
});
it('supports missing optional API fields and the unfilled warranty status', () => {
  const minimal = JSON.parse('{"id":"minimal","customer":"API","serial":"MINIMAL"}');
  expect(matchesInventoryQuery(minimal, { ...emptyQuery(), keyword: 'minimal', warrantyStatuses: ['未填写'] }, today)).toBe(true);
  expect(matchesInventoryQuery(minimal, { ...emptyQuery(), keyword: 'minimal missing' }, today)).toBe(false);
});
