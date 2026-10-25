import { readFileSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { test, expect } from '@playwright/test';

test.use({ locale: 'zh-CN' });

test('rack hierarchy, default fit, four-way comparison, cross-rack move and guarded deletion', async ({ page, request }, testInfo) => {
  page.setDefaultTimeout(15000);
  const info = await (await request.get('/__preview/info')).json();
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
  expect(info.ready).toBe(true);
  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  const errors: string[] = [];
  page.on('pageerror', (error) => {
    errors.push(error.message);
    console.error('Racks browser error:', error.message);
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
  const room = await api('rooms', 'POST', { name: `Rack acceptance ${suffix}` });
  const racks: any[] = [];
  let device: any;
  const catalogHas = async (type: string, id: string) => (await api('catalog'))[type].some((item: any) => item.id === id);
  const confirm = () => page.getByRole('dialog').getByRole('button', { name: /^删\s*除$/ });
  try {
    for (const name of ['A', 'B', 'C', 'D', 'E']) {
      racks.push(await api('racks', 'POST', { roomId: room.id, name: `${name}-${suffix}`, units: name === 'B' ? 60 : 42 }));
    }
    device = await api('devices', 'POST', { customer: 'Rack acceptance', serial: `RACK-${suffix}`, hostname: `RACK-${suffix}`, rackId: racks[0].id, uStart: 2, uEnd: 3 });
    await page.goto('/targets/racks');
    const roomNode = page.locator('.rack-room').filter({ has: page.locator('.rack-room-name', { hasText: suffix }) });
    await expect(roomNode).not.toHaveAttribute('open', '');
    await roomNode.locator('summary').click();
    await expect(roomNode).toHaveAttribute('open', '');
    await expect(roomNode.locator('.rack-list-item')).toHaveCount(5);
    await roomNode.getByRole('button', { name: `编辑机房 ${room.name}`, exact: true }).click();
    const renamedRoom = `Renamed room ${suffix}`;
    await page.getByRole('dialog').getByLabel('机房名称', { exact: true }).fill(renamedRoom);
    await page
      .getByRole('dialog')
      .getByRole('button', { name: /保\s*存/ })
      .click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    room.name = renamedRoom;
    await expect(roomNode.locator('.rack-room-name')).toHaveText(renamedRoom);
    await roomNode.getByRole('button', { name: `编辑机柜 ${racks[0].name}`, exact: true }).click();
    const renamedRack = `A-edited-${suffix}`;
    await page.getByRole('dialog').getByLabel('机柜名称', { exact: true }).fill(renamedRack);
    await page
      .getByRole('dialog')
      .getByRole('button', { name: /保\s*存/ })
      .click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    racks[0].name = renamedRack;
    racks[0].version += 1;
    await roomNode.locator('.rack-list-item').filter({ hasText: racks[0].name }).click();
    await expect(page.getByRole('button', { name: '还原 100%' })).toHaveCount(0);
    const fits = () =>
      page.locator('.rack-panel').evaluateAll((panels) =>
        panels.every((panel) => {
          const viewport = panel.querySelector('.rack-viewport')!;
          const cabinet = panel.querySelector('.rack-zoom-object')!;
          const view = viewport.getBoundingClientRect();
          const object = cabinet.getBoundingClientRect();
          return object.width <= view.width - 40 && object.height <= view.height - 40 && object.y >= view.y && object.bottom <= view.bottom;
        }),
      );
    await expect.poll(fits).toBe(true);
    await page.getByRole('button', { name: `删除机房 ${room.name}`, exact: true }).click();
    await confirm().click();
    await expect(page.getByRole('dialog').getByRole('alert')).toContainText('还有机柜');
    await page
      .getByRole('dialog')
      .getByRole('button', { name: /取\s*消/ })
      .click();
    await roomNode.getByRole('button', { name: `删除机柜 ${racks[0].name}`, exact: true }).click();
    await confirm().click();
    await expect(page.getByRole('dialog').getByRole('alert')).toContainText('还有设备');
    await page
      .getByRole('dialog')
      .getByRole('button', { name: /取\s*消/ })
      .click();
    const comparison = page.locator('.rack-compare');
    await comparison.locator('.ant-select-selector').click();
    for (const rack of racks.slice(1, 4)) {
      await page.locator('.ant-select-dropdown:visible').getByText(`${room.name} / ${rack.name}`, { exact: true }).click();
    }
    await expect(page.locator('.rack-panel')).toHaveCount(4);
    await expect(page.locator('.ant-select-dropdown:visible .ant-select-item-option').filter({ hasText: `${room.name} / ${racks[4].name}` })).toHaveClass(
      /ant-select-item-option-disabled/,
    );
    await page.getByPlaceholder('搜索机房 / 机柜').click();
    await expect(page.locator('.ant-select-dropdown')).toBeHidden();
    await expect.poll(fits).toBe(true);
    const horizontal = await page.locator('.rack-panels').evaluate((el) => ({ scroll: el.scrollWidth, client: el.clientWidth }));
    expect(horizontal.scroll).toBeGreaterThan(horizontal.client);
    await testInfo.attach('four-cabinets-desktop', { body: await page.screenshot(), contentType: 'image/png' });
    const transfer = await page.evaluateHandle(() => new DataTransfer());
    await page.locator('.rack-device').filter({ hasText: device.serial }).dispatchEvent('dragstart', { dataTransfer: transfer });
    const target = page.getByRole('button', { name: `${racks[3].name} 17U`, exact: true });
    await target.scrollIntoViewIfNeeded();
    const bounds = (await target.boundingBox())!;
    const pointer = { clientX: bounds.x + bounds.width / 2, clientY: bounds.y + bounds.height / 2 };
    await target.dispatchEvent('dragover', { dataTransfer: transfer, ...pointer });
    await target.dispatchEvent('drop', { dataTransfer: transfer, ...pointer });
    await expect.poll(async () => (await api('devices')).find((item: any) => item.id === device.id).rackId).toBe(racks[3].id);
    device = (await api('devices')).find((item: any) => item.id === device.id);
    expect([device.uStart, device.uEnd]).toEqual([17, 18]);
    const fourth = page.locator('.rack-panel').nth(3);
    const percentage = fourth.getByLabel('机柜图缩放比例');
    await fourth.getByRole('button', { name: '放大机柜图' }).click();
    const zoom = await percentage.textContent();
    await page.setViewportSize({ width: 800, height: 768 });
    await expect(percentage).toHaveText(zoom!);
    await fourth.getByRole('button', { name: '适应窗口' }).click();
    await expect.poll(fits).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await testInfo.attach('four-cabinets-narrow', { body: await page.screenshot(), contentType: 'image/png' });
    await page.getByRole('button', { name: `关闭并排机柜 ${racks[3].name}`, exact: true }).click();
    await expect(page.locator('.rack-panel')).toHaveCount(3);
    // A non-selected rack can be deleted directly in the hierarchy without changing the primary.
    await roomNode.getByRole('button', { name: `删除机柜 ${racks[4].name}`, exact: true }).click();
    await confirm().click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(await catalogHas('racks', racks[4].id)).toBe(false);
    await expect(page.locator('.rack-title h2').first()).toHaveAttribute('title', racks[0].name);
    // Delete from the actual rack device details, verify disappearance and recycle-bin persistence.
    await roomNode.getByRole('button', { name: `${racks[3].name} ${racks[3].units}U`, exact: true }).click();
    await page.locator('.rack-device').filter({ hasText: device.serial }).click();
    await page.getByRole('dialog').getByRole('button', { name: '删除设备', exact: true }).click();
    await page.getByRole('tooltip').getByRole('button', { name: '确认删除', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect.poll(async () => (await api('devices')).some((item: any) => item.id === device.id)).toBe(false);
    await expect(page.locator('.rack-device').filter({ hasText: device.serial })).toHaveCount(0);
    const recycled = (await api('devices?deleted=true')).find((item: any) => item.id === device.id);
    expect(recycled).toBeTruthy();
    await api('restore', 'POST', [recycled]);
    device = (await api('devices')).find((item: any) => item.id === device.id);
    await roomNode.locator('.rack-list-item').filter({ hasText: racks[0].name }).click();
    // Delete a primary empty rack through the UI and verify the selection recovers.
    device = await api(`devices/${device.id}`, 'PUT', { ...device, rackId: '', room: '', rack: '', uStart: null, uEnd: null });
    await roomNode.getByRole('button', { name: `删除机柜 ${racks[0].name}`, exact: true }).click();
    await confirm().click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(await catalogHas('racks', racks[0].id)).toBe(false);
    await expect(page.locator('.rack-panel').first()).toBeVisible();
    for (const rack of racks.slice(1, 4)) await api(`racks/${rack.id}`, 'DELETE', { version: rack.version });
    await page.getByRole('button', { name: /^刷\s*新$/ }).click();
    await expect(roomNode.locator('.rack-list-item')).toHaveCount(0);
    await page.getByRole('button', { name: `删除机房 ${room.name}`, exact: true }).click();
    await confirm().click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(roomNode).toHaveCount(0);
    expect(await catalogHas('rooms', room.id)).toBe(false);
    expect(errors).toEqual([]);
  } finally {
    if (device) {
      const current = (await api('devices')).find((item: any) => item.id === device.id);
      if (current) {
        const detached = await api(`devices/${device.id}`, 'PUT', { ...current, rackId: '', room: '', rack: '', uStart: null, uEnd: null });
        await api('delete', 'POST', [detached]);
      }
    }
    const current = await api('catalog');
    for (const rack of current.racks.filter((r: any) => r.roomId === room.id)) await api(`racks/${rack.id}`, 'DELETE', { version: rack.version });
    if (current.rooms.some((r: any) => r.id === room.id)) await api(`rooms/${room.id}`, 'DELETE');
  }
});
