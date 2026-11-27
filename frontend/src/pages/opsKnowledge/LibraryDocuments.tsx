import EnvironmentRelations from '@/components/EnvironmentRelations';
import React, { useEffect, useRef, useState } from 'react';
import { Alert, Button, Drawer, Input, Space, Table, Tabs, Tag, message } from 'antd';
import { Link } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import request from '@/utils/request';

export type LibraryDocument = {
  id: number;
  document_id: number;
  archive_id: number;
  source_version_id: number;
  title: string;
  sequence: number;
  state: string;
  summary_state: string;
  error: string;
  markdown?: string;
  summary?: string;
};
export const libraryAPI = async <T,>(path = '', method = 'GET'): Promise<T> => {
  const r = await request('/api/n9e/knowledge-documents' + path, { method });
  if (r.err) throw new Error(r.err);
  return r.dat;
};
const names: Record<string, string> = { queued: '等待解析', ready: '原文可检索', retry: '等待重试', failed: '处理失败', awaiting_ocr: '待 OCR', empty: '空文档' };
export function LibraryStatus({ item }: { item?: LibraryDocument }) {
  if (!item) return <Tag>等待同步</Tag>;
  return (
    <Space wrap>
      <Tag color={item.state === 'ready' ? 'green' : 'orange'}>{names[item.state] || item.state}</Tag>
      {item.state === 'ready' && <Tag>{item.summary_state === 'ready' ? 'AI 已整理·待核实' : item.summary_state === 'failed' ? 'AI 整理失败' : 'AI 整理中'}</Tag>}
    </Space>
  );
}
export default function LibraryDocuments() {
  const [items, setItems] = useState<LibraryDocument[]>([]);
  const [error, setError] = useState('');
  const [reading, setReading] = useState<LibraryDocument>();
  const [query, setQuery] = useState('');
  const readGeneration = useRef(0);
  const readingId = useRef<number>();
  const [hits, setHits] = useState<{ id: number; title: string; heading: string; body: string; url: string; sequence: number }[]>();
  const load = async () => {
    try {
      const next = await libraryAPI<LibraryDocument[]>();
      setItems(next);
      setError('');
      if (readingId.current) {
        const id = readingId.current;
        const generation = readGeneration.current;
        try {
          const fresh = await libraryAPI<LibraryDocument>(`/${id}`);
          if (generation === readGeneration.current) setReading(fresh);
        } catch {
          if (generation === readGeneration.current) {
            readingId.current = undefined;
            setReading(undefined);
            message.warning('资料已换版、删除或暂不可读，请重新选择当前资料。');
          }
        }
      }
    } catch (e) {
      setError(String(e));
    }
  };
  useEffect(() => {
    load();
    const timer = setInterval(load, 10000);
    return () => clearInterval(timer);
  }, []);
  const read = async (id: number) => {
    const generation = ++readGeneration.current;
    readingId.current = id;
    try {
      const value = await libraryAPI<LibraryDocument>(`/${id}`);
      if (generation === readGeneration.current) setReading(value);
    } catch (e) {
      if (generation === readGeneration.current) message.error(String(e));
    }
  };
  return (
    <div className='ops-page'>
      <Space wrap style={{ marginBottom: 16 }}>
        <Link to='/archives'>到档案管理上传资料</Link>
        <Button onClick={load}>刷新</Button>
        <Input.Search
          placeholder='搜索资料内容'
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onSearch={async (q) => {
            try {
              setHits(await libraryAPI(`/search?q=${encodeURIComponent(q)}`));
            } catch (e) {
              message.error(String(e));
            }
          }}
          style={{ width: 320 }}
        />
      </Space>
      {error && <Alert type='error' message={error} />}
      {hits && (
        <div style={{ marginBottom: 16 }}>
          <Button type='link' onClick={() => setHits(undefined)}>
            返回资料列表
          </Button>
          {!hits.length && <Alert message='未找到相关资料' />}
          {hits.map((h) => (
            <div key={h.id} style={{ padding: 12, borderBottom: '1px solid #eee' }}>
              <Link to={h.url}>
                {h.title} · V{h.sequence} · {h.heading}
              </Link>
              <p style={{ whiteSpace: 'pre-wrap' }}>{h.body}</p>
            </div>
          ))}
        </div>
      )}
      <Table
        rowKey='id'
        dataSource={items}
        scroll={{ x: 850 }}
        columns={[
          {
            title: '资料名称',
            dataIndex: 'title',
            render: (v, r) => (
              <Button type='link' onClick={() => read(r.id)}>
                {v}
              </Button>
            ),
          },
          { title: '来源版本', render: (_, r) => <Link to={`/archives/${r.archive_id}/documents/${r.document_id}?version=${r.source_version_id}`}>档案文档 V{r.sequence}</Link> },
          { title: '处理状态', render: (_, r) => <LibraryStatus item={r} /> },
          { title: '说明', dataIndex: 'error' },
          {
            title: '操作',
            render: (_, r) => (
              <Button
                disabled={r.state === 'empty'}
                onClick={async () => {
                  try {
                    await libraryAPI(`/${r.id}/retry?mode=${r.summary_state === 'ready' ? 'regenerate' : 'unfinished'}`, 'POST');
                    message.success(r.summary_state === 'ready' ? '已提交重新生成，原文与来源版本保持不变' : '已提交重试，保留已完成章节');
                    load();
                  } catch (e) {
                    message.error(String(e));
                  }
                }}
              >
                {r.state === 'ready' && r.summary_state === 'ready' ? '重新生成整理' : '重试未完成项'}
              </Button>
            ),
          },
        ]}
      />
      <Drawer width='min(900px, 96vw)' title={reading ? `${reading.title} · V${reading.sequence}` : ''} visible={!!reading} onClose={() => { readGeneration.current += 1; readingId.current = undefined; setReading(undefined); }}>
        {reading && (
          <>
            <Space wrap><LibraryStatus item={reading} /><EnvironmentRelations objectRef={`document:${reading.id}`} /></Space>
            <Tabs defaultActiveKey='markdown'>
              <Tabs.TabPane tab='Markdown 原文' key='markdown'>
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{reading.markdown || '原文尚未解析，请查看处理状态。'}</ReactMarkdown>
              </Tabs.TabPane>
              <Tabs.TabPane tab='AI 整理（待核实）' key='summary'>
                <Alert type='info' showIcon message='整理内容是基于本版本原文生成的参考知识；覆盖进度见下方，不替代原文，也不代表设备当前状态。' style={{ marginBottom: 12 }} />
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{reading.summary || '尚未完成 AI 整理，原文可用后即可检索。'}</ReactMarkdown>
              </Tabs.TabPane>
            </Tabs>
          </>
        )}
      </Drawer>
    </div>
  );
}
