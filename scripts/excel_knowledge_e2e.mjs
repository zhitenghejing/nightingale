// Explicit disposable preview only. All credentials, fixtures and model replies
// remain in the private output directory; no tokens/reasoning are logged.
import {createRequire} from 'node:module';
import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {randomUUID,createHash} from 'node:crypto';
import path from 'node:path';
const require=createRequire(import.meta.url);
const {chromium,request,expect}=require('../frontend/node_modules/@playwright/test');
const ExcelJS=require('../frontend/node_modules/exceljs');
const base=process.env.N9E_E2E_BASE_URL,build=process.env.N9E_E2E_BUILD_ID,out=process.env.N9E_EXCEL_OUTPUT,creds=process.env.N9E_E2E_CREDENTIALS_FILE;
if(!base||!build||!out||!creds)throw Error('Set disposable preview URL/build ID/private credentials and output');
await mkdir(out,{recursive:true,mode:0o700});
const http=await request.newContext({baseURL:base,timeout:45000});
const info=await(await http.get('/__preview/info')).json();expect(info.ready).toBe(true);expect(info.role).toBe('development');expect(info.buildId).toBe(build);
const browser=await chromium.launch({headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
const context=await browser.newContext({viewport:{width:1440,height:1000},locale:'zh-CN'}),page=await context.newPage();page.setDefaultTimeout(30000);
const checks=[];const record=(name,detail={})=>{checks.push({name,state:'passed',...detail});console.log('PASS '+name);};
try{
 const credential=JSON.parse(await readFile(creds,'utf8'));
 await page.goto(base+'/login');await page.locator('#username').fill(credential.username);await page.locator('#password').fill(credential.password);await page.locator('button.ant-btn-primary').click();await page.waitForURL(u=>u.pathname!='/login');
 const token=await page.evaluate(()=>localStorage.getItem('access_token'));const headers={Authorization:`Bearer ${token}`,'X-Language':'zh_CN'};
 const api=async(route,method='GET',data)=>{const res=await http.fetch('/api/n9e'+route,{method,data,headers});const b=await res.json();if(!res.ok()||b.err!=='')throw Error(`${method} ${route}: ${res.status()} ${b.err}`);return b.dat;};
 const suffix=randomUUID().slice(0,8),title='Excel模拟巡检-'+suffix;
 const wb=new ExcelJS.Workbook();wb.creator='隔离验证';const s=wb.addWorksheet('接口巡检');s.addRow(['检查编号','接口名称','告警次数','状态','采样比例']);
 for(let i=1;i<=120;i++){s.addRow([String(i).padStart(4,'0'),`模拟接口${i}`,i,i%2?'正常':'待复核',0.125]);}
 s.getColumn(5).numFmt='0.00%';s.getRow(3).hidden=true;s.getCell('F1').value='说明';s.mergeCells('F2:G3');s.getCell('F2').value='合并格只保留左上值';s.getCell('H2').value={formula:'SUM(C2:C121)',result:7260};s.getCell('H3').value={formula:'SUM(1,2)'};
 const notes=wb.addWorksheet('实施说明');notes.addRow(['项目','结论']);notes.addRow(['知识标记','琥珀巡检验证'+suffix]);notes.addRow(['处理建议','先核对日志，再由负责人判断操作适用性。此为模拟资料，不代表真实医院环境。']);
 const hidden=wb.addWorksheet('隐藏备注',{state:'hidden'});hidden.addRow(['隐藏标记','白鹭内部模拟说明']);
 const buffer=Buffer.from(await wb.xlsx.writeBuffer());await writeFile(path.join(out,'sample.xlsx'),buffer);
 const a=await api('/archives','POST',{category:'other',metadata:{title,custom_type:'隔离测试资料'}});
 await page.goto(base+`/archives/${a.id}`);
 const inputs=page.locator('input[type=file]');await expect(inputs.first()).toHaveAttribute('accept',/\.xlsx/);
 await inputs.first().setInputFiles({name:title+'.xlsx',mimeType:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',buffer});
 let detail;await expect.poll(async()=>{detail=await api(`/archives/${a.id}`);return detail.documents.length;},{timeout:60000}).toBe(1);
 const d=detail.documents[0];expect(d.kind).toBe('xlsx');record('Existing archive upload accepts XLSX and creates immutable formal version',{archive_id:a.id,document_id:d.id,version_id:d.current_version_id});
 const original=await http.get(`/api/n9e/archives/files/${d.version.blob_id}?download=true`,{headers});expect(original.status()).toBe(200);const saved=await original.body();expect(createHash('sha256').update(saved).digest('hex')).toBe(createHash('sha256').update(buffer).digest('hex'));record('Downloaded original SHA-256 matches upload');
 expect((await http.get(`/api/n9e/archives/documents/${d.id}/workbook`)).status()).toBe(401);
 const catalog=await api(`/archives/documents/${d.id}/workbook?version_id=${d.current_version_id}`);expect(catalog.sheets.length).toBe(3);expect(catalog.sheets.find(x=>x.name==='隐藏备注').hidden).toBe(true);record('Authenticated exact-version worksheet catalog includes hidden sheet');
 await page.getByRole('button',{name:/阅\s*读/}).first().click();await page.waitForURL(u=>u.pathname.includes(`/documents/${d.id}`));await expect(page.getByText('0001',{exact:true})).toBeVisible();await expect(page.getByText('12.50%',{exact:true}).first()).toBeVisible();
 await page.screenshot({path:path.join(out,'excel-reader.png'),fullPage:true});
 await page.getByLabel('单元格范围').fill('A118:E121');await page.getByRole('button',{name:'定位范围'}).click();await expect(page.getByText('0120',{exact:true})).toBeVisible();record('Readonly workbook UI preserves identifiers, percentages and last rows');
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:path.join(out,'excel-mobile.png'),fullPage:true});await page.setViewportSize({width:1440,height:1000});
 let k;await expect.poll(async()=>{const versions=await api('/knowledge-documents');k=versions.find(v=>v.document_id===d.id);return k?.state;},{timeout:180000,intervals:[1000,3000,5000]}).toBe('ready');
 const hits=await api('/knowledge-documents/search?q='+encodeURIComponent('琥珀巡检验证'+suffix));expect(hits.some(h=>h.version_id===k.id&&h.heading.includes('实施说明'))).toBe(true);record('Automatic knowledge ingestion indexes the second sheet with exact version/locator',{library_version_id:k.id});
 const chat=await api('/assistant/chat/new','POST',{page:'chat',param:{}});
 const question=`请查阅《${title}.xlsx》，计算“接口巡检”工作表中 C2:C121 的告警次数总和，并统计 D2:D121 中“待复核”的行数（应覆盖所有行，包括隐藏行），另查“实施说明”的知识标记。请实际使用表格统计工具，引用工作表和范围，不要从几条检索片段估计。`;
 const sent=await api('/assistant/message/new','POST',{chat_id:chat.chat_id,query:{mode:'knowledge_qa',content:question,action:{},page_from:{url:'/nightingale-ai'}}});
 let message;await expect.poll(async()=>{message=await api('/assistant/message/detail','POST',{chat_id:chat.chat_id,seq_id:sent.seq_id});return message?.is_finish;},{timeout:420000,intervals:[2000,5000]}).toBe(true);
 const answer=(message.response||[]).filter(r=>r.content_type==='markdown').map(r=>r.content).join('\n');await writeFile(path.join(out,'model-answer.txt'),answer);
 if(message.err_code)throw Error('Model Q&A returned '+message.err_code);
 expect(answer.replaceAll(',','')).toContain('7260');expect(answer).toContain('60');expect(answer).toContain('琥珀巡检验证'+suffix);expect(answer).toContain('依据来源');expect(answer).toContain('C2:C121');
 record('Configured real model answers full-range sum/filter count and second-sheet fact',{chat_id:chat.chat_id});
 await page.goto(base+`/nightingale-ai/chat/${chat.chat_id}?ai_chat_readonly=1`);await expect(page.getByText(/7260|7,260/).first()).toBeVisible({timeout:90000});await page.screenshot({path:path.join(out,'excel-answer.png'),fullPage:true});
 const follow=await api('/assistant/message/new','POST',{chat_id:chat.chat_id,query:{mode:'knowledge_qa',content:'那检查编号 0120 对应的接口名称是什么，告警次数是多少？',action:{},page_from:{url:'/nightingale-ai'}}});
 let followMessage;await expect.poll(async()=>{followMessage=await api('/assistant/message/detail','POST',{chat_id:chat.chat_id,seq_id:follow.seq_id});if(followMessage.err_code)throw Error('Follow-up failed');return followMessage.is_finish;},{timeout:300000,intervals:[2000,5000]}).toBe(true);
 const followAnswer=(followMessage.response||[]).filter(r=>r.content_type==='markdown').map(r=>r.content).join('\n');await writeFile(path.join(out,'followup-answer.txt'),followAnswer);expect(followAnswer).toContain('模拟接口120');expect(followAnswer).toContain('120');record('Natural-language follow-up finds the last record by its zero-padded identifier',{seq_id:follow.seq_id});
 // Explicit legacy rejection is checked through the upload API without creating a document.
 const legacy=await http.post(`/api/n9e/archives/${a.id}/files`,{headers,multipart:{purpose:'other',file:{name:'legacy.xls',mimeType:'application/vnd.ms-excel',buffer:Buffer.from('legacy-not-supported')}}});expect(legacy.status()).toBe(422);expect((await legacy.json()).err).toContain('xlsx');record('Legacy XLS rejection gives conversion guidance');
 const replacementArchive=await api('/archives','POST',{category:'other',metadata:{title:title+'-换版校验',custom_type:'隔离测试资料'}});
 const upload=async(documentID=0,lease)=>{const fields={purpose:'other',document_id:String(documentID),file:{name:title+'.xlsx',mimeType:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',buffer}};if(lease){fields.session_id=lease.session_id;fields.epoch=String(lease.epoch);}return http.post(`/api/n9e/archives/${replacementArchive.id}/files`,{headers,multipart:fields});};
 const initialUpload=await upload();expect(initialUpload.status()).toBe(200);const replacementDoc=(await initialUpload.json()).dat.documents[0];let oldIndex;
 await expect.poll(async()=>{oldIndex=(await api('/knowledge-documents')).find(v=>v.document_id===replacementDoc.id);return oldIndex?.state;},{timeout:180000,intervals:[1000,3000]}).toBe('ready');
 expect((await upload(replacementDoc.id)).status()).toBe(409);
 const editing=await api(`/archives/documents/${replacementDoc.id}/lease`,'POST',{});expect((await upload(replacementDoc.id,editing.lease)).status()).toBe(200);
 const stale=await http.get(`/api/n9e/knowledge-documents/${oldIndex.id}`,{headers});expect(stale.status()).toBe(404);
 const historical=await api(`/archives/documents/${replacementDoc.id}/workbook?version_id=${replacementDoc.current_version_id}`);expect(historical.sheets.length).toBe(3);
 await api(`/archives/documents/${replacementDoc.id}/lease`,'DELETE',{session_id:editing.lease.session_id,epoch:editing.lease.epoch});record('Replacement requires lease, invalidates previous knowledge immediately, retains historical workbook',{replacement_archive_id:replacementArchive.id});
 await writeFile(path.join(out,'report.json'),JSON.stringify({build_id:build,base,checks,archive_id:a.id,document_id:d.id,library_version_id:k.id,chat_id:chat.chat_id},null,2));
}catch(error){await page.screenshot({path:path.join(out,'failure.png'),fullPage:true}).catch(()=>{});await writeFile(path.join(out,'failure.json'),JSON.stringify({error:String(error),checks},null,2));throw error;}finally{await browser.close();await http.dispose();}
