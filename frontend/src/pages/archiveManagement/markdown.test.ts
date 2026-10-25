import { projectMarkdown, restoreMarkdown } from './markdown';
import { Body, emptyBody } from './types';
function fixture(): Body {
  return {
    ...emptyBody(),
    data: {
      header: [{ value: '页眉' }],
      footer: [{ value: '页脚' }],
      main: [
        { type: 'title', value: '', level: 'first', valueList: [{ value: '归档报告' }] },
        { value: '\n原文段落，保留内容。' },
        {
          type: 'table',
          value: '',
          colgroup: [{ width: 200 }, { width: 300 }],
          trList: [
            {
              height: 40,
              tdList: [
                { value: [{ value: '项目' }], colspan: 1, rowspan: 1 },
                { value: [{ value: '结果' }], colspan: 1, rowspan: 1 },
              ],
            },
            {
              height: 40,
              tdList: [
                { value: [{ value: '设备' }], colspan: 1, rowspan: 1 },
                { value: [{ value: '正常' }], colspan: 1, rowspan: 1 },
              ],
            },
          ],
        },
        { type: 'table', value: '', extension: { archiveNodeID: 'merged' }, trList: [{ height: 40, tdList: [{ value: [{ value: '合并内容' }], colspan: 2, rowspan: 1 }] }] },
        { type: 'image', value: 'archive-asset:7', width: 200, height: 80 },
        { type: 'image', value: 'archive-asset:8', width: 80, height: 80, imgDisplay: 'float-top', extension: { archiveNodeID: 'float' } },
      ],
    },
  };
}
test('basic text/table/image are editable while layout and complex objects survive', () => {
  const input = fixture();
  const p = projectMarkdown(input);
  const result = restoreMarkdown(p, p.markdown.replace('原文段落', '修改后的段落').replace('| 正常 |', '| 异常 |'));
  expect(JSON.stringify(result.data.main)).toContain('修改后的段落');
  expect(JSON.stringify(result.data.main)).toContain('异常');
  expect(result.data.main.find((x) => x.extension?.archiveNodeID === 'merged')).toEqual(input.data.main[3]);
  expect(result.data.main.find((x) => x.extension?.archiveNodeID === 'float')).toEqual(input.data.main[5]);
  expect(result.data.main.find((x) => x.type === 'image' && x.value === 'archive-asset:7')).toMatchObject({ width: 200, height: 80 });
  expect(result.options).toEqual(input.options);
  expect(result.data.header).toEqual(input.data.header);
  expect(result.data.footer).toEqual(input.data.footer);
  expect(input).toEqual(fixture());
});
test.each(['missing', 'duplicate', 'unknown', 'reordered'])('rejects %s protected blocks without changing original', (kind) => {
  const p = projectMarkdown(fixture());
  const original = JSON.stringify(p.baseline);
  let text = p.markdown;
  if (kind === 'missing') text = text.replace('<!--archive-block:b1-->', '');
  if (kind === 'duplicate') text += '\n<!--archive-block:b1-->';
  if (kind === 'unknown') text += '\n<!--archive-block:evil-->';
  if (kind === 'reordered')
    text = text.replace('archive-block:b1', 'archive-block:tmp').replace('archive-block:b2', 'archive-block:b1').replace('archive-block:tmp', 'archive-block:b2');
  expect(() => restoreMarkdown(p, text)).toThrow('只读占位块');
  expect(JSON.stringify(p.baseline)).toBe(original);
});
test('rejects raw HTML, script links and external images', () => {
  const p = projectMarkdown(emptyBody());
  expect(() => restoreMarkdown(p, '<iframe src="https://example.com"></iframe>')).toThrow();
  expect(() => restoreMarkdown(p, '[bad](javascript:alert)')).toThrow();
  expect(() => restoreMarkdown(p, '![bad](https://example.com/image.png)')).toThrow();
});

test('reused image assets keep each occurrence layout after Markdown deletion or reorder', () => {
  const b = emptyBody();
  b.data.main = [{ type: 'image', value: 'archive-asset:7', width: 200, height: 80 }, { value: '\n' }, { type: 'image', value: 'archive-asset:7', width: 100, height: 40 }];
  const p = projectMarkdown(b);
  const all = restoreMarkdown(p, p.markdown).data.main.filter((n) => n.type === 'image');
  expect(all.map((n) => n.width)).toEqual([200, 100]);
  const refs = p.markdown.match(/!\[[^\]]*\]\([^)]*\)/g)!;
  const moved = restoreMarkdown(p, `${refs[1]}\n\n${refs[0]}`).data.main.filter((n) => n.type === 'image');
  expect(moved.map((n) => n.width)).toEqual([100, 200]);
  expect(restoreMarkdown(p, refs[1]).data.main.find((n) => n.type === 'image')).toMatchObject({ width: 100, height: 40, value: 'archive-asset:7' });
});

test('mixed font and size survive text edits without turning ordinary paragraphs into read-only blocks', () => {
  const b = emptyBody();
  b.data.main = [
    { value: '第一段', font: 'Noto Sans CJK SC', size: 18 },
    { value: '第二段', font: 'Liberation Sans', size: 14 },
  ];
  const p = projectMarkdown(b);
  expect(p.blocks).toHaveLength(0);
  const result = restoreMarkdown(p, p.markdown.replace('第一段', '第一段新增'));
  const first = result.data.main.filter((n) => n.font === 'Noto Sans CJK SC');
  const second = result.data.main.filter((n) => n.font === 'Liberation Sans');
  expect(first.map((n) => n.value).join('')).toContain('第一段新增');
  expect(first.every((n) => n.size === 18)).toBe(true);
  expect(second.map((n) => n.value).join('')).toBe('第二段');
  expect(second.every((n) => n.size === 14)).toBe(true);
});

test('Word default text colors and ordinary table cells stay editable with formatting metadata', () => {
  const b = emptyBody();
  b.data.main = [
    { type: 'title', value: '', level: 'first', rowFlex: 'center', valueList: [{ type: 'text', value: '标题', size: 26, color: '#000000' }] },
    { type: 'text', value: '正文', size: 14, color: '#000000', underline: true },
  ];
  const p = projectMarkdown(b);
  expect(p.blocks).toHaveLength(0);
  const r = restoreMarkdown(p, p.markdown.replace('正文', '正文增加'));
  expect(JSON.stringify(r.data.main)).toContain('正文增加');
  const text = r.data.main.filter((n) => n.value?.includes('正文'))[0];
  expect(text).toMatchObject({ color: '#000000', size: 14, underline: true });
  expect(r.data.main.find((n) => n.type === 'title').valueList[0]).toMatchObject({ rowFlex: 'center', color: '#000000', size: 26 });
});
