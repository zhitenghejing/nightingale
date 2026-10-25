import React, { useEffect, useState } from 'react';
import { Modal, Form, Input, InputNumber, DatePicker, Select, Checkbox, Row, Col, Alert } from 'antd';
import moment from 'moment';
import { Category, Metadata } from './types';
const timeKeys = ['start_time', 'end_time', 'occurred_at', 'resolved_at'];
export const fieldLabels: Record<string, string> = {
  title: '名称',
  department: '培训部门',
  location: '培训地点',
  training_status: '培训状态',
  start_time: '培训开始时间',
  end_time: '培训结束时间',
  duration: '培训时长（小时）',
  lecturer: '培训讲师',
  participants: '参加人员',
  training_content: '培训内容',
  training_effect: '培训效果',
  inspection_result: '巡检结果',
  scope: '巡检设备范围',
  handler: '处理人',
  summary: '总结 / 说明',
  level: '故障等级',
  affected_business: '影响业务',
  occurred_at: '故障发生时间',
  resolved_at: '故障解决时间',
  main_engineer: '主要处理工程师',
  auxiliary_engineers: '辅助处理工程师',
  custom_type: '文档类型',
};
export function getFieldLabel(category: Category, key: string) {
  if (key === 'title') return category === 'training' ? '培训主题' : category === 'inspection' ? '巡检名称' : '文档名称';
  if (key === 'summary') return category === 'inspection' ? '巡检总结' : '说明';
  return fieldLabels[key] || key;
}
export const categoryFields: Record<Category, string[]> = {
  training: ['title', 'training_status', 'start_time', 'end_time', 'duration', 'department', 'location', 'lecturer', 'participants', 'training_content', 'training_effect'],
  inspection: ['title', 'inspection_result', 'handler', 'scope', 'summary'],
  incident: ['level', 'affected_business', 'occurred_at', 'resolved_at', 'main_engineer', 'auxiliary_engineers'],
  other: ['title', 'custom_type', 'summary'],
};
export default function MetadataForm({
  category,
  initial,
  visible,
  onCancel,
  onSubmit,
}: {
  category: Category;
  initial?: Metadata;
  visible: boolean;
  onCancel: () => void;
  onSubmit: (m: Metadata) => Promise<void>;
}) {
  const [form] = Form.useForm();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!visible) return;
    form.resetFields();
    const values: any = { training_status: 'normal', level: 'IV', ...initial };
    timeKeys.forEach((k) => {
      if (values[k]) values[k] = moment(values[k]);
    });
    form.setFieldsValue(values);
    setError('');
  }, [visible, initial, category]);
  const submit = async () => {
    try {
      const m = await form.validateFields();
      timeKeys.forEach((k) => {
        if (m[k]) m[k] = m[k].toISOString();
        else m[k] = '';
      });
      setSaving(true);
      setError('');
      await onSubmit(m);
    } catch (e: any) {
      if (!e.errorFields) setError(e.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };
  const renderField = (key: string) => {
    if (timeKeys.includes(key)) return <DatePicker showTime format='YYYY-MM-DD HH:mm:ss' style={{ width: '100%' }} />;
    if (key === 'duration') return <InputNumber min={0.01} max={1000000} style={{ width: '100%' }} />;
    if (key === 'training_status') return <Select options={[{ value: 'normal', label: '正常' }]} disabled />;
    if (key === 'inspection_result')
      return (
        <Select
          allowClear
          options={[
            { value: 'normal', label: '正常' },
            { value: 'abnormal', label: '异常' },
          ]}
        />
      );
    if (key === 'level') return <Select options={['I', 'II', 'III', 'IV'].map((v, i) => ({ value: v, label: ['Ⅰ', 'Ⅱ', 'Ⅲ', 'Ⅳ'][i] + '级' }))} />;
    if (key === 'scope') return <Checkbox.Group options={['运维平台', '服务器', '存储', '安全设备', '网络', '交换机']} />;
    if (['participants', 'training_content', 'training_effect', 'summary', 'auxiliary_engineers'].includes(key)) return <Input.TextArea rows={3} maxLength={10000} />;
    return <Input maxLength={255} />;
  };
  return (
    <Modal
      title={initial ? '编辑档案信息' : '新建档案'}
      visible={visible}
      onCancel={onCancel}
      onOk={submit}
      confirmLoading={saving}
      width={800}
      destroyOnClose
      okText={initial ? '保存信息' : '保存草稿'}
      cancelText='取消'
    >
      {error && <Alert type='error' showIcon message={error} className='mb-4' />}
      <Form form={form} layout='vertical'>
        <Row gutter={24}>
          {categoryFields[category].map((key) => (
            <Col key={key} span={['scope', 'summary', 'training_content', 'training_effect', 'participants'].includes(key) ? 24 : 12}>
              <Form.Item
                name={key}
                label={getFieldLabel(category, key)}
                rules={['title', 'level', 'affected_business'].includes(key) ? [{ required: true, message: '请填写此项' }] : undefined}
              >
                {renderField(key)}
              </Form.Item>
            </Col>
          ))}
        </Row>
      </Form>
      <div className='archive-hint'>人员和部门由操作者填写。附件在档案详情中添加，草稿允许暂不提交必交资料。</div>
    </Modal>
  );
}
