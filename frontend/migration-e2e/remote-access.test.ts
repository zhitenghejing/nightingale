import { readFileSync } from 'node:fs';
import { test, expect } from '@playwright/test';

test.use({ locale: 'zh-CN' });

test('SSH device credentials stay in the device form and launch through the console', async ({ page, request }) => {
  const fixtureFile = process.env.N9E_E2E_SSH_FIXTURE_FILE;
  const hostKeyFile = process.env.N9E_E2E_SSH_HOST_KEY_FILE;
  test.skip(!fixtureFile || !hostKeyFile, 'Requires a disposable native Target and SSH endpoint');
  const fixture = JSON.parse(readFileSync(fixtureFile!, 'utf8'));
  const server = JSON.parse(readFileSync(hostKeyFile!, 'utf8'));
  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  const info = await (await request.get('/__preview/info')).json();
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
  await page.goto('/login');
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill(credentials.password);
  await page.locator('button.ant-btn-primary').click();
  await page.waitForURL((url) => !url.pathname.startsWith('/login'));
  const token = await page.evaluate(() => localStorage.getItem('access_token'));
  const headers = { Authorization: `Bearer ${token}` };
  const path = `/api/n9e/remote-access/connection?ident=${encodeURIComponent(fixture.ident)}`;
  const binding = async () => {
    const response = await request.get(path, { headers });
    expect(response.ok()).toBe(true);
    return (await response.json()).dat;
  };
  expect((await binding()).configured).toBe(false);
  try {
    await page.goto(`/targets?ids=${fixture.groupID}&isLeaf=true`);
    const filter = page.getByPlaceholder('多个关键词用空格分隔，关系为"且"');
    await filter.fill(fixture.ident);
    await filter.press('Enter');
    await expect(page.getByRole('button', { name: '运维凭据', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: `SSH ${fixture.deviceName}`, exact: true })).toHaveCount(0);
    await page.getByRole('button', { name: `编辑 ${fixture.deviceName}`, exact: true }).click();
    await page.getByRole('button', { name: '配置设备控制台' }).click();
    const form = page.getByRole('dialog').filter({ hasText: `编辑设备控制台 · ${fixture.ident}` });
    await form.getByText('设备控制台（可选）', { exact: true }).click();
    await form.getByRole('switch', { name: '启用控制台连接' }).click();
    await form.getByLabel('SSH 地址（固定 IP）').fill(server.host);
    await form.getByLabel('端口', { exact: true }).fill(String(server.port));
    await form.getByLabel('SSH 账号', { exact: true }).fill('synthetic-readonly');
    await form.getByLabel('SSH 密码', { exact: true }).fill(server.testPassword);
    await form.getByRole('button', { name: '测试 SSH 连接' }).click();
    const confirmation = page.getByRole('dialog').filter({ hasText: '确认目标设备指纹' });
    await expect(confirmation).toContainText(server.fingerprint);
    await confirmation.getByRole('button', { name: '接受指纹并继续' }).click();
    await expect(form.getByText(/SSH 连接成功/)).toBeVisible();
    expect((await binding()).configured).toBe(false);
    await form.getByRole('button', { name: '保存连接' }).click();
    await expect(form).not.toBeVisible();
    const saved = await binding();
    expect(saved.target.fingerprint).toBe(server.fingerprint);
    expect(JSON.stringify(saved)).not.toContain(server.testPassword);
    expect(JSON.stringify(saved)).not.toContain('synthetic-readonly');
    await page.getByRole('button', { name: '配置设备控制台' }).click();
    await form.getByText('设备控制台（可选）', { exact: true }).click();
    await expect(form.getByLabel('SSH 账号', { exact: true })).toHaveValue('');
    await expect(form.getByLabel('SSH 密码', { exact: true })).toHaveValue('');
    await form.getByRole('button', { name: '保存连接' }).click();
    await expect(form).not.toBeVisible();
    await page.locator('.ant-drawer:visible .ant-drawer-close').click();
    await page.getByRole('button', { name: `控制台 ${fixture.deviceName}`, exact: true }).click();
    await expect(page.getByRole('radio', { name: 'SSH' })).toBeChecked();
    await expect(page.getByRole('radio', { name: 'RDP' })).toBeDisabled();
  } finally {
    const saved = await binding();
    if (saved.configured) {
      const response = await request.delete(path, { headers, data: { record_version: saved.record_version } });
      expect(response.ok()).toBe(true);
    }
  }
});
