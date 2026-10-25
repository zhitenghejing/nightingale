// Build an offline CLI, including dependencies. No customer-side node_modules.
import { build } from 'vite';
import { fileURLToPath } from 'node:url';
import { builtinModules, createRequire } from 'node:module';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { readFile, writeFile, readdir, realpath } from 'node:fs/promises';
const root = fileURLToPath(new URL('../', import.meta.url));
let patched = false;
await build({
  configFile: false,
  publicDir: false,
  root,
  plugins: [
    {
      name: 'archive-offline-docx',
      transform(code, id) {
        if (!id.endsWith('/canvas-editor-plugin-docx/dist/docx.js')) return;
        const original = 'return Cp(G, `${r}.docx`), G;';
        if (code.split(original).length !== 2) throw new Error('Pinned DOCX plugin changed; review offline patch');
        patched = true;
        return code.replace(original, 'return G;');
      },
    },
  ],
  ssr: { noExternal: true },
  build: {
    ssr: 'src/pages/archiveManagement/exportWorker.ts',
    outDir: '../archives/renderer',
    emptyOutDir: false,
    minify: false,
    rollupOptions: {
      external: [...builtinModules, ...builtinModules.map((n) => `node:${n}`)],
      output: { format: 'cjs', entryFileNames: 'worker.cjs', inlineDynamicImports: true },
    },
  },
});
if (!patched) throw new Error('Offline DOCX patch was not applied');
const seen = new Map();
async function notice(packageRoot) {
  packageRoot = await realpath(packageRoot);
  if (seen.has(packageRoot)) return;
  const filename = path.join(packageRoot, 'package.json');
  const meta = JSON.parse(await readFile(filename, 'utf8'));
  seen.set(packageRoot, { name: meta.name, version: meta.version, license: meta.license, text: '' });
  const files = (await readdir(packageRoot)).filter((name) => /^(licen[cs]e|notice|copying)(\.|$)/i.test(name)).sort();
  const texts = await Promise.all(files.map(async (name) => `${name}\n${await readFile(path.join(packageRoot, name), 'utf8')}`));
  seen.get(packageRoot).text = texts.join('\n');
  const require = createRequire(filename);
  for (const dep of Object.keys(meta.dependencies || {}).sort()) {
    let resolved;
    try {
      resolved = require.resolve(`${dep}/package.json`);
    } catch {
      resolved = require.resolve(dep);
    }
    let parent = path.dirname(resolved);
    for (;;) {
      try {
        if (JSON.parse(await readFile(path.join(parent, 'package.json'), 'utf8')).name === dep) break;
      } catch {}
      const next = path.dirname(parent);
      if (next === parent) throw new Error(`Cannot find license root for ${dep}`);
      parent = next;
    }
    await notice(parent);
  }
}
for (const p of ['@hufe921/canvas-editor', '@hufe921/canvas-editor-plugin-docx', 'pdf-lib', 'marked', 'pdfjs-dist', 'diff']) await notice(`${root}node_modules/${p}`);
const packages = [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
await writeFile(
  `${root}../archives/renderer/NOTICE`,
  `Offline archive renderer and archive browser components.\nDOCX plugin 1.0.0 modified at build time to return its Blob without initiating a browser download.\nFor dual-licensed dependencies the permissive MIT alternative is selected.\n\n${packages
    .map((p) => `${p.name}@${p.version} (${p.license})\n${p.text}`)
    .join('\n\n')}`,
);
const assetNotices = [];
for (const dir of ['wasm', 'standard_fonts', 'cmaps', 'iccs']) {
  for (const name of (await readdir(`${root}node_modules/pdfjs-dist/${dir}`)).filter((n) => n.startsWith('LICENSE') && n !== 'LICENSE_LIBERATION').sort())
    assetNotices.push(`PDF.js ${dir}/${name}\n${await readFile(`${root}node_modules/pdfjs-dist/${dir}/${name}`, 'utf8')}`);
}
assetNotices.push(await readFile(`${root}public/archive-fonts/LICENSE.txt`, 'utf8'));
assetNotices.push(await readFile(`${root}public/font/OFL-NotoSansSC.txt`, 'utf8'));
const noticePath = `${root}../archives/renderer/NOTICE`;
await writeFile(noticePath, (await readFile(noticePath, 'utf8')) + '\n\n' + assetNotices.join('\n\n'));
const inputs = {};
for (const relative of [
  'pnpm-lock.yaml',
  'scripts/build-archive-worker.mjs',
  'src/pages/archiveManagement/exportWorker.ts',
  'public/archive-fonts/manifest.json',
  'public/archive-fonts/LICENSE.txt',
  'public/font/OFL-NotoSansSC.txt',
])
  inputs[relative] = createHash('sha256')
    .update(await readFile(path.join(root, relative)))
    .digest('hex');
const sha256 = createHash('sha256')
  .update(await readFile(`${root}../archives/renderer/worker.cjs`))
  .digest('hex');
await writeFile(
  `${root}../archives/renderer/manifest.json`,
  JSON.stringify(
    {
      inputs,
      sha256,
      noticeSha256: createHash('sha256')
        .update(await readFile(noticePath))
        .digest('hex'),
      packages: packages.map(({ text, ...p }) => p),
    },
    null,
    2,
  ) + '\n',
);
