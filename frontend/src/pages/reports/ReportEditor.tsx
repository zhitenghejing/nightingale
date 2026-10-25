import React, { useEffect, useState } from 'react';
import { Alert, Button, Form, Input, Modal, Upload } from 'antd';
import { UploadOutlined } from '@ant-design/icons';
import { Report, ReportFields, ReportKind, saveReport } from './api';

interface Props {
  open: boolean;
  kind: ReportKind;
  existing?: Report;
  onClose: () => void;
  onSaved: (report: Report) => void;
}
export default function ReportEditor({ open, kind, existing, onClose, onSaved }: Props) {
  const [form] = Form.useForm<ReportFields>();
  const [file, setFile] = useState<File>();
  const [base, setBase] = useState<Report>();
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const reset = () => {
    form.resetFields();
    setFile(undefined);
    setBase(undefined);
    setError('');
    setSaving(false);
  };
  useEffect(() => {
    reset();
    if (open) {
      setBase(existing);
      form.setFieldsValue(existing || { reportDate: new Date().toLocaleDateString('sv-SE') });
    }
  }, [open, existing?.id]);
  const close = () => {
    if (saving) return;
    reset();
    onClose();
  };
  const submit = async () => {
    try {
      const values = await form.validateFields();
      if (!existing && !file) throw new Error('请选择报告附件');
      setSaving(true);
      setError('');
      const saved = await saveReport(kind, values, file, base);
      reset();
      onSaved(saved);
      onClose();
    } catch (e) {
      if (e instanceof Error) setError(e.message);
      setSaving(false);
    }
  };
  return (
    <Modal
      title={`${existing ? '编辑' : '上传'}${kind === 'inspection' ? '巡检报告' : '故障报告'}`}
      visible={open}
      width={720}
      onCancel={close}
      onOk={submit}
      confirmLoading={saving}
      maskClosable={false}
      closable={!saving}
      cancelButtonProps={{ disabled: saving }}
      forceRender
      destroyOnClose
    >
      {error && <Alert type='error' message={error} showIcon className='mb-4' />}
      <Form form={form} layout='vertical' disabled={saving}>
        <Form.Item name='title' label='报告标题' rules={[{ required: true, whitespace: true, message: '请填写报告标题' }, { max: 180 }]}>
          <Input placeholder={kind === 'inspection' ? '例如：数据中心 10 月例行巡检报告' : '例如：核心交换机故障分析与恢复报告'} />
        </Form.Item>
        <div className='grid grid-cols-2 gap-x-4'>
          <Form.Item name='engineer' label='工程师姓名' rules={[{ required: true, whitespace: true, message: '请填写工程师姓名' }, { max: 70 }]}>
            <Input />
          </Form.Item>
          <Form.Item name='reportDate' label='报告日期' rules={[{ required: true, message: '请选择报告日期' }]}>
            <Input type='date' />
          </Form.Item>
          <Form.Item name='company' label='所属单位' rules={[{ max: 180 }]}>
            <Input />
          </Form.Item>
          <Form.Item name='contact' label='联系方式' rules={[{ max: 70 }]}>
            <Input placeholder='电话或邮箱' />
          </Form.Item>
        </div>
        <Form.Item name='summary' label='文档简要内容' rules={[{ required: true, whitespace: true, message: '请填写报告摘要' }, { max: 4000 }]}>
          <Input.TextArea
            rows={4}
            showCount
            maxLength={4000}
            placeholder={kind === 'inspection' ? '巡检范围、检查结果、发现的问题及建议…' : '故障现象、影响范围、处理过程与根因…'}
          />
        </Form.Item>
        {existing ? (
          <Alert type='info' message={`已归档原件：${existing.filename}`} description='可修改报告信息；如需更新文档，请上传为一份新报告，保留原始记录。' />
        ) : (
          <Form.Item label='报告附件' required extra='支持 .doc、.docx、.pdf，单份最大 30 MB。Word 保存原件并自动生成 PDF 预览。'>
            <Upload
              accept='.doc,.docx,.pdf'
              maxCount={1}
              fileList={file ? [{ uid: 'report', name: file.name, status: 'done' }] : []}
              beforeUpload={(candidate) => {
                if (!/\.(docx?|pdf)$/i.test(candidate.name) || candidate.size > 30 * 1024 * 1024) {
                  setError('请选择不超过 30 MB 的 Word 或 PDF 文件');
                  return Upload.LIST_IGNORE;
                }
                setFile(candidate);
                setError('');
                return false;
              }}
              onRemove={() => {
                setFile(undefined);
                return true;
              }}
            >
              <Button icon={<UploadOutlined />}>选择报告文件</Button>
            </Upload>
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
}
