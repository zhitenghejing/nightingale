// Keep the popup on an HTML document: embedded browsers may download a PDF
// navigation instead of loading it, so its load event cannot drive printing.
export function openPrintWindow(): Window {
  const popup = window.open('', '_blank');
  if (!popup) throw new Error('浏览器阻止了打印窗口，请允许本站弹出窗口后重试');
  popup.document.open();
  popup.document.write(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>打印预览</title>
    <style>
      body { margin: 0; background: #eef0f3; font-family: sans-serif; }
      header { position: sticky; top: 0; padding: 16px; background: white; border-bottom: 1px solid #ddd; }
      button { margin-left: 16px; padding: 6px 20px; cursor: pointer; }
      article { margin: 16px auto; background: white; }
      canvas { display: block; width: 100%; height: 100%; }
      @media print {
        header { display: none; }
        body { background: white; }
        article { margin: 0; break-inside: avoid; break-after: page; }
        article:last-child { break-after: auto; }
      }
    </style></head><body><header><span role="status">正在准备打印内容…</span><button disabled>打印</button></header><main></main></body></html>`);
  popup.document.close();
  return popup;
}

export async function printPdf(popup: Window, blob: Blob, onPrint: () => Promise<unknown>): Promise<void> {
  if (popup.closed) throw new Error('打印窗口已关闭，请重新打开');
  const { renderPrintPages } = await import('./printRenderer');
  await renderPrintPages(popup, blob);
  if (popup.closed) throw new Error('打印窗口已关闭，请重新打开');
  const status = popup.document.querySelector<HTMLElement>('[role=status]')!;
  const button = popup.document.querySelector('button')!;
  const print = async () => {
    button.disabled = true;
    try {
      await onPrint();
      if (popup.closed) return;
      status.textContent = '打印内容已就绪。如未出现打印对话框，请点击“打印”。';
      try {
        popup.focus();
        popup.print();
      } catch {
        status.textContent = '未能唤起打印对话框，请点击“打印”重试，或使用浏览器的打印功能。';
      }
    } catch (e) {
      status.textContent = e instanceof Error ? e.message : '打印失败，请重试';
      throw e;
    } finally {
      if (!popup.closed) button.disabled = false;
    }
  };
  button.onclick = () => {
    print().catch(() => undefined);
  };
  await print();
}
