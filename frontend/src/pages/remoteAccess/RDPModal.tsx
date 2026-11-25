import React, { useEffect, useRef, useState } from 'react';
import { Button, Modal, Space, Tag } from 'antd';
import { basePrefix } from '@/App';
import { RequestMethod } from '@/store/common';
import { api } from './api';
import './terminal.less';

// The Apache browser library is loaded only when opening a graphical console.
type Guacamole = Record<string, any>;
const getGuacamole = () => (window as Window & { Guacamole?: Guacamole }).Guacamole;
let library: Promise<Guacamole> | undefined;
function loadGuacamole() {
  if (!library) {
    library = new Promise<Guacamole>((resolve, reject) => {
      const script = document.createElement('script');
      script.src = `${basePrefix}/vendor/guacamole-1.6.0/all.min.js`;
      const fail = () => {
        script.remove();
        reject(new Error('无法加载 RDP 控制台组件'));
      };
      script.onload = () => {
        const client = getGuacamole();
        if (client?.Client && client.Parser) resolve(client);
        else fail();
      };
      script.onerror = fail;
      document.head.appendChild(script);
    }).catch((error) => {
      library = undefined;
      throw error;
    });
  }
  return library;
}

export default function RDPModal({ ident, name, onClose }: { ident: string; name: string; onClose: () => void }) {
  const viewport = useRef<HTMLDivElement>(null);
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<'connecting' | 'connected' | 'closed' | 'error'>('connecting');
  const [detail, setDetail] = useState('');
  useEffect(() => {
    const node = viewport.current;
    if (!node) return;
    let active = true;
    let socket: WebSocket | undefined;
    let dispose: (() => void) | undefined;
    setStatus('connecting');
    setDetail('');
    Promise.all([loadGuacamole(), api<{ ticket: string }>('terminal/ticket', RequestMethod.Post, { ident, protocol: 'rdp' })])
      .then(([guac, { ticket }]) => {
        if (!active) return;
        const tunnel = new guac.Tunnel();
        const parser = new guac.Parser();
        const decoder = new TextDecoder();
        parser.oninstruction = (opcode: string, elements: string[]) => tunnel.oninstruction?.(opcode, elements);
        tunnel.connect = () => {
          const url = new URL(`${basePrefix}/api/n9e/remote-access/terminal/socket`, window.location.origin);
          url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
          tunnel.setState(guac.Tunnel.State.CONNECTING);
          socket = new WebSocket(url, ['n9e-terminal-v1', `ticket.${ticket}`]);
          socket.binaryType = 'arraybuffer';
          socket.onmessage = (event) => {
            if (!active) return;
            if (event.data instanceof ArrayBuffer) {
              parser.receive(decoder.decode(event.data, { stream: true }));
            } else {
              const update = JSON.parse(event.data) as { type: string; message: string };
              if (update.type === 'ready') tunnel.setState(guac.Tunnel.State.OPEN);
              if (update.type === 'error' || update.type === 'closed') {
                setStatus(update.type);
                setDetail(update.message);
              }
            }
          };
          socket.onclose = () => {
            if (!active) return;
            tunnel.setState(guac.Tunnel.State.CLOSED);
            setStatus((state) => (state === 'connecting' ? 'error' : state === 'connected' ? 'closed' : state));
          };
        };
        tunnel.disconnect = () => {
          if (socket?.readyState === WebSocket.CONNECTING || socket?.readyState === WebSocket.OPEN) socket.close();
        };
        tunnel.sendMessage = (...elements: (string | number)[]) => {
          if (!tunnel.isConnected() || socket?.readyState !== WebSocket.OPEN || socket.bufferedAmount > 256 * 1024) return;
          const instruction = elements
            .map(String)
            .map((element) => `${Array.from(element).length}.${element}`)
            .join(',');
          socket.send(`${instruction};`);
        };
        const client = new guac.Client(tunnel);
        const display = client.getDisplay();
        const screen = display.getElement();
        node.appendChild(screen);
        const fit = () => {
          if (display.getWidth() > 0 && display.getHeight() > 0) display.scale(Math.min(1, node.clientWidth / display.getWidth(), node.clientHeight / display.getHeight()));
        };
        const observer = new ResizeObserver(fit);
        observer.observe(node);
        display.onresize = fit;
        const mouse = new guac.Mouse(screen);
        mouse.onEach(['mousedown', 'mouseup', 'mousemove'], (event: { state: unknown }) => client.sendMouseState(event.state, true));
        screen.tabIndex = 0;
        screen.setAttribute('aria-label', 'RDP 远程桌面');
        const focus = () => screen.focus();
        screen.addEventListener('pointerdown', focus);
        const keyboard = new guac.Keyboard(screen);
        keyboard.onkeydown = (keysym: number) => {
          client.sendKeyEvent(1, keysym);
          return false;
        };
        keyboard.onkeyup = (keysym: number) => client.sendKeyEvent(0, keysym);
        const releaseKeys = () => keyboard.reset();
        screen.addEventListener('blur', releaseKeys);
        client.onstatechange = (state: number) => {
          if (active && state === guac.Client.State.CONNECTED) {
            setStatus('connected');
            focus();
          }
        };
        client.onerror = () => {
          if (active) {
            setStatus('error');
            setDetail('RDP 登录或远程桌面连接失败');
          }
        };
        dispose = () => {
          releaseKeys();
          observer.disconnect();
          client.disconnect();
          screen.remove();
        };
        client.connect();
      })
      .catch((e: Error) => {
        if (active) {
          setStatus('error');
          setDetail(e.message || '无法打开 RDP 控制台');
        }
      });
    return () => {
      active = false;
      dispose?.();
    };
  }, [attempt, ident]);

  return (
    <Modal
      visible
      keyboard={false}
      maskClosable={false}
      width={1140}
      title={`控制台 · ${name} · RDP`}
      onCancel={onClose}
      footer={
        <Space>
          {(status === 'error' || status === 'closed') && <Button onClick={() => setAttempt((value) => value + 1)}>重新连接</Button>}
          <Button onClick={onClose}>关闭控制台</Button>
        </Space>
      }
    >
      <div className='ssh-terminal-status' role='status'>
        <Tag color={status === 'connected' ? 'green' : status === 'error' ? 'red' : 'default'}>
          {{ connecting: '正在连接', connected: '已连接', error: '连接失败', closed: '连接已断开' }[status]}
        </Tag>
        {detail}
      </div>
      <div className='ssh-terminal-frame'>
        <div ref={viewport} className='rdp-console-screen' />
      </div>
    </Modal>
  );
}
