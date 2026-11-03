import React, { useRef, useState } from 'react';
import { Button, Form, Input, InputNumber, message, Modal, Select, Space, Switch, Tree } from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';
import { Catalog, Category, categoryPath, categoryTree, moveCategory, moveTemplate, removeCategory, saveCategory } from './catalog';
import { categoryDestination, TEMPLATE_DRAG_TYPE } from './catalogDrag';
import { errorText, formError } from '@/pages/collection/errors';

const levelNames = ['类型', '品牌', '型号'];
export default function CatalogCategoryMenu({
  catalog,
  selected,
  onSelect,
  onChanged,
  canEdit,
  canDelete,
}: {
  catalog?: Catalog;
  selected: string;
  onSelect: (id: string) => void;
  onChanged: () => Promise<void>;
  canEdit: boolean;
  canDelete: boolean;
}) {
  const categories = catalog?.categories || [];
  const [expanded, setExpanded] = useState<string[]>([]),
    [expandAll, setExpandAll] = useState(true);
  const [editing, setEditing] = useState<Partial<Category>>(),
    [form] = Form.useForm();
  const [busy, setBusy] = useState(false),
    [dropTarget, setDropTarget] = useState('');
  const dragCategory = useRef<Category>();
  React.useEffect(() => {
    if (expandAll) setExpanded(categories.map((c) => c.id));
  }, [catalog?.categories]);
  const depth = (id: string) => categoryPath(categories, id).length;
  const showForm = (value: Partial<Category>) => {
    form.resetFields();
    form.setFieldsValue({ sort: 0, disabled: false, ...value });
    setEditing(value);
  };
  const expandPath = (id: string) => setExpanded((keys) => [...new Set([...keys, ...categoryPath(categories, id).map((c) => c.id)])]);
  const mutate = async (operation: () => Promise<unknown>, destination?: string) => {
    setBusy(true);
    try {
      await operation();
      await onChanged();
      if (destination) expandPath(destination);
    } catch (e) {
      message.error(errorText(e, '目录操作失败，请重试'));
    } finally {
      setBusy(false);
      setDropTarget('');
    }
  };
  const del = (c: Category) =>
    Modal.confirm({
      title: `删除${levelNames[depth(c.id) - 1]}“${c.name}”？`,
      content: '目录中的模板和子目录需要先移出或删除。',
      onOk: () =>
        removeCategory(c)
          .then(async () => {
            if (categoryPath(categories, selected).some((node) => node.id === c.id)) onSelect('');
            await onChanged();
          })
          .catch((e) => {
            message.error(errorText(e, '删除失败'));
            return Promise.reject(e);
          }),
    });
  const tree = categoryTree(categories).map(function node(item): any {
    const c = categories.find((value) => value.id === item.key)!;
    const level = depth(c.id);
    return {
      ...item,
      selectable: true,
      category: c,
      children: item.children?.map(node),
      title: (
        <div
          className={`builtin-category-row${dropTarget === c.id ? ' is-drop-target' : ''}`}
          data-category-id={c.id}
          onDragOver={(e) => {
            if (canEdit && !busy && e.dataTransfer.types.includes(TEMPLATE_DRAG_TYPE)) {
              e.stopPropagation();
              if (level === 3) {
                e.preventDefault();
                e.dataTransfer.dropEffect = 'move';
                setDropTarget(c.id);
              } else e.dataTransfer.dropEffect = 'none';
            }
          }}
          onDragEnter={(e) => {
            if (canEdit && e.dataTransfer.types.includes(TEMPLATE_DRAG_TYPE) && level < 3) expandPath(c.id);
          }}
          onDragLeave={() => setDropTarget('')}
          onDrop={(e) => {
            if (!e.dataTransfer.types.includes(TEMPLATE_DRAG_TYPE)) return;
            e.preventDefault();
            e.stopPropagation();
            setDropTarget('');
            if (!canEdit || busy) return;
            if (level !== 3) {
              message.info('请将模板拖到第三级型号目录');
              return;
            }
            try {
              const payload = JSON.parse(e.dataTransfer.getData(TEMPLATE_DRAG_TYPE));
              const entry = catalog?.entries.find((value) => value.component_id === payload.component_id);
              if (!entry || !Number.isInteger(payload.record_version)) return;
              if (entry.category_id === c.id) return;
              mutate(() => moveTemplate({ ...entry, record_version: payload.record_version }, c.id), c.id);
            } catch {
              message.error('无法识别拖动的模板，请重试');
            }
          }}
        >
          <span className='builtin-category-name' title={`${levelNames[level - 1]}：${c.name}`}>
            {c.name}
            {c.disabled ? '（已停用）' : ''}
          </span>
          <span className='builtin-category-actions' onMouseDown={(e) => e.stopPropagation()} onClick={(e) => e.stopPropagation()}>
            {canEdit && (
              <Button
                size='small'
                type='text'
                disabled={busy}
                icon={<PlusOutlined />}
                aria-label={`${level < 3 ? `添加${levelNames[level]}` : '添加同级型号'} ${c.name}`}
                title={level < 3 ? `添加${levelNames[level]}` : '添加同级型号'}
                onClick={() => showForm({ parent_id: level < 3 ? c.id : c.parent_id, name: '' })}
              />
            )}
            {canEdit && (
              <Button size='small' type='text' disabled={busy} icon={<EditOutlined />} aria-label={`编辑目录 ${c.name}`} title='编辑名称和属性' onClick={() => showForm(c)} />
            )}
            {canDelete && (
              <Button size='small' type='text' disabled={busy} danger icon={<DeleteOutlined />} aria-label={`删除目录 ${c.name}`} title='删除目录' onClick={() => del(c)} />
            )}
          </span>
        </div>
      ),
    };
  });
  const editLevel = editing?.id ? depth(editing.id) : depth(editing?.parent_id || '') + 1;
  return (
    <>
      <Space className='mb-2'>
        <Button
          size='small'
          onClick={() => {
            setExpandAll(true);
            setExpanded(categories.map((c) => c.id));
          }}
        >
          全部展开
        </Button>
        <Button
          size='small'
          onClick={() => {
            setExpandAll(false);
            setExpanded([]);
          }}
        >
          全部折叠
        </Button>
        {canEdit && (
          <Button
            type='text'
            size='small'
            icon={<PlusOutlined />}
            aria-label='添加类型'
            title='添加一级类型'
            onClick={() => showForm({ parent_id: '', name: '' })}
            disabled={busy}
          />
        )}
      </Space>
      <Button block type={!selected ? 'primary' : 'text'} onClick={() => onSelect('')}>
        全部模板
      </Button>
      {/* Keep native dragend cleanup mounted while saving; reject further drops instead. */}
      <Tree
        blockNode
        expandAction='click'
        treeData={tree}
        selectedKeys={selected ? [selected] : []}
        expandedKeys={expanded}
        autoExpandParent={false}
        draggable={canEdit ? { icon: false } : false}
        onExpand={(keys) => {
          setExpandAll(false);
          setExpanded(keys.map(String));
        }}
        onSelect={(keys) => onSelect(String(keys[0] || ''))}
        onDragStart={(info) => {
          dragCategory.current = categories.find((c) => c.id === String(info.node.key));
        }}
        onDragEnd={() => {
          dragCategory.current = undefined;
        }}
        allowDrop={(info) => !busy && !!dragCategory.current && !!categoryDestination(categories, dragCategory.current.id, String(info.dropNode.key), info.dropPosition)}
        onDrop={(info) => {
          const source = dragCategory.current;
          dragCategory.current = undefined;
          if (!source || !canEdit || busy) return;
          const position = info.dropToGap ? info.dropPosition - Number(info.node.pos.split('-').pop()) : 0;
          const destination = categoryDestination(categories, source.id, String(info.node.key), position);
          if (destination) mutate(() => moveCategory(source, destination), destination.parent_id);
        }}
      />
      {canEdit && <p className='builtin-category-hint mt-3'>目录可拖动排序或更换同级归属；模板可从卡片或列表拖到型号目录。</p>}
      <Modal
        visible={!!editing}
        confirmLoading={busy}
        okText='保存'
        cancelText='取消'
        title={`${editing?.id ? '编辑' : '添加'}${levelNames[editLevel - 1] || '目录'}`}
        onCancel={() => setEditing(undefined)}
        onOk={async () => {
          setBusy(true);
          try {
            const values = await form.validateFields();
            await saveCategory({ ...editing, ...values });
            setEditing(undefined);
            await onChanged();
            expandPath(values.parent_id || '');
          } catch (e) {
            if (!formError(e)) message.error(errorText(e, '保存失败'));
          } finally {
            setBusy(false);
          }
        }}
      >
        <Form form={form} layout='vertical'>
          <Form.Item name='name' label={`${levelNames[editLevel - 1] || '目录'}名称`} rules={[{ required: true, whitespace: true, max: 128 }]}>
            <Input maxLength={128} />
          </Form.Item>
          <Form.Item name='parent_id' label='上级目录'>
            <Select
              disabled={editLevel === 1}
              options={
                editLevel === 1
                  ? [{ value: '', label: '根目录（类型）' }]
                  : categories
                      .filter((c) => depth(c.id) === editLevel - 1)
                      .map((c) => ({
                        value: c.id,
                        label: categoryPath(categories, c.id)
                          .map((part) => part.name)
                          .join(' → '),
                      }))
              }
            />
          </Form.Item>
          <Space>
            <Form.Item name='sort' label='排序'>
              <InputNumber />
            </Form.Item>
            <Form.Item name='disabled' valuePropName='checked' label='停用目录'>
              <Switch />
            </Form.Item>
          </Space>
        </Form>
      </Modal>
    </>
  );
}
