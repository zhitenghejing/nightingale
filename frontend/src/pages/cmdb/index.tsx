import React, { useMemo, useState } from 'react';
import { Alert, Button, Checkbox, Modal, Popover, Space, Table, Tag, Upload, message } from 'antd';
import { DatabaseOutlined, DeleteOutlined, DownloadOutlined, PlusOutlined, SettingOutlined, UploadOutlined } from '@ant-design/icons';
import { ColumnsType } from 'antd/lib/table';
import PageLayout from '@/components/pageLayout';
import { Device, exportCsv, importCsv, warrantyStatus } from './model';
import './index.less';
import { api } from './api';
import useInventory from './useInventory';
import DeviceEditor from './DeviceEditor';
import Administration from './Administration';
import { attachMaterial } from './materialCatalog';
import InventoryFilters from './InventoryFilters';
import { emptyQuery, InventoryQuery, matchesInventoryQuery } from './inventoryQuery';

type Mode = 'add' | 'edit' | 'view';
interface Editor {
  mode: Mode;
  device?: Device;
}

function statusTag(text: string) {
  const color = ['在保', '在线'].includes(text) ? 'green' : ['已过保', '心跳中断'].includes(text) ? 'red' : text === '即将过保' ? 'orange' : undefined;
  return <Tag color={color}>{text}</Tag>;
}

