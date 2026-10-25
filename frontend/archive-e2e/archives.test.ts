import { readFileSync } from 'node:fs';
import { test, expect, Page } from '@playwright/test';
import { PDFDocument, PDFName, PDFNumber, PDFDict, PDFArray, StandardFonts, degrees } from 'pdf-lib';
const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAEAAAAAoCAIAAADBrGu+AAABMUlEQVR4nO2YQQ7DIAwEnT6wl/YZ7Wv6jV7aF/YQCblAjLGXIEvsiRyyu0OcCGV73D4UWZfZBbxaALMVD+D1vvLLYAB7e84QCYD3TuswANnkPO/ffREGgCu1JyBAOZ1AcVvenlAAvD2cQTYEAJQBQIaj0U/yAgyamap52Z6cAEKAH0zTnjwA1QAUg7I9mQGUATYGfXuyAcgB2aVzluT2ZADQbI+HQfjkV9UHoH+4muymv0YdAF2jSf0vdK//Li2AzV3PYPMnJYDZXfaB+LcBnO2bL7TTvwEA2XuBwe8vAaAmR3mvzf8QANi+dCgP3mb/OgC8femDOsZWAAa1F+SJyAFGty8NnRF/AOfsPT94+yO27O90YjhhciDKRwi1MacpfwLhFPLHFtcCmK0FMFsLYLZ+hiSuX0B2nRwAAAAASUVORK5CYII=',
  'base64',
);
async function login(page: Page) {
  const credentials = JSON.parse(readFileSync(process.env.N9E_E2E_CREDENTIALS_FILE!, 'utf8'));
  await page.goto('/login');
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill(credentials.password);
  await page.locator('button.ant-btn-primary').click();
  await page.waitForURL((u) => !u.pathname.startsWith('/login'));
}
async function api(page: Page, path: string, method = 'GET', data?: any) {
  return page.evaluate(
    async ({ path, method, data }) => {
      const response = await fetch('/api/n9e/archives' + path, {
        method,
        headers: { Authorization: `Bearer ${localStorage.getItem('access_token')}`, 'Content-Type': 'application/json', 'X-Language': 'zh_CN' },
        ...(data === undefined ? {} : { body: JSON.stringify(data) }),
      });
      const result = await response.json();
      if (!response.ok || result.err) throw new Error(result.err || String(response.status));
      return result.dat;
    },
    { path, method, data },
  );
}
async function create(page: Page, category: string, title: string) {
  await page.goto(`/archives?tab=${category}`);
  await expect(page.locator('.archive-header-tabs .ant-tabs-tab')).toHaveCount(4);
  await page.getByRole('button', { name: '新建档案' }).click();
  const modal = page.locator('.ant-modal:visible');
  await modal.locator('#title').fill(title);
  await modal.getByRole('button', { name: '保存草稿' }).click();
  await page.waitForURL(/\/archives\/\d+$/);
  return Number(new URL(page.url()).pathname.split('/').pop());
}
async function attach(page: Page, name: string, data: Buffer, mimeType: string) {
  await page.locator('input[type=file]').first().setInputFiles({ name, mimeType, buffer: data });
  await expect(page.getByRole('button', { name, exact: true })).toBeVisible();
}

test.beforeAll(async ({ request }) => {
  const response = await request.get('/__preview/info');
  const info = await response.json();
  expect(info.role).toBe('development');
  expect(info.buildId).toBe(process.env.N9E_E2E_BUILD_ID);
  expect(info.ready).toBe(true);
  expect(info.frontend.kind).toBe('in-tree');
});

