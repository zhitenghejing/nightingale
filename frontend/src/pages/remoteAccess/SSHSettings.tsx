import React, { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
import { Alert, Button, Checkbox, Collapse, Form, Input, InputNumber, Select, Space, Switch } from 'antd';
import { RequestMethod } from '@/store/common';
import { api, Connection, connectionPath, ConsoleProtocol, Endpoint, Features } from './api';
import { confirmHostKeys, HostKeyConfirmationCancelled } from './hostKeys';

export interface SSHSettingsHandle {
  value: (confirmKeys?: boolean) => Promise<Connection | undefined>;
}
interface Props {
  ident?: string;
  group?: number;
  host?: string;
  defaultProtocol?: ConsoleProtocol;
}
interface EndpointFields {
  host?: string;
  port?: number;
  username?: string;
  password?: string;
}

const defaultPort = (protocol: ConsoleProtocol) => (protocol === 'rdp' ? 3389 : 22);

export function endpointInput(values: EndpointFields, group: number, saved?: Endpoint): Endpoint {
  const { host, port, username, password } = values;
  if (!host?.trim() || !port || !Number.isInteger(port) || port < 1 || port > 65535) throw new Error('请填写连接地址和有效端口');
  const matches = saved && host.trim() === saved.host && port === saved.port;
  const endpoint: Endpoint = { host: host.trim(), port, fingerprint: matches ? saved.fingerprint : '', credential_id: '' };
  if (username || password || !matches || !saved?.credential_id) {
    if (!username?.trim() || !password) throw new Error('请手工填写此连接的账号和密码');
    endpoint.new_credential = { name: '', group_id: group, username: username.trim(), password };
  } else {
    endpoint.credential_id = saved.credential_id;
  }
  return endpoint;
}

export default forwardRef<SSHSettingsHandle, Props>(function SSHSettings({ ident, group, host, defaultProtocol = 'ssh' }, ref) {
  const [form] = Form.useForm();
  const [features, setFeatures] = useState<Features>();
  const [existing, setExisting] = useState<Connection>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string }>();
  const acceptedPins = useRef<Connection>();
  const editGeneration = useRef(0);
  const dirty = useRef(false);
  const lifetime = useRef<AbortController>();
  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    return () => controller.abort();
  }, []);
  const previousGroup = useRef(group);
  const previousHost = useRef(host);
  const enabled = Form.useWatch('enabled', form);
  const jump = Form.useWatch('via_jump', form);
  const protocol = (Form.useWatch('protocol', form) || defaultProtocol) as ConsoleProtocol;
  useEffect(() => {
    let active = true;
    setLoading(true);
    dirty.current = false;
    setError('');
    setExisting(undefined);
    acceptedPins.current = undefined;
    setTestResult(undefined);
    form.resetFields();
    api<Features>('features')
      .then(async (f) => {
        if (!active) return;
        setFeatures(f);
        if (!f.enabled || !ident) return;
        const c = await api<Connection>(connectionPath(ident));
        if (active && c.stale) setError('设备归属或标识已变化，请先移除旧连接。');
        if (active && c.configured) {
          setExisting(c);
          form.setFieldsValue({
            enabled: c.enabled,
            protocol: c.protocol || 'ssh',
            via_jump: !!c.jump,
            log_paths_text: (c.log_paths || []).join('\n'),
            target: { host: c.target.host, port: c.target.port },
            jump: c.jump ? { host: c.jump.host, port: c.jump.port } : undefined,
          });
        }
      })
      .catch((e) => {
        if (active) setError(e.message || '无法读取设备控制台配置');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [ident]);
  useEffect(() => {
    if (previousGroup.current && previousGroup.current !== group) {
      form.setFieldsValue({ target: { username: undefined, password: undefined }, jump: { username: undefined, password: undefined } });
      if (features?.enabled && features.configure && (existing?.configured || form.getFieldValue('enabled'))) dirty.current = true;
      editGeneration.current++;
      setTestResult(undefined);
    }
    previousGroup.current = group;
  }, [group, features?.enabled]);
  useEffect(() => {
    if (!existing?.configured && !dirty.current) {
      form.setFieldsValue({ protocol: defaultProtocol, target: { port: defaultPort(defaultProtocol) } });
    }
  }, [defaultProtocol, existing?.configured, loading]);
  useEffect(() => {
    const current = form.getFieldValue(['target', 'host']);
    if (host && (!current || (!existing?.configured && current === previousHost.current))) {
      form.setFieldsValue({ target: { host } });
    }
    previousHost.current = host;
  }, [host, existing?.configured, loading]);

  const readConnection = async (confirmKeys = false, test = false): Promise<Connection | undefined> => {
    if (!dirty.current && !test) return undefined;
    if (loading || error) throw new Error(error || '控制台配置尚未加载完成');
    if (!features?.enabled || !features.configure) throw new Error('当前账号没有设备控制台配置权限');
    const v = form.getFieldsValue(true);
    if (!v.enabled) return existing?.configured && !test ? { ...existing, enabled: false } : undefined;
    if (!group) throw new Error('请先选择业务组');
    const sameBinding = existing && existing.group_id === group && (existing.protocol || 'ssh') === v.protocol;
    const connection: Connection = {
      protocol: v.protocol,
      enabled: true,
      group_id: group,
      target: endpointInput(v.target || {}, group, sameBinding ? existing?.target : undefined),
      ...(v.protocol === 'ssh' && v.via_jump ? { jump: endpointInput(v.jump || {}, group, sameBinding ? existing?.jump : undefined) } : {}),
      log_paths:
        v.protocol === 'ssh'
          ? (v.log_paths_text || '')
              .split('\n')
              .map((p: string) => p.trim())
              .filter(Boolean)
          : [],
      record_version: existing?.record_version || 0,
    };
    if (!confirmKeys || connection.protocol === 'rdp') return connection;

    const confirmed = await confirmHostKeys(connection, ident, acceptedPins.current || existing, lifetime.current!.signal);
    const pin = (e: Endpoint): Endpoint => ({ host: e.host, port: e.port, fingerprint: e.fingerprint, credential_id: '' });
    acceptedPins.current = { ...confirmed, target: pin(confirmed.target), ...(confirmed.jump ? { jump: pin(confirmed.jump) } : {}) };
    return confirmed;
  };
  useImperativeHandle(ref, () => ({ value: readConnection }));
  const testConnection = async () => {
    setTesting(true);
    setTestResult(undefined);
    const generation = editGeneration.current;
    try {
      const connection = await readConnection(true, true);
      if (!connection?.enabled) throw new Error('请先启用并填写 SSH 连接');
      const result = await api<{ ok: boolean; via_jump: boolean; duration_ms: number }>('connection/test', RequestMethod.Post, { ident: ident || '', connection });
      if (!lifetime.current?.signal.aborted && generation === editGeneration.current)
        setTestResult({ ok: result.ok, message: `SSH 连接成功${result.via_jump ? '（通过堡垒机）' : ''}，耗时 ${result.duration_ms} 毫秒` });
    } catch (e: any) {
      if (!(e instanceof HostKeyConfirmationCancelled) && !lifetime.current?.signal.aborted && generation === editGeneration.current)
        setTestResult({ ok: false, message: `SSH 连接测试失败：${e.message || '请检查地址和凭据'}` });
    } finally {
      setTesting(false);
    }
  };
  if (loading) return <div className='mb-3'>正在加载控制台配置…</div>;
  if (!features?.enabled) return error ? <Alert className='mb-3' type='warning' message={error} /> : null;
  const disabled = !features.configure || !!error || testing;
  const fields = (name: 'target' | 'jump', title: string) => {
    const variant = name === 'jump' ? 'SSH' : protocol.toUpperCase();
    const saved = existing && existing.group_id === group && (existing.protocol || 'ssh') === protocol ? existing[name] : undefined;
    return (
      <div>
        <h4>{title}</h4>
        <Space align='start' wrap>
          <Form.Item name={[name, 'host']} label={`${variant} 地址（固定 IP）`} required>
            <Input disabled={disabled} placeholder={name === 'target' ? host : undefined} />
          </Form.Item>
          <Form.Item name={[name, 'port']} label='端口' required>
            <InputNumber disabled={disabled} min={1} max={65535} />
          </Form.Item>
        </Space>
        <Form.Item name={[name, 'username']} label={`${variant} 账号`}>
          <Input disabled={disabled || !features.manage_credentials} autoComplete='off' placeholder={saved ? '留空沿用此设备账号' : undefined} />
        </Form.Item>
        <Form.Item name={[name, 'password']} label={`${variant} 密码`}>
          <Input.Password disabled={disabled || !features.manage_credentials} autoComplete='new-password' placeholder={saved ? '同时填写账号和密码可更换' : undefined} />
        </Form.Item>
        {variant === 'SSH' && <div className='mb-3 text-soft'>保存 SSH 连接时确认主机指纹。</div>}
      </div>
    );
  };
  return (
    <Collapse className='mb-3'>
      <Collapse.Panel header='设备控制台（可选）' key='console'>
        {error && <Alert type='error' message={error} />}
        {!features.configure && <Alert type='info' message='当前账号没有控制台配置权限。' />}
        <Form
          component={false}
          form={form}
          layout='vertical'
          initialValues={{ enabled: false, protocol: defaultProtocol, target: { host, port: defaultPort(defaultProtocol) }, jump: { port: 22 } }}
          onValuesChange={(changes) => {
            dirty.current = true;
            editGeneration.current++;
            setTestResult(undefined);
            if (changes.protocol) {
              form.setFieldsValue({
                target: { port: defaultPort(changes.protocol), username: undefined, password: undefined },
                jump: { username: undefined, password: undefined },
              });
            }
          }}
        >
          <Form.Item name='enabled' label='启用控制台连接' valuePropName='checked'>
            <Switch disabled={disabled || !group} />
          </Form.Item>
          {enabled && (
            <>
              <Form.Item name='protocol' label='连接方式' required>
                <Select
                  disabled={disabled}
                  options={[
                    { value: 'ssh', label: 'SSH' },
                    { value: 'rdp', label: 'RDP' },
                  ]}
                />
              </Form.Item>
              {fields('target', '目标设备')}
              {protocol === 'ssh' && (
                <>
                  <Form.Item name='via_jump' valuePropName='checked'>
                    <Checkbox disabled={disabled}>通过堡垒机 / 跳板机连接</Checkbox>
                  </Form.Item>
                  {jump && fields('jump', '堡垒机')}
                  <Form.Item name='log_paths_text' label='允许读取的日志文件（每行一个绝对路径）'>
                    <Input.TextArea disabled={disabled} rows={3} placeholder='/var/log/nginx/error.log' />
                  </Form.Item>
                  <Button className='mb-3' loading={testing} disabled={disabled || !group} onClick={testConnection}>
                    测试 SSH 连接
                  </Button>
                  {testResult && <Alert type={testResult.ok ? 'success' : 'error'} showIcon message={testResult.message} />}
                </>
              )}
            </>
          )}
        </Form>
      </Collapse.Panel>
    </Collapse>
  );
});
