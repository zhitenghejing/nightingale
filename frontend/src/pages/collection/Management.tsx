import { formError, errorText } from './errors';
import React, { useContext, useEffect, useState } from 'react';
import { Alert, Button, Drawer, Form, Input, InputNumber, Modal, Space, Table, Tabs } from 'antd';
import { useTranslation } from 'react-i18next';
import { CommonStateContext } from '@/App';
import { collectionRequest, Collector, Credential, getCollectors, getCredentials } from '@/services/collection';
import { RequestMethod } from '@/store/common';
import DeviceDetail from './DeviceDetail';

export default function Management({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation('collection');
  const { profile, perms } = useContext(CommonStateContext);
  const [collectors, setCollectors] = useState<Collector[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [templates, setTemplates] = useState<any[]>([]);
  const [publications, setPublications] = useState<any[]>([]);
  const [pending, setPending] = useState<any[]>([]);
  const [error, setError] = useState('');
  const [register, setRegister] = useState(false);
  const [collectorEditing, setCollectorEditing] = useState<Collector>();
  const [secret, setSecret] = useState('');
  const [editing, setEditing] = useState<Credential>();
  const [affected, setAffected] = useState(0);
  const [device, setDevice] = useState('');
  const [busy, setBusy] = useState(false);
  const [form] = Form.useForm();
  const [credentialForm] = Form.useForm();
  const canWrite = profile.admin || perms?.includes('/collection/write');
  const canDelete = profile.admin || perms?.includes('/collection/delete');
  const load = () => {
    setError('');
    Promise.all([getCollectors(), getCredentials(), collectionRequest('templates'), collectionRequest('publications')])
      .then(([c, cr, tpl, pub]) => {
        setCollectors(c);
        setCredentials(cr);
        setTemplates(tpl);
        setPublications(pub);
      })
      .catch((e) => setError(e.message || t('query_failed')));
    if (profile.admin)
      collectionRequest('devices?include_deleted=true&limit=50')
        .then((r) => setPending(r.list.filter((d) => !d.enabled || d.deleted)))
        .catch((e) => setError(e.message));
  };
  useEffect(load, []);
  const split = (v: string) =>
    v
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
  const action = (path: string, method: RequestMethod, data?: any) =>
    collectionRequest(path, method, data)
      .then(load)
      .catch((e) => setError(e.message || t('query_failed')));
  return (
    <Drawer visible width='85%' title={t('manage')} onClose={onClose} destroyOnClose>
      {error && <Alert type='error' message={error} showIcon className='mb-2' />}
      <Button onClick={load}>{t('refresh')}</Button>
      <Tabs>
        <Tabs.TabPane tab={t('collectors')} key='collectors'>
          {profile.admin && (
            <Button
              className='mb-2'
              onClick={() => {
                form.resetFields();
                setCollectorEditing(undefined);
                setRegister(true);
              }}
            >
              {t('add_collector')}
            </Button>
          )}
          <Table
            rowKey='id'
            dataSource={collectors}
            columns={[
              { title: t('collector'), dataIndex: 'name' },
              { title: t('agent_ident'), dataIndex: 'agent_ident' },
              { title: t('datasource'), dataIndex: 'datasource_id' },
              { title: t('phase'), render: (_, c) => (c.last_client_version === c.desired_version ? t('client_seen') : t('pending')) },
              {
                title: '',
                render: (_, c) =>
                  profile.admin && (
                    <Space>
                      <Button
                        onClick={() => {
                          setCollectorEditing(c);
                          form.resetFields();
                          form.setFieldsValue({ ...c, group_ids: c.group_ids.join(',') });
                          setRegister(true);
                        }}
                      >
                        {t('edit')}
                      </Button>
                      <Button
                        onClick={async () => {
                          try {
                            const r = await collectionRequest(`collectors/${c.id}/rotate`, RequestMethod.Post, { record_version: c.record_version });
                            setSecret(r.secret);
                            load();
                          } catch (e) {
                            setError(errorText(e, t('query_failed')));
                          }
                        }}
                      >
                        {t('rotate')}
                      </Button>
                      <Button danger onClick={() => action(`collectors/${c.id}`, RequestMethod.Delete, { record_version: c.record_version })}>
                        {t('delete')}
                      </Button>
                    </Space>
                  ),
              },
            ]}
          />
        </Tabs.TabPane>
        <Tabs.TabPane tab={t('credentials')} key='credentials'>
          <Table
            rowKey='id'
            dataSource={credentials}
            columns={[
              { title: t('credential'), dataIndex: 'name' },
              { title: t('group'), dataIndex: 'group_id' },
              {
                title: '',
                render: (_, c) => (
                  <Space>
                    {canWrite && (
                      <Button
                        onClick={async () => {
                          try {
                            const refs = await collectionRequest(`credentials/${c.id}/references`);
                            setAffected(refs.affected_devices);
                            credentialForm.resetFields();
                            credentialForm.setFieldsValue({ name: c.name });
                            setEditing(c);
                          } catch (e) {
                            setError(errorText(e, t('query_failed')));
                          }
                        }}
                      >
                        {t('edit')}
                      </Button>
                    )}
                    {canDelete && (
                      <Button
                        danger
                        onClick={() => Modal.confirm({ title: t('delete'), onOk: () => action(`credentials/${c.id}`, RequestMethod.Delete, { record_version: c.record_version }) })}
                      >
                        {t('delete')}
                      </Button>
                    )}
                  </Space>
                ),
              },
            ]}
          />
        </Tabs.TabPane>
        <Tabs.TabPane tab={t('templates')} key='templates'>
          <Table
            rowKey='id'
            dataSource={templates}
            columns={[
              { title: t('name'), dataIndex: 'name' },
              { title: 'ID', dataIndex: 'id' },
              {
                title: '',
                render: (_, tpl) => (
                  <Button
                    onClick={() =>
                      Modal.info({
                        title: tpl.name,
                        width: 700,
                        content: (
                          <pre style={{ whiteSpace: 'pre-wrap' }}>{JSON.stringify(typeof tpl.definition === 'string' ? JSON.parse(tpl.definition) : tpl.definition, null, 2)}</pre>
                        ),
                      })
                    }
                  >
                    {t('detail')}
                  </Button>
                ),
              },
            ]}
          />
        </Tabs.TabPane>
        <Tabs.TabPane tab={t('publications')} key='publications'>
          <Table
            rowKey='id'
            dataSource={publications}
            columns={[
              { title: t('collector'), dataIndex: 'collector_id' },
              { title: t('phase'), dataIndex: 'reason' },
              { title: 'Version', dataIndex: 'version' },
              {
                title: '',
                render: (_, p) =>
                  profile.admin && (
                    <Button onClick={() => Modal.confirm({ title: t('confirm_rollback'), onOk: () => action(`publications/${p.id}/rollback`, RequestMethod.Post) })}>
                      {t('rollback')}
                    </Button>
                  ),
              },
            ]}
          />
        </Tabs.TabPane>
        {profile.admin && (
          <Tabs.TabPane tab={t('pending_deletions')} key='pending'>
            <Alert type='info' message={t('deleted_hint')} />
            <Table
              rowKey='id'
              dataSource={pending}
              columns={[
                { title: t('name'), dataIndex: 'name' },
                { title: t('phase'), render: (_, d) => t(d.collection.configuration_state) },
                { title: '', render: (_, d) => <Button onClick={() => setDevice(d.id)}>{t('detail')}</Button> },
              ]}
            />
          </Tabs.TabPane>
        )}
      </Tabs>
      <Modal
        visible={register}
        title={t(collectorEditing ? 'edit' : 'add_collector')}
        onCancel={() => setRegister(false)}
        confirmLoading={busy}
        onOk={async () => {
          setBusy(true);
          try {
            const f = await form.validateFields();
            const r = await collectionRequest(collectorEditing ? `collectors/${collectorEditing.id}` : 'collectors', collectorEditing ? RequestMethod.Put : RequestMethod.Post, {
              ...f,
              record_version: collectorEditing?.record_version || 0,
              group_ids: split(f.group_ids).map(Number),
            });
            if (r.secret) setSecret(r.secret);
            setRegister(false);
            load();
          } catch (e) {
            if (!formError(e)) setError(errorText(e, t('query_failed')));
          } finally {
            setBusy(false);
          }
        }}
      >
        <Form form={form} layout='vertical'>
          {['name', 'agent_ident', 'writer_user', 'group_ids'].map((name) => (
            <Form.Item key={name} name={name} label={t(name === 'group_ids' ? 'groups' : name)} rules={[{ required: true }]}>
              <Input />
            </Form.Item>
          ))}
          <Form.Item name='datasource_id' label={t('datasource')} rules={[{ required: true }]}>
            <InputNumber min={1} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        visible={!!editing}
        title={t('edit')}
        onCancel={() => {
          setEditing(undefined);
          credentialForm.resetFields();
        }}
        confirmLoading={busy}
        onOk={async () => {
          if (!editing) return;
          setBusy(true);
          try {
            const f = await credentialForm.validateFields();
            await collectionRequest(`credentials/${editing.id}`, RequestMethod.Put, { ...f, group_id: editing.group_id, record_version: editing.record_version });
            credentialForm.resetFields();
            setEditing(undefined);
            load();
          } catch (e) {
            if (!formError(e)) setError(errorText(e, t('query_failed')));
          } finally {
            setBusy(false);
          }
        }}
      >
        <Alert type='warning' message={t('affected', { count: affected })} className='mb-2' />
        <Form form={credentialForm} layout='vertical'>
          <Form.Item name='name' label={t('credential_name')} rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name='community' label={t('community')} rules={[{ required: true }]}>
            <Input.Password autoComplete='new-password' />
          </Form.Item>
        </Form>
      </Modal>
      <Modal visible={!!secret} title={t('token_once')} onCancel={() => setSecret('')} onOk={() => setSecret('')} destroyOnClose>
        <Input.TextArea readOnly value={secret} autoComplete='off' />
      </Modal>
      {device && <DeviceDetail id={device} onClose={() => setDevice('')} onChanged={load} />}
    </Drawer>
  );
}
