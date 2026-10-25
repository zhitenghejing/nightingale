import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Descriptions, Empty, Input, Space, Spin, Tag, Tooltip, message } from 'antd';
import { AuditOutlined, DownloadOutlined, EditOutlined, FilePdfOutlined, FileWordOutlined, PlusOutlined, ReloadOutlined, ToolOutlined, ExportOutlined } from '@ant-design/icons';
import { useLocation } from 'react-router-dom';
import PageLayout from '@/components/pageLayout';
import ReportEditor from './ReportEditor';
import { filterReports, listReports, Report, reportBlob, ReportKind } from './api';
import './index.less';

export default function Reports() {
  const location = useLocation();
  const kind: ReportKind = location.pathname.endsWith('/incident') ? 'incident' : 'inspection';
  const title = kind === 'inspection' ? '巡检报告' : '故障报告';
  const icon = kind === 'inspection' ? <AuditOutlined /> : <ToolOutlined />;
  const [rows, setRows] = useState<Report[]>([]);
  const [selected, setSelected] = useState<string>();
  const [search, setSearch] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [editor, setEditor] = useState<'new' | 'edit'>();
  const [preview, setPreview] = useState('');
  const [previewError, setPreviewError] = useState('');
  const [previewLoading, setPreviewLoading] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const current = rows.find((r) => r.id === selected);
  const visibleRows = useMemo(() => filterReports(rows, search), [rows, search]);
  useEffect(() => {
    setRows([]);
    setSelected(undefined);
    setSearch('');
    setEditor(undefined);
  }, [kind]);
  const pending = rows.some((r) => r.previewStatus === 'pending');
  useEffect(() => {
    if (!pending) return;
    const timer = window.setInterval(() => setRefresh((x) => x + 1), 3000);
    return () => window.clearInterval(timer);
  }, [pending]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    listReports(kind, controller.signal)
      .then((data) => {
        setRows(data);
        setError('');
        setSelected((id) => (data.some((r) => r.id === id) ? id : data[0]?.id));
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(e instanceof Error ? e.message : '报告加载失败');
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [kind, refresh]);
  useEffect(() => {
    const controller = new AbortController();
    let url = '';
    setPreview('');
    setPreviewError('');
    setPreviewLoading(false);
    if (current?.previewStatus === 'ready') {
      setPreviewLoading(true);
      reportBlob(current, true, controller.signal)
        .then((blob) => {
          if (controller.signal.aborted) return;
          url = URL.createObjectURL(blob);
          setPreview(url);
        })
        .catch((e) => {
          if (!controller.signal.aborted) setPreviewError(e instanceof Error ? e.message : '预览加载失败');
        })
        .finally(() => {
          if (!controller.signal.aborted) setPreviewLoading(false);
        });
    }
    return () => {
      controller.abort();
      if (url) URL.revokeObjectURL(url);
    };
  }, [current?.id, current?.previewStatus]);
  const download = async () => {
    if (!current) return;
    setDownloading(true);
    try {
      const blob = await reportBlob(current, false);
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = current.filename;
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 10000);
    } catch (e) {
      message.error(e instanceof Error ? e.message : '下载失败');
    } finally {
      setDownloading(false);
    }
  };
  return (
    <PageLayout title={title} icon={icon}>
      <div className='reports-page flex flex-col h-full min-h-0'>
        {error && (
          <Alert
            type='error'
            message={error}
            showIcon
            className='mb-3'
            action={
              <Button size='small' onClick={() => setRefresh((v) => v + 1)}>
                重试
              </Button>
            }
          />
        )}
        <div className='reports-workspace flex flex-1 min-h-0'>
          <section className='reports-list flex flex-col min-h-0' aria-label='报告清单'>
            <div className='p-4 flex flex-col gap-3'>
              <div className='flex items-center justify-between'>
                <span className='font-medium text-base'>
                  {icon} {title}
                </span>
                <Tag>{rows.length} 份</Tag>
              </div>
              <Button type='primary' icon={<PlusOutlined />} block onClick={() => setEditor('new')}>
                上传{title}
              </Button>
              <div className='flex gap-2'>
                <Input.Search allowClear value={search} onChange={(e) => setSearch(e.target.value)} placeholder='搜索报告、工程师、摘要' />
                <Tooltip title='刷新清单'>
                  <Button aria-label='刷新报告' icon={<ReloadOutlined />} onClick={() => setRefresh((v) => v + 1)} />
                </Tooltip>
              </div>
            </div>
            <div className='reports-items flex-1 overflow-auto' aria-busy={loading}>
              {loading && !rows.length ? (
                <div className='p-8 text-center'>
                  <Spin />
                </div>
              ) : visibleRows.length ? (
                visibleRows.map((r) => (
                  <button type='button' key={r.id} className={`reports-row w-full text-left p-4 ${r.id === current?.id ? 'is-selected' : ''}`} onClick={() => setSelected(r.id)}>
                    <div className='flex items-start gap-3'>
                      <span className='reports-file-icon'>{r.extension === '.pdf' ? <FilePdfOutlined /> : <FileWordOutlined />}</span>
                      <div className='min-w-0 flex-1'>
                        <div className='font-medium break-words'>{r.title}</div>
                        <div className='reports-muted text-xs mt-2'>
                          {r.reportDate} · {r.engineer}
                        </div>
                      </div>
                    </div>
                    <p className='reports-excerpt text-xs mt-3 mb-0'>{r.summary}</p>
                  </button>
                ))
              ) : (
                <Empty className='mt-10' image={Empty.PRESENTED_IMAGE_SIMPLE} description={search ? '没有匹配的报告' : '上传第一份报告'} />
              )}
            </div>
            <div className='reports-muted text-xs p-4'>仅本人及管理员可见 · 原始附件独立归档</div>
          </section>
          <section className='reports-detail flex-1 min-w-0 overflow-auto' aria-label='报告详情与预览'>
            {current ? (
              <>
                <header className='p-5 flex items-start justify-between gap-4'>
                  <div className='min-w-0'>
                    <div className='reports-muted text-xs mb-2'>报告归档 / {title}</div>
                    <h2 className='text-xl font-medium mb-2 break-words'>{current.title}</h2>
                    <span className='reports-muted text-xs'>
                      {current.filename} · {(current.size / 1024 / 1024).toFixed(2)} MB
                    </span>
                  </div>
                  <Space wrap>
                    <Button icon={<EditOutlined />} onClick={() => setEditor('edit')}>
                      编辑信息
                    </Button>
                    <Button icon={<DownloadOutlined />} loading={downloading} onClick={download}>
                      下载原件
                    </Button>
                  </Space>
                </header>
                <div className='px-5 pb-4'>
                  <Descriptions size='small' column={{ xs: 1, sm: 2, lg: 3 }}>
                    <Descriptions.Item label='工程师'>{current.engineer}</Descriptions.Item>
                    <Descriptions.Item label='所属单位'>{current.company || '—'}</Descriptions.Item>
                    <Descriptions.Item label='联系方式'>{current.contact || '—'}</Descriptions.Item>
                    <Descriptions.Item label='报告日期'>{current.reportDate}</Descriptions.Item>
                    <Descriptions.Item label='归档时间'>{new Date(current.createdAt).toLocaleString()}</Descriptions.Item>
                    <Descriptions.Item label='文件状态'>
                      <Tag color='green'>原件已归档</Tag>
                    </Descriptions.Item>
                  </Descriptions>
                  <div className='reports-summary p-3 rounded'>
                    <div className='text-xs reports-muted mb-2'>文档简要内容</div>
                    <div className='whitespace-pre-wrap break-words'>{current.summary}</div>
                  </div>
                </div>
                <div className='reports-preview-bar px-5 py-3 flex justify-between items-center'>
                  <Space>
                    <FilePdfOutlined />
                    <span>文档预览</span>
                    {current.extension !== '.pdf' && <Tag>Word 转 PDF</Tag>}
                  </Space>
                  <Button
                    size='small'
                    icon={<ExportOutlined />}
                    disabled={!preview}
                    onClick={() => {
                      if (preview) window.open(preview, '_blank', 'noopener,noreferrer');
                    }}
                  >
                    新窗口打开
                  </Button>
                </div>
                <div className='reports-preview'>
                  {previewLoading ? (
                    <Spin tip='正在加载报告…' />
                  ) : preview ? (
                    <iframe title={`${current.title} 在线预览`} src={preview} />
                  ) : current.previewStatus === 'pending' ? (
                    <Spin tip='正在生成 Word 预览，完成后自动显示…' />
                  ) : (
                    <Alert type='warning' showIcon message={previewError || current.previewError || '预览暂不可用'} description='原始附件已保存，可使用“下载原件”在本地打开。' />
                  )}
                </div>
                <details className='reports-muted text-xs px-5 py-3'>
                  <summary>归档校验信息</summary>
                  <div className='break-all mt-2'>
                    SHA-256：{current.sha256}
                    <br />
                    报告编号：{current.id}
                  </div>
                </details>
              </>
            ) : (
              <div className='flex h-full items-center justify-center'>
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description='从中间清单选择报告，查看详情与文档预览' />
              </div>
            )}
          </section>
        </div>
        <ReportEditor
          open={!!editor}
          kind={kind}
          existing={editor === 'edit' ? current : undefined}
          onClose={() => setEditor(undefined)}
          onSaved={(saved) => {
            setRows((old) => [saved, ...old.filter((r) => r.id !== saved.id)]);
            setSelected(saved.id);
            setRefresh((v) => v + 1);
            message.success('报告已保存');
          }}
        />
      </div>
    </PageLayout>
  );
}
