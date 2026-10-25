import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Alert, Button, Input, InputNumber, Modal, Select, Space, Tooltip, Switch } from 'antd';
import * as pdfjs from 'pdfjs-dist';
import { EventBus, PDFViewer, PDFLinkService, PDFFindController } from 'pdfjs-dist/web/pdf_viewer.mjs';
import workerURL from 'pdfjs-dist/build/pdf.worker.mjs?url';
import 'pdfjs-dist/web/pdf_viewer.css';
import { Annotation, AnnotationBody } from './types';
import { annotationToViewport, viewportToAnnotation } from './pdfGeometry';
import { errorText } from './services';
pdfjs.GlobalWorkerOptions.workerSrc = workerURL;
interface Props {
  blob: Blob;
  annotations?: AnnotationBody;
  editable?: boolean;
  onChange?: (v: AnnotationBody) => void;
  onPosition?: (page: number) => void;
  initialPage?: number;
}
function Thumbnail({ pdf, number }: { pdf: any; number: number }) {
  const canvas = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    let done = false;
    let task: any;
    const observer = new IntersectionObserver(async (entries) => {
      if (done || !entries.some((e) => e.isIntersecting)) return;
      done = true;
      observer.disconnect();
      try {
        const page = await pdf.getPage(number);
        if (!canvas.current) return;
        const base = page.getViewport({ scale: 1 });
        const viewport = page.getViewport({ scale: 76 / base.width });
        canvas.current.width = viewport.width;
        canvas.current.height = viewport.height;
        task = page.render({ canvas: canvas.current, canvasContext: canvas.current.getContext('2d'), viewport });
        await task.promise;
      } catch {}
    });
    if (canvas.current) observer.observe(canvas.current);
    return () => {
      done = true;
      observer.disconnect();
      task?.cancel();
    };
  }, [pdf, number]);
  return <canvas ref={canvas} width={76} height={100} />;
}
export default function PdfReader({ blob, annotations, editable, onChange, onPosition, initialPage = 1 }: Props) {
  const container = useRef<HTMLDivElement>(null);
  const viewerRef = useRef<any>();
  const busRef = useRef<any>();
  const pdfRef = useRef<any>();
  const [error, setError] = useState('');
  const [page, setPage] = useState(initialPage);
  const [count, setCount] = useState(0);
  const [scale, setScale] = useState('page-width');
  const [rotation, setRotation] = useState(0);
  const [outline, setOutline] = useState<any[]>([]);
  const [sidebar, setSidebar] = useState('pages');
  const linkRef = useRef<any>();
  const [tool, setTool] = useState('read');
  const [showAnnotations, setShowAnnotations] = useState(true);
  const [singlePage, setSinglePage] = useState(false);
  const [targets, setTargets] = useState<{ number: number; div: HTMLElement; view: any }[]>([]);
  const [note, setNote] = useState<{ annotation: Annotation; editing: boolean }>();
  const values = useRef(annotations?.annotations || []);
  values.current = annotations?.annotations || [];
  useEffect(() => {
    let disposed = false;
    let task: any;
    let viewer: any;
    (async () => {
      try {
        const data = new Uint8Array(await blob.arrayBuffer());
        if (disposed) return;
        task = pdfjs.getDocument({
          data,
          useSystemFonts: false,
          cMapUrl: `${import.meta.env.BASE_URL}archive-renderer/cmaps/`,
          cMapPacked: true,
          standardFontDataUrl: `${import.meta.env.BASE_URL}archive-renderer/standard_fonts/`,
          wasmUrl: `${import.meta.env.BASE_URL}archive-renderer/wasm/`,
          iccUrl: `${import.meta.env.BASE_URL}archive-renderer/iccs/`,
        });
        const pdf = await task.promise;
        if (disposed) return;
        const eventBus = new EventBus();
        const linkService = new PDFLinkService({ eventBus });
        const findController = new PDFFindController({ eventBus, linkService });
        viewer = new PDFViewer({
          container: container.current!,
          viewer: container.current!.querySelector<HTMLDivElement>('.pdfViewer')!,
          eventBus,
          linkService,
          findController,
          textLayerMode: 1,
          annotationMode: 1,
        });
        linkRef.current = linkService;
        const flatten = (items: any[]): any[] => items.flatMap((n) => [n, ...flatten(n.items || [])]);
        setOutline(flatten((await pdf.getOutline()) || []));
        linkService.setViewer(viewer);
        linkService.setDocument(pdf);
        viewer.setDocument(pdf);
        setScale('page-width');
        setRotation(0);
        setSinglePage(false);
        viewerRef.current = viewer;
        busRef.current = eventBus;
        pdfRef.current = pdf;
        setCount(pdf.numPages);
        const refresh = () => {
          if (disposed) return;
          const next: any[] = [];
          for (let i = 0; i < pdf.numPages; i++) {
            const view = viewer.getPageView(i);
            if (view?.div?.querySelector('canvas')) next.push({ number: i + 1, div: view.div, view });
          }
          setTargets(next);
        };
        eventBus.on('pagesinit', () => {
          viewer.currentScaleValue = 'page-width';
          viewer.currentPageNumber = Math.min(Math.max(initialPage, 1), pdf.numPages);
        });
        const resize = new ResizeObserver(() => {
          if (['page-width', 'page-fit'].includes(viewer.currentScaleValue)) viewer.currentScaleValue = viewer.currentScaleValue;
        });
        resize.observe(container.current!);
        (viewer as any).__archiveResize = resize;
        eventBus.on('pagerendered', (event: any) => {
          if (event.error) setError(`第 ${event.pageNumber} 页无法渲染，请检查文件。原件已保留。`);
          refresh();
        });
        eventBus.on('scalechanging', () => setTimeout(refresh, 0));
        eventBus.on('rotationchanging', () => setTimeout(refresh, 0));
        eventBus.on('pagechanging', (e: any) => {
          if (!disposed) {
            setPage(e.pageNumber);
            onPosition?.(e.pageNumber);
          }
        });
      } catch (e) {
        if (!disposed) setError(errorText(e));
      }
    })();
    return () => {
      disposed = true;
      viewer?.__archiveResize?.disconnect();
      viewer?.setDocument(null);
      task?.destroy();
      setTargets([]);
    };
  }, [blob]);
  useEffect(() => {
    if (!editable) setTool('read');
  }, [editable]);
  const navigate = (p: number) => {
    if (viewerRef.current) viewerRef.current.currentPageNumber = Math.min(Math.max(1, p), count);
  };
  const transform = (a: Annotation, view: any) => a.rects.map((r) => annotationToViewport(r, view.viewport));
  const select = (event: React.MouseEvent) => {
    if (!editable || tool === 'read' || (event.target instanceof HTMLElement && event.target.closest('.archive-pdf-annotation'))) return;
    const target = (event.target as HTMLElement).closest('.page') as HTMLElement;
    if (!target) return;
    const n = Number(target.dataset.pageNumber);
    const view = viewerRef.current?.getPageView(n - 1);
    if (!view) return;
    const box = target.getBoundingClientRect();
    const rect = (r: { left: number; top: number; right: number; bottom: number }) =>
      viewportToAnnotation({ left: r.left - box.left, top: r.top - box.top, right: r.right - box.left, bottom: r.bottom - box.top }, view.viewport);
    let rects: ReturnType<typeof rect>[] = [];
    const selection = window.getSelection();
    if (tool === 'note') rects = [rect({ left: event.clientX, top: event.clientY, right: event.clientX + 22, bottom: event.clientY + 22 })];
    else if (selection && selection.rangeCount && !selection.isCollapsed)
      rects = Array.from(selection.getRangeAt(0).getClientRects())
        .filter((r) => r.width > 1 && r.height > 1 && r.top >= box.top && r.bottom <= box.bottom)
        .slice(0, 100)
        .map(rect);
    if (!rects.length) return;
    const annotation: Annotation = { id: crypto.randomUUID(), type: tool as Annotation['type'], page: n, rects, text: selection?.toString() || '', color: '#ffe58f' };
    selection?.removeAllRanges();
    if (tool === 'note') setNote({ annotation: { ...annotation, text: '' }, editing: true });
    else onChange?.({ annotations: [...values.current, annotation] });
  };
  return (
    <div className='archive-pdf-reader'>
      <div className='archive-pdf-toolbar'>
        <Space wrap>
          <Button
            size='small'
            onClick={() => {
              const next = !singlePage;
              setSinglePage(next);
              if (viewerRef.current) viewerRef.current.scrollMode = next ? 3 : 0;
            }}
          >
            {singlePage ? '连续阅读' : '单页阅读'}
          </Button>
          <Space size={4}>
            <Switch size='small' checked={showAnnotations} onChange={setShowAnnotations} />
            <span>批注</span>
          </Space>
          <Button size='small' onClick={() => navigate(page - 1)} disabled={page <= 1}>
            上一页
          </Button>
          <InputNumber size='small' min={1} max={count || 1} value={page} style={{ width: 64 }} onChange={(v) => v && navigate(v)} />
          <span>/ {count}</span>
          <Button size='small' onClick={() => navigate(page + 1)} disabled={page >= count}>
            下一页
          </Button>
          <Select
            size='small'
            value={scale}
            style={{ width: 115 }}
            onChange={(v) => {
              setScale(v);
              if (viewerRef.current) viewerRef.current.currentScaleValue = v;
            }}
            options={[
              { value: 'page-width', label: '适合宽度' },
              { value: 'page-fit', label: '整页' },
              ...['0.75', '1', '1.25', '1.5', '2'].map((v) => ({ value: v, label: `${Number(v) * 100}%` })),
            ]}
          />
          <Button
            size='small'
            onClick={() => {
              const r = (rotation + 90) % 360;
              setRotation(r);
              if (viewerRef.current) viewerRef.current.pagesRotation = r;
            }}
          >
            旋转
          </Button>
          <Input.Search
            size='small'
            placeholder='搜索文档文字'
            style={{ width: 220 }}
            onSearch={(query) =>
              busRef.current?.dispatch('find', {
                source: container.current,
                type: 'again',
                query,
                caseSensitive: false,
                entireWord: false,
                highlightAll: true,
                findPrevious: false,
              })
            }
          />
          {editable && (
            <Select
              size='small'
              value={tool}
              style={{ width: 145 }}
              onChange={setTool}
              options={[
                { value: 'read', label: '选择 / 阅读' },
                { value: 'highlight', label: '选中文字高亮' },
                { value: 'strikeout', label: '选中文字划线' },
                { value: 'note', label: '点击添加批注' },
              ]}
            />
          )}
        </Space>
      </div>
      {error && <Alert type='error' message={error} showIcon />}
      <div className='archive-pdf-stage'>
        <div className='archive-pdf-thumbnails'>
          <Select
            size='small'
            value={sidebar}
            onChange={setSidebar}
            options={[
              { value: 'pages', label: '缩略图' },
              { value: 'outline', label: '目录' },
            ]}
          />
          {sidebar === 'outline' ? (
            outline.length ? (
              outline.map((item, i) => (
                <Button key={i} type='text' title={item.title} onClick={() => linkRef.current?.goToDestination(item.dest)}>
                  {item.title}
                </Button>
              ))
            ) : (
              <div className='archive-hint'>文档无目录</div>
            )
          ) : (
            Array.from({ length: count }, (_, i) => (
              <button className={`archive-thumbnail ${page === i + 1 ? 'active' : ''}`} key={i} onClick={() => navigate(i + 1)}>
                {pdfRef.current && <Thumbnail pdf={pdfRef.current} number={i + 1} />}
                <span>第 {i + 1} 页</span>
              </button>
            ))
          )}
        </div>
        <div ref={container} className='archive-pdf-container' onMouseUp={select}>
          <div className='pdfViewer' />
        </div>
      </div>
      {targets.flatMap(({ number, div, view }) =>
        values.current
          .filter((a) => showAnnotations && a.page === number)
          .flatMap((a) =>
            transform(a, view).map((r, i) =>
              createPortal(
                <Tooltip title={a.text || (a.type === 'highlight' ? '高亮' : '划线')}>
                  <button
                    className={`archive-pdf-annotation ${a.type}`}
                    style={{
                      ...r,
                      background: a.type === 'strikeout' ? 'transparent' : a.color,
                      borderBottom: a.type === 'strikeout' ? `2px solid ${a.color === '#ffe58f' ? '#ff4d4f' : a.color}` : undefined,
                    }}
                    onMouseUp={(e) => e.stopPropagation()}
                    onClick={(e) => {
                      e.stopPropagation();
                      setNote({ annotation: a, editing: false });
                    }}
                    aria-label={`第${a.page}页批注：${a.text}`}
                  >
                    {a.type === 'note' ? '✎' : ''}
                  </button>
                </Tooltip>,
                div,
                `${a.id}-${i}`,
              ),
            ),
          ),
      )}
      <Modal
        title={note?.editing ? '添加批注' : `批注 · ${note?.annotation.author_name || '当前编辑'}`}
        visible={!!note}
        onCancel={() => setNote(undefined)}
        onOk={() => {
          if (!note) return;
          onChange?.({ annotations: note.editing ? [...values.current, note.annotation] : values.current.map((a) => (a.id === note.annotation.id ? note.annotation : a)) });
          setNote(undefined);
        }}
        okButtonProps={{ disabled: !editable }}
        okText='保存批注'
        cancelText='关闭'
        footer={
          editable ? (
            <Space>
              <Button
                danger
                onClick={() => {
                  onChange?.({ annotations: values.current.filter((a) => a.id !== note?.annotation.id) });
                  setNote(undefined);
                }}
              >
                删除批注
              </Button>
              <Button onClick={() => setNote(undefined)}>取消</Button>
              <Button
                type='primary'
                onClick={() => {
                  if (!note) return;
                  onChange?.({ annotations: note.editing ? [...values.current, note.annotation] : values.current.map((a) => (a.id === note.annotation.id ? note.annotation : a)) });
                  setNote(undefined);
                }}
              >
                保存批注
              </Button>
            </Space>
          ) : null
        }
      >
        <Input.TextArea
          rows={5}
          value={note?.annotation.text}
          readOnly={!editable}
          onChange={(e) => note && setNote({ ...note, annotation: { ...note.annotation, text: e.target.value } })}
          maxLength={10000}
        />
      </Modal>
    </div>
  );
}
