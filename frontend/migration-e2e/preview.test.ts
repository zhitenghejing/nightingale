import { readFileSync } from 'node:fs';
import { test, expect } from '@playwright/test';

test.beforeAll(async ({ request }) => {
  const response = await request.get('/__preview/info');
  expect(response.ok()).toBe(true);
  const info = await response.json();
  expect(info.role).toBe('development');
  expect(info.ready).toBe(true);
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
  expect(info.frontend.kind).toBe('in-tree');
});

test('anonymous API access is rejected', async ({ request }) => {
  const response = await request.get('/api/n9e/self/profile');
  expect(response.status()).toBe(401);
});

test('production UI rejects a bad password, signs in and survives route reloads', async ({ page }, testInfo) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  page.on('response', (response) => {
    const url = new URL(response.url());
    if (url.origin === new URL(process.env.N9E_E2E_BASE_URL!).origin && response.status() >= 400
        && !url.pathname.includes('/auth/login')) {
      errors.push(`${response.status()} ${url.pathname}`);
    }
  });
  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  await page.goto('/login');
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill('migration-deliberately-invalid-password');
  const rejected = page.waitForResponse((r) => r.url().includes('/auth/login') && r.request().method() === 'POST');
  await page.locator('button.ant-btn-primary').click();
  const failure = await (await rejected).json();
  expect(Boolean(failure.err)).toBe(true);
  await expect(page).toHaveURL(/\/login/);

  await page.locator('#password').fill(credentials.password);
  const accepted = page.waitForResponse((r) => r.url().includes('/auth/login') && r.request().method() === 'POST');
  await page.locator('button.ant-btn-primary').click();
  expect(Boolean((await (await accepted).json()).err)).toBe(false);
  await page.waitForURL((url) => !url.pathname.startsWith('/login'));
  await expect.poll(() => page.evaluate(() => Boolean(localStorage.getItem('access_token')))).toBe(true);

  for (const route of ['/dashboards', '/alert-rules']) {
    const content = page.locator(route === '/dashboards' ? '.dashboards-v2' : '.alert-rules-list-container');
    const expectReady = async () => {
      await expect(page).toHaveURL((url) => url.pathname === route);
      await expect(page.locator('.preloader')).toHaveCount(0);
      await expect(content).toBeVisible();
      await expect(content).toContainText(/Name|名称/);
      await expect(content.locator('.ant-spin-spinning')).toHaveCount(0);
      await expect(page.locator('#username')).toHaveCount(0);
    };
    await page.goto(route);
    await expectReady();
    await page.reload();
    await expectReady();
  }
  await testInfo.attach('preview-build', {
    body: Buffer.from(JSON.stringify({ buildId: process.env.N9E_E2E_BUILD_ID })),
    contentType: 'application/json',
  });
  await testInfo.attach('authenticated-rules-page', {
    body: await page.screenshot(), contentType: 'image/png',
  });
  expect(errors).toEqual([]);
});

test('expired browser session returns to login', async ({ page }) => {
  await page.addInitScript(() => {
    // Seed an expired session once; let the application's 401 handler clear it.
    if (!sessionStorage.getItem('migration-expired-session-seeded')) {
      localStorage.setItem('access_token', 'invalid-migration-token');
      sessionStorage.setItem('migration-expired-session-seeded', 'true');
    }
  });
  await page.goto('/dashboards');
  await expect(page).toHaveURL(/\/login/);
  await expect(page.locator('#username')).toBeVisible();
});
