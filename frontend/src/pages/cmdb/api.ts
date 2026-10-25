import request from '@/utils/request';
import { Device } from './model';
export interface Room {
  id: string;
  name: string;
}
export interface Rack {
  id: string;
  roomId: string;
  name: string;
  units: number;
  version: number;
}
export interface Catalog {
  rooms: Room[];
  racks: Rack[];
}
export interface Permissions {
  userId: number;
  read: boolean;
  create: boolean;
  edit: boolean;
  delete: boolean;
  import: boolean;
  export: boolean;
}
export const api = async <T>(path: string, method = 'GET', data?: unknown): Promise<T> => {
  let result;
  try {
    result = await request(`/api/cmdb/${path}`, { method, data, silence: true });
  } catch (error) {
    if (error instanceof Error) throw error;
    // The shared request interceptor rejects HTTP failures as plain objects.
    const detail = error && typeof error === 'object' && 'message' in error && typeof error.message === 'string' ? error.message : 'CMDB 请求失败，请重试';
    throw new Error(detail);
  }
  if (!result || typeof result !== 'object' || !Object.prototype.hasOwnProperty.call(result, 'dat') || result.err) throw new Error(result?.err || '服务未返回有效结果');
  return result.dat;
};
export const saveAsset = (device: Device, create: boolean) => api<Device>(create ? 'devices' : `devices/${device.id}`, create ? 'POST' : 'PUT', device);
