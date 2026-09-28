/* Local demo adapter. Every mutation is serialized and persisted before publication.
   A future server must own authentication, detection, revisions and notification delivery. */
window.CareAPI = (() => {
  'use strict';
  const KEY = 'yihe-community-v1';
  const STAFF = [{id:'staff-1',name:'张护士'},{id:'staff-2',name:'李社工'},{id:'staff-3',name:'值班主管'}];
  const TYPES = {health:'健康指标异常',medication:'漏服药',offline:'设备离线',fence:'安全围栏'};
  const STATES = {new:'新预警',claimed:'已接单',processing:'处理中',resolved:'已解决',false_alarm:'误报',escalated:'已升级'};
  const METRICS = {heartRate:'心率',bloodOxygen:'血氧',temperature:'体温',systolic:'收缩压',diastolic:'舒张压',bloodSugar:'血糖'};
  const iso = time => new Date(time).toISOString();
  const isOpen = e => !['resolved','false_alarm'].includes(e.state);
  const knownStaff = id => STAFF.some(s=>s.id===id);
  const clone = value => JSON.parse(JSON.stringify(value));
  const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
  const text = value => typeof value === 'string' && !!value.trim();
  const date = value => typeof value === 'string' && Number.isFinite(Date.parse(value));
  function failure(code,message) {return Object.assign(new Error(message),{code});}
  function validate(s) {
    const check=(ok,field)=>{if(!ok)throw failure('DATA_INVALID','本机监护记录格式异常（'+field+'）。原记录已保留，请联系维护人员修复后重新读取。');};
    check(record(s)&&s.version===1,'版本');
    check(Number.isSafeInteger(s.revision)&&s.revision>=0&&typeof s.simulator==='boolean','状态');
    check(record(s.residents)&&Array.isArray(s.events)&&record(s.conditions)&&Array.isArray(s.logs),'集合');
    for(const [id,p] of Object.entries(s.residents)) {
      check(record(p)&&Number.isSafeInteger(p.id)&&p.id>0&&String(p.id)===id,'居民编号');
      check(text(p.name)&&Number.isInteger(p.age)&&p.age>0&&p.age<=120&&knownStaff(p.responsible),'居民资料');
      check(['lastSeen','updatedAt','locationAt','stationarySince'].every(k=>date(p[k])),'采样时间');
      check(['devicePaused','locationPaused','risk','outside'].every(k=>typeof p[k]==='boolean'),'设备状态');
      check(record(p.pos)&&Number.isFinite(p.pos.x)&&Number.isFinite(p.pos.y),'位置');
      check(Array.isArray(p.contacts)&&p.contacts.every(c=>record(c)&&text(c.name)&&typeof c.phone==='string'),'联系人');
      check(Array.isArray(p.doses)&&p.doses.every(d=>record(d)&&text(d.id)&&text(d.name)&&date(d.dueAt)&&date(d.createdAt)&&text(d.source)&&(!d.confirmedAt||date(d.confirmedAt))),'用药计划');
      check(new Set(p.doses.map(d=>d.id)).size===p.doses.length,'重复用药编号');
      check(Array.isArray(p.trend)&&p.trend.every(t=>record(t)&&date(t.at)),'趋势');
    }
    const events=new Map();
    for(const e of s.events) {
      check(record(e)&&text(e.id)&&!events.has(e.id),'事件编号');
      check(Number.isSafeInteger(e.residentId)&&Object.hasOwn(s.residents,e.residentId),'事件居民引用');
      check(Object.hasOwn(TYPES,e.type)&&Object.hasOwn(STATES,e.state)&&['warning','danger'].includes(e.severity),'事件状态');
      check(text(e.source)&&text(e.detail)&&Number.isSafeInteger(e.revision)&&e.revision>0,'事件内容');
      check(e.state==='new'?e.owner===null:knownStaff(e.owner),'事件负责人');
      check(date(e.createdAt)&&date(e.dueAt)&&['updatedAt','closedAt','sourceRecoveredAt'].every(k=>e[k]===undefined||date(e[k])),'事件时间');
      check(Array.isArray(e.history)&&e.history.every(t=>record(t)&&date(t.at)&&text(t.action)&&text(t.actor)&&typeof t.note==='string'&&Object.hasOwn(STATES,t.state)&&(t.owner===null||knownStaff(t.owner))),'事件历史');
      const n=e.notification;
      check(record(n)&&n.channel==='page'&&n.externalDelivered===false&&date(n.queuedAt)&&text(n.label),'提醒记录');
      check(['cycle','presentedCycle'].every(k=>n[k]===undefined||Number.isSafeInteger(n[k])&&n[k]>=0)&&(!n.pagePresentedAt||date(n.pagePresentedAt)),'提醒轮次');
      events.set(e.id,e);
    }
    for(const [key,c] of Object.entries(s.conditions)) {
      check(record(c)&&typeof c.active==='boolean'&&events.has(c.eventId)&&events.get(c.eventId).source===key&&['warning','danger'].includes(c.severity),'检测条件');
    }
    check(s.logs.every(l=>record(l)&&date(l.at)&&text(l.actor)&&text(l.action)&&Object.hasOwn(s.residents,l.residentId)),'操作记录');
    return s;
  }
  function seed(now) {
    const residents = {};
    Object.values(window.RESIDENT_DATA).forEach((p,index) => {
      let contacts = [],profile = {};
      try {
        const old = JSON.parse(localStorage.getItem('hm-p-emergency-'+p.id)||'{}');
        contacts = [1,2,3].filter(i=>old['n'+i]&&old['p'+i]).map(i=>({name:old['n'+i],phone:old['p'+i]}));
        const saved = JSON.parse(localStorage.getItem('hm-p-profile-'+p.id)||'{}');
        if(typeof saved.name==='string'&&saved.name.trim())profile.name=saved.name.trim();
        if(Number.isInteger(saved.age)&&saved.age>0&&saved.age<=120)profile.age=saved.age;
      } catch (_) { /* Old entries remain untouched; do not guess an owner. */ }
      residents[p.id] = {...clone(p),...profile,responsible:STAFF[index%STAFF.length].id,contacts,doses:[],
        lastSeen:iso(now),updatedAt:iso(now),devicePaused:false,locationPaused:false,
        locationAt:iso(now),stationarySince:iso(now),risk:p.careLevel!=='自理',
        pos:{x:p.posX||150,y:p.posY||125},outside:false,trend:[]};
      sample(residents[p.id],now);
    });
    return {version:1,revision:0,simulator:true,residents,events:[],conditions:{},logs:[]};
  }
  function read() {
    let raw,value;
    try {raw=localStorage.getItem(KEY);}
    catch (_) {throw failure('STORAGE_UNAVAILABLE','无法读取本机监护记录，请允许此网站使用浏览器存储后重新读取。');}
    if (raw===null) return null;
    try {value=JSON.parse(raw);}
    catch (_) {throw failure('DATA_INVALID','本机监护记录无法解析。原记录已保留，请联系维护人员修复后重新读取。');}
    return validate(value);
  }
  function sample(p,now) {
    p.updatedAt=iso(now);p.lastSeen=iso(now);
    p.trend.push({at:iso(now),...Object.fromEntries(Object.keys(METRICS).map(k=>[k,p[k]]))});
    p.trend=p.trend.slice(-72);
  }
  async function mutate(fn) {
    if (!navigator.locks?.request||!crypto.randomUUID) throw failure('ENV_UNSUPPORTED','当前浏览器无法安全保存监护记录。请通过 localhost 或 HTTPS 打开，并使用支持 Web Locks 的新版浏览器后重试。');
    return navigator.locks.request(KEY,async()=>{
      const original=read(),s=original||seed(Date.now()),before=JSON.stringify(s);
      const result=fn(s,Date.now());
      if(!original||before!==JSON.stringify(s)) {
        s.revision++;
        validate(s);
        try { localStorage.setItem(KEY,JSON.stringify(s)); }
        catch (_) { throw Error('本机保存失败，操作未完成，请检查存储空间后重试'); }
        window.dispatchEvent(new CustomEvent('care:change'));
      }
      return clone(result===undefined?s:result);
    });
  }
  function resident(s,id) {const p=s.residents[id];if(!p)throw Error('居民不存在');return p;}
  function entry(e,action,actor,note,now) {e.history.push({at:iso(now),action,actor,note,owner:e.owner,state:e.state});}
  function detect(s,now) {
    const active = new Set();
    function condition(p,type,suffix,detail,severity='warning') {
      const key=p.id+':'+type+':'+suffix; active.add(key);
      const prior=s.conditions[key];
      if(prior?.active){
        const current=s.events.find(e=>e.id===prior.eventId);
        if(current&&isOpen(current)){
          if(current.detail!==detail){current.detail=detail;current.revision++;}
          if(severity==='danger'&&current.severity!=='danger'){
            current.severity='danger';current.revision++;
            current.dueAt=iso(Math.min(Date.parse(current.dueAt),now+15*60000));
            current.notification.cycle=(current.notification.cycle||1)+1;
            entry(current,'信号恶化，重新提醒','system',detail,now);
          }
          prior.severity=severity;return;
        }
        if(severity!=='danger'||prior.severity==='danger'){prior.severity=severity;return;}
      }
      let event=s.events.find(e=>e.source===key&&isOpen(e));
      if(!event) {
        event={id:crypto.randomUUID(),residentId:p.id,type,source:key,detail,severity,state:'new',owner:null,
          createdAt:iso(now),dueAt:iso(now+(severity==='danger'?15:60)*60000),revision:1,history:[],
          notification:{channel:'page',queuedAt:iso(now),cycle:1,externalDelivered:false,label:'仅页面提醒，未发送短信'}};
        entry(event,'发现并生成页面提醒','system',detail,now);s.events.push(event);
      } else {delete event.sourceRecoveredAt;event.revision++;entry(event,'信号再次异常','system',detail,now);}
      s.conditions[key]={active:true,eventId:event.id,severity};
    }
    for(const p of Object.values(s.residents)) {
      const offline=now-Date.parse(p.lastSeen)>5*60000;
      if(offline)condition(p,'offline','heartbeat','超过 5 分钟未收到模拟设备心跳');
      const abnormal=Object.keys(METRICS).filter(k=>Metrics.metricStatus(k,p[k])!=='normal');
      if(!offline&&abnormal.length)condition(p,'health','metrics',abnormal.map(k=>METRICS[k]+' '+(Metrics.metricStatus(k,p[k])==='unknown'?'数据不足，请核实采样':p[k])).join('；'),abnormal.some(k=>Metrics.metricStatus(k,p[k])==='danger')?'danger':'warning');
      // An absent heartbeat cannot demonstrate recovery of a previous health signal.
      if(offline&&s.conditions[p.id+':health:metrics']?.active)active.add(p.id+':health:metrics');
      for(const dose of p.doses)if(!dose.confirmedAt&&now>Date.parse(dose.dueAt)+30*60000)condition(p,'medication',dose.id,dose.name+' · 超过计划时间 30 分钟未确认');
      if(p.outside)condition(p,'fence','outside','模拟定位超出社区安全围栏','danger');
      if(now-Date.parse(p.locationAt)>10*60000)condition(p,'fence','location','超过 10 分钟未收到模拟定位');
      if(p.risk&&now-Date.parse(p.stationarySince)>120*60000)condition(p,'fence','stationary','高风险居民模拟位置超过 2 小时未变化');
    }
    for(const [key,c] of Object.entries(s.conditions))if(c.active&&!active.has(key)) {
      c.active=false;const event=s.events.find(e=>e.id===c.eventId);
      if(event&&isOpen(event)){event.sourceRecoveredAt=iso(now);event.revision++;entry(event,'信号恢复，待人工复核','system','不会自动结案',now);}
    }
  }
  function scan() {return mutate((s,now)=>{
    for(const p of Object.values(s.residents))if(s.simulator&&!p.devicePaused&&now-Date.parse(p.lastSeen)>=30000){sample(p,now);if(!p.locationPaused)p.locationAt=iso(now);}
    detect(s,now);
  });}
  function simulate(id,type) {return mutate((s,now)=>{
    const p=resident(s,id);
    if(type==='health'){p.bloodOxygen=80;p.devicePaused=false;sample(p,now);}
    else if(type==='medication')p.doses.push({id:crypto.randomUUID(),name:'演示服药确认（非医嘱）',dueAt:iso(now-45*60000),createdAt:iso(now),source:'模拟信号'});
    else if(type==='fence'){p.outside=true;p.pos={x:790,y:310};p.locationAt=iso(now);}
    else if(type==='offline'){p.devicePaused=true;p.lastSeen=iso(now-6*60000);}
    else if(type==='location'){p.locationPaused=true;p.locationAt=iso(now-11*60000);}
    else if(type==='stationary'){p.risk=true;p.stationarySince=iso(now-121*60000);}
    else if(type==='recover'){
      const baseline=window.RESIDENT_DATA[id];Object.keys(METRICS).forEach(k=>p[k]=baseline[k]);
      p.devicePaused=false;p.locationPaused=false;p.outside=false;p.pos={x:baseline.posX||150,y:baseline.posY||125};
      p.locationAt=iso(now);p.stationarySince=iso(now);sample(p,now);
    } else throw Error('未知模拟信号');
    detect(s,now);
  });}
  function transition(id,action,actor,note,revision,target) {return mutate((s,now)=>{
    if(!knownStaff(actor))throw Error('请选择当前工作人员');
    const e=s.events.find(x=>x.id===id);if(!e)throw Error('事件不存在');
    if(e.revision!==revision)throw Error('事件已被更新，请刷新后重试');
    if(!isOpen(e))throw Error('事件已经结束');
    const result=String(note||'').trim();
    if(action==='claim'){
      if(e.state!=='new'||e.owner)throw Error('事件已被接单');
      e.owner=actor;e.state='claimed';
    }else{
      if(e.owner!==actor)throw Error('仅当前负责人可处理该事件');
      if(action==='processing'&&['claimed','escalated'].includes(e.state))e.state='processing';
      else if(['resolved','false_alarm','escalated'].includes(action)&&e.state==='processing'){
        if(!result)throw Error('请填写处理结果或升级原因');
        if(action==='escalated'){
          if(!knownStaff(target)||target===actor)throw Error('请选择另一位升级接收人');
          e.owner=target;
        }else e.closedAt=iso(now);
        e.state=action;
      }else throw Error('请按接单、处理中、完成的顺序操作');
    }
    e.updatedAt=iso(now);e.revision++;entry(e,STATES[e.state],actor,result,now);return e;
  });}
  function saveResident(id,patch,actor) {return mutate((s,now)=>{
    const p=resident(s,id);
    if(patch.responsible!==undefined){if(!knownStaff(actor)||!knownStaff(patch.responsible))throw Error('请选择责任人员');p.responsible=patch.responsible;}
    if(patch.contacts!==undefined){
      if(!Array.isArray(patch.contacts)||patch.contacts.length>3)throw Error('最多保存三位联系人');
      for(const c of patch.contacts)if(!record(c)||!text(c.name)||c.name.trim().length>30||typeof c.phone!=='string'||!/^\+?[\d -]{5,22}$/.test(c.phone))throw Error('请填写联系人姓名和有效电话号码');
      p.contacts=patch.contacts.map(c=>({name:c.name.trim(),phone:c.phone.trim()}));
    }
    if(patch.name!==undefined){if(!text(patch.name)||patch.name.trim().length>20)throw Error('姓名须为 1 至 20 个字符');p.name=patch.name.trim();}
    if(patch.age!==undefined){if(!Number.isInteger(patch.age)||patch.age<1||patch.age>120)throw Error('请填写有效年龄');p.age=patch.age;}
    s.logs.push({at:iso(now),actor:actor||'resident-'+id,residentId:Number(id),action:'更新责任或个人资料'});
  });}
  function addDose(id,name,dueAt,actor) {return mutate((s,now)=>{
    if(!knownStaff(actor)||!text(name)||name.trim().length>100||!date(dueAt))throw Error('请填写核对后的用药名称（最多 100 字）与时间');
    if(Date.parse(dueAt)<now-60000)throw Error('计划时间不能早于当前时间');
    resident(s,id).doses.push({id:crypto.randomUUID(),name:name.trim(),dueAt:iso(Date.parse(dueAt)),createdAt:iso(now),source:'工作人员登记',actor});
    s.logs.push({at:iso(now),actor,residentId:Number(id),action:'登记已核对的用药任务'});
  });}
  function confirmDose(id,doseId) {return mutate((s,now)=>{
    const p=resident(s,id),d=p.doses.find(x=>x.id===doseId);
    if(!d)throw Error('用药任务不存在');if(d.confirmedAt)return;
    if(Date.parse(d.dueAt)>now+30*60000)throw Error('尚未到本次确认时间');
    d.confirmedAt=iso(now);s.logs.push({at:iso(now),actor:'resident-'+id,residentId:Number(id),action:'确认服药：'+d.name});detect(s,now);
  });}
  function markPresented(ids,actor) {return mutate((s,now)=>{
    for(const e of s.events)if(ids.includes(e.id)&&(!e.notification.pagePresentedAt||(e.notification.presentedCycle||0)<(e.notification.cycle||1))){
      e.notification.pagePresentedAt=iso(now);e.notification.presentedCycle=e.notification.cycle||1;entry(e,'页面提醒已展示',actor,'仅页面展示，无外部通知',now);
    }
  });}
  return {KEY,STAFF,TYPES,STATES,METRICS,isOpen,init:()=>mutate((s,now)=>detect(s,now)),snapshot:async()=>clone(read()||seed(Date.now())),scan,simulate,transition,saveResident,addDose,confirmDose,markPresented,
    setSimulator:enabled=>mutate(s=>{s.simulator=!!enabled;})};
})();
