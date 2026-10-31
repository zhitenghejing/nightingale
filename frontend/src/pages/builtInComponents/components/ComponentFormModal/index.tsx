import React from 'react';
import _ from 'lodash';
import { Modal, Form, Input, Space, message, Switch, Button, TreeSelect } from 'antd';
import { useTranslation } from 'react-i18next';
import ModalHOC, { ModalWrapProps } from '@/components/ModalHOC';
import { postComponents, putComponent, Component } from '../../services';
import LogoPicker from '../LogoPicker';
import { CatalogEntry, Category, categoryTree, saveMetadata } from '../../catalog';
import { errorText } from '@/pages/collection/errors';

interface Props {
  components: Component[];
  catalogEntry?: CatalogEntry;
  categories?: Category[];
  action: 'create' | 'edit';
  initialValues?: any;
  onOk: (values: any) => void;
}

function index(props: Props & ModalWrapProps) {
  const { t } = useTranslation('builtInComponents');
  const { components, action, initialValues, onOk, visible, destroy, catalogEntry, categories } = props;
  const [form] = Form.useForm();
  const logo = Form.useWatch('logo', form);

  return (
    <Modal
      keyboard={false}
      maskClosable={false}
      width={900}
      title={catalogEntry ? '编辑基本信息' : t(`componentFormModal.${action}`)}
      visible={visible}
      onOk={() => {
        form
          .validateFields()
          .then((values) => {
            if (catalogEntry) {
              saveMetadata(catalogEntry, values)
                .then(() => {
                  message.success(t('common:success.modify'));
                  destroy();
                  onOk(values);
                })
                .catch((e) => message.error(errorText(e, '保存失败')));
            } else if (action === 'edit') {
              putComponent(values).then(() => {
                message.success(t('common:success.modify'));
                destroy();
                onOk(values);
              });
            } else if (action === 'create') {
              postComponents([values]).then((res) => {
                if (_.isEmpty(res)) {
                  message.success(t('common:success.create'));
                  destroy();
                  onOk(values);
                } else {
                  let msg = '';
                  _.forEach(res, (v, k) => {
                    msg += `${k}: ${v}; `;
                  });
                  message.error(msg);
                }
              });
            }
          })
          .catch((e) => {
            console.error(e);
          });
      }}
      onCancel={() => {
        destroy();
      }}
    >
      <Form
        form={form}
        initialValues={catalogEntry ? { name: catalogEntry.name, logo: catalogEntry.logo, category_id: catalogEntry.category_id, disabled: catalogEntry.disabled } : initialValues}
        layout='vertical'
      >
        <Form.Item name='id' hidden>
          <div />
        </Form.Item>
        <Form.Item name='readme' hidden>
          <div />
        </Form.Item>
        <Form.Item
          label={t('ident')}
          name={catalogEntry ? 'name' : 'ident'}
          rules={[
            {
              required: true,
            },
          ]}
        >
          <Input maxLength={128} disabled={!catalogEntry && initialValues?.created_by === 'system'} />
        </Form.Item>
        {catalogEntry && (
          <Form.Item label='设备分类' name='category_id' rules={[{ required: true }]}>
            <TreeSelect treeData={categoryTree(categories || [])} treeDefaultExpandAll />
          </Form.Item>
        )}
        <Form.Item
          label={t('enable')}
          name='disabled'
          valuePropName='checked'
          getValueFromEvent={(checked) => (catalogEntry ? !checked : checked ? 0 : 1)}
          getValueProps={(value) => ({ checked: catalogEntry ? !value : value === 0 })}
        >
          <Switch />
        </Form.Item>
        <Form.Item
          label={
            <Space>
              {t('logo')}
              <LogoPicker components={components} onSelect={(logoURL) => form.setFieldsValue({ logo: logoURL })}>
                <a>{t('logo_picker_title')}</a>
              </LogoPicker>
            </Space>
          }
          name='logo'
          rules={[
            {
              required: true,
            },
          ]}
        >
          <Input placeholder={t('logo_picker_title_placeholder')} />
        </Form.Item>
        <Button
          className='mb-4'
          onClick={() => {
            const input = document.createElement('input');
            input.type = 'file';
            input.accept = 'image/png,image/jpeg,image/svg+xml,image/webp';
            input.onchange = () => {
              const file = input.files?.[0];
              if (!file) return;
              if (file.size > 2 * 1024 * 1024 || !['image/png', 'image/jpeg', 'image/svg+xml', 'image/webp'].includes(file.type)) {
                message.error('请选择 2 MB 以内的 PNG、JPG、SVG 或 WebP 图片');
                return;
              }
              const reader = new FileReader();
              reader.onload = () => form.setFieldsValue({ logo: String(reader.result) });
              reader.readAsDataURL(file);
            };
            input.click();
          }}
        >
          导入本地图标
        </Button>
        {logo && <img src={logo} className='mb-4' style={{ height: 42, maxWidth: '60%' }} />}
      </Form>
    </Modal>
  );
}

export default ModalHOC<Props>(index);
