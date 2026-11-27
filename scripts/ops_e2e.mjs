// Run only against an explicitly verified disposable preview. Credentials and
// screenshots stay outside the checkout; never record login traces or tokens.
import { createRequire } from 'node:module';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
const require = createRequire(import.meta.url);
const { chromium, request, expect } = require('../frontend/node_modules/@playwright/test');
const base = process.env.N9E_E2E_BASE_URL;
const build = process.env.N9E_E2E_BUILD_ID;
const credentialsFile = process.env.N9E_E2E_CREDENTIALS_FILE;
const output = process.env.N9E_OPS_E2E_OUTPUT;
if (!base || !build || !credentialsFile || !output) throw Error('Set preview URL, build ID, private credentials file and output directory');
await mkdir(output, {recursive:true,mode:0o700});
const http = await request.newContext({baseURL:base,timeout:30000});
const info = await (await http.get('/__preview/info')).json();
expect(info.role).toBe('development'); expect(info.ready).toBe(true); expect(info.buildId).toBe(build);
expect((await http.get('/api/n9e/ops/config')).status()).toBe(401);
const credentials = JSON.parse(await readFile(credentialsFile,'utf8'));
const browser = await chromium.launch({headless:true,executablePath:process.env.N9E_E2E_CHROME || undefined,args:['--no-sandbox','--disable-dev-shm-usage']});
const context = await browser.newContext({viewport:{width:1440,height:1000},locale:'zh-CN'});
const page = await context.newPage(); page.setDefaultTimeout(20000);
const errors=[]; page.on('pageerror',e=>errors.push(e.message));
const checks=[]; const record=(name,detail={})=>{checks.push({name,state:'passed',...detail});console.log('PASS '+name);};
try {
  await page.goto(base+'/login');
  await page.locator('#username').fill(credentials.username);await page.locator('#password').fill('ops-deliberately-invalid-password');
  let response=page.waitForResponse(r=>r.url().includes('/auth/login') && r.request().method()==='POST');await page.locator('button.ant-btn-primary').click();
  expect(Boolean((await (await response).json()).err)).toBe(true);record('UI rejects incorrect password');
  await page.locator('#password').fill(credentials.password);response=page.waitForResponse(r=>r.url().includes('/auth/login') && r.request().method()==='POST');await page.locator('button.ant-btn-primary').click();expect((await response).status()).toBe(200);
  await page.waitForURL(u=>u.pathname!='/login');
  const token=await page.evaluate(()=>localStorage.getItem('access_token'));const headers={Authorization:`Bearer ${token}`,'X-Language':'zh_CN'};
  const api=async (route,method='GET',data)=>{const r=await http.fetch('/api/n9e'+route,{method,data,headers});const body=await r.json();const error = Object.hasOwn(body,'err') ? body.err : body.error; if(!r.ok() || error!=='')throw Error(`${method} ${route}: HTTP ${r.status()} ${JSON.stringify(error)}`);return Object.hasOwn(body,'dat') ? body.dat : body.data;};
  const ops=async (route,method='GET',data)=>api('/ops'+route,method,data);
  let cfg=await ops('/config');expect(cfg.demo_enabled).toBe(true);expect(cfg.model_enabled).toBe(true);expect(cfg.execution_enabled).toBe(false);
  const group=cfg.groups.find(g=>g.writable)?.id;expect(group).toBeGreaterThan(0);
  const engine='preview-'+info.storage.databaseName.replace('n9ep_','');
  const sources=await api('/datasource/list','POST',{});let ds=sources.find(d=>d.name==='Isolated Ops Demo');
  if(!ds){const result=await api('/datasource/upsert','POST',{name:'Isolated Ops Demo',identifier:'ops-demo-private',plugin_id:1,plugin_type:'prometheus',plugin_type_name:'Prometheus Like',category:'timeseries',cluster_name:engine,http:{url:'http://127.0.0.1:39709',timeout:5000,dial_timeout:3000},settings:{'prometheus.cluster_name':engine},auth:{},force_save:false});ds={id:result.id};}
  const models=await api('/ai-llm-configs');if(!models.some(m=>m.name==='Ops scripted fixture (not a real model)'))await api('/ai-llm-configs','POST',{name:'Ops scripted fixture (not a real model)',description:'Local deterministic HTTP fixture; no real-model evaluation',api_type:'openai',api_url:'http://127.0.0.1:39708/v1/chat/completions',api_key:'fixture-not-a-secret',model:'ops-scripted-fixture',enabled:true,is_default:true,extra_config:{timeout_seconds:20}});
  const setup=await ops('/demo/setup','POST',{group_id:group,datasource_id:ds.id});expect(setup.test_only).toBe(true);record('Isolated target, native alert rule and immutable test knowledge prepared',{rule_id:setup.rule_id});
  // Reset only this fixed test scenario through the authenticated operator
  // channel; never silently approve a pending production action.
  await ops('/demo/fault','POST',{group_id:group,kind:'clear'});
  const previousCases=(await ops('/cases')).filter(c=>c.rule_id===setup.rule_id && c.state!=='closed');
  for(const previous of previousCases){
    await ops(`/cases/${previous.id}/runs`,'DELETE');
    let d=await ops(`/cases/${previous.id}`);
    if(d.episodes.some(e=>!e.recovered)){
      if(!d.case.knowledge_version_id) await ops(`/cases/${previous.id}/knowledge`,'POST',{revision:d.case.revision,version_id:setup.knowledge_version_id});
      const proposal=await ops(`/cases/${previous.id}/actions`,'POST',{key:'reclaim'});
      await ops(`/cases/${previous.id}/actions/${proposal.id}/decision`,'POST',{accept:true});
    }
    d=await ops(`/cases/${previous.id}`);await ops(`/cases/${previous.id}/close`,'POST',{revision:d.case.revision,kind:'cancelled',summary:'Previous isolated fixture run ended; no new production recovery is claimed.'});
  }
  if(previousCases.length) await expect.poll(async()=>{for(const c of previousCases){if((await ops(`/cases/${c.id}`)).episodes.some(e=>!e.recovered))return false;}return true;},{timeout:90000,intervals:[2000,5000]}).toBe(true);
  let policy=(await ops('/policies')).find(p=>p.group_id===group);
  await ops('/policies','PUT',{...policy,enabled:true,auto_investigate:true,service_user_id:cfg.user_id,rule_ids:[setup.rule_id]});
  // Exercise source privacy, round-trip download, and immutable knowledge UI.
  const sourceText='# Demo operational source\nUse the isolated quota metric. Require business and data reconciliation.\n';
  const upload=await http.post('/api/n9e/ops/sources',{headers,multipart:{group_id:String(group),file:{name:'ops-e2e-source.md',mimeType:'text/plain',buffer:Buffer.from(sourceText)}}});expect(upload.ok()).toBe(true);const source=(await upload.json()).dat;
  const downloaded=await http.get(`/api/n9e/ops/sources/${source.id}/file`,{headers});expect(downloaded.status()).toBe(200);expect(await downloaded.text()).toBe(sourceText);expect((await http.get(`/api/n9e/ops/sources/${source.id}/file`)).status()).toBe(401);
  const generated=await ops('/knowledge-draft','POST',{group_id:group,source_ids:[source.id]});expect(generated.current_version_id).toBe(0);expect(generated.draft.validated_at).toBe(0);expect(generated.draft.actions).toEqual([]);
  const premature=await http.post(`/api/n9e/ops/knowledge/${generated.id}/publish`,{headers,data:{revision:generated.revision}});expect(premature.status()).toBe(422);record('Source-to-draft HTTP path stays unverified and cannot publish automatically');
  await page.goto(base+'/ops/knowledge');await expect(page.getByRole('button',{name:'编写知识',exact:true})).toBeVisible();await page.locator('button:not([disabled])',{hasText:'查看正式版本'}).first().click();await expect(page.getByRole('dialog')).toContainText('正式版本历史'); await page.getByRole('dialog').getByRole('button',{name:/阅\s*读/}).first().click(); await expect(page.getByRole('dialog').last()).toContainText('排查步骤'); await page.keyboard.press('Escape');await page.getByRole('dialog').getByRole('button',{name:/关|Close/}).first().click().catch(async()=>{await page.keyboard.press('Escape');});
  await page.screenshot({path:path.join(output,'knowledge.png')});record('Private source round-trip, anonymous rejection and knowledge version UI');
  const before=await ops('/cases');const existing=new Set(before.map(c=>c.id));
  await ops('/demo/fault','POST',{group_id:group,kind:'clear'});
  await ops('/demo/fault','POST',{group_id:group,kind:'storage'});
  let caseID;
  await expect.poll(async()=>{const c=(await ops('/cases')).find(c=>c.rule_id===setup.rule_id && !existing.has(c.id));caseID=c?.id;return Boolean(caseID);},{timeout:180000,intervals:[2000,5000]}).toBe(true);
  let detail;
  await expect.poll(async()=>{detail=await ops(`/cases/${caseID}`);return detail.runs.some(r=>r.state==='awaiting_operator') && detail.actions.some(a=>a.state==='proposed');},{timeout:60000,intervals:[1000,3000]}).toBe(true);
  expect(detail.case.knowledge_version_id).toBe(setup.knowledge_version_id);expect(detail.evidence.some(e=>e.kind==='metric' && e.status==='ok')).toBe(true);expect(detail.audit.some(a=>a.action==='tool_started')).toBe(true);expect(detail.actions.every(a=>a.state==='proposed')).toBe(true);
  const action=detail.actions.find(a=>a.state==='proposed');record('Native firing, scanner, durable Agent HTTP tool loop and proposal before approval',{case_id:caseID,evidence:detail.evidence.length});
  await page.goto(base+`/ops/cases/${caseID}`);await expect(page.locator('.ops-page')).toContainText('恢复验证');
  await page.reload();await expect(page.locator('.ops-page')).toContainText('Quota watermark remains normal');await page.screenshot({path:path.join(output,'case-proposed.png')});
  // Keep business failure active while quota recovers.
  await ops('/demo/fault','POST',{group_id:group,kind:'business'});
  await page.getByRole('button',{name:'确认操作',exact:true}).first().click();await expect(page.getByRole('dialog')).toContainText('确认执行此操作');await page.getByRole('dialog').getByRole('button',{name:/确.*定|OK/}).click();
  await expect.poll(async()=>{detail=await ops(`/cases/${caseID}`);return detail.actions.find(a=>a.id===action.id)?.state;}).toBe('succeeded');
  const replay=await http.post(`/api/n9e/ops/cases/${caseID}/actions/${action.id}/decision`,{headers,data:{accept:true}});expect(replay.status()).toBe(409);expect(detail.actions.find(a=>a.id===action.id).result.execution_count).toBe(1);record('Human approval executes once; repeat decision is rejected');
  await expect.poll(async()=>{detail=await ops(`/cases/${caseID}`);return detail.episodes.every(e=>e.recovered);},{timeout:120000,intervals:[2000,5000]}).toBe(true);
  await expect.poll(async()=>{const checks=await ops(`/cases/${caseID}/verify`,'POST');return checks.find(c=>c.check_key==='metric')?.state;},{timeout:60000,intervals:[2000,5000]}).toBe('passed');
  detail=await ops(`/cases/${caseID}`);expect(detail.verifications.find(c=>c.check_key==='business').state).toBe('failed');
  const denied=await http.post(`/api/n9e/ops/cases/${caseID}/close`,{headers,data:{revision:detail.case.revision,kind:'resolved',summary:'Must reject business failure'}});expect(denied.status()).toBe(409);record('Native alert recovers and metric passes while business failure blocks closure');
  await page.getByRole('button',{name:/刷\s*新/}).click();await expect(page.locator('.ops-page')).toContainText('Business probe or data reconciliation failed');await page.screenshot({path:path.join(output,'business-blocked.png')});
  await ops('/demo/fault','POST',{group_id:group,kind:'clear'});const verification=await ops(`/cases/${caseID}/verify`,'POST');expect(verification.every(c=>c.state==='passed')).toBe(true);
  await page.getByRole('button',{name:/刷\s*新/}).click();await page.getByRole('button',{name:'填写结论并结束',exact:true}).click();await page.getByRole('dialog').getByLabel('处置结论与恢复依据').fill('Isolated quota recovered; fresh metric window, business probe and independent test ledger all passed.');await page.getByRole('dialog').getByRole('button',{name:/确.*定|OK/}).click();
  await expect.poll(async()=>(await ops(`/cases/${caseID}`)).case.state).toBe('closed');
  const feedback=await ops(`/cases/${caseID}/feedback`,'POST',{note:'Controlled cache reclaim restored quota. Keep business and ledger checks mandatory; review before publishing.'});expect(feedback.current_version_id).toBe(0);record('All checks pass, human UI closes case, feedback stays a draft',{draft_id:feedback.id});
  // A subsequent real firing must create another episode/case; do not let an
  // auto-approved recovery hide repeated faults.
  await ops('/demo/fault','POST',{group_id:group,kind:'storage'});let nextID;
  await expect.poll(async()=>{const c=(await ops('/cases')).find(c=>c.rule_id===setup.rule_id && c.id!==caseID && !existing.has(c.id));nextID=c?.id;return Boolean(nextID);},{timeout:120000,intervals:[2000,5000]}).toBe(true);
  expect(nextID).not.toBe(caseID);record('Next native firing creates a separate case',{case_id:nextID});
  // Preserve this second pending case for human acceptance; its proposed action
  // remains unapproved. Pause automatic enqueueing before handing it over.
  policy=(await ops('/policies')).find(p=>p.group_id===group);await ops('/policies','PUT',{...policy,auto_investigate:false});
  await page.goto(base+`/ops/cases/${nextID}`);await expect(page.locator('.ops-page')).toContainText('恢复验证');await page.screenshot({path:path.join(output,'acceptance-case.png')});
  await page.setViewportSize({width:390,height:844});await page.reload();await expect(page.locator('.ops-page')).toBeVisible();const overflow=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth}));expect(overflow.scroll).toBeLessThanOrEqual(overflow.width+1); const clipped=await page.locator('.ops-toolbar').evaluate(node=>[...node.children].filter(child=>child.getBoundingClientRect().right>innerWidth+1).map(child=>child.textContent));expect(clipped).toEqual([]);await page.screenshot({path:path.join(output,'mobile.png')});record('Detail refresh and mobile layout',{...overflow});
  expect(errors).toEqual([]);record('No browser runtime errors');
  await writeFile(path.join(output,'report.json'),JSON.stringify({buildId:build,source:info.source,frontend:info.frontend,baseURL:base,transport:'local production UI',checks,caseID,acceptanceCaseID:nextID,realModel:'not tested; scripted HTTP fixture only',ibex:'not tested; fixed demo adapter only',humanAcceptance:'pending'},null,2));
} catch(error){await page.screenshot({path:path.join(output,'failure.png')}).catch(()=>{});await writeFile(path.join(output,'failure.json'),JSON.stringify({buildId:build,checks,error:String(error),browserErrors:errors},null,2));throw error;
} finally {await context.close();await browser.close();await http.dispose();}
