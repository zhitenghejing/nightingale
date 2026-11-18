import React from 'react';
import { Button, Card, Form, Input, InputNumber, Select, Space, Switch } from 'antd';
import { FormInstance } from 'antd/lib/form';
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons';
import { key } from './api';

// Both knowledge and case plans use the same structured procedure editor.
export default function ProcedureFields({ form, demoEnabled = false }: { form: FormInstance; demoEnabled?: boolean }) {
  return (
    <>
      <Card size='small' title='适用环境' style={{ marginBottom: 16 }}>
        <Form.List name='conditions_rows'>
          {(fields, { add, remove }) => (
            <>
              {fields.map((field) => (
                <Space key={field.key} align='baseline'>
                  <Form.Item name={[field.name, 'name']} rules={[{ required: true }]}>
                    <Input placeholder='条件，如 system_version' />
                  </Form.Item>
                  <Form.Item name={[field.name, 'value']} rules={[{ required: true }]}>
                    <Input placeholder='需要的值' />
                  </Form.Item>
                  <Button icon={<MinusCircleOutlined />} onClick={() => remove(field.name)} />
                </Space>
              ))}
              <Button onClick={() => add({ name: '', value: '' })}>增加条件</Button>
            </>
          )}
        </Form.List>
      </Card>
      <Card size='small' title='排查步骤' style={{ marginBottom: 16 }}>
        <Form.List name='steps'>
          {(fields, { add, remove }) => (
            <>
              {fields.map((field, index) => (
                <div className='ops-field-block' key={field.key}>
                  <Form.Item name={[field.name, 'key']} hidden>
                    <Input />
                  </Form.Item>
                  <Space>
                    <strong>步骤 {index + 1}</strong>
                    <Button size='small' onClick={() => remove(field.name)}>
                      移除
                    </Button>
                  </Space>
                  <Form.Item name={[field.name, 'title']} label='检查事项' rules={[{ required: true }]}>
                    <Input />
                  </Form.Item>
                  <Form.Item name={[field.name, 'kind']} label='检查方式'>
                    <Select
                      options={[
                        { value: 'manual', label: '人员检查' },
                        { value: 'metric', label: '指标查询' },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item name={[field.name, 'query']} label='指标查询表达式（指标检查时必填）'>
                    <Input.TextArea rows={2} />
                  </Form.Item>
                  <Form.Item name={[field.name, 'instruction']} label='如何检查和记录'>
                    <Input.TextArea rows={2} />
                  </Form.Item>
                  <Form.Item name={[field.name, 'expected']} label='预期现象与判断依据'>
                    <Input.TextArea rows={2} />
                  </Form.Item>
                  <Form.Item shouldUpdate>
                    {() => {
                      const options = (form.getFieldValue('steps') || []).map((s: { key: string; title: string }, i: number) => ({
                        value: s.key,
                        label: `${i + 1}. ${s.title || '未命名步骤'}`,
                      }));
                      return (
                        <Space>
                          <Form.Item name={[field.name, 'next']} label='正常时下一步'>
                            <Select allowClear style={{ width: 220 }} options={options} />
                          </Form.Item>
                          <Form.Item name={[field.name, 'on_failure']} label='异常时下一步'>
                            <Select allowClear style={{ width: 220 }} options={options} />
                          </Form.Item>
                        </Space>
                      );
                    }}
                  </Form.Item>
                </div>
              ))}
              <Button icon={<PlusOutlined />} onClick={() => add({ key: key(), title: '', kind: 'manual', query: '', instruction: '', expected: '' })}>
                增加排查步骤
              </Button>
            </>
          )}
        </Form.List>
      </Card>
      <Card size='small' title='允许的操作与风险' style={{ marginBottom: 16 }}>
        <Form.List name='actions'>
          {(fields, { add, remove }) => (
            <>
              {fields.map((field) => (
                <div className='ops-field-block' key={field.key}>
                  <Form.Item name={[field.name, 'key']} hidden>
                    <Input />
                  </Form.Item>
                  <Button size='small' onClick={() => remove(field.name)}>
                    移除操作
                  </Button>
                  <Form.Item name={[field.name, 'title']} label='操作名称' rules={[{ required: true }]}>
                    <Input />
                  </Form.Item>
                  <Form.Item shouldUpdate>
                    {() => (
                      <Form.Item name={[field.name, 'required_steps']} label='执行前必须完成的检查' extra='留空时要求全部步骤完成；分支操作请明确选择必需步骤。失败、跳过或过期证据不能作为执行依据。'>
                        <Select mode='multiple' allowClear options={(form.getFieldValue('steps') || []).map((s: { key: string; title: string }) => ({ value: s.key, label: s.title || s.key }))} />
                      </Form.Item>
                    )}
                  </Form.Item>
                  <Form.Item name={[field.name, 'kind']} label='执行方式'>
                    <Select
                      options={[
                        { value: 'manual', label: '人工执行并登记' },
                        { value: 'ibex_template', label: '固定作业模板（需启用）' },
                        ...(demoEnabled ? [{ value: 'demo_reclaim', label: '隔离演练：清理测试缓存' }] : []),
                      ]}
                    />
                  </Form.Item>
                  <Form.Item name={[field.name, 'template_id']} label='固定作业模板 ID（仅模板执行）'>
                    <InputNumber min={1} />
                  </Form.Item>
                  <Form.Item name={[field.name, 'instruction']} label='操作说明'>
                    <Input.TextArea />
                  </Form.Item>
                  <Form.Item name={[field.name, 'risk']} label='影响与停止条件' rules={[{ required: true }]}>
                    <Input.TextArea />
                  </Form.Item>
                  <Form.Item name={[field.name, 'rollback']} label='回退或升级方法' rules={[{ required: true }]}>
                    <Input.TextArea />
                  </Form.Item>
                </div>
              ))}
              <Button onClick={() => add({ key: key(), kind: 'manual', template_id: 0, title: '', instruction: '', risk: '', rollback: '' })}>增加受控操作</Button>
            </>
          )}
        </Form.List>
      </Card>
      <Card size='small' title='恢复验证标准' style={{ marginBottom: 16 }}>
        <Form.List name='checks'>
          {(fields, { add, remove }) => (
            <>
              {fields.map((field) => (
                <div className='ops-field-block' key={field.key}>
                  <Form.Item name={[field.name, 'key']} hidden>
                    <Input />
                  </Form.Item>
                  <Button size='small' onClick={() => remove(field.name)}>
                    移除检查
                  </Button>
                  <Form.Item name={[field.name, 'title']} label='验证事项与通过标准' rules={[{ required: true }]}>
                    <Input />
                  </Form.Item>
                  <Space align='start'>
                    <Form.Item name={[field.name, 'kind']} label='验证方式'>
                      <Select
                        style={{ width: 200 }}
                        options={[
                          { value: 'manual', label: '人员验证业务结果' },
                          { value: 'metric', label: '程序验证指标' },
                          ...(demoEnabled
                            ? [
                                { value: 'demo_business', label: '演练业务探测' },
                                { value: 'demo_data', label: '演练数据核对' },
                              ]
                            : []),
                        ]}
                      />
                    </Form.Item>
                    <Form.Item name={[field.name, 'required']} label='结束前必需通过' valuePropName='checked'>
                      <Switch />
                    </Form.Item>
                  </Space>
                  <Form.Item name={[field.name, 'query']} label='指标验证表达式'>
                    <Input.TextArea rows={2} />
                  </Form.Item>
                  <Space align='start'>
                    <Form.Item name={[field.name, 'operator']} label='比较'>
                      <Select style={{ width: 90 }} options={['<', '<=', '>', '>=', '==', '!='].map((v) => ({ value: v, label: v }))} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'threshold']} label='恢复阈值'>
                      <InputNumber />
                    </Form.Item>
                    <Form.Item name={[field.name, 'window_seconds']} label='观察窗口（秒）'>
                      <InputNumber min={0} max={3600} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'fresh_seconds']} label='最大采样间隔（秒）'>
                      <InputNumber min={1} max={3600} />
                    </Form.Item>
                  </Space>
                </div>
              ))}
              <Button onClick={() => add({ key: key(), title: '', kind: 'manual', query: '', operator: '<', threshold: 0, window_seconds: 0, fresh_seconds: 60, required: true })}>
                增加恢复检查
              </Button>
            </>
          )}
        </Form.List>
      </Card>
    </>
  );
}
