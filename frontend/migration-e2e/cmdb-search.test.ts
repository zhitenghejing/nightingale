import { readFileSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { test, expect } from '@playwright/test';

test.use({ locale: 'zh-CN' });

test('compact CMDB search combines keywords, field conditions, alternatives, expiry ranges and filtered export', async ({ page, request }, testInfo) => {
  page.setDefaultTimeout(15000);
  const info = await (await request.get('/__preview/info')).json();
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
  expect(info.ready).toBe(true);
  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  const errors: string[] = [];
  page.on('pageerror', (error) => {
    errors.push(error.message);
    console.error('CMDB search browser error:', error.message);
  });
  await page.goto('/login');
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill(credentials.password);
  await page.locator('button.ant-btn-primary').click();
  await page.waitForURL((url) => !url.pathname.startsWith('/login'));
  const token = await page.evaluate(() => localStorage.getItem('access_token'));
  const headers = { Authorization: `Bearer ${token}` };
  const api = async (path: string, method = 'GET', data?: unknown) => {
    const response = await request.fetch(`/api/cmdb/${path}`, { headers, method, data, timeout: 15000 });
    expect(response.ok()).toBe(true);
    const body = await response.json();
    expect(body.err).toBe('');
    return body.dat;
  };
  const suffix = randomUUID().slice(0, 8);
  const room = await api('rooms', 'POST', { name: `Search room ${suffix}` });
  const rack = await api('racks', 'POST', { roomId: room.id, name: `Search rack ${suffix}`, units: 42 });
  const assets: any[] = [];
  const baseDay = new Date();
  baseDay.setUTCHours(0, 0, 0, 0);
  const dateAfter = (days: number) => new Date(baseDay.getTime() + days * 86400000).toISOString().slice(0, 10);
  const specifications = [
    { hostname: 'WebAlpha', vendor: 'Dell', model: 'R650', type: '机架式服务器', warrantyType: '官方维保', expire: dateAfter(10), businessIp: '192.0.2.201' },
    { hostname: 'DbAlpha', vendor: 'Dell', model: 'R750', type: '存储', warrantyType: '第三方维保', expire: dateAfter(60), businessIp: '192.0.2.202' },
    { hostname: 'WebBeta', vendor: 'Huawei', model: '2288H', type: '机架式服务器', warrantyType: '官方维保', expire: dateAfter(15), businessIp: '192.0.2.203' },
    { hostname: 'NoDate', vendor: 'Dell', model: 'R650', type: '机架式服务器', warrantyType: '官方维保', expire: '', businessIp: '192.0.2.204' },
  ];
  const rows = async (indices: number[]) => {
    for (let i = 0; i < assets.length; i++) await expect(page.getByRole('cell', { name: assets[i].serial, exact: true })).toHaveCount(indices.includes(i) ? 1 : 0);
  };
  const multi = async (label: string, values: string[]) => {
    const select = page.getByRole('combobox', { name: label });
    await select.press('ArrowDown');
    for (const value of values) await page.locator('.ant-select-dropdown:visible').locator('.ant-select-item-option-content').getByText(value, { exact: true }).click();
    await page.getByRole('textbox', { name: '综合搜索设备' }).click();
    await expect(page.locator('.ant-select-dropdown:visible')).toHaveCount(0);
  };
  const add = async (field: string, value: string) => {
    await page.getByRole('button', { name: /添加条件/ }).click();
    await page.getByRole('combobox', { name: '筛选字段' }).press('ArrowDown');
    await page.locator('.ant-select-dropdown:visible').locator('.ant-select-item-option-content').getByText(field, { exact: true }).click();
    await page.getByRole('textbox', { name: '筛选内容' }).fill(value);
    await page.getByRole('button', { name: '添加筛选条件', exact: true }).click();
  };
  const setDate = async (placeholder: string, value: string) => {
    const input = page.getByPlaceholder(placeholder, { exact: true });
    await input.click();
    await input.fill(value);
    await input.press('Enter');
  };
  try {
    for (let i = 0; i < specifications.length; i++) {
      assets.push(
        await api('devices', 'POST', {
          ...specifications[i],
          customer: 'Search acceptance',
          hostname: `${specifications[i].hostname}-${suffix}`,
          serial: `SEARCH-${i}-${suffix}`,
          business: '检索平台',
          rackId: rack.id,
          disks: [],
        }),
      );
    }
    await page.goto('/targets/cmdb');
    const query = page.getByRole('textbox', { name: '综合搜索设备' });
    await query.fill(suffix);
    await rows([0, 1, 2, 3]);
    const filter = page.getByRole('region', { name: '设备组合筛选' });
    await expect(filter.locator('input[type=date]')).toHaveCount(0);
    expect(await page.getByPlaceholder('过保开始日期').evaluate((input: HTMLInputElement) => input.type)).toBe('text');
    const box = (await filter.boundingBox())!;
    expect(box.height).toBeLessThan(230);
    const firstRow = page.getByRole('row').filter({ has: page.getByRole('cell', { name: assets[0].serial, exact: true }) });
    await firstRow.getByRole('checkbox').check();
    await expect(page.getByRole('button', { name: /删除所选/ })).toBeEnabled();
    await query.fill(`${suffix} dElL WebAlpha`);
    await rows([0]);
    await expect(page.getByRole('button', { name: /删除所选/ })).toBeDisabled();
    await query.fill(suffix);
    await add('业务 IP', '192.0.2.201');
    await rows([0]);
    await page.getByRole('button', { name: '移除筛选 业务 IP：192.0.2.201', exact: true }).click();
    await rows([0, 1, 2, 3]);
    await add('厂商', 'Dell');
    await rows([0, 1, 3]);
    await multi('设备类型筛选', ['存储']);
    await rows([1]);
    await multi('设备类型筛选', ['机架式服务器']);
    await rows([0, 1, 3]);
    await multi('保修类型筛选', ['官方维保']);
    await rows([0, 3]);
    await multi('保修类型筛选', ['第三方维保']);
    await rows([0, 1, 3]);
    await multi('保修状态筛选', ['即将过保', '在保']);
    await rows([0, 1]);
    await setDate('过保开始日期', dateAfter(1));
    await setDate('过保结束日期', dateAfter(30));
    await query.click();
    await rows([0]);
    const downloadEvent = page.waitForEvent('download');
    await page.getByRole('button', { name: /数据导出/ }).click();
    const download = await downloadEvent;
    const csv = readFileSync((await download.path())!, 'utf8');
    expect(csv).toContain(assets[0].serial);
    for (const asset of assets.slice(1)) expect(csv).not.toContain(asset.serial);
    await testInfo.attach('cmdb-search-desktop', { body: await page.screenshot(), contentType: 'image/png' });
    await page.setViewportSize({ width: 800, height: 768 });
    expect((await filter.boundingBox())!.height).toBeLessThan(300);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await testInfo.attach('cmdb-search-narrow', { body: await page.screenshot(), contentType: 'image/png' });
    await page.getByRole('button', { name: '重置筛选', exact: true }).click();
    await expect(query).toHaveValue('');
    await expect(page.getByPlaceholder('过保开始日期')).toHaveValue('');
    await expect(page.getByPlaceholder('过保结束日期')).toHaveValue('');
    await expect(page.getByRole('button', { name: /^移除筛选/ })).toHaveCount(0);
    await query.fill(suffix);
    await setDate('过保结束日期', dateAfter(30));
    await query.click();
    await rows([0, 2]);
    await page.getByRole('button', { name: '重置筛选', exact: true }).click();
    await query.fill(suffix);
    await setDate('过保开始日期', dateAfter(60));
    await query.click();
    await rows([1]);
    expect(errors).toEqual([]);
  } finally {
    const current = await api('devices');
    for (const asset of assets) {
      const stored = current.find((item: any) => item.id === asset.id);
      if (stored) {
        const detached = await api(`devices/${asset.id}`, 'PUT', { ...stored, rackId: '', rack: '', room: '', uStart: null, uEnd: null });
        await api('delete', 'POST', [detached]);
      }
    }
    await api(`racks/${rack.id}`, 'DELETE', { version: rack.version });
    await api(`rooms/${room.id}`, 'DELETE');
  }
});
