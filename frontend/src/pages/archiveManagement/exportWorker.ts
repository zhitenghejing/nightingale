// Private Node CLI. Input paths come only from the authenticated Go job runner.
import fs from 'node:fs/promises';
import path from 'node:path';
import docxPlugin from '@hufe921/canvas-editor-plugin-docx';
import { PDFDocument, PDFName, PDFString, PDFHexString } from 'pdf-lib';

const [inputPath, outputPath] = process.argv.slice(2);
async function main() {
  const input = JSON.parse(await fs.readFile(inputPath, 'utf8'));
  if (input.operation === 'inspect_pdf') {
    const pdf = await PDFDocument.load(await fs.readFile(input.source), { ignoreEncryption: false });
    if (pdf.getPageCount() < 1) throw new Error('Empty PDF');
    await fs.writeFile(outputPath, JSON.stringify({ pages: pdf.getPageCount() }));
    return;
  }
  if (input.operation === 'annotate') {
    const pdf = await PDFDocument.load(await fs.readFile(input.source), { ignoreEncryption: false });
    const pages = pdf.getPages();
    for (const a of input.annotations || []) {
      if (a.page < 1 || a.page > pages.length) throw new Error('Annotation page exceeds PDF page count');
      const page = pages[a.page - 1];
      const crop = page.getCropBox();
      const media = page.getMediaBox();
      const left = Math.max(crop.x, media.x);
      const bottom = Math.max(crop.y, media.y);
      const width = Math.min(crop.x + crop.width, media.x + media.width) - left;
      const height = Math.min(crop.y + crop.height, media.y + media.height) - bottom;
      const rects = a.rects.map((r: any) => [left + r.x * width, bottom + (1 - r.y - r.height) * height, left + (r.x + r.width) * width, bottom + (1 - r.y) * height]);
      const bounds = [
        Math.min(...rects.map((r: any) => r[0])),
        Math.min(...rects.map((r: any) => r[1])),
        Math.max(...rects.map((r: any) => r[2])),
        Math.max(...rects.map((r: any) => r[3])),
      ];
      const hex = a.color || '#ffe066';
      const color = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
      const data: any = {
        Type: 'Annot',
        Subtype: a.type === 'note' ? 'Text' : a.type === 'highlight' ? 'Highlight' : 'StrikeOut',
        Rect: bounds,
        C: color,
        CA: 0.4,
        F: 4,
        NM: PDFString.of(a.id),
        T: PDFHexString.fromText(a.author_name || ''),
        Contents: PDFHexString.fromText(a.text || ''),
        M: PDFString.fromDate(new Date(a.update_at || a.create_at)),
      };
      if (a.type !== 'note') data.QuadPoints = rects.flatMap((r: number[]) => [r[0], r[3], r[2], r[3], r[0], r[1], r[2], r[1]]);
      else data.Name = 'Comment';
      const annotation = pdf.context.register(pdf.context.obj(data));
      const annots = page.node.lookup(PDFName.of('Annots'));
      if (annots && 'push' in annots) (annots as any).push(annotation);
      else page.node.set(PDFName.of('Annots'), pdf.context.obj([annotation]));
    }
    await fs.writeFile(outputPath, await pdf.save());
    return;
  }
  if (input.operation !== 'docx') throw new Error('Unsupported render operation');
  const snapshot = input.body;
  const walk = async (v: any): Promise<void> => {
    if (!v || typeof v !== 'object') return;
    if (v.type === 'image') {
      const id = /^archive-asset:(\d+)$/.exec(v.value)?.[1];
      const asset = id && input.assets[id];
      if (!asset || !['image/png', 'image/jpeg'].includes(asset.content_type)) throw new Error('Missing managed image');
      v.value = `data:${asset.content_type};base64,${(await fs.readFile(asset.path)).toString('base64')}`;
    }
    if (typeof v.value === 'string' && !v.font) v.font = snapshot.options?.defaultFont || 'Noto Sans CJK SC';
    for (const child of Object.values(v)) await walk(child);
  };
  await walk(snapshot.data);
  const editor: any = { command: { getValue: () => snapshot } };
  docxPlugin(editor);
  const blob = await editor.command.executeExportDocx({ fileName: path.basename(outputPath, '.docx') });
  await fs.writeFile(outputPath, Buffer.from(await blob.arrayBuffer()));
}
main().catch((err) => {
  process.stderr.write(String(err.message).slice(0, 1000));
  process.exitCode = 1;
});
