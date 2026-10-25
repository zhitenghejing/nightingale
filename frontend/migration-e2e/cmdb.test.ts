import { readFileSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { test, expect } from '@playwright/test';

test.use({ locale: 'zh-CN' });

test('CMDB production UI, native ACL, rack moves', async ({ page, request }, testInfo) => {
  page.setDefaultTimeout(15000);
  const info = await (await request.get('/__preview/info')).json();
  expect(info.role).toBe('development');
  expect(info.ready).toBe(true);
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  expect((await request.get('/api/cmdb/devices')).status()).toBe(401);
  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  await page.goto('/login');
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill(credentials.password);
  await page.locator('button.ant-btn-primary').click();
  await page.waitForURL((url) => !url.pathname.startsWith('/login'));
  const token = await page.evaluate(() => localStorage.getItem('access_token'));
  const headers = { Authorization: `Bearer ${token}` };
  const api = async (path: string, method = 'GET', data?: unknown) => {
    const response = await request.fetch(`/api/cmdb/${path}`, { method, data, headers });
    expect(response.ok()).toBe(true);
    const body = await response.json();
    expect(body.err).toBe('');
    return body.dat;
  };
  const suffix = randomUUID().slice(0, 8);
  const room = await api('rooms', 'POST', { name: `E2E room ${suffix}` });
  const rack = await api('racks', 'POST', { roomId: room.id, name: `E2E rack ${suffix}`, units: 42 });
  const serial = `E2E-${suffix}`;
  await page.goto('/targets/cmdb');
  await expect(page.locator('aside').getByRole('link', { name: 'CMDB', exact: true })).toBeVisible();
  await expect(page.locator('.header-tab-menu')).toHaveCount(0);
  await page.getByRole('button', { name: '添加设备' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('客户信息', { exact: true }).fill('E2E customer');
  await dialog.getByLabel('序列号', { exact: true }).fill(serial);
  await dialog.getByLabel('主机名', { exact: true }).fill(serial);
  await dialog.getByRole('button', { name: '保存设备', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('cell', { name: serial, exact: true })).toHaveCount(2);
  let device = (await api('devices')).find((d: any) => d.serial === serial);
  expect(device).toBeTruthy();
  device = await api(`devices/${device.id}`, 'PUT', {
    ...device, rackId: rack.id, uStart: 10, uEnd: 10, vendor: 'Dell', model: 'PowerEdge R650', assetMaterialId: 'dell-r650',
  });
  // A conflicting batch must leave neither record behind.
  const rejected = await request.post('/api/cmdb/import', { headers, data: [
    { ...device, serial: `IMPORT-A-${suffix}`, uStart: 20, uEnd: 20 },
    { ...device, serial: `IMPORT-B-${suffix}`, uStart: 20, uEnd: 20 },
  ] });
  expect(rejected.status()).toBe(400);
  expect((await api('devices')).some((d: any) => d.serial === `IMPORT-A-${suffix}`)).toBe(false);
  await page.locator('aside').getByRole('link', { name: '机柜图', exact: true }).click();
  await page.locator('.rack-room summary').filter({ hasText: room.name }).click();
  await page.locator('.rack-list-item').filter({ hasText: rack.name }).click();
  await expect(page.locator('.header-tab-menu')).toHaveCount(0);
  await expect(page.getByText('点击设备查看或编辑', { exact: false })).toHaveCount(0);
  await page.getByRole('button', { name: '机柜图使用说明' }).hover();
  await expect(page.getByRole('tooltip')).toContainText('滚轮以鼠标位置为中心缩放');
  await page.locator('.rack-heading').first().hover();
  await expect(page.getByRole('tooltip')).toBeHidden();
  const viewportBox = await page.getByRole('region', { name: '机柜图缩放区域' }).first().boundingBox();
  expect(viewportBox!.y).toBeLessThan(260);
  expect(viewportBox!.height).toBeGreaterThan(page.viewportSize()!.height * 0.6);
  await testInfo.attach('rack-focused-layout', { body: await page.screenshot(), contentType: 'image/png' });
  await expect(page.locator(`img[data-material-id="dell-r650"][data-view="front"]`)).toBeVisible();
  await page.getByText('背视图', { exact: true }).click();
  const rear = page.locator(`img[data-material-id="dell-r650"][data-view="rear"]`);
  await expect(rear).toBeVisible();
  await expect.poll(() => rear.evaluate((node: HTMLImageElement) => node.complete && node.naturalWidth > 0)).toBe(true);
  const transfer = await page.evaluateHandle(() => new DataTransfer());
  await page.locator('.rack-device').filter({ hasText: serial }).dispatchEvent('dragstart', { dataTransfer: transfer });
  const target = page.getByRole('button', { name: `${rack.name} 15U`, exact: true });
  await target.scrollIntoViewIfNeeded();
  const bounds = await target.boundingBox();
  expect(bounds).toBeTruthy();
  const pointer = { clientX: bounds!.x + bounds!.width / 2, clientY: bounds!.y + bounds!.height / 2 };
  await target.dispatchEvent('dragover', { dataTransfer: transfer, ...pointer });
  await target.dispatchEvent('drop', { dataTransfer: transfer, ...pointer });
  await expect.poll(async () => (await api('devices')).find((d: any) => d.id === device.id).uStart).toBe(15);
  expect((await request.put(`/api/cmdb/devices/${device.id}`, { headers, data: device })).status()).toBe(400);
  device = (await api('devices')).find((d: any) => d.id === device.id);
  await api('delete', 'POST', [device]);
  const deleted = (await api('devices?deleted=true')).find((d: any) => d.id === device.id);
  await api('restore', 'POST', [deleted]);
  expect((await api('devices')).some((d: any) => d.id === device.id)).toBe(true);

  // Verify a real Standard identity, rather than mocking the CMDB identity callback.
  const username = `cmdb-e2e-${suffix}`;
  const password = `E2e-private-${randomUUID()}`;
  const created = await request.post('/api/n9e/users', { headers, data: { username, password, roles: ['Standard'], contacts: {} } });
  expect((await created.json()).err).toBe('');
  const users = await (await request.get(`/api/n9e/users?query=${username}`, { headers })).json();
  const userId = users.dat.list.find((u: any) => u.username === username).id;
  const login = await (await request.post('/api/n9e/auth/login', { data: { username, password } })).json();
  const userHeaders = { Authorization: `Bearer ${login.dat.access_token}` };
  expect((await request.get('/api/cmdb/devices', { headers: userHeaders })).status()).toBe(403);
  await api('acl', 'PUT', { userId, read: true });
  expect((await request.get('/api/cmdb/devices', { headers: userHeaders })).status()).toBe(200);
  expect((await request.post('/api/cmdb/devices', { headers: userHeaders, data: device })).status()).toBe(403);
  await request.put(`/api/n9e/user/${userId}/disabled`, { headers, data: { disabled: 1 } });
  expect((await request.get('/api/cmdb/devices', { headers: userHeaders })).status()).toBe(401);
  for (const route of ['/targets/cmdb', '/targets/racks']) {
    await page.goto(route);
    await page.reload();
    await expect(page.locator('#username')).toHaveCount(0);
    await expect(page.locator('.cmdb-page, .rack-views').first()).toBeVisible();
  }
  await testInfo.attach('cmdb-preview', { body: await page.screenshot(), contentType: 'image/png' });
  expect(errors).toEqual([]);
});
