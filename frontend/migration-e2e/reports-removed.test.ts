import { readFileSync } from 'node:fs';
import { test, expect } from '@playwright/test';

test.use({ locale: 'zh-CN' });

test('report menus, pages and archive APIs are removed while inventory remains available', async ({ page, request }, testInfo) => {
  const info = await (await request.get('/__preview/info')).json();
  expect(info.role).toBe('development');
  expect(info.ready).toBe(true);
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  await page.goto('/login');
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill(credentials.password);
  await page.locator('button.ant-btn-primary').click();
  await page.waitForURL((url) => !url.pathname.startsWith('/login'));
  const token = await page.evaluate(() => localStorage.getItem('access_token'));
  const headers = { Authorization: `Bearer ${token}`, 'X-Language': 'zh_CN' };

  for (const route of ['/targets/cmdb', '/targets/racks']) {
    await page.goto(route);
    await page.reload();
    await expect(page.locator('.cmdb-page, .rack-views').first()).toBeVisible();
    await expect(page.locator('aside a[href^="/reports/"]')).toHaveCount(0);
    await expect(page.locator('aside').filter({ has: page.locator('a[href="/targets/cmdb"]') })).not.toContainText('巡检报告');
    await expect(page.locator('aside').filter({ has: page.locator('a[href="/targets/cmdb"]') })).not.toContainText('故障报告');
  }
  await testInfo.attach('sidebar-without-report-menus', { body: await page.screenshot(), contentType: 'image/png' });

  for (const route of ['/reports/inspection', '/reports/incident']) {
    await page.goto(route);
    await page.reload();
    await expect(page.locator('.page-error .ant-result-title')).toHaveText('页面不存在');
  }
  // The CMDB dispatcher rejects these as unknown endpoints, including mutations.
  for (const [method, path] of [
    ['GET', 'reports?kind=inspection'],
    ['GET', 'reports?kind=incident'],
    ['POST', 'reports'],
    ['PUT', 'reports/00000000-0000-0000-0000-000000000001'],
    ['GET', 'reports/00000000-0000-0000-0000-000000000001/file'],
    ['GET', 'reports/00000000-0000-0000-0000-000000000001/preview'],
  ]) {
    const response = await request.fetch(`/api/cmdb/${path}`, { method, headers });
    expect(response.status()).toBe(400);
    expect((await response.json()).err).toBe('接口或请求方法不存在');
  }
  expect(errors).toEqual([]);
});
