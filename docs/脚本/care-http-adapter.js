/* Adapter for the frontend contract. Metadata is shared; business methods never fall back to demo. */
window.CareHttpAdapter = (() => {
  'use strict';
  const {request,failure}=CareTransport;
  const object=value=>value!==null&&typeof value==='object'&&!Array.isArray(value);
  const text=value=>typeof value==='string'&&!!value.trim();
  const date=value=>typeof value==='string'&&/(Z|[+-]\d{2}:\d{2})$/.test(value)&&Number.isFinite(Date.parse(value));
  function create(metadata) {
    let staff=[],readSequence=0,serverEpoch=null,receivedAt=0;
    const now=()=>serverEpoch===null?NaN:serverEpoch+Math.max(0,performance.now()-receivedAt);
    function validate(value){
      const check=(ok,field)=>{if(!ok)throw failure('INVALID_RESPONSE','服务数据不完整（'+field+'），请联系维护人员。');};
      check(object(value)&&Number.isSafeInteger(value.revision)&&value.revision>=0,'版本');
      check(date(value.serverTime),'服务端时间');
      check(object(value.residents)&&Array.isArray(value.events)&&Array.isArray(value.logs)&&Array.isArray(value.staff),'集合');
      check(value.staff.every(s=>object(s)&&text(s.id)&&text(s.name))&&new Set(value.staff.map(s=>s.id)).size===value.staff.length,'工作人员');
      const knownStaff=id=>value.staff.some(s=>s.id===id);
      for(const [id,p] of Object.entries(value.residents)){
        check(object(p)&&Number.isSafeInteger(p.id)&&p.id>0&&String(p.id)===id&&text(p.name)&&Number.isInteger(p.age)&&p.age>0&&p.age<=120&&Number.isSafeInteger(p.revision)&&p.revision>=0,'居民');
        check(text(p.responsible)&&knownStaff(p.responsible)&&date(p.updatedAt)&&date(p.lastSeen)&&date(p.locationAt),'居民责任与采样时间');
        check(['online','offline','unknown'].includes(p.deviceState),'设备状态');
        if(p.metricStates!==undefined)check(object(p.metricStates)&&Object.values(p.metricStates).every(state=>['normal','warning','danger','unknown'].includes(state)),'指标状态');
        check(object(p.pos)&&Number.isFinite(p.pos.x)&&Number.isFinite(p.pos.y)&&typeof p.risk==='boolean','位置');
        check(Array.isArray(p.contacts)&&p.contacts.every(c=>object(c)&&text(c.name)&&typeof c.phone==='string'),'联系人');
        check(Array.isArray(p.doses)&&p.doses.every(d=>object(d)&&text(d.id)&&text(d.name)&&text(d.source)&&date(d.dueAt)&&(!d.confirmedAt||date(d.confirmedAt)))&&new Set(p.doses.map(d=>d.id)).size===p.doses.length,'用药任务');
        check(Array.isArray(p.trend)&&p.trend.every(t=>object(t)&&date(t.at)),'趋势');
      }
      const ids=new Set();
      for(const e of value.events){
        check(object(e)&&text(e.id)&&!ids.has(e.id),'事件编号');ids.add(e.id);
        check(Number.isSafeInteger(e.residentId)&&Object.hasOwn(value.residents,e.residentId),'事件居民');
        check(Object.hasOwn(metadata.TYPES,e.type)&&Object.hasOwn(metadata.STATES,e.state)&&['warning','danger'].includes(e.severity)&&text(e.detail),'事件状态');
        check(Number.isSafeInteger(e.revision)&&e.revision>0&&date(e.createdAt)&&date(e.dueAt)&&(!e.sourceRecoveredAt||date(e.sourceRecoveredAt)),'事件版本与时间');
        check(e.state==='new'?e.owner===null:knownStaff(e.owner),'事件负责人');
        check(Array.isArray(e.history)&&e.history.every(t=>object(t)&&date(t.at)&&text(t.action)&&text(t.actor)&&typeof t.note==='string'),'事件历史');
        check(object(e.notification)&&Number.isSafeInteger(e.notification.cycle)&&e.notification.cycle>=1&&['deliveredCycle','presentedCycle','acknowledgedCycle'].every(key=>Number.isSafeInteger(e.notification[key])&&e.notification[key]>=0&&e.notification[key]<=e.notification.cycle),'提醒轮次');
      }
      check(value.logs.every(l=>object(l)&&date(l.at)&&text(l.actor)&&text(l.action)&&Object.hasOwn(value.residents,l.residentId)),'操作记录');
      return value;
    }
    async function snapshot(){const sequence=++readSequence,value=validate(await request('/care/snapshot'));if(sequence===readSequence){staff=value.staff;serverEpoch=Date.parse(value.serverTime);receivedAt=performance.now();}return value;}
    return {
      TYPES:metadata.TYPES,STATES:metadata.STATES,METRICS:metadata.METRICS,isOpen:metadata.isOpen,
      get STAFF(){return staff;},now,mode:'http',init:snapshot,snapshot,
      scan:snapshot,
      simulate:()=>Promise.reject(failure('FEATURE_DISABLED','服务模式不支持演示信号。')),
      setSimulator:()=>Promise.reject(failure('FEATURE_DISABLED','服务模式不支持演示开关。')),
      transition:(id,action,_actor,note,revision,target)=>request('/events/'+encodeURIComponent(id)+'/transitions',{method:'POST',body:{action,note,revision,targetStaffId:target}}),
      saveResident:(id,patch,_actor,expectedRevision)=>request('/residents/'+encodeURIComponent(id),{method:'PATCH',body:{...patch,expectedRevision}}),
      addDose:(id,name,dueAt,_actor,idempotencyKey)=>request('/residents/'+encodeURIComponent(id)+'/doses',{method:'POST',body:{name,dueAt},idempotencyKey}),
      confirmDose:(id,doseId)=>request('/residents/'+encodeURIComponent(id)+'/doses/'+encodeURIComponent(doseId)+'/confirm',{method:'POST',body:{}}),
      markPresented:(ids,_actor,cycles)=>request('/events/presented',{method:'POST',body:{events:ids.map((id,index)=>({id,cycle:cycles[index]}))}})
    };
  }
  return {create};
})();
