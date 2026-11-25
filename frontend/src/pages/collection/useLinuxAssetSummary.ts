import { useEffect, useState } from 'react';
import request from '@/utils/request';
import { RequestMethod } from '@/store/common';
import type { DeviceDetailData } from '@/services/collection';
import { errorText } from './errors';

interface Summary { cores?: number; memoryBytes?: number; uptimeSeconds?: number; error?: string; }
const metrics = ['CPU 核数', '物理内存总量', '设备运行时间'];
export function assetNumber(value: number | undefined, suffix = '') {
  return value === undefined ? '—' : `${value.toLocaleString(undefined, { maximumFractionDigits: 2 })}${suffix}`;
}
export function assetBytes(value?: number) {
  return value === undefined ? '—' : value >= 1024 ** 3 ? assetNumber(value / 1024 ** 3, ' GiB') : assetNumber(value / 1024 ** 2, ' MiB');
}
// Configuration capacity is an estimate; retain the measured bytes separately.
export function assetMemoryCapacity(value?: number) {
  return value === undefined ? '—' : value >= 1024 ** 3 ? `${Math.ceil(value / 1024 ** 3)} GB` : `${Math.ceil(value / 1024 ** 2)} MB`;
}
export default function useLinuxAssetSummary(data: DeviceDetailData | undefined, enabled: boolean, refresh: number): Summary {
  const [summary, setSummary] = useState<Summary>({});
  const id = data?.device?.id;
  const revision = data?.device?.collection.revision;
  const interval = data?.device?.interval_seconds || 30;
  const names = metrics.filter((name) => data?.metrics.some((metric) => metric.name === name)).join('|');
  useEffect(() => {
    let active = true;
    setSummary({});
    if (!enabled || !id || !names) return;
    const load = async () => {
      try {
        const params = new URLSearchParams([['step_seconds', '30'], ...names.split('|').map((name) => ['profile_metric', name])]);
        const result = await request(`/api/n9e/collection/devices/${id}/metrics?${params}`, { method: RequestMethod.Get, silence: true });
        const latest = (name: string): number | undefined => {
          let value: number | undefined; let time = 0;
          for (const series of result.dat.metrics[name] || []) for (const [timestamp, raw] of series.values || []) {
            const at = Number(timestamp); const sample = Number(raw);
            if (Number.isFinite(sample) && at >= Date.now() / 1000 - Math.max(120, interval * 3) && at >= time) { time = at; value = sample; }
          }
          return value;
        };
        if (active) setSummary({ cores: latest('CPU 核数'), memoryBytes: latest('物理内存总量'), uptimeSeconds: latest('设备运行时间') });
      } catch (e) { if (active) setSummary({ error: errorText(e, '资产指标查询失败') }); }
    };
    load(); const timer = window.setInterval(load, 30000);
    return () => { active = false; window.clearInterval(timer); };
  }, [enabled, id, revision, interval, names, refresh]);
  return summary;
}