test('training archive: materials, private autosave, lease, recovery, history and recycle bin', async ({ page, context }, testInfo) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await login(page);
  const archiveID = await create(page, 'training', `培训验收 ${Date.now()}`);
  const rejected = page.waitForResponse((r) => r.url().endsWith(`/${archiveID}/archive`) && r.request().method() === 'POST');
  await page.getByRole('button', { name: /^归\s*档$/ }).click();
  expect((await rejected).status()).toBe(422);
  await page.locator('.archive-section-title .ant-select-selector').click();
  await page.getByTitle('参会人员签到', { exact: true }).click();
  await attach(page, 'attendance.png', png, 'image/png');
  await page.getByRole('button', { name: '在线新建文档' }).click();
  const modal = page.locator('.ant-modal:visible');
  await modal.locator('#title').fill('在线培训讲义');
  await modal.getByRole('button', { name: '创建并开始编写' }).click();
  await page.waitForURL(/\/documents\/\d+/);
  const documentID = Number(new URL(page.url()).pathname.split('/').pop());
  await expect(page.locator('.archive-canvas canvas').first()).toBeVisible();
  await page
    .locator('.archive-canvas canvas')
    .first()
    .click({ position: { x: 110, y: 110 } });
  await page.keyboard.insertText('培训正文，第一版。');
  await page.getByText('Markdown', { exact: true }).click();
  const source = page.getByRole('textbox', { name: 'Markdown 文档源码' });
  await expect(source).toBeVisible();
  await expect(source).toHaveValue(/培训正文/);
  await source.fill('# 培训讲义\n\n第一版正式正文');
  await page.getByRole('button', { name: '保存正式版本' }).click();
  await expect(page.getByText('正式版本已保存', { exact: true })).toBeVisible();
  const second = await context.newPage();
  await second.goto(`/archives/${archiveID}/documents/${documentID}?edit=true`);
  await expect(second.locator('.ant-alert')).toContainText(/其他窗口|already being edited/);
  await second.close();
  await source.fill('# 培训讲义\n\n只属于本人的未发布草稿');
  await expect(page.locator('.archive-document-heading')).toContainText('私有草稿已保存', { timeout: 20000 });
  await page.reload();
  await page.locator('.ant-modal:visible').getByRole('button', { name: '继续草稿' }).click();
  await expect(source).toHaveValue(/未发布草稿/);
  const formal = await api(page, `/documents/${documentID}`);
  expect(JSON.stringify(formal.version.body)).toContain('第一版正式正文');
  expect(JSON.stringify(formal.version.body)).not.toContain('未发布草稿');
  await page.getByRole('button', { name: '退出编辑（保留草稿）' }).click();
  await expect(page.locator('.archive-pdf-container canvas').first()).toBeVisible({ timeout: 40000 });
  await page.getByRole('button', { name: '编辑文档' }).click();
  await page.locator('.ant-modal:visible').getByRole('button', { name: '继续草稿' }).click();
  await expect(source).toHaveValue(/未发布草稿/);
  await page.getByRole('button', { name: '保存正式版本' }).click();
  await expect.poll(async () => JSON.stringify((await api(page, `/documents/${documentID}`)).version.body)).toContain('未发布草稿');
  await page.getByRole('button', { name: '退出编辑（保留草稿）' }).click();
  await page.goto(`/archives/${archiveID}`);
  await page.getByRole('button', { name: /^归\s*档$/ }).click();
  await expect(page.locator('.archive-detail-heading')).toContainText('已归档');
  await page.getByRole('button', { name: '编辑信息' }).click();
  await page.locator('.ant-modal:visible #department').fill('手填培训部门');
  await page.locator('.ant-modal:visible').getByRole('button', { name: '保存信息' }).click();
  await expect(page.locator('.ant-descriptions')).toContainText('手填培训部门');
  await page.getByRole('button', { name: '更多档案操作' }).click();
  await page.getByRole('menuitem', { name: '操作日志' }).click();
  await expect(page.locator('.ant-drawer:visible')).toContainText('归档');
  await page.locator('.ant-drawer:visible .ant-drawer-close').click();
  await testInfo.attach('training-archive', { body: await page.screenshot(), contentType: 'image/png' });
  const current = await api(page, `/${archiveID}`);
  await api(page, `/${archiveID}`, 'DELETE', { revision: current.revision });
  await page.goto('/archives?tab=training');
  await page.getByRole('button', { name: '回收站' }).click();
  await expect(page.locator('tbody')).toContainText(current.title);
  await page
    .locator('tr')
    .filter({ hasText: current.title })
    .getByRole('button', { name: /恢\s*复/ })
    .click();
  const restored = await api(page, `/${archiveID}`);
  expect(restored.deleted).toBe(false);
  expect(errors).toEqual([]);
});

