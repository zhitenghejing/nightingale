import { readFileSync } from 'node:fs';
import { test, expect } from '@playwright/test';

test('AI composer keeps IME confirmation out of the send shortcut', async ({ page, request }, testInfo) => {
  const response = await request.get('/__preview/info');
  expect(response.ok()).toBe(true);
  const info = await response.json();
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
  expect(info.ready).toBe(true);

  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  await page.goto('/login');
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill(credentials.password);
  await page.locator('button.ant-btn-primary').click();
  await page.waitForURL((url) => !url.pathname.startsWith('/login'));
  await page.goto('/dashboards');

  // Capture the send request locally: this keyboard regression must never
  // create a real chat, call a model, or send monitoring data out of the preview.
  const sends: unknown[] = [];
  await page.route('**/api/n9e/assistant/chat/new', (route) => route.fulfill({ json: { err: '', dat: { chat_id: 'ime-browser-test', title: '', last_update: 0 } } }));
  await page.route('**/api/n9e/assistant/message/new', (route) => {
    sends.push(route.request().postDataJSON());
    return route.fulfill({ json: { err: '', dat: { chat_id: 'ime-browser-test', seq_id: 1 } } });
  });
  await page.route('**/api/n9e/assistant/message/detail', (route) =>
    route.fulfill({ json: { err: '', dat: { chat_id: 'ime-browser-test', seq_id: 1, is_finish: true, response: [] } } }),
  );

  await page.getByRole('button', { name: '致慧AI', exact: true }).first().click();
  const composer = page.locator('textarea.ant-input:visible').last();
  await expect(composer).toBeVisible();
  await composer.fill('nginx error log');
  await composer.evaluate((element) => {
    element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', bubbles: true, isComposing: true }));
  });
  await expect(composer).toHaveValue('nginx error log');
  expect(sends).toHaveLength(0);

  await composer.evaluate((element) => {
    element.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
    element.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: 'nginx' }));
    element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', code: 'Enter', keyCode: 229, bubbles: true, isComposing: false }));
  });
  await expect(composer).toHaveValue('nginx error log');
  expect(sends).toHaveLength(0);

  await composer.press('Shift+Enter');
  await expect(composer).toHaveValue('nginx error log\n');
  expect(sends).toHaveLength(0);
  await composer.press('Enter');
  await expect.poll(() => sends.length).toBe(1);
  expect(sends[0]).toMatchObject({ query: { content: 'nginx error log' } });
  await testInfo.attach('preview-build', { body: Buffer.from(JSON.stringify({ buildId: info.buildId })), contentType: 'application/json' });
});
