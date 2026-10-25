/**
 * Throwaway frontend-only prototype: three structural variants at
 * /prototype/archive-management/index.html?variant=A|B|C.
 * Uses main's theme and Ant Design components. All mutations stay in memory.
 */
import React, { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  Button,
  Checkbox,
  ConfigProvider,
  DatePicker,
  Descriptions,
  Divider,
  Drawer,
  Dropdown,
  Empty,
  Form,
  Input,
  InputNumber,
  Menu,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Timeline,
  Tooltip,
  Upload,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { UploadFile } from 'antd/es/upload/interface';
import zhCN from 'antd/es/locale/zh_CN';
import {
  AlignCenter,
  AlignLeft,
  AlignRight,
  Archive,
  ArrowDownToLine,
  ArrowLeft,
  ArrowRight,
  Bell,
  BookOpen,
  Bookmark,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  CircleHelp,
  Clock3,
  FileCheck2,
  FileText,
  FolderClosed,
  FolderOpen,
  Heading1,
  Highlighter,
  History,
  House,
  Image as ImageIcon,
  Italic,
  LayoutGrid,
  List,
  ListChecks,
  ListOrdered,
  Maximize2,
  MessageSquareText,
  MoreHorizontal,
  PanelLeftClose,
  PanelRight,
  Pencil,
  Plus,
  Printer,
  Redo2,
  RefreshCw,
  Save,
  Search,
  Settings,
  ShieldCheck,
  Sparkles,
  Table2,
  Trash2,
  Underline,
  Undo2,
  UploadCloud,
  Users,
  X,
  Bold,
} from 'lucide-react';
import { RowActionCell } from '../../components/EnhancedTable/RowActionCell';
import '../../components/EnhancedTable/style.less';
import 'antd/dist/antd.less';
import '../../theme/index.less';
import './prototype.less';
import { ArchiveRecord, Category, DocumentItem, Variant, categoryLabels, documentBody, records as initialRecords, variantNames } from './data';

const icon = (Icon: React.ComponentType<any>, size = 15) => <Icon size={size} strokeWidth={1.7} />;
const demo = (text = '此操作仅展示交互，不会上传、下载或保存真实资料') => message.info(text);
const kindName = { online: '在线文档', pdf: 'PDF', word: 'Word' };
const sourceLabels: Record<Category, string> = { training: '培训主题', inspection: '巡检名称', incident: '影响业务', other: '文档标题' };
const CATEGORY_KEYS: Category[] = ['training', 'inspection', 'incident', 'other'];
const VARIANTS: Variant[] = ['A', 'B', 'C'];
const initialParams = () => new URLSearchParams(window.location.search);

function Status({ value }: { value: string }) {
  return (
    <span className={`archive-status ${value === '异常' ? 'is-warning' : value === '草稿' ? 'is-draft' : 'is-success'}`}>
      <i />
      {value}
    </span>
  );
}
function DocIcon({ kind = 'online', large = false }: { kind?: DocumentItem['kind']; large?: boolean }) {
  return (
    <span className={`archive-doc-icon kind-${kind} ${large ? 'large' : ''}`}>{icon(kind === 'online' ? FileText : kind === 'pdf' ? FileCheck2 : FileText, large ? 24 : 16)}</span>
  );
}

function PrototypeSwitcher({ variant, change, category, count, view }: { variant: Variant; change: (v: Variant) => void; category: Category; count: number; view: string }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      if (e.defaultPrevented || target.closest('input,textarea,select,[contenteditable=true],.ant-select') || e.altKey || e.ctrlKey || e.metaKey) return;
      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        e.preventDefault();
        change(VARIANTS[(VARIANTS.indexOf(variant) + (e.key === 'ArrowRight' ? 1 : 2)) % 3]);
      }
    };
    window.addEventListener('keydown', onKey);
    console.info('[Archive UI prototype]', { variant, category, count, view, persistence: 'memory-only' });
    return () => window.removeEventListener('keydown', onKey);
  }, [variant, change, category, count, view]);
  if (!import.meta.env.DEV && !import.meta.env.VITE_ARCHIVE_UI_PROTOTYPE) return null;
  return (
    <div className='archive-prototype-switcher' aria-label='原型布局切换'>
      <span className='prototype-label'>界面原型</span>
      <button aria-label='上一个布局' onClick={() => change(VARIANTS[(VARIANTS.indexOf(variant) + 2) % 3])}>
        {icon(ChevronLeft)}
      </button>
      <div>
        <strong>
          {variant} · {variantNames[variant]}
        </strong>
        <span>
          {categoryLabels[category]} · {count} 条 · 刷新还原
        </span>
      </div>
      <button aria-label='下一个布局' onClick={() => change(VARIANTS[(VARIANTS.indexOf(variant) + 1) % 3])}>
        {icon(ChevronRight)}
      </button>
      <span className='prototype-dots'>
        {VARIANTS.map((v) => (
          <button key={v} className={v === variant ? 'selected' : ''} aria-label={`布局 ${v}`} onClick={() => change(v)} />
        ))}
      </span>
    </div>
  );
}