test('protected Markdown round-trip, real PDF reading/annotations and Word editable copy', async ({ page }, testInfo) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await login(page);
  const archiveID = await create(page, 'other', `文档验收 ${Date.now()}`);
  const asset = await page.evaluate(
    async (bytes) => {
      const f = new FormData();
      f.append('file', new File([new Uint8Array(bytes)], 'image.png', { type: 'image/png' }));
      const res = await fetch(`/api/n9e/archives/${location.pathname.split('/').pop()}/assets`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${localStorage.getItem('access_token')}` },
        body: f,
      });
      const data = await res.json();
      if (data.err) throw new Error(data.err);
      return data.dat;
    },
    [...png],
  );
  const merged = {
    type: 'table',
    value: '',
    colgroup: [{ width: 240 }, { width: 240 }],
    trList: [{ height: 40, tdList: [{ value: [{ value: '合并单元格内容' }], colspan: 2, rowspan: 1 }] }],
    extension: { archiveNodeID: 'merged' },
  };
  const floating = {
    type: 'image',
    value: `archive-asset:${asset.id}`,
    width: 80,
    height: 80,
    imgDisplay: 'float-top',
    imgFloatPosition: { x: 300, y: 500, pageNo: 0 },
    extension: { archiveNodeID: 'float' },
  };
  const body = {
    data: {
      header: [{ value: '验证页眉' }],
      main: [
        { type: 'title', value: '', level: 'first', valueList: [{ value: '文档完整性验证' }] },
        { value: '\n需要修改的普通正文。' },
        merged,
        { value: '\n表格之后。' },
        floating,
      ],
      footer: [{ value: '验证页脚' }],
    },
    options: { defaultFont: 'Noto Sans CJK SC', defaultSize: 16, width: 794, height: 1123, margins: [72, 72, 72, 72] },
  };
  const result = await api(page, `/${archiveID}/documents`, 'POST', { title: '复合文档', purpose: 'other', body });
  const doc = result.documents[result.documents.length - 1];
  await page.goto(`/archives/${archiveID}/documents/${doc.id}?edit=true`);
  await expect(page.locator('.archive-canvas canvas').first()).toBeVisible();
  await expect(page.getByText('Markdown', { exact: true })).toBeEnabled();
  const freeze = page.waitForRequest((r) => r.url().endsWith(`/documents/${doc.id}/draft`) && r.method() === 'PUT' && r.postDataJSON()?.switch_mode === true);
  await page.getByText('Markdown', { exact: true }).click();
  const frozen = (await freeze).postDataJSON().body;
  const source = page.getByRole('textbox', { name: 'Markdown 文档源码' });
  const before = await source.inputValue();
  expect(before).toContain('archive-block:b1');
  await source.fill(before.replace('需要修改', '已经修改').replace('<!--archive-block:b1-->', ''));
  await page.getByRole('button', { name: '保存正式版本' }).click();
  await expect(page.locator('.ant-alert-error')).toContainText('只读占位块');
  expect((await api(page, `/documents/${doc.id}`)).version.sequence).toBe(1);
  await source.fill(before.replace('需要修改', '已经修改'));
  await page.getByText('可视化', { exact: true }).click();
  await expect(page.locator('.archive-canvas canvas').first()).toBeVisible();
  await page.getByRole('button', { name: '保存正式版本' }).click();
  await expect.poll(async () => (await api(page, `/documents/${doc.id}`)).version.sequence).toBe(2);
  const saved = await api(page, `/documents/${doc.id}`);
  expect(JSON.stringify(saved.version.body)).toContain('已经修改');
  expect(saved.version.body.data.header).toEqual(body.data.header);
  expect(saved.version.body.data.main.find((n: any) => n.extension?.archiveNodeID === 'merged')).toEqual(
    frozen.data.main.find((n: any) => n.extension?.archiveNodeID === 'merged'),
  );
  expect(saved.version.body.data.main.find((n: any) => n.extension?.archiveNodeID === 'float')).toEqual(frozen.data.main.find((n: any) => n.extension?.archiveNodeID === 'float'));
  await page.getByRole('button', { name: '退出编辑（保留草稿）' }).click();
  await expect(page.locator('.archive-pdf-container canvas').first()).toBeVisible({ timeout: 40000 });
  await testInfo.attach('online-pdf-reader', { body: await page.screenshot(), contentType: 'image/png' });
  const [wordDownload] = await Promise.all([page.waitForEvent('download', { timeout: 40000 }), page.getByRole('button', { name: '导出 DOCX' }).click()]);
  const word = readFileSync((await wordDownload.path())!);
  const [pdfDownload] = await Promise.all([page.waitForEvent('download', { timeout: 40000 }), page.getByRole('button', { name: '导出 PDF' }).click()]);
  const plainPdf = readFileSync((await pdfDownload.path())!);
  const cropped = await PDFDocument.load(plainPdf);
  const helvetica = await cropped.embedFont(StandardFonts.Helvetica);
  cropped.addPage([300, 400]).drawText('Standard Helvetica OFL font check', { x: 20, y: 200, size: 14, font: helvetica });
  const croppedPage = cropped.getPage(0);
  const size = croppedPage.getSize();
  croppedPage.setCropBox(15, 25, size.width - 30, size.height - 50);
  croppedPage.setRotation(degrees(90));
  croppedPage.node.set(PDFName.of('UserUnit'), PDFNumber.of(2));
  const pdf = Buffer.from(await cropped.save());
  await page.goto(`/archives/${archiveID}`);
  await attach(page, 'report.docx', word, 'application/vnd.openxmlformats-officedocument.wordprocessingml.document');
  await attach(page, 'report.pdf', pdf, 'application/pdf');
  await page
    .locator('tr')
    .filter({ hasText: 'report.pdf' })
    .getByRole('button', { name: /批\s*注/ })
    .click();
  await expect(page.locator('.archive-pdf-container canvas').first()).toBeVisible();
  await page.locator('.archive-pdf-container .page').nth(1).scrollIntoViewIfNeeded();
  await expect(page.locator('.archive-pdf-container .page').nth(1).locator('.textLayer')).toContainText('Standard Helvetica OFL font check');
  await page.locator('.archive-pdf-container .page').first().scrollIntoViewIfNeeded();
  await page.locator('.archive-pdf-toolbar .ant-select').last().click();
  await page.getByTitle('点击添加批注', { exact: true }).click();
  await page
    .locator('.archive-pdf-container .page')
    .first()
    .click({ position: { x: 180, y: 160 } });
  await page.locator('.ant-modal:visible textarea').fill('在线 PDF 批注验证');
  await page.locator('.ant-modal:visible').getByRole('button', { name: '保存批注' }).click();
  await page.getByRole('button', { name: '保存正式版本' }).click();
  await expect(page.getByText('正式版本已保存', { exact: true })).toBeVisible();
  await testInfo.attach('pdf-annotations', { body: await page.screenshot(), contentType: 'image/png' });
  await page.getByRole('button', { name: '退出编辑（保留草稿）' }).click();
  const pdfDocumentID = Number(new URL(page.url()).pathname.split('/').pop());
  const pdfState = await api(page, `/documents/${pdfDocumentID}`);
  expect(pdfState.version.body.annotations).toHaveLength(1);
  const [annotatedDownload] = await Promise.all([page.waitForEvent('download'), page.getByRole('button', { name: '导出 PDF' }).click()]);
  const annotated = await PDFDocument.load(readFileSync((await annotatedDownload.path())!));
  const outputPage = annotated.getPage(0);
  const entries = outputPage.node.Annots()!.asArray();
  const note = entries.map((ref) => annotated.context.lookup(ref, PDFDict)).find((d) => d.get(PDFName.of('Subtype'))?.toString() === '/Text')!;
  const coordinates = note
    .lookup(PDFName.of('Rect'), PDFArray)
    .asArray()
    .map((n) => (n as PDFNumber).asNumber());
  const stored = pdfState.version.body.annotations[0].rects[0];
  const box = outputPage.getCropBox();
  const expected = [
    box.x + stored.x * box.width,
    box.y + (1 - stored.y - stored.height) * box.height,
    box.x + (stored.x + stored.width) * box.width,
    box.y + (1 - stored.y) * box.height,
  ];
  coordinates.forEach((n, i) => expect(n).toBeCloseTo(expected[i], 3));
  await page.goto(`/archives/${archiveID}`);
  await page.locator('tr').filter({ hasText: 'report.docx' }).getByRole('button', { name: '转在线编辑' }).click();
  await expect(page.locator('.archive-canvas canvas').first()).toBeVisible();
  await page.getByRole('button', { name: '转在线编辑' }).click();
  const originalWordURL = page.url();
  await page.locator('.ant-modal:visible').getByRole('button', { name: '确认并转在线编辑' }).click();
  await page.waitForURL((url) => url.href !== originalWordURL && url.searchParams.get('edit') === 'true');
  await expect(page.locator('.archive-canvas canvas').first()).toBeVisible();
  const after = await api(page, `/${archiveID}`);
  const onlineCopy = after.documents.find((n: any) => n.source_document_id > 0);
  expect(onlineCopy).toBeTruthy();
  expect(after.documents.find((n: any) => n.id === onlineCopy.source_document_id).kind).toBe('docx');
  expect(errors).toEqual([]);
});

test('inspection and incident fields, material purposes, main document and information exports', async ({ page }, testInfo) => {
  await login(page);
  const inspectionID = await create(page, 'inspection', `巡检验收 ${Date.now()}`);
  await page.getByRole('button', { name: '编辑信息' }).click();
  let modal = page.locator('.ant-modal:visible');
  await modal.locator('.ant-select:has(#inspection_result) .ant-select-selector').click();
  await page.getByTitle('异常', { exact: true }).click();
  await modal.locator('#handler').fill('手填巡检人员');
  await modal.getByText('服务器', { exact: true }).click();
  await expect(modal.getByLabel('服务器', { exact: true })).toBeChecked();
  await modal.getByRole('button', { name: '保存信息' }).click();
  await attach(page, 'inspection.png', png, 'image/png');
  await page.getByRole('button', { name: /^归\s*档$/ }).click();
  await expect(page.locator('.archive-detail-heading')).toContainText('已归档');
  const inspect = await api(page, `/${inspectionID}`);
  expect(inspect.metadata.inspection_result).toBe('abnormal');
  expect(inspect.metadata.scope).toContain('服务器');
  expect(inspect.documents[0].purpose).toBe('inspection_report');
  const infoJob = await api(page, `/${inspectionID}/exports`, 'POST', { format: 'information_pdf' });
  await expect.poll(async () => (await api(page, `/exports/${infoJob.id}`)).state, { timeout: 30000 }).toBe('done');
  await page.getByRole('button', { name: '更多档案操作' }).click();
  const [zipDownload] = await Promise.all([page.waitForEvent('download', { timeout: 40000 }), page.getByRole('menuitem', { name: '导出整份档案' }).click()]);
  expect(
    readFileSync((await zipDownload.path())!)
      .subarray(0, 2)
      .toString(),
  ).toBe('PK');
  await page.goto('/archives?tab=incident');
  await page.getByRole('button', { name: '新建档案' }).click();
  modal = page.locator('.ant-modal:visible');
  await modal.locator('#affected_business').fill(`影响业务 ${Date.now()}`);
  await modal.locator('.ant-select:has(#level) .ant-select-selector').click();
  await page.getByTitle('Ⅲ级', { exact: true }).click();
  await modal.locator('#main_engineer').fill('手填故障工程师');
  await modal.getByRole('button', { name: '保存草稿' }).click();
  await page.waitForURL(/\/archives\/\d+$/);
  const incidentID = Number(new URL(page.url()).pathname.split('/').pop());
  await attach(page, 'incident.png', png, 'image/png');
  await page.getByRole('button', { name: /^归\s*档$/ }).click();
  await expect(page.locator('.archive-detail-heading')).toContainText('已归档');
  const incident = await api(page, `/${incidentID}`);
  expect(incident.metadata.level).toBe('III');
  expect(incident.documents[0].purpose).toBe('incident_report');
  await attach(page, 'incident-extra.png', png, 'image/png');
  await page.locator('tr').filter({ hasText: 'incident-extra.png' }).getByRole('button', { name: '设为主文档' }).click();
  await expect(page.locator('tr').filter({ hasText: 'incident-extra.png' })).toContainText('主文档');
  await page.getByRole('button', { name: '版本历史', exact: false }).first().click();
  await expect(page.locator('.ant-drawer:visible')).toContainText(/v\d/);
  await page.locator('.ant-drawer:visible .ant-drawer-close').click();
  await testInfo.attach('incident-detail', { body: await page.screenshot(), contentType: 'image/png' });
});

test('metadata edit keeps its opening revision and typed values during background uploads', async ({ page }) => {
  await login(page);
  const id = await create(page, 'other', `并发字段验收 ${Date.now()}`);
  const before = await api(page, `/${id}`);
  let continueUpload!: () => void;
  let intercepted!: () => void;
  const gate = new Promise<void>((resolve) => {
    continueUpload = resolve;
  });
  const arrived = new Promise<void>((resolve) => {
    intercepted = resolve;
  });
  await page.route(`**/api/n9e/archives/${id}/files`, async (route) => {
    intercepted();
    await gate;
    await route.continue();
  });
  await page.locator('input[type=file]').first().setInputFiles({ name: 'late.png', mimeType: 'image/png', buffer: png });
  await arrived;
  await page.getByRole('button', { name: '编辑信息' }).click();
  const modal = page.locator('.ant-modal:visible');
  await modal.locator('#summary').fill('保留我的输入');
  await api(page, `/${id}`, 'PUT', { revision: before.revision, metadata: { ...before.metadata, summary: '外部修改' }, primary_document_id: before.primary_document_id });
  continueUpload();
  await expect(page.locator('.ant-table-tbody')).toContainText('late.png');
  await expect(modal.locator('#summary')).toHaveValue('保留我的输入');
  await modal.getByRole('button', { name: '保存信息' }).click();
  await expect(modal.locator('.ant-alert')).toContainText(/changed|更新|刷新/);
  expect((await api(page, `/${id}`)).metadata.summary).toBe('外部修改');
});

test('formal save waits for private autosave to finish instead of dropping a click', async ({ page }) => {
  await login(page);
  const id = await create(page, 'other', `保存等待验收 ${Date.now()}`);
  const detail = await api(page, `/${id}/documents`, 'POST', {
    title: '保存等待文档',
    purpose: 'other',
    body: { schema_version: 1, data: { header: [], footer: [], main: [{ value: '初始正文' }] }, options: { width: 794, height: 1123, margins: [72, 72, 72, 72] } },
  });
  const doc = detail.documents[detail.documents.length - 1];
  await page.goto(`/archives/${id}/documents/${doc.id}?edit=true`);
  await expect(page.locator('.archive-canvas canvas').first()).toBeVisible();
  let resume!: () => void;
  let arrived!: () => void;
  const gate = new Promise<void>((r) => {
    resume = r;
  });
  const intercepted = new Promise<void>((r) => {
    arrived = r;
  });
  await page.route(`**/documents/${doc.id}/draft`, async (route) => {
    if (route.request().method() === 'PUT' && !route.request().postDataJSON().switch_mode) {
      arrived();
      await gate;
    }
    await route.continue();
  });
  await page.getByText('Markdown', { exact: true }).click();
  const source = page.getByRole('textbox', { name: 'Markdown 文档源码' });
  await source.fill((await source.inputValue()) + '\n自动保存中的输入');
  await Promise.race([intercepted, new Promise((_, reject) => setTimeout(() => reject(new Error('Private autosave did not start')), 25000))]);
  const save = page.getByRole('button', { name: '保存正式版本' });
  try {
    await expect(save).toBeDisabled({ timeout: 5000 });
  } finally {
    resume();
  }
  await save.click();
  await expect.poll(async () => (await api(page, `/documents/${doc.id}`)).version.sequence).toBe(2);
});
