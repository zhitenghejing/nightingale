import React, { useEffect, useState } from 'react';
import { Alert, Modal, Radio, Spin } from 'antd';
import { api, Connection, connectionPath, ConsoleProtocol, Features } from './api';
import RDPModal from './RDPModal';
import TerminalModal from './TerminalModal';

interface Props {
  ident: string;
  name: string;
  features: Features;
  onClose: () => void;
}

export default function DeviceConsole({ ident, name, features, onClose }: Props) {
  const [connection, setConnection] = useState<Connection>();
  const [protocol, setProtocol] = useState<ConsoleProtocol>();
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    api<Connection>(connectionPath(ident))
      .then((saved) => {
        if (!active) return;
        setConnection(saved);
        if (saved.configured && saved.enabled && !saved.stale) setProtocol(saved.protocol || 'ssh');
      })
      .catch((e: Error) => {
        if (active) setError(e.message || '无法读取设备控制台配置');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [ident]);

  if (connected) return protocol === 'rdp' ? <RDPModal ident={ident} name={name} onClose={onClose} /> : <TerminalModal ident={ident} name={name} onClose={onClose} />;
  const configured = connection?.configured && connection.enabled && !connection.stale;
  const available = protocol === 'rdp' ? features.rdp : features.terminal;
  return (
    <Modal
      visible
      title={`控制台 · ${name}`}
      okText='连接'
      cancelText='取消'
      okButtonProps={{ disabled: !configured || !protocol || !available || loading }}
      onOk={() => setConnected(true)}
      onCancel={onClose}
    >
      {loading ? (
        <Spin />
      ) : error ? (
        <Alert type='error' showIcon message={error} />
      ) : !configured ? (
        <Alert type='info' showIcon message='请先在设备编辑表单里填写账号密码并启用控制台连接。' />
      ) : (
        <>
          <Radio.Group value={protocol} onChange={(event) => setProtocol(event.target.value)} aria-label='连接方式'>
            <Radio value='ssh' disabled={connection?.protocol === 'rdp' || !features.terminal}>
              SSH
            </Radio>
            <Radio value='rdp' disabled={connection?.protocol !== 'rdp' || !features.rdp}>
              RDP
            </Radio>
          </Radio.Group>
          {!available && <Alert className='mt-3' type='warning' showIcon message={protocol === 'rdp' ? 'RDP 网关尚未启用' : 'SSH 控制台尚未启用'} />}
        </>
      )}
    </Modal>
  );
}