export default function CMDB() {
  const { devices, catalog, access, error, loading, reload } = useInventory();
  const [filters, setFilters] = useState<InventoryQuery>(emptyQuery);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<React.Key[]>([]);
  const [editor, setEditor] = useState<Editor>();
  const today = new Date().toISOString().slice(0, 10);
  const filtered = useMemo(() => devices.filter((device) => matchesInventoryQuery(device, filters, today)), [devices, filters, today]);

  const openEditor = (mode: Mode, device?: Device) => setEditor({ mode, device });

  const columns: ColumnsType<Device> = [
    { title: '序号', key: 'number', width: 65, render: (_, __, index) => index + 1 },
    { title: '机房名称', dataIndex: 'room', width: 165 },
    { title: '机柜名称', dataIndex: 'rack', width: 115 },
    {
      title: '主机名',
      dataIndex: 'hostname',
      width: 185,
      render: (value, device) => (
        <Button type='link' className='cmdb-name-link' onClick={() => openEditor('view', device)}>
          {value || '未命名设备'}
        </Button>
      ),
    },
    { title: '厂商', dataIndex: 'vendor', width: 100 },
    { title: '序列号', dataIndex: 'serial', width: 160 },
    { title: '业务 IP', dataIndex: 'businessIp', width: 135, render: (value) => value || '—' },
    { title: '设备类型', dataIndex: 'type', width: 135 },
    { title: '型号', dataIndex: 'model', width: 200 },
    { title: '所属业务', dataIndex: 'business', width: 130 },
    { title: '备注', dataIndex: 'note', width: 180, ellipsis: true },
    { title: '过保时间', dataIndex: 'expire', width: 130, sorter: (a, b) => (a.expire || '').localeCompare(b.expire || ''), render: (value) => value || '—' },
    { title: '保修状态', key: 'warranty', width: 110, render: (_, device) => statusTag(warrantyStatus(device.expire, today)) },
    { title: '监控状态', key: 'monitor', width: 120, render: (_, device) => statusTag(device.monitored ? device.monitorState : '未接入') },
    {
      title: '操作',
      key: 'actions',
      width: 125,
      fixed: 'right',
      render: (_, device) => (
        <Space>
          <Button type='link' size='small' onClick={() => openEditor('view', device)}>
            查看
          </Button>
          <Button type='link' size='small' disabled={!access?.permissions.edit} onClick={() => openEditor('edit', device)}>
            编辑
          </Button>
        </Space>
      ),
    },
  ];
  const [visibleColumns, setVisibleColumns] = useState<string[]>(columns.map((column) => String(column.key || ('dataIndex' in column ? column.dataIndex : ''))));
  const download = async (template: boolean) => {
    if (!template) {
      try {
        await api('export');
      } catch (e) {
        message.error(e instanceof Error ? e.message : '导出失败');
        return;
      }
    }
    const url = URL.createObjectURL(new Blob([exportCsv(template ? [] : filtered)], { type: 'text/csv;charset=utf-8' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = template ? '设备档案导入模板.csv' : '设备档案.csv';
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };

  return (
    <PageLayout title='CMDB' icon={<DatabaseOutlined />}>
      <div className='cmdb-page p-4'>
        {error && <Alert type='error' showIcon message={error} action={<Button onClick={() => void reload().catch(() => {})}>重试</Button>} className='mb-4' />}
        <div className='flex flex-wrap items-center justify-between gap-3 mb-4'>
          <div>
            <h2 className='m-0 text-lg font-medium'>设备档案</h2>
            <span className='text-fc-text-4'>机房、硬件、维保与监控信息统一查询</span>
          </div>
          <Tag>设备档案总表</Tag>
        </div>
        <Alert
          className='mb-4'
          type='info'
          showIcon
          message={
            <Space wrap>
              <span>设备到期提醒</span>
              <span>
                即将过保 <strong>{devices.filter((device) => warrantyStatus(device.expire, today) === '即将过保').length}</strong> 台
              </span>
              <span>
                已过保 <strong>{devices.filter((device) => warrantyStatus(device.expire, today) === '已过保').length}</strong> 台
              </span>
              <span>统计日 {today} · 提前 30 天提醒</span>
            </Space>
          }
        />
        <InventoryFilters
          value={filters}
          onChange={(query) => {
            setFilters(query);
            setSelected([]);
            setPage(1);
          }}
        />
        <div className='cmdb-surface'>
          <div className='flex flex-wrap items-center justify-between gap-3 p-4'>
            <Space wrap>
              <Button type='primary' icon={<PlusOutlined />} disabled={!access?.permissions.create} onClick={() => openEditor('add')}>
                添加设备
              </Button>
              <Button icon={<DownloadOutlined />} onClick={() => download(true)}>
                模板下载
              </Button>
              <Upload
                accept='.csv,text/csv'
                showUploadList={false}
                beforeUpload={async (file) => {
                  if (file.size > 5 * 1024 * 1024) {
                    message.error('导入文件不能超过 5 MB');
                    return false;
                  }
                  try {
                    const incoming = importCsv(await file.text(), devices);
                    await api(
                      'import',
                      'POST',
                      incoming.map((device, index) => {
                        try {
                          return attachMaterial(device);
                        } catch (e) {
                          throw new Error(`第 ${index + 2} 行：${e instanceof Error ? e.message : '素材匹配失败'}`);
                        }
                      }),
                    );
                    await reload();
                    message.success(`已导入 ${incoming.length} 台设备`);
                  } catch (error) {
                    message.error(error instanceof Error ? error.message : '导入失败');
                  }
                  return false;
                }}
              >
                <Button disabled={!access?.permissions.import} icon={<UploadOutlined />}>
                  数据导入
                </Button>
              </Upload>
              <Button icon={<DownloadOutlined />} disabled={!access?.permissions.export} onClick={() => download(false)}>
                数据导出
              </Button>
              <Button
                disabled={!selected.length || !access?.permissions.delete}
                icon={<DeleteOutlined />}
                onClick={() =>
                  Modal.confirm({
                    title: `删除所选 ${selected.length} 台设备？`,
                    content: '设备移入回收站，U 位将释放；可从回收站恢复。',
                    okText: '确认删除',
                    cancelText: '取消',
                    onOk: async () => {
                      await api(
                        'delete',
                        'POST',
                        devices.filter((d) => selected.includes(d.id)),
                      );
                      await reload();
                      setSelected([]);
                    },
                  })
                }
              >
                删除所选
              </Button>
            </Space>
            <Popover
              trigger='click'
              placement='bottomRight'
              title='显示字段'
              content={
                <div className='cmdb-column-options'>
                  <Checkbox.Group value={visibleColumns} onChange={(values) => setVisibleColumns(values.map(String))}>
                    {columns
                      .filter((column) => column.key !== 'actions')
                      .map((column) => {
                        const key = String(column.key || ('dataIndex' in column ? column.dataIndex : ''));
                        return (
                          <div key={key}>
                            <Checkbox value={key}>{String(column.title)}</Checkbox>
                          </div>
                        );
                      })}
                  </Checkbox.Group>
                </div>
              }
            >
              <Button icon={<SettingOutlined />}>显示字段</Button>
            </Popover>
          </div>
          <Table
            className='fc-enhanced-table'
            rowKey='id'
            size='small'
            columns={columns.filter((column) => column.key === 'actions' || visibleColumns.includes(String(column.key || ('dataIndex' in column ? column.dataIndex : ''))))}
            loading={loading}
            dataSource={filtered}
            scroll={{ x: 'max-content' }}
            rowSelection={{ selectedRowKeys: selected, onChange: setSelected }}
            pagination={{ current: page, onChange: setPage, defaultPageSize: 20, showSizeChanger: true, showTotal: (total) => `共 ${total} 台 · 已选择 ${selected.length} 台` }}
          />
        </div>
        <p className='cmdb-preview-note mt-3 mb-0'>设备档案与机柜图共用数据，保存后同步更新。监控和报警开关目前仅保存管理标记，不触发采集或通知。</p>
        <DeviceEditor
          editor={editor}
          devices={devices}
          catalog={catalog}
          onClose={() => setEditor(undefined)}
          onSaved={reload}
          canEdit={!!access?.permissions.edit}
          canDelete={!!access?.permissions.delete}
        />
        <Administration access={access} onChanged={reload} />
      </div>
    </PageLayout>
  );
}
