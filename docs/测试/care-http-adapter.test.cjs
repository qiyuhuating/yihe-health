const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const fixture=require('./fixtures/http-snapshot.json');
const root=require('node:path').resolve(__dirname,'..');
const response=(status,body)=>new Response(status===204?null:JSON.stringify(body),{status,headers:{'Content-Type':'application/json'}});
function app(fetch,timeout=1000){
  const c={fetch,performance,Headers,AbortController,setTimeout,clearTimeout,URL,console,
    YIHE_RUNTIME_CONFIG:{mode:'http',apiBaseUrl:'/api/v1',requestTimeoutMs:timeout},events:[],
    CustomEvent:class{constructor(type){this.type=type;}},dispatchEvent(event){this.events.push(event.type);},addEventListener(){}};
  c.window=c;vm.createContext(c);
  for(const file of ['care-transport','care-session','care-http-adapter','care-api'])vm.runInContext(fs.readFileSync(root+'/脚本/'+file+'.js','utf8'),c);
  c.CareTransport.setCsrfToken('test-csrf');
  return c;
}
test('server snapshot is validated and supplies personnel without demo fallback',async()=>{
  const c=app(async()=>response(200,{data:fixture}));
  assert.equal(c.CareAPI.STAFF.length,0);
  const s=await c.CareAPI.snapshot();
  assert.equal(s.residents[101].name,'测试居民');
  assert.equal(c.CareAPI.STAFF[0].id,'care-nurse');
  for(const invalid of [{}, {...fixture,logs:null}, {...fixture,events:[{...fixture.events[0],residentId:999}]}, {...fixture,residents:{101:{...fixture.residents[101],contacts:null}}}]){
    const bad=app(async()=>response(200,invalid));
    await assert.rejects(bad.CareAPI.snapshot(),e=>e.code==='INVALID_RESPONSE');
    assert.equal(bad.CareAPI.STAFF.length,0);
  }
});
test('writes use session credentials and CSRF and never submit a trusted actor',async()=>{
  let sent;
  const c=app(async(url,options)=>{sent={url,...options};return response(204);});
  await c.CareAPI.transition('id/1','resolved','forged','核实完成',7,'care-2');
  assert.equal(sent.url,'/api/v1/events/id%2F1/transitions');
  assert.equal(sent.credentials,'same-origin');
  assert.equal(sent.cache,'no-store');
  assert.equal(sent.redirect,'error');
  assert.equal(sent.headers['X-CSRF-Token'],'test-csrf');
  assert.deepEqual(JSON.parse(sent.body),{action:'resolved',note:'核实完成',revision:7,targetStaffId:'care-2'});
});
for(const [status,code] of [[401,'UNAUTHENTICATED'],[403,'FORBIDDEN'],[409,'CONFLICT'],[422,'VALIDATION_FAILED'],[429,'RATE_LIMITED'],[503,'SERVICE_UNAVAILABLE']]){
  test('HTTP '+status+' rejects with '+code,async()=>{
    const c=app(async()=>response(status,{message:'internal details must not leak'}));
    await assert.rejects(c.CareAPI.snapshot(),e=>e.code===code&&!e.message.includes('internal'));
    assert.equal(c.events.includes('care:unauthenticated'),status===401);
  });
}
test('network failure and timeout settle without retrying ambiguous writes',async()=>{
  let count=0;
  const c=app(async()=>{count++;throw new TypeError('offline');});
  await assert.rejects(c.CareAPI.addDose(101,'药品','2026-10-01T00:00:00Z'),e=>e.code==='NETWORK_ERROR'&&e.message.includes('结果尚未确认'));
  assert.equal(count,1);
  const hanging=app((_url,{signal})=>new Promise((_,reject)=>signal.addEventListener('abort',()=>reject(new DOMException('aborted','AbortError')))),100);
  await assert.rejects(hanging.CareAPI.snapshot(),e=>e.code==='REQUEST_TIMEOUT');
});
test('HTML and malformed JSON are rejected instead of published as business data',async()=>{
  const html=app(async()=>new Response('<html>login</html>',{headers:{'Content-Type':'text/html'}}));
  await assert.rejects(html.CareAPI.snapshot(),e=>e.code==='INVALID_RESPONSE');
  const invalid=app(async()=>new Response('{',{headers:{'Content-Type':'application/json'}}));
  await assert.rejects(invalid.CareAPI.snapshot(),e=>e.code==='INVALID_RESPONSE');
});
test('session login validates server role and retains tokens only in memory',async()=>{
  const calls=[];
  const c=app(async(url,options)=>{
    calls.push({url,...options});
    return response(200,{user:options.method==='POST'?{name:'测试居民',role:'resident',residentId:101}:null,csrfToken:'rotated-token'});
  });
  const user=await c.CareSession.login('resident','account','secret');
  assert.equal(user.residentId,101);
  assert.equal(calls[1].headers['X-CSRF-Token'],'rotated-token');
  assert.equal(calls[1].url,'/api/v1/session');
  assert.deepEqual(JSON.parse(calls[1].body),{role:'resident',username:'account',password:'secret'});
  await assert.rejects(c.CareSession.login('staff','account','secret'),e=>e.code==='FORBIDDEN');
});
test('simulator operations never issue requests in HTTP mode',async()=>{
  let calls=0;const c=app(async()=>{calls++;return response(200,fixture);});
  await assert.rejects(c.CareAPI.simulate(101,'health'),e=>e.code==='FEATURE_DISABLED');
  await assert.rejects(c.CareAPI.setSimulator(true),e=>e.code==='FEATURE_DISABLED');
  assert.equal(calls,0);
});
test('presentation acknowledgement carries the cycle that was actually displayed',async()=>{
  let body;const c=app(async(_url,options)=>{body=JSON.parse(options.body);return response(204);});
  await c.CareAPI.markPresented(['alert-1'],'forged',[3]);
  assert.deepEqual(body,{events:[{id:'alert-1',cycle:3}]});
});
test('missing CSRF session prevents any write request',async()=>{
  let calls=0;const c=app(async()=>{calls++;return response(204);});
  c.CareTransport.setCsrfToken('');
  await assert.rejects(c.CareAPI.confirmDose(101,'dose-1'),e=>e.code==='SESSION_REQUIRED');
  assert.equal(calls,0);
});

