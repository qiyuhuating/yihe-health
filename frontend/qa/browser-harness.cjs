const fs = require('fs');
const http = require('http');
const path = require('path');
let playwright;
playwright = require('playwright');
const dist = process.env.CARE_DIST ? path.resolve(process.env.CARE_DIST) : path.resolve(__dirname, '../../frontend/dist');
const output = path.join(__dirname,'artifacts');
fs.mkdirSync(path.join(output,'screenshots'),{recursive:true});
const mime = {'.html':'text/html','.js':'application/javascript','.css':'text/css','.webp':'image/webp','.png':'image/png','.jpg':'image/jpeg','.svg':'image/svg+xml','.ttf':'font/ttf','.woff':'font/woff','.woff2':'font/woff2'};
const report = {environment:'Chromium / '+process.platform+', production bundle, all API traffic mocked',layouts:[],checks:[],pageErrors:[],missingAssets:[],unexpectedApi:[],contrast:[]};
const check = (name, condition, details) => { report.checks.push({name,pass:Boolean(condition),details}); };
const server = http.createServer((req,res)=>{
 const pathname = decodeURIComponent(new URL(req.url,'http://localhost').pathname);
 const file = path.resolve(dist, '.' + (pathname==='/' ? '/index.html' : pathname));
 if (!file.startsWith(dist+path.sep)) {res.writeHead(403);res.end();return;}
 fs.readFile(file,(error,data)=>{if(error){res.writeHead(404);res.end();return;}res.setHeader('Content-Security-Policy',"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https://tile.openstreetmap.org; font-src 'self'; object-src 'none'");res.setHeader('Referrer-Policy','strict-origin-when-cross-origin');res.setHeader('Content-Type',mime[path.extname(file)]||'application/octet-stream');if(/\.(js|css|html)$/.test(file)){res.setHeader('Content-Encoding','gzip');data=require('zlib').gzipSync(data);}res.end(data);});
});
module.exports = async function (run) {
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const base = 'http://127.0.0.1:'+server.address().port;
 const browser = await playwright.chromium.launch({headless:true,...(process.env.CARE_CHROME_PATH ? {executablePath:process.env.CARE_CHROME_PATH} : {})});
 let role='FAMILY', signedIn=true;
 let writes=[], generated=false, passwordRequests=[], mode='normal', loginMode='failure', counter=0, comments=[];
 const context=await browser.newContext({viewport:{width:1440,height:1000},deviceScaleFactor:Number(process.env.CARE_DPR||1)});
 await context.addInitScript(()=>{localStorage.setItem('token','review-token');localStorage.setItem('user_name','小明');localStorage.setItem('oldIdfamily','1');localStorage.setItem('familyPhone','13000000000');const large=new URLSearchParams(location.search).get('large');if(large!==null)localStorage.setItem('large_text',large==='1'?'true':'false');});
 await context.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url());const endpoint=url.pathname.replace(/^\//,'');
  if(endpoint==='api/v1/family/updatePassword')passwordRequests.push(route.request().postDataJSON());
  if(/\/(create|add|update|delete|reply|replace|toggle-like)$/.test(endpoint))writes.push({endpoint,data:route.request().postDataJSON()});
  const unknown=mode==='unknown';
  const fixtures={
   'api/v1/service/status':{databaseReady:true},
   'api/v1/health/latest':{returnCode:'1111',oldId:1,items:[]},
   'api/v1/care/incidents':{items:[],total:0,page:1,pageSize:20,duty:'supervisor',actorId:1},
   'api/v1/care/diagnostics':{workerHealthy:true,notificationConfigured:false,receiptConfigured:false,pendingNotifications:0,failedNotifications:0,staleSending:0,overdueIncidents:0},
   'api/v1/care/staff':{staff:[]},
   'api/v1/operations/snapshot':{health:{heartRate:1,bloodPressure:0,sleep:0},totalResidents:1,activeLocationAlerts:0,activeCareIncidents:0,overdueCareIncidents:0,pendingNotifications:0,failedNotifications:0,staleNotifications:0,activeDevices:1,offlineDevices:0,lowBatteryDevices:0,unboundDevices:0,capturedAt:'2026-11-11T00:00:00Z'},
   'api/v1/auth/session':{authenticated:signedIn,account:signedIn?{role,id:1,duty:role==='ADMIN'?'supervisor':undefined,phone:'13000000000',oldId:role==='FAMILY'?1:null,displayName:role==='FAMILY'?'小明':'管理员'}:{},csrfToken:'mock-csrf-token',csrfHeader:'X-CSRF-TOKEN'},
   'api/v1/auth/logout':{returnCode:'1111'},
   'api/v1/family/updateTouPic':{returnCode:'1111'},
   'api/v1/notices/list':{texts:mode==='empty'?[]:[{textId:1,picId:0,title:'设备使用与照护提醒',time:'2026-10-13 10:20'},{textId:2,picId:0,title:'服务安排与平台使用说明',time:'2026-10-12 09:00'}]},
   'api/v1/vitals/history':{vitalRecords:mode==='empty'?[]:[{name:'李奶奶',preState:unknown?null:0,rateState:unknown?null:1,createTime:'2026-10-13 10:20'}]},
   'api/v1/activity/history':{activityRecords:mode==='empty'?[]:[{name:'李奶奶',step:2860,sleep:'7小时20分',sleepAlertState:unknown?null:0,createTime:'2026-10-13 10:20'}]},
   'api/v1/position/selectFamily':{positionDtos:mode==='empty'?[]:[{name:'李奶奶',status:unknown?null:1,lng:121.4737,lat:31.2304,beTime:'2026-10-13 10:20'}]},
   'api/v1/family/selectTwo':{familyDto2:{familyId:1,familyName:'小明',familySex:'男',familyAddress:'示例联系地址',familyTouPic:''}},
   'api/v1/vitals/chart':{hrs:[72,76,73],bps:[120,125,122],createTimes:['08:00','09:00','10:00'],time:'2026-10-13'},
   'api/v1/activity/chart':{sleep:[7,8,7],createTime:['10-11','10-12','10-13']},
   'api/v1/notices/detail':{text:{picId:1,title:'设备使用与照护提醒',time:'2026-10-13 10:20',textMain:'请定期查看设备上传记录。\n平台公告会在这里展示完整内容。'},pics:[]},
   'api/v1/notice-comments/list':{comments:comments},
   'api/v1/residents/list':{oldPeople:[{oldId:1,name:'李奶奶',sex:'女',age:78}]},
   'api/v1/residents/details':{oldPersonDetails:[{oldId:1,name:'李奶奶',sex:'女',age:78,customerName:'张女士',phone:'13000000000',address:'示例联系地址',deviceId:1}],total:1},
   'api/v1/guardians/list':{customers:[{customerId:1,customerName:'张女士',name:'张女士',customerPhone:'13000000000',customerSex:'女',customerAddress:'示例联系地址'}],total:1},
   'api/v1/monitoring-profiles/list':{watchDtos:[{deviceId:1,name:'李奶奶',sex:'女',oldId:1,rateMax:100,rateMin:60}]},
   'api/v1/activity/latest':{activityRecords:[{name:'李奶奶',step:2860,sleep:'7小时20分',sleepAlertState:0,createTime:'2026-10-14 10:20'}]},
   'api/v1/activity/alerts/device-counts':{sleep:0},
   'api/v1/notice-comments/search':{moderatedComments:[]},
   'api/v1/device/list':{devices:mode==='empty'?[]:[{deviceId:1,deviceNo:'CARE-001',alias:'家人的手表',oldId:1,oldPersonName:'李奶奶',online:1,battery:76,lastTime:'2026-10-14 10:20',lastLng:121.4737,lastLat:31.2304},{deviceId:2,deviceNo:'CARE-002',alias:'备用手表',oldId:null,oldPersonName:'',online:0,battery:null,lastTime:'',lastLng:null,lastLat:null}]},
   'api/v1/device/alertCounts':{items:[{deviceId:1,personCount:0,rateCount:1,preCount:0}],personTotal:0,rateTotal:1,preTotal:0,total:1},
   'api/v1/device/create':{returnCode:'1111',deviceId:3,deviceNo:'CARE-003',secret:'mock-device-secret'},
   'api/v1/device/bindCodeList':{bindCodes:[]},
   'api/v1/device/alertDetail':{hasAlert:1,deviceNo:'CARE-001',alias:'家人的手表',oldPersonName:'李奶奶',reason:'心率状态异常',advice:'请联系家属确认情况',detail:'测试设备上报',occurTime:'2026-10-14 10:20',kindText:'心率',recordId:1},
   'api/v1/inviteCode/oldPersonList':{oldPeople:[{oldId:1,name:'李奶奶'}]},
   'api/v1/inviteCode/list':{inviteCodes:mode==='empty'?[]:[{id:1,code:'CARE-FAMILY',oldPersonName:'李奶奶',createUser:'管理员',createTime:'2026-10-14 10:20',expireTime:'',status:'unused',usedTime:''}]},
   'api/v1/inviteCode/create':{returnCode:'1111',codes:['CARE-NEW-01']},
   'api/v1/notices/create':{returnCode:mode==='save-error'?'0000':'1111'},
   'api/v1/image/saveImage':{returnCode:mode==='upload-error'?'0000':'1111',filename:'saved-image.png'},
   'api/v1/notices/delete':{returnCode:'1111'},
   'api/v1/notices/update':{returnCode:mode==='save-error'?'0000':'1111'} ,
   'api/v1/locations/alerts/summary':{totalResidents:1,activeAlerts:1},
   'api/v1/position/select':{positionDtos:[{status:1}]},
   'api/v1/health/alerts/device-counts':{heartRate:1,bloodPressure:0,sleep:0},
   'api/v1/family/select':{familyDtos:[{name:'李奶奶',oldId:1,familyName:'小明',familyRelation:'子女',familySex:'男',familyPhone:'13000000000',familyAddress:'示例地址'}]},
   'api/v1/vitals/latest':{vitalRecords:[{name:'李奶奶',rateState:1,preState:0,createTime:'2026-10-14 09:00'}]},
   'api/v1/vitals/alerts/device-counts':{heartRate:1,bloodPressure:0},
   'api/v1/vitals/alerts/count':{rateTotal:1,preTotal:0},
   'api/v1/family/updatePassword':{returnCode:mode==='password-success'?'1111':'0000',message:'手机号或原密码不正确'},
   'api/v1/auth/login':loginMode==='success'?{authenticated:true}:{code:'INVALID_CREDENTIALS',message:'手机号或密码不正确，请重试'},
   'api/v1/inviteCode/checkCode':{returnCode:'0000',message:'邀请码无效'},
   'api/v1/notice-comments/create':{returnCode:mode==='comment-failure'?'0000':'1111'},
   'api/v1/notice-comments/toggle-like':{returnCode:'1111'}
  };
  if(!(endpoint in fixtures)){report.unexpectedApi.push(endpoint);await route.fulfill({status:404,body:'{}'});return;}
  if(mode==='loading')await new Promise(resolve=>setTimeout(resolve,700));
  if(mode==='error' && endpoint!=='api/v1/auth/login' && endpoint!=='api/v1/service/status'){await route.fulfill({status:503,contentType:'application/json',body:'{}'});return;}
  if(endpoint==='api/v1/notice-comments/create' && mode!=='comment-failure'){const data=route.request().postDataJSON();comments.push({saidId:1,personName:role==='FAMILY'?'小明':'管理员',said:data.said,niceCount:0,good:0});}
  if(endpoint==='api/v1/notice-comments/toggle-like' && comments[0]){comments[0].good=comments[0].good?0:1;comments[0].niceCount=comments[0].good;}
  if (endpoint==='api/v1/auth/logout') signedIn=false;
  if (endpoint==='api/v1/auth/login' && mode==='login-success') { signedIn=true; fixtures[endpoint]={authenticated:true}; }
  if (route.request().method()==='POST') report.checks.push({name:'CSRF header '+endpoint,pass:route.request().headers()['x-csrf-token']==='mock-csrf-token'});
  await route.fulfill({status:endpoint==='api/v1/auth/login' && loginMode!=='success' && mode!=='login-success' ? 401 : fixtures[endpoint].returnCode==='0000' ? 400 : 200,contentType:'application/json',body:JSON.stringify(fixtures[endpoint].returnCode === '0000' ? {code:'INVALID_REQUEST',message:'请求参数无效',...fixtures[endpoint]} : {returnCode:'1111',...fixtures[endpoint]})});
 });
 const page=await context.newPage();
 page.on('pageerror',error=>report.pageErrors.push(error.message));
 page.on('response',r=>{if(r.status()>=400 && !r.url().includes('/api/v1/'))report.missingAssets.push(r.url());});

 try { await run({page,context,browser,base,report,setMode:value=>{mode=value},setRole:value=>{role=value},setSignedIn:value=>{signedIn=value}}) } finally {await browser.close();await new Promise(resolve=>server.close(resolve));}
};
