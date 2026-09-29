const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
function app() {
  const values = new Map(); let fail = false, queue = Promise.resolve(), now = Date.UTC(2026,8,27,4);
  class Clock extends Date {constructor(...args){super(...(args.length?args:[now]));}static now(){return now;}}
  const context = {console, YIHE_RUNTIME_CONFIG:{mode:'demo'}, Date:Clock, Math, JSON, structuredClone, crypto:require('node:crypto').webcrypto,
    localStorage:{getItem:k=>values.get(k)||null,setItem:(k,v)=>{if(fail)throw Error('quota');values.set(k,v);}},
    navigator:{locks:{request:(_name,fn)=>{const task=queue.then(fn);queue=task.catch(()=>{});return task;}}},
    dispatchEvent(){}, CustomEvent:class {}, addEventListener(){}};
  context.window=context; vm.createContext(context);
  for(const file of ['数据/residents.js','脚本/metrics.js','脚本/care-api.js']) vm.runInContext(fs.readFileSync(path.join(__dirname,'..',file),'utf8'),context);
  return {api:context.CareAPI,context,values,fail:()=>fail=true,advance:ms=>now+=ms,now:()=>now};
}
for(const type of ['health','medication','fence','offline']) test(type+' completes with durable trail',async()=>{
  const {api}=app(); await api.init(); await api.simulate(1,type); const s=await api.snapshot();
  const event=s.events.find(e=>e.residentId===1&&e.type===type); assert.ok(event); assert.equal(event.state,'new');
  let e=await api.transition(event.id,'claim','staff-1','',event.revision);
  e=await api.transition(e.id,'processing','staff-1','已联系核实',e.revision);
  e=await api.transition(e.id,'resolved','staff-1','复核完成，已告知家属',e.revision);
  const saved=(await api.snapshot()).events.find(x=>x.id===e.id);
  assert.equal(saved.state,'resolved'); assert.equal(saved.owner,'staff-1'); assert.equal(saved.history.length,4);
  assert.ok(saved.closedAt); await api.scan();
  assert.equal((await api.snapshot()).events.filter(x=>x.residentId===1&&x.type===type).length,1);
});
test('illegal, ownerless and stale transitions reject',async()=>{
  const {api}=app();await api.init();await api.simulate(1,'health');let e=(await api.snapshot()).events.find(e=>e.residentId===1&&e.type==='health');
  await assert.rejects(api.transition(e.id,'resolved','staff-1','done',e.revision));
  const original=e.revision;e=await api.transition(e.id,'claim','staff-1','',e.revision);
  await assert.rejects(api.transition(e.id,'claim','staff-2','',original));
  await assert.rejects(api.transition(e.id,'processing','staff-2','',e.revision));
  e=await api.transition(e.id,'processing','staff-1','',e.revision);
  await assert.rejects(api.transition(e.id,'resolved','staff-1','',e.revision));
});
test('escalation stays open and target takes over',async()=>{
  const {api}=app();await api.init();await api.simulate(1,'fence');let e=(await api.snapshot()).events.find(e=>e.residentId===1&&e.type==='fence');
  e=await api.transition(e.id,'claim','staff-1','',e.revision);e=await api.transition(e.id,'processing','staff-1','',e.revision);
  e=await api.transition(e.id,'escalated','staff-1','需要上级协助',e.revision,'staff-2');
  assert.equal(e.owner,'staff-2');assert.ok(api.isOpen(e));e=await api.transition(e.id,'processing','staff-2','接手',e.revision);
  assert.equal(e.state,'processing');
});
test('storage failure leaves every event unchanged',async()=>{
  const {api,fail}=app();await api.init();await api.simulate(1,'health');const s=await api.snapshot(),e=s.events.find(e=>e.type==='health');fail();
  await assert.rejects(api.transition(e.id,'claim','staff-1','',e.revision));
  assert.deepEqual(await api.snapshot(),s);
});
test('simultaneous claims only have one winner',async()=>{
  const {api}=app();await api.init();await api.simulate(1,'health');const e=(await api.snapshot()).events.find(e=>e.residentId===1&&e.type==='health');
  const results=await Promise.allSettled(['staff-1','staff-2'].map(actor=>api.transition(e.id,'claim',actor,'',e.revision)));
  assert.equal(results.filter(x=>x.status==='fulfilled').length,1);
});
test('medication confirmation recovers signal but never closes a staff event',async()=>{
  const {api}=app();await api.init();await api.simulate(1,'medication');const s=await api.snapshot(),dose=s.residents['1'].doses.at(-1);
  await api.confirmDose(1,dose.id);const e=(await api.snapshot()).events.find(e=>e.type==='medication');
  assert.equal(e.state,'new');assert.ok(e.sourceRecoveredAt);
});
test('heartbeat boundary and recovery retain an open event',async()=>{
  const {api,advance}=app();await api.init();await api.setSimulator(false);advance(300000);await api.scan();
  assert.equal((await api.snapshot()).events.filter(e=>e.type==='offline').length,0);
  advance(1);await api.scan();assert.equal((await api.snapshot()).events.filter(e=>e.type==='offline').length,12);
  await api.simulate(1,'recover');const e=(await api.snapshot()).events.find(e=>e.residentId===1&&e.type==='offline');
  assert.equal(e.state,'new');assert.ok(e.sourceRecoveredAt);
});
test('confirmed schedule detects overdue only after grace boundary',async()=>{
  const {api,advance,now}=app();await api.init();const due=new Date(now()+60000).toISOString();
  await api.addDose(1,'演示任务',due,'staff-1');advance(31*60000);await api.scan();
  assert.equal((await api.snapshot()).events.filter(e=>e.type==='medication').length,0);
  advance(1);await api.scan();assert.equal((await api.snapshot()).events.filter(e=>e.type==='medication').length,1);
});
test('resolved episode can recur only after recovered signal',async()=>{
  const {api}=app();await api.init();await api.simulate(1,'fence');let e=(await api.snapshot()).events.find(e=>e.type==='fence');
  e=await api.transition(e.id,'claim','staff-1','',e.revision);e=await api.transition(e.id,'processing','staff-1','',e.revision);
  await api.transition(e.id,'false_alarm','staff-1','定位复核为误报',e.revision);
  await api.scan();assert.equal((await api.snapshot()).events.filter(e=>e.type==='fence').length,1);
  await api.simulate(1,'recover');await api.simulate(1,'fence');assert.equal((await api.snapshot()).events.filter(e=>e.type==='fence').length,2);
});
test('presentation trace is idempotent and never implies SMS delivery',async()=>{
  const {api}=app();await api.init();await api.simulate(1,'health');const e=(await api.snapshot()).events.find(e=>e.residentId===1&&e.type==='health');
  await api.markPresented([e.id],'staff-1');await api.markPresented([e.id],'staff-2');
  const saved=(await api.snapshot()).events.find(x=>x.id===e.id);
  assert.equal(saved.history.length,2);assert.ok(saved.notification.pagePresentedAt);assert.equal(saved.notification.externalDelivered,false);
});
test('worsening signal updates current event severity and queues a new presentation',async()=>{
  const {api}=app();await api.init();const before=(await api.snapshot()).events.find(e=>e.residentId===3&&e.type==='health');
  assert.equal(before.severity,'warning');await api.markPresented([before.id],'staff-1');
  await api.simulate(3,'health');const after=(await api.snapshot()).events.find(e=>e.id===before.id);
  assert.equal(after.severity,'danger');assert.match(after.detail,/80/);assert.ok(Date.parse(after.dueAt)<Date.parse(before.dueAt));
  assert.ok(after.notification.cycle>after.notification.presentedCycle);
});

