import { readFileSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { test, expect } from '@playwright/test';

test.use({ locale: 'zh-CN' });

test('recycle bin confirms permanent single/clear deletion, protects stale snapshots and releases retired inventory', async ({ page, request }, testInfo) => {
  page.setDefaultTimeout(15000);
  const info = await (await request.get('/__preview/info')).json();
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
  expect(info.role).toBe('development');
  expect(info.ready).toBe(true);
  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/login');
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill(credentials.password);
  await page.locator('button.ant-btn-primary').click();
  await page.waitForURL((url) => !url.pathname.startsWith('/login'));
  const token = await page.evaluate(() => localStorage.getItem('access_token'));
  const headers = { Authorization: `Bearer ${token}` };
  const api = async (path: string, method = 'GET', data?: unknown) => {
    const response = await request.fetch(`/api/cmdb/${path}`, { headers, method, data });
    expect(response.ok()).toBe(true);
    const body = await response.json();
    expect(body.err).toBe('');
    return body.dat;
  };
  const suffix = randomUUID().slice(0, 8);
  const room = await api('rooms', 'POST', { name: `Purge room ${suffix}` });
  const rack = await api('racks', 'POST', { roomId: room.id, name: `Purge rack ${suffix}`, units: 42 });
  const ids = new Set<string>();
  let rackRemoved = false,
    roomRemoved = false;
  const create = async (name: string, bound = true) => {
    const saved = await api('devices', 'POST', {
      customer: 'Recycle acceptance',
      serial: `PURGE-${name}-${suffix}`,
      hostname: `retired-${name}-${suffix}`,
      rackId: bound ? rack.id : '',
      disks: [{ model: 'SSD', description: 'purge fixture', count: 1 }],
    });
    ids.add(saved.id);
    return saved;
  };
  // Restrict only the browser's recycle listing to this test's real API fixtures.
  // Writes use the real authenticated endpoint. Existing preview recycle data is preserved.
  await page.route('**/api/cmdb/devices?deleted=true', async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    body.dat = body.dat.filter((d: any) => ids.has(d.id));
    await route.fulfill({ response, json: body });
  });
  const recycled = async () => (await api('devices?deleted=true')).filter((d: any) => ids.has(d.id));
  try {
    const a = await create('A'),
      b = await create('B'),
      c = await create('C');
    const active = await create('active', false);
    const rejected = await request.post('/api/cmdb/purge', { headers, data: [{ id: active.id, version: active.version }] });
    expect(rejected.status()).toBe(400);
    await api('delete', 'POST', [a, b, c]);
    await page.goto('/targets/cmdb');
    await page.getByRole('button', { name: '回收站', exact: true }).click();
    const bin = page.getByRole('dialog', { name: '设备回收站', exact: true });
    await expect(bin.getByText('共 3 台设备', { exact: true })).toBeVisible();
    const rowA = bin.getByRole('row').filter({ has: page.getByRole('cell', { name: a.serial, exact: true }) });
    await rowA.getByRole('button', { name: '彻底删除', exact: true }).click();
    let confirmation = page.getByRole('dialog', { name: '彻底删除设备', exact: true });
    await expect(confirmation).toContainText('无法恢复');
    await confirmation.getByRole('button', { name: /取\s*消/ }).click();
    expect((await recycled()).some((d: any) => d.id === a.id)).toBe(true);
    await rowA.getByRole('button', { name: '彻底删除', exact: true }).click();
    await confirmation.getByRole('button', { name: '确认彻底删除', exact: true }).click();
    await expect(bin.getByRole('cell', { name: a.serial, exact: true })).toHaveCount(0);
    await expect(bin.getByText('共 2 台设备', { exact: true })).toBeVisible();
    expect((await recycled()).some((d: any) => d.id === a.id)).toBe(false);
    expect((await request.post('/api/cmdb/restore', { headers, data: [{ id: a.id, version: a.version + 1 }] })).status()).toBe(400);

    await bin.getByRole('button', { name: '一键清空', exact: true }).click();
    confirmation = page.getByRole('dialog', { name: '清空回收站', exact: true });
    await expect(confirmation).toContainText('这 2 台设备');
    const rb = (await recycled()).find((d: any) => d.id === b.id);
    await api('restore', 'POST', [rb]);
    await confirmation.getByRole('button', { name: '确认清空', exact: true }).click();
    await expect(confirmation.getByRole('alert').filter({ hasText: '只能彻底删除回收站设备' })).toBeVisible();
    expect((await recycled()).some((d: any) => d.id === c.id)).toBe(true);
    const restored = (await api('devices')).find((d: any) => d.id === b.id);
    expect(restored).toBeTruthy();
    await confirmation.getByRole('button', { name: /取\s*消/ }).click();
    await api('delete', 'POST', [restored]);
    await bin.getByRole('button', { name: '刷新回收站', exact: true }).click();
    await expect(bin.getByText('共 2 台设备', { exact: true })).toBeVisible();
    await bin.getByRole('button', { name: '一键清空', exact: true }).click();
    await expect(confirmation).toContainText('这 2 台设备');
    const late = await create('late');
    await api('delete', 'POST', [late]);
    const purgeRequest = page.waitForRequest((r) => r.url().endsWith('/api/cmdb/purge') && r.method() === 'POST');
    await confirmation.getByRole('button', { name: '确认清空', exact: true }).click();
    const snapshot = (await purgeRequest).postDataJSON();
    expect(snapshot.map((d: any) => d.id).sort()).toEqual([b.id, c.id].sort());
    await expect(confirmation).toHaveCount(0);
    await expect(bin.getByRole('cell', { name: late.serial, exact: true })).toBeVisible();
    await expect(bin.getByText('共 1 台设备', { exact: true })).toBeVisible();
    await testInfo.attach('recycle-after-clear-snapshot', { body: await page.screenshot(), contentType: 'image/png' });
    await bin.getByRole('button', { name: /关\s*闭/ }).click();
    await page.reload();
    const remaining = await recycled();
    expect(remaining.map((d: any) => d.id)).toEqual([late.id]);
    expect((await api('devices')).some((d: any) => d.id === active.id)).toBe(true);
    const reused = await api('devices', 'POST', { customer: 'Recycle acceptance', serial: a.serial.toLowerCase(), hostname: 'reused', disks: [] });
    ids.add(reused.id);
    const audit = await api(`changes?assetId=${a.id}`);
    expect(audit.some((change: any) => change.action === 'purge')).toBe(true);
    expect((await request.delete(`/api/cmdb/racks/${rack.id}`, { headers, data: { version: rack.version } })).status()).toBe(400);
    await api(
      'purge',
      'POST',
      remaining.map(({ id, version }: any) => ({ id, version })),
    );
    await api(`racks/${rack.id}`, 'DELETE', { version: rack.version });
    rackRemoved = true;
    await api(`rooms/${room.id}`, 'DELETE');
    roomRemoved = true;
    expect(errors).toEqual([]);
  } finally {
    const active = (await api('devices')).filter((d: any) => ids.has(d.id));
    if (active.length) await api('delete', 'POST', active);
    const remaining = await recycled();
    if (remaining.length)
      await api(
        'purge',
        'POST',
        remaining.map(({ id, version }: any) => ({ id, version })),
      );
    if (!rackRemoved) await api(`racks/${rack.id}`, 'DELETE', { version: rack.version });
    if (!roomRemoved) await api(`rooms/${room.id}`, 'DELETE');
  }
});
