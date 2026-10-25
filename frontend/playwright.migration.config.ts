import { defineConfig } from '@playwright/test';

if (!process.env.N9E_E2E_BASE_URL || !process.env.N9E_E2E_BUILD_ID) {
  throw new Error('Run make frontend-e2e against a verified disposable preview');
}

export default defineConfig({
  testDir: './migration-e2e',
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  timeout: 120000,
  expect: { timeout: 15000 },
  use: {
    // Full Chromium's PDF viewer is needed for report preview and reload scenarios.
    channel: 'chromium',
    baseURL: process.env.N9E_E2E_BASE_URL,
    viewport: { width: 1440, height: 900 },
    proxy: process.env.N9E_E2E_PROXY_SERVER ? { server: process.env.N9E_E2E_PROXY_SERVER } : undefined,
    // Login requests contain private preview credentials. Never record traces.
    trace: 'off',
    video: 'off',
  },
  reporter: [['list'], ['html', { open: 'never' }]],
});
