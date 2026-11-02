import React from 'react';
import { Tag } from 'antd';
import { useTranslation } from 'react-i18next';
export function Health({ state }: { state: string }) {
  const { t } = useTranslation('collection');
  return (
    <Tag color={state === 'healthy' ? 'green' : ['failed', 'interrupted', 'clock_error'].includes(state) ? 'red' : state === 'query_error' ? 'orange' : 'default'}>{t(state)}</Tag>
  );
}
