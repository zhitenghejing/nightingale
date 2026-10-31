import React, { lazy, Suspense, useContext, useState } from 'react';
import { Button, Space, Spin } from 'antd';
import { useTranslation } from 'react-i18next';
import { CommonStateContext } from '@/App';
const Editor = lazy(() => import('./DeviceEditor'));
const Detail = lazy(() => import('./DeviceDetail'));
const Management = lazy(() => import('./Management'));
export { Health } from './Health';
export function CollectionActions({ onChanged }: { onChanged: () => void }) {
  const { t } = useTranslation('collection');
  const { profile, perms } = useContext(CommonStateContext);
  const [create, setCreate] = useState(false);
  const [manage, setManage] = useState(false);
  const [id, setId] = useState('');
  return (
    <Space>
      {(profile.admin || perms?.includes('/collection/write')) && (
        <Button type='primary' onClick={() => setCreate(true)}>
          {t('add')}
        </Button>
      )}
      {(profile.admin || perms?.includes('/collection/read')) && <Button onClick={() => setManage(true)}>{t('manage')}</Button>}
      <Suspense fallback={<Spin />}>
        {create && (
          <Editor
            onClose={() => setCreate(false)}
            onSaved={(d) => {
              setCreate(false);
              setId(d.id);
              onChanged();
            }}
          />
        )}
        {id && <Detail id={id} onClose={() => setId('')} onChanged={onChanged} />}
        {manage && <Management onClose={() => setManage(false)} />}
      </Suspense>
    </Space>
  );
}
export function CollectionLink({ id, name, onChanged }: { id: string; name: string; onChanged: () => void }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <a onClick={() => setOpen(true)}>
        {name} <small>SNMP</small>
      </a>
      <Suspense fallback={<Spin />}>{open && <Detail id={id} onClose={() => setOpen(false)} onChanged={onChanged} />}</Suspense>
    </>
  );
}
