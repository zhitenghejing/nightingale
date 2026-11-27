import React, { useEffect, useRef, useState } from 'react';
import { Alert, Button, Modal, Space, Tag } from 'antd';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import { useTranslation } from 'react-i18next';
import { basePrefix } from '@/App';
import { NS } from '@/pages/hosts/constants';
import { RequestMethod } from '@/store/common';
import { api } from './api';
import '@xterm/xterm/css/xterm.css';
import './terminal.less';

type Status = 'connecting' | 'connected' | 'disconnected' | 'failed';

export default function TerminalModal({ ident, name, onClose }: { ident: string; name: string; onClose: () => void }) {
  const { t } = useTranslation(NS);
  const container = useRef<HTMLDivElement>(null);
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<Status>('connecting');
  const [detail, setDetail] = useState('');
  const [inputLimited, setInputLimited] = useState(false);

  useEffect(() => {
    const node = container.current;
    if (!node) return;

    let active = true;
    let ready = false;
    let socket: WebSocket | undefined;
    setStatus('connecting');
    setDetail('');
    setInputLimited(false);

    const term = new Terminal({
      cursorBlink: true,
      disableStdin: true,
      scrollback: 2000,
      fontSize: 14,
      fontFamily: 'ui-monospace, SFMono-Regular, Consolas, monospace',
      theme: { background: '#111827', foreground: '#e5e7eb', cursor: '#e5e7eb' },
      linkHandler: { activate: () => {} },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(node);
    const observer = new ResizeObserver(() => {
      const size = fit.proposeDimensions();
      if (size) term.resize(Math.min(500, Math.max(2, size.cols)), Math.min(250, Math.max(1, size.rows)));
    });
    observer.observe(node);

    const sendResize = () => {
      if (ready && socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
      }
    };
    const resized = term.onResize(sendResize);
    const encoder = new TextEncoder();
    const sendInput = (bytes: Uint8Array) => {
      if (!ready || socket?.readyState !== WebSocket.OPEN) return;
      if (socket.bufferedAmount + bytes.byteLength > 256 * 1024) {
        setInputLimited(true);
        return;
      }
      setInputLimited(false);
      for (let offset = 0; offset < bytes.byteLength; offset += 8192) {
        socket.send(bytes.subarray(offset, offset + 8192));
      }
    };
    const data = term.onData((input) => sendInput(encoder.encode(input)));
    const binary = term.onBinary((input) => sendInput(Uint8Array.from(input, (char) => char.charCodeAt(0))));

    api<{ ticket: string }>('terminal/ticket', RequestMethod.Post, { ident })
      .then(({ ticket }) => {
        if (!active) return;
        const url = new URL(`${basePrefix}/api/n9e/remote-access/terminal/socket`, window.location.origin);
        url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
        socket = new WebSocket(url, ['n9e-terminal-v1', `ticket.${ticket}`]);
        socket.binaryType = 'arraybuffer';
        socket.onmessage = (event: MessageEvent) => {
          if (!active) return;
          if (event.data instanceof ArrayBuffer) {
            term.write(new Uint8Array(event.data));
            return;
          }
          const update = JSON.parse(event.data) as { type: string; message: string };
          if (update.type === 'ready') {
            ready = true;
            term.options.disableStdin = false;
            setStatus('connected');
            sendResize();
            term.focus();
          } else if (update.type === 'error' || update.type === 'closed') {
            setStatus(update.type === 'error' ? 'failed' : 'disconnected');
            setDetail(update.message);
          }
        };
        socket.onclose = () => {
          if (!active) return;
          ready = false;
          term.options.disableStdin = true;
          setStatus((previous) => (previous === 'connecting' ? 'failed' : previous === 'connected' ? 'disconnected' : previous));
        };
      })
      .catch((error: Error) => {
        if (active) {
          setDetail(error.message || '');
          setStatus('failed');
        }
      });

    return () => {
      active = false;
      observer.disconnect();
      if (socket?.readyState === WebSocket.CONNECTING || socket?.readyState === WebSocket.OPEN) socket.close();
      resized.dispose();
      data.dispose();
      binary.dispose();
      term.dispose();
    };
  }, [attempt, ident]);

  const message = detail === 'SSH connection or credential not found' ? t('terminal.connection_required') : detail;
  return (
    <Modal
      visible
      keyboard={false}
      maskClosable={false}
      width={1040}
      title={`${t('terminal.console')} · ${name} · SSH`}
      onCancel={onClose}
      footer={
        <Space>
          {(status === 'failed' || status === 'disconnected') && <Button onClick={() => setAttempt((value) => value + 1)}>{t('terminal.reconnect')}</Button>}
          <Button onClick={onClose}>{t('terminal.close')}</Button>
        </Space>
      }
    >
      <div className='ssh-terminal-status' role='status'>
        <Tag color={status === 'connected' ? 'green' : status === 'failed' ? 'red' : 'default'}>{t(`terminal.${status}`)}</Tag>
        <span>{message || t('terminal.account')}</span>
      </div>
      {inputLimited && <Alert type='warning' showIcon message={t('terminal.input_limit')} />}
      <div className='ssh-terminal-frame'>
        <div ref={container} className='ssh-terminal-screen' />
      </div>
    </Modal>
  );
}
