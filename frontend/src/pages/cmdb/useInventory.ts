import { useCallback, useEffect, useRef, useState } from 'react';
import { api, Catalog, Permissions, RackAlert } from './api';
import { Device } from './model';
export default function useInventory(includeRackAlerts = false) {
  const [devices, setDevices] = useState<Device[]>([]);
  const [catalog, setCatalog] = useState<Catalog>({ rooms: [], racks: [] });
  const [access, setAccess] = useState<{ permissions: Permissions; admin: boolean }>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [rackAlerts, setRackAlerts] = useState<Record<string, RackAlert[]>>({});
  const [rackAlertsFailed, setRackAlertsFailed] = useState(false);
  const revision = useRef(0);
  const reload = useCallback(async () => {
    const attempt = ++revision.current;
    try {
      const permission = await api<{ permissions: Permissions; admin: boolean }>('access');
      if (attempt !== revision.current) return;
      setAccess(permission);
      if (!permission.permissions.read) throw new Error('当前账号没有 CMDB 查看权限，请联系致腾慧运维管理员授权');
      if (includeRackAlerts) {
        // A temporary alert error should leave the rack and device catalog usable.
        void api<Record<string, RackAlert[]>>('rack-alerts').then(
          (data) => {
            if (attempt !== revision.current) return;
            setRackAlerts(data);
            setRackAlertsFailed(false);
          },
          () => {
            if (attempt !== revision.current) return;
            setRackAlerts({});
            setRackAlertsFailed(true);
          },
        );
      }
      const [rows, hierarchy] = await Promise.all([api<Device[]>('devices'), api<Catalog>('catalog')]);
      if (attempt !== revision.current) return;
      setDevices(rows);
      setCatalog(hierarchy);
      setError('');
    } catch (e) {
      if (attempt === revision.current) {
        setError(e instanceof Error ? e.message : '加载失败');
        setRackAlerts({});
      }
      throw e;
    } finally {
      if (attempt === revision.current) setLoading(false);
    }
  }, [includeRackAlerts]);
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
      revision.current++;
      window.clearInterval(timer);
      window.removeEventListener('focus', refresh);
    };
  }, [reload]);
  return { devices, catalog, access, error, loading, reload, rackAlerts, rackAlertsFailed };
}
