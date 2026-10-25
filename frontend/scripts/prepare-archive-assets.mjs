// Keep PDF.js codecs, CMaps and fonts local; no CDN or production document service.
import { cp, mkdir, readFile, rm } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../', import.meta.url));
const fonts = JSON.parse(await readFile(`${root}public/archive-fonts/manifest.json`, 'utf8'));
for (const { file, sha256 } of fonts.files) {
  if (
    createHash('sha256')
      .update(await readFile(`${root}public/archive-fonts/${file}`))
      .digest('hex') !== sha256
  )
    throw new Error(`Pinned archive font changed: ${file}`);
}
await mkdir(`${root}public/archive-renderer`, { recursive: true });
for (const dir of ['cmaps', 'standard_fonts', 'wasm', 'iccs']) await cp(`${root}node_modules/pdfjs-dist/${dir}`, `${root}public/archive-renderer/${dir}`, { recursive: true });
// Use the pinned OFL 2.1.5 font binaries instead of PDF.js's older GPL font data.
for (const style of ['Regular', 'Bold', 'Italic', 'BoldItalic'])
  await cp(`${root}public/archive-fonts/LiberationSans-${style}.ttf`, `${root}public/archive-renderer/standard_fonts/LiberationSans-${style}.ttf`);
await rm(`${root}public/archive-renderer/standard_fonts/LICENSE_LIBERATION`, { force: true });
await cp(`${root}public/archive-fonts/LICENSE.txt`, `${root}public/archive-renderer/standard_fonts/LICENSE_LIBERATION_OFL.txt`);
await cp(`${root}node_modules/pdfjs-dist/LICENSE`, `${root}public/archive-renderer/PDFJS-LICENSE.txt`);
