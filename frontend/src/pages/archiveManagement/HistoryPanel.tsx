import React, { useEffect, useState } from 'react';
import { Alert, Button, Drawer, Modal, Space, Table, Tag, message } from 'antd';
import { Link } from 'react-router-dom';
import { Archive, Log, Version, fmtDate } from './types';
import { api, errorText, getDocument, logs, versions } from './services';
import { fieldLabels, getFieldLabel } from './MetadataForm';
export const actions: Record<string, string> = {
  create: '新建档案',
  update: '修改档案信息',
  update_fields: '修改档案信息',
  restore_document_version: '恢复文档版本',
  archive: '归档',
  delete: '移入回收站',
  restore: '恢复档案',
  upload: '上传 / 替换文件',
  create_document: '新建在线文档',
  document_version: '文档版本',
  save_document: '保存文档',
  delete_document: '移除文档',
  restore_version: '恢复档案版本',
  restore_document: '恢复文档版本',
  export_requested: '发起导出',
  download_original: '下载原文件',
  download_export: '下载导出文件',
  print_requested: '发起打印',
  update_document: '修改文档信息',
};
function describe(value?: string) {
  if (!value) return '';
  const known = {
    'Created online document': '新建在线文档',
    'Saved document': '保存文档',
    'Restored historical version': '恢复历史版本',
    'Restored archive snapshot': '恢复档案快照',
  };
  return known[value] || value.replace(/^Uploaded /, '上传文件：');
}
export function plainText(body: any): string {
  if (!body) return '';
  if (body.annotations) return body.annotations.map((a: any) => `第${a.page}页 ${a.type} ${a.text || ''}`).join('\n');
  const walk = (v: any): string => {
    if (Array.isArray(v)) return v.map(walk).join('');
    if (!v || typeof v !== 'object') return '';
    if (v.type === 'image') return `[图片 ${v.value}]`;
    if (v.type === 'table') return '\n' + (v.trList || []).map((tr: any) => tr.tdList.map((td: any) => walk(td.value)).join(' | ')).join('\n') + '\n';
    if (v.valueList) return walk(v.valueList);
    return String(v.value || '').replace(/\u200b/g, '') + (v.type === 'title' || v.type === 'list' ? '\n' : '');
  };
  return walk(body.data?.main);
}
function LogChanges({ log }: { log: Log }) {
  const detail = log.detail || {};
  const before = detail.before || {};
  const after = detail.after || {};
  const oldValues = { ...(before.metadata || {}), state: before.state, title: before.title, purpose: before.purpose };
  const newValues = { ...(after.metadata || {}), state: after.state, title: after.title, purpose: after.purpose };
  const labels = { ...fieldLabels, state: '文档状态', purpose: '文档用途' };
  const display = (v: any) =>
    v === undefined || v === null || v === ''
      ? '—'
      : Array.isArray(v)
      ? v.join('、')
      : {
          draft: '草稿',
          archived: '已归档',
          normal: '正常',
          abnormal: '异常',
          training_material: '培训讲义',
          attendance: '参会人员签到',
          inspection_report: '巡检报告',
          incident_report: '故障报告',
          other: '其他资料',
        }[v] || String(v);
  const changes = Object.keys(labels)
    .filter((key) => JSON.stringify(oldValues[key]) !== JSON.stringify(newValues[key]))
    .map((key) => ({ key, label: labels[key], before: display(oldValues[key]), after: display(newValues[key]) }));
  return (
    <div className='mt-4'>
      {changes.length > 0 && (
        <Table
          rowKey='key'
          size='small'
          pagination={false}
          dataSource={changes}
          columns={[
            { title: '字段', dataIndex: 'label' },
            { title: '修改前', dataIndex: 'before' },
            { title: '修改后', dataIndex: 'after' },
          ]}
        />
      )}
      <Space direction='vertical'>
        {before.revision && after.revision && (
          <span>
            档案版本：v{before.revision} → v{after.revision}
          </span>
        )}
        {detail.version && <span>文档版本：v{detail.version}</span>}
        {detail.description && <span>{describe(detail.description)}</span>}
        {detail.format && <span>导出格式：{detail.format.toUpperCase()}</span>}
        {!changes.length && !detail.version && !detail.description && !detail.format && <span>此操作已记录。</span>}
      </Space>
    </div>
  );
}
export default function HistoryPanel({
  archive,
  documentID,
  kind,
  visible,
  onClose,
  onRestored,
}: {
  archive: Archive;
  documentID?: number;
  kind: 'versions' | 'logs';
  visible: boolean;
  onClose: () => void;
  onRestored: () => void;
}) {
  const [rows, setRows] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState<React.Key[]>([]);
  const [compare, setCompare] = useState<{ a: string; b: string; title: string }>();
  const load = async (offset = 0) => {
    setLoading(true);
    try {
      const res: any = kind === 'logs' ? await logs(archive.id, offset) : await versions(documentID || archive.id, !!documentID, offset);
      const next = Array.isArray(res) ? res : res.list;
      setRows((old) => (offset ? [...old, ...next] : next));
      setTotal(Array.isArray(res) ? offset + next.length + (next.length === 100 ? 1 : 0) : res.total);
      setError('');
    } catch (e) {
      setError(errorText(e));
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    if (visible) {
      setRows([]);
      setSelected([]);
      load();
    }
  }, [visible, kind, documentID]);
  const restore = (v: Version) =>
    Modal.confirm({
      title: `恢复到版本 ${v.sequence}？`,
      content: '恢复结果会成为新的当前版本，现有历史继续保留。',
      onOk: async () => {
        try {
          await api(documentID ? `/documents/${documentID}/versions/${v.id}/restore` : `/${archive.id}/versions/${v.id}/restore`, 'POST', { revision: archive.revision });
          message.success('已恢复并生成新版本');
          onRestored();
          onClose();
        } catch (e) {
          message.error(errorText(e));
          throw e;
        }
      },
    });
  const compareVersions = async () => {
    try {
      const [a, b] = selected.map((id) => rows.find((v) => v.id === id) as Version).sort((x, y) => x.sequence - y.sequence);
      if (documentID) {
        const [da, db] = await Promise.all([getDocument(documentID, a.id), getDocument(documentID, b.id)]);
        setCompare({ title: `版本 ${a.sequence} → ${b.sequence}`, a: plainText(da.version.body) || da.file?.name || '', b: plainText(db.version.body) || db.file?.name || '' });
      } else {
        const readable = (v: Version) => {
          const snap = v.snapshot!;
          const r = snap.record;
          return [
            `状态：${r.state === 'archived' ? '已归档' : '草稿'}`,
            ...Object.entries(r.metadata)
              .filter(([, x]) => x !== '' && x !== null)
              .map(([k, x]) => `${getFieldLabel(archive.category, k)}：${Array.isArray(x) ? x.join('、') : x}`),
            '文档：',
            ...snap.documents.map((x) => `${x.document.title} · 文档版本ID ${x.version_id}`),
          ].join('\n');
        };
        setCompare({ title: `版本 ${a.sequence} → ${b.sequence}`, a: readable(a), b: readable(b) });
      }
    } catch (e) {
      message.error(errorText(e));
    }
  };
  const columns =
    kind === 'logs'
      ? [
          { title: '时间', dataIndex: 'create_at', render: fmtDate, width: 180 },
          { title: '操作人', render: (_: any, r: Log) => `${r.actor_name} (${r.actor_username})`, width: 160 },
          { title: '操作', dataIndex: 'action', render: (v: string) => actions[v] || v },
          {
            title: '详情',
            render: (_: any, r: Log) => (
              <span className='archive-log-detail'>
                {r.document_id > 0 && `文档 #${r.document_id} · `}
                {describe(r.detail?.description) || (r.detail?.version && `版本 ${r.detail.version}`) || r.detail?.format || ''}
                <Button
                  type='link'
                  size='small'
                  onClick={() =>
                    Modal.info({
                      title: actions[r.action] || r.action,
                      width: 800,
                      content: (
                        <div>
                          <div>
                            {r.actor_name} · {fmtDate(r.create_at)}
                          </div>
                          <LogChanges log={r} />
                        </div>
                      ),
                    })
                  }
                >
                  查看变更
                </Button>
              </span>
            ),
          },
        ]
      : [
          { title: '版本', dataIndex: 'sequence', width: 70, render: (v: number) => <Tag>v{v}</Tag> },
          { title: '操作 / 说明', render: (_: any, r: Version) => describe(r.description) || actions[r.action || ''] || r.action },
          { title: '操作人', dataIndex: 'create_name' },
          { title: '时间', dataIndex: 'create_at', render: fmtDate, width: 180 },
          {
            title: '操作',
            render: (_: any, v: Version) => (
              <Space>
                {documentID && (
                  <Link to={`/archives/${archive.id}/documents/${documentID}?version=${v.id}`} target='_blank'>
                    阅读
                  </Link>
                )}
                <Button type='link' size='small' onClick={() => restore(v)}>
                  恢复
                </Button>
              </Space>
            ),
          },
        ];
  return (
    <>
      <Drawer title={kind === 'logs' ? '人员操作日志' : documentID ? '文档版本历史' : '档案版本历史'} width={960} visible={visible} onClose={onClose} destroyOnClose>
        {error && <Alert type='error' message={error} className='mb-4' />}
        {kind === 'versions' && (
          <div className='mb-4'>
            <Space>
              <Button disabled={selected.length !== 2} onClick={compareVersions}>
                比较所选两个版本
              </Button>
              <span className='archive-hint'>正文对照显示内容，复杂版式请分别打开版本阅读。</span>
            </Space>
          </div>
        )}
        <Table
          rowKey='id'
          size='small'
          columns={columns}
          dataSource={rows}
          loading={loading}
          pagination={false}
          rowSelection={kind === 'versions' ? { selectedRowKeys: selected, onChange: (keys) => setSelected(keys.slice(-2)) } : undefined}
        />
        {rows.length < total && (
          <Button className='mt-4' onClick={() => load(rows.length)} loading={loading}>
            加载更多
          </Button>
        )}
      </Drawer>
      <Modal title={compare?.title} visible={!!compare} onCancel={() => setCompare(undefined)} footer={null} width={1100}>
        <div className='archive-compare'>
          {compare &&
            [compare.a, compare.b].map((text, i) => (
              <div key={i}>
                <strong>{i ? '较新版本' : '较早版本'}</strong>
                <pre>
                  {text.split('\n').map((line, n) => (
                    <div key={n} className={(i ? compare.a : compare.b).split('\n').includes(line) ? '' : 'archive-diff-line'}>
                      {line || ' '}
                    </div>
                  ))}
                </pre>
              </div>
            ))}
        </div>
      </Modal>
    </>
  );
}
