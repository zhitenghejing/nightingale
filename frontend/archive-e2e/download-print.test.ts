import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { test, expect, Response } from '@playwright/test';

test('archive information renders printable pages and invokes print after loading', async ({ page, context }, testInfo) => {
  const calls: number[] = [];
  let pdfResponse: Response | undefined;
  let rendererURL = '';
  page.on('response', (response) => {
    if (/\/archives\/exports\/[^/]+\/file/.test(response.url())) pdfResponse = response;
    if (/\/assets\/printRenderer-[^/]+\.js/.test(response.url())) rendererURL = response.url();
  });
  await context.exposeBinding('archivePrintInvoked', ({ page: source }) => calls.push(context.pages().indexOf(source)));
  await context.addInitScript(() => {
    window.print = () => (window as any).archivePrintInvoked();
  });
  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  await page.goto('/login');
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill(credentials.password);
  await page.locator('button.ant-btn-primary').click();
  await page.waitForURL((url) => !url.pathname.startsWith('/login'));
  const record = await page.evaluate(async () => {
    const response = await fetch('/api/n9e/archives', {
      method: 'POST',
      headers: { Authorization: `Bearer ${localStorage.getItem('access_token')}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ category: 'other', metadata: { title: `打印回归 ${Date.now()}`, custom_type: '验收' } }),
    });
    const result = await response.json();
    if (result.err) throw new Error(result.err);
    return result.dat;
  });
  await page.goto(`/archives/${record.id}`);
  await page.getByRole('button', { name: '更多档案操作' }).click();
  const [popup] = await Promise.all([page.waitForEvent('popup'), page.getByRole('menuitem', { name: '打印档案信息' }).click()]);
  await expect.poll(() => calls.length, { timeout: 30000 }).toBe(1);
  await expect(popup.locator('canvas').first()).toBeVisible();
  // Compare against the same PDF rendered in its own document. Rendering into
  // a popup with fonts registered in the opener silently produces missing glyphs.
  expect(pdfResponse).toBeDefined();
  expect(rendererURL).not.toBe('');
  const bytes = Array.from(await pdfResponse!.body());
  const reference = await context.newPage();
  await reference.goto('/login');
  await reference.setContent('<main></main><span role="status"></span>');
  const referenceImage = await reference.evaluate(
    async ({ url, bytes }) => {
      const render = Object.values(await import(url)).find((value) => typeof value === 'function');
      if (typeof render !== 'function') throw new Error('Print renderer missing');
      await render(window, new Blob([new Uint8Array(bytes)], { type: 'application/pdf' }));
      return document.querySelector('canvas')!.toDataURL();
    },
    { url: rendererURL, bytes },
  );
  const printImage = await popup
    .locator('canvas')
    .first()
    .evaluate((canvas: HTMLCanvasElement) => canvas.toDataURL());
  expect(createHash('sha256').update(printImage).digest('hex')).toBe(createHash('sha256').update(referenceImage).digest('hex'));
  await reference.close();
  await testInfo.attach('information-print-preview', { body: await popup.screenshot({ fullPage: true }), contentType: 'image/png' });
  await popup.getByRole('button', { name: '打印', exact: true }).click();
  await expect.poll(() => calls.length).toBe(2);
  await popup.close();
  // The document reader uses the same print path, with the saved document version.
  const doc = await page.evaluate(async (id) => {
    const response = await fetch(`/api/n9e/archives/${id}/documents`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${localStorage.getItem('access_token')}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: '正文打印回归',
        purpose: 'other',
        body: { schema_version: 1, data: { main: [{ value: '正文打印回归：中文及文字必须可见。' }], header: [], footer: [] } },
      }),
    });
    const result = await response.json();
    if (result.err) throw new Error(result.err);
    return result.dat.documents[0];
  }, record.id);
  await page.goto(`/archives/${record.id}/documents/${doc.id}`);
  const [documentPopup] = await Promise.all([page.waitForEvent('popup'), page.getByRole('button', { name: '打印', exact: false }).click()]);
  await expect.poll(() => calls.length, { timeout: 30000 }).toBe(3);
  await expect(documentPopup.locator('canvas').first()).toBeVisible();
  await documentPopup.close();
  await page.evaluate(() => {
    window.open = () => null;
  });
  let exports = 0;
  page.on('request', (r) => {
    if (r.method() === 'POST' && r.url().endsWith('/exports')) exports++;
  });
  await page.getByRole('button', { name: '打印', exact: false }).click();
  await expect(page.locator('.ant-message')).toContainText('浏览器阻止了打印窗口');
  expect(exports).toBe(0);
});
