import React from 'react';
import { Alert, Descriptions, Modal, Typography } from 'antd';
import { RequestMethod } from '@/store/common';
import { api, Connection, Endpoint } from './api';

export class HostKeyConfirmationCancelled extends Error {}
interface HostKey {
  host: string;
  port: number;
  algorithm: string;
  fingerprint: string;
}
function trusted(endpoint: Endpoint, previous?: Endpoint) {
  return previous?.host === endpoint.host && previous.port === endpoint.port ? previous.fingerprint : undefined;
}
function confirm(key: HostKey, kind: string, previous: string | undefined, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new HostKeyConfirmationCancelled());
      return;
    }
    let dialog: ReturnType<typeof Modal.confirm>;
    const finish = (accepted: boolean) => {
      signal.removeEventListener('abort', abort);
      if (accepted) resolve();
      else reject(new HostKeyConfirmationCancelled());
    };
    const abort = () => {
      dialog.destroy();
      finish(false);
    };
    dialog = Modal.confirm({
      title: previous ? `${kind}指纹已变化` : `确认${kind}指纹`,
      width: 620,
      okText: previous ? '接受新指纹并继续' : '接受指纹并继续',
      cancelText: '取消保存',
      okButtonProps: { danger: !!previous },
      content: (
        <>
          <Alert
            className='mb-3'
            type={previous ? 'warning' : 'info'}
            message={previous ? '当前指纹与已信任记录不同，请确认设备身份后再接受。' : '首次连接此地址，请确认这是要连接的设备。接受后信任本次操作；保存连接后会固定校验此指纹。'}
          />
          <Descriptions column={1} size='small'>
            <Descriptions.Item label='设备地址'>
              {key.host}:{key.port}
            </Descriptions.Item>
            <Descriptions.Item label='密钥类型'>{key.algorithm}</Descriptions.Item>
            {previous && (
              <Descriptions.Item label='原指纹'>
                <Typography.Text code style={{ wordBreak: 'break-all' }}>
                  {previous}
                </Typography.Text>
              </Descriptions.Item>
            )}
            <Descriptions.Item label='当前指纹'>
              <Typography.Text code style={{ wordBreak: 'break-all' }}>
                {key.fingerprint}
              </Typography.Text>
            </Descriptions.Item>
          </Descriptions>
        </>
      ),
      onOk: () => {
        finish(true);
      },
      onCancel: () => {
        finish(false);
      },
    });
    signal.addEventListener('abort', abort, { once: true });
  });
}

// A jump is approved before the server is allowed to authenticate to it.
// No confirmation or discovered pin is persisted until the caller saves.
export async function confirmHostKeys(connection: Connection, ident: string | undefined, previous: Connection | undefined, signal: AbortSignal): Promise<Connection> {
  const f: Connection = { ...connection, target: { ...connection.target }, ...(connection.jump ? { jump: { ...connection.jump } } : {}) };
  for (const endpoint of [...(f.jump ? ['jump' as const] : []), 'target' as const]) {
    if (signal.aborted) throw new HostKeyConfirmationCancelled();
    const key = await api<HostKey>('host-key', RequestMethod.Post, { ident: ident || '', endpoint, connection: f });
    if (signal.aborted) throw new HostKeyConfirmationCancelled();
    const selected = f[endpoint]!;
    if (key.host !== selected.host || key.port !== selected.port || !/^SHA256:[A-Za-z0-9+/]{43}$/.test(key.fingerprint)) throw new Error('探测到的 SSH 指纹信息无效，请重试');
    const pinned = trusted(selected, previous?.[endpoint]);
    if (pinned !== key.fingerprint) await confirm(key, endpoint === 'jump' ? '堡垒机' : '目标设备', pinned, signal);
    selected.fingerprint = key.fingerprint;
  }
  return f;
}
