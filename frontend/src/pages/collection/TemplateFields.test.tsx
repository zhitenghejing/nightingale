/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Form } from 'antd';
import TemplateFields from './TemplateFields';
import { activeFields, connectionFields } from '@/pages/builtInComponents/catalog';
jest.mock('@/services/collection', () => ({ collectionRequest: jest.fn() }));

function FormFixture({ adapter }: { adapter: 'mysql' | 'redfish' }) {
  const [form] = Form.useForm();
  return (
    <Form form={form} initialValues={{ parameters: { tls: 'verify' } }}>
      <TemplateFields form={form} fields={connectionFields(adapter)} credentials={[]} group={1} />
      <button type='button' onClick={() => form.setFieldsValue({ parameters: { tls: 'ca' } })}>
        指定 CA
      </button>
      <button type='button' onClick={() => form.setFieldsValue({ parameters: { tls: 'verify' } })}>
        默认校验
      </button>
    </Form>
  );
}

test('database controls are generated from the template, including username and a masked password', () => {
  render(<FormFixture adapter='mysql' />);
  expect(screen.getByLabelText('监控账号')).toBeTruthy();
  expect(screen.getByLabelText('监控密码').getAttribute('type')).toBe('password');
  expect(screen.queryByLabelText('团体字')).toBeNull();
});
test('conditional hardware fields appear only for their selected TLS mode', async () => {
  render(<FormFixture adapter='redfish' />);
  expect(screen.queryByLabelText('CA 文件路径')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '指定 CA' }));
  await waitFor(() => expect(screen.getByLabelText('CA 文件路径')).toBeTruthy());
  fireEvent.click(screen.getByRole('button', { name: '默认校验' }));
  await waitFor(() => expect(screen.queryByLabelText('CA 文件路径')).toBeNull());
  expect(activeFields(connectionFields('redfish'), { tls: 'verify', ca_file: 'stale-path' }).some((f) => f.key === 'ca_file')).toBe(false);
});