export function ArchiveManagementPrototype() {
  const [params, setParams] = useState(initialParams);
  const variant = VARIANTS.includes(params.get('variant') as Variant) ? (params.get('variant') as Variant) : 'A';
  const category = CATEGORY_KEYS.includes(params.get('tab') as Category) ? (params.get('tab') as Category) : 'training';
  const view = params.get('view') || 'list';
  const [records, setRecords] = useState<ArchiveRecord[]>(initialRecords);
  const [keyword, setKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [department, setDepartment] = useState<string | undefined>();
  const [selected, setSelected] = useState<React.Key[]>([]);
  const [activeId, setActiveId] = useState(initialRecords[0].id);
  const [drawer, setDrawer] = useState<'detail' | 'versions' | 'logs' | null>(null);
  const [formMode, setFormMode] = useState<'online' | 'upload' | 'metadata' | null>(null);
  const [files, setFiles] = useState<UploadFile[]>([]);
  const [form] = Form.useForm();
  const [compare, setCompare] = useState(false);
  const [notes, setNotes] = useState(true);
  const [bookmark, setBookmark] = useState(false);
  const [focusMode, setFocusMode] = useState(false);
  const [readingSearch, setReadingSearch] = useState('');
  const [zoom, setZoom] = useState(100);
  const [page, setPage] = useState(1);
  const [noteText, setNoteText] = useState('');
  const [annotations, setAnnotations] = useState([{ author: '王明', text: '建议在培训时补充现场操作的注意事项。', time: '10-22 14:30' }]);
  const [highlight, setHighlight] = useState(true);
  const [dirty, setDirty] = useState(false);
  const [saved, setSaved] = useState(false);
  const [editorTab, setEditorTab] = useState('开始');
  const [versionCount, setVersionCount] = useState(3);
  const [logs, setLogs] = useState([
    { action: '更新文档内容', detail: '补充操作要点与验证步骤 · V3.0', time: '2026-10-23 09:42', author: '王明' },
    { action: '归档档案', detail: '资料完整，保存归档版本 V2.0', time: '2026-10-22 17:08', author: '陈静' },
    { action: '上传培训签到资料', detail: '培训签到表.pdf · 1.8 MiB', time: '2026-10-22 16:35', author: '王明' },
    { action: '创建档案', detail: '新建业务信息和在线文档 · V1.0', time: '2026-10-22 09:10', author: '王明' },
  ]);
  const paperRef = useRef<HTMLDivElement>(null);
  const filtered = records.filter(
    (r) =>
      r.category === category &&
      (!keyword || `${r.title}${r.department}${r.person}${r.location}${r.summary}`.includes(keyword)) &&
      (statusFilter === 'all' || r.status === statusFilter || r.result === statusFilter) &&
      (!department || r.department === department),
  );
  const categoryRecords = records.filter((r) => r.category === category);
  const current = records.find((r) => r.id === (params.get('record') || activeId)) || categoryRecords[0] || records[0] || initialRecords[0];
  const document = current.documents.find((d) => d.id === params.get('doc')) || current.documents[0];
  const isPdf = document?.kind === 'pdf';

  function navigate(patch: Record<string, string | null>) {
    const next = new URLSearchParams(window.location.search);
    Object.entries(patch).forEach(([k, v]) => (v === null ? next.delete(k) : next.set(k, v)));
    window.history.replaceState(null, '', `${window.location.pathname}?${next}`);
    setParams(next);
  }
  useEffect(() => {
    window.document.body.classList.add('theme-light');
    const pop = () => setParams(initialParams());
    window.addEventListener('popstate', pop);
    return () => window.removeEventListener('popstate', pop);
  }, []);
  useEffect(() => {
    if (view === 'edit' && paperRef.current) paperRef.current.innerHTML = documentBody(current);
    setDirty(false);
    setSaved(false);
  }, [view, current.id]);

  function log(action: string, detail: string) {
    setLogs((old) => [{ action, detail, time: '刚刚 · 原型会话', author: '管理员' }, ...old]);
  }
  function openRecord(r: ArchiveRecord, target = 'read', doc?: DocumentItem) {
    setActiveId(r.id);
    setDrawer(null);
    setPage(1);
    const chosen = doc || (target === 'edit' ? r.documents.find((d) => d.kind !== 'pdf') : undefined) || r.documents[0];
    if (target === 'edit' && chosen?.kind === 'pdf') {
      target = 'read';
      setNotes(true);
      demo('PDF 使用阅读和批注模式，在线正文支持内容编辑');
    }
    navigate({ view: target, record: r.id, doc: chosen?.id || null });
  }
  function openDrawer(r: ArchiveRecord, target: typeof drawer) {
    setActiveId(r.id);
    navigate({ record: r.id });
    setDrawer(target);
  }
  function updateRecord(r: ArchiveRecord, patch: Partial<ArchiveRecord>) {
    setRecords((old) => old.map((item) => (item.id === r.id ? { ...item, ...patch } : item)));
  }
  function archiveRecord(r: ArchiveRecord) {
    updateRecord(r, { status: '已归档' });
    log('归档档案', `${r.title} · 演示操作`);
    message.success('已演示归档，刷新后还原');
  }
  function newRecord(mode: 'online' | 'upload') {
    setFiles([]);
    setFormMode(mode);
    form.resetFields();
    form.setFieldsValue({ status: '草稿', result: category === 'training' ? '正常' : undefined });
  }
  function editMetadata(r: ArchiveRecord) {
    setActiveId(r.id);
    navigate({ record: r.id });
    setFiles([]);
    setFormMode('metadata');
    form.setFieldsValue({
      ...r,
      ...(r.category === 'incident' ? { title: r.scope } : {}),
      ...(r.category === 'inspection' ? { scope: r.scope.split('、') } : {}),
      ...(r.category === 'other' ? { type: [r.type] } : {}),
    });
  }
  async function submitForm() {
    const values = await form.validateFields();
    if (formMode === 'metadata') {
      updateRecord(current, {
        ...values,
        ...(current.category === 'incident' ? { scope: values.title, title: `${values.title}故障报告` } : {}),
        ...(Array.isArray(values.scope) ? { scope: values.scope.join('、') } : {}),
        ...(Array.isArray(values.type) ? { type: values.type.join('、') } : {}),
      });
      log('修改业务信息', current.title);
      setFormMode(null);
      message.success('业务信息已在原型中更新');
      return;
    }
    const id = `demo-${Date.now()}`;
    const doc: DocumentItem = {
      id: `${id}-online`,
      name: `${values.title || '未命名文档'} · 在线文档`,
      kind: 'online',
      purpose: category === 'training' ? '培训讲义' : '正文',
      size: '在线文档',
    };
    const uploaded: DocumentItem[] = files.map((f, i) => ({
      id: `${id}-f${i}`,
      name: f.name,
      kind: /\.docx?$/i.test(f.name) ? 'word' : 'pdf',
      purpose: '附件',
      size: '未上传 · 演示',
    }));
    const r: ArchiveRecord = {
      ...initialRecords[0],
      ...values,
      id,
      category,
      title: values.title || '新建故障报告',
      date: '2026-10-23',
      updated: '刚刚',
      status: '草稿',
      scope: category === 'incident' ? values.title : Array.isArray(values.scope) ? values.scope.join('、') : values.scope || '',
      type: Array.isArray(values.type) ? values.type.join('、') : values.type || '',
      documents: formMode === 'online' || !uploaded.length ? [doc, ...uploaded] : uploaded,
      body: `<h1>${String(values.title || '未命名文档').replace(/[<>&]/g, '')}</h1><p><br></p>`,
    };
    setRecords((old) => [r, ...old]);
    setFormMode(null);
    setActiveId(id);
    log('新建档案', r.title);
    if (formMode === 'online') navigate({ view: 'edit', record: id, doc: doc.id });
    message.success('已加入原型数据，刷新后还原');
  }
  function saveDocument() {
    if (paperRef.current) updateRecord(current, { body: paperRef.current.innerHTML });
    setDirty(false);
    setSaved(true);
    setVersionCount((v) => v + 1);
    log('保存在线文档', `${document.name} · 新版本（演示）`);
    message.success('已演示保存，本次内容仅保留在当前页面');
  }
  function exec(command: string, value?: string) {
    paperRef.current?.focus();
    window.document.execCommand(command, false, value);
    setDirty(true);
    setSaved(false);
  }
  const documentList = (r: ArchiveRecord, compact = false) => (
    <div className={`archive-files ${compact ? 'compact' : ''}`}>
      {r.documents.map((d, i) => (
        <div key={d.id} className='archive-file-row'>
          <button className='archive-file-main' onClick={() => openRecord(r, 'read', d)}>
            <DocIcon kind={d.kind} />
            <span className='file-label'>
              <strong>{d.name}</strong>
              <span>
                {d.purpose} · {d.size}
                {i === 0 ? ' · 主文档' : ''}
              </span>
            </span>
          </button>
          <Dropdown
            trigger={['click']}
            overlay={
              <Menu
                items={[
                  { key: 'read', label: '阅读文档', icon: icon(BookOpen), onClick: () => openRecord(r, 'read', d) },
                  {
                    key: 'replace',
                    label: '替换文件',
                    icon: icon(UploadCloud),
                    onClick: () =>
                      Modal.confirm({
                        title: '替换文件 · 交互示意',
                        content: (
                          <>
                            <p>当前文件：{d.name}</p>
                            <Upload.Dragger beforeUpload={() => false}>
                              <p>点击选择新的文件（不会上传）</p>
                            </Upload.Dragger>
                            <p className='archive-muted'>替换后保留原文件版本，并记录操作人。</p>
                          </>
                        ),
                        okText: '演示替换',
                        onOk: () => {
                          log('替换附件', `${d.name}（演示）`);
                          demo('替换交互已演示，真实文件未修改');
                        },
                      }),
                  },
                  { key: 'download', label: '下载原件', icon: icon(ArrowDownToLine), onClick: () => demo() },
                ]}
              />
            }
          >
            <button className='archive-file-more' aria-label={`${d.name}的操作`}>
              {icon(MoreHorizontal)}
            </button>
          </Dropdown>
        </div>
      ))}
    </div>
  );
  const recordInfo = (r: ArchiveRecord) => (
    <Descriptions column={2} size='small' colon={false}>
      <Descriptions.Item label='档案状态'>
        <Status value={r.status} />
      </Descriptions.Item>
      <Descriptions.Item label={r.category === 'training' ? '培训讲师' : r.category === 'incident' ? '主要处理工程师' : '处理人'}>{r.person}</Descriptions.Item>
      <Descriptions.Item label={r.category === 'training' ? '培训部门' : '所属部门'}>{r.department}</Descriptions.Item>
      <Descriptions.Item label='业务日期'>{r.date}</Descriptions.Item>
      {r.location && <Descriptions.Item label={r.category === 'training' ? '培训地点' : '位置'}>{r.location}</Descriptions.Item>}
      {r.category === 'training' && <Descriptions.Item label='培训时长'>{r.duration} 小时</Descriptions.Item>}
      {r.category === 'inspection' && (
        <>
          <Descriptions.Item label='巡检结果'>
            <Status value={r.result} />
          </Descriptions.Item>
          <Descriptions.Item label='设备范围'>{r.scope}</Descriptions.Item>
        </>
      )}
      {r.category === 'incident' && (
        <>
          <Descriptions.Item label='故障等级'>{r.level}</Descriptions.Item>
          <Descriptions.Item label='影响业务'>{r.scope}</Descriptions.Item>
          <Descriptions.Item label='发生时间'>{r.date} 09:30</Descriptions.Item>
          <Descriptions.Item label='解决时间'>{r.date} 11:20</Descriptions.Item>
          <Descriptions.Item label='辅助处理工程师'>陈静</Descriptions.Item>
        </>
      )}
      {r.category === 'training' && (
        <Descriptions.Item label='参加人员' span={2}>
          张晓明、陈静、李文、赵晨
        </Descriptions.Item>
      )}
      <Descriptions.Item label={r.category === 'inspection' ? '巡检总结' : r.category === 'training' ? '培训内容' : '备注'} span={2}>
        {r.summary}
      </Descriptions.Item>
      {r.category === 'training' && (
        <Descriptions.Item label='培训效果' span={2}>
          完成现场操作与问题答疑，参与人员已掌握基本操作流程。
        </Descriptions.Item>
      )}
      <Descriptions.Item label='创建时间'>{r.date} 09:10</Descriptions.Item>
      <Descriptions.Item label='更新时间'>{r.updated}</Descriptions.Item>
    </Descriptions>
  );

  const columns: ColumnsType<ArchiveRecord> = [
    {
      title: sourceLabels[category],
      dataIndex: 'title',
      key: 'title',
      width: 300,
      render: (_, r) => (
        <button className='archive-title-link' onClick={() => openDrawer(r, 'detail')}>
          <span>{category === 'incident' ? r.scope : r.title}</span>
          <small>
            {r.id.toUpperCase()} · {r.documents.length} 份资料
          </small>
        </button>
      ),
    },
    ...(category === 'training'
      ? [
          { title: '培训状态', dataIndex: 'result', width: 100, render: (v: string) => <Status value={v} /> },
          { title: '培训时间', dataIndex: 'date', width: 128 },
          { title: '培训部门', dataIndex: 'department', width: 120 },
          { title: '培训地点', dataIndex: 'location', width: 175 },
          { title: '时长', dataIndex: 'duration', width: 72, render: (v: string) => `${v}h` },
          { title: '讲师', dataIndex: 'person', width: 78 },
        ]
      : category === 'inspection'
      ? [
          { title: '设备范围', dataIndex: 'scope', width: 150 },
          { title: '巡检结果', dataIndex: 'result', width: 100, render: (v: string) => <Status value={v} /> },
          { title: '处理人', dataIndex: 'person', width: 90 },
          { title: '巡检总结', dataIndex: 'summary', width: 260, ellipsis: true },
        ]
      : category === 'incident'
      ? [
          { title: '故障等级', dataIndex: 'level', width: 95 },
          { title: '发生时间', dataIndex: 'date', width: 130 },
          { title: '主要处理工程师', dataIndex: 'person', width: 140 },
          {
            title: '故障报告',
            key: 'report',
            width: 200,
            render: (_: unknown, r: ArchiveRecord) => (
              <button className='archive-text-button' onClick={() => openRecord(r)}>
                {icon(FileText, 14)}报告正文及附件
              </button>
            ),
          },
        ]
      : [
          { title: '文档类型', dataIndex: 'type', width: 120 },
          { title: '创建人', dataIndex: 'person', width: 100 },
          { title: '内容摘要', dataIndex: 'summary', width: 280, ellipsis: true },
        ]),
    { title: '档案状态', dataIndex: 'status', width: 100, render: (v) => <Status value={v} /> },
    { title: '更新时间', dataIndex: 'updated', width: 150, sorter: (a, b) => a.updated.localeCompare(b.updated) },
    {
      title: '操作',
      key: 'operate',
      fixed: 'right',
      width: 105,
      render: (_, r) => (
        <RowActionCell
          actions={{
            inline: [
              { key: 'read', text: '阅读', icon: 'view', onClick: () => openRecord(r) },
              { key: 'edit', text: '编辑信息', icon: 'edit', onClick: () => editMetadata(r) },
            ],
            menu: [
              { text: '在线编辑', icon: 'edit', onClick: () => openRecord(r, 'edit') },
              { text: '版本历史', icon: 'refresh', onClick: () => openDrawer(r, 'versions') },
              { text: '操作日志', icon: 'view', onClick: () => openDrawer(r, 'logs') },
              { text: '归档', icon: 'offline', onClick: () => archiveRecord(r) },
              { text: '导出档案', icon: 'offline', onClick: () => demo('导出交互示意：正式功能将生成整条档案 ZIP') },
              {
                text: '删除',
                icon: 'delete',
                danger: true,
                onClick: () =>
                  Modal.confirm({
                    title: '从原型列表中移除此档案？',
                    content: '仅改变当前页面的模拟数据，刷新后恢复。',
                    onOk: () => setRecords((old) => old.filter((item) => item.id !== r.id)),
                  }),
              },
            ],
          }}
        />
      ),
    },
  ];

  function VariantA() {
    return (
      <div className='archive-table-layout'>
        <Table<ArchiveRecord>
          className='fc-enhanced-table'
          rowKey='id'
          columns={columns}
          dataSource={filtered}
          rowSelection={{ selectedRowKeys: selected, onChange: setSelected }}
          scroll={{ x: category === 'training' ? 1430 : 1200 }}
          pagination={{ pageSize: 8, showSizeChanger: true, showQuickJumper: true, showTotal: (total) => `共 ${total} 条` }}
        />
      </div>
    );
  }
  function VariantB() {
    const chosen = filtered.find((r) => r.id === activeId) || filtered[0];
    return (
      <div className='archive-split-layout'>
        <div className='archive-master'>
          <div className='master-caption'>
            档案列表 <span>{filtered.length} 条</span>
          </div>
          {filtered.map((r) => (
            <button
              key={r.id}
              className={`master-record ${chosen?.id === r.id ? 'selected' : ''}`}
              onClick={() => {
                setActiveId(r.id);
                navigate({ record: r.id });
              }}
            >
              <span className='master-title'>{r.title}</span>
              <span className='master-meta'>
                {r.person} · {r.department}
              </span>
              <span className='master-bottom'>
                <Status value={r.status} />
                <span>{r.date}</span>
              </span>
            </button>
          ))}
        </div>
        <div className='archive-split-detail'>
          {chosen ? (
            <>
              <div className='split-detail-heading'>
                <div>
                  <div className='archive-overline'>
                    {categoryLabels[category]} / {chosen.id.toUpperCase()}
                  </div>
                  <h2>{chosen.title}</h2>
                </div>
                <Button type='primary' icon={icon(BookOpen)} onClick={() => openRecord(chosen)}>
                  打开阅读
                </Button>
              </div>
              <div className='detail-section'>
                <h3>
                  业务信息{' '}
                  <Button type='link' icon={icon(Pencil, 13)} onClick={() => editMetadata(chosen)}>
                    编辑
                  </Button>
                </h3>
                {recordInfo(chosen)}
              </div>
              <div className='detail-section'>
                <h3>
                  文档与附件 <span>{chosen.documents.length} 份</span>
                </h3>
                {documentList(chosen)}
              </div>
              <div className='detail-section'>
                <h3>
                  最近动态{' '}
                  <Button type='link' onClick={() => openDrawer(chosen, 'logs')}>
                    全部日志
                  </Button>
                </h3>
                <div className='archive-recent-log'>
                  <span className='small-avatar'>{chosen.person[0]}</span>
                  <div>
                    {chosen.person} 更新了在线文档<small>{chosen.updated} · V3.0</small>
                  </div>
                </div>
              </div>
            </>
          ) : (
            <Empty description='没有匹配的档案' />
          )}
        </div>
      </div>
    );
  }
  function VariantC() {
    return (
      <div className='archive-card-layout'>
        {filtered.map((r) => (
          <article className='archive-record-card' key={r.id}>
            <div className='card-top'>
              <DocIcon large />
              <Status value={r.status} />
            </div>
            <button className='card-title' onClick={() => openDrawer(r, 'detail')}>
              {r.title}
            </button>
            <p>{r.summary}</p>
            <div className='card-facts'>
              <span>
                {icon(Users, 13)}
                {r.person} · {r.department}
              </span>
              <span>
                {icon(Clock3, 13)}
                {r.date}
              </span>
            </div>
            <div className='card-files'>
              {r.documents.map((d) => (
                <span key={d.id}>
                  <i className={`file-kind-dot ${d.kind}`} />
                  {kindName[d.kind]}
                </span>
              ))}
            </div>
            <div className='card-footer'>
              <button onClick={() => openRecord(r)}>{icon(BookOpen, 14)}阅读文档</button>
              <Space>
                <Tooltip title='编辑信息'>
                  <Button type='text' icon={icon(Pencil)} onClick={() => editMetadata(r)} />
                </Tooltip>
                <Tooltip title='版本历史'>
                  <Button type='text' icon={icon(History)} onClick={() => openDrawer(r, 'versions')} />
                </Tooltip>
              </Space>
            </div>
          </article>
        ))}
      </div>
    );
  }

  const listView = (
    <>
      <header className='archive-page-header'>
        <div className='archive-heading'>
          {icon(Archive, 19)}
          <strong>档案管理</strong>
        </div>
        <div className='archive-header-actions'>
          <Tag>界面原型 · 模拟数据</Tag>
          <Button size='small' icon={icon(Sparkles, 13)} onClick={() => demo()}>
            Nightingale AI
          </Button>
          <Button size='small' icon={icon(BookOpen, 13)} onClick={() => demo('当前仅用于确认档案页面样式')}>
            产品文档
          </Button>
        </div>
      </header>
      <div className='archive-category-tabs' role='tablist'>
        {CATEGORY_KEYS.map((c) => (
          <button
            role='tab'
            aria-selected={category === c}
            key={c}
            className={category === c ? 'selected' : ''}
            onClick={() => {
              navigate({ tab: c, view: null, record: null, doc: null });
              setKeyword('');
              setStatusFilter('all');
              setDepartment(undefined);
              setSelected([]);
              const first = records.find((r) => r.category === c);
              if (first) setActiveId(first.id);
            }}
          >
            {categoryLabels[c]}
            <span>{records.filter((r) => r.category === c).length}</span>
          </button>
        ))}
      </div>
      <section className='archive-list-body'>
        <div className='archive-filter-row'>
          <Input
            prefix={icon(Search, 14)}
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            allowClear
            placeholder={`搜索${
              category === 'training' ? '培训主题、讲师' : category === 'inspection' ? '巡检名称、处理人' : category === 'incident' ? '影响业务、工程师' : '标题、内容'
            }…`}
            className='archive-search'
          />
          <Select
            aria-label='档案状态筛选'
            value={statusFilter}
            onChange={setStatusFilter}
            options={[
              { value: 'all', label: '全部档案状态' },
              { value: '已归档', label: '已归档' },
              { value: '草稿', label: '草稿' },
              ...(category === 'inspection' ? [{ value: '异常', label: '巡检异常' }] : []),
            ]}
          />
          <Select
            aria-label='部门筛选'
            placeholder={category === 'training' ? '培训部门' : '所属部门'}
            allowClear
            value={department}
            onChange={setDepartment}
            options={['信息技术部', '运维保障部', '综合管理部'].map((value) => ({ value, label: value }))}
          />
          <DatePicker.RangePicker placeholder={['开始日期', '结束日期']} onChange={() => demo('时间筛选样式演示，当前样例按固定日期展示')} />
          <Button
            icon={icon(RefreshCw)}
            aria-label='重置筛选'
            onClick={() => {
              setKeyword('');
              setStatusFilter('all');
              setDepartment(undefined);
            }}
          />
        </div>
        <div className='archive-list-toolbar'>
          <Space>
            <Dropdown
              overlay={
                <Menu
                  items={[
                    { key: 'upload', label: '填写信息并上传资料', icon: icon(UploadCloud), onClick: () => newRecord('upload') },
                    { key: 'online', label: '从空白在线编写', icon: icon(FileText), onClick: () => newRecord('online') },
                  ]}
                />
              }
              trigger={['click']}
            >
              <Button type='primary' icon={icon(Plus)}>
                新建档案 {icon(ChevronDown, 12)}
              </Button>
            </Dropdown>
            <Button icon={icon(FileText)} onClick={() => newRecord('online')}>
              在线新建
            </Button>
            <Dropdown
              overlay={
                <Menu
                  items={[
                    { key: 'export', label: '导出所选档案', onClick: () => demo() },
                    {
                      key: 'archive',
                      label: '归档所选',
                      onClick: () => {
                        setRecords((old) => old.map((r) => (selected.includes(r.id) ? { ...r, status: '已归档' } : r)));
                        message.success('已演示批量归档');
                      },
                    },
                    {
                      key: 'delete',
                      label: '删除所选',
                      danger: true,
                      onClick: () => {
                        if (!selected.length) return demo('请先选择档案');
                        setRecords((old) => old.filter((r) => !selected.includes(r.id)));
                        setSelected([]);
                        message.success('已从模拟列表移除，刷新后还原');
                      },
                    },
                  ]}
                />
              }
            >
              <Button>批量操作 {icon(ChevronDown, 12)}</Button>
            </Dropdown>
            {selected.length > 0 && <span className='archive-muted'>已选择 {selected.length} 条</span>}
          </Space>
          <span className='archive-muted'>
            共 {filtered.length} 条档案 <span className='toolbar-separator'>|</span> {variantNames[variant]}
          </span>
        </div>
        {filtered.length ? (
          variant === 'A' ? (
            <VariantA />
          ) : variant === 'B' ? (
            <VariantB />
          ) : (
            <VariantC />
          )
        ) : (
          <div className='archive-empty'>
            <Empty description='没有找到匹配的档案' />
            <Button
              onClick={() => {
                setKeyword('');
                setStatusFilter('all');
                setDepartment(undefined);
              }}
            >
              清空筛选
            </Button>
          </div>
        )}
        <div className='archive-list-footnote'>
          {icon(ShieldCheck, 13)}所有已登录账号均可使用 · 操作保留人员日志<span>当前为样式确认原型</span>
        </div>
      </section>
    </>
  );

  const documentHeader = (
    <header className='archive-document-header'>
      <div className='document-heading-left'>
        <Tooltip title='返回档案列表'>
          <Button type='text' icon={icon(ArrowLeft, 18)} onClick={() => navigate({ view: null, record: null, doc: null })} />
        </Tooltip>
        <span className='document-heading-divider' />
        <div>
          <div className='document-breadcrumb'>
            档案管理 / {categoryLabels[current.category]} / {view === 'edit' ? '在线编辑' : '阅读'}
          </div>
          <strong>{current.title}</strong>
        </div>
      </div>
      <Space>
        <Tag>界面原型</Tag>
        {view === 'edit' ? (
          <>
            <span className='editor-save-state'>{dirty ? '有未保存修改' : saved ? '刚刚保存 · 演示' : '修改仅在本页保留'}</span>
            <Button
              icon={icon(BookOpen)}
              onClick={() => {
                if (paperRef.current) updateRecord(current, { body: paperRef.current.innerHTML });
                navigate({ view: 'read' });
              }}
            >
              阅读预览
            </Button>
            <Button type='primary' icon={icon(Save)} onClick={saveDocument}>
              保存文档
            </Button>
          </>
        ) : (
          <>
            <Tooltip title='档案信息'>
              <Button icon={icon(PanelRight)} onClick={() => setDrawer('detail')} />
            </Tooltip>
            <Button icon={icon(History)} onClick={() => setDrawer('versions')}>
              版本
            </Button>
            <Dropdown
              overlay={
                <Menu
                  items={[
                    { key: 'pdf', label: '导出 PDF', onClick: () => demo() },
                    { key: 'word', label: '导出 Word', onClick: () => demo() },
                    { key: 'original', label: '下载上传原件', onClick: () => demo() },
                    { key: 'zip', label: '打包整个档案', onClick: () => demo() },
                  ]}
                />
              }
            >
              <Button icon={icon(ArrowDownToLine)}>下载 / 导出</Button>
            </Dropdown>
            <Button type='primary' icon={icon(Pencil)} onClick={() => (isPdf ? (setNotes(true), demo('PDF 阅读与批注交互示意，可在右侧添加备注')) : navigate({ view: 'edit' }))}>
              {isPdf ? '添加批注' : '在线编辑'}
            </Button>
          </>
        )}
      </Space>
    </header>
  );

  const reader = (
    <>
      {documentHeader}
      <div className='archive-reader-toolbar'>
        <Space>
          <DocIcon kind={document?.kind} />
          <span>{document?.name}</span>
          <Tag>{isPdf ? 'PDF 阅读示意' : '在线文档'}</Tag>
          <span className='archive-muted'>V{versionCount}.0</span>
        </Space>
        <Space size={6}>
          <Button type='text' icon={icon(ChevronLeft)} aria-label='上一页' onClick={() => setPage(1)} />
          <InputNumber min={1} max={2} value={page} onChange={(n) => setPage(n || 1)} size='small' />
          <span className='archive-muted'>/ 2</span>
          <Button type='text' icon={icon(ChevronRight)} aria-label='下一页' onClick={() => setPage(2)} />
          <Divider type='vertical' />
          <Select size='small' value={zoom} onChange={setZoom} options={[75, 90, 100, 125].map((n) => ({ value: n, label: `${n}%` }))} />
          <Tooltip title='适合宽度'>
            <Button type='text' icon={icon(Maximize2)} onClick={() => setZoom(90)} />
          </Tooltip>
          <Tooltip title={focusMode ? '退出全屏阅读' : '全屏阅读'}>
            <Button type='text' aria-label='切换全屏阅读' icon={icon(PanelLeftClose)} onClick={() => setFocusMode(!focusMode)} />
          </Tooltip>
          <Tooltip title='书签'>
            <Button
              className={bookmark ? 'is-active' : ''}
              type='text'
              icon={icon(Bookmark)}
              onClick={() => {
                setBookmark(!bookmark);
                message.success(bookmark ? '已移除原型书签' : '已添加原型书签');
              }}
            />
          </Tooltip>
          <Tooltip title='打印'>
            <Button type='text' icon={icon(Printer)} onClick={() => demo('打印按钮位置示意，原型不执行实际打印')} />
          </Tooltip>
          <Divider type='vertical' />
          <Button type='text' icon={icon(MessageSquareText)} onClick={() => setNotes(!notes)}>
            {isPdf ? '批注' : '文档信息'}
          </Button>
        </Space>
      </div>
      <div className='archive-reader-layout'>
        <aside className='archive-document-nav'>
          <div className='archive-reading-search'>
            <Input prefix={icon(Search, 13)} placeholder='在文档中查找…' allowClear value={readingSearch} onChange={(e) => setReadingSearch(e.target.value)} />
            {readingSearch && <small>示例内容匹配 {documentBody(current).split(readingSearch).length - 1} 处</small>}
          </div>
          <div className='document-nav-heading'>
            本档案文档 <span>{current.documents.length}</span>
          </div>
          {current.documents.map((d) => (
            <button
              key={d.id}
              className={`document-nav-file ${d.id === document.id ? 'selected' : ''}`}
              onClick={() => {
                navigate({ doc: d.id });
                setPage(1);
              }}
            >
              <DocIcon kind={d.kind} />
              <span>
                {d.name}
                <small>{d.purpose}</small>
              </span>
            </button>
          ))}
          <div className='document-nav-heading section-spacer'>{isPdf ? '页面导航' : '文档目录'}</div>
          {isPdf ? (
            <div className='archive-thumbnails'>
              {[1, 2].map((n) => (
                <button className={n === page ? 'selected' : ''} key={n} onClick={() => setPage(n)}>
                  <div className='mini-paper'>
                    <strong>{current.title}</strong>
                    {[0, 1, 2, 3, 4, 5].map((i) => (
                      <i key={i} />
                    ))}
                  </div>
                  <span>第 {n} 页</span>
                </button>
              ))}
            </div>
          ) : (
            ['一、培训目标', '二、内容与安排', '三、操作要点', '四、总结与后续'].map((text, i) => (
              <button
                className='archive-toc-row'
                key={text}
                onClick={() => window.document.querySelector(`#section-${['goal', 'plan', 'steps', 'result'][i]}`)?.scrollIntoView({ block: 'center', behavior: 'smooth' })}
              >
                {text}
              </button>
            ))
          )}
        </aside>
        <div className='archive-reader-canvas'>
          <div className='reader-mode-label'>
            {isPdf ? 'PDF · 页面阅读' : '专注阅读'}
            <span>
              {current.status} · 最后更新 {current.updated}
            </span>
          </div>
          <article className={`archive-paper ${highlight && isPdf ? 'show-highlight' : ''}`} style={{ zoom: zoom / 100 }}>
            <div className='paper-page-header'>NIGHTINGALE　/　{categoryLabels[current.category]}</div>
            <div
              className='paper-content'
              dangerouslySetInnerHTML={{
                __html:
                  page === 1
                    ? documentBody(current)
                    : `<div class="paper-kicker">附录 / 资料留档</div><h1>补充资料与执行确认</h1><p>${current.summary}</p><h2>检查清单</h2><ul><li>业务信息已核对</li><li>正式资料已整理</li><li>相关记录可追溯</li></ul><h2>补充说明</h2><p>此页用于展示多页阅读的页面样式。正式内容将在功能开发后由文档生成。</p>`,
              }}
            />
            <footer>
              {categoryLabels[current.category]} · {current.date}
              <span>第 {page} 页 / 共 2 页</span>
            </footer>
          </article>
          <div className='reader-end-caption'>文档阅读示意 · 点击上方“在线编辑”查看编写界面</div>
        </div>
        {notes && (
          <aside className='archive-annotations'>
            <div className='annotation-heading'>
              <strong>{isPdf ? '文档批注' : '阅读信息'}</strong>
              <Button type='text' size='small' aria-label='收起批注' icon={icon(X)} onClick={() => setNotes(false)} />
            </div>
            {isPdf ? (
              <>
                <div className='annotation-tools'>
                  <Button size='small' type={highlight ? 'primary' : 'default'} icon={icon(Highlighter, 13)} onClick={() => setHighlight(!highlight)}>
                    高亮
                  </Button>
                  <Button size='small' icon={icon(MessageSquareText, 13)} onClick={() => window.document.getElementById('prototype-note-input')?.focus()}>
                    文字备注
                  </Button>
                </div>
                {annotations.map((n, i) => (
                  <div className='annotation-note' key={`${n.time}-${i}`}>
                    <div>
                      <span className='small-avatar'>{n.author[0]}</span>
                      <strong>{n.author}</strong>
                      <small>{n.time}</small>
                    </div>
                    <span className='annotation-anchor'>第 1 页 · 操作要点</span>
                    <p>{n.text}</p>
                    <Button type='link' size='small' onClick={() => setAnnotations((old) => old.filter((_, index) => index !== i))}>
                      删除
                    </Button>
                  </div>
                ))}
                <div className='annotation-compose'>
                  <Input.TextArea id='prototype-note-input' rows={4} placeholder='添加此处的批注…' value={noteText} onChange={(e) => setNoteText(e.target.value)} />
                  <Button
                    type='primary'
                    size='small'
                    disabled={!noteText.trim()}
                    onClick={() => {
                      setAnnotations((old) => [...old, { author: '管理员', text: noteText, time: '刚刚' }]);
                      setNoteText('');
                      log('添加 PDF 批注', document.name);
                    }}
                  >
                    添加批注
                  </Button>
                </div>
              </>
            ) : (
              <>
                <p className='archive-muted annotation-intro'>当前为在线正文。可以在左侧切换 PDF 查看高亮与文字批注样式。</p>
                <Descriptions column={1} size='small' colon={false}>
                  <Descriptions.Item label='编写人'>{current.person}</Descriptions.Item>
                  <Descriptions.Item label='状态'>{current.status}</Descriptions.Item>
                  <Descriptions.Item label='版本'>V{versionCount}.0</Descriptions.Item>
                </Descriptions>
                <Button block icon={icon(History)} onClick={() => setDrawer('versions')}>
                  查看版本历史
                </Button>
              </>
            )}
          </aside>
        )}
      </div>
    </>
  );

  const editor = (
    <>
      {documentHeader}
      <div className='archive-editor-menubar'>
        {['开始', '插入', '页面布局'].map((t) => (
          <button className={editorTab === t ? 'selected' : ''} key={t} onClick={() => setEditorTab(t)}>
            {t}
          </button>
        ))}
        <span>{icon(ShieldCheck, 13)}仅当前会话编辑 · 样式演示</span>
      </div>
      <div className='archive-editor-toolbar'>
        {editorTab === '开始' ? (
          <>
            <Space size={2}>
              <Tooltip title='撤销'>
                <Button type='text' icon={icon(Undo2)} onMouseDown={(e) => e.preventDefault()} onClick={() => exec('undo')} />
              </Tooltip>
              <Tooltip title='重做'>
                <Button type='text' icon={icon(Redo2)} onMouseDown={(e) => e.preventDefault()} onClick={() => exec('redo')} />
              </Tooltip>
            </Space>
            <Divider type='vertical' />
            <Select
              defaultValue='正文'
              options={['正文', '标题 1', '标题 2', '标题 3'].map((x) => ({ value: x, label: x }))}
              onChange={(v) => exec('formatBlock', v === '正文' ? 'p' : `h${v.slice(-1)}`)}
            />
            <Select
              defaultValue='思源黑体'
              options={['思源黑体', '宋体', 'Arial'].map((x) => ({ value: x, label: x }))}
              onChange={(v) => exec('fontName', v === '宋体' ? 'SimSun' : v === 'Arial' ? 'Arial' : 'sans-serif')}
            />
            <Select
              defaultValue='14'
              options={['12', '14', '16', '18', '24', '32'].map((x) => ({ value: x, label: x }))}
              onChange={(v) => exec('fontSize', v === '24' || v === '32' ? '5' : '3')}
            />
            <Divider type='vertical' />
            {[
              [Bold, 'bold', '加粗'],
              [Italic, 'italic', '斜体'],
              [Underline, 'underline', '下划线'],
              [AlignLeft, 'justifyLeft', '左对齐'],
              [AlignCenter, 'justifyCenter', '居中'],
              [AlignRight, 'justifyRight', '右对齐'],
              [List, 'insertUnorderedList', '无序列表'],
              [ListOrdered, 'insertOrderedList', '有序列表'],
            ].map(([Icon, command, label]) => (
              <Tooltip title={label as string} key={command as string}>
                <Button
                  type='text'
                  aria-label={label as string}
                  icon={icon(Icon as React.ComponentType<any>)}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => exec(command as string)}
                />
              </Tooltip>
            ))}
          </>
        ) : editorTab === '插入' ? (
          <Space>
            <Button
              icon={icon(Table2)}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => exec('insertHTML', '<table><tbody><tr><th>列 1</th><th>列 2</th></tr><tr><td>内容</td><td>内容</td></tr></tbody></table><p><br></p>')}
            >
              插入表格
            </Button>
            <Button
              icon={icon(ImageIcon)}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => exec('insertHTML', '<div class="paper-image-placeholder">图片位置示意 · 实际图片功能待开发</div><p><br></p>')}
            >
              图片
            </Button>
            <Button icon={icon(Heading1)} onClick={() => demo('目录位置示意，正式功能按标题层级生成目录')}>
              目录
            </Button>
          </Space>
        ) : (
          <Space>
            <Select defaultValue='A4' options={['A4', 'A3', 'Letter'].map((value) => ({ value, label: value }))} onChange={() => demo('纸张规格控件样式演示')} />
            <Button onClick={() => demo('页边距设置样式演示')}>页边距</Button>
            <Button onClick={() => demo('页眉、页脚设置样式演示')}>页眉 / 页脚</Button>
            <Button onClick={() => demo('插入分页符的交互位置示意')}>分页</Button>
            <Button onClick={() => demo('封面排版的交互位置示意')}>封面</Button>
          </Space>
        )}
      </div>
      <div className='archive-editor-layout'>
        <aside className='archive-outline'>
          <div className='document-nav-heading'>文档大纲</div>
          <button className='outline-title'>{current.title}</button>
          {['一、培训目标', '二、内容与安排', '三、操作要点', '四、总结与后续'].map((t, i) => (
            <button
              key={t}
              onClick={() => paperRef.current?.querySelector(`#section-${['goal', 'plan', 'steps', 'result'][i]}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })}
            >
              {t}
            </button>
          ))}
          <div className='outline-divider' />
          <Button type='text' icon={icon(FolderClosed, 14)} onClick={() => setDrawer('detail')}>
            业务信息与资料
          </Button>
          <Button type='text' icon={icon(History, 14)} onClick={() => setDrawer('versions')}>
            版本历史
          </Button>
        </aside>
        <div className='archive-editor-canvas'>
          <div className='archive-ruler'>
            0<span>2</span>
            <span>4</span>
            <span>6</span>
            <span>8</span>
            <span>10</span>
            <span>12</span>
            <span>14</span>
            <span>16</span>
          </div>
          <article className='archive-paper editor-paper'>
            <div className='paper-page-header'>NIGHTINGALE　/　{categoryLabels[current.category]}</div>
            <div
              ref={paperRef}
              className='paper-content'
              contentEditable
              suppressContentEditableWarning
              role='textbox'
              aria-label='文档正文编辑区域'
              aria-multiline='true'
              onInput={() => {
                setDirty(true);
                setSaved(false);
              }}
            />
            <footer>
              {categoryLabels[current.category]} · {current.date}
              <span>第 1 页</span>
            </footer>
          </article>
          <div className='editor-canvas-note'>可输入文字、选择内容并试用排版；保存仅影响当前原型页面。</div>
        </div>
      </div>
    </>
  );

  return (
    <ConfigProvider locale={zhCN}>
      <div id='#tailwind' className={`archive-prototype-shell ${focusMode && view === 'read' ? 'focus-reading' : ''}`}>
        <aside className='archive-app-sidebar'>
          <div className='archive-sidebar-brand'>
            <img src='/image/logo-light-l.png' alt='Nightingale' />
            <button aria-label='收起菜单示意' onClick={() => demo('当前原型保持 main 默认侧栏宽度')}>
              {icon(PanelLeftClose, 14)}
            </button>
          </div>
          <div className='archive-onboarding'>
            <span className='onboarding-ring' />
            开始使用<span>1/9</span>
          </div>
          <nav>
            {[
              [House, '首页'],
              [Search, '快捷跳转'],
              [Sparkles, 'Nightingale AI'],
              [LayoutGrid, '数据查询'],
              [Bell, '告警通知'],
              [FolderClosed, '基础设施'],
              [Archive, '档案管理'],
              [ListChecks, '集成中心'],
              [Users, '人员组织'],
              [Settings, '系统配置'],
            ].map(([Icon, label]) => (
              <button
                key={label as string}
                className={label === '档案管理' ? 'selected' : ''}
                onClick={() => (label === '档案管理' ? navigate({ view: null, record: null, doc: null }) : demo('其他菜单为 main 风格参照，本轮只预览档案管理'))}
              >
                <span>{icon(Icon as React.ComponentType<any>, 15)}</span>
                <span>{label as string}</span>
                {['数据查询', '告警通知', '基础设施', '集成中心', '人员组织', '系统配置'].includes(label as string) && icon(ChevronRight, 13)}
              </button>
            ))}
          </nav>
          <div className='archive-sidebar-profile'>
            <span className='profile-avatar'>A</span>
            <span>
              <strong>管理员</strong>
              <small>root</small>
            </span>
            <button aria-label='设置示意' onClick={() => demo()}>
              {icon(Settings, 15)}
            </button>
          </div>
        </aside>
        <main className='archive-app-content'>{view === 'read' ? reader : view === 'edit' ? editor : listView}</main>
        <PrototypeSwitcher variant={variant} change={(v) => navigate({ variant: v })} category={category} count={filtered.length} view={view} />

        <Drawer
          className='n9e-antd-drawer'
          width={720}
          visible={drawer === 'detail'}
          destroyOnClose
          title='档案详情'
          onClose={() => setDrawer(null)}
          extra={
            <Button
              icon={icon(Pencil)}
              onClick={() => {
                setDrawer(null);
                editMetadata(current);
              }}
            >
              编辑信息
            </Button>
          }
        >
          <div className='archive-overline'>
            {categoryLabels[current.category]} / {current.id.toUpperCase()}
          </div>
          <h2 className='drawer-record-title'>{current.title}</h2>
          <div className='detail-section'>
            <h3>业务信息</h3>
            {recordInfo(current)}
          </div>
          <div className='detail-section'>
            <h3>
              文档与附件 <span>{current.documents.length} 份</span>
            </h3>
            {documentList(current)}
            <div className='detail-buttons'>
              <Button type='primary' icon={icon(BookOpen)} onClick={() => openRecord(current)}>
                打开阅读
              </Button>
              <Button icon={icon(FileText)} onClick={() => openRecord(current, 'edit')}>
                在线编辑
              </Button>
            </div>
          </div>
          <div className='detail-section'>
            <h3>留档记录</h3>
            <Space>
              <Button icon={icon(History)} onClick={() => setDrawer('versions')}>
                版本历史
              </Button>
              <Button icon={icon(ListChecks)} onClick={() => setDrawer('logs')}>
                操作日志
              </Button>
              <Button icon={icon(Archive)} onClick={() => archiveRecord(current)}>
                归档
              </Button>
            </Space>
          </div>
        </Drawer>
        <Drawer className='n9e-antd-drawer' width={520} visible={drawer === 'versions'} title='版本历史' onClose={() => setDrawer(null)}>
          <h3>{current.title}</h3>
          <p className='archive-muted'>修改会形成版本，恢复后保留原历史。以下为交互示意。</p>
          <Button block icon={icon(History)} onClick={() => setCompare(true)}>
            比较 V3.0 与 V2.0
          </Button>
          <div className='version-list'>
            {[versionCount, 2, 1].map((v, i) => (
              <div className='version-item' key={`${v}-${i}`}>
                <span className={`version-dot ${i === 0 ? 'current' : ''}`} />
                <div>
                  <h4>
                    V{v}.0 {i === 0 && <Tag color='purple'>当前版本</Tag>}
                  </h4>
                  <p>{i === 0 ? '补充操作要点，更新文档内容' : i === 1 ? '资料核对完成，正式归档' : '创建业务信息与文档初稿'}</p>
                  <small>
                    {i === 0 ? current.updated : `2026-10-22 ${i === 1 ? '17:08' : '09:10'}`} · {i === 0 ? current.person : '陈静'}
                  </small>
                  <Space>
                    <Button
                      type='link'
                      size='small'
                      onClick={() => {
                        setDrawer(null);
                        openRecord(current);
                      }}
                    >
                      查看
                    </Button>
                    {i !== 0 && (
                      <Button
                        type='link'
                        size='small'
                        onClick={() =>
                          Modal.confirm({
                            title: `演示恢复 V${v}.0？`,
                            content: '将增加一个模拟版本，真实资料不会被修改。',
                            onOk: () => {
                              setVersionCount((n) => n + 1);
                              log('恢复文档版本', `从 V${v}.0 恢复（演示）`);
                              message.success('已演示版本恢复');
                            },
                          })
                        }
                      >
                        恢复此版本
                      </Button>
                    )}
                  </Space>
                </div>
              </div>
            ))}
          </div>
        </Drawer>
        <Drawer className='n9e-antd-drawer' width={560} visible={drawer === 'logs'} title='人员操作日志' onClose={() => setDrawer(null)}>
          <h3>{current.title}</h3>
          <p className='archive-muted'>归档后的修改、替换及恢复均保留人员记录。以下为模拟日志。</p>
          <Timeline className='archive-log-timeline'>
            {logs.map((l, i) => (
              <Timeline.Item key={i} color={i === 0 ? '#8162dc' : '#c8c8d0'}>
                <div className='log-entry-title'>
                  {l.action}
                  <span>{l.author}</span>
                </div>
                <p>{l.detail}</p>
                <small>{l.time}</small>
              </Timeline.Item>
            ))}
          </Timeline>
        </Drawer>
        <Drawer
          className='n9e-antd-drawer'
          width={800}
          visible={!!formMode}
          destroyOnClose
          title={formMode === 'metadata' ? '编辑业务信息' : formMode === 'online' ? '在线新建档案' : '新建档案 · 上传资料'}
          onClose={() => setFormMode(null)}
          footer={
            <div className='archive-form-footer'>
              <span>仅在当前原型会话中保存</span>
              <Space>
                <Button onClick={() => setFormMode(null)}>取消</Button>
                <Button type='primary' onClick={submitForm}>
                  {formMode === 'online' ? '进入在线编辑' : '保存草稿'}
                </Button>
              </Space>
            </div>
          }
        >
          <div className='form-category-label'>
            {icon(Archive, 16)}
            {categoryLabels[category]}
            <Tag>草稿</Tag>
          </div>
          <Form form={form} layout='vertical' requiredMark>
            <h3 className='form-section-title'>基本信息</h3>
            <Form.Item label={sourceLabels[category]} name='title' rules={[{ required: true, message: `请填写${sourceLabels[category]}` }]}>
              <Input placeholder={`请输入${sourceLabels[category]}`} />
            </Form.Item>
            <div className='archive-form-grid'>
              {category === 'training' ? (
                <>
                  <Form.Item label='培训部门' name='department'>
                    <Input placeholder='手动填写部门' />
                  </Form.Item>
                  <Form.Item label='培训地点' name='location'>
                    <Input placeholder='请输入培训地点' />
                  </Form.Item>
                  <Form.Item label='培训时间' name='time'>
                    <DatePicker.RangePicker showTime style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item label='培训时长（小时）' name='duration'>
                    <InputNumber min={0.5} step={0.5} placeholder='实际培训时长' style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item label='培训讲师' name='person'>
                    <Input placeholder='手动填写姓名' />
                  </Form.Item>
                  <Form.Item label='培训状态'>
                    <Input value='正常' readOnly />
                  </Form.Item>
                </>
              ) : category === 'inspection' ? (
                <>
                  <Form.Item label='巡检结果' name='result' rules={[{ required: true, message: '请选择巡检结果' }]}>
                    <Select placeholder='请选择' options={['正常', '异常'].map((value) => ({ value, label: value }))} />
                  </Form.Item>
                  <Form.Item label='处理人' name='person'>
                    <Input placeholder='手动填写姓名' />
                  </Form.Item>
                </>
              ) : category === 'incident' ? (
                <>
                  <Form.Item label='故障等级' name='level' rules={[{ required: true, message: '请选择故障等级' }]}>
                    <Select options={['Ⅰ级', 'Ⅱ级', 'Ⅲ级', 'Ⅳ级'].map((value) => ({ value, label: value }))} placeholder='请选择故障等级' />
                  </Form.Item>
                  <Form.Item label='报告状态'>
                    <Input value='草稿' readOnly />
                  </Form.Item>
                  <Form.Item label='故障发生时间' name='startTime'>
                    <DatePicker showTime style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item label='故障解决时间' name='endTime'>
                    <DatePicker showTime style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item label='主要处理工程师' name='person'>
                    <Input placeholder='手动填写姓名' />
                  </Form.Item>
                  <Form.Item label='辅助处理工程师' name='assistant'>
                    <Input placeholder='可填写多人' />
                  </Form.Item>
                </>
              ) : (
                <>
                  <Form.Item label='文档类型' name='type'>
                    <Select mode='tags' placeholder='填写新类型或选择已有类型' options={['操作规范', '操作手册', '技术说明'].map((value) => ({ value, label: value }))} />
                  </Form.Item>
                  <Form.Item label='编写人' name='person'>
                    <Input placeholder='手动填写姓名' />
                  </Form.Item>
                </>
              )}
            </div>
            {category === 'training' && (
              <Form.Item label='参加人员' name='participants'>
                <Input.TextArea rows={2} placeholder='手动填写参加人员，多人可用顿号分隔' />
              </Form.Item>
            )}
            {category === 'inspection' && (
              <Form.Item label='巡检设备范围' name='scope'>
                <Checkbox.Group options={['运维平台', '服务器', '存储', '安全设备', '网络', '交换机']} />
              </Form.Item>
            )}
            <Form.Item label={category === 'training' ? '培训内容' : category === 'inspection' ? '巡检总结' : '备注说明'} name='summary'>
              <Input.TextArea rows={3} placeholder='填写简要说明，正式文档内容可在在线编辑页编写' />
            </Form.Item>
            {category === 'training' && (
              <Form.Item label='培训效果' name='effect'>
                <Input.TextArea rows={2} placeholder='填写培训效果与反馈' />
              </Form.Item>
            )}
            {formMode !== 'metadata' && (
              <>
                <h3 className='form-section-title'>文档与附件</h3>
                <div className='archive-upload-purpose'>
                  <span>资料用途</span>
                  <Select
                    defaultValue={category === 'training' ? '培训讲义' : '报告附件'}
                    options={(category === 'training' ? ['培训讲义', '签到资料', '补充资料'] : ['报告附件', '补充资料']).map((value) => ({ value, label: value }))}
                  />
                </div>
                <Upload.Dragger multiple beforeUpload={() => false} fileList={files} onChange={({ fileList }) => setFiles(fileList)}>
                  <div className='upload-icon'>{icon(UploadCloud, 30)}</div>
                  <p>拖拽文件到此处，或点击选择</p>
                  <span>单文件 50 MiB，最多 20 份；此原型只展示选择结果，不会上传文件</span>
                </Upload.Dragger>
                <Button type='link' onClick={() => setFiles([{ uid: 'demo1', name: category === 'training' ? '培训签到表.pdf' : '报告附件.pdf', status: 'done' }])}>
                  使用示例文件查看效果
                </Button>
              </>
            )}
          </Form>
        </Drawer>
        <Modal width={1050} visible={compare} title='版本比较 · V2.0 → V3.0' footer={<Button onClick={() => setCompare(false)}>关闭</Button>} onCancel={() => setCompare(false)}>
          <div className='archive-compare'>
            <div>
              <h4>
                V2.0 <span>2026-10-22 17:08 · 陈静</span>
              </h4>
              <h3>三、操作要点</h3>
              <p>操作前确认当前环境与计划内容一致。</p>
              <p className='diff-removed'>完成后检查操作结果。</p>
            </div>
            <div>
              <h4>
                V3.0 <Tag color='purple'>当前版本</Tag>
              </h4>
              <h3>三、操作要点</h3>
              <p>操作前确认当前环境与计划内容一致。</p>
              <p className='diff-added'>执行期间做好过程记录，异常情况先确认影响范围。</p>
              <p className='diff-added'>完成后复查结果，补充报告及相关资料。</p>
            </div>
          </div>
          <p className='archive-muted'>此处用于确认版本差异展示样式，未执行真实内容比较。</p>
        </Modal>
      </div>
    </ConfigProvider>
  );
}

createRoot(window.document.getElementById('root')!).render(<ArchiveManagementPrototype />);