test('server clock ignores local wall-clock changes and rejects missing time',async()=>{
  const c=app(async()=>response(200,fixture));let elapsed=100;
  c.performance={now:()=>elapsed};await c.CareAPI.snapshot();
  const start=Date.parse(fixture.serverTime);assert.equal(c.CareAPI.now(),start);
  vm.runInContext('Date.now=()=>0',c);elapsed+=60000;
  assert.equal(c.CareAPI.now(),start+60000);
  const bad=app(async()=>response(200,{...fixture,serverTime:undefined}));
  await assert.rejects(bad.CareAPI.snapshot(),e=>e.code==='INVALID_RESPONSE');
});
test('wrong-role login revokes server session and clears local identity',async()=>{
  const methods=[];const c=app(async(_url,options)=>{
    methods.push(options.method);
    return options.method==='DELETE'?response(204):response(200,{user:options.method==='POST'?{name:'居民',role:'resident',residentId:101}:null,csrfToken:'token'});
  });
  await assert.rejects(c.CareSession.login('staff','account','secret'),e=>e.code==='FORBIDDEN');
  assert.deepEqual(methods,['GET','POST','DELETE']);assert.equal(c.CareSession.current,null);
  await assert.rejects(c.CareAPI.confirmDose(101,'dose'),e=>e.code==='SESSION_REQUIRED');
});
test('failed wrong-role revocation is reported as an unknown result',async()=>{
  const c=app(async(_url,options)=>{
    if(options.method==='DELETE')throw new TypeError('offline');
    return response(200,{user:options.method==='POST'?{name:'居民',role:'resident',residentId:101}:null,csrfToken:'token'});
  });
  await assert.rejects(c.CareSession.login('staff','account','secret'),e=>e.code==='SESSION_REVOKE_FAILED');
  assert.equal(c.CareSession.current,null);
});
test('offline dangerous sample keeps its severity and freshness warning',async()=>{
  const c=app(async()=>response(200,fixture));c.escapeHtml=value=>String(value);
  vm.runInContext(fs.readFileSync(root+'/脚本/care-ui.js','utf8'),c);
  const markup=c.CareUI.vitals({...fixture.residents[101],deviceState:'offline',bloodOxygen:80,metricStates:{bloodOxygen:'danger'}});
  assert.match(markup,/data-status="danger"/);assert.match(markup,/离线前危险采样，当前状态未知/);
});
test('staff idle timeout locks locally and requests server revocation',async()=>{
  const methods=[];const c=app(async(_url,options)=>{
    methods.push(options.method);
    if(options.method==='DELETE')return response(204);
    return response(200,{user:options.method==='POST'?{name:'工作人员',role:'staff',staffId:'care-nurse'}:null,csrfToken:'token'});
  });
  let elapsed=0,idleCallback;
  c.performance={now:()=>elapsed};const realTimeout=c.setTimeout;
  c.setTimeout=(fn,ms)=>ms>1000?(idleCallback=fn,123):realTimeout(fn,ms);
  await c.CareSession.login('staff','account','secret');elapsed=16*60000;idleCallback();
  await new Promise(resolve=>setImmediate(resolve));
  assert.equal(c.CareSession.current,null);assert.ok(methods.includes('DELETE'));
  assert.ok(c.events.includes('care:unauthenticated'));
});
test('public validation codes produce fixed hints without exposing server prose',async()=>{
  const c=app(async()=>response(422,{error:{code:'NOTE_REQUIRED'},message:'private server data'}));
  await assert.rejects(c.CareAPI.transition('id','resolved','forged','',1),e=>e.code==='VALIDATION_FAILED'&&e.message==='请填写处置记录后再提交。');
});
