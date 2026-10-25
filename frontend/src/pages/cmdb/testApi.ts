// Shared in-memory API double for component tests; production always calls the service.
import { sampleDevices, Device } from './model';
let rows: Device[] = [];
export const catalog = {
  rooms: [{ id: 'room', name: '北院7号楼11F' }],
  racks: [
    { id: 'rack', roomId: 'room', name: '5号柜', units: 42, version: 1 },
    { id: 'rack2', roomId: 'room', name: '6号柜', units: 48, version: 1 },
  ],
};
export function resetMock() {
  rows = sampleDevices().map((d) => ({ ...d, version: 1, rackId: d.rack === '6号柜' ? 'rack2' : 'rack' }));
}
export function setMockDevices(devices: Device[]) {
  rows = devices;
}
export const saveAsset = jest.fn(async (d: Device, create: boolean) => {
  const saved = { ...d, id: create ? 'created' : d.id, version: (d.version || 0) + 1 };
  rows = create ? [...rows, saved] : rows.map((x) => (x.id === saved.id ? saved : x));
  return saved;
});
export const api = jest.fn(async (path: string) => {
  if (path === 'access') return { admin: true, permissions: { userId: 1, read: true, create: true, edit: true, delete: true, import: true, export: true } };
  if (path === 'catalog') return catalog;
  if (path === 'devices' || path === 'export') return rows;
  return [];
});
