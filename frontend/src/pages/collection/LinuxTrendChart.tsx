import React, { useId, useMemo } from 'react';
import moment from 'moment';
import Timeseries from '@/pages/dashboard/Renderer/Renderer/TimeSeriesNG';
import { defaultCustomValuesMap } from '@/pages/dashboard/Editor/config';
import type { IPanel } from '@/pages/dashboard/types';
import type { CalculatedSeries } from '@/pages/dashboard/Renderer/utils/getCalculatedValuesBySeries';

interface Point { time: number; value: number; series: string; }
const units: Record<string, string> = { '%': 'percent', bytes: 'bytesIEC', 'bytes/s': 'bytesSecIEC', 'bit/s': 'bitsSecSI', '次/秒': 'iops' };

// Reuse the template dashboard's renderer, palette, axes, legend and tooltip.
export default function LinuxTrendChart({ points, unit }: { points: Point[]; unit: string }) {
  const id = `linux-trend-${useId().replace(/:/g, '')}`;
  const series = useMemo(() => {
    const grouped = new Map<string, CalculatedSeries>();
    for (const point of points) {
      let item = grouped.get(point.series);
      if (!item) {
        const refId = `A${grouped.size}`;
        item = { id: `${id}-${refId}`, refId, name: point.series, metric: {}, data: [], target: { refId, datasource: { cate: 'prometheus', id: 0 } } };
        grouped.set(point.series, item);
      }
      // Linux chart points are milliseconds; dashboard time series use seconds.
      item.data.push([point.time / 1000, point.value]);
    }
    return [...grouped.values()];
  }, [points, id]);
  const panel: IPanel = {
    id, name: '', description: '', type: 'timeseries',
    layout: { i: id, x: 0, y: 0, w: 12, h: 8 },
    targets: [], overrides: [],
    custom: { ...defaultCustomValuesMap.timeseries, fillOpacity: 0.03, spanNulls: false },
    options: {
      standardOptions: { unit: units[unit] || 'none', decimals: 2 },
      tooltip: { mode: 'all', sort: 'desc' },
      legend: {
        displayMode: series.length > 1 ? 'list' : 'hidden', placement: 'bottom',
        calcs: [], columns: [], detailName: '', detailUrl: '', behaviour: 'showItem', selectMode: 'multiple',
      },
    },
  };
  const time = useMemo(() => { const end = moment(); return { start: end.clone().subtract(1, 'hour'), end }; }, [points]);
  return <div className='linux-dashboard-trend' style={{ height: 280 }}><Timeseries id={id} values={panel} series={series} annotations={[]} time={time} isPreview /></div>;
}
