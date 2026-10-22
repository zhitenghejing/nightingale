import React from 'react';
import type { DashboardVariablePlugin } from '@/pages/dashboard/Variables/pluginTypes';

// Exercise the public extension contract without depending on private Plus code.
export const dashboardVariablePlugins: Record<string, DashboardVariablePlugin> = {
  gcm: {
    capabilities: () => ({ multi: true, all: true }),
    transformQuery: (query) => query,
    selectPresentation: (query) => query?.query_type === 'metricTypes' ? {
      dropdownClassName: 'n9e-gcm-metric-dropdown',
      optionFilterProp: 'label_search',
      optionLabelProp: 'label_original',
      renderOption: (option) => ({
        content: <><div className='font-bold'>{option.label}</div><div className='italic'>{option.value}</div></>,
        labelOriginal: String(option.label),
        labelSearch: `${option.label} ${option.value} ${option.metricDescription ?? ''}`,
      }),
    } : undefined,
  },
};
