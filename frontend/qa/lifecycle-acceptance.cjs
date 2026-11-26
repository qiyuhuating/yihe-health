const fs=require('fs'),path=require('path'),run=require('./browser-harness.cjs');
const {EventEmitter,once}=require('node:events');
run(async({page,base,report,setRole,setSignedIn})=>{
 const checks=[];const check=(name,pass,details)=>checks.push({name,pass:Boolean(pass),details});
 // Chrome may not emit Playwright requestfailed while an intercepted route
 // remains unresolved, even though Axios has aborted the underlying XHR.
 // Observe the real transport's abort invocation in the page instead.
 await page.addInitScript(() => {
  const open=XMLHttpRequest.prototype.open,send=XMLHttpRequest.prototype.send,abort=XMLHttpRequest.prototype.abort;
  XMLHttpRequest.prototype.open=function(...args) {
   this.__careUrl=String(args[1]);
   return open.apply(this,args);
  };
  XMLHttpRequest.prototype.send=function(...args) {
   this.__careBody=String(args[0]??'');
   return send.apply(this,args);
  };
  XMLHttpRequest.prototype.abort=function(...args) {
   (window.__careAbortedXHR ||= []).push({url:this.__careUrl,body:this.__careBody});
   return abort.apply(this,args);
  };
 });
 const abortSpec=request=>({url:new URL(request.url()).pathname,body:request.postData()});
 const abortedCount=async(request)=>page.evaluate(spec=>(window.__careAbortedXHR||[])
  .filter(({url,body})=>url?.endsWith(spec.url)&&body===spec.body).length,abortSpec(request));
 const expectAbort=async(request,before)=>page.waitForFunction(
  ({spec,before})=>(window.__careAbortedXHR||[])
   .filter(({url,body})=>url?.endsWith(spec.url)&&body===spec.body).length>before,
  {spec:abortSpec(request),before},{timeout:5000});

 const withTimeout=(promise,ms,message)=>Promise.race([promise,new Promise((_,reject)=>setTimeout(()=>reject(new Error(message)),ms))]);
 const visit=async(route,role)=>{setRole(role);setSignedIn(true);await page.goto(base+'/?lifecycle='+Date.now()+'#'+route);await page.locator('.care-app').waitFor();};
 // Exercise real Axios/XHR cancellation against held mock responses.
 const customerRequests=[],customerRoutes=new EventEmitter();
 const heldSearches=new Set(['F02旧查询','F02超时','F02离页']);
 await page.route('**/api/v1/guardians/list',route=>{
  const request=route.request();
  const {customerName}=request.postDataJSON();
  customerRequests.push(request);
  customerRoutes.emit(customerName,request);
  if(heldSearches.has(customerName))return;
  if(customerName==='F02当前错误')return route.fulfill({status:503,json:{returnCode:'0000',code:'SERVICE_UNAVAILABLE',message:'请求未完成'}});
  const name=customerName==='F02新查询'?'F02新查询结果':'F02当前监护人';
  return route.fulfill({json:{returnCode:'1111',customers:[{customerId:1,customerName:name,customerPhone:'19912345678',customerAge:70}],total:1}});
 });
 await visit('/operations/guardians','ADMIN');
 await page.getByText('F02当前监护人',{exact:true}).first().waitFor();
 const searchCustomer=async(name)=>{
  await page.getByLabel('监护人姓名',{exact:true}).fill(name);
  const [[request]]=await Promise.all([
   once(customerRoutes,name,{signal:AbortSignal.timeout(8000)}),
   page.getByRole('button',{name:'查询监护人姓名',exact:true}).click()
  ]);
  return request;
 };
 const superseded=await searchCustomer('F02旧查询');
 const canceled=await abortedCount(superseded);
 await searchCustomer('F02新查询');await expectAbort(superseded,canceled);
 await page.locator('.roles[aria-busy="false"]').waitFor();
 check('New guardian query aborts a hung XHR and displays its own result',await page.getByText('F02新查询结果',{exact:true}).count()>0);
 check('Read cancellation preserves the CSRF header',customerRequests.every(r=>r.headers()['x-csrf-token']==='mock-csrf-token'));
 const timedOut=await searchCustomer('F02超时');
 const timeoutCanceled=await abortedCount(timedOut);
 await page.getByRole('alert').filter({hasText:'监护人信息暂时无法加载'}).waitFor({timeout:15000});await expectAbort(timedOut,timeoutCanceled);
 check('Guardian timeout aborts XHR and ends loading',await page.locator('.roles').getAttribute('aria-busy')==='false');
 await searchCustomer('F02恢复');
 await page.getByText('F02当前监护人',{exact:true}).first().waitFor();
 check('A fresh query recovers after timeout',await page.getByRole('alert').filter({hasText:'监护人信息暂时无法加载'}).count()===0);
 await searchCustomer('F02当前错误');
 await page.getByRole('alert').filter({hasText:'请求未完成'}).waitFor();
 check('A current HTTP failure ends loading and remains visible',await page.locator('.roles').getAttribute('aria-busy')==='false');
 const leaving=await searchCustomer('F02离页');
 const unmounted=await abortedCount(leaving);
 await page.evaluate(()=>{location.hash='/operations/overview'});await expectAbort(leaving,unmounted);
 await page.getByRole('heading',{name:'运营总览',exact:true}).waitFor();
 const countAfterLeaving=customerRequests.length;await page.waitForTimeout(100);
 check('Navigation aborts the guardian read without starting a replacement',customerRequests.length===countAfterLeaving);
 await page.unroute('**/api/v1/guardians/list');

 await page.route('**/api/v1/care/incidents',route=>route.fulfill({json:{returnCode:'1111',items:[{id:1,oldPersonName:'F02照护任务',state:'received'}],total:1,duty:'supervisor',actorId:1}}));
 const historyRequests=[];
 await page.route('**/api/v1/care/events',route=>{historyRequests.push(route.request())});
 await visit('/operations/overview','ADMIN');await page.locator('.care-task').first().waitFor();
 const openHistory=async()=>{
  const requested=page.waitForRequest(r=>r.url().endsWith('/api/v1/care/events'));
  await page.getByRole('button',{name:'处置记录',exact:true}).first().click();return requested;
 };
 const historyRequest=await openHistory();
 const historyCanceled=await abortedCount(historyRequest);
 await page.getByRole('dialog',{name:'照护事件履历'}).locator('.el-dialog__headerbtn').click();await expectAbort(historyRequest,historyCanceled);
 check('Closing history aborts its hung XHR',historyRequests.length===1);
 const historyAgain=await openHistory();
 const historyUnmounted=await abortedCount(historyAgain);
 await page.evaluate(()=>{location.hash='/operations/guardians'});await expectAbort(historyAgain,historyUnmounted);
 await page.locator('.roles').waitFor();
 check('History can reopen and is aborted when leaving the page',historyRequests.length===2);
 await page.unroute('**/api/v1/care/events');await page.unroute('**/api/v1/care/incidents');

 await visit('/operations/devices','ADMIN');
 await page.getByRole('heading',{name:'设备管理',exact:true}).waitFor();
 await page.waitForTimeout(250);
 let release,started,requests=0;
 const gate=new Promise(resolve=>{release=resolve}),pending=new Promise(resolve=>{started=resolve});
 await page.route('**/api/v1/device/list',async route=>{requests++;started();await gate;await route.fulfill({contentType:'application/json',body:JSON.stringify({returnCode:'1111',devices:[]})});});
 await Promise.race([pending,new Promise((_,reject)=>setTimeout(()=>reject(new Error('Refresh did not run')),12000))]);
 await page.locator('a[href="#/operations/overview"]').first().click();
 await page.getByRole('heading',{name:'运营总览',exact:true}).waitFor();
 release();await page.waitForTimeout(5500);
 check('Pending device refresh cannot restart polling after leaving the page',requests===1,{requests});
 await page.unroute('**/api/v1/device/list');
 for(const [route,role,endpoint,button,home,body]of[
  ['/operations/vitals','ADMIN','vitals/chart','查看健康详情','/operations/overview',{hrs:[72],bps:[120],createTimes:['08:00'],time:'2026-10-16'}],
  ['/operations/activity','ADMIN','activity/chart','查看睡眠详情','/operations/overview',{sleep:[7],createTime:['10-16']}],
  ['/family-portal/vitals','FAMILY','vitals/chart',/详情|详细|趋势/,'/family-portal/overview',{hrs:[72],bps:[120],createTimes:['08:00'],time:'2026-10-16'}],
  ['/family-portal/activity','FAMILY','activity/chart',/详情|详细|趋势/,'/family-portal/overview',{sleep:[7],createTime:['10-16']}]
 ]){
  await visit(route,role);await page.waitForTimeout(200);
  let resume,arrived;const hold=new Promise(r=>{resume=r}),request=new Promise(r=>{arrived=r});
  await page.route('**/api/v1/'+endpoint,async r=>{arrived();await hold;await r.fulfill({contentType:'application/json',body:JSON.stringify({returnCode:'1111',...body})});});
  await page.getByRole('button',{name:button}).first().click();await withTimeout(request,8000,route+': detail request was not issued');
  await page.evaluate(hash=>{location.hash=hash},home);
  await page.waitForURL(url=>url.hash==='#'+home);await page.locator('.el-dialog:visible').waitFor({state:'hidden'});
  resume();await page.waitForTimeout(350);
  check(route+': delayed chart result does not recreate a chart after navigation',await page.locator('[id^="myChart"] canvas').count()===0);
  await page.unroute('**/api/v1/'+endpoint);
 }
 check('No lifecycle JavaScript errors',report.pageErrors.length===0,report.pageErrors);
 check('No missing lifecycle assets',report.missingAssets.length===0,report.missingAssets);
 fs.writeFileSync(path.join(__dirname,'artifacts/lifecycle-acceptance.json'),JSON.stringify({checks},null,2));
 console.log(JSON.stringify({checks:checks.length,failed:checks.filter(c=>!c.pass)},null,2));if(checks.some(c=>!c.pass))process.exitCode=1;
}).catch(e=>{console.error(e);process.exitCode=1});
