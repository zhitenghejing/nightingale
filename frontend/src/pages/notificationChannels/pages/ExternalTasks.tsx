import React, { useEffect, useState } from 'react';
import { Alert, Button, Drawer, Form, Input, message, Modal, Select, Space, Table, Tag } from 'antd';
import { useTranslation } from 'react-i18next';
import PageLayout from '@/components/pageLayout';
import request from '@/utils/request';
import { RequestMethod } from '@/store/common';
import { NS } from '../constants';

export const taskStates = ['pending', 'sending', 'accepted', 'confirmed_received', 'not_delivered', 'validation_failed', 'unknown', 'waiting_predecessor', 'manual'];
interface Task {
  id: number;
  destination: string;
  state: string;
  reason: string;
  dispatch_id: string;
  incident_id: string;
  sequence: number;
  attempts: number;
  version: number;
  channel_id: number;
}
interface Detail {
  omitted_tags?: { label: string; reason: string }[];
  payload_preview?: Record<string, string>;
  task: Task;
  attempts: { id: number; number: number; state: string; summary: string; started_at: number; finished_at: number }[];
  operations: { id: number; actor: string; action: string; reason: string; before_state: string; after_state: string; created_at: number }[];
}

export default function ExternalTasks() {
  const { t } = useTranslation(NS);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [filter, setFilter] = useState<string>();
  const [before, setBefore] = useState<number>();
  const [detail, setDetail] = useState<Detail>();
  const [action, setAction] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [form] = Form.useForm();
  const load = () => request('/api/n9e/external-push-tasks', { method: RequestMethod.Get, params: { state: filter, before } }).then((res) => setTasks(res.dat || []));
  const open = (id: number) => request(`/api/n9e/external-push-tasks/${id}`, { method: RequestMethod.Get }).then((res) => setDetail(res.dat));
  useEffect(() => {
    load();
  }, [filter, before]);
  const when = (v: number) => (v ? new Date(v).toLocaleString() : '-');
  return (
    <PageLayout title={t('external.tasks')} showBack>
      <div className='n9e'>
        <Alert className='mb-4' type='warning' showIcon message={t('external.simulation_notice')} description={t('external.recovery_notice')} />
        <Alert className='mb-4' type='info' message={t('external.accepted_notice')} />
        <Space className='mb-4'>
          <Select
            allowClear
            style={{ width: 230 }}
            placeholder={t('external.state')}
            value={filter}
            onChange={(value) => {
              setFilter(value);
              setBefore(undefined);
            }}
            options={taskStates.map((value) => ({ value, label: t(`external.states.${value}`) }))}
          />
          <Button
            onClick={() => {
              load();
              if (detail) open(detail.task.id);
            }}
          >
            {t('external.refresh')}
          </Button>
          <Button onClick={() => setBefore(undefined)}>{t('external.latest')}</Button>
          <Button disabled={tasks.length < 100} onClick={() => setBefore(tasks[tasks.length - 1]?.id)}>
            {t('external.older')}
          </Button>
        </Space>
        <Table<Task>
          rowKey='id'
          dataSource={tasks}
          pagination={false}
          scroll={{ x: 1000 }}
          columns={[
            { title: 'ID', dataIndex: 'id', width: 75 },
            { title: t('external.destination'), dataIndex: 'destination', width: 170 },
            { title: t('external.state'), dataIndex: 'state', width: 180, render: (v) => <Tag>{t(`external.states.${v}`)}</Tag> },
            { title: t('external.reason'), dataIndex: 'reason', render: (value) => <span style={{ overflowWrap: 'anywhere' }}>{value}</span> },
            { title: t('external.attempts'), dataIndex: 'attempts', width: 100 },
            {
              title: t('external.detail'),
              width: 90,
              render: (_, row) => (
                <Button type='link' onClick={() => open(row.id)}>
                  {t('external.detail')}
                </Button>
              ),
            },
          ]}
        />
        <Drawer width={960} visible={!!detail} title={t('external.detail')} onClose={() => setDetail(undefined)}>
          {detail && (
            <>
              <p>
                ID: {detail.task.id} · {t(`external.states.${detail.task.state}`)}
              </p>
              <p style={{ overflowWrap: 'anywhere' }}>
                {t('external.reason')}: {detail.task.reason}
              </p>
              <p>
                {t('external.dispatch')}: {detail.task.dispatch_id}
              </p>
              <p>
                {t('external.incident')}: {detail.task.incident_id} / {detail.task.sequence}
              </p>
              <Alert className='mb-3' type='info' message={t('external.pairing_notice')} />
              {!!detail.omitted_tags?.length && <Alert className='mb-3' type='info' message={t('external.labels.omitted_title')} description={detail.omitted_tags.map((item) => `${item.label}: ${t(`external.labels.omitted_${item.reason}`)}`).join('；')} />}
              <h3>{t('external.payload_title')}</h3>
              <p>{t('external.payload_notice')}</p>
              {detail.payload_preview ? (
                <pre aria-label={t('external.payload_title')} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
                  {JSON.stringify(detail.payload_preview, null, 2)}
                </pre>
              ) : (
                <p>{t('external.payload_unavailable')}</p>
              )}
              <Space className='mb-4'>
                {['unknown', 'manual'].includes(detail.task.state) && (
                  <>
                    <Button
                      disabled={!detail.task.attempts}
                      onClick={() => {
                        form.resetFields();
                        setAction('confirm_received');
                      }}
                    >
                      {t('external.confirm_received')}
                    </Button>
                    <Button
                      danger
                      onClick={() => {
                        form.resetFields();
                        setAction('authorize_resend');
                      }}
                    >
                      {t('external.authorize_resend')}
                    </Button>
                  </>
                )}
                {!detail.task.attempts && ['pending', 'waiting_predecessor', 'manual', 'validation_failed'].includes(detail.task.state) && (
                  <Button
                    danger
                    onClick={() => {
                      form.resetFields();
                      setAction('cancel_unsent');
                    }}
                  >
                    {t('external.cancel_unsent')}
                  </Button>
                )}
                {!['sending', 'accepted', 'confirmed_received', 'not_delivered'].includes(detail.task.state) && (
                  <Button
                    onClick={() => {
                      form.resetFields();
                      setAction('hold');
                    }}
                  >
                    {t('external.hold')}
                  </Button>
                )}
              </Space>
              <h3>{t('external.attempts')}</h3>
              <Table
                rowKey='id'
                dataSource={detail.attempts}
                pagination={false}
                scroll={{ x: 800 }}
                columns={[
                  { title: '#', dataIndex: 'number', width: 50 },
                  { title: t('external.state'), dataIndex: 'state', width: 140, render: (v) => t(`external.states.${v}`) },
                  { title: t('external.started'), dataIndex: 'started_at', width: 140, render: when },
                  { title: t('external.finished'), dataIndex: 'finished_at', width: 140, render: when },
                  { title: t('external.summary'), dataIndex: 'summary', render: (value) => <span style={{ overflowWrap: 'anywhere' }}>{value}</span> },
                ]}
              />
              <h3 className='mt-4'>{t('external.audit')}</h3>
              <Table
                rowKey='id'
                dataSource={detail.operations}
                pagination={false}
                columns={[
                  { title: t('external.actor'), dataIndex: 'actor' },
                  { title: t('external.action'), dataIndex: 'action' },
                  { title: t('external.reason'), dataIndex: 'reason', render: (value) => <span style={{ overflowWrap: 'anywhere' }}>{value}</span> },
                  { title: t('external.started'), dataIndex: 'created_at', render: when },
                ]}
              />
            </>
          )}
        </Drawer>
        <Modal
          visible={!!action}
          title={action ? t(`external.${action}`) : ''}
          confirmLoading={busy}
          onCancel={() => setAction(undefined)}
          onOk={async () => {
            if (!detail || !action) return;
            // Form validation already renders its error beside the reason.
            // Keep the modal open without an unhandled click-handler rejection.
            const values = await form.validateFields().catch(() => undefined);
            if (!values) return;
            setBusy(true);
            try {
              await request(`/api/n9e/external-push-tasks/${detail.task.id}/actions`, {
                method: RequestMethod.Post,
                data: { action, version: detail.task.version, reason: values.reason },
              });
              message.success(t('external.recorded'));
              setAction(undefined);
              await open(detail.task.id);
              await load();
            } finally {
              setBusy(false);
            }
          }}
        >
          <Alert className='mb-4' type='warning' showIcon message={t(action === 'cancel_unsent' ? 'external.cancel_notice' : 'external.manual_notice')} />
          <Form form={form} layout='vertical'>
            <Form.Item name='reason' label={t('external.reason')} rules={[{ required: true, whitespace: true }, { max: 250 }]}>
              <Input.TextArea rows={4} />
            </Form.Item>
          </Form>
        </Modal>
      </div>
    </PageLayout>
  );
}
