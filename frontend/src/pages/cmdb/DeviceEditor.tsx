import React, { useEffect, useState } from 'react';
import { Alert, Button, Col, Form, Input, InputNumber, Modal, Popconfirm, Radio, Row, Select, Space, message } from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import { Device, Field, blankDevice, deviceError, fieldGroups } from './model';
import { api, Catalog, saveAsset } from './api';
import './index.less';
import { attachMaterial, matchMaterial, materials, searchMaterials } from './materialCatalog';
import { materialImages } from './materialImages';
export interface Editor {
  mode: 'add' | 'edit' | 'view';
  device?: Device;
}
function fieldControl(field: Field) {
  if (field.kind === 'boolean')
    return (
      <Radio.Group
        options={[
          { label: '是', value: true },
          { label: '否', value: false },
        ]}
      />
    );
  if (field.options) return <Select options={field.options.map((value) => ({ value, label: value }))} />;
  if (field.kind === 'number') return <InputNumber min={0} precision={0} className='w-full' />;
  return <Input type={field.kind === 'date' ? 'date' : 'text'} />;
}

export default function DeviceEditor({
  editor: initial,
  devices,
  catalog,
  onClose,
  onSaved,
  canEdit = true,
  canDelete = false,
}: {
  editor?: Editor;
  devices: Device[];
  catalog: Catalog;
  onClose: () => void;
  onSaved: () => Promise<void>;
  canEdit?: boolean;
  canDelete?: boolean;
}) {
  const [editor, setEditor] = useState(initial);
  const [editorError, setEditorError] = useState('');
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();
  const readonly = editor?.mode === 'view';
  const monitored = Form.useWatch('monitored', form);
  const vendor = Form.useWatch('vendor', form) || '';
  const model = Form.useWatch('model', form) || '';
  const type = Form.useWatch('type', form) || '';
  const uStart = Form.useWatch('uStart', form);
  const uEnd = Form.useWatch('uEnd', form);
  const materialId = Form.useWatch('assetMaterialId', form);
  const materialMatch = matchMaterial({ ...blankDevice, vendor, model, type, uStart, uEnd, assetMaterialId: materialId });
  useEffect(() => {
    setEditor(initial);
    setEditorError('');
    form.resetFields();
    if (initial)
      form.setFieldsValue(
        initial.device ? { ...blankDevice, ...initial.device, disks: (initial.device.disks || []).map((x) => ({ ...x })) } : { ...blankDevice, customer: '', disks: [] },
      );
  }, [initial, form]);
  const closeEditor = () => {
    if (saving) return;
    form.resetFields();
    setEditor(undefined);
    setEditorError('');
    onClose();
  };
  const saveDevice = async () => {
    if (readonly) return;
    try {
      await form.validateFields();
      const values = form.getFieldsValue(true);
      const next: Device = {
        ...blankDevice,
        ...editor?.device,
        ...values,
        id: editor?.device?.id || '',
        serial: values.serial.trim(),
        customer: values.customer.trim(),
        room: (values.room || '').trim(),
        rack: (values.rack || '').trim(),
        disks: values.disks || [],
      };
      const error = deviceError(next, devices, catalog.racks.find((r) => r.id === next.rackId)?.units || 60);
      if (error) {
        setEditorError(error);
        return;
      }
      if (!next.monitored) {
        next.alertEnabled = false;
        next.monitorState = '未接入';
      } else if (!editor?.device?.monitored) next.monitorState = '等待心跳';
      setSaving(true);
      await saveAsset(attachMaterial(next), editor?.mode === 'add');
      form.resetFields();
      setEditor(undefined);
      onClose();
      void onSaved().catch(() => {});
      message.success('设备档案已保存');
    } catch (error) {
      // Ant Design displays individual field validation failures in the form.
      if (error instanceof Error) setEditorError(error.message);
    } finally {
      setSaving(false);
    }
  };

  const deleteDevice = async () => {
    if (!canDelete || saving || !editor?.device?.id) return;
    setSaving(true);
    setEditorError('');
    try {
      await api('delete', 'POST', [{ id: editor.device.id, version: editor.device.version }]);
      setEditor(undefined);
      onClose();
      void onSaved().catch(() => {});
      message.success('设备已移入回收站，U 位已释放');
    } catch (error) {
      setEditorError(error instanceof Error ? error.message : '删除失败，请刷新重试');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={editor?.mode === 'add' ? '添加设备' : readonly ? '查看设备' : '编辑设备'}
      visible={!!editor}
      width={1120}
      className='cmdb-device-modal'
      forceRender
      destroyOnClose
      onCancel={closeEditor}
      maskClosable={false}
      footer={
        <Space>
          <span className='cmdb-modal-hint'>{readonly ? '只读查看' : '保存后同步更新清单与机柜图'}</span>
          {editor?.mode !== 'add' && editor?.device?.id && (
            <Popconfirm title='设备将移入回收站并释放 U 位，可从 CMDB 回收站恢复。' okText='确认删除' cancelText='取消' disabled={!canDelete || saving} onConfirm={deleteDevice}>
              <Button danger aria-label='删除设备' icon={<DeleteOutlined />} disabled={!canDelete || saving} loading={saving}>
                删除设备
              </Button>
            </Popconfirm>
          )}
          <Button disabled={saving} onClick={closeEditor}>
            {readonly ? '关闭' : '取消'}
          </Button>
          {readonly ? (
            <Button disabled={!canEdit || saving} type='primary' onClick={() => setEditor({ ...editor!, mode: 'edit' })}>
              编辑设备
            </Button>
          ) : (
            <Button type='primary' loading={saving} onClick={saveDevice}>
              保存设备
            </Button>
          )}
        </Space>
      }
    >
      {editorError && <Alert className='mb-4' type='error' showIcon message={editorError} />}
      <Form
        name='cmdb-device'
        form={form}
        layout='vertical'
        disabled={readonly}
        onValuesChange={(changed) => {
          if ('vendor' in changed || 'model' in changed || 'type' in changed) form.setFieldsValue({ assetMaterialId: undefined });
        }}
      >
        <section className='cmdb-material-selector mb-4'>
          <Form.Item name='assetMaterialId' label='设备素材型号'>
            <Select
              showSearch
              allowClear
              filterOption={(input, option) => {
                const material = materials.find((m) => m.id === option?.value);
                return !!material && searchMaterials(input, material);
              }}
              placeholder='选择准确型号，自动填写厂商、型号和设备类型'
              options={materials.map((m) => ({ value: m.id, label: `${m.brand} ${m.model} · ${m.units}U` }))}
              onChange={(id) => {
                const material = materials.find((m) => m.id === id);
                if (!material) return;
                const start = form.getFieldValue('uStart');
                form.setFieldsValue({
                  assetMaterialId: material.id,
                  vendor: material.vendor,
                  model: material.model,
                  type: material.type,
                  ...(editor?.mode === 'add' && start != null ? { uEnd: start + material.units - 1 } : {}),
                });
              }}
            />
          </Form.Item>
          <Alert
            type={materialMatch.status === 'matched' ? 'success' : materialMatch.status === 'unmatched' ? 'info' : 'warning'}
            showIcon
            message={materialMatch.message}
            action={
              !readonly && materialMatch.status === 'height-mismatch' && uStart != null && materialMatch.material ? (
                <Button size='small' onClick={() => form.setFieldsValue({ uEnd: uStart + materialMatch.material!.units - 1 })}>
                  按标准高度计算结束 U
                </Button>
              ) : undefined
            }
          />
          {materialMatch.status === 'matched' && materialMatch.material && (
            <div className='cmdb-material-preview-grid'>
              {(['front', 'rear'] as const).map((side) => (
                <figure key={side}>
                  <figcaption>{side === 'front' ? '正面素材' : '背面素材'}</figcaption>
                  <img
                    src={materialImages[materialMatch.material!.id][side]}
                    alt={`${model} ${side === 'front' ? '正面' : '背面'}预览`}
                    draggable={false}
                    style={{ aspectRatio: `10 / ${materialMatch.material!.units}` }}
                  />
                </figure>
              ))}
            </div>
          )}
        </section>
        {fieldGroups.map((group) => (
          <section key={group.title} className='mb-2'>
            <h3 className='cmdb-form-heading'>{group.title}</h3>
            <Row gutter={[20, 0]}>
              {group.fields.map((field) => (
                <Col xs={24} sm={12} lg={6} key={field.key}>
                  <Form.Item
                    name={field.key === 'rack' ? undefined : field.key}
                    label={field.label}
                    rules={['customer', 'serial'].includes(field.key) ? [{ required: true, whitespace: true, message: `请填写${field.label}` }] : undefined}
                  >
                    {field.key === 'rack' ? (
                      <Form.Item name='rackId' noStyle>
                        <Select
                          allowClear
                          placeholder='选择机柜（先在机柜图创建）'
                          options={catalog.racks.map((r) => ({ value: r.id, label: `${catalog.rooms.find((x) => x.id === r.roomId)?.name} / ${r.name} · ${r.units}U` }))}
                          onChange={(id) => {
                            const r = catalog.racks.find((x) => x.id === id);
                            form.setFieldsValue({ rackId: id || '', rack: r?.name || '', room: catalog.rooms.find((x) => x.id === r?.roomId)?.name || '' });
                          }}
                        />
                      </Form.Item>
                    ) : field.key === 'room' ? (
                      <Input readOnly />
                    ) : field.key === 'alertEnabled' ? (
                      <Radio.Group
                        disabled={readonly || !monitored}
                        options={[
                          { value: true, label: '是' },
                          { value: false, label: '否' },
                        ]}
                      />
                    ) : (
                      fieldControl(field)
                    )}
                  </Form.Item>
                </Col>
              ))}
            </Row>
          </section>
        ))}
        <h3 className='cmdb-form-heading'>硬盘信息</h3>
        <Form.List name='disks'>
          {(items, { add, remove }) => (
            <>
              {items.map((item, index) => (
                <Row gutter={16} key={item.key} align='middle'>
                  <Col xs={24} sm={9}>
                    <Form.Item name={[item.name, 'model']} label={`硬盘${index + 1}型号`}>
                      <Input />
                    </Form.Item>
                  </Col>
                  <Col xs={24} sm={9}>
                    <Form.Item name={[item.name, 'description']} label={`硬盘${index + 1}描述`}>
                      <Input />
                    </Form.Item>
                  </Col>
                  <Col xs={18} sm={4}>
                    <Form.Item name={[item.name, 'count']} label='数量' rules={[{ required: true, type: 'number', min: 1, message: '数量须大于 0' }]}>
                      <InputNumber min={1} precision={0} className='w-full' />
                    </Form.Item>
                  </Col>
                  {!readonly && (
                    <Col xs={6} sm={2}>
                      <Button type='link' danger onClick={() => remove(item.name)}>
                        移除
                      </Button>
                    </Col>
                  )}
                </Row>
              ))}
              {!readonly && (
                <Button type='dashed' icon={<PlusOutlined />} onClick={() => add({ model: '', description: '', count: 1 })}>
                  添加硬盘组
                </Button>
              )}
              {readonly && !items.length && <span className='cmdb-preview-note'>暂无硬盘信息</span>}
            </>
          )}
        </Form.List>
      </Form>
    </Modal>
  );
}
