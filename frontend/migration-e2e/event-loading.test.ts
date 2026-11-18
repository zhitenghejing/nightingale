import { readFileSync } from 'node:fs';
import { test, expect } from '@playwright/test';

test.beforeAll(async ({ request }) => {
  const response = await request.get('/__preview/info');
  expect(response.ok()).toBe(true);
  const info = await response.json();
  expect(info.role).toBe('development');
  expect(info.ready).toBe(true);
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
});

for (const route of ['/alert-his-events', '/alert-cur-events']) {
  test(`${route} renders without downloading the detail editor`, async ({ page }) => {
    const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
    await page.goto('/login');
    await page.locator('#username').fill(credentials.username);
    await page.locator('#password').fill(credentials.password);
    await page.locator('button.ant-btn-primary').click();
    await page.waitForURL((url) => !url.pathname.startsWith('/login'));

    // A slow/unavailable detail editor must not suspend the entire event list.
    // Routing also disables browser cache so a previously loaded chunk cannot
    // hide a regression in the production dependency graph.
    const editorRequests: string[] = [];
    await page.route('**/assets/monaco-editor*.js', async (request) => {
      editorRequests.push(new URL(request.request().url()).pathname);
      await request.abort();
    });
    const errors: string[] = [];
    page.on('pageerror', (error) => errors.push(error.message));
    for (let visit = 0; visit < 2; visit++) {
      await page.goto(route, { waitUntil: 'domcontentloaded' });
      await expect(page.locator(route === '/alert-his-events' ? '.event-content .ant-table' : '.cur-events').first()).toBeVisible();
      await expect(page.locator('.preloader')).toHaveCount(0);
      expect(editorRequests).toEqual([]);
      expect(errors).toEqual([]);
    }
  });
}
