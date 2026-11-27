import React, { useEffect, useState } from 'react';
import { Alert, Button, Input, Select, Space, Spin, Table, Tag, Tooltip } from 'antd';
import { api, errorText } from './services';

type Cell = { address: string; row: number; column: number; value: string; raw: string; formula?: string; missing_cache?: boolean; type: string };
type Sheet = { name: string; hidden: boolean; rows: number; columns: number; hidden_rows: number[]; hidden_columns: number[]; merges: string[] };
type Result = { sheets?: Sheet[]; rows: Cell[][]; warnings: string[]; has_more: boolean; matched_rows: number; range: string };
function letter(column: number): string {
  let name = '';
  for (let n = column; n > 0; n = Math.floor((n - 1) / 26)) name = String.fromCharCode(65 + ((n - 1) % 26)) + name;
  return name;
}
export default function WorkbookReader({ documentID, versionID }: { documentID: number; versionID: number }) {
  const initial = new URLSearchParams(window.location.search);
  const [sheets, setSheets] = useState<Sheet[]>([]);
  const [sheet, setSheet] = useState(initial.get('sheet') || '');
  const [range, setRange] = useState(initial.get('range') || '');
  const [activeRange, setActiveRange] = useState(initial.get('range') || '');
  const [offset, setOffset] = useState(0);
  const [data, setData] = useState<Result>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [reload, setReload] = useState(0);
  const base = `/documents/${documentID}/workbook?version_id=${versionID}`;
  useEffect(() => {
    let alive = true;
    setError('');
    setLoading(true);
    api<Result>(base)
      .then((r) => {
        if (!alive) return;
        const list = r.sheets || [];
        setSheets(list);
        const selected = list.find((s) => s.name === sheet) || list[0];
        if (selected) {
          setSheet(selected.name);
          if (!activeRange) {
            const value = `A1:${letter(Math.max(selected.columns, 1))}${Math.max(selected.rows, 1)}`;
            setRange(value);
            setActiveRange(value);
          }
        }
      })
      .catch((e) => {
        if (alive) setError(errorText(e));
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [base, reload]);
  useEffect(() => {
    if (!sheet || !activeRange) return;
    let alive = true;
    setError('');
    setData(undefined);
    setLoading(true);
    api<Result>(`${base}&operation=read&sheet=${encodeURIComponent(sheet)}&range=${encodeURIComponent(activeRange)}&offset=${offset}&limit=50`)
      .then((r) => {
        if (alive) setData(r);
      })
      .catch((e) => {
        if (alive) setError(errorText(e));
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [base, sheet, activeRange, offset, reload]);
  const selected = sheets.find((s) => s.name === sheet);
  return (
    <div>
      <Alert type='info' showIcon message='Excel 只读预览；原件、历史版本和下载保持不变。单元格按保存时的值展示，公式不重新计算；图片、图表及打印版式请查看原件。' />
      <Space wrap className='archive-workbook-controls' style={{ margin: '16px 0', display: 'flex' }}>
        <Select
          aria-label='工作表'
          style={{ width: 200, maxWidth: '100%' }}
          value={sheet || undefined}
          options={sheets.map((s) => ({ value: s.name, label: s.name + (s.hidden ? '（隐藏）' : '') }))}
          onChange={(value) => {
            setSheet(value);
            const s = sheets.find((item) => item.name === value)!;
            const r = `A1:${letter(Math.max(s.columns, 1))}${Math.max(s.rows, 1)}`;
            setRange(r);
            setActiveRange(r);
            setOffset(0);
          }}
        />
        <Input aria-label='单元格范围' style={{ width: 200, maxWidth: '100%' }} value={range} onChange={(e) => setRange(e.target.value)} placeholder='A1:D100' />
        <Button
          onClick={() => {
            setOffset(0);
            setActiveRange(range);
            setReload((v) => v + 1);
          }}
        >
          定位范围
        </Button>
        <span>{selected ? `${selected.rows} 行 · ${selected.columns} 列` : '暂无工作表'}</span>
      </Space>
      {error && <Alert type='error' message={error} action={<Button onClick={() => setReload((v) => v + 1)}>重试</Button>} />}
      {selected && (
        <div style={{ marginBottom: 12, overflowWrap: 'anywhere' }}>
          {selected.hidden && <Tag>隐藏工作表</Tag>}
          隐藏行：{selected.hidden_rows.join(', ') || '无'}；隐藏列：{selected.hidden_columns.map(letter).join(', ') || '无'}；合并范围：{selected.merges.join(', ') || '无'}
          。合并值保留在左上角单元格，预览不复制填充。
        </div>
      )}
      <Spin spinning={loading}>
        <Table
          size='small'
          bordered
          pagination={false}
          rowKey='row'
          scroll={{ x: Math.max(700, (selected?.columns || 1) * 150), y: 520 }}
          dataSource={(data?.rows || []).map((cells) => ({ row: cells[0].row, cells }))}
          columns={[
            {
              title: '行',
              width: 70,
              fixed: 'left' as const,
              dataIndex: 'row',
              render: (row: number) => (
                <span>
                  {row}
                  {selected?.hidden_rows.includes(row) ? '（隐藏）' : ''}
                </span>
              ),
            },
            ...Array.from({ length: selected?.columns || 0 }, (_, index) => ({
              title: letter(index + 1) + (selected?.hidden_columns.includes(index + 1) ? '（隐藏）' : ''),
              width: 150,
              key: String(index),
              render: (_: unknown, row: { cells: Cell[] }) => {
                const c = row.cells.find((v) => v.column === index + 1);
                return c ? (
                  <Tooltip title={`${c.address} · 原始值 ${c.raw}${c.formula ? ` · 公式 ${c.formula}（缓存值）` : ''}`}>
                    <span style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', color: c.missing_cache || c.type === 'e' ? '#cf1322' : undefined }}>{c.value}</span>
                  </Tooltip>
                ) : (
                  ''
                );
              },
            })),
          ]}
        />
      </Spin>
      <Space wrap style={{ margin: '16px 0' }}>
        <Button disabled={offset === 0 || loading} onClick={() => setOffset(Math.max(0, offset - 50))}>
          上一页
        </Button>
        <span>
          指定范围非空行 {data?.matched_rows || 0}；当前显示 {offset + (data?.rows.length ? 1 : 0)}–{offset + (data?.rows.length || 0)}，含隐藏数据
        </span>
        <Button disabled={!data?.has_more || loading} onClick={() => setOffset(offset + (data?.rows.length || 50))}>
          下一页
        </Button>
      </Space>
    </div>
  );
}
