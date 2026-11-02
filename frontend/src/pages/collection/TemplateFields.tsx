import React from 'react';
import { Form, Input, InputNumber, Select, Switch } from 'antd';
import type { FormInstance } from 'antd';
import { activeFields, DeviceField } from '@/pages/builtInComponents/catalog';
import type { Credential } from '@/services/collection';

export const fieldPath = (field: DeviceField): (string | number)[] =>
  ['address', 'port', 'protocol', 'credential_id'].includes(field.key) ? [field.key] : ['parameters', field.key];
export default function TemplateFields({
  form,
  fields,
  credentials,
  group,
  savedSecrets = {},
  disabled = false,
}: {
  form: FormInstance;
  fields: DeviceField[];
  credentials: Credential[];
  group: number;
  savedSecrets?: Record<string, boolean>;
  disabled?: boolean;
}) {
  const params = Form.useWatch('parameters', form);
  const protocol = Form.useWatch('protocol', form);
  const values = { ...params, protocol };
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '0 18px' }}>
      {activeFields(fields, values).map((field) => {
        const stored = savedSecrets[field.key] && (field.secret || field.type === 'password');
        const options = (field.options || []).map((value) => ({ value, label: value === 'snmp_v2c' ? 'SNMP v2c' : value }));
        let control: React.ReactNode;
        switch (field.type) {
          case 'number':
            control = <InputNumber disabled={disabled} min={field.min} max={field.max} style={{ width: '100%' }} />;
            break;
          case 'switch':
            control = <Switch disabled={disabled} />;
            break;
          case 'select':
          case 'multiselect':
            control = <Select disabled={disabled} mode={field.type === 'multiselect' ? 'multiple' : undefined} options={options} />;
            break;
          case 'credential':
            control = <Select disabled={disabled} options={credentials.filter((c) => c.group_id === group && c.kind === 'snmp_v2c').map((c) => ({ value: c.id, label: c.name }))} />;
            break;
          case 'password':
            control = <Input.Password disabled={disabled} autoComplete='new-password' placeholder={stored ? '已设置；留空保留' : undefined} />;
            break;
          case 'endpoints':
            control = <Select disabled={disabled} mode='tags' tokenSeparators={[',']} />;
            break;
          default:
            control = <Input disabled={disabled} autoComplete='off' />;
        }
        return (
          <Form.Item
            key={field.key}
            name={fieldPath(field)}
            label={field.label}
            valuePropName={field.type === 'switch' ? 'checked' : 'value'}
            rules={[{ required: field.required && !stored, message: `请填写${field.label}` }, ...(field.type === 'url' ? [{ type: 'url' as const }] : [])]}
            extra={field.help}
          >
            {control}
          </Form.Item>
        );
      })}
    </div>
  );
}
