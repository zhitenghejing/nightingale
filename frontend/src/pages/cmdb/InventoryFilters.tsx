import React, { useState } from 'react';
import { Button, DatePicker, Input, Popover, Select, Space, Tag, Tooltip } from 'antd';
import { CloseOutlined, FilterOutlined, InfoCircleOutlined, SearchOutlined } from '@ant-design/icons';
import moment from 'moment';
import { emptyQuery, InventoryQuery, textFilters, TextFilterKey } from './inventoryQuery';

const options = (values: string[]) => values.map((value) => ({ value, label: value }));

export default function InventoryFilters({ value, onChange }: { value: InventoryQuery; onChange: (query: InventoryQuery) => void }) {
  const [adding, setAdding] = useState(false);
  const [field, setField] = useState<TextFilterKey>('room');
  const [text, setText] = useState('');
  const duplicate = value.conditions.some((condition) => condition.field === field && condition.value.toLowerCase() === text.trim().toLowerCase());
  const add = () => {
    if (!text.trim() || duplicate) return;
    onChange({ ...value, conditions: [...value.conditions, { field, value: text.trim() }] });
    setText('');
    setAdding(false);
  };
  return (
    <section className='cmdb-surface cmdb-filters mb-4' aria-label='设备组合筛选'>
      <div className='cmdb-filter-primary'>
        <Input
          aria-label='综合搜索设备'
          prefix={<SearchOutlined />}
          placeholder='搜索机房、机柜、主机名、IP、厂商、序列号、业务、型号'
          allowClear
          value={value.keyword}
          onChange={(event) => onChange({ ...value, keyword: event.target.value })}
        />
        <Popover
          placement='bottomRight'
          trigger='click'
          visible={adding}
          onVisibleChange={setAdding}
          title='添加筛选条件'
          content={
            <div className='cmdb-condition-editor'>
              <Select aria-label='筛选字段' value={field} onChange={setField} options={textFilters.map(({ key, label }) => ({ value: key, label }))} />
              <Input aria-label='筛选内容' placeholder='输入要匹配的内容' value={text} allowClear onChange={(event) => setText(event.target.value)} onPressEnter={add} />
              <Button type='primary' aria-label='添加筛选条件' disabled={!text.trim() || duplicate} onClick={add}>
                添加
              </Button>
            </div>
          }
        >
          <Button icon={<FilterOutlined />}>添加条件</Button>
        </Popover>
        <Button
          aria-label='重置筛选'
          onClick={() => {
            onChange(emptyQuery());
            setText('');
            setField('room');
            setAdding(false);
          }}
        >
          重置
        </Button>
        <Tooltip
          trigger={['hover', 'focus']}
          title='输入即检索。空格分隔的关键词全部匹配，可跨字段；添加条件可限定具体字段。不同条件同时满足，同一多选框中满足任意一项即可。过保日期包含起止当天，也可只填一端。'
        >
          <Button type='text' aria-label='组合筛选说明' icon={<InfoCircleOutlined />} />
        </Tooltip>
      </div>
      <div className='cmdb-filter-secondary'>
        <Select
          mode='multiple'
          aria-label='设备类型筛选'
          placeholder='设备类型：全部'
          maxTagCount={1}
          allowClear
          value={value.types}
          options={options(['机架式服务器', '存储', '交换机', '防火墙', '其他'])}
          onChange={(types) => onChange({ ...value, types })}
        />
        <Select
          mode='multiple'
          aria-label='保修类型筛选'
          placeholder='保修类型：全部'
          maxTagCount={1}
          allowClear
          value={value.warrantyTypes}
          options={options(['官方维保', '第三方维保', '无维保'])}
          onChange={(warrantyTypes) => onChange({ ...value, warrantyTypes })}
        />
        <Select
          mode='multiple'
          aria-label='保修状态筛选'
          placeholder='保修状态：全部'
          maxTagCount={1}
          allowClear
          value={value.warrantyStatuses}
          options={options(['在保', '即将过保', '已过保', '未填写'])}
          onChange={(warrantyStatuses) => onChange({ ...value, warrantyStatuses })}
        />
        <DatePicker.RangePicker
          className='cmdb-expiry-range'
          allowEmpty={[true, true]}
          placeholder={['过保开始日期', '过保结束日期']}
          format='YYYY-MM-DD'
          value={[value.expireFrom ? moment(value.expireFrom, 'YYYY-MM-DD') : null, value.expireTo ? moment(value.expireTo, 'YYYY-MM-DD') : null]}
          onChange={(_, dates) => onChange({ ...value, expireFrom: dates[0], expireTo: dates[1] })}
        />
      </div>
      {value.conditions.length > 0 && (
        <Space className='cmdb-filter-conditions' wrap size={[4, 4]} aria-label='已添加筛选条件'>
          {value.conditions.map((condition, index) => {
            const label = textFilters.find(({ key }) => key === condition.field)!.label;
            return (
              <Tag key={`${condition.field}:${condition.value}`} className='cmdb-filter-condition'>
                <span>
                  {label}：{condition.value}
                </span>
                <button
                  type='button'
                  aria-label={`移除筛选 ${label}：${condition.value}`}
                  onClick={() => onChange({ ...value, conditions: value.conditions.filter((_, i) => i !== index) })}
                >
                  <CloseOutlined aria-hidden />
                </button>
              </Tag>
            );
          })}
        </Space>
      )}
    </section>
  );
}
