import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Descriptions, Dropdown, Empty, Form, Grid, Input, Menu, Modal, Select, Space, Spin, Tag, Tooltip, Upload, message } from 'antd';
import {
  PrinterOutlined,
  FolderOpenOutlined,
  UploadOutlined,
  FileAddOutlined,
  EditOutlined,
  HistoryOutlined,
  DownloadOutlined,
  DownOutlined,
  FileTextOutlined,
  PaperClipOutlined,
  InfoCircleOutlined,
} from '@ant-design/icons';
import { Link, useHistory, useParams } from 'react-router-dom';
import PageLayout from '@/components/pageLayout';
import EnhancedTable from '@/components/EnhancedTable';
import { Archive, ArchiveDocument, categories, emptyBody, fmtBytes, fmtDate, purposes } from './types';
import { acquire, api, downloadBlob, errorText, exportDocument, fileBlob, getArchive, release, upload } from './services';
import MetadataForm, { categoryFields, getFieldLabel } from './MetadataForm';
import HistoryPanel from './HistoryPanel';
export default function Detail() {
  const { id } = useParams<{ id: string }>();
  const history = useHistory();
  const [a, setA] = useState<Archive>();
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<Archive>();
  const [busy, setBusy] = useState(false);
  const [purpose, setPurpose] = useState('');
  const [newDoc, setNewDoc] = useState(false);
  const [form] = Form.useForm();
  const [panel, setPanel] = useState<{ kind: 'versions' | 'logs'; documentID?: number }>();
  const [infoCollapsed, setInfoCollapsed] = useState(false);
  const screens = Grid.useBreakpoint();
  const infoColumns = screens.xl ? 3 : screens.md ? 2 : 1;
  const load = useCallback(async () => {
    try {
      const r = await getArchive(Number(id));
      setA(r);
      setPurpose((p) => p || purposes[r.category][0].value);
      setError('');
    } catch (e) {
      setError(errorText(e));
    }
  }, [id]);
  useEffect(() => {
    setEditing(undefined);
    setPanel(undefined);
    setInfoCollapsed(false);
    load();
  }, [load]);
  const perform = async (fn: () => Promise<any>, success?: string) => {
    setBusy(true);
    try {
      await fn();
      if (success) message.success(success);
      await load();
    } catch (e) {
      message.error(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  if (!a)
    return (
      <PageLayout title='档案管理' showBack backPath='/archives'>
        <div className='n9e'>{error ? <Alert type='error' message={error} action={<Button onClick={load}>重试</Button>} /> : <Spin />}</div>
      </PageLayout>
    );
  const openDoc = (d: ArchiveDocument, edit = false) => history.push(`/archives/${a.id}/documents/${d.id}${edit ? '?edit=true' : ''}`);
  const removeDoc = (d: ArchiveDocument) =>
    Modal.confirm({
      title: `移除“${d.title}”？`,
      content: '历史版本仍然保留。归档所需资料不足时，操作会被阻止。',
      onOk: () => perform(() => api(`/${a.id}/documents/${d.id}`, 'DELETE')),
    });
  const replaceFile = async (d: ArchiveDocument, file: File) => {
    const session = await acquire(d.id);
    try {
      await upload(a.id, file, d.purpose, d.id, session.lease);
    } finally {
      await release(d.id, session.lease).catch(() => undefined);
    }
  };
  const download = (d: ArchiveDocument) =>
    perform(async () => {
      if (!d.file) return;
      await downloadBlob(await fileBlob(d.file.id, true), d.file.name);
    });
  const editDocument = (d: ArchiveDocument) => {
    let title = d.title;
    let nextPurpose = d.purpose;
    Modal.confirm({
      title: '文档信息',
      okText: '保存',
      cancelText: '取消',
      content: (
        <Space direction='vertical' style={{ width: '100%' }}>
          <Input
            defaultValue={d.title}
            maxLength={255}
            onChange={(e) => {
              title = e.target.value;
            }}
          />
          <Select
            defaultValue={d.purpose}
            options={purposes[a.category].filter((p) => d.kind !== 'online' || p.value !== 'attendance')}
            style={{ width: '100%' }}
            onChange={(v) => {
              nextPurpose = v;
            }}
          />
        </Space>
      ),
      onOk: async () => {
        await api(`/${a.id}/documents/${d.id}`, 'PUT', { revision: a.revision, title, purpose: nextPurpose });
        await load();
      },
    });
  };
  const printInformation = () =>
    perform(async () => {
      const popup = window.open('', '_blank');
      try {
        const { blob, job } = await exportDocument(a.id, 'information_pdf');
        await api(`/${a.id}/print`, 'POST', { version_id: (job as any).archive_version_id });
        const url = URL.createObjectURL(blob);
        if (popup) {
          popup.onload = () => popup.print();
          popup.location.href = url;
        } else message.info('请允许本站打开打印窗口');
        setTimeout(() => URL.revokeObjectURL(url), 120000);
      } catch (e) {
        popup?.close();
        throw e;
      }
    });
  const createOnline = async () => {
    const v = await form.validateFields();
    setBusy(true);
    try {
      const res = await api<Archive>(`/${a.id}/documents`, 'POST', { ...v, body: emptyBody() });
      const d = res.documents![res.documents!.length - 1];
      history.push(`/archives/${a.id}/documents/${d.id}?edit=true`);
    } catch (e) {
      message.error(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const humanValue = (key: string, v: any) => {
    if (v === undefined || v === null || v === '') return '—';
    if (['start_time', 'end_time', 'occurred_at', 'resolved_at'].includes(key)) return fmtDate(v);
    if (v === 'normal') return '正常';
    if (v === 'abnormal') return '异常';
    if (key === 'level') return ({ I: 'Ⅰ', II: 'Ⅱ', III: 'Ⅲ', IV: 'Ⅳ' }[v] || v) + '级';
    return Array.isArray(v) ? v.join('、') : String(v);
  };
  const metadataItems: { key: string; span: number }[] = [];
  let usedColumns = 0;
  categoryFields[a.category].forEach((key) => {
    const fullWidth = ['participants', 'training_content', 'training_effect', 'summary', 'scope'].includes(key);
    if (fullWidth && usedColumns) {
      metadataItems[metadataItems.length - 1].span += infoColumns - usedColumns;
      usedColumns = 0;
    }
    metadataItems.push({ key, span: fullWidth ? infoColumns : 1 });
    usedColumns = fullWidth ? 0 : (usedColumns + 1) % infoColumns;
  });
  if (usedColumns) metadataItems[metadataItems.length - 1].span += infoColumns - usedColumns;
  const materialRequirement =
    a.category === 'training'
      ? '归档前需提交培训讲义及签到文件。在线讲义可满足讲义要求，签到必须上传。'
      : a.category === 'inspection' || a.category === 'incident'
      ? '归档前至少提供一份对应报告，上传文件或在线编写均可。'
      : '归档前至少提供一份有内容的文档或上传资料。';
  return (
    <PageLayout title='档案管理' icon={<FolderOpenOutlined />} showBack backPath={`/archives?tab=${a.category}`}>
      <div className='n9e archive-page archive-detail-page'>
        {error && <Alert type='error' showIcon message={error} className='mb-4' />}
        <div className='archive-detail-heading'>
          <div className='archive-detail-heading-row'>
            <div className='archive-detail-title'>
              <h2 title={a.title}>{a.title}</h2>
              <Tag color={a.state === 'archived' ? 'green' : undefined}>{a.state === 'archived' ? '已归档' : '草稿'}</Tag>
              {a.deleted && <Tag color='red'>回收站</Tag>}
            </div>
            <Space className='archive-detail-actions' size={8} wrap>
              <Button icon={<HistoryOutlined />} onClick={() => setPanel({ kind: 'versions' })}>
                版本历史
              </Button>
              <Dropdown
                trigger={['click']}
                placement='bottomRight'
                overlay={
                  <Menu>
                    <Menu.Item key='logs' onClick={() => setPanel({ kind: 'logs' })}>
                      操作日志
                    </Menu.Item>
                    <Menu.Item key='print' icon={<PrinterOutlined />} disabled={busy} onClick={printInformation}>
                      打印档案信息
                    </Menu.Item>
                    <Menu.Item
                      key='export'
                      icon={<DownloadOutlined />}
                      disabled={busy}
                      onClick={() =>
                        perform(async () => {
                          const { blob, job } = await exportDocument(a.id, 'zip');
                          await downloadBlob(blob, job.file_name);
                        })
                      }
                    >
                      导出整份档案
                    </Menu.Item>
                  </Menu>
                }
              >
                <Button aria-label='更多档案操作'>
                  更多操作 <DownOutlined />
                </Button>
              </Dropdown>
              {a.deleted ? (
                <Button type='primary' onClick={() => perform(() => api(`/${a.id}/restore`, 'POST', { revision: a.revision }), '已恢复')}>
                  恢复档案
                </Button>
              ) : (
                <>
                  <Button icon={<EditOutlined />} onClick={() => setEditing(a)}>
                    编辑信息
                  </Button>
                  {a.state === 'draft' && (
                    <Button type='primary' loading={busy} onClick={() => perform(() => api(`/${a.id}/archive`, 'POST', { revision: a.revision }), '已归档')}>
                      归档
                    </Button>
                  )}
                </>
              )}
            </Space>
          </div>
          <div className='archive-detail-meta archive-hint'>
            <span>
              {categories.find((c) => c.key === a.category)?.label} · 档案 #{a.id}
            </span>
            <span>
              {a.update_name} 更新于 {fmtDate(a.update_at)}
            </span>
            <span>资料 {fmtBytes(a.current_bytes)} / 500 MB</span>
          </div>
        </div>
        <section className='archive-section'>
          <div className='archive-section-header archive-section-title'>
            <h3>
              <FileTextOutlined />
              基本信息
            </h3>
            <Button type='text' size='small' aria-expanded={!infoCollapsed} aria-controls='archive-basic-information' onClick={() => setInfoCollapsed(!infoCollapsed)}>
              {infoCollapsed ? '展开信息' : '收起信息'} <DownOutlined rotate={infoCollapsed ? 0 : 180} />
            </Button>
          </div>
          {!infoCollapsed && (
            <div className='archive-section-body' id='archive-basic-information'>
              <Descriptions bordered size='small' column={infoColumns}>
                {metadataItems.map(({ key, span }) => (
                  <Descriptions.Item key={key} label={getFieldLabel(a.category, key)} span={span}>
                    {humanValue(key, a.metadata[key])}
                  </Descriptions.Item>
                ))}
              </Descriptions>
              <div className='archive-information-footer archive-hint'>
                <span>创建时间：{fmtDate(a.create_at)}</span>
                <span>创建人：{a.create_name}</span>
              </div>
            </div>
          )}
        </section>
        <section className='archive-section archive-documents-section'>
          <div className='archive-section-header archive-section-title'>
            <div className='archive-section-heading'>
              <h3>
                <PaperClipOutlined />
                文档资料 <span className='archive-hint'>（{a.documents?.length || 0} / 20 份）</span>
                <Tooltip
                  trigger={['hover', 'focus', 'click']}
                  title={
                    <>
                      {materialRequirement}
                      <br />
                      支持 DOCX、PDF、PNG、JPG/JPEG；单份不超过 50 MB。
                    </>
                  }
                >
                  <Button type='text' size='small' icon={<InfoCircleOutlined />} aria-label='归档资料要求' />
                </Tooltip>
              </h3>
              <div className='archive-hint archive-material-hint'>
                {a.category === 'training'
                  ? '培训讲义及签到必交，签到须上传'
                  : a.category === 'inspection' || a.category === 'incident'
                  ? '报告必交，可上传或在线编写'
                  : '至少提供一份有内容的文档或上传资料'}
              </div>
            </div>
            {!a.deleted && (
              <Space className='archive-upload-actions' size={8}>
                <Select value={purpose} onChange={setPurpose} options={purposes[a.category]} style={{ width: 128 }} />
                <Upload
                  accept='.docx,.pdf,.png,.jpg,.jpeg'
                  multiple
                  showUploadList={false}
                  beforeUpload={(file) => {
                    if (file.size > 50 * 1048576) {
                      message.error('单份文件不能超过 50 MB');
                      return false;
                    }
                    perform(() => upload(a.id, file, purpose), '上传成功');
                    return false;
                  }}
                >
                  <Button icon={<UploadOutlined />} loading={busy}>
                    上传文件
                  </Button>
                </Upload>
                <Button
                  type='primary'
                  icon={<FileAddOutlined />}
                  onClick={() => {
                    form.resetFields();
                    form.setFieldsValue({ purpose: purpose === 'attendance' ? 'training_material' : purpose });
                    setNewDoc(true);
                  }}
                >
                  在线新建文档
                </Button>
              </Space>
            )}
          </div>
          <div className='archive-section-body'>
            <EnhancedTable
              className='archive-documents-table'
              rowKey='id'
              size='small'
              tableLayout='fixed'
              scroll={{ x: 900 }}
              pagination={false}
              dataSource={a.documents || []}
              actionColumn={{ width: 112 }}
              rowActions={(d) => ({
                inline: [
                  { key: 'read', text: '阅读', icon: 'view', onClick: () => openDoc(d) },
                  {
                    key: 'edit',
                    text: d.kind === 'docx' ? '转在线编辑' : d.kind === 'pdf' ? '批注' : d.kind === 'image' ? '查看' : '编辑',
                    icon: d.kind === 'image' ? 'open' : 'edit',
                    visible: !a.deleted,
                    onClick: () => openDoc(d, true),
                  },
                ],
                menu: [
                  {
                    key: 'replace',
                    visible: !a.deleted && d.kind !== 'online',
                    node: (
                      <Upload
                        accept='.docx,.pdf,.png,.jpg,.jpeg'
                        showUploadList={false}
                        beforeUpload={(f) => {
                          perform(() => replaceFile(d, f), '已替换并保留原版本');
                          return false;
                        }}
                      >
                        <Button type='link' className='fc-table-action-menu-btn' icon={<UploadOutlined className='fc-table-action-menu-icon' />}>
                          替换
                        </Button>
                      </Upload>
                    ),
                  },
                  { key: 'download', text: '下载原件', icon: 'offline', visible: !!d.file, onClick: () => download(d) },
                  { key: 'history', text: '历史', icon: 'refresh', onClick: () => setPanel({ kind: 'versions', documentID: d.id }) },
                  { key: 'settings', text: '设置', icon: 'settings', visible: !a.deleted, onClick: () => editDocument(d) },
                  { key: 'remove', text: '移除', icon: 'delete', visible: !a.deleted, danger: true, onClick: () => removeDoc(d) },
                ],
              })}
              locale={{ emptyText: <Empty description='暂无文档，请上传资料或在线新建' image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
              columns={[
                {
                  title: '文档名称',
                  dataIndex: 'title',
                  render: (v, d) => (
                    <Button type='link' size='small' className='archive-document-name' title={v} onClick={() => openDoc(d)}>
                      {v}
                    </Button>
                  ),
                },
                { title: '用途', dataIndex: 'purpose', width: 124, render: (v) => purposes[a.category].find((p) => p.value === v)?.label || v },
                { title: '格式', dataIndex: 'kind', width: 96, render: (v) => <Tag>{v === 'online' ? '在线文档' : v === 'image' ? '图片' : v.toUpperCase()}</Tag> },
                {
                  title: '主文档',
                  width: 110,
                  render: (_, d) =>
                    a.primary_document_id === d.id ? (
                      <Tag color='blue'>主文档</Tag>
                    ) : !a.deleted ? (
                      <Button
                        type='link'
                        size='small'
                        onClick={() => perform(() => api(`/${a.id}`, 'PUT', { revision: a.revision, metadata: a.metadata, primary_document_id: d.id }))}
                      >
                        设为主文档
                      </Button>
                    ) : (
                      '—'
                    ),
                },
                { title: '当前版本', width: 80, render: (_, d) => `v${d.version.sequence}` },
                { title: '大小', width: 80, render: (_, d) => fmtBytes(d.version.size) },
              ]}
            />
          </div>
        </section>
        <MetadataForm
          category={a.category}
          initial={editing?.metadata}
          visible={!!editing}
          onCancel={() => setEditing(undefined)}
          onSubmit={async (metadata) => {
            if (!editing) return;
            await api(`/${editing.id}`, 'PUT', { revision: editing.revision, metadata, primary_document_id: editing.primary_document_id });
            setEditing(undefined);
            await load();
          }}
        />
        <Modal
          title='在线新建文档'
          visible={newDoc}
          onCancel={() => setNewDoc(false)}
          onOk={createOnline}
          confirmLoading={busy}
          okText='创建并开始编写'
          cancelText='取消'
          destroyOnClose
        >
          <Form form={form} layout='vertical'>
            <Form.Item name='title' label='文档名称' rules={[{ required: true, whitespace: true, message: '请输入文档名称' }]}>
              <Input maxLength={255} />
            </Form.Item>
            <Form.Item name='purpose' label='文档用途' rules={[{ required: true }]}>
              <Select options={purposes[a.category].filter((p) => p.value !== 'attendance')} />
            </Form.Item>
          </Form>
        </Modal>
        {panel && <HistoryPanel archive={a} documentID={panel.documentID} kind={panel.kind} visible onClose={() => setPanel(undefined)} onRestored={load} />}
      </div>
    </PageLayout>
  );
}
