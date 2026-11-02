// Native UI acceptance against this worktree's disposable LAN preview only.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { chromium, expect } = require('../frontend/node_modules/@playwright/test');

async function main() {
  const instance = process.env.N9E_TEMPLATE_E2E_INSTANCE;
  const artifacts = process.env.N9E_TEMPLATE_E2E_ARTIFACTS;
  if (!instance || !artifacts) throw new Error('Set the private preview and artifact directories');
  const manifest = JSON.parse(fs.readFileSync(path.join(instance, 'manifest.json')));
  assert.equal(manifest.role, 'development');
  assert.equal(manifest.lanHost, '192.168.0.152');
  const base = `http://${manifest.lanHost}:${manifest.port}`;
  const credentials = JSON.parse(fs.readFileSync(path.join(instance, 'credentials.json')));
  fs.mkdirSync(artifacts, { recursive: true, mode: 0o700 });
  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
  context.setDefaultTimeout(15000);
  context.setDefaultNavigationTimeout(30000);
  const page = await context.newPage();
  const errors = [], checks = [];
  const record = (message) => { checks.push(message); console.log(message); };
  page.on('pageerror', (e) => errors.push(e.message));
  let api, device, oldDevice, component, entry, credential;
  const report = { base, buildId: manifest.buildId, checks, errors };
  const refreshEntry = async () => api(`collection/catalog/${component.id}`);
  const select = async (locator, name) => {
    const selector = await locator.evaluate((e) => e.classList.contains('ant-select-selector')) ? locator : locator.locator('xpath=ancestor::div[contains(@class,"ant-select-selector")][1]');
    await selector.click();
    await page.locator('.ant-select-dropdown:visible .ant-select-item-option').filter({ hasText: name }).last().click();
  };
  try {
    const identity = await (await context.request.get(base + '/__preview/info')).json();
    assert.equal(identity.buildId, manifest.buildId);
    await page.goto(base + '/login');
    await page.locator('#username').fill(credentials.username);
    await page.locator('#password').fill('incorrect-preview-password');
    await page.locator('button.ant-btn-primary').click();
    await expect(page).toHaveURL(/login/);
    await page.locator('#password').fill(credentials.password);
    await page.locator('button.ant-btn-primary').click();
    await page.waitForURL((u) => !u.pathname.startsWith('/login'));
    const token = await page.evaluate(() => localStorage.getItem('access_token'));
    api = async (resource, method = 'GET', data) => {
      const response = await context.request.fetch(base + '/api/n9e/' + resource, { method, data, headers: { Authorization: `Bearer ${token}` } });
      const raw = await response.text();
      if (!raw.trim()) throw new Error(`${method} ${resource}: ${response.status()} empty response`);
      let body;try {body=JSON.parse(raw);} catch {throw new Error(`${method} ${resource}: ${response.status()} non-JSON response`);}
      if (!response.ok() || body.err) throw new Error(`${method} ${resource}: ${response.status()} ${body.err || 'request failed'}`);
      return body.dat;
    };
    const anonymous = await context.request.get(base + '/api/n9e/collection/catalog');
    assert.equal(anonymous.status(), 401);
    const seed = 'Catalog_QA_' + Date.now();
    const folder = path.join(__dirname, '../integrations/Windows_SNMP');
    const created = await api('builtin-components', 'POST', [{ ident: seed, logo: '', readme: fs.readFileSync(path.join(folder, 'markdown/README.md'), 'utf8'), disabled: 0 }]);
    assert.deepEqual(created, {});
    component = (await api('builtin-components')).find((c) => c.ident === seed);
    const metrics = JSON.parse(fs.readFileSync(path.join(folder, 'metrics/categraf.json'))).map((m) => ({ ...m, id: 0, typ: seed }));
    assert.deepEqual(await api('builtin-metrics', 'POST', metrics), {});
    const payloads = [];
    for (const [type, subfolder] of [['alert', 'alerts'], ['dashboard', 'dashboards']]) {
      for (const name of fs.readdirSync(path.join(folder, subfolder)).filter((f) => f.endsWith('.json'))) {
        const content = JSON.parse(fs.readFileSync(path.join(folder, subfolder, name)));
        for (const item of Array.isArray(content) ? content : [content]) payloads.push({ component_id: component.id, type, cate: '', name: item.name, content: JSON.stringify({ ...item, uuid: 0 }), uuid: 0 });
      }
    }
    assert.deepEqual(await api('builtin-payloads', 'POST', payloads), {});
    entry = await refreshEntry();
    assert(entry.published_version_id);
    record('real native four-part fixture imported and catalogued into an immutable release');
    await page.goto(base + '/components');
    await page.getByPlaceholder('搜索模板或分类').fill(seed);
    const card = page.locator(`[data-template-id="${component.id}"]`);
    await expect(card).toBeVisible();
    assert.equal(await card.evaluate((e) => getComputedStyle(e).backgroundColor), 'rgb(255, 255, 255)');
    await card.hover();
    await card.getByRole('button', { name: `编辑基本信息 ${seed}` }).click();
    const modal = page.getByRole('dialog', { name: '编辑基本信息' });
    await expect(modal).toBeVisible();
    await expect(page.locator('.builtin-drawer-tabs:visible')).toHaveCount(0);
    await expect(modal.locator('textarea')).toHaveCount(0);
    const displayName = seed + ' renamed';
    await modal.locator('#name').fill(displayName);
    const chooser = page.waitForEvent('filechooser');
    await modal.getByRole('button', { name: '导入本地图标' }).click();
    await (await chooser).setFiles({ name: 'catalog-icon.svg', mimeType: 'image/svg+xml', buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="42" height="42"><rect fill="#7354dc" width="42" height="42"/></svg>') });
    await modal.getByRole('button', { name: /确 定|OK/ }).click();
    await expect(modal).not.toBeVisible();
    entry = await refreshEntry();
    assert.equal(entry.name, displayName);
    assert(entry.logo.startsWith('data:image/svg+xml;base64,'));
    assert.equal((await api('builtin-components')).find((c) => c.id === component.id).readme, component.readme);
    await page.reload();
    await expect(card.getByRole('button', { name: `配置模板 ${displayName}` })).toBeVisible();
    record('white card; independent basic editor; local icon/name persisted without rewriting instructions; reload restores data');
    await page.getByRole('button', { name: '全部折叠' }).click();
    await page.getByRole('button', { name: '列表模式' }).click();
    await expect(page.getByText(displayName, { exact: true })).toBeVisible();
    await page.getByRole('button', { name: '卡片模式' }).click();
    await expect(page.getByPlaceholder('搜索模板或分类')).toHaveValue(seed);
    await card.getByRole('button', { name: `配置模板 ${displayName}` }).click();
    const drawer = page.locator('.ant-drawer-content').last();
    for (const section of ['采集方式与兼容性', '设备添加表单', '采集与指标说明', '仪表盘模板', '告警规则与通知']) await expect(drawer.getByRole('menuitem', { name: section })).toBeVisible();
    const originalVersion = entry.published_version_id;
    const collectors = await api('collection/collectors');
    const collector = collectors.find((c) => c.group_ids.length);
    assert(collector, 'preview requires an existing authorized collector');
    const groups = await api('busi-groups');
    const group = groups.find((g) => g.id === collector.group_ids[0]);
    credential = await api('collection/credentials', 'POST', { name: seed, group_id: group.id, community: 'synthetic-catalog-community' });
    const oldResponse = await context.request.post(base + '/api/n9e/collection/devices', {headers:{Authorization:`Bearer ${token}`,'Idempotency-Key':seed+'-old'},data:{name:seed+' old',address:'192.0.2.91',ident:seed+'-old',group_id:group.id,collector_id:collector.id,credential_id:credential.id,template_version_id:originalVersion,enabled:false}});
    const oldBody = await oldResponse.json(); assert(!oldBody.err, oldBody.err); oldDevice=oldBody.dat;
    report.fixture = {componentId:component.id,credentialId:credential.id,oldDeviceId:oldDevice.id};
    fs.writeFileSync(path.join(artifacts,'owned-fixtures.json'),JSON.stringify(report.fixture,null,2),{mode:0o600});
    await drawer.getByRole('menuitem',{name:'设备添加表单'}).click();
    await drawer.getByRole('button',{name:'装入标准连接字段'}).click();
    await page.getByRole('dialog').getByRole('button',{name:/确 定|OK/}).click();
    await drawer.getByRole('row').filter({has:page.locator('code').filter({hasText:/^port$/})}).getByRole('button',{name:/^编\s*辑$/}).click();
    const fieldModal=page.getByRole('dialog',{name:'编辑设备字段'});
    await fieldModal.locator('#label').fill('模板指定 SNMP 端口');
    await fieldModal.locator('#help').fill('此字段来自完整模板草稿');
    await fieldModal.getByRole('button',{name:/确 定|OK/}).click();
    await drawer.getByRole('menuitem',{name:'采集与指标说明'}).click();
    const draftReadme = 'Catalog draft instructions '+seed;
    await drawer.locator('textarea').first().fill(draftReadme);
    await drawer.getByRole('button',{name:'保存草稿',exact:true}).click();
    await expect.poll(async()=>(await refreshEntry()).record_version).toBeGreaterThan(entry.record_version);
    entry=await refreshEntry(); assert.equal(entry.published_version_id,originalVersion);assert.equal(entry.draft.pack.readme,draftReadme);
    await api('builtin-components','PUT',{...component,readme:'outside-draft mutation '+seed});
    await drawer.getByRole('menuitem',{name:'告警规则与通知'}).click();
    await drawer.getByRole('button',{name:'编辑模板内容'}).first().click();
    const ruleModal=page.getByRole('dialog',{name:'编辑告警规则模板'});
    const ruleContent=JSON.parse(entry.draft.pack.alerts[0].content);ruleContent.prom_for_duration=600;
    await ruleModal.locator('#content').fill(JSON.stringify(ruleContent,null,2));
    await ruleModal.getByRole('button',{name:/确 定|OK/}).click();
    await drawer.getByRole('button',{name:'发布新版本',exact:true}).click();
    await expect.poll(async()=>(await refreshEntry()).published.version).toBe(2);
    entry=await refreshEntry();assert.equal(entry.published.definition.pack.readme,draftReadme);assert.equal(JSON.parse(entry.published.definition.pack.alerts[0].content).prom_for_duration,600);
    assert.equal((await api(`collection/devices/${oldDevice.id}`)).template_version_id,originalVersion);
    await page.screenshot({path:path.join(artifacts,'native-package-editor.png')});
    await drawer.locator('.ant-drawer-header .anticon-close').click();
    record('five-section package editor; full draft isolation; release snapshots edited instructions, form, dashboard, alerts and notification together; existing binding stays pinned');
    await page.goto(base+'/targets?ids='+group.id+'&isLeaf=true');
    await page.getByRole('button',{name:/新增采集设备|Add collection device/}).click();
    const editor=page.getByRole('dialog',{name:/^(新增采集设备|Add collection device)$/});
    for (const step of ['设备类型与采集配置','仪表盘与告警通知','确认添加']) await expect(editor.getByText(step,{exact:true})).toBeVisible();
    await expect(editor.getByRole('button',{name:'确认添加设备'})).toHaveCount(0);
    await expect(editor.getByRole('combobox',{name:'品牌'})).toBeDisabled();
    await editor.locator('#name').fill(seed);
    await select(editor.getByRole('combobox',{name:'设备类型'}),'待归类');
    await select(editor.getByRole('combobox',{name:'品牌'}),seed);
    await select(editor.getByRole('combobox',{name:'设备型号'}),'通用');
    await expect(editor.getByText('模板指定 SNMP 端口',{exact:true})).toBeVisible();
    await editor.locator('#address').fill('192.0.2.91');
    await editor.locator('#ident').fill(seed);
    await select(editor.locator('.ant-select-selector:has(#group_id)'),group.name);
    await select(editor.locator('.ant-select-selector:has(#collector_id)'),collector.name);
    const privateValue='synthetic-wizard-community';
    await editor.getByLabel('团体字',{exact:true}).fill(privateValue);
    // Switching the type clears private type-specific values and retains common fields.
    await select(editor.getByRole('combobox',{name:'设备类型'}),'操作系统');
    await expect(editor.locator('#name')).toHaveValue(seed);
    await select(editor.getByRole('combobox',{name:'设备类型'}),'待归类');
    await select(editor.getByRole('combobox',{name:'品牌'}),seed);
    await select(editor.getByRole('combobox',{name:'设备型号'}),'通用');
    await expect(editor.getByLabel('团体字',{exact:true})).toHaveValue('');
    await editor.getByLabel('团体字',{exact:true}).fill(privateValue);
    await editor.locator('#enabled').click(); // Test objects never contact a target.
    await page.screenshot({path:path.join(artifacts,'native-device-step1.png')});
    await editor.getByRole('button',{name:'下一步',exact:true}).click();
    await expect(editor.getByText('按模板应用完整监控包',{exact:true})).toBeVisible();
    await expect(editor.getByText('600 秒',{exact:true})).toBeVisible();
    await editor.getByRole('button',{name:'上一步',exact:true}).click();
    await expect(editor.getByLabel('团体字',{exact:true})).toHaveValue(privateValue);
    await editor.getByRole('button',{name:'下一步',exact:true}).click();
    await page.screenshot({path:path.join(artifacts,'native-device-step2.png')});
    await editor.getByRole('button',{name:'下一步',exact:true}).click();
    await expect(editor.getByText('确认设备添加方案',{exact:true})).toBeVisible();
    assert(!(await editor.innerText()).includes(privateValue));
    await expect(editor.getByText('••••••（加密保存）',{exact:true})).toBeVisible();
    await page.screenshot({path:path.join(artifacts,'native-device-step3.png')});
    const saved=page.waitForResponse((r)=>r.url().endsWith('/collection/devices')&&r.request().method()==='POST');
    await editor.getByRole('button',{name:'确认添加设备',exact:true}).click();
    const result=await(await saved).json();assert(!result.err,result.err);device=result.dat;
    assert.equal(device.enabled,false);assert.equal(device.profile.catalog.version,2);assert.equal(device.profile.boards.length,1);assert.equal(device.profile.rules.length,7);assert.equal(device.profile.secret_fields.community,true);
    assert(!JSON.stringify(device).includes(privateValue));
    record('three-stage device wizard; inline taxonomy and generated password field; upstream reset and Back preservation; template resource confirmation; masked final review; atomic device/resource creation');
    report.fixture.deviceId=device.id;report.fixture.inlineCredentialId=device.credential_id;
    fs.writeFileSync(path.join(artifacts,'owned-fixtures.json'),JSON.stringify(report.fixture,null,2),{mode:0o600});
    const mysqlPage=await context.newPage();mysqlPage.on('pageerror',(e)=>errors.push(e.message));
    const mysqlSelect=async(locator,name)=>{const selector=await locator.evaluate((e)=>e.classList.contains('ant-select-selector'))?locator:locator.locator('xpath=ancestor::div[contains(@class,"ant-select-selector")][1]');await selector.click();await mysqlPage.locator('.ant-select-dropdown:visible .ant-select-item-option').filter({hasText:name}).last().click();};
    await mysqlPage.goto(base+'/targets?ids='+group.id+'&isLeaf=true');
    await mysqlPage.getByRole('button',{name:/新增采集设备|Add collection device/}).click();
    const databaseEditor=mysqlPage.getByRole('dialog',{name:/^(新增采集设备|Add collection device)$/});
    assert.equal(await databaseEditor.locator('.collection-wizard-layout').evaluate((e)=>getComputedStyle(e).display),'grid');
    await mysqlSelect(databaseEditor.getByRole('combobox',{name:'设备类型'}),'数据库');
    await mysqlSelect(databaseEditor.getByRole('combobox',{name:'品牌'}),'甲骨文');
    await mysqlSelect(databaseEditor.getByRole('combobox',{name:'设备型号'}),'MySQL 8.4 LTS');
    await expect(databaseEditor.getByLabel('MySQL 端口',{exact:true})).toHaveValue('3306');
    await expect(databaseEditor.getByLabel('监控账号',{exact:true})).toBeVisible();
    assert.equal(await databaseEditor.getByLabel('监控密码',{exact:true}).getAttribute('type'),'password');
    await expect(databaseEditor.getByLabel('团体字',{exact:true})).toHaveCount(0);
    await databaseEditor.locator('#name').fill(seed+' database preview');await databaseEditor.locator('#address').fill('192.0.2.92');
    await mysqlSelect(databaseEditor.locator('.ant-select-selector:has(#group_id)'),group.name);
    await mysqlSelect(databaseEditor.locator('.ant-select-selector:has(#collector_id)'),collector.name);
    await databaseEditor.getByLabel('监控账号',{exact:true}).fill('monitor');
    await databaseEditor.getByLabel('监控密码',{exact:true}).fill('preview-only-no-connection');
    await databaseEditor.getByRole('button',{name:'下一步',exact:true}).click();
    await expect(databaseEditor.getByText('MySQL unavailable',{exact:true})).toBeVisible();
    await databaseEditor.getByRole('button',{name:'下一步',exact:true}).click();
    await expect(databaseEditor.getByText('确认设备添加方案',{exact:true})).toBeVisible();
    assert(!(await databaseEditor.innerText()).includes('preview-only-no-connection'));
    await mysqlPage.screenshot({path:path.join(artifacts,'native-mysql-confirmation.png')});await mysqlPage.close();
    record('fresh direct device page loads wizard styles; database taxonomy selects MySQL 8.4 package, generates account/password and port 3306, and reaches the masked confirmation without saving or probing');
    assert.deepEqual(errors,[]);
    report.note='Paused synthetic SNMP bindings; real MySQL/Categraf behavior is covered separately by the explicit external-service gate; no notification delivery or physical hardware certification is claimed.';
    fs.writeFileSync(path.join(artifacts,'result.json'),JSON.stringify(report,null,2),{mode:0o600});
    console.log(JSON.stringify({buildId:report.buildId,checks,report:path.join(artifacts,'result.json')},null,2));
  } catch (error) {
    await page.screenshot({ path: path.join(artifacts, 'failure.png'), fullPage: true });
    throw error;
  } finally {
    for (const owned of [device,oldDevice].filter(Boolean)) if (api) {
      try { const current = await api(`collection/devices/${owned.id}`); if (!current.deleted) await api(`collection/devices/${owned.id}`, 'DELETE', { record_version: current.record_version }); }
      catch(error) { console.error('Fixture device cleanup:',error.message); }
    }
    if (api && component && !device && !oldDevice) {
      try {entry = await refreshEntry(); if (entry) await api(`collection/catalog/${component.id}`, 'DELETE', { record_version: entry.record_version });}
      catch(error) { console.error('Fixture cleanup:',error.message); }
    }
    // A completed fixture's soft-deleted device keeps its release evidence;
    // referenced template and encrypted credential are intentionally retained.
    await browser.close();
  }
}
main().catch((e) => { console.error(e.stack || e.message); process.exitCode = 1; });
