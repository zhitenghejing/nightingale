import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Input, Space, Tabs, Tag, Select, Modal, message, DatePicker, Popover, Tooltip } from 'antd';
import { PlusOutlined, ReloadOutlined, DeleteOutlined, FilterOutlined } from '@ant-design/icons';
import { Link, useHistory, useLocation } from 'react-router-dom';
import moment from 'moment';
import PageLayout from '@/components/pageLayout';
import EnhancedTable from '@/components/EnhancedTable';
import { Archive, Category, categories, states, fmtDate } from './types';
import { api, createArchive, errorText, listArchives } from './services';
import MetadataForm from './MetadataForm';
const levelLabel = (v?: string) => ({ I: 'Ⅰ', II: 'Ⅱ', III: 'Ⅲ', IV: 'Ⅳ' }[v || ''] || '—');
export default function List() {
  const history = useHistory();
  const location = useLocation();
  const query = new URLSearchParams(location.search);
  const category = (categories.some((c) => c.key === query.get('tab')) ? query.get('tab') : 'training') as Category;
  const [filter, setFilter] = useState<Record<string, any>>({});
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [trash, setTrash] = useState(false);
  const [data, setData] = useState<Archive[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);
  const [moreFiltersOpen, setMoreFiltersOpen] = useState(false);
  const [selected, setSelected] = useState<React.Key[]>([]);
  const seq = useRef(0);
  const load = useCallback(async () => {
    const token = ++seq.current;
    setLoading(true);
    try {
      const res = await listArchives({ category, ...filter, q: search, page, limit: size, deleted: trash });
      if (token === seq.current) {
        setData(res.list || []);
        setTotal(res.total);
        setError('');
      }
    } catch (e) {
      if (token === seq.current) setError(errorText(e));
    } finally {
      if (token === seq.current) setLoading(false);
    }
  }, [category, filter, search, page, size, trash]);
  useEffect(() => {
    load();
    return () => {
      ++seq.current;
    };
  }, [load]);
  const changeFilter = (k: string, value: any) => {
    setFilter((v) => ({ ...v, [k]: value }));
    setPage(1);
    setSelected([]);
  };
  const changeCategory = (key: string) => {
    history.replace(`/archives?tab=${key}`);
    setFilter({});
    setSearch('');
    setPage(1);
    setSelected([]);
    setMoreFiltersOpen(false);
  };
  const extraFilterCount = Number(!!filter.department) + Number(!!filter.location) + Number(!!(filter.start || filter.end));
  const searchPlaceholder = {
    training: '搜索培训主题 / 讲师',
    inspection: '搜索巡检名称 / 处理人',
    incident: '搜索影响业务 / 主要工程师',
    other: '搜索文档名称',
  }[category];
  const remove = (items: Archive[]) =>
    Modal.confirm({
      title: `将 ${items.length} 条档案移入回收站？`,
      content: '原文件和版本历史将保留，可以从回收站恢复。正在编辑的档案会阻止本次操作。',
      onOk: async () => {
        try {
          await api('', 'DELETE', { items: items.map(({ id, revision }) => ({ id, revision })) });
          setSelected([]);
          load();
        } catch (e) {
          message.error(errorText(e));
          throw e;
        }
      },
    });
  const columns: any[] = [{ title: '序号', key: 'index', width: 65, render: (_: any, __: any, index: number) => (page - 1) * size + index + 1 }];
  const titleColumn = {
    title: category === 'training' ? '培训主题' : category === 'inspection' ? '巡检名称' : category === 'incident' ? '影响业务' : '文档名称',
    dataIndex: 'title',
    width: 240,
    render: (v: string, r: Archive) => <Link to={`/archives/${r.id}`}>{category === 'incident' ? r.metadata.affected_business : v}</Link>,
  };
  columns.push(titleColumn);
  if (category === 'training')
    columns.push(
      { title: '培训状态', width: 90, render: () => <Tag color='green'>正常</Tag> },
      { title: '培训时间', width: 300, render: (_: any, r: Archive) => `${fmtDate(r.metadata.start_time)} ～ ${fmtDate(r.metadata.end_time)}` },
      ...['department', 'location', 'duration', 'lecturer'].map((key) => ({
        title: { department: '培训部门', location: '培训地点', duration: '培训时长（小时）', lecturer: '培训讲师' }[key],
        width: 140,
        render: (_: any, r: Archive) => r.metadata[key] ?? '—',
      })),
    );
  if (category === 'inspection')
    columns.push(
      { title: '巡检设备范围', width: 230, render: (_: any, r: Archive) => r.metadata.scope?.join('、') || '—' },
      {
        title: '巡检结果',
        width: 100,
        render: (_: any, r: Archive) =>
          r.metadata.inspection_result ? (
            <Tag color={r.metadata.inspection_result === 'normal' ? 'green' : 'red'}>{r.metadata.inspection_result === 'normal' ? '正常' : '异常'}</Tag>
          ) : (
            '—'
          ),
      },
      { title: '处理人', width: 130, render: (_: any, r: Archive) => r.metadata.handler || '—' },
      { title: '巡检总结', width: 260, ellipsis: true, render: (_: any, r: Archive) => r.metadata.summary || '—' },
    );
  if (category === 'incident')
    columns.push(
      { title: '故障等级', width: 100, render: (_: any, r: Archive) => `${levelLabel(r.metadata.level)}级` },
      ...['occurred_at', 'resolved_at'].map((key) => ({
        title: key === 'occurred_at' ? '发生时间' : '解决时间',
        width: 190,
        render: (_: any, r: Archive) => fmtDate(r.metadata[key]),
      })),
      { title: '主要处理工程师', width: 150, render: (_: any, r: Archive) => r.metadata.main_engineer || '—' },
    );
  if (category === 'other')
    columns.push(
      { title: '文档类型', width: 180, render: (_: any, r: Archive) => r.metadata.custom_type || '—' },
      { title: '说明', width: 260, ellipsis: true, render: (_: any, r: Archive) => r.metadata.summary || '—' },
    );
  columns.push(
    { title: '文档状态', width: 100, dataIndex: 'state', render: (v: string) => <Tag color={v === 'archived' ? 'green' : 'default'}>{v === 'archived' ? '已归档' : '草稿'}</Tag> },
    { title: '资料', width: 85, render: (_: any, r: Archive) => `${r.documents?.length || 0} 份` },
    { title: '创建时间', dataIndex: 'create_at', width: 190, render: fmtDate },
    { title: '更新时间', width: 190, dataIndex: 'update_at', render: fmtDate },
    { title: '更新人', width: 130, dataIndex: 'update_name' },
    {
      title: '操作',
      key: 'operate',
      width: 135,
      fixed: 'right',
      render: (_: any, r: Archive) => (
        <Space>
          <Link to={`/archives/${r.id}`}>查看</Link>
          {trash ? (
            <Button
              type='link'
              size='small'
              onClick={async () => {
                try {
                  await api(`/${r.id}/restore`, 'POST', { revision: r.revision });
                  load();
                } catch (e) {
                  message.error(errorText(e));
                }
              }}
            >
              恢复
            </Button>
          ) : (
            <Button danger type='link' size='small' onClick={() => remove([r])}>
              删除
            </Button>
          )}
        </Space>
      ),
    },
  );
  return (
    <div className='archive-list-layout'>
      <PageLayout
        title={
          <Tabs className='archive-header-tabs' activeKey={category} onChange={changeCategory} tabBarGutter={0}>
            {categories.map((c) => (
              <Tabs.TabPane key={c.key} tab={c.label} />
            ))}
          </Tabs>
        }
      >
        <div className='n9e archive-page'>
          <div className='archive-list-panel fc-border rounded-lg'>
            <div className='archive-list-toolbar'>
              <Tooltip title='刷新列表'>
                <Button aria-label='刷新档案' icon={<ReloadOutlined spin={loading} />} onClick={load} />
              </Tooltip>
              <Input.Search
                className='archive-list-search'
                placeholder={searchPlaceholder}
                allowClear
                key={category}
                onSearch={(v) => {
                  setSearch(v);
                  setPage(1);
                }}
              />
              <Select value={filter.state} allowClear placeholder='文档状态' options={states} className='archive-list-state' onChange={(v) => changeFilter('state', v)} />
              {category === 'inspection' && (
                <Select
                  allowClear
                  placeholder='巡检结果'
                  className='archive-list-category-filter'
                  value={filter.result}
                  onChange={(v) => changeFilter('result', v)}
                  options={[
                    { value: 'normal', label: '正常' },
                    { value: 'abnormal', label: '异常' },
                  ]}
                />
              )}
              {category === 'incident' && (
                <Select
                  allowClear
                  placeholder='故障等级'
                  value={filter.level}
                  onChange={(v) => changeFilter('level', v)}
                  className='archive-list-category-filter'
                  options={['I', 'II', 'III', 'IV'].map((v) => ({ value: v, label: levelLabel(v) + '级' }))}
                />
              )}
              <Popover
                title='更多筛选'
                placement='bottomLeft'
                trigger='click'
                visible={moreFiltersOpen}
                onVisibleChange={setMoreFiltersOpen}
                content={
                  <div className='archive-extra-filters'>
                    {category === 'training' && (
                      <div className='archive-extra-filter-row'>
                        <label htmlFor='archive-department-filter'>
                          培训部门
                          <Input
                            id='archive-department-filter'
                            placeholder='培训部门'
                            allowClear
                            value={filter.department || ''}
                            onChange={(e) => changeFilter('department', e.target.value)}
                          />
                        </label>
                        <label htmlFor='archive-location-filter'>
                          培训地点
                          <Input
                            id='archive-location-filter'
                            placeholder='培训地点'
                            allowClear
                            value={filter.location || ''}
                            onChange={(e) => changeFilter('location', e.target.value)}
                          />
                        </label>
                      </div>
                    )}
                    <div className='archive-extra-filter-time'>
                      <div>{category === 'training' ? '培训开始时间' : category === 'incident' ? '故障发生时间' : '创建时间'}</div>
                      <DatePicker.RangePicker
                        showTime
                        value={[filter.start ? moment(filter.start) : null, filter.end ? moment(filter.end) : null]}
                        onChange={(dates) => {
                          setFilter((v) => ({ ...v, start: dates?.[0]?.toISOString() || '', end: dates?.[1]?.toISOString() || '' }));
                          setPage(1);
                          setSelected([]);
                        }}
                      />
                    </div>
                    <div className='archive-extra-filter-footer'>
                      <Button
                        type='link'
                        onClick={() => {
                          setFilter({});
                          setPage(1);
                          setSelected([]);
                        }}
                      >
                        清空筛选
                      </Button>
                      <Button type='primary' onClick={() => setMoreFiltersOpen(false)}>
                        完成
                      </Button>
                    </div>
                  </div>
                }
              >
                <Button icon={<FilterOutlined />} type={extraFilterCount ? 'dashed' : 'default'}>
                  更多筛选{extraFilterCount ? ` (${extraFilterCount})` : ''}
                </Button>
              </Popover>
              <Space className='archive-list-actions'>
                {selected.length > 0 && (
                  <>
                    <span className='archive-selection-count'>已选 {selected.length} 条</span>
                    <Button danger onClick={() => remove(data.filter((r) => selected.includes(r.id)))}>
                      批量删除
                    </Button>
                    <Button onClick={() => setSelected([])}>取消选择</Button>
                  </>
                )}
                {!trash && (
                  <Button type='primary' icon={<PlusOutlined />} onClick={() => setAdding(true)}>
                    新建档案
                  </Button>
                )}
                <Button
                  icon={<DeleteOutlined />}
                  onClick={() => {
                    setTrash(!trash);
                    setPage(1);
                    setSelected([]);
                  }}
                >
                  {trash ? '返回档案' : '回收站'}
                </Button>
              </Space>
            </div>
            {error && <Alert type='error' showIcon message={error} action={<Button onClick={load}>重试</Button>} className='mb-4' />}
            <EnhancedTable
              rowKey='id'
              size='small'
              columns={columns}
              dataSource={data}
              loading={loading}
              autoSortColumns={false}
              scroll={{ x: 'max-content' }}
              rowSelection={trash ? undefined : { selectedRowKeys: selected, onChange: setSelected }}
              pagination={{
                total,
                current: page,
                pageSize: size,
                showSizeChanger: true,
                showTotal: (v) => `共 ${v} 条`,
                onChange: (p, n) => {
                  setPage(p);
                  setSize(n);
                  setSelected([]);
                },
              }}
              locale={{ emptyText: trash ? '回收站为空' : '暂无档案，点击“新建档案”开始留档' }}
            />
            <MetadataForm
              category={category}
              visible={adding}
              onCancel={() => setAdding(false)}
              onSubmit={async (metadata) => {
                const a = await createArchive(category, metadata);
                history.push(`/archives/${a.id}`);
              }}
            />
          </div>
        </div>
      </PageLayout>
    </div>
  );
}
