// Generate an explicitly named, read-only print prototype alongside the existing
// archive mock. It is not imported by the application's print or export flows.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const frontend = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const source = path.join(frontend, 'src/pages/archiveManagementPrototype/print-prototype.html');
const output = path.join(frontend, 'public/prototype/archive-management/print.html');
fs.mkdirSync(path.dirname(output), { recursive: true });
fs.copyFileSync(source, output);
console.log(`Print prototype (mock data only): ${output}\nPreview path: /prototype/archive-management/print.html?variant=A`);
