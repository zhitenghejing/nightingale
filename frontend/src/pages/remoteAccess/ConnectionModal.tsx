import React, { useContext, useEffect, useRef, useState } from 'react';
import { Alert, Button, Form, Modal, Popconfirm, Select } from 'antd';
import { CommonStateContext } from '@/App';
import { RequestMethod } from '@/store/common';
import { api, Connection, connectionPath, ConsoleProtocol } from './api';
import { HostKeyConfirmationCancelled } from './hostKeys';
import SSHSettings, { SSHSettingsHandle } from './SSHSettings';
export default function ConnectionModal({
  ident,
  host,
  groups,
  defaultProtocol,
  onClose,
}: {
  ident: string;
  host?: string;
  groups: number[];
  defaultProtocol?: ConsoleProtocol;
  onClose: () => void;
}) {
  const { busiGroups } = useContext(CommonStateContext);
  const [group, setGroup] = useState(groups.length === 1 ? groups[0] : undefined);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  const [connection, setConnection] = useState<Connection>();
  useEffect(() => {
    let active = true;
    api<Connection>(connectionPath(ident))
      .then((v) => {
        if (active) {
          setConnection(v);
          if (v.configured && groups.includes(v.group_id)) setGroup(v.group_id);
        }
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [ident, revision]);
  const ssh = useRef<SSHSettingsHandle>(null);
  return (
    <Modal
      visible
      destroyOnClose
      width={720}
      title={`编辑设备控制台 · ${ident}`}
      onCancel={onClose}
      confirmLoading={busy}
      okText='保存连接'
      onOk={async () => {
        setBusy(true);
        setError('');
        try {
          const v = await ssh.current?.value(true);
          if (v) await api(connectionPath(ident), RequestMethod.Put, v);
          onClose();
        } catch (e: any) {
          if (!(e instanceof HostKeyConfirmationCancelled)) setError(e.message || '保存控制台配置失败');
        } finally {
          setBusy(false);
        }
      }}
    >
      {error && <Alert className='mb-3' type='error' message={error} />}
      <Form layout='vertical'>
        <Form.Item label='连接所属业务组' required>
          <Select value={group} onChange={setGroup} options={busiGroups.filter((b) => groups.includes(b.id)).map((b) => ({ value: b.id, label: b.name }))} />
        </Form.Item>
      </Form>
      <SSHSettings key={revision} ref={ssh} ident={ident} host={host} group={group} defaultProtocol={defaultProtocol} />
      {connection?.configured && (
        <Popconfirm
          title='移除此设备的控制台连接？'
          onConfirm={async () => {
            try {
              await api(connectionPath(ident), RequestMethod.Delete, { record_version: connection.record_version });
              setRevision((v) => v + 1);
            } catch (e: any) {
              setError(e.message || '移除失败');
            }
          }}
        >
          <Button danger>移除控制台连接</Button>
        </Popconfirm>
      )}
    </Modal>
  );
}
