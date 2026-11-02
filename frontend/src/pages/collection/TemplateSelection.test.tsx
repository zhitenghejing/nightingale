/** @jest-environment jsdom */
import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Form, Input } from 'antd';
import TemplateSelection from './TemplateSelection';
import { Catalog } from '@/pages/builtInComponents/catalog';
import type { Device } from '@/services/collection';
jest.mock('@/services/collection', () => ({ collectionRequest: jest.fn() }));
const catalog: Catalog = {
  categories: [
    { id: 'db', parent_id: '', name: '数据库', record_version: 1 },
    { id: 'mysql', parent_id: 'db', name: 'MySQL', record_version: 1 },
    { id: 'mysql84', parent_id: 'mysql', name: '8.4 LTS', record_version: 1 },
  ],
  entries: [
    {
      component_id: 1,
      ident: 'MySQL',
      name: 'MySQL',
      logo: '',
      category_id: 'mysql84',
      disabled: false,
      record_version: 1,
      draft: { adapter: 'mysql', fields: [], config: '', notification: 'events_only' },
      published_version_id: 'release',
      published: { version: 1, category_path: ['数据库', 'MySQL', '8.4 LTS'], definition: { adapter: 'mysql', fields: [], config: '', notification: 'events_only' } },
    },
  ],
};
function Fixture({ data = catalog }: { data?: Catalog }) {
  const [form] = Form.useForm();
  return (
    <Form form={form} initialValues={{ name: 'keep-common-name', parameters: { community: 'old-type-secret' } }}>
      <TemplateSelection catalog={data} form={form} onSelection={() => {}} />
      <Form.Item name='name' label='对象名称'>
        <Input />
      </Form.Item>
      <Form.Item name={['parameters', 'community']} label='旧类型密码'>
        <Input.Password />
      </Form.Item>
    </Form>
  );
}
test('switching the upstream device category removes nested secrets and keeps common fields', async () => {
  render(<Fixture />);
  const selector = screen.getByRole('combobox', { name: '设备类型' }).closest('.ant-select')!.querySelector('.ant-select-selector')!;
  fireEvent.mouseDown(selector);
  fireEvent.click(await screen.findByText('数据库', { selector: '.ant-select-item-option-content' }));
  await waitFor(() => expect((screen.getByLabelText('旧类型密码') as HTMLInputElement).value).toBe(''));
  expect((screen.getByLabelText('对象名称') as HTMLInputElement).value).toBe('keep-common-name');
});

test('existing unpublished templates remain selectable through all three category levels', async () => {
  const existing: Catalog = {
    categories: [
      ...catalog.categories,
      { id: 'cache', parent_id: '', name: '缓存、文档与搜索', record_version: 1 },
      { id: 'redis', parent_id: 'cache', name: 'Redis', record_version: 1 },
      { id: 'redis-general', parent_id: 'redis', name: '待核验版本', record_version: 1 },
    ],
    entries: [
      ...catalog.entries,
      { ...catalog.entries[0], component_id: 2, ident: 'Redis', name: 'Redis', category_id: 'redis-general', published: null, published_version_id: '', draft: { adapter: 'redis', fields: [], config: '', notification: 'events_only' } },
    ],
  };
  render(<Fixture data={existing} />);
  for (const [label, option] of [['设备类型', '缓存、文档与搜索'], ['品牌', 'Redis'], ['设备型号', '待核验版本']]) {
    fireEvent.mouseDown(screen.getByRole('combobox', { name: label }).closest('.ant-select')!.querySelector('.ant-select-selector')!);
    fireEvent.click(await screen.findByText(option, { selector: '.ant-select-item-option-content' }));
  }
  expect(await screen.findByText(/尚未发布/, { selector: '.ant-alert-message' })).toBeTruthy();
});

test('legacy device snapshots remain selected when their component is absent from the catalog', async () => {
  const device = { id: 'old-device', template_version_id: 'old-release', profile: { name: '原设备快照' } } as Device;
  let actualForm: ReturnType<typeof Form.useForm>[0];
  function LegacyFixture() {
    const [form] = Form.useForm(); actualForm = form;
    return <Form form={form} initialValues={{ template_version_id: device.template_version_id }}><TemplateSelection catalog={catalog} form={form} device={device} onSelection={() => {}} /></Form>;
  }
  render(<LegacyFixture />);
  await waitFor(() => expect(actualForm.getFieldValue('template_selection')).toBe('snapshot:old-release'));
  expect(await screen.findByText('原设备快照（设备当前快照）', { selector: '.ant-select-selection-item' })).toBeTruthy();
  await expect(actualForm!.validateFields()).resolves.toMatchObject({ template_version_id: 'old-release', template_selection: 'snapshot:old-release' });
});
