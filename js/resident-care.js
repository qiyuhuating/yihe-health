(() => {
  'use strict';
  const {h,time,mount,empty}=CareUI;
  let session;
  try {session=JSON.parse(localStorage.getItem('hm-login')||'null');}catch(_){session=null;}
  if(!session?.uid||!RESIDENT_DATA[session.uid]||!session.ts||Date.now()-session.ts>86400000){location.replace('login.html');return;}
  const id=session.uid;let initialized=false,revision=-1,busy=false,pendingDoseId=null,refreshRequest=0;
  const el=x=>document.getElementById(x);
  const recovery=CareUI.recovery(async()=>{await CareAPI.init();await refresh(true);});
  const fail=recovery.show;
  const failDose=e=>{el('doseError').textContent=e.message||'记录失败，请重试';el('doseError').hidden=false;};
  function tab(name){
    if(!['home','health','me'].includes(name))name='home';
    document.querySelectorAll('.tab-view').forEach(x=>x.classList.toggle('active',x.id==='tab-'+name));
    document.querySelectorAll('[data-tab]').forEach(x=>{const active=x.dataset.tab===name;x.classList.toggle('active',active);x.setAttribute('aria-current',active?'page':'false');});
    history.replaceState(null,'','#'+name);window.scrollTo(0,0);
  }
  document.querySelectorAll('[data-tab]').forEach(x=>x.addEventListener('click',()=>tab(x.dataset.tab)));
  function renderDoses(p){
    const now=Date.now(),today=new Date().toDateString();
    const doses=p.doses.filter(d=>new Date(d.dueAt).toDateString()===today||(!d.confirmedAt&&Date.parse(d.dueAt)<now));
    mount('todayDoses',doses.length?doses.map(d=>{
      const canConfirm=now>=Date.parse(d.dueAt)-30*60000;
      const disabled=!!d.confirmedAt||!canConfirm||pendingDoseId===d.id;
      const label=d.confirmedAt?'已记录服药':pendingDoseId===d.id?'正在记录…':canConfirm?'确认已服药':'尚未到确认时间';
      return `<div class="dose-row"><div><strong>${h(d.name)}</strong><p>计划 ${h(time(d.dueAt))} · ${h(d.source)}</p>${!d.confirmedAt&&!canConfirm?'<p class="data-note">可在计划时间前 30 分钟确认</p>':''}</div><button class="btn btn-primary" data-dose="${h(d.id)}" ${disabled?'disabled':''}>${label}</button></div>`;
    }).join(''):empty('暂无已核对的用药任务，请工作人员核对后登记；不要自行改变医嘱'));
  }
  function render(s){
    const p=s.residents[id];if(!p)throw Object.assign(Error('本机监护记录中找不到当前居民档案，请联系维护人员。'),{code:'DATA_INVALID'});
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
      mount('contactFields',[0,1,2].map(i=>`<fieldset><legend>联系人 ${i+1}</legend><label>姓名<input class="input" name="name${i}" maxlength="30" value="${h(p.contacts[i]?.name||'')}"></label><label>电话<input class="input" name="phone${i}" type="tel" value="${h(p.contacts[i]?.phone||'')}"></label></fieldset>`).join(''));initialized=true;
    }
  }
  async function refresh(force=false){const request=++refreshRequest;const s=await CareAPI.snapshot();if(request!==refreshRequest)return;if(force||s.revision!==revision||document.body.hasAttribute('data-care-unavailable')){render(s);revision=s.revision;}else renderDoses(s.residents[id]);recovery.restored();}
  async function action(button,fn,error=fail){if(busy)return;busy=true;button.disabled=true;try{await fn();await refresh(true);recovery.clear();showToast('已保存到本机记录');}catch(e){error(e);if(e.code)fail(e);}finally{busy=false;if(button.isConnected)button.disabled=false;}}
  el('todayDoses').addEventListener('click',event=>{const b=event.target.closest('[data-dose]');if(!b||b.disabled||busy)return;pendingDoseId=b.dataset.dose;b.disabled=true;el('doseError').hidden=true;renderDoses(window.App.currentPatient);action(b,()=>CareAPI.confirmDose(id,b.dataset.dose),failDose).finally(()=>{pendingDoseId=null;refresh().catch(fail);});});
  el('residentProfileForm').addEventListener('submit',event=>{event.preventDefault();action(event.submitter,()=>CareAPI.saveResident(id,{name:el('profileName').value,age:Number(el('profileAge').value)}));});
  el('contactForm').addEventListener('submit',event=>{event.preventDefault();const data=new FormData(event.target),contacts=[0,1,2].map(i=>({name:data.get('name'+i).trim(),phone:data.get('phone'+i).trim()})).filter(c=>c.name||c.phone);action(event.submitter,()=>CareAPI.saveResident(id,{contacts}));});
  el('themeToggleMe').addEventListener('click',()=>YiheTheme.set(YiheTheme.get()==='dark'?'light':'dark'));
  el('residentLogout').addEventListener('click',()=>{try{localStorage.removeItem('hm-login');location.replace('login.html');}catch(e){fail(e);}});
  window.addEventListener('storage',e=>{if(e.key===CareAPI.KEY)refresh(true).catch(fail);if(e.key==='hm-login'||e.key===null)location.reload();});
  window.addEventListener('care:change',()=>refresh().catch(fail));
  CareAPI.init().then(()=>refresh(true)).catch(fail);tab(location.hash.slice(1));
  setInterval(()=>CareAPI.scan().then(()=>refresh()).catch(fail),5000);
})();
