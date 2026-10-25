import { useCallback, useEffect, useState } from 'react';
import { api, Catalog, Permissions } from './api';
import { Device } from './model';
export default function useInventory() {
  const [devices, setDevices] = useState<Device[]>([]);
  const [catalog, setCatalog] = useState<Catalog>({ rooms: [], racks: [] });
  const [access, setAccess] = useState<{ permissions: Permissions; admin: boolean }>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const reload = useCallback(async () => {
    try {
      const permission = await api<{ permissions: Permissions; admin: boolean }>('access');
      setAccess(permission);
      if (!permission.permissions.read) throw new Error('当前账号没有 CMDB 查看权限，请联系夜莺管理员授权');
      const [rows, hierarchy] = await Promise.all([api<Device[]>('devices'), api<Catalog>('catalog')]);
      setDevices(rows);
      setCatalog(hierarchy);
      setError('');
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
      throw e;
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    let active = true;
    const refresh = () => {
      if (active && document.visibilityState !== 'hidden') void reload().catch(() => {});
    };
    refresh();
    const timer = window.setInterval(refresh, 10000);
    window.addEventListener('focus', refresh);
    return () => {
      active = false;
      window.clearInterval(timer);
      window.removeEventListener('focus', refresh);
    };
  }, [reload]);
  return { devices, catalog, access, error, loading, reload };
}
