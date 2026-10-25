import { preserveTextStyles } from './markdownStyles';
import { marked } from 'marked';
import { Body } from './types';
export interface Projection {
  markdown: string;
  baseline: Body;
  blocks: { id: string; node: any }[];
  tables: any[];
  images: Record<string, any>;
}
const clone = <T>(x: T): T => JSON.parse(JSON.stringify(x));
const levels = ['first', 'second', 'third', 'fourth', 'fifth', 'sixth'];
const escape = (s: string) =>
  s
    .replace(/\\/g, '\\\\')
    .replace(/([*_[\]`<>|])/g, '\\$1')
    .replace(/\u200b/g, '');
function complex(node: any): boolean {
  if (node.valueList?.some(complex)) return true;
  if (node.type === 'table')
    return node.trList?.some((r: any) => r.tdList.some((c: any) => c.colspan > 1 || c.rowspan > 1 || c.value?.some((n: any) => n.type === 'table' || complex(n))));
  if (node.type === 'image') return ['surround', 'float-top', 'float-bottom'].includes(node.imgDisplay);
  return Boolean((node.type && !['text', 'title', 'list', 'hyperlink', 'image', 'table'].includes(node.type)) || (node.type === 'list' && node.listStyle === 'checkbox'));
}
export function projectMarkdown(body: Body): Projection {
  const out: Projection = { markdown: '', baseline: clone(body), blocks: [], tables: [], images: {} };
  const render = (nodes: any[]): string =>
    nodes
      .map((n: any) => {
        if (complex(n)) {
          const id = `b${out.blocks.length + 1}`;
          out.blocks.push({ id, node: clone(n) });
          return `\n\n<!--archive-block:${id}-->\n\n`;
        }
        if (n.type === 'image') {
          const reference = `${n.value}#image:${Object.keys(out.images).length + 1}`;
          out.images[reference] = clone(n);
          return `![${escape(n.extension?.archiveAlt || '图片')}](${reference})`;
        }
        if (n.type === 'table') {
          out.tables.push(clone(n));
          const rows = (n.trList || []).map((r: any) => '| ' + r.tdList.map((c: any) => render(c.value || []).replace(/\n/g, '<br>')).join(' | ') + ' |');
          if (rows.length) rows.splice(1, 0, '| ' + (n.trList[0]?.tdList || []).map(() => '---').join(' | ') + ' |');
          return '\n\n' + rows.join('\n') + '\n\n';
        }
        const value = n.valueList ? render(n.valueList) : escape(n.value || '');
        if (n.type === 'title') return `\n\n${'#'.repeat(Math.max(1, levels.indexOf(n.level) + 1))} ${value.trim()}\n\n`;
        if (n.type === 'list')
          return (
            '\n' +
            value
              .split('\n')
              .map((s: string, i: number) => `${'  '.repeat(n.listLevel || 0)}${n.listType === 'ol' ? `${i + 1}.` : '-'} ${s}`)
              .join('\n') +
            '\n'
          );
        if (n.type === 'hyperlink') return `[${value}](${n.url})`;
        let text = value;
        if (n.bold) text = `**${text}**`;
        if (n.italic) text = `*${text}*`;
        if (n.strikeout) text = `~~${text}~~`;
        return text;
      })
      .join('');
  out.markdown = render(body.data.main).trim();
  return out;
}
const safeLink = (url: string) => /^(https?:\/\/|mailto:)/i.test(url);
export function restoreMarkdown(projection: Projection, source: string): Body {
  const wanted = projection.blocks.map((b) => b.id);
  const found = [...source.matchAll(/<!--archive-block:([^>]+)-->/g)].map((m) => m[1]);
  if (JSON.stringify(found) !== JSON.stringify(wanted)) throw new Error('只读占位块缺失、重复或顺序改变。原稿已保留，请恢复占位块后重试。');
  const allowedImages = projection.images;
  const inline = (tokens: any[], style: any = {}): any[] =>
    tokens.flatMap((t: any): any[] => {
      switch (t.type) {
        case 'text':
          return t.tokens ? inline(t.tokens, style) : [{ ...style, value: t.text }];
        case 'escape':
          return [{ ...style, value: t.text }];
        case 'strong':
          return inline(t.tokens, { ...style, bold: true });
        case 'em':
          return inline(t.tokens, { ...style, italic: true });
        case 'del':
          return inline(t.tokens, { ...style, strikeout: true });
        case 'codespan':
          return [{ ...style, value: t.text, font: 'monospace' }];
        case 'br':
          return [{ value: '\n' }];
        case 'link':
          if (!safeLink(t.href)) throw new Error('链接只允许 http、https 或 mailto');
          return [{ type: 'hyperlink', value: '', url: t.href, valueList: inline(t.tokens, style) }];
        case 'image':
          if (!/^archive-asset:\d+#image:\d+$/.test(t.href) || !allowedImages[t.href]) throw new Error('请在可视化模式插入图片；Markdown 图片必须引用原文中的受管图片');
          return [{ ...clone(allowedImages[t.href]), extension: { ...allowedImages[t.href].extension, archiveAlt: t.text }, imgDisplay: 'inline' }];
        default:
          throw new Error(`无法保留 Markdown 结构：${t.type}。原稿未改变。`);
      }
    });
  let tableIndex = 0;
  const parse = (text: string): any[] => {
    const nodes: any[] = [];
    const tokens: any[] = marked.lexer(text, { gfm: true, breaks: true });
    for (const t of tokens) {
      if (t.type === 'space') continue;
      if (t.type === 'paragraph' || t.type === 'text') {
        if (nodes.length) nodes.push({ value: '\n' });
        nodes.push(...inline(t.tokens || [{ type: 'text', text: t.text }]));
      } else if (t.type === 'heading') nodes.push({ type: 'title', value: '', level: levels[t.depth - 1], valueList: inline(t.tokens) }, { value: '\n' });
      else if (t.type === 'list')
        t.items.forEach((item: any) => {
          nodes.push({ type: 'list', value: '', listType: t.ordered ? 'ol' : 'ul', listStyle: t.ordered ? 'decimal' : 'disc', valueList: parse(item.text) }, { value: '\n' });
        });
      else if (t.type === 'table') {
        const original = projection.tables[tableIndex++];
        const count = t.header.length;
        nodes.push({
          ...(original || {}),
          type: 'table',
          value: '',
          width: original?.width || 650,
          colgroup: original?.colgroup?.length === count ? clone(original.colgroup) : Array.from({ length: count }, () => ({ width: 650 / count })),
          trList: [t.header, ...t.rows].map((cells: any[], ri: number) => ({
            height: original?.trList?.[ri]?.height || 36,
            tdList: cells.map((c: any, ci: number) => ({ ...(original?.trList?.[ri]?.tdList?.[ci] || {}), colspan: 1, rowspan: 1, value: inline(c.tokens) })),
          })),
        });
      } else if (t.type === 'code') nodes.push({ value: (nodes.length ? '\n' : '') + t.text + '\n', font: 'monospace' });
      else throw new Error(`暂不支持 ${t.type} 结构转换，原稿已保留。`);
    }
    return nodes;
  };
  const main: any[] = [];
  const matcher = /<!--archive-block:([^>]+)-->/g;
  let start = 0;
  let match: RegExpExecArray | null;
  while ((match = matcher.exec(source))) {
    main.push(...parse(source.slice(start, match.index)));
    main.push(clone(projection.blocks.find((b) => b.id === match![1])!.node));
    start = matcher.lastIndex;
  }
  main.push(...parse(source.slice(start)));
  return {
    ...clone(projection.baseline),
    data: { ...clone(projection.baseline.data), main: main.length ? preserveTextStyles(projection.baseline.data.main, main, complex) : [{ value: '' }] },
  };
}
