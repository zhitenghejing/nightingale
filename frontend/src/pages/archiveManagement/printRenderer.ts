import * as pdfjs from 'pdfjs-dist';
import workerURL from 'pdfjs-dist/build/pdf.worker.mjs?url';

pdfjs.GlobalWorkerOptions.workerSrc = workerURL;

export async function renderPrintPages(popup: Window, blob: Blob): Promise<void> {
  const task = pdfjs.getDocument({
    data: new Uint8Array(await blob.arrayBuffer()),
    ownerDocument: popup.document,
    useSystemFonts: false,
    cMapUrl: `${import.meta.env.BASE_URL}archive-renderer/cmaps/`,
    cMapPacked: true,
    standardFontDataUrl: `${import.meta.env.BASE_URL}archive-renderer/standard_fonts/`,
    wasmUrl: `${import.meta.env.BASE_URL}archive-renderer/wasm/`,
    iccUrl: `${import.meta.env.BASE_URL}archive-renderer/iccs/`,
  });
  try {
    const pdf = await task.promise;
    const document = popup.document;
    const pages = document.querySelector('main')!;
    for (let n = 1; n <= pdf.numPages; n++) {
      if (popup.closed) throw new Error('打印窗口已关闭，请重新打开');
      document.querySelector('[role=status]')!.textContent = `正在准备打印内容：${n} / ${pdf.numPages} 页…`;
      const page = await pdf.getPage(n);
      const size = page.getViewport({ scale: 1 });
      const viewport = page.getViewport({ scale: 2 });
      const article = document.createElement('article');
      article.style.width = `${size.width}pt`;
      article.style.height = `${size.height}pt`;
      article.style.setProperty('page', `archive-${n}`);
      const style = document.createElement('style');
      style.textContent = `@page archive-${n} { size: ${size.width}pt ${size.height}pt; margin: 0; }`;
      document.head.appendChild(style);
      const canvas = document.createElement('canvas');
      canvas.width = Math.ceil(viewport.width);
      canvas.height = Math.ceil(viewport.height);
      article.appendChild(canvas);
      pages.appendChild(article);
      await page.render({ canvas, viewport, annotationMode: pdfjs.AnnotationMode.ENABLE_STORAGE }).promise;
      page.cleanup();
    }
  } finally {
    await task.destroy();
  }
}
