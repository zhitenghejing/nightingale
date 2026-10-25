import React, { useRef, useState } from 'react';
import { Alert, Button, Checkbox, Form, InputNumber, Modal, Space, Table, message } from 'antd';
import { api, Permissions } from './api';
import { Device } from './model';
export default function Administration({ access, onChanged }: { access?: { permissions: Permissions; admin: boolean }; onChanged: () => Promise<void> }) {
  const [panel, setPanel] = useState('');
  const [rows, setRows] = useState<any[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [purging, setPurging] = useState<{ all: boolean; targets: { id: string; version: number }[] }>();
  const [purgeError, setPurgeError] = useState('');
  const requestId = useRef(0);
  const [form] = Form.useForm();
  const close = () => {
    requestId.current++;
    setPanel('');
    setRows([]);
    setError('');
    setPurging(undefined);
    setLoading(false);
    if (panel === 'acl') form.resetFields();
  };
  const open = async (name: string) => {
    const request = ++requestId.current;
    setPanel(name);
    setRows([]);
    setError('');
    setLoading(true);
    if (panel === 'acl') form.resetFields();
    try {
      const result = await api<any[]>(name);
      if (request === requestId.current) setRows(result);
    } catch (e) {
      if (request === requestId.current) setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      if (request === requestId.current) setLoading(false);
    }
  };
  const confirmPurge = (targets: Device[], all: boolean) => {
    setPurgeError('');
    setPurging({ all, targets: targets.map(({ id, version }) => ({ id, version: version ?? 0 })) });
  };
  const purge = async () => {
    if (!purging || busy) return;
    setBusy(true);
    setPurgeError('');
    try {
      await api('purge', 'POST', purging.targets);
    } catch (e) {
      setPurgeError(e instanceof Error ? e.message : '彻底删除失败');
      setBusy(false);
      return;
    }
    setPurging(undefined);
    message.success(`已彻底删除 ${purging.targets.length} 台设备`);
    await open('devices?deleted=true');
    try {
      await onChanged();
    } catch (e) {
      setError('设备已彻底删除，但刷新资产列表失败，请刷新页面');
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Space className='mt-4'>
        <Button disabled={!access?.permissions.read} onClick={() => open('devices?deleted=true')}>
          回收站
        </Button>
        <Button disabled={!access?.permissions.read} onClick={() => open('changes')}>
          变更记录
        </Button>
        {access?.admin && <Button onClick={() => open('acl')}>CMDB 权限管理</Button>}
      </Space>
      <Modal
        visible={!!panel}
        width={1000}
        title={panel === 'acl' ? 'CMDB 权限管理' : panel === 'changes' ? '最近 200 条变更记录' : '设备回收站'}
        onCancel={() => !busy && close()}
        maskClosable={!busy}
        keyboard={!busy}
        closable={!busy}
        footer={
          <Button disabled={busy} onClick={close}>
            关闭
          </Button>
        }
        destroyOnClose
      >
        {error && <Alert type='error' message={error} />}
        {panel === 'acl' ? (
          <>
            <Alert className='mb-4' type='info' message='夜莺管理员拥有全部权限。其他账号需按用户 ID 授权；权限作用于全部 CMDB 资产，请仅授权给相应运维人员。' />
            <Form
              form={form}
              layout='inline'
              onFinish={async (values) => {
                try {
                  await api('acl', 'PUT', { read: false, create: false, edit: false, delete: false, import: false, export: false, ...values });
                  message.success('权限已保存');
                  await open('acl');
                } catch (e) {
                  setError(e instanceof Error ? e.message : '保存失败');
                }
              }}
            >
              <Form.Item name='userId' label='夜莺用户 ID' rules={[{ required: true }]}>
                <InputNumber min={1} precision={0} />
              </Form.Item>
              {(['read', 'create', 'edit', 'delete', 'import', 'export'] as const).map((k, i) => (
                <Form.Item key={k} name={k} valuePropName='checked'>
                  <Checkbox>{['查看', '添加', '编辑/移动/恢复', '删除', '导入', '导出'][i]}</Checkbox>
                </Form.Item>
              ))}
              <Button htmlType='submit' type='primary'>
                保存授权
              </Button>
            </Form>
            <Table
              rowKey='userId'
              size='small'
              dataSource={rows}
              columns={[
                { title: '用户 ID', dataIndex: 'userId' },
                ...(['read', 'create', 'edit', 'delete', 'import', 'export'] as const).map((k) => ({ title: k, dataIndex: k, render: (v: boolean) => (v ? '允许' : '禁止') })),
                {
                  title: '操作',
                  render: (_, row) => (
                    <Button
                      onClick={() => {
                        form.resetFields();
                        form.setFieldsValue(row);
                      }}
                    >
                      编辑
                    </Button>
                  ),
                },
              ]}
            />
          </>
        ) : panel === 'changes' ? (
          <Table
            rowKey='id'
            size='small'
            loading={loading}
            dataSource={rows}
            columns={[
              { title: '时间', dataIndex: 'createdAt' },
              { title: '用户 ID', dataIndex: 'userId' },
              { title: '操作', dataIndex: 'action' },
              { title: '资产 ID', dataIndex: 'assetId' },
            ]}
            expandable={{
              expandedRowRender: (row) => (
                <>
                  <p>修改前</p>
                  <pre style={{ whiteSpace: 'pre-wrap' }}>{row.before}</pre>
                  <p>修改后</p>
                  <pre style={{ whiteSpace: 'pre-wrap' }}>{row.after}</pre>
                </>
              ),
            }}
          />
        ) : (
          <>
            <Space className='mb-4' wrap>
              <span>共 {rows.length} 台设备</span>
              <Button disabled={busy || loading} onClick={() => open('devices?deleted=true')}>
                刷新回收站
              </Button>
              <Button danger disabled={!access?.permissions.delete || busy || loading || !!error || !rows.length} onClick={() => confirmPurge(rows, true)}>
                一键清空
              </Button>
            </Space>
            <Table
              rowKey='id'
              size='small'
              loading={loading}
              dataSource={rows}
              columns={[
                { title: '名称', dataIndex: 'hostname' },
                { title: '序列号', dataIndex: 'serial' },
                { title: '原机柜', dataIndex: 'rack' },
                {
                  title: '操作',
                  render: (_, row: Device) => (
                    <Space>
                      <Button
                        disabled={!access?.permissions.edit || busy || loading}
                        onClick={async () => {
                          setBusy(true);
                          try {
                            await api('restore', 'POST', [row]);
                            await onChanged();
                            await open('devices?deleted=true');
                            message.success('已恢复设备');
                          } catch (e) {
                            setError(e instanceof Error ? e.message : '恢复失败');
                          } finally {
                            setBusy(false);
                          }
                        }}
                      >
                        恢复
                      </Button>
                      <Button danger disabled={!access?.permissions.delete || busy || loading} onClick={() => confirmPurge([row], false)}>
                        彻底删除
                      </Button>
                    </Space>
                  ),
                },
              ]}
            />
          </>
        )}
      </Modal>
      <Modal
        visible={!!purging}
        title={purging?.all ? '清空回收站' : '彻底删除设备'}
        okText={purging?.all ? '确认清空' : '确认彻底删除'}
        cancelText='取消'
        okButtonProps={{ danger: true }}
        confirmLoading={busy}
        cancelButtonProps={{ disabled: busy }}
        closable={!busy}
        maskClosable={!busy}
        keyboard={!busy}
        onCancel={() => !busy && setPurging(undefined)}
        onOk={purge}
        destroyOnClose
      >
        <Alert type='warning' showIcon message={`确认彻底删除这 ${purging?.targets.length || 0} 台设备？设备及磁盘信息将永久删除，无法恢复。变更记录仍会保留。`} />
        {purgeError && <Alert className='mt-4' type='error' showIcon message={purgeError} />}
      </Modal>
    </>
  );
}