test('invalid metric values are unknown, never healthy or fabricated dangerous samples',()=>{
  const {context:{Metrics}}=app();
  for(const value of [undefined,null,NaN,Infinity,'97','',{},false]) {
    assert.equal(Metrics.metricStatus('bloodOxygen',value),'unknown');
    assert.equal(Metrics.isLifeThreat('bloodOxygen',value),false);
  }
  assert.equal(Metrics.metricStatus('bloodOxygen',97),'normal');
  assert.equal(Metrics.metricStatus('bloodOxygen',80),'danger');
});

test('corrupt aggregates reject reads and writes without replacing the original',async()=>{
  const {api,values}=app();await api.init();const good=await api.snapshot();
  const cases=[null,{},'{broken', {...good,revision:-1}, {...good,residents:{1:null}},
    {...good,events:[{...good.events[0],residentId:999}]},
    {...good,events:[{...good.events[0],history:[null]}]},
    {...good,logs:[null]}, {...good,conditions:{bad:{active:true,eventId:'missing'}}}];
  for(const change of [p=>p.doses=null,p=>p.contacts=[null],p=>p.trend=[null],p=>p.lastSeen='invalid',p=>p.pos={x:'NaN',y:0}]) {
    const next=structuredClone(good);change(next.residents[1]);cases.push(next);
  }
  for(const bad of cases){
    const raw=typeof bad==='string'?bad:JSON.stringify(bad);values.set(api.KEY,raw);
    await assert.rejects(api.snapshot(),/本机监护记录/);
    await assert.rejects(api.scan(),/本机监护记录/);
    assert.equal(values.get(api.KEY),raw);
  }
  values.set(api.KEY,JSON.stringify(good));assert.deepEqual(await api.snapshot(),good);
});

test('missing health sample stays actionable and cannot claim signal recovery',async()=>{
  const {api,values}=app();await api.init();await api.simulate(1,'health');
  let s=await api.snapshot(),e=s.events.find(e=>e.residentId===1&&e.type==='health');
  delete s.residents[1].bloodOxygen;values.set(api.KEY,JSON.stringify(s));await api.scan();
  e=(await api.snapshot()).events.find(x=>x.id===e.id);
  assert.equal(e.sourceRecoveredAt,undefined);assert.match(e.detail,/血氧.*数据不足/);
  await api.simulate(1,'recover');e=(await api.snapshot()).events.find(x=>x.id===e.id);
  assert.ok(e.sourceRecoveredAt);assert.equal(e.state,'new');
});

test('offline data cannot prove an existing health condition has recovered',async()=>{
  const {api}=app();await api.init();await api.simulate(1,'health');await api.simulate(1,'offline');
  const s=await api.snapshot(),health=s.events.find(e=>e.residentId===1&&e.type==='health');
  assert.equal(health.sourceRecoveredAt,undefined);
  assert.ok(s.events.some(e=>e.residentId===1&&e.type==='offline'));
});

test('unsupported browser rejects every write with an actionable message',async()=>{
  const {api,context,values}=app();await api.init();const before=values.get(api.KEY);delete context.navigator.locks;
  await assert.rejects(api.simulate(1,'health'),/HTTPS|浏览器/);
  assert.equal(values.get(api.KEY),before);assert.ok((await api.snapshot()).residents[1]);
});
