import { defineConfig } from '@playwright/test';
if (!process.env.N9E_E2E_BASE_URL || !process.env.N9E_E2E_BUILD_ID || !process.env.N9E_E2E_CREDENTIALS_FILE) throw new Error('Archives E2E requires a verified disposable preview');
export default defineConfig({
  testDir: './archive-e2e',
  workers: 1,
  retries: 0,
  timeout: 180000,
  expect: { timeout: 20000 },
  reporter: [['list'], ['html', { outputFolder: 'archive-e2e-report', open: 'never' }]],
  use: {
    baseURL: process.env.N9E_E2E_BASE_URL,
    viewport: { width: 1600, height: 1000 },
    actionTimeout: 25000,
    navigationTimeout: 45000,
    trace: 'off',
    video: 'off',
    proxy: process.env.N9E_E2E_PROXY_SERVER ? { server: process.env.N9E_E2E_PROXY_SERVER } : undefined,
  },
});
