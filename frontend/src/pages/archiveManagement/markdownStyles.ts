// Markdown carries semantic formatting. Font/size/spacing stay in this shadow
// map; inserted text inherits adjacent formatting, as in a visual editor.
import { diffChars } from 'diff';
const keys = ['font', 'size', 'letterSpacing', 'rowMargin', 'color', 'highlight', 'underline', 'rowFlex', 'textDecoration'];
interface Run {
  start: number;
  end: number;
  style: Record<string, any>;
}
export function preserveTextStyles(before: any[], after: any[], protectedNode: (node: any) => boolean): any[] {
  const old: Run[] = [];
  let oldText = '';
  let newText = '';
  const visit = (nodes: any[], add: (node: any) => void, inherited: any = {}) => {
    for (const n of nodes) {
      if (protectedNode(n) || n.type === 'image') continue;
      if (n.type === 'table') for (const row of n.trList || []) for (const cell of row.tdList || []) visit(cell.value || [], add);
      else if (n.valueList) visit(n.valueList, add, { ...inherited, ...Object.fromEntries(keys.filter((k) => n[k] !== undefined).map((k) => [k, n[k]])) });
      else if (typeof n.value === 'string') add({ ...inherited, ...n });
    }
  };
  visit(before, (n) => {
    const text = n.value.replace(/\u200b/g, '');
    const start = oldText.length;
    oldText += text;
    if (text) old.push({ start, end: oldText.length, style: Object.fromEntries(keys.filter((k) => n[k] !== undefined).map((k) => [k, n[k]])) });
  });
  visit(after, (n) => {
    newText += n.value;
  });
  if (!old.length || !old.some((r) => Object.keys(r.style).length)) return after;
  const at = (offset: number): Run => {
    let low = 0;
    let high = old.length - 1;
    while (low < high) {
      const mid = (low + high) >>> 1;
      if (old[mid].end <= offset) low = mid + 1;
      else high = mid;
    }
    return old[low];
  };
  const deadline = Date.now() + 1500;
  let comparisons = 0;
  const changes: { value: string; added?: boolean; removed?: boolean }[] = diffChars(oldText, newText, {
    comparator: (a: string, b: string) => {
      if (++comparisons % 4096 === 0 && Date.now() > deadline) throw new Error('文档格式差异过大，未覆盖原稿。请减少本次 Markdown 改动，或在可视化模式继续编辑。');
      return a === b;
    },
  });
  const mapped: Run[] = [];
  let previous = 0;
  let current = 0;
  for (const change of changes) {
    if (change.removed) {
      previous += change.value.length;
      continue;
    }
    if (change.added) {
      mapped.push({ start: current, end: current + change.value.length, style: at(Math.max(0, previous - 1)).style });
      current += change.value.length;
      continue;
    }
    let remaining = change.value.length;
    while (remaining > 0) {
      const run = at(previous);
      const length = Math.min(remaining, Math.max(1, run.end - previous));
      mapped.push({ start: current, end: current + length, style: run.style });
      previous += length;
      current += length;
      remaining -= length;
    }
  }
  let cursor = 0;
  let runIndex = 0;
  const rewrite = (nodes: any[]): any[] =>
    nodes.flatMap((n): any[] => {
      if (protectedNode(n) || n.type === 'image') return [n];
      if (n.type === 'table') {
        n.trList?.forEach((r: any) =>
          r.tdList?.forEach((c: any) => {
            c.value = rewrite(c.value || []);
          }),
        );
        return [n];
      }
      if (n.valueList) {
        n.valueList = rewrite(n.valueList);
        return [n];
      }
      if (typeof n.value !== 'string' || !n.value) return [n];
      const out: any[] = [];
      let offset = 0;
      while (offset < n.value.length) {
        while (runIndex < mapped.length - 1 && mapped[runIndex].end <= cursor) runIndex++;
        const run = mapped[runIndex];
        const length = Math.min(n.value.length - offset, Math.max(1, (run?.end || newText.length) - cursor));
        const node = { ...n, value: n.value.slice(offset, offset + length) };
        for (const k of keys) if (node[k] === undefined && run?.style[k] !== undefined) node[k] = run.style[k];
        const last = out[out.length - 1];
        if (last && JSON.stringify({ ...last, value: '' }) === JSON.stringify({ ...node, value: '' })) last.value += node.value;
        else out.push(node);
        cursor += length;
        offset += length;
      }
      return out;
    });
  return rewrite(after);
}
