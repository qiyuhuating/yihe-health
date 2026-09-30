(() => {
  'use strict';
  const {h,time,mount,empty}=CareUI;
  const remote=CareAPI.mode==='http';
  let id=null,initialized=false,revision=-1,residentRevision=null,profileFormRevision=null,contactFormRevision=null,profileFormDirty=false,contactFormDirty=false,busy=false,pendingDoseId=null,refreshRequest=0;
  const el=x=>document.getElementById(x);
  function ensureConflictControls(form){
    if(form.querySelector('[data-form-conflict]'))return;
    const status=document.createElement('p'),reload=document.createElement('button');
    status.className='care-error';status.dataset.formConflict='';status.setAttribute('role','alert');status.hidden=true;status.textContent='记录已更新。放弃本表草稿后可载入最新值。';
    reload.className='btn';reload.type='button';reload.dataset.formReload='';reload.hidden=true;reload.textContent='放弃本表草稿并载入最新值';form.append(status,reload);
  }
  function formValues(form){return [...form.querySelectorAll('input,select,textarea')].map(field=>[field.name,field.type==='checkbox'?field.checked:field.value]);}
  async function reloadForm(form){
    const button=form.querySelector('[data-form-reload]');if(button)button.disabled=true;
    try{
      const s=await CareAPI.snapshot(),p=s.residents[id];if(!p)throw Error('找不到最新居民记录');
      render(s);revision=s.revision;
      if(form.id==='residentProfileForm'){
        el('profileName').value=p.name;el('profileAge').value=p.age;profileFormRevision=p.revision;profileFormDirty=false;
      }else{
        for(let i=0;i<3;i++){const contact=p.contacts[i]||{};el('contactFields').querySelector(`[name="name${i}"]`).value=contact.name||'';el('contactFields').querySelector(`[name="phone${i}"]`).value=contact.phone||'';}
        contactFormRevision=p.revision;contactFormDirty=false;
      }
      form.querySelector('[data-form-conflict]').hidden=true;form.querySelector('[data-form-reload]').hidden=true;
    }catch(e){fail(e);}finally{if(button?.isConnected)button.disabled=false;}
  }
  const recovery=CareUI.recovery(initialize);
  const fail=recovery.show;
  const failDose=e=>{el('doseError').textContent=e.message||'记录失败，请重试';el('doseError').hidden=false;};
  function tab(name,moveFocus=false){
    if(!['home','health','me'].includes(name))name='home';
    document.querySelectorAll('.tab-view').forEach(x=>x.classList.toggle('active',x.id==='tab-'+name));
    document.querySelectorAll('[data-tab]').forEach(x=>{const active=x.dataset.tab===name;x.classList.toggle('active',active);if(active)x.setAttribute('aria-current','page');else x.removeAttribute('aria-current');});
    history.replaceState(null,'','#'+name);window.scrollTo(0,0);
    if(moveFocus)el(name==='home'?'greetingName':name==='health'?'healthHeading':'meHeading').focus({preventScroll:true});
  }
  document.querySelectorAll('[data-tab]').forEach(x=>x.addEventListener('click',()=>tab(x.dataset.tab,true)));
  function renderDoses(p){
    const now=CareAPI.now(),today=new Date(now).toDateString();
    const doses=p.doses.filter(d=>new Date(d.dueAt).toDateString()===today||(!d.confirmedAt&&Date.parse(d.dueAt)<now));
    const markup=doses.length?doses.map(d=>{
      const canConfirm=now>=Date.parse(d.dueAt)-30*60000;
      const disabled=!!d.confirmedAt||!canConfirm||pendingDoseId===d.id;
      const label=d.confirmedAt?'已记录服药':pendingDoseId===d.id?'正在记录…':canConfirm?'确认已服药':'尚未到确认时间';
      return `<div class="dose-row"><div><strong>${h(d.name)}</strong><p>计划 ${h(time(d.dueAt))} · ${h(d.source)}</p>${!d.confirmedAt&&!canConfirm?'<p class="data-note">可在计划时间前 30 分钟确认</p>':''}</div><button class="btn btn-primary" data-dose="${h(d.id)}" ${disabled?'disabled':''}>${label}</button></div>`;
    }).join(''):empty('暂无已核对的用药任务，请工作人员核对后登记；不要自行改变医嘱');
    const container=el('todayDoses');
    if(container.dataset.renderedMarkup===markup)return;
    const focusedDose=container.contains(document.activeElement)?document.activeElement.closest('[data-dose]')?.dataset.dose:null;
    container.dataset.renderedMarkup=markup;
    container.innerHTML=markup;
    if(focusedDose){const next=[...container.querySelectorAll('[data-dose]')].find(button=>button.dataset.dose===focusedDose);if(next&&!next.disabled)next.focus({preventScroll:true});else{if(el('doseStatus').hidden){el('doseStatus').textContent=pendingDoseId?'正在记录您的用药反馈。':'用药列表已更新。';el('doseStatus').hidden=false;}el('doseStatus').focus({preventScroll:true});}}
  }
  function render(s){
    const p=s.residents[id];if(!p)throw Object.assign(Error('监护记录中找不到当前居民档案，请联系维护人员。'),{code:'DATA_INVALID'});residentRevision=p.revision;
    window.App={USER_ID:id,currentPatient:p};
    el('greetingName').textContent='你好，'+p.name;el('todayDate').textContent=new Date().toLocaleDateString('zh-CN');
    const events=s.events.filter(e=>e.residentId===id&&CareAPI.isOpen(e));
    const status=events.some(e=>e.severity==='danger')?'danger':events.length?'attention':'normal';
    el('statusTitle').textContent=status==='danger'?'有紧急情况需要关注':status==='attention'?'有 '+events.length+' 项情况需要关注':'当前没有待处理提醒';
    el('healthSummary').textContent=events.some(e=>e.owner)?'工作人员正在跟进，请留意来电。':'如有不适，请联系家人或工作人员。';
    document.querySelector('.home-health').dataset.status=status;
    el('dataUpdated').textContent=CareUI.device(p)+' · 指标采样于 '+time(p.updatedAt);
    mount('residentVitals',CareUI.vitals(p));
    renderDoses(p);
    mount('residentAlerts',events.length?events.map(e=>`<article class="resident-notice"><strong>${h(CareAPI.TYPES[e.type])} · ${h(CareAPI.STATES[e.state])}</strong><p>${h(e.detail)}</p><small>${h(time(e.createdAt))} · ${h(CareUI.staff(e.owner))}${e.sourceRecoveredAt?' · 信号恢复待复核':''}</small></article>`).join(''):empty('目前没有待处理提醒'));
    mount('homeContacts',CareUI.contacts(p));mount('residentTrend',CareUI.trend(p));mount('recordBody',CareUI.record(p));
    mount('doseHistory',p.doses.length?p.doses.slice().reverse().map(d=>`<p>${h(d.name)} · 计划 ${h(time(d.dueAt))} · ${d.confirmedAt?'已服药 '+h(time(d.confirmedAt)):'未确认'}</p>`).join(''):empty('暂无用药记录'));
    if(!initialized){
      el('profileName').value=p.name;el('profileAge').value=p.age;
      profileFormRevision=p.revision;contactFormRevision=p.revision;profileFormDirty=false;contactFormDirty=false;
      mount('contactFields',[0,1,2].map(i=>`<fieldset><legend>联系人 ${i+1}</legend><label>姓名<input class="input" name="name${i}" maxlength="30" value="${h(p.contacts[i]?.name||'')}"></label><label>电话<input class="input" name="phone${i}" type="tel" value="${h(p.contacts[i]?.phone||'')}"></label></fieldset>`).join(''));initialized=true;
    }else{
      if(!profileFormDirty){el('profileName').value=p.name;el('profileAge').value=p.age;profileFormRevision=p.revision;}
      if(!contactFormDirty){for(let i=0;i<3;i++){const contact=p.contacts[i]||{};el('contactFields').querySelector(`[name="name${i}"]`).value=contact.name||'';el('contactFields').querySelector(`[name="phone${i}"]`).value=contact.phone||'';}contactFormRevision=p.revision;}
    }
  }
  async function refresh(force=false,snapshotValue=null){const request=++refreshRequest,s=snapshotValue||await CareAPI.snapshot();if(request!==refreshRequest)return;if(force||s.revision!==revision||document.body.hasAttribute('data-care-unavailable')){render(s);revision=s.revision;}else renderDoses(s.residents[id]);recovery.restored();}
  async function action(button,fn,error=fail,onSaved,form=null,submittedValues=null){if(busy)return false;busy=true;button.disabled=true;const fields=form?[...form.querySelectorAll('input,select,textarea,button[type="submit"]')]:[],disabled=fields.map(field=>field.disabled);fields.forEach(field=>field.disabled=true);let saved=false,completed=false;try{await fn();saved=true;await refresh(true);if(onSaved)onSaved(submittedValues&&JSON.stringify(formValues(form))===JSON.stringify(submittedValues));recovery.clear();showToast(remote?'已保存':'已保存到本机记录');completed=true;}catch(e){if(saved)e.message='保存已完成，但最新记录读取失败。'+e.message;const handled=error(e)===true;if(!handled&&e.code)fail(e);}finally{busy=false;fields.forEach((field,index)=>{if(field.isConnected)field.disabled=disabled[index];});if(button.isConnected)button.disabled=false;}return completed;}
  el('todayDoses').addEventListener('click',event=>{const b=event.target.closest('[data-dose]');if(!b||b.disabled||busy)return;pendingDoseId=b.dataset.dose;b.disabled=true;el('doseError').hidden=true;el('doseStatus').hidden=true;renderDoses(window.App.currentPatient);action(b,()=>CareAPI.confirmDose(id,b.dataset.dose),failDose).then(()=>{if(el('doseError').hidden){el('doseStatus').textContent='已记录您的服药反馈。';el('doseStatus').hidden=false;el('doseStatus').focus({preventScroll:true});}}).finally(()=>{pendingDoseId=null;refresh().catch(fail);});});
  el('residentProfileForm').addEventListener('input',()=>{profileFormDirty=true;});
  el('contactForm').addEventListener('input',()=>{contactFormDirty=true;});
  for(const form of [el('residentProfileForm'),el('contactForm')]){ensureConflictControls(form);form.addEventListener('click',event=>{if(event.target.closest('[data-form-reload]'))reloadForm(form);});}
  el('residentProfileForm').addEventListener('submit',event=>{event.preventDefault();const form=event.currentTarget,expectedRevision=profileFormRevision,submitted=formValues(form),values={name:el('profileName').value,age:Number(el('profileAge').value)};action(event.submitter,()=>CareAPI.saveResident(id,values,undefined,expectedRevision),e=>{if(e.code!=='CONFLICT')return fail(e);form.querySelector('[data-form-conflict]').hidden=false;form.querySelector('[data-form-reload]').hidden=false;return true;},unchanged=>{if(unchanged){const latest=window.App.currentPatient;el('profileName').value=latest.name;el('profileAge').value=latest.age;profileFormDirty=false;profileFormRevision=latest.revision;}else profileFormDirty=true;},form,submitted);});
  el('contactForm').addEventListener('submit',event=>{event.preventDefault();const form=event.currentTarget,expectedRevision=contactFormRevision,data=new FormData(form),contacts=[0,1,2].map(i=>({name:data.get('name'+i).trim(),phone:data.get('phone'+i).trim()})).filter(c=>c.name||c.phone),submitted=formValues(form);action(event.submitter,()=>CareAPI.saveResident(id,{contacts},undefined,expectedRevision),e=>{if(e.code!=='CONFLICT')return fail(e);form.querySelector('[data-form-conflict]').hidden=false;form.querySelector('[data-form-reload]').hidden=false;return true;},unchanged=>{if(unchanged){const latest=window.App.currentPatient;for(let i=0;i<3;i++){const contact=latest.contacts[i]||{};el('contactFields').querySelector(`[name="name${i}"]`).value=contact.name||'';el('contactFields').querySelector(`[name="phone${i}"]`).value=contact.phone||'';}contactFormDirty=false;contactFormRevision=latest.revision;}else contactFormDirty=true;},form,submitted);});
  el('themeToggleMe').addEventListener('click',()=>YiheTheme.set(YiheTheme.get()==='dark'?'light':'dark'));
  el('residentLogout').addEventListener('click',async()=>{el('residentLogout').disabled=true;try{if(remote)await CareSession.logout();else localStorage.removeItem('hm-login');location.replace('login.html');}catch(e){fail(e);}finally{el('residentLogout').disabled=false;}});
  window.addEventListener('storage',e=>{if(remote)return;if(e.key===CareAPI.KEY)refresh(true).catch(fail);if(e.key==='hm-login'||e.key===null)location.reload();});
  window.addEventListener('care:change',()=>refresh().catch(fail));
  async function initialize(){
    let session;
    if(remote){
      try{session=await CareSession.requireRole('resident');}
      catch(e){if(e.code==='UNAUTHENTICATED'){location.replace('login.html');return;}throw e;}
      if(id!==session.residentId)initialized=false;
      id=session.residentId;
    }else{
      try{session=JSON.parse(localStorage.getItem('hm-login')||'null');}catch(_){session=null;}
      if(!session?.uid||!RESIDENT_DATA[session.uid]||!session.ts||Date.now()-session.ts>86400000){location.replace('login.html');return;}
      id=session.uid;
    }
    const initial=await CareAPI.init();await refresh(true,initial);
  }
  window.addEventListener('care:authenticated',()=>{
    if(CareSession.current?.residentId!==id){document.querySelector('main').hidden=true;el('residentProfileForm').reset();el('contactForm').reset();location.reload();return;}
    initialize().then(()=>{document.querySelector('main').inert=false;}).catch(fail);
  });
  recovery.loading();initialize().catch(fail);tab(location.hash.slice(1));
  CareUI.poll(async()=>{if(!id||busy||remote&&!CareSession.current)return;if(!remote)await CareAPI.scan();await refresh();},fail);
})();
