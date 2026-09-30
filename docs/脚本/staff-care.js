(() => {
  'use strict';
  const {h,time,mount,empty,staff}=CareUI,el=id=>document.getElementById(id);
  const titles={summary:'工作台',alerts:'预警',residents:'居民',map:'地图',more:'更多'};
  const remote=CareAPI.mode==='http';
  let state,view='summary',actor='staff-1',masked=false,page=1,eventId=null,eventRevision=null,activeResidentId=null,busy=false;
  let initialized=false,revision=-1,renderedMinute=-1,refreshRequest=0,doseIntent=null,suspendedDraft=null;const seen=new Set();
  function setBusy(value){
    busy=value;
    document.querySelectorAll('#eventBody input,#eventBody textarea,#eventBody select,#eventBody [data-event-action],#residentBody input,#residentBody select,#residentBody button[type="submit"],#currentStaff,[data-sim]').forEach(node=>node.disabled=value);
    if(remote)el('currentStaff').disabled=true;
    if(!CareAPI.STAFF.some(s=>s.id!==actor))document.querySelector('[data-event-action="escalated"]')?.setAttribute('disabled','');
  }
  const recovery=CareUI.recovery(async()=>{
    const note=el('eventNote')?.value||'',eventOpen=el('eventDialog').open;
    const residentOpen=el('residentDialog').open?activeResidentId:null;
    const initial=await CareAPI.init();await refresh(true,initial);
    if(eventOpen)openEvent(eventId,note);
    else if(residentOpen!==null){
      if(state.residents[residentOpen]){
        openResident(residentOpen);
      }else{el('residentDialog').close();activeResidentId=null;}
    }
  });
  const error=recovery.show;
  const options=value=>CareAPI.STAFF.map(s=>`<option value="${h(s.id)}" ${s.id===value?'selected':''}>${h(s.name)}</option>`).join('');
  const name=p=>displayName(p.name,masked);
  const openEvents=()=>state.events.filter(CareAPI.isOpen);
  function closeNav(restoreFocus=true){const panel=el('menuPanel'),trigger=el('hamburgerBtn'),wasOpen=trigger.getAttribute('aria-expanded')==='true',mobile=window.matchMedia('(max-width: 1023px)').matches;panel.classList.remove('open');panel.inert=mobile;el('menuOverlay').classList.remove('open');el('menuOverlay').inert=true;document.querySelector('.console-workspace').inert=false;trigger.setAttribute('aria-expanded','false');if(restoreFocus&&wasOpen)trigger.focus();}
  function navigate(next){
    if(!titles[next])next='summary';view=next;closeNav(false);
    document.querySelectorAll('.view').forEach(x=>x.classList.toggle('active',x.id==='view-'+next));
    document.querySelectorAll('.menu-item').forEach(x=>{x.classList.toggle('active',x.dataset.view===next);x.setAttribute('aria-current',x.dataset.view===next?'page':'false');});
    el('topbarTitle').textContent=titles[next];history.replaceState(null,'','#'+next);render();el('topbarTitle').focus();
  }
  function eventList(id,list){mount(id,list.length?list.slice().sort((a,b)=>Number(b.severity==='danger')-Number(a.severity==='danger')||Number(CareAPI.now()>Date.parse(b.dueAt))-Number(CareAPI.now()>Date.parse(a.dueAt))||Date.parse(a.createdAt)-Date.parse(b.createdAt)).map(e=>CareUI.eventCard(e,state,masked)).join(''):empty('当前范围没有事件'));}
  function render(){
    if(!state)return;
    const focusTarget=document.activeElement,focusView=focusTarget.closest?.('.view'),focused=focusTarget?.dataset?.todo?['todo',focusTarget.dataset.todo]:focusTarget?.dataset?.event?['event',focusTarget.dataset.event]:focusTarget?.dataset?.resident?['resident',focusTarget.dataset.resident]:null;
    const pending=openEvents(),mine=pending.filter(e=>e.owner===actor),unassigned=pending.filter(e=>!e.owner),overdue=pending.filter(e=>CareAPI.now()>Date.parse(e.dueAt));
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
  function renderResidents(announcePage=false){
    const pagination=el('residentPagination'),focusedPage=pagination.contains(document.activeElement)?document.activeElement.dataset.page:null;
    const query=el('residentSearch').value.trim(),list=Object.values(state.residents).filter(p=>p.name.includes(query));
    const pages=Math.ceil(list.length/12);page=Math.max(1,Math.min(page,pages||1));
    mount('residentList',list.length?list.slice((page-1)*12,page*12).map(p=>`<article class="panel"><h3>${h(name(p))} <small>${h(p.age)} 岁</small></h3>${context(p)}<button class="btn" data-resident="${p.id}">查看处置页</button></article>`).join(''):empty('没有符合条件的居民'));
    mount('residentPagination',pages>1?`<button class="btn" data-page="prev" ${page===1?'disabled':''}>上一页</button><span>${page} / ${pages}</span><button class="btn" data-page="next" ${page===pages?'disabled':''}>下一页</button>`:'');
    if(announcePage)el('residentPageStatus').textContent=list.length?`第 ${page} 页，共 ${pages} 页；本页显示 ${Math.min(12,Math.max(0,list.length-(page-1)*12))} 位，共 ${list.length} 位居民。`:'未找到符合条件的居民。';
    if(focusedPage){const next=pagination.querySelector(`button[data-page="${focusedPage}"]`);if(next&&!next.disabled)next.focus({preventScroll:true});else pagination.focus({preventScroll:true});}
  }
  function renderMap(){
    const safety=pendingSafety();
    mount('safetyMap','<div class="safety-fence"><span>'+(remote?'社区安全围栏 · 位置示意':'社区安全围栏 · 模拟')+'</span></div>'+Object.values(state.residents).map(p=>{
      const active=safety.some(e=>e.residentId===p.id),lost=CareAPI.now()-Date.parse(p.locationAt)>600000;
      return `<button class="map-resident ${active?'map-alert':p.risk?'map-risk':'map-normal'}" style="left:${Math.max(4,Math.min(96,p.pos.x/8))}%;top:${Math.max(10,Math.min(92,p.pos.y/6))}%" data-resident="${p.id}" aria-label="${h(name(p))}，${active?'安全事件待处理':p.risk?'高风险居民':'暂无安全事件'}${lost?'，最后定位已过期':''}">${h(name(p))}</button>`;
    }).join(''));
    eventList('safetyEvents',safety);
  }
  function pendingSafety(){return openEvents().filter(e=>e.type==='fence'||e.type==='offline');}
  function renderMore(){
    if(!remote){
      const chosen=el('simulationResident').value;
      mount('simulationResident',Object.values(state.residents).map(p=>`<option value="${p.id}" ${String(p.id)===chosen?'selected':''}>${h(name(p))}</option>`).join(''));
      el('simToggle').checked=state.simulator;
    }
    const records=[...state.logs,...state.events.flatMap(e=>e.history.map(t=>({...t,residentId:e.residentId})))].sort((a,b)=>Date.parse(b.at)-Date.parse(a.at));
    mount('operationLog',records.length?records.slice(0,80).map(r=>`<p>${h(time(r.at))} · ${h(state.residents[r.residentId]?name(state.residents[r.residentId]):'')} · ${h(staff(r.actor)==='未分配'?r.actor:staff(r.actor))} · ${h(r.action)} ${h(r.note||'')}</p>`).join(''):empty('暂无操作记录'));
    if(remote)return;
    const check=Audit.verify();el('auditCheck').textContent='本机记录一致性校验：'+(check.ok?'通过':'发现异常')+(check.retainedOnly?' · 仅校验保留记录':'')+'；不证明防篡改。';
    mount('legacyAudit',Audit.all().slice(-30).reverse().map(r=>`<p>${h(time(r.ts))} · ${h(r.action)} · ${h(r.detail||'')}</p>`).join('')||empty('没有旧记录'));
  }
  async function refresh(force=false,snapshotValue=null){
    const request=++refreshRequest,next=snapshotValue||await CareAPI.snapshot();if(request!==refreshRequest)return;
    if(remote){
      if(!CareAPI.STAFF.some(person=>person.id===actor))throw Object.assign(Error('当前工作人员不在服务返回的授权人员列表中。'),{code:'FORBIDDEN'});
      mount('currentStaff',options(actor));el('currentStaff').disabled=true;
    }
    const minute=Math.floor(CareAPI.now()/60000);
    if(force||next.revision!==revision||minute!==renderedMinute||document.body.hasAttribute('data-care-unavailable')){state=next;revision=next.revision;renderedMinute=minute;render();}
    recovery.restored();
    const fresh=state.events.filter(e=>CareAPI.isOpen(e)&&!seen.has(e.id+':'+e.notification.cycle)&&e.notification.cycle>(e.notification.acknowledgedCycle||0));
    if(fresh.length&&!el('residentDialog').open&&!el('eventDialog').open){
      el('newAlertText').textContent=fresh.length+' 条新增或变化的预警：'+fresh.map(e=>name(state.residents[e.residentId])+' · '+CareAPI.TYPES[e.type]).join('；');
      if(!el('dangerModal').open)el('dangerModal').showModal();
      await CareAPI.markPresented(fresh.map(e=>e.id),actor,fresh.map(e=>e.notification.cycle||1));
      fresh.forEach(e=>seen.add(e.id+':'+(e.notification.cycle||1)));
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
    mount('eventBody',`<h2 id="eventTitle">${h(name(p))} · ${h(CareAPI.TYPES[e.type])}</h2><ol class="disposition-steps" aria-label="处置进度">${steps.map(([key,label],i)=>`<li class="${i===stepIndex?'current':i<stepIndex?'is-complete':''}" ${i===stepIndex?'aria-current="step"':''}>${label}</li>`).join('')}</ol><section class="care-context-grid"><h3>事件与责任</h3><p class="event-detail">${h(e.detail)}</p><p>${h(CareAPI.STATES[e.state])} · 当前负责人 ${h(staff(e.owner))}</p><p>居民责任人 ${h(staff(p.responsible))}</p><p>发现 ${h(time(e.createdAt))} · 处理时限 ${h(time(e.dueAt))}</p>${CareUI.contacts(p,masked)}</section><section class="signal-status"><h3>信号状态</h3><p>${e.sourceRecoveredAt?'信号已恢复，仍需人工复核结案。':'异常信号仍待核实。'}</p></section>${CareAPI.isOpen(e)?`<label>处置记录<textarea class="input" id="eventNote" rows="3" maxlength="1000" placeholder="记录联系、核实与处理结果">${h(note)}</textarea></label><p class="care-error" id="eventError" role="alert" hidden></p><div class="event-actions">${controls}${e.state==='processing'&&e.owner===actor?`<details class="escalation-options"><summary>其他处置选项</summary><button type="button" class="btn" data-event-action="false_alarm">判为误报</button><label>升级接收人<select class="input" id="escalateTo">${options(CareAPI.STAFF.find(s=>s.id!==actor)?.id||'')}</select></label><button type="button" class="btn" data-event-action="escalated" ${CareAPI.STAFF.some(s=>s.id!==actor)?'':'disabled'}>升级给其他人员</button></details>`:''}</div>`:`<p class="data-note">此事件已结案，处置记录见下方时间线。</p>`}<h3>处理时间线</h3><ol class="event-timeline">${e.history.map(t=>`<li><strong>${h(t.action)}</strong><span>${h(time(t.at))} · ${h(t.actor==='system'?'系统':staff(t.actor))}</span><p>${h(t.note)}</p></li>`).join('')}</ol>`);
    if(el('residentDialog').open){el('residentDialog').close();activeResidentId=null;}if(el('dangerModal').open)el('dangerModal').close();
    if(!el('eventDialog').open)el('eventDialog').showModal();setBusy(busy);
  }
  function openResident(id,cleanFormId=null){
    const p=state.residents[id];if(!p)return;
    const events=openEvents().filter(e=>e.residentId===p.id);
    const drafts=activeResidentId===id?[...el('residentBody').querySelectorAll('form[data-pid]')].filter(form=>form.id!==cleanFormId&&form.dataset.dirty==='true').map(form=>({id:form.id,revision:form.dataset.revision,fields:[...form.querySelectorAll('input,textarea,select')].map(field=>({name:field.name,value:field.type==='checkbox'?field.checked:field.value}))})):[];
    const maintenanceOpen=activeResidentId===id&&el('residentBody').querySelector('.resident-maintenance')?.open||false;activeResidentId=id;
    mount('residentBody',`<header class="care-profile-header"><h2 id="residentTitle">${h(name(p))} · 居民处置</h2><p>${h(p.age)} 岁 · ${h(p.place||'居住信息待补充')}</p><p>当前未完成问题：${events.length} 项</p><p>责任人员：${h(staff(p.responsible))}</p>${CareUI.contacts(p,masked)}</header><section class="care-context-grid"><h3>当前问题与健康情况</h3>${context(p)}${events.length?events.map(e=>CareUI.eventCard(e,state,masked)).join(''):empty('目前没有未完成事件')}</section><button class="btn" data-resident="${p.id}">刷新详情</button><details class="resident-maintenance" ${maintenanceOpen?'open':''}><summary>维护资料与用药任务</summary><form id="responsibilityForm" data-pid="${p.id}" data-revision="${p.revision}"><label>居民责任人员<select class="input" name="responsible">${options(p.responsible)}</select></label><button class="btn" type="submit">保存责任人员</button></form><form id="staffContactForm" data-pid="${p.id}" data-revision="${p.revision}"><h3>登记首位联系人</h3><label>姓名<input class="input" name="name" value="${h(masked?'':p.contacts[0]?.name||'')}" required maxlength="30"></label><label>电话<input class="input" name="phone" type="tel" value="${h(p.contacts[0]?.phone||'')}" required></label><button class="btn" type="submit">保存联系人</button><p class="data-note">只更新首位联系人，其余联系人保留。</p></form><form id="doseForm" data-pid="${p.id}" data-revision="${p.revision}"><h3>登记已核对的用药任务</h3><label>用药名称<input class="input" name="name" maxlength="100" required></label><label>计划时间<input class="input" name="dueAt" type="datetime-local" required></label><label><input type="checkbox" name="verified" required> 已核对用药计划${remote?'':'（演示）'}</label><button class="btn" type="submit">登记任务</button></form></details><p id="residentError" class="care-error" role="alert" hidden></p><details><summary>病史与健康档案</summary>${CareUI.record(p)}</details><details><summary>指标趋势</summary>${CareUI.trend(p)}</details><details><summary>历史事件</summary>${state.events.filter(e=>e.residentId===p.id&&!CareAPI.isOpen(e)).map(e=>CareUI.eventCard(e,state,masked)).join('')||empty('暂无已完成事件')}</details>`);
    for(const formId of ['responsibilityForm','staffContactForm']){
      const form=el(formId),status=document.createElement('p'),reload=document.createElement('button');
      status.className='care-error';status.setAttribute('role','alert');status.hidden=true;status.textContent='记录已更新。放弃本表草稿后可载入最新值。';
      reload.className='btn';reload.type='button';reload.dataset.formReload='';reload.hidden=true;reload.textContent='放弃本表草稿并载入最新值';form.append(status,reload);
    }
    el('residentBody').querySelectorAll('form[data-pid]').forEach(form=>form.dataset.dirty='false');
    for(const draft of drafts){const target=el(draft.id);if(!target)continue;target.dataset.revision=draft.revision;target.dataset.dirty='true';for(const field of draft.fields){const input=target.querySelector(`[name="${CSS.escape(field.name)}"]`);if(input){if(input.type==='checkbox')input.checked=field.value;else input.value=field.value;}}}
    if(el('dangerModal').open)el('dangerModal').close();if(!el('residentDialog').open)el('residentDialog').showModal();setBusy(busy);
  }
  async function perform(button,fn,after,onError){
    if(busy)return;setBusy(true);button.disabled=true;
    let saved=false;
    try{const result=await fn();saved=true;const snapshotValue=result&&result.residents&&Array.isArray(result.events)?result:null;await refresh(true,snapshotValue);if(after)after();recovery.clear();showToast('操作已完成');}
    catch(e){if(onError&&onError(e))return;if(saved)e.message='操作已完成，但最新记录读取失败。'+e.message;const local=el('eventDialog').open?el('eventError'):el('residentDialog').open?el('residentError'):null;if(local){local.textContent=e.message;local.hidden=false;}else error(e);if(e.code)error(e);if(button.id==='simToggle'&&state)button.checked=state.simulator;}
    finally{setBusy(false);if(button.isConnected)button.disabled=false;}
  }
  function wire(){
    el('residentDialog').addEventListener('close',()=>{activeResidentId=null;});
    for(const type of ['input','change'])el('residentDialog').addEventListener(type,event=>{const form=event.target.closest('form[data-pid]');if(form)form.dataset.dirty='true';});
    document.querySelectorAll('[data-view]').forEach(b=>b.addEventListener('click',()=>navigate(b.dataset.view)));
    const navQuery=window.matchMedia('(max-width: 1023px)'),panel=el('menuPanel'),overlay=el('menuOverlay'),workspace=document.querySelector('.console-workspace');
    panel.inert=navQuery.matches;overlay.inert=true;workspace.inert=false;
    const onNavBreakpoint=event=>{if(!event.matches){closeNav(false);panel.inert=false;workspace.inert=false;}else{const isOpen=el('hamburgerBtn').getAttribute('aria-expanded')==='true';panel.inert=!isOpen;overlay.inert=!isOpen;workspace.inert=isOpen;}};
    if(navQuery.addEventListener)navQuery.addEventListener('change',onNavBreakpoint);else navQuery.addListener(onNavBreakpoint);
    el('hamburgerBtn').onclick=()=>{if(!navQuery.matches)return;panel.inert=false;panel.classList.add('open');overlay.inert=false;overlay.classList.add('open');workspace.inert=true;el('hamburgerBtn').setAttribute('aria-expanded','true');panel.querySelector('.menu-item')?.focus();};
    el('menuOverlay').onclick=()=>closeNav();el('menuClose').onclick=()=>closeNav();
    document.addEventListener('keydown',e=>{
      const open=navQuery.matches&&el('hamburgerBtn').getAttribute('aria-expanded')==='true';
      if(e.key==='Escape'&&open){closeNav();return;}
      if(e.key==='Tab'&&open){
        const focusable=[...panel.querySelectorAll('a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])')].filter(node=>!node.hidden),first=focusable[0],last=focusable[focusable.length-1];
        if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus();}
        else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus();}
      }
    });
    el('currentStaff').onchange=()=>{actor=el('currentStaff').value;render();};
    for(const id of ['todoFilter','eventFilter','typeFilter'])el(id).onchange=render;
    el('eventSearch').oninput=render;
    el('resetEventFilters').onclick=()=>{el('eventSearch').value='';el('eventFilter').value='open';el('typeFilter').value='';render();el('eventSearch').focus();};
    let residentSearchTimer;el('residentSearch').oninput=()=>{page=1;clearTimeout(residentSearchTimer);residentSearchTimer=setTimeout(()=>renderResidents(true),250);};
    el('refreshCare').onclick=()=>perform(el('refreshCare'),()=>CareAPI.scan());
    el('settingDarkMode').onchange=()=>YiheTheme.set(el('settingDarkMode').checked?'dark':'light');
    el('settingMask').onchange=()=>{
      const previous=masked;try{const settings=loadJSON('hm-settings',{});settings.mask=el('settingMask').checked;localStorage.setItem('hm-settings',JSON.stringify(settings));masked=settings.mask;render();}
      catch(e){el('settingMask').checked=previous;error(e);}
    };
    if(!remote)el('simToggle').onchange=()=>perform(el('simToggle'),()=>CareAPI.setSimulator(el('simToggle').checked));
    el('viewNewAlerts').onclick=()=>{el('dangerModal').close();navigate('alerts');};el('dismissNewAlerts').onclick=()=>el('dangerModal').close();
    document.addEventListener('click',event=>{
      const b=event.target.closest('button');if(!b)return;
      if(b.hasAttribute('data-form-reload')){const form=b.closest('form[data-pid]');if(form){b.disabled=true;CareAPI.snapshot().then(next=>{state=next;revision=next.revision;openResident(Number(form.dataset.pid),form.id);}).catch(error).finally(()=>{if(b.isConnected)b.disabled=false;});}return;}
      if(b.dataset.event)openEvent(b.dataset.event);
      if(b.dataset.resident)openResident(Number(b.dataset.resident));
      if(b.dataset.todo){el('todoFilter').value=b.dataset.todo;render();}
      if(b.dataset.page){page+=b.dataset.page==='next'?1:-1;renderResidents(true);}
      if(b.dataset.sim)perform(b,()=>CareAPI.simulate(Number(el('simulationResident').value),b.dataset.sim));
      if(b.dataset.eventAction)perform(b,()=>CareAPI.transition(eventId,b.dataset.eventAction,actor,el('eventNote')?.value||'',eventRevision,el('escalateTo')?.value),()=>openEvent(eventId,el('eventNote')?.value||''));
    });
    el('residentDialog').addEventListener('submit',event=>{
      const form=event.target;if(!form.dataset.pid)return;event.preventDefault();const id=Number(form.dataset.pid),data=new FormData(form);
      const expectedRevision=Number(form.dataset.revision);
      perform(event.submitter,async()=>{
        if(form.id==='responsibilityForm')return CareAPI.saveResident(id,{responsible:data.get('responsible')},actor,expectedRevision);
        if(form.id==='staffContactForm')return CareAPI.saveResident(id,{contactUpdates:[{index:0,value:{name:data.get('name'),phone:data.get('phone')}}]},actor,expectedRevision);
        if(form.id==='doseForm'){
          const name=data.get('name'),dueAt=new Date(data.get('dueAt')).toISOString(),intent=JSON.stringify([id,name,dueAt]);
          if(!doseIntent||doseIntent.intent!==intent)doseIntent={intent,key:CareTransport.newIdempotencyKey()};
          const submitted=doseIntent,result=await CareAPI.addDose(id,name,dueAt,actor,submitted.key);
          if(doseIntent===submitted)doseIntent=null;
          return result;
        }
      },()=>openResident(id,form.id),e=>{if(e.code!=='CONFLICT'||!['responsibilityForm','staffContactForm'].includes(form.id))return false;const status=form.querySelector('[role="alert"]'),reload=form.querySelector('[data-form-reload]');if(status)status.hidden=false;if(reload)reload.hidden=false;return true;});
    });
    el('exportEvents').onclick=async()=>{
      try{const maskAtStart=masked,s=await CareAPI.snapshot();const records=s.events.map(e=>({...e,residentName:displayName(s.residents[e.residentId].name,maskAtStart)}));const url=URL.createObjectURL(new Blob([JSON.stringify({exportedAt:new Date().toISOString(),simulated:!remote,events:records},null,2)],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download='yihe-events.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}catch(e){error(e);}
    };
    window.addEventListener('care:change',()=>refresh().catch(error));
    window.addEventListener('storage',event=>{
      if(event.key===CareAPI.KEY)refresh(true).catch(error);
      if(event.key===null)location.reload();
      if(event.key==='hm-settings'){masked=!!loadJSON('hm-settings',{}).mask;el('settingMask').checked=masked;render();if(el('eventDialog').open)openEvent(eventId,el('eventNote')?.value||'');}
    });
  }
  window.addEventListener('care:before-session-lock',()=>{
    suspendedDraft=suspendedDraft||{actor,eventOpen:el('eventDialog').open,residentOpen:el('residentDialog').open,residentId:activeResidentId};
  });
  AdminLogin.onReady(async(session)=>{
    if(initialized){
      if(remote&&session.staffId!==actor){mount('eventBody','');mount('residentBody','');location.reload();return;}
      el('staffApp').hidden=false;el('staffApp').inert=false;document.querySelector('main').inert=false;
      try{
        await refresh(true);
        if(suspendedDraft?.actor===actor){
          if(suspendedDraft.eventOpen)el('eventDialog').showModal();
          else if(suspendedDraft.residentOpen){activeResidentId=suspendedDraft.residentId;el('residentDialog').showModal();}
        }
        suspendedDraft=null;
      }catch(e){error(e);}
      return;
    }initialized=true;el('staffApp').hidden=false;el('staffApp').inert=false;
    if(remote){actor=session.staffId;el('accountDescription').textContent='当前账号：'+session.name;}
    recovery.loading();
    try{
      masked=!!loadJSON('hm-settings',{}).mask;el('settingMask').checked=masked;mount('currentStaff',options(actor));
      mount('typeFilter','<option value="">全部类型</option>'+Object.entries(CareAPI.TYPES).map(([key,label])=>`<option value="${h(key)}">${h(label)}</option>`).join(''));
      wire();const initial=await CareAPI.init();await refresh(true,initial);navigate(location.hash.slice(1)||'summary');
    }catch(e){error(e);}
    CareUI.poll(async()=>{if(busy||remote&&!CareSession.current)return;if(!remote)await CareAPI.scan();await refresh();},error);
  });
})();
