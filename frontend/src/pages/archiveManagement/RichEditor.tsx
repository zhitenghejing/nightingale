import React, { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
import { Alert, Button, Input, InputNumber, Modal, Select, Space, Tooltip, Upload, message } from 'antd';
import Editor, { EditorMode } from '@hufe921/canvas-editor';
import docxPlugin from '@hufe921/canvas-editor-plugin-docx';
import { Body, Lease } from './types';
import { DocumentAssets } from './assets';
import { errorText } from './services';
export interface RichHandle {
  getBody(): Body;
  importWord(buffer: ArrayBuffer): Promise<void>;
}
interface Props {
  body: Body;
  assets: DocumentAssets;
  archiveID: number;
  documentID: number;
  lease?: Lease;
  readOnly?: boolean;
  onChange?: () => void;
  onReady?: () => void;
  wordBuffer?: ArrayBuffer;
}
const clone = (x: any) => JSON.parse(JSON.stringify(x));
const RichEditor = forwardRef<RichHandle, Props>(({ body, assets, archiveID, documentID, lease, readOnly, onChange, onReady, wordBuffer }, ref) => {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<Editor>();
  const changed = useRef(onChange);
  changed.current = onChange;
  const [ready, setReady] = useState(false);
  const [error, setError] = useState('');
  const [pages, setPages] = useState(1);
  const [settings, setSettings] = useState(false);
  const [layout, setLayout] = useState<any>({});
  const [catalog, setCatalog] = useState<any[]>([]);
  const [toc, setToc] = useState(false);
  useEffect(() => {
    let disposed = false;
    (async () => {
      try {
        await document.fonts.load('16px "Noto Sans CJK SC"');
        await Promise.all(['16px', 'bold 16px', 'italic 16px', 'italic bold 16px'].map((style) => document.fonts.load(`${style} "Liberation Sans"`)));
        const display = await assets.display(body);
        if (disposed || !host.current) return;
        const e = new Editor(host.current, display.data, {
          ...display.options,
          mode: readOnly ? EditorMode.READONLY : EditorMode.EDIT,
          scale: 1,
          locale: 'zhCN',
          scrollContainerSelector: '.archive-canvas-scroll',
        });
        e.use(docxPlugin);
        editor.current = e;
        e.listener.contentChange = () => changed.current?.();
        e.listener.pageSizeChange = setPages;
        if (!readOnly && lease)
          e.override.pasteImage = async (f) => {
            try {
              const image = await assets.insertImage(f instanceof File ? f : new File([f], 'clipboard.png', { type: f.type }), archiveID, documentID, lease);
              e.command.executeImage(image);
            } catch (err) {
              message.error(errorText(err));
            }
            return { preventDefault: true };
          };
        if (wordBuffer) await (e.command as any).executeImportDocx({ arrayBuffer: wordBuffer });
        setReady(true);
        onReady?.();
      } catch (err) {
        setError(errorText(err));
      }
    })();
    return () => {
      disposed = true;
      editor.current?.destroy();
      editor.current = undefined;
    };
  }, []);
  useEffect(() => {
    editor.current?.command.executeMode(readOnly ? EditorMode.READONLY : EditorMode.EDIT);
  }, [readOnly]);
  useImperativeHandle(ref, () => ({
    getBody: () => {
      if (!editor.current) throw new Error('编辑器尚未准备好');
      return editor.current.command.getValue() as Body;
    },
    importWord: async (buffer) => {
      if (!editor.current) throw new Error('编辑器尚未准备好');
      await (editor.current.command as any).executeImportDocx({ arrayBuffer: buffer });
      changed.current?.();
    },
  }));
  const call = (name: string, ...args: any[]) => {
    const e: any = editor.current;
    if (!e) return;
    e.command[name](...args);
  };
  const tool = (text: string, command: string, ...args: any[]) => (
    <Tooltip title={text} key={command + text}>
      <Button size='small' disabled={!ready || readOnly} onMouseDown={(e) => e.preventDefault()} onClick={() => call(command, ...args)}>
        {text}
      </Button>
    </Tooltip>
  );
  const openSettings = () => {
    const v = editor.current!.command.getValue();
    setLayout({
      width: v.options.width,
      height: v.options.height,
      margins: v.options.margins,
      header: v.data.header?.map((n: any) => n.value || '').join(''),
      footer: v.data.footer?.map((n: any) => n.value || '').join(''),
      pageNumber: !v.options.pageNumber?.disabled,
      editHeader: false,
      editFooter: false,
    });
    setSettings(true);
  };
  const saveSettings = () => {
    const e = editor.current!;
    e.command.executePaperSize(layout.width, layout.height);
    e.command.executeSetPaperMargin(layout.margins);
    e.command.executeUpdateOptions({ pageNumber: { ...e.command.getOptions().pageNumber, disabled: !layout.pageNumber } });
    if (layout.editHeader) e.command.executeSetValue({ header: [{ value: layout.header, size: 11 }] });
    if (layout.editFooter) e.command.executeSetValue({ footer: [{ value: layout.footer, size: 11 }] });
    changed.current?.();
    setSettings(false);
  };
  const catalogItems = (items: any[]): any[] => items.flatMap((n) => [n, ...catalogItems(n.subCatalog || [])]);
  return (
    <div className='archive-rich-editor'>
      {error && <Alert type='error' showIcon message={error} />}
      <div className='archive-office-toolbar'>
        <Space wrap size={4}>
          {tool('撤销', 'executeUndo')}
          {tool('重做', 'executeRedo')}
          <Select
            size='small'
            disabled={readOnly || !ready}
            defaultValue='正文'
            style={{ width: 105 }}
            onChange={(v) => call('executeTitle', v === '正文' ? null : v)}
            options={[{ value: '正文', label: '正文' }, ...['first', 'second', 'third', 'fourth'].map((v, i) => ({ value: v, label: `标题 ${i + 1}` }))]}
          />
          <Select
            size='small'
            disabled={readOnly || !ready}
            defaultValue='Noto Sans CJK SC'
            style={{ width: 130 }}
            onChange={(v) => call('executeFont', v)}
            options={[
              { value: 'Noto Sans CJK SC', label: '思源黑体' },
              { value: 'Liberation Sans', label: 'Liberation Sans' },
            ]}
          />
          <InputNumber size='small' disabled={readOnly || !ready} min={5} max={72} defaultValue={16} style={{ width: 65 }} onChange={(v) => v && call('executeSize', v)} />
          {tool('加粗', 'executeBold')}
          {tool('斜体', 'executeItalic')}
          {tool('下划线', 'executeUnderline')}
          {tool('删除线', 'executeStrikeout')}
          <input aria-label='文字颜色' disabled={readOnly} type='color' onChange={(e) => call('executeColor', e.target.value)} />
          {tool('左对齐', 'executeRowFlex', 'left')}
          {tool('居中', 'executeRowFlex', 'center')}
          {tool('右对齐', 'executeRowFlex', 'right')}
          {tool('项目符号', 'executeList', 'ul', 'disc')}
          {tool('编号', 'executeList', 'ol', 'decimal')}
          <Select
            size='small'
            placeholder='行距'
            style={{ width: 80 }}
            disabled={readOnly}
            onChange={(v) => call('executeRowMargin', v)}
            options={[1, 1.5, 2].map((v) => ({ value: v, label: String(v) }))}
          />
        </Space>
        <Space wrap size={4}>
          <Button
            size='small'
            disabled={readOnly || !ready}
            onClick={() => {
              let rows = 3;
              let cols = 3;
              Modal.confirm({
                title: '插入表格',
                content: (
                  <Space>
                    行
                    <InputNumber
                      min={1}
                      max={50}
                      defaultValue={3}
                      onChange={(v) => {
                        rows = v || 3;
                      }}
                    />
                    列
                    <InputNumber
                      min={1}
                      max={12}
                      defaultValue={3}
                      onChange={(v) => {
                        cols = v || 3;
                      }}
                    />
                  </Space>
                ),
                onOk: () => call('executeInsertTable', rows, cols),
              });
            }}
          >
            表格
          </Button>
          <Upload
            disabled={readOnly || !lease}
            showUploadList={false}
            accept='.png,.jpg,.jpeg'
            beforeUpload={(file) => {
              assets
                .insertImage(file, archiveID, documentID, lease!)
                .then((image) => call('executeImage', image))
                .catch((e) => message.error(errorText(e)));
              return false;
            }}
          >
            <Button size='small' disabled={readOnly || !lease}>
              图片
            </Button>
          </Upload>
          <Button
            size='small'
            disabled={readOnly || !ready}
            onClick={() => {
              let url = '';
              let value = '';
              Modal.confirm({
                title: '插入链接',
                content: (
                  <Space direction='vertical'>
                    <Input
                      placeholder='显示文字'
                      onChange={(e) => {
                        value = e.target.value;
                      }}
                    />
                    <Input
                      placeholder='https://…'
                      onChange={(e) => {
                        url = e.target.value;
                      }}
                    />
                  </Space>
                ),
                onOk: () => {
                  if (!/^(https?:\/\/|mailto:)/.test(url)) return Promise.reject(message.error('请输入 http、https 或 mailto 链接'));
                  call('executeHyperlink', { value: '', type: 'hyperlink', url, valueList: [{ value: value || url }] });
                },
              });
            }}
          >
            链接
          </Button>
          {tool('分页符', 'executePageBreak')}
          <Button size='small' disabled={readOnly || !ready} onClick={openSettings}>
            页面 / 页眉页脚
          </Button>
          <Button
            size='small'
            disabled={readOnly || !ready}
            onClick={() => {
              let title = '';
              Modal.confirm({
                title: '插入封面',
                content: (
                  <Input
                    placeholder='封面标题'
                    onChange={(e) => {
                      title = e.target.value;
                    }}
                  />
                ),
                onOk: () =>
                  call('executeInsertElementList', [
                    { type: 'title', value: '', level: 'first', valueList: [{ value: title || '文档标题', size: 36 }], rowFlex: 'center' },
                    { value: '\n' },
                    { type: 'pageBreak', value: '' },
                  ]),
              });
            }}
          >
            封面
          </Button>
          <Button
            size='small'
            disabled={!ready}
            onClick={async () => {
              const c: any = await editor.current!.command.getCatalog();
              setCatalog(catalogItems(c || []));
              setToc(true);
            }}
          >
            目录
          </Button>
          <Input.Search
            size='small'
            placeholder='查找正文'
            style={{ width: 180 }}
            onSearch={(v) => {
              call('executeSearch', v || null);
              if (v) call('executeSearchNavigateNext');
            }}
          />
          <Button size='small' onClick={() => call('executeSearchNavigateNext')}>
            下一处
          </Button>
          <span className='archive-hint'>{pages} 页</span>
        </Space>
      </div>
      <div className='archive-canvas-scroll'>
        <div ref={host} className='archive-canvas' />
      </div>
      <Modal title='页面与页眉页脚' visible={settings} onCancel={() => setSettings(false)} onOk={saveSettings} okText='应用' cancelText='取消'>
        <Space direction='vertical' style={{ width: '100%' }}>
          <Space>
            纸张
            <Select
              value={`${layout.width}x${layout.height}`}
              style={{ width: 180 }}
              onChange={(v) => {
                const [width, height] = v.split('x').map(Number);
                setLayout({ ...layout, width, height });
              }}
              options={[
                { value: '794x1123', label: 'A4 纵向' },
                { value: '1123x794', label: 'A4 横向' },
                { value: '816x1056', label: 'Letter' },
              ]}
            />
          </Space>
          <div>页边距（像素，上 / 右 / 下 / 左）</div>
          <Space>
            {(layout.margins || []).map((v, i) => (
              <InputNumber key={i} min={12} max={200} value={v} onChange={(x) => setLayout({ ...layout, margins: layout.margins.map((n, j) => (j === i ? x : n)) })} />
            ))}
          </Space>
          <label>
            页眉
            <Input value={layout.header} onChange={(e) => setLayout({ ...layout, header: e.target.value, editHeader: true })} />
          </label>
          <label>
            页脚
            <Input value={layout.footer} onChange={(e) => setLayout({ ...layout, footer: e.target.value, editFooter: true })} />
          </label>
          <Select
            value={layout.pageNumber}
            onChange={(v) => setLayout({ ...layout, pageNumber: v })}
            options={[
              { value: true, label: '显示页码' },
              { value: false, label: '隐藏页码' },
            ]}
          />
          <div className='archive-hint'>复杂页眉页脚可在页面相应区域直接编辑。此处修改文字会替换该区域内容。</div>
        </Space>
      </Modal>
      <Modal title='文档目录' visible={toc} onCancel={() => setToc(false)} footer={null}>
        {catalog.length ? (
          <Space direction='vertical'>
            {catalog.map((c, i) => (
              <Button
                key={i}
                type='link'
                onClick={() => {
                  call('executeLocationCatalog', c.id);
                  setToc(false);
                }}
              >
                {c.name || c.title || `标题 ${i + 1}`}
              </Button>
            ))}
            {!readOnly && (
              <Button
                onClick={() => {
                  call('executeInsertElementList', [
                    { type: 'title', value: '', level: 'first', valueList: [{ value: '目录' }] },
                    ...catalog.flatMap((c) => [{ value: '\n' + (c.name || c.title || '') }]),
                    { value: '\n' },
                    { type: 'pageBreak', value: '' },
                  ]);
                  setToc(false);
                }}
              >
                在当前位置插入目录文字
              </Button>
            )}
          </Space>
        ) : (
          <div>文档尚无标题。先为正文设置标题层级即可生成目录。</div>
        )}
      </Modal>
    </div>
  );
});
export default RichEditor;
