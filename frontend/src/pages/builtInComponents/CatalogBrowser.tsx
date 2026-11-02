import React, { useContext, useEffect, useState } from 'react';
import { Button, Empty, Input, message, Modal, Select, Space, Table, Tag, Form, TreeSelect } from 'antd';
import { AppstoreOutlined, UnorderedListOutlined, EditOutlined, DeleteOutlined, SearchOutlined } from '@ant-design/icons';
import AuthorizationWrapper from '@/components/AuthorizationWrapper';
import { Component } from './types';
import TemplateLogo from './TemplateLogo';
import ComponentFormModal from './components/ComponentFormModal';
import { Catalog, CatalogEntry, categoryPath, categoryTree, getCatalog, matchingEntries, removeCatalog, createCatalog } from './catalog';
import { CommonStateContext } from '@/App';
import CatalogCategoryMenu from './CatalogCategoryMenu';
import { TEMPLATE_DRAG_TYPE } from './catalogDrag';
import { errorText } from '@/pages/collection/errors';

export default function CatalogBrowser({ components, onOpen, reload }: { components: Component[]; onOpen: (component: Component) => void; reload: () => void }) {
  const commonState = useContext(CommonStateContext);
  const { profile } = commonState;
  const canEdit = !!profile.admin;
  const [creating, setCreating] = useState(false);
  const [createForm] = Form.useForm();
  const [catalog, setCatalog] = useState<Catalog>();
  const [error, setError] = useState('');
  const [query, setQuery] = useState(localStorage.getItem('builtin-list-search-value') || '');
  const [category, setCategory] = useState('');
  const [status, setStatus] = useState('all');
  const [layout, setLayout] = useState(localStorage.getItem('builtin-catalog-layout') || 'grid');
  const fetchCatalog = () =>
    getCatalog()
      .then((c) => {
        setCatalog(c);
        setError('');
      })
      .catch((e) => setError(errorText(e, '加载模板失败')));
  useEffect(() => {
    fetchCatalog();
  }, [components]);
  const changed = async () => {
    await fetchCatalog();
    reload();
  };
  const draggable = (entry: CatalogEntry): React.HTMLAttributes<HTMLElement> => ({
    draggable: canEdit,
    onDragStart: (event) => {
      event.dataTransfer.setData(TEMPLATE_DRAG_TYPE, JSON.stringify({ component_id: entry.component_id, record_version: entry.record_version }));
      event.dataTransfer.effectAllowed = 'move';
    },
  });
  const edit = (entry: CatalogEntry) => {
    const component = components.find((c) => c.id === entry.component_id);
    if (component) ComponentFormModal({ components, action: 'edit', initialValues: component, catalogEntry: entry, categories: catalog?.categories, onOk: changed, commonState });
  };
  const open = (entry: CatalogEntry) => {
    const component = components.find((c) => c.id === entry.component_id);
    if (component) onOpen({ ...component, logo: entry.logo, display_name: entry.name });
  };
  const del = (entry: CatalogEntry) =>
    Modal.confirm({
      title: `删除模板“${entry.name}”？`,
      content: '被设备引用的模板不能删除，可以停用以停止新的接入。',
      onOk: () =>
        removeCatalog(entry)
          .then(changed)
          .catch((e) => {
            message.error(errorText(e, '删除失败'));
            return Promise.reject(e);
          }),
    });
  const actions = (entry: CatalogEntry) => (
    <Space size={0}>
      <AuthorizationWrapper allowedPerms={['/components/put']}>
        <Button type='text' size='small' aria-label={`编辑基本信息 ${entry.name}`} title='编辑基本信息' icon={<EditOutlined />} onClick={() => edit(entry)} />
      </AuthorizationWrapper>
      <AuthorizationWrapper allowedPerms={['/components/del']}>
        <Button type='text' size='small' danger aria-label={`删除模板 ${entry.name}`} title='删除模板' icon={<DeleteOutlined />} onClick={() => del(entry)} />
      </AuthorizationWrapper>
    </Space>
  );
  const entries = catalog ? matchingEntries(catalog.entries, catalog.categories, query, category, status) : [];
  return (
    <div className='builtin-catalog'>
      <aside className='builtin-catalog-tree'>
        <CatalogCategoryMenu catalog={catalog} selected={category} onSelect={setCategory} onChanged={changed} canEdit={canEdit} canDelete={canEdit} />
      </aside>
      <main className='builtin-catalog-content'>
        <div className='builtin-catalog-breadcrumb'>
          {category
            ? categoryPath(catalog?.categories || [], category)
                .map((c) => c.name)
                .join(' → ')
            : '全部模板'}
        </div>
        <div className='builtin-catalog-toolbar'>
          <Space wrap>
            <Input
              allowClear
              prefix={<SearchOutlined />}
              placeholder='搜索模板或分类'
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                localStorage.setItem('builtin-list-search-value', e.target.value);
              }}
            />
            <Select
              aria-label='模板状态'
              value={status}
              onChange={setStatus}
              options={[
                { value: 'all', label: '全部状态' },
                { value: 'ready', label: '可自动接入' },
                { value: 'draft', label: '未发布 / 资料包' },
                { value: 'disabled', label: '已停用' },
              ]}
            />
          </Space>
          <Space>
            <Button.Group>
              <Button
                aria-label='卡片模式'
                type={layout === 'grid' ? 'primary' : 'default'}
                icon={<AppstoreOutlined />}
                onClick={() => {
                  setLayout('grid');
                  localStorage.setItem('builtin-catalog-layout', 'grid');
                }}
              />
              <Button
                aria-label='列表模式'
                type={layout === 'list' ? 'primary' : 'default'}
                icon={<UnorderedListOutlined />}
                onClick={() => {
                  setLayout('list');
                  localStorage.setItem('builtin-catalog-layout', 'list');
                }}
              />
            </Button.Group>
            <AuthorizationWrapper allowedPerms={['/components/add']}>
              <Button
                type='primary'
                onClick={() => {
                  createForm.resetFields();
                  createForm.setFieldsValue({ category_id: categoryPath(catalog?.categories || [], category).length === 3 ? category : undefined });
                  setCreating(true);
                }}
              >
                新建模板
              </Button>
            </AuthorizationWrapper>
          </Space>
        </div>
        {error && (
          <div role='alert'>
            {error} <Button onClick={fetchCatalog}>重试</Button>
          </div>
        )}
        {layout === 'grid' ? (
          <div className='builtin-cates-grid'>
            {entries.map((entry) => (
              <article key={entry.component_id} className='builtin-cates-grid-item' data-template-id={entry.component_id} {...draggable(entry)}>
                <button className='builtin-catalog-card-body' onClick={() => open(entry)} aria-label={`配置模板 ${entry.name}`}>
                  <TemplateLogo src={entry.logo} />
                  <span>{entry.name}</span>
                </button>
                <div className='builtin-cates-grid-item-operations'>{actions(entry)}</div>
              </article>
            ))}
          </div>
        ) : (
          <Table
            rowKey='component_id'
            onRow={(entry) => ({ ...draggable(entry), 'data-template-id': entry.component_id })}
            dataSource={entries}
            pagination={{ pageSize: 20 }}
            columns={[
              {
                title: '模板名称',
                render: (_, entry) => (
                  <a onClick={() => open(entry)}>
                    <Space>
                      <TemplateLogo src={entry.logo} small />
                      {entry.name}
                    </Space>
                  </a>
                ),
              },
              {
                title: '分类',
                render: (_, entry) =>
                  categoryPath(catalog?.categories || [], entry.category_id)
                    .map((c) => c.name)
                    .join(' / '),
              },
              { title: '发布状态', render: (_, entry) => <Tag>{entry.disabled ? '已停用' : entry.published ? `已发布 v${entry.published.version}` : '资料包 / 草稿'}</Tag> },
              { title: '采集方式', render: (_, entry) => entry.published?.definition.adapter || entry.draft.adapter },
              { title: '操作', render: (_, entry) => actions(entry) },
            ]}
          />
        )}
        {!error && catalog && entries.length === 0 && <Empty description='没有匹配的模板' />}
      </main>
      <Modal
        visible={creating}
        title='新建模板'
        onCancel={() => setCreating(false)}
        onOk={async () => {
          try {
            await createCatalog(await createForm.validateFields());
            setCreating(false);
            changed();
          } catch (e) {
            if (!(e as any).errorFields) message.error(errorText(e, '创建失败'));
          }
        }}
      >
        <Form form={createForm} layout='vertical'>
          <Form.Item name='name' label='模板名称' rules={[{ required: true, max: 128 }]}>
            <Input />
          </Form.Item>
          <Form.Item name='category_id' label='设备分类（第三级）' rules={[{ required: true }]}>
            <TreeSelect treeData={categoryTree(catalog?.categories || [])} treeDefaultExpandAll />
          </Form.Item>
          <Form.Item name='base_component_id' label='基于已有模板新建（可选；复制完整草稿）'>
            <Select allowClear showSearch optionFilterProp='label' options={catalog?.entries.map((e) => ({ value: e.component_id, label: e.name }))} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
