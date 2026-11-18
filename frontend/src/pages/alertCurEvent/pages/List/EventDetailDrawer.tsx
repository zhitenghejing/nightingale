import React, { Suspense } from 'react';
import { Drawer, Spin } from 'antd';
import { useTranslation } from 'react-i18next';

import { NS } from '../../constants';
import type { Props } from './EventDetailDrawerContent';

// Detail includes datasource editors and graphs. Keep it out of the list's
// initial dependency graph, including while a closed drawer is mounted.
const Content = React.lazy(() => import('./EventDetailDrawerContent'));

export default function EventDetailDrawer(props: Props) {
  const { t } = useTranslation(NS);
  if (!props.visible) return null;

  return (
    <Suspense
      fallback={
        <Drawer width='80%' title={t('detail_title')} visible onClose={props.onClose} className='n9e-antd-drawer'>
          <Spin />
        </Drawer>
      }
    >
      <Content {...props} />
    </Suspense>
  );
}
