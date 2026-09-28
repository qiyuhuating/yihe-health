(() => {
  'use strict';
  const {h,time,mount,empty,staff}=CareUI,el=id=>document.getElementById(id);
  const titles={summary:'工作台',alerts:'预警',residents:'居民',map:'地图',more:'更多'};
  let state,view='summary',actor='staff-1',masked=false,page=1,eventId=null,eventRevision=null,activeResidentId=null,busy=false;
  let initialized=false,revision=-1,renderedMinute=-1,refreshRequest=0;const seen=new Set();
  function setBusy(value){
    busy=value;
    document.querySelectorAll('#eventBody input,#eventBody textarea,#eventBody select,#eventBody [data-event-action],#residentBody input,#residentBody select,#residentBody button[type="submit"],#currentStaff,[data-sim]').forEach(node=>node.disabled=value);
  }
  const recovery=CareUI.recovery(async()=>{await CareAPI.init();await refresh(true);});
  const error=recovery.show;
  const options=value=>CareAPI.STAFF.map(s=>`<option value="${h(s.id)}" ${s.id===value?'selected':''}>${h(s.name)}</option>`).join('');
  const name=p=>displayName(p.name,masked);
  const openEvents=()=>state.events.filter(CareAPI.isOpen);
  function closeNav(){el('menuPanel').classList.remove('open');el('menuOverlay').classList.remove('open');el('hamburgerBtn').setAttribute('aria-expanded','false');}
  function navigate(next){
    if(!titles[next])next='summary';view=next;closeNav();
    document.querySelectorAll('.view').forEach(x=>x.classList.toggle('active',x.id==='view-'+next));
    document.querySelectorAll('.menu-item').forEach(x=>{x.classList.toggle('active',x.dataset.view===next);x.setAttribute('aria-current',x.dataset.view===next?'page':'false');});
    el('topbarTitle').textContent=titles[next];history.replaceState(null,'','#'+next);render();el('topbarTitle').focus();
  }
  function eventList(id,list){mount(id,list.length?list.slice().sort((a,b)=>Number(b.severity==='danger')-Number(a.severity==='danger')||Number(Date.now()>Date.parse(b.dueAt))-Number(Date.now()>Date.parse(a.dueAt))||Date.parse(a.createdAt)-Date.parse(b.createdAt)).map(e=>CareUI.eventCard(e,state,masked)).join(''):empty('当前范围没有事件'));}
  function render(){
    if(!state)return;
    const focusTarget=document.activeElement,focusView=focusTarget.closest?.('.view'),focused=focusTarget?.dataset?.todo?['todo',focusTarget.dataset.todo]:focusTarget?.dataset?.event?['event',focusTarget.dataset.event]:focusTarget?.dataset?.resident?['resident',focusTarget.dataset.resident]:null;
    const pending=openEvents(),mine=pending.filter(e=>e.owner===actor),unassigned=pending.filter(e=>!e.owner),overdue=pending.filter(e=>Date.now()>Date.parse(e.dueAt));
    const selected=el('todoFilter').value;
    mount('workCounts',[['all','待处理',pending.length],['mine','我的待办',mine.length],['unassigned','未分配',unassigned.length],['overdue','超时',overdue.length]].map(([key,label,count])=>`<button class="work-count" data-todo="${h(key)}" aria-pressed="${selected===key}"><strong>${count}</strong><span>${h(label)}</span></button>`).join(''));
    const filters={all:pending,mine,unassigned,overdue},todo=filters[selected];
    el('todoHeading').textContent=`${el('todoFilter').selectedOptions[0].textContent} · ${todo.length} 件`;
    eventList('todoEvents',todo);
    const filter=el('eventFilter').value,type=el('typeFilter').value,query=el('eventSearch').value.trim().toLocaleLowerCase();
    const matches=e=>(!query||state.residents[e.residentId].name.toLocaleLowerCase().includes(query)||e.detail.toLocaleLowerCase().includes(query));
    const events=state.events.filter(e=>(filter==='all'||filter==='open'&&CareAPI.isOpen(e)||e.state===filter)&&(!type||e.type===type)&&matches(e));
    el('eventHeading').textContent=`预警事件 · ${events.length} 件`;
    eventList('allEvents',events);
    if(view==='residents')renderResidents();if(view==='map')renderMap();if(view==='more')renderMore();
    if(focused&&!focusTarget.isConnected){const scope=focusView?.isConnected?focusView:document,node=scope.querySelector(`[data-${focused[0]}="${CSS.escape(String(focused[1]))}"]`);node?.focus();}
  }
  function context(p){
    const count=openEvents().filter(e=>e.residentId===p.id).length;
    return `<p>血氧 <strong>${CareUI.metric(p.bloodOxygen)}%</strong> · 心率 ${CareUI.metric(p.heartRate)} bpm</p><p class="data-note">采样 ${h(time(p.updatedAt))} · ${h(CareUI.device(p))}</p><p class="data-note">${h(CareUI.direction(p))}</p><p>未完成事件 ${count} · 责任人 ${h(staff(p.responsible))}</p>`;
  }
  function renderResidents(){
    const query=el('residentSearch').value.trim(),list=Object.values(state.residents).filter(p=>p.name.includes(query));
    const pages=Math.ceil(list.length/12);page=Math.max(1,Math.min(page,pages||1));
    mount('residentList',list.length?list.slice((page-1)*12,page*12).map(p=>`<article class="panel"><h3>${h(name(p))} <small>${h(p.age)} 岁</small></h3>${context(p)}<button class="btn" data-resident="${p.id}">查看处置页</button></article>`).join(''):empty('没有符合条件的居民'));
    mount('residentPagination',pages>1?`<button class="btn" data-page="prev" ${page===1?'disabled':''}>上一页</button><span>${page} / ${pages}</span><button class="btn" data-page="next" ${page===pages?'disabled':''}>下一页</button>`:'');
  }
  function renderMap(){
    const safety=pendingSafety();
    mount('safetyMap','<div class="safety-fence"><span>社区安全围栏 · 模拟</span></div>'+Object.values(state.residents).map(p=>{
      const active=safety.some(e=>e.residentId===p.id),lost=Date.now()-Date.parse(p.locationAt)>600000;
      return `<button class="map-resident ${active?'map-alert':p.risk?'map-risk':'map-normal'}" style="left:${Math.max(4,Math.min(96,p.pos.x/8))}%;top:${Math.max(10,Math.min(92,p.pos.y/6))}%" data-resident="${p.id}" aria-label="${h(name(p))}，${active?'安全事件待处理':p.risk?'高风险居民':'暂无安全事件'}${lost?'，最后定位已过期':''}">${h(name(p))}</button>`;
    }).join(''));
    eventList('safetyEvents',safety);
  }
  function pendingSafety(){return openEvents().filter(e=>e.type==='fence'||e.type==='offline');}
  function renderMore(){
    const chosen=el('simulationResident').value;
    mount('simulationResident',Object.values(state.residents).map(p=>`<option value="${p.id}" ${String(p.id)===chosen?'selected':''}>${h(name(p))}</option>`).join(''));
    el('simToggle').checked=state.simulator;
    const records=[...state.logs,...state.events.flatMap(e=>e.history.map(t=>({...t,residentId:e.residentId})))].sort((a,b)=>Date.parse(b.at)-Date.parse(a.at));
    mount('operationLog',records.length?records.slice(0,80).map(r=>`<p>${h(time(r.at))} · ${h(state.residents[r.residentId]?name(state.residents[r.residentId]):'')} · ${h(staff(r.actor)==='未分配'?r.actor:staff(r.actor))} · ${h(r.action)} ${h(r.note||'')}</p>`).join(''):empty('暂无操作记录'));
    const check=Audit.verify();el('auditCheck').textContent='本机记录一致性校验：'+(check.ok?'通过':'发现异常')+(check.retainedOnly?' · 仅校验保留记录':'')+'；不证明防篡改。';
    mount('legacyAudit',Audit.all().slice(-30).reverse().map(r=>`<p>${h(time(r.ts))} · ${h(r.action)} · ${h(r.detail||'')}</p>`).join('')||empty('没有旧记录'));
  }
  async function refresh(force=false){
    const request=++refreshRequest,next=await CareAPI.snapshot();if(request!==refreshRequest)return;
    const minute=Math.floor(Date.now()/60000);
    if(force||next.revision!==revision||minute!==renderedMinute||document.body.hasAttribute('data-care-unavailable')){state=next;revision=next.revision;renderedMinute=minute;render();}
    recovery.restored();
    const fresh=state.events.filter(e=>CareAPI.isOpen(e)&&!seen.has(e.id+':'+(e.notification.cycle||1))&&(e.state==='new'||(e.notification.cycle||1)>(e.notification.presentedCycle||0)));
    if(fresh.length&&!el('residentDialog').open&&!el('eventDialog').open){
      fresh.forEach(e=>seen.add(e.id+':'+(e.notification.cycle||1)));el('newAlertText').textContent=fresh.length+' 条新增或变化的预警：'+fresh.map(e=>name(state.residents[e.residentId])+' · '+CareAPI.TYPES[e.type]).join('；');
      if(!el('dangerModal').open)el('dangerModal').showModal();
      await CareAPI.markPresented(fresh.map(e=>e.id),actor);
    }
  }
  function openEvent(id,note=''){
    const e=state.events.find(x=>x.id===id);if(!e)return;
    eventId=id;eventRevision=e.revision;const p=state.residents[e.residentId];
    const action=(key,label)=>`<button type="button" class="btn ${['claim','processing','resolved'].includes(key)?'btn-primary':''}" data-event-action="${key}">${label}</button>`;
    let controls='';
    if(e.state==='new')controls=action('claim','接单');
    else if(e.owner===actor&&['claimed','escalated'].includes(e.state))controls=action('processing','开始处理');
    else if(e.owner===actor&&e.state==='processing')controls=action('resolved','完成处置');
    else if(CareAPI.isOpen(e))controls='<p>由 '+h(staff(e.owner))+' 跟进；当前人员不能代其提交处置。</p>';
    const steps=[['new','待接单'],['claimed',e.state==='escalated'?'已转交':'已接单'],['processing','处理中'],['terminal','已结案']],stage=e.state==='resolved'||e.state==='false_alarm'?'terminal':e.state==='escalated'?'claimed':e.state;
    const stepIndex=steps.findIndex(([key])=>key===stage);
    mount('eventBody',`<h2 id="eventTitle">${h(name(p))} · ${h(CareAPI.TYPES[e.type])}</h2><ol class="disposition-steps" aria-label="处置进度">${steps.map(([key,label],i)=>`<li class="${i===stepIndex?'current':i<stepIndex?'is-complete':''}" ${i===stepIndex?'aria-current="step"':''}>${label}</li>`).join('')}</ol><section class="care-context-grid"><h3>事件与责任</h3><p class="event-detail">${h(e.detail)}</p><p>${h(CareAPI.STATES[e.state])} · 当前负责人 ${h(staff(e.owner))}</p><p>居民责任人 ${h(staff(p.responsible))}</p><p>发现 ${h(time(e.createdAt))} · 处理时限 ${h(time(e.dueAt))}</p>${CareUI.contacts(p,masked)}</section><section class="signal-status"><h3>信号状态</h3><p>${e.sourceRecoveredAt?'信号已恢复，仍需人工复核结案。':'异常信号仍待核实。'}</p></section>${CareAPI.isOpen(e)?`<label>处置记录<textarea class="input" id="eventNote" rows="3" maxlength="1000" placeholder="记录联系、核实与处理结果">${h(note)}</textarea></label><p class="care-error" id="eventError" role="alert" hidden></p><div class="event-actions">${controls}${e.state==='processing'&&e.owner===actor?`<details class="escalation-options"><summary>其他处置选项</summary><button type="button" class="btn" data-event-action="false_alarm">判为误报</button><label>升级接收人<select class="input" id="escalateTo">${options(CareAPI.STAFF.find(s=>s.id!==actor).id)}</select></label><button type="button" class="btn" data-event-action="escalated">升级给其他人员</button></details>`:''}</div>`:`<p class="data-note">此事件已结案，处置记录见下方时间线。</p>`}<h3>处理时间线</h3><ol class="event-timeline">${e.history.map(t=>`<li><strong>${h(t.action)}</strong><span>${h(time(t.at))} · ${h(t.actor==='system'?'系统':staff(t.actor))}</span><p>${h(t.note)}</p></li>`).join('')}</ol>`);
    if(el('residentDialog').open){el('residentDialog').close();activeResidentId=null;}if(el('dangerModal').open)el('dangerModal').close();
    if(!el('eventDialog').open)el('eventDialog').showModal();setBusy(busy);
  }
  function openResident(id){
    const p=state.residents[id];if(!p)return;
    const events=openEvents().filter(e=>e.residentId===p.id);
    const maintenanceOpen=activeResidentId===id&&el('residentBody').querySelector('.resident-maintenance')?.open||false;activeResidentId=id;
    mount('residentBody',`<header class="care-profile-header"><h2 id="residentTitle">${h(name(p))} · 居民处置</h2><p>${h(p.age)} 岁 · ${h(p.place||'居住信息待补充')}</p><p>当前未完成问题：${events.length} 项</p><p>责任人员：${h(staff(p.responsible))}</p>${CareUI.contacts(p,masked)}</header><section class="care-context-grid"><h3>当前问题与健康情况</h3>${context(p)}${events.length?events.map(e=>CareUI.eventCard(e,state,masked)).join(''):empty('目前没有未完成事件')}</section><button class="btn" data-resident="${p.id}">刷新详情</button><details class="resident-maintenance" ${maintenanceOpen?'open':''}><summary>维护资料与用药任务</summary><form id="responsibilityForm" data-pid="${p.id}"><label>居民责任人员<select class="input" name="responsible">${options(p.responsible)}</select></label><button class="btn" type="submit">保存责任人员</button></form><form id="staffContactForm" data-pid="${p.id}"><h3>登记首位联系人</h3><label>姓名<input class="input" name="name" value="${h(masked?'':p.contacts[0]?.name||'')}" required maxlength="30"></label><label>电话<input class="input" name="phone" type="tel" value="${h(p.contacts[0]?.phone||'')}" required></label><button class="btn" type="submit">保存联系人</button><p class="data-note">只更新首位联系人，其余联系人保留。</p></form><form id="doseForm" data-pid="${p.id}"><h3>登记已核对的用药任务</h3><label>用药名称<input class="input" name="name" maxlength="100" required></label><label>计划时间<input class="input" name="dueAt" type="datetime-local" required></label><label><input type="checkbox" name="verified" required> 已核对用药计划（演示）</label><button class="btn" type="submit">登记任务</button></form></details><p id="residentError" class="care-error" role="alert" hidden></p><details><summary>病史与健康档案</summary>${CareUI.record(p)}</details><details><summary>指标趋势</summary>${CareUI.trend(p)}</details><details><summary>历史事件</summary>${state.events.filter(e=>e.residentId===p.id&&!CareAPI.isOpen(e)).map(e=>CareUI.eventCard(e,state,masked)).join('')||empty('暂无已完成事件')}</details>`);
    if(el('dangerModal').open)el('dangerModal').close();if(!el('residentDialog').open)el('residentDialog').showModal();setBusy(busy);
  }
  async function perform(button,fn){
    if(busy)return;setBusy(true);button.disabled=true;
    try{await fn();await refresh(true);recovery.clear();showToast('已保存');}
    catch(e){const local=el('eventDialog').open?el('eventError'):el('residentDialog').open?el('residentError'):null;if(local){local.textContent=e.message;local.hidden=false;}else error(e);if(e.code)error(e);if(button.id==='simToggle'&&state)button.checked=state.simulator;}
    finally{setBusy(false);if(button.isConnected)button.disabled=false;}
  }
  function wire(){
    el('residentDialog').addEventListener('close',()=>{activeResidentId=null;});
    document.querySelectorAll('[data-view]').forEach(b=>b.addEventListener('click',()=>navigate(b.dataset.view)));
    el('hamburgerBtn').onclick=()=>{el('menuPanel').classList.add('open');el('menuOverlay').classList.add('open');el('hamburgerBtn').setAttribute('aria-expanded','true');};
    el('menuOverlay').onclick=closeNav;el('menuClose').onclick=()=>{closeNav();el('hamburgerBtn').focus();};
    document.addEventListener('keydown',e=>{if(e.key==='Escape')closeNav();});
    el('currentStaff').onchange=()=>{actor=el('currentStaff').value;render();};
    for(const id of ['todoFilter','eventFilter','typeFilter'])el(id).onchange=render;
    el('eventSearch').oninput=render;
    el('resetEventFilters').onclick=()=>{el('eventSearch').value='';el('eventFilter').value='open';el('typeFilter').value='';render();el('eventSearch').focus();};
    el('residentSearch').oninput=()=>{page=1;renderResidents();};
    el('refreshCare').onclick=()=>perform(el('refreshCare'),()=>CareAPI.scan());
    el('settingDarkMode').onchange=()=>YiheTheme.set(el('settingDarkMode').checked?'dark':'light');
    el('settingMask').onchange=()=>{
      const previous=masked;try{const settings=loadJSON('hm-settings',{});settings.mask=el('settingMask').checked;localStorage.setItem('hm-settings',JSON.stringify(settings));masked=settings.mask;render();}
      catch(e){el('settingMask').checked=previous;error(e);}
    };
    el('simToggle').onchange=()=>perform(el('simToggle'),()=>CareAPI.setSimulator(el('simToggle').checked));
    el('viewNewAlerts').onclick=()=>{el('dangerModal').close();navigate('alerts');};el('dismissNewAlerts').onclick=()=>el('dangerModal').close();
    document.addEventListener('click',event=>{
      const b=event.target.closest('button');if(!b)return;
      if(b.dataset.event)openEvent(b.dataset.event);
      if(b.dataset.resident)openResident(Number(b.dataset.resident));
      if(b.dataset.todo){el('todoFilter').value=b.dataset.todo;render();}
      if(b.dataset.page){page+=b.dataset.page==='next'?1:-1;renderResidents();}
      if(b.dataset.sim)perform(b,()=>CareAPI.simulate(Number(el('simulationResident').value),b.dataset.sim));
      if(b.dataset.eventAction)perform(b,async()=>{const actionKey=b.dataset.eventAction,note=el('eventNote')?.value||'',target=el('escalateTo')?.value;await CareAPI.transition(eventId,actionKey,actor,note,eventRevision,target);await refresh(true);openEvent(eventId,note);});
    });
    el('residentDialog').addEventListener('submit',event=>{
      const form=event.target;if(!form.dataset.pid)return;event.preventDefault();const id=Number(form.dataset.pid),data=new FormData(form);
      perform(event.submitter,async()=>{
        if(form.id==='responsibilityForm')await CareAPI.saveResident(id,{responsible:data.get('responsible')},actor);
        if(form.id==='staffContactForm')await CareAPI.saveResident(id,{contacts:[{name:data.get('name'),phone:data.get('phone')},...state.residents[id].contacts.slice(1)]},actor);
        if(form.id==='doseForm')await CareAPI.addDose(id,data.get('name'),new Date(data.get('dueAt')).toISOString(),actor);
        await refresh(true);openResident(id);
      });
    });
    el('exportEvents').onclick=async()=>{
      try{const maskAtStart=masked,s=await CareAPI.snapshot();const records=s.events.map(e=>({...e,residentName:displayName(s.residents[e.residentId].name,maskAtStart)}));const url=URL.createObjectURL(new Blob([JSON.stringify({exportedAt:new Date().toISOString(),simulated:true,events:records},null,2)],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download='yihe-events.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}catch(e){error(e);}
    };
    window.addEventListener('care:change',()=>refresh().catch(error));
    window.addEventListener('storage',event=>{
      if(event.key===CareAPI.KEY)refresh(true).catch(error);
      if(event.key===null)location.reload();
      if(event.key==='hm-settings'){masked=!!loadJSON('hm-settings',{}).mask;el('settingMask').checked=masked;render();if(el('eventDialog').open)openEvent(eventId,el('eventNote')?.value||'');}
    });
  }
  AdminLogin.onReady(async()=>{
    if(initialized)return;initialized=true;el('staffApp').hidden=false;el('staffApp').inert=false;
    try{
      masked=!!loadJSON('hm-settings',{}).mask;el('settingMask').checked=masked;mount('currentStaff',options(actor));
      mount('typeFilter','<option value="">全部类型</option>'+Object.entries(CareAPI.TYPES).map(([key,label])=>`<option value="${h(key)}">${h(label)}</option>`).join(''));
      wire();await CareAPI.init();await refresh(true);navigate(location.hash.slice(1)||'summary');
    }catch(e){error(e);}
    setInterval(()=>CareAPI.scan().then(()=>refresh()).catch(error),5000);
  });
})();
