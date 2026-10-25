import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Input, Modal, Segmented, Space, Spin, Tag, message } from 'antd';
import { FullscreenOutlined, SaveOutlined, EditOutlined, BookOutlined, DownloadOutlined, PrinterOutlined } from '@ant-design/icons';
import { Link, Prompt, useHistory, useLocation, useParams } from 'react-router-dom';
import { flushSync } from 'react-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import PageLayout from '@/components/pageLayout';
import { AnnotationBody, Archive, ArchiveDocument, Body, Draft, Lease, emptyBody, fmtDate } from './types';
import { acquire, api, downloadBlob, errorText, exportDocument, fileBlob, getArchive, getDocument, release } from './services';
import { claim, bindWindow, storedLease, rememberLease, forgetLease } from './lease';
import { Select } from 'antd';
import { DocumentAssets } from './assets';
import { Projection, projectMarkdown, restoreMarkdown } from './markdown';
import type { RichHandle } from './RichEditor';
const RichEditor = React.lazy(() => import('./RichEditor'));
const PdfReader = React.lazy(() => import('./PdfReader'));
import HistoryPanel, { plainText } from './HistoryPanel';

export default function DocumentPage() {
  const { id, documentID } = useParams<{ id: string; documentID: string }>();
  const location = useLocation();
  const history = useHistory();
  const versionID = Number(new URLSearchParams(location.search).get('version')) || undefined;
  const [archive, setArchive] = useState<Archive>();
  const [doc, setDoc] = useState<ArchiveDocument>();
  const [error, setError] = useState('');
  const [lockError, setLockError] = useState('');
  const [lease, setLease] = useState<Lease>();
  const leaseRef = useRef<Lease>();
  leaseRef.current = lease;
  const [body, setBody] = useState<Body>(emptyBody());
  const [annotations, setAnnotations] = useState<AnnotationBody>({ annotations: [] });
  const annotationRef = useRef(annotations);
  annotationRef.current = annotations;
  const [mode, setMode] = useState<'visual' | 'markdown'>('visual');
  const modeRef = useRef(mode);
  modeRef.current = mode;
  const [markdown, setMarkdown] = useState('');
  const markdownRef = useRef(markdown);
  markdownRef.current = markdown;
  const projection = useRef<Projection>();
  const [pdf, setPdf] = useState<Blob>();
  const [image, setImage] = useState('');
  const [wordBuffer, setWordBuffer] = useState<ArrayBuffer>();
  const [ready, setReady] = useState(false);
  const [busy, setBusy] = useState(false);
  const [autosaving, setAutosaving] = useState(false);
  const [recovering, setRecovering] = useState(false);
  const recoveringRef = useRef(false);
  recoveringRef.current = recovering;
  const [includeAnnotations, setIncludeAnnotations] = useState(true);
  const [status, setStatus] = useState('');
  const [dirty, setDirty] = useState(false);
  const generation = useRef(0);
  const savedGeneration = useRef(0);
  const operation = useRef(false);
  const [editorKey, setEditorKey] = useState(0);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [page, setPage] = useState(1);
  const [initialPage, setInitialPage] = useState<number>();
  const [imageScale, setImageScale] = useState(100);
  const editor = useRef<RichHandle>(null);
  const assets = useRef(new DocumentAssets());
  const root = useRef<HTMLDivElement>(null);
  const mark = () => {
    generation.current++;
    setDirty(true);
  };
  const load = useCallback(async () => {
    setError('');
    setReady(false);
    setPdf(undefined);
    setWordBuffer(undefined);
    setInitialPage(undefined);
    try {
      const [a, d, b] = await Promise.all([getArchive(Number(id)), getDocument(Number(documentID), versionID), api<any>(`/documents/${documentID}/bookmark`)]);
      if (d.archive_id !== a.id) throw new Error('文档不属于此档案');
      setArchive(a);
      setDoc(d);
      setBody(d.kind === 'online' ? (d.version.body as Body) : emptyBody());
      setAnnotations(d.kind === 'pdf' ? (d.version.body as AnnotationBody) || { annotations: [] } : { annotations: [] });
      setInitialPage(b?.version_id === d.version.id ? b.position?.page || 1 : 1);
      generation.current = 0;
      savedGeneration.current = 0;
      setDirty(false);
      setMode('visual');
    } catch (e) {
      setError(errorText(e));
    }
  }, [id, documentID, versionID]);
  useEffect(() => {
    load();
    return () => {
      const l = leaseRef.current;
      if (l) {
        release(Number(documentID), l).catch(() => undefined);
        forgetLease(Number(documentID));
      }
      assets.current.destroy();
    };
  }, [load]);
  useEffect(() => {
    if (!doc || initialPage === undefined || (doc.kind === 'online' && lease)) return;
    const controller = new AbortController();
    let url = '';
    (async () => {
      try {
        setStatus(doc.kind === 'online' ? '正在生成与导出一致的阅读版式…' : '正在载入文档…');
        if (doc.kind === 'online') {
          const { blob } = await exportDocument(doc.archive_id, 'pdf', doc.id, doc.version.id, controller.signal, true);
          if (!controller.signal.aborted) setPdf(blob);
        } else {
          const blob = await fileBlob(doc.file!.id);
          if (controller.signal.aborted) return;
          if (doc.kind === 'pdf') setPdf(blob);
          else if (doc.kind === 'docx') setWordBuffer(await blob.arrayBuffer());
          else {
            url = URL.createObjectURL(blob);
            setImage(url);
          }
        }
        if (!controller.signal.aborted) setStatus('');
      } catch (e) {
        if (!controller.signal.aborted) {
          setError(errorText(e));
          setStatus('');
        }
      }
    })();
    return () => {
      controller.abort();
      if (url) URL.revokeObjectURL(url);
    };
  }, [doc?.version.id, initialPage, doc?.kind === 'online' && !!lease]);
  useEffect(() => bindWindow(Number(documentID), () => leaseRef.current), [documentID]);
  useEffect(() => {
    if (doc && lease) rememberLease(doc.id, lease);
  }, [doc?.id, lease]);
  const installDraft = async (draft: Draft, active: Lease) => {
    let restored: any = draft.body;
    if (doc?.kind === 'online') restored = await assets.current.canonical(restored, doc.archive_id, doc.id, active);
    await api(`/documents/${doc!.id}/draft/discard`, 'POST', active);
    await api(`/documents/${doc!.id}/draft`, 'PUT', { ...active, body: restored, mode: draft.mode, markdown: draft.markdown || '', switch_mode: true });
    if (doc?.kind === 'online') {
      await assets.current.display(restored);
      setBody(restored);
      if (draft.mode === 'markdown') {
        projection.current = projectMarkdown(restored);
        setMarkdown(draft.markdown || projection.current.markdown);
        setMode('markdown');
      } else setMode('visual');
      setEditorKey((v) => v + 1);
    } else setAnnotations(restored);
    mark();
    setRecovering(false);
    setStatus('已恢复本人私有草稿，正式版本尚未改变');
  };
  const beginEdit = async (options?: { resume?: Lease; reacquire?: boolean }) => {
    if (!doc || versionID || operation.current || (lease && !options?.reacquire)) return;
    setBusy(true);
    let local: Draft | undefined;
    try {
      if (options?.reacquire && lease) {
        local = {
          body: doc.kind === 'pdf' ? annotationRef.current : modeRef.current === 'markdown' ? projection.current!.baseline : editor.current!.getBody(),
          mode: doc.kind === 'pdf' ? 'annotations' : modeRef.current,
          markdown: markdownRef.current,
          base_version_id: lease.base_version_id,
          update_at: Date.now(),
        };
        await release(doc.id, lease);
      }
      const result = await claim(doc.id, options?.resume);
      setLease(result.lease);
      setDoc(result.document);
      setBody(result.document.kind === 'online' ? (result.document.version.body as Body) : emptyBody());
      setLockError('');
      setReady(false);
      setMode('visual');
      setEditorKey((v) => v + 1);
      const draft = local || result.draft;
      if (draft) {
        setRecovering(true);
        const stale = draft.base_version_id !== result.document.version.id;
        Modal.confirm({
          title: stale ? '旧草稿与正式版本存在差异' : '发现本人未发布的草稿',
          width: stale ? 1100 : 600,
          content: stale ? (
            <div>
              正式版本不会自动被覆盖。请先比较，再选择是否以旧草稿继续编辑；正式保存会生成新版本。
              <div className='archive-compare'>
                <div>
                  <strong>当前正式版本</strong>
                  <pre>{plainText(result.document.version.body).slice(0, 50000)}</pre>
                </div>
                <div>
                  <strong>本人草稿</strong>
                  <pre>{(draft.mode === 'markdown' ? draft.markdown : plainText(draft.body)).slice(0, 50000)}</pre>
                </div>
              </div>
            </div>
          ) : (
            `保存于 ${fmtDate(draft.update_at)}。继续草稿，或明确放弃草稿使用正式版本。`
          ),
          okText: stale ? '以旧草稿继续编辑' : '继续草稿',
          cancelText: '使用正式版本',
          onOk: () => installDraft(draft, result.lease),
          onCancel: async () => {
            await api(`/documents/${doc.id}/draft/discard`, 'POST', result.lease);
            setRecovering(false);
          },
        });
      } else setRecovering(false);
    } catch (e) {
      setLockError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const autoBegin = useRef(false);
  useEffect(() => {
    if (!doc || !archive || archive.deleted || versionID || autoBegin.current || !['online', 'pdf'].includes(doc.kind)) return;
    const stored = storedLease(doc.id);
    const reload = (performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming)?.type === 'reload';
    if (new URLSearchParams(location.search).get('edit') === 'true' || (reload && stored)) {
      autoBegin.current = true;
      beginEdit(reload && stored ? { resume: stored } : undefined);
    }
  }, [doc?.id, archive?.id]);
  useEffect(() => {
    if (!lease || !doc) return;
    const timer = setInterval(async () => {
      try {
        const previous = leaseRef.current;
        const next = await api<Lease>(`/documents/${doc.id}/lease`, 'PUT', previous);
        if (leaseRef.current?.session_id !== previous?.session_id) return;
        setLease(next);
        setLockError('');
      } catch (e) {
        setLockError(errorText(e));
      }
    }, 30000);
    return () => clearInterval(timer);
  }, [lease?.session_id, doc?.id]);
  const makeInput = async (formal: boolean) => {
    if (!doc || !leaseRef.current) throw new Error('没有有效编辑会话');
    let content: any;
    if (doc.kind === 'pdf') content = annotationRef.current;
    else if (modeRef.current === 'markdown') {
      if (!projection.current) throw new Error('Markdown 保留快照缺失');
      content = formal ? restoreMarkdown(projection.current, markdownRef.current) : projection.current.baseline;
    } else content = await assets.current.canonical(editor.current!.getBody(), doc.archive_id, doc.id, leaseRef.current);
    return {
      ...leaseRef.current,
      base_version_id: leaseRef.current.base_version_id,
      body: content,
      mode: doc.kind === 'pdf' ? 'annotations' : modeRef.current,
      markdown: modeRef.current === 'markdown' ? markdownRef.current : '',
    };
  };
  const autosave = async () => {
    if (
      !leaseRef.current ||
      recoveringRef.current ||
      !doc ||
      lockError ||
      operation.current ||
      generation.current === savedGeneration.current ||
      (doc.kind === 'online' && modeRef.current === 'visual' && !ready)
    )
      return;
    operation.current = true;
    setAutosaving(true);
    setStatus('正在保存本人私有草稿…');
    const gen = generation.current;
    try {
      const input = await makeInput(false);
      await api(`/documents/${doc.id}/draft`, 'PUT', input);
      savedGeneration.current = gen;
      setStatus(`私有草稿已保存 · ${new Date().toLocaleTimeString()}`);
    } catch (e) {
      setStatus('草稿保存失败：' + errorText(e));
      throw e;
    } finally {
      operation.current = false;
      setAutosaving(false);
    }
  };
  const autosaveRef = useRef(autosave);
  autosaveRef.current = autosave;
  useEffect(() => {
    if (!lease) return;
    const timer = setInterval(() => autosaveRef.current().catch(() => undefined), 10000);
    return () => clearInterval(timer);
  }, [lease?.session_id]);
  useEffect(() => {
    const listener = (e: BeforeUnloadEvent) => {
      if (leaseRef.current && generation.current !== savedGeneration.current) {
        e.preventDefault();
        e.returnValue = '';
      }
    };
    window.addEventListener('beforeunload', listener);
    return () => window.removeEventListener('beforeunload', listener);
  }, [dirty]);
  const save = async () => {
    if (!doc || !lease || operation.current) return;
    operation.current = true;
    setBusy(true);
    const gen = generation.current;
    try {
      const input = await makeInput(true);
      const result = await api<ArchiveDocument>(`/documents/${doc.id}/save`, 'POST', { ...input, description: doc.kind === 'pdf' ? '保存 PDF 批注' : '保存在线文档' });
      setDoc(result);
      setLease({ ...lease, base_version_id: result.version.id });
      if (doc.kind === 'pdf') setAnnotations(result.version.body as AnnotationBody);
      else if (mode === 'markdown') {
        projection.current = projectMarkdown(result.version.body as Body);
        setMarkdown(projection.current.markdown);
        setBody(result.version.body as Body);
      }
      savedGeneration.current = gen;
      if (generation.current === gen) setDirty(false);
      setStatus(`已保存正式版本 v${result.version.sequence}`);
      message.success('正式版本已保存');
    } catch (e) {
      setError(errorText(e));
    } finally {
      operation.current = false;
      setBusy(false);
    }
  };
  const switchMode = async (next: string) => {
    if (next === mode || !doc || !lease || operation.current) return;
    operation.current = true;
    setBusy(true);
    try {
      if (next === 'markdown') {
        const b = await assets.current.canonical(editor.current!.getBody(), doc.archive_id, doc.id, lease);
        const p = projectMarkdown(b);
        await api(`/documents/${doc.id}/draft`, 'PUT', { ...lease, body: b, mode: 'markdown', markdown: p.markdown, switch_mode: true });
        projection.current = p;
        setMarkdown(p.markdown);
        setBody(b);
        setMode('markdown');
      } else {
        const b = restoreMarkdown(projection.current!, markdown);
        await api(`/documents/${doc.id}/draft`, 'PUT', { ...lease, body: b, mode: 'visual', markdown: '', switch_mode: true });
        setBody(b);
        setMode('visual');
        setReady(false);
        setEditorKey((v) => v + 1);
      }
      mark();
      setError('');
    } catch (e) {
      setError(errorText(e));
    } finally {
      operation.current = false;
      setBusy(false);
    }
  };
  const exitEdit = async () => {
    if (!doc || !lease || operation.current) return;
    setBusy(true);
    try {
      if (lockError && generation.current !== savedGeneration.current) throw new Error('当前会话无法保存草稿，请恢复编辑会话后重试');
      await autosave();
      if (!dirty) await api(`/documents/${doc.id}/draft/discard`, 'POST', lease);
      await release(doc.id, lease);
      forgetLease(doc.id);
      flushSync(() => {
        setLease(undefined);
        setDirty(false);
      });
      history.replace(location.pathname);
      setLockError('');
      setDirty(false);
      await load();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const convertWord = () => {
    if (!doc || !archive) return;
    Modal.confirm({
      title: '确认 Word 转换结果',
      content: '当前展示的是可编辑转换结果。请检查文字、标题、表格和图片；确认后保存为新的在线文档，原始 Word 及其版本继续保留。',
      okText: '确认并转在线编辑',
      cancelText: '继续检查',
      onOk: async () => {
        setBusy(true);
        try {
          const b = await assets.current.canonical(editor.current!.getBody(), archive.id);
          const result = await api<Archive>(`/${archive.id}/documents`, 'POST', {
            title:
              Array.from(doc.title.replace(/\.docx$/i, ''))
                .slice(0, 251)
                .join('') + '（在线）',
            purpose: doc.purpose,
            source_document_id: doc.id,
            source_version_id: doc.version.id,
            body: b,
          });
          const next = result.documents!.find((d) => !archive.documents?.some((x) => x.id === d.id))!;
          history.push(`/archives/${archive.id}/documents/${next.id}?edit=true`);
          window.location.reload();
        } catch (e) {
          message.error(errorText(e));
          throw e;
        } finally {
          setBusy(false);
        }
      },
    });
  };
  const output = async (format: string, print = false) => {
    if (!doc || !archive) return;
    const popup = print ? window.open('', '_blank') : null;
    setBusy(true);
    try {
      const result =
        format === 'pdf_original'
          ? { blob: await fileBlob(doc.file!.id, true), job: { file_name: doc.file!.name } }
          : await exportDocument(archive.id, format, doc.id, doc.version.id);
      if (print) {
        await api(`/${archive.id}/print`, 'POST', { document_id: doc.id, version_id: doc.version.id });
        const url = URL.createObjectURL(result.blob);
        if (popup) {
          popup.location.href = url;
          popup.onload = () => {
            try {
              popup.print();
            } catch {
              message.info('请在打开的 PDF 窗口中使用打印');
            }
          };
        } else message.info('浏览器阻止了打印窗口，请允许本站弹出窗口');
        setTimeout(() => URL.revokeObjectURL(url), 120000);
      } else await downloadBlob(result.blob, result.job.file_name);
    } catch (e) {
      if (popup) popup.close();
      message.error(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  if (!doc || !archive)
    return (
      <PageLayout title='档案管理' showBack backPath={`/archives/${id}`}>
        <div className='n9e'>{error ? <Alert type='error' message={error} action={<Button onClick={load}>重试</Button>} /> : <Spin />}</div>
      </PageLayout>
    );
  const pdfFormat = doc.kind === 'pdf' ? 'annotated_pdf' : 'pdf';
  return (
    <React.Suspense fallback={<Spin tip='正在载入文档工具…' />}>
      <PageLayout title='档案管理' showBack backPath={`/archives/${id}`}>
        <div className='n9e archive-document-page' ref={root}>
          <Prompt when={!!lease && dirty} message='当前编辑内容保存在私有草稿中。确认离开编辑页面？尚未自动保存的内容可能丢失。' />
          <div className='archive-document-heading'>
            <div>
              <Link to={`/archives/${archive.id}`}>{archive.title}</Link>
              <h3>
                {doc.title} <Tag>v{doc.version.sequence}</Tag>
                {versionID && <Tag color='orange'>历史版本</Tag>}
                {lease && <Tag color='blue'>独占编辑中</Tag>}
              </h3>
              <div className='archive-hint'>{status || `${doc.version.create_name} · ${fmtDate(doc.version.create_at)}`}</div>
            </div>
            <Space wrap>
              {archive.documents && archive.documents.length > 1 && (
                <Select
                  disabled={!!lease}
                  value={doc.id}
                  style={{ width: 190 }}
                  onChange={(v) => history.push(`/archives/${archive.id}/documents/${v}`)}
                  options={archive.documents.map((d) => ({ value: d.id, label: d.title }))}
                />
              )}
              {!lease && doc.kind === 'pdf' && (
                <Select
                  size='small'
                  value={includeAnnotations}
                  onChange={setIncludeAnnotations}
                  style={{ width: 125 }}
                  options={[
                    { value: true, label: '含批注输出' },
                    { value: false, label: '不含批注输出' },
                  ]}
                />
              )}
              <Button size='small' onClick={() => setHistoryOpen(true)}>
                版本历史
              </Button>
              <Button size='small' icon={<FullscreenOutlined />} onClick={() => (document.fullscreenElement ? document.exitFullscreen() : root.current?.requestFullscreen())}>
                全屏
              </Button>
              {lease ? (
                <>
                  <Button loading={busy} disabled={busy || autosaving || operation.current} onClick={exitEdit}>
                    退出编辑（保留草稿）
                  </Button>
                  <Button
                    type='primary'
                    icon={<SaveOutlined />}
                    loading={busy}
                    disabled={busy || autosaving || !!lockError || recovering || (!ready && doc.kind === 'online' && mode === 'visual')}
                    onClick={save}
                  >
                    保存正式版本
                  </Button>
                </>
              ) : (
                <>
                  <Button
                    size='small'
                    icon={<BookOutlined />}
                    onClick={async () => {
                      try {
                        await api(`/documents/${doc.id}/bookmark`, 'PUT', { version_id: doc.version.id, position: { page } });
                        message.success(`已保存第 ${page} 页阅读位置`);
                      } catch (e) {
                        message.error(errorText(e));
                      }
                    }}
                  >
                    保存书签
                  </Button>
                  {doc.file && (
                    <Button
                      size='small'
                      icon={<DownloadOutlined />}
                      onClick={async () => {
                        try {
                          await downloadBlob(await fileBlob(doc.file!.id, true), doc.file!.name);
                        } catch (e) {
                          message.error(errorText(e));
                        }
                      }}
                    >
                      下载原件
                    </Button>
                  )}
                  {['online', 'pdf'].includes(doc.kind) && (
                    <>
                      <Button size='small' loading={busy} onClick={() => output(doc.kind === 'pdf' && !includeAnnotations ? 'pdf_original' : pdfFormat)}>
                        导出 PDF
                      </Button>
                      <Button
                        size='small'
                        icon={<PrinterOutlined />}
                        loading={busy}
                        onClick={() => output(doc.kind === 'pdf' && !includeAnnotations ? 'pdf_original' : pdfFormat, true)}
                      >
                        打印
                      </Button>
                    </>
                  )}
                  {doc.kind === 'online' && (
                    <Button size='small' loading={busy} onClick={() => output('docx')}>
                      导出 DOCX
                    </Button>
                  )}
                  {!archive.deleted && !versionID && doc.kind === 'docx' && doc.purpose !== 'attendance' && (
                    <Button type='primary' disabled={!ready} loading={busy} onClick={convertWord}>
                      转在线编辑
                    </Button>
                  )}
                  {!archive.deleted && !versionID && ['online', 'pdf'].includes(doc.kind) && (
                    <Button type='primary' icon={<EditOutlined />} loading={busy} onClick={() => beginEdit()}>
                      {doc.kind === 'pdf' ? '编辑批注' : '编辑文档'}
                    </Button>
                  )}
                </>
              )}
            </Space>
          </div>
          {error && (
            <Alert
              className='mb-2'
              type='error'
              showIcon
              closable
              onClose={() => setError('')}
              message={error}
              action={!lease ? <Button onClick={load}>重试</Button> : undefined}
            />
          )}
          {lockError && (
            <Alert
              className='mb-2'
              type='warning'
              showIcon
              message={lockError}
              action={
                lease ? (
                  <Button disabled={busy} onClick={() => beginEdit({ reacquire: true })}>
                    重新取得编辑权并比较
                  </Button>
                ) : undefined
              }
              description={lease ? '编辑已暂停，正文仍保留在当前窗口。恢复连接后会重新检查会话；超时后请退出并重新取得编辑权。' : '此文档已有其他编辑窗口，或暂时无法取得编辑权。'}
            />
          )}
          {lease && doc.kind === 'online' && (
            <div className='archive-modebar'>
              <Space>
                <Segmented
                  value={mode}
                  disabled={busy || autosaving || !!lockError || recovering || (!ready && mode === 'visual')}
                  onChange={(v) => switchMode(String(v))}
                  options={[
                    { value: 'visual', label: '可视化' },
                    { value: 'markdown', label: 'Markdown' },
                  ]}
                />
                <span className='archive-hint'>每 10 秒保存本人草稿；点击“保存正式版本”后对其他账号可见。</span>
              </Space>
            </div>
          )}
          {lease && doc.kind === 'online' ? (
            mode === 'visual' ? (
              <RichEditor
                key={editorKey}
                ref={editor}
                body={body}
                assets={assets.current}
                archiveID={archive.id}
                documentID={doc.id}
                lease={lease}
                readOnly={!!lockError || busy || recovering}
                onChange={mark}
                onReady={() => setReady(true)}
              />
            ) : (
              <div className='archive-markdown'>
                <div>
                  <Alert type='info' message='复杂对象使用只读占位块保留；页设置、页眉页脚请在可视化模式修改。' />
                  <Input.TextArea
                    aria-label='Markdown 文档源码'
                    value={markdown}
                    disabled={busy || !!lockError || recovering}
                    onChange={(e) => {
                      setMarkdown(e.target.value);
                      mark();
                    }}
                    spellCheck={false}
                  />
                </div>
                <div className='archive-markdown-preview'>
                  <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    transformImageUri={(uri) => uri}
                    skipHtml
                    components={{
                      img: ({ src }) => {
                        const image = projection.current?.images[src || ''];
                        const url = image && assets.current.url(image.value);
                        return url ? <img src={url} alt='文档图片' /> : <span>〔受管图片〕</span>;
                      },
                    }}
                  >
                    {markdown.replace(/<!--archive-block:([^>]+)-->/g, '\n\n> 〔只读对象 $1：回到可视化模式查看〕\n\n')}
                  </ReactMarkdown>
                </div>
              </div>
            )
          ) : doc.kind === 'docx' ? (
            wordBuffer ? (
              <>
                <Alert type='info' showIcon message='Word 转换预览：请检查文字、表格和图片。复杂排版可能与原始 Word 不同，原件始终保留。' />
                <RichEditor
                  key={doc.version.id}
                  ref={editor}
                  body={emptyBody()}
                  wordBuffer={wordBuffer}
                  assets={assets.current}
                  archiveID={archive.id}
                  documentID={doc.id}
                  readOnly
                  onReady={() => setReady(true)}
                />
              </>
            ) : (
              <Spin tip={status} />
            )
          ) : doc.kind === 'image' ? (
            <div className='archive-image-reader'>
              <Space>
                <Button onClick={() => setImageScale((v) => Math.max(10, v - 25))}>缩小</Button>
                <span>{imageScale}%</span>
                <Button onClick={() => setImageScale((v) => Math.min(400, v + 25))}>放大</Button>
              </Space>
              <div>
                <img src={image} style={{ width: `${imageScale}%` }} alt={doc.title} />
              </div>
            </div>
          ) : pdf && initialPage !== undefined ? (
            <PdfReader
              key={`${doc.id}-${lease ? 'edit' : 'read'}`}
              blob={pdf}
              annotations={doc.kind === 'pdf' ? annotations : undefined}
              editable={!!lease && !lockError && !busy && !recovering}
              onChange={(v) => {
                setAnnotations(v);
                mark();
              }}
              initialPage={lease ? page : initialPage}
              onPosition={setPage}
            />
          ) : (
            <div className='archive-document-loading'>
              <Spin tip={status || '正在载入阅读版式'} />
            </div>
          )}
          <HistoryPanel archive={archive} documentID={doc.id} kind='versions' visible={historyOpen} onClose={() => setHistoryOpen(false)} onRestored={load} />
        </div>
      </PageLayout>
    </React.Suspense>
  );
}
