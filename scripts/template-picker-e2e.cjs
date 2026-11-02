// Read-only acceptance of the complete template picker on this LAN preview.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { chromium, expect } = require('../frontend/node_modules/@playwright/test');

async function main() {
  const instance = process.env.N9E_TEMPLATE_E2E_INSTANCE;
  const artifacts = process.env.N9E_TEMPLATE_E2E_ARTIFACTS;
  assert(instance && artifacts, 'Set private preview and artifact directories');
  const manifest = JSON.parse(fs.readFileSync(path.join(instance, 'manifest.json')));
  assert.equal(manifest.role, 'development');
  assert.equal(manifest.lanHost, '192.168.0.152');
  const base = `http://192.168.0.152:${manifest.port}`;
  const credentials = JSON.parse(fs.readFileSync(path.join(instance, 'credentials.json')));
  fs.mkdirSync(artifacts, { recursive: true, mode: 0o700 });
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1600, height: 1100 } });
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  const report = { buildId: manifest.buildId, base, templates: [], errors: [] };
  page.on('pageerror', (e) => report.errors.push(e.message));
  try {
    const identity = await (await context.request.get(base + '/__preview/info')).json();
    assert.equal(identity.buildId, manifest.buildId);
    await page.goto(base + '/login');
    await page.locator('#username').fill(credentials.username);
    await page.locator('#password').fill(credentials.password);
    await page.locator('button.ant-btn-primary').click();
    await page.waitForURL((u) => !u.pathname.startsWith('/login'));
    const token = await page.evaluate(() => localStorage.getItem('access_token'));
    const api = async (resource) => {
      const response = await context.request.get(base + '/api/n9e/' + resource, { headers: { Authorization: `Bearer ${token}` } });
      const body = await response.json();
      assert(response.ok() && !body.err, `GET ${resource} failed`);
      return body.dat;
    };
    const catalog = await api('collection/catalog');
    const byID = new Map(catalog.categories.map((c) => [c.id, c]));
    const groups = await api('busi-groups');
    await page.goto(base + `/targets?ids=${groups[0].id}&isLeaf=true`);
    await page.getByRole('button', { name: /新增采集设备|Add collection device/ }).click();
    const editor = page.getByRole('dialog', { name: /^(新增采集设备|Add collection device)$/ });
    await expect(editor.getByText(`全部 ${catalog.entries.length} 个模板均可按分类查看；自动接入状态以各模板的发布与采集能力为准。`, { exact: true })).toBeVisible();
    const select = async (label, name) => {
      const control = editor.getByRole('combobox', { name: label });
      await control.locator('xpath=ancestor::div[contains(@class,"ant-select-selector")][1]').click();
      // Ant Design virtualizes long lists. Use the same built-in search a
      // person can use, so off-screen options are rendered before selecting.
      await control.fill(name);
      await page.locator('.ant-select-dropdown:visible').getByText(name, { exact: true }).click();
    };
    for (const entry of catalog.entries) {
      const route = [];
      for (let id = entry.category_id; id;) {
        const category = byID.get(id);
        assert(category, `Missing category for ${entry.ident}`);
        route.unshift(category);
        id = category.parent_id;
        assert(route.length <= 3);
      }
      assert.equal(route.length, 3, entry.ident);
      for (let i = 0; i < 3; i++) await select(['设备类型', '品牌', '设备型号'][i], route[i].name + (route[i].disabled ? '（已停用）' : ''));
      const related = catalog.entries.filter((e) => e.category_id === entry.category_id);
      const status = entry.disabled || entry.onboarding.state === 'disabled' ? '已停用' : entry.onboarding.ready ? `可自动接入 · v${entry.published.version}` : '尚未发布 / 待接入';
      if (related.length > 1) await select('适用模板', `${entry.name} · ${status}`);
      await expect(editor.locator('.ant-alert-message').filter({ hasText: `${entry.name} · ${status}` })).toBeVisible();
      if (entry.onboarding.ready) {
        await expect(editor.getByRole('button', { name: '下一步', exact: true })).toBeEnabled();
      } else {
        await expect(editor.getByText('模板接入状态：待完成', { exact: true })).toBeVisible();
        await expect(editor.getByRole('button', { name: '下一步', exact: true })).toBeDisabled();
        for (const issue of entry.onboarding.issues) await expect(editor.getByText(issue, { exact: true })).toBeVisible();
      }
      const definition = entry.published?.definition || entry.draft;
      // Check fields with no conditional dependencies. No secret is entered.
      for (const field of definition.fields.filter((f) => !f.when?.length)) {
        await expect(editor.getByLabel(field.label, { exact: true })).toBeVisible();
      }
      report.templates.push({ componentId: entry.component_id, ident: entry.ident, path: route.map((c) => c.name), ready: entry.onboarding.ready, fields: definition.fields.length });
      if (report.templates.length % 10 === 0) console.log(`Selected ${report.templates.length}/${catalog.entries.length} catalog templates`);
      if (['Redis', 'SQLServer', 'Redfish', 'Linux', 'AliYun', 'MySQL_84_Remote'].includes(entry.ident)) await page.screenshot({ path: path.join(artifacts, `${entry.ident}.png`) });
    }
    assert.equal(report.templates.length, catalog.entries.length);
    // All unpublished templates have separate catalog selections even though
    // their release IDs are empty. Inspect a real guide loaded from detail.
    const redis = catalog.entries.find((e) => e.ident === 'Redis');
    assert(redis);
    const route = [];
    for (let id = redis.category_id; id;) { const c = byID.get(id); route.unshift(c); id = c.parent_id; }
    for (let i = 0; i < 3; i++) await select(['设备类型', '品牌', '设备型号'][i], route[i].name);
    await editor.getByText('查看原模板的采集说明、仪表盘与告警规则', { exact: true }).click();
    await expect(editor.locator('pre')).toContainText('info');
    await expect(editor.getByLabel('SNMP 版本', { exact: true })).toHaveCount(0);
    await page.screenshot({ path: path.join(artifacts, 'redis-original-guide.png') });
    await editor.getByRole('button', { name: /取\s*消|Cancel/ }).click();
    // Reload the production route and ensure the directory is still complete.
    await page.reload();
    await page.getByRole('button', { name: /新增采集设备|Add collection device/ }).click();
    await expect(page.getByText(`全部 ${catalog.entries.length} 个模板均可按分类查看；自动接入状态以各模板的发布与采集能力为准。`, { exact: true })).toBeVisible();
    assert.equal((await (await context.request.get(base + '/__preview/info')).json()).buildId, report.buildId, 'preview changed during acceptance');
    assert.deepEqual(report.errors, []);
    report.passed = true;
    console.log(JSON.stringify({ buildId: report.buildId, selected: report.templates.length, ready: report.templates.filter((t) => t.ready).length, pageErrors: report.errors.length }));
  } finally {
    fs.writeFileSync(path.join(artifacts, 'result.json'), JSON.stringify(report, null, 2), { mode: 0o600 });
    await browser.close();
  }
}
main().catch((e) => { console.error(e.message); process.exitCode = 1; });
