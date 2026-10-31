import React, { useEffect, useState } from 'react';
import { Alert, Form, Select, Space } from 'antd';
import type { FormInstance } from 'antd';
import type { Device } from '@/services/collection';
import { Catalog, CatalogEntry, categoryPath, DeviceField, entryDefinition, entryStatus } from '@/pages/builtInComponents/catalog';

export const legacyFields: DeviceField[] = [
  { key: 'protocol', label: 'SNMP 版本', type: 'select', required: true, default: 'snmp_v2c', options: ['snmp_v2c'] },
  { key: 'port', label: 'SNMP 端口', type: 'number', required: true, default: 161 },
  { key: 'credential_id', label: 'SNMP 团体字凭据', type: 'credential', required: true },
];
// rc-field-form merges nested setFieldsValue objects. Explicit empty controlled
// values also prevent password inputs retaining their previous DOM value.
export function resetTemplateParameters(form: FormInstance, defaults: Record<string, unknown> = {}) {
  const current = form.getFieldValue('parameters') || {};
  const empty = Object.fromEntries(
    Object.entries(current).map(([key, value]) => [key, Array.isArray(value) ? [] : typeof value === 'number' ? null : typeof value === 'boolean' ? false : '']),
  );
  form.setFields(Object.entries({ ...empty, ...defaults }).map(([key, value]) => ({ name: ['parameters', key], value })));
}
export default function TemplateSelection({
  catalog,
  form,
  device,
  onSelection,
}: {
  catalog?: Catalog;
  form: FormInstance;
  device?: Device;
  onSelection: (entry?: CatalogEntry) => void;
}) {
  const [selected, setSelected] = useState<string[]>([]);
  const version = Form.useWatch('template_version_id', form);
  const selection = Form.useWatch('template_selection', form);
  const entries = catalog?.entries || [];
  const related = entries.filter((e) => e.category_id === selected[2]);
  const savedEntry = catalog?.entries.find((e) => e.component_id === device?.profile?.component_id);
  useEffect(() => {
    if (device && catalog) {
      if (savedEntry) setSelected(categoryPath(catalog.categories, savedEntry.category_id).map((c) => c.id));
      form.setFieldsValue({ template_selection: savedEntry?.published_version_id === device.template_version_id ? `catalog:${savedEntry.component_id}` : `snapshot:${device.template_version_id}` });
    }
  }, [savedEntry?.component_id, device?.id, !!catalog]);
  function clear(next: string[]) {
    setSelected(next);
    resetTemplateParameters(form);
    form.setFieldsValue({
      template_version_id: undefined,
      template_selection: undefined,
      protocol: undefined,
      port: undefined,
      credential_id: undefined,
      custom_config: undefined,
      custom_metrics: undefined,
      reset_resources: false,
      parameters: {},
      notify: undefined,
    });
    onSelection(undefined);
  }
  function choose(entry: CatalogEntry) {
    resetTemplateParameters(form);
    const definition = entryDefinition(entry);
    const defaults = Object.fromEntries((definition.fields || []).filter((f) => f.default !== undefined).map((f) => [f.key, f.default]));
    form.setFieldsValue({
      ...defaults,
      parameters: Object.fromEntries(Object.entries(defaults).filter(([key]) => !['protocol', 'port', 'credential_id'].includes(key))),
      protocol: definition.adapter,
      port: defaults.port,
      template_selection: `catalog:${entry.component_id}`,
      template_version_id: entry.published_version_id || undefined,
      credential_id: undefined,
      notify: definition.notify || { mode: 'events_only', rule_ids: [], message: '' },
    });
    onSelection(entry);
  }
  const inSavedCategory = device && savedEntry?.category_id === selected[2];
  const currentEntry = entries.find((e) => selection === `catalog:${e.component_id}`);
  return (
    <div className='mb-4'>
      <p>全部 {entries.length} 个模板均可按分类查看；自动接入状态以各模板的发布与采集能力为准。</p>
      <Space align='start' wrap>
        {['设备类型', '产品 / 厂商', '版本 / 型号'].map((label, index) => {
          const eligible = new Set(entries.flatMap((e) => categoryPath(catalog?.categories || [], e.category_id).map((c) => c.id)));
          if (savedEntry) categoryPath(catalog?.categories || [], savedEntry.category_id).forEach((c) => eligible.add(c.id));
          return (
            <Form.Item label={label} required key={label}>
              <Select
                showSearch
                optionFilterProp='label'
                aria-label={label}
                style={{ width: index === 2 ? 220 : 180 }}
                placeholder={`请选择${label}`}
                value={selected[index]}
                disabled={index > 0 && !selected[index - 1]}
                options={(catalog?.categories || [])
                  .filter((c) => c.parent_id === (index === 0 ? '' : selected[index - 1]) && eligible.has(c.id))
                  .map((c) => ({ value: c.id, label: `${c.name}${c.disabled ? '（已停用）' : ''}` }))}
                onChange={(value) => {
                  const next = [...selected.slice(0, index), value];
                  clear(next);
                  if (index === 2) {
                    const matches = entries.filter((e) => e.category_id === value);
                    if (matches.length === 1) choose(matches[0]);
                  }
                }}
              />
            </Form.Item>
          );
        })}
      </Space>
      <Form.Item hidden name='template_version_id'><input /></Form.Item>
      <Form.Item hidden={related.length === 1 && !device} name='template_selection' label='适用模板' rules={[{ required: true, message: '请完成三级分类并选择模板' }]}>
        <Select
          showSearch
          optionFilterProp='label'
          aria-label='适用模板'
          disabled={!selected[2] && !device}
          options={[
            ...related.map((e) => ({ value: `catalog:${e.component_id}`, label: `${e.name} · ${entryStatus(e)}` })),
            ...(device && (inSavedCategory || !savedEntry) && !related.some((e) => e.published_version_id === device.template_version_id)
              ? [{ value: `snapshot:${device.template_version_id}`, label: `${device.profile?.name || '历史模板'}（设备当前快照）` }]
              : []),
          ]}
          onChange={(value) => {
            const entry = related.find((e) => `catalog:${e.component_id}` === value);
            if (entry) choose(entry);
            else {
              resetTemplateParameters(form, device?.parameters || {});
              form.setFieldsValue({ template_version_id: device?.template_version_id, protocol: device?.protocol, port: device?.port, credential_id: device?.credential_id });
              onSelection(undefined);
            }
          }}
        />
      </Form.Item>
      {(!!version || !!currentEntry) && (
        <Alert
          type={currentEntry && (!currentEntry.published || currentEntry.onboarding?.ready === false) ? 'warning' : 'info'}
          showIcon
          message={currentEntry ? `${currentEntry.name} · ${entryStatus(currentEntry)}` : `${device?.profile?.name || '设备当前快照'} · v${device?.profile?.catalog?.version || 1}`}
          description={currentEntry && !currentEntry.published ? '已选择原有模板，可查看表单草稿和采集说明。完成接入并发布后才能添加受管采集设备。' : '将使用此发布版本的表单、采集方式、仪表盘和告警通知。'}
        />
      )}
    </div>
  );
}
