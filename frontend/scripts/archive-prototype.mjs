// Build a self-contained mock UI with main's pinned toolchain and theme.
// pnpm prototype:archives: build, refresh the existing isolated preview, print URL.
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { fileURLToPath } from 'node:url';

const frontend = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
process.chdir(frontend);
function run(command, args, options = {}) {
  const result = spawnSync(command, args, { cwd: frontend, stdio: 'inherit', ...options });
  if (result.status !== 0) process.exit(result.status || 1);
  return result;
}
run('node', ['scripts/check-toolchain.cjs']);
run('pnpm', ['exec', 'tsc', '--noEmit']);
run('pnpm', ['exec', 'vite', 'build', '--config', 'vite.archive-prototype.config.ts']);
const build = path.join(frontend, 'dist/archive-prototype');
const generated = path.join(frontend, 'public/prototype/archive-management');
const published = path.join(frontend, 'dist/archive-prototype-publish');
fs.mkdirSync(generated, { recursive: true });
fs.mkdirSync(published, { recursive: true });
let html = fs.readFileSync(path.join(build, 'index.html'), 'utf8');
// One HTML file avoids depending on the app's large eager JS bundle. It also
// makes the throwaway prototype downloadable and usable without a backend.
html = html.replace(/<script\b[^>]*\bsrc="([^"]+)"[^>]*><\/script>/g, (_, url) => {
  const script = fs.readFileSync(path.join(build, url.replace('/prototype/archive-management/', '')), 'utf8');
  return `<script type="module">${script.replace(/<\/script/gi, '<\\/script')}</script>`;
});
html = html.replace(/<link\b[^>]*rel="stylesheet"[^>]*>/g, (tag) => {
  const url = tag.match(/href="([^"]+)"/)?.[1];
  if (!url) return tag;
  let css = fs.readFileSync(path.join(build, url.replace('/prototype/archive-management/', '')), 'utf8');
  // Keep main's font stack; use local Chinese glyphs in this offline prototype.
  css = css.replace(/@font-face\s*\{[^}]*\}/g, '');
  const inter = fs.readFileSync(path.join(frontend, 'public/font/Inter-latin.woff2')).toString('base64');
  return `<style>@font-face{font-family:Inter;font-style:normal;font-weight:100 900;font-display:swap;src:url(data:font/woff2;base64,${inter}) format('woff2')} ${css}</style>`;
});
const logo = fs.readFileSync(path.join(frontend, 'public/image/logo-light-l.png')).toString('base64');
html = html.replaceAll('/image/logo-light-l.png', `data:image/png;base64,${logo}`);
const revision = crypto.createHash('sha256').update(html).digest('hex');
html = html.replace('<head>', `<head><meta name="archive-prototype-revision" content="${revision}">`);
fs.writeFileSync(path.join(generated, 'index.html'), html);
fs.writeFileSync(path.join(published, 'index.html'), html);
fs.writeFileSync(
  path.join(generated, 'identity.json'),
  JSON.stringify({ kind: 'frontend-only-prototype', revision, source: 'src/pages/archiveManagementPrototype', variants: ['A', 'B', 'C'], persistence: 'memory-only' }, null, 2),
);
console.log(`\nPrototype revision: ${revision}\nGenerated: ${generated}/index.html`);
if (process.argv.includes('--build-only')) process.exit(0);
// Use the existing explicit file-assets preview mode. Serve the standalone
// prototype as the root index; ServeFile canonicalizes nested /index.html URLs.
run('python3', ['-B', '../scripts/preview.py', 'refresh', '--frontend-dir', published, '--frontend-ref', `archive-ui-prototype:${revision}`]);
const result = spawnSync('python3', ['-B', '../scripts/preview.py', 'url'], { cwd: frontend, encoding: 'utf8' });
console.log(result.stdout.trim().replace('/__preview/ready', '/prototype/archive-management?variant=A'));
