/* Resident application entry. DOM requirements are explicit; optional decoration
   is guarded at the call site instead of using a no-op Proxy. */
(() => {
  'use strict';
  const login=LoginModule.getLogin();
  if(!login?.uid){location.replace('login.html');return;}
  const app=window.App;app.USER_ID=login.uid;
  const el=id=>{const node=document.getElementById(id);if(!node)throw new Error(`缺少必需元素 #${id}`);return node;};
  const on=(id,fn,event='click')=>el(id).addEventListener(event,fn);
  const text=(id,value)=>el(id).textContent=value;
  const open=id=>el(id).hidden=false,close=id=>el(id).hidden=true;
  const profileKey=userKey(KEYS.PROFILE,app.USER_ID);
  let profile=loadJSON(profileKey,{}),active='home',request=0;
  // Old global profile has no reliable owner; retain it but never assign it to another user.
  let theme=loadJSON(KEYS.THEME,'light');
  function applyTheme(){document.body.classList.toggle('dark',theme==='dark');document.body.classList.toggle('light',theme!=='dark');el('themeToggleMe').setAttribute('aria-pressed',String(theme==='dark'));}
  applyTheme();on('themeToggleMe',()=>{theme=theme==='dark'?'light':'dark';saveJSON(KEYS.THEME,theme);applyTheme();});
  ChatModule(app);ReportsModule(app);CheckinModule(app);
  function tab(name){
    if(!['home','chat','health','me'].includes(name))return;
    active=name;document.body.classList.toggle('chat-open',name==='chat');
    document.querySelectorAll('.tab-view').forEach(x=>x.classList.toggle('active',x.id==='tab-'+name));
    document.querySelectorAll('[data-tab]').forEach(x=>{const selected=x.dataset.tab===name;x.classList.toggle('active',selected);x.setAttribute('aria-current',selected?'page':'false');});
    if(name==='chat')app.chat.renderContacts();
    if(name==='health'){app.reports.refreshTrendInline();app.checkin.refreshCheckinHistory();}
    history.replaceState(null,'','#'+name);window.scrollTo(0,0);
  }
  app.navigate=tab;
  document.querySelectorAll('[data-tab]').forEach(x=>x.addEventListener('click',()=>tab(x.dataset.tab)));
  on('chatExitBtn',()=>tab('home'));
  function render(p){
    app.currentPatient=p;
    const name=profile.name||p.name,age=profile.age||p.age,blood=profile.bloodType||p.medicalHistory.bloodType||'未记录';
    text('greetingName',`你好，${name}`);text('meName',name);text('meMeta',`${age}岁 · ${blood}`);text('profileNameDisplay',name);text('profileMeta',`${p.gender} · ${age}岁 · ${blood}`);
    text('profileTags',p.chronic||'暂无慢性病记录');
    text('healthSummary',p.status==='normal'?'模拟指标平稳':'模拟指标有异常');
    text('dataUpdated',`本机模拟 · ${new Date().toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'})} 更新`);
    for(const [id,key,unit] of [['heart','heartRate','bpm'],['oxygen','bloodOxygen','%'],['temp','temperature','°C']]){
      const valueId={heart:'heartRateVal',oxygen:'oxygenVal',temp:'tempVal'}[id];text(valueId,p[key].toFixed(1));
      const state=Metrics.metricStatus(key,p[key]),card=el(id+'Card');card.dataset.state=state;
      card.querySelector('.vital-status').textContent=Metrics.statusText(state);
    }
    text('bpVal',`${Math.round(p.systolic)}/${Math.round(p.diastolic)}`);
    const states=[Metrics.metricStatus('systolic',p.systolic),Metrics.metricStatus('diastolic',p.diastolic)];
    const state=states.includes('danger')?'danger':states.includes('warning')?'warning':'normal';el('bpCard').dataset.state=state;el('bpCard').querySelector('.vital-status').textContent=Metrics.statusText(state);
    text('locPlace',p.place||'未记录');text('locTime','模拟位置 · 未连接定位设备');
    // Static baseline is labelled; no fabricated activity accrues while the page is open.
    const steps=Math.round(2400+(p.mobility||0.5)*3600);text('stepsCount',steps.toLocaleString());text('stepsUpdateTime','演示样本');el('stepsRing').style.strokeDashoffset=263.9*(1-steps/8000);el('stepsBar').style.width=steps/80+'%';
    const meds=DemoDomain.medications(p);text('medCount',`${meds.length}种 · 仅展示档案`);
    el('medList').innerHTML=meds.length?meds.map(m=>`<li class="med-row"><span class="med-dot"></span><div>${escapeHtml(m.name)}<span class="med-time">${escapeHtml(m.times.length?m.times.join('、'):'服用时间未配置，请核对医嘱')}</span></div></li>`).join(''):'<li class="med-row">暂无用药档案</li>';
    el('recordBody').innerHTML=[['既往病史','pastHistory'],['过敏史','allergies'],['当前用药','medications'],['手术史','surgicalHistory'],['最近体检','lastCheckup']].map(([label,key])=>`<div class="rec-row"><span class="rec-lbl">${label}</span><span class="rec-val">${escapeHtml(p.medicalHistory[key]||'未记录')}</span></div>`).join('');
  }
  async function refresh(){const version=++request;try{const p=await API.getPatientDetail(app.USER_ID);if(version!==request||!p)return;render(p);app.reports.recordTrend(p);}catch(e){text('dataUpdated','读取本机数据失败，请刷新重试');console.error(e);}}
  on('recordToggle',()=>{const hidden=!el('recordBody').hidden;el('recordBody').hidden=hidden;el('recordToggle').setAttribute('aria-expanded',String(!hidden));});
  on('btnHomeSOS',()=>{
    try{DemoDomain.requestHelp(app.USER_ID);text('sosTitle','模拟求助已记录');text('sosDetail','没有通知任何人，也没有拨打电话。');text('sosPlace','当前位置为演示数据，请自行确认实际位置。');open('sosOverlay');Audit.log('模拟求助','仅记录本机事件，未发送通知');}
    catch(e){showToast(e.message);}
  });
  on('sosOk',()=>close('sosOverlay'));
  on('btnSaveProfile',()=>{
    const age=Number(el('editAge').value),height=Number(el('editHeight').value),weight=Number(el('editWeight').value);
    if(!el('editName').value.trim()||![age,height,weight].every(Number.isFinite)||!Number.isInteger(age)||age<1||age>120||height<50||height>250||weight<10||weight>300)return showToast('请填写姓名及合理的年龄、身高、体重');
    const next={name:el('editName').value.trim(),age,height,weight,bloodType:document.querySelector('[name="editBlood"]:checked')?.value||profile.bloodType};
    if(DemoDomain.write(profileKey,next)){profile=next;close('editOverlay');render(app.currentPatient);showToast('资料已保存到当前账号的本机记录');}
  });
  on('btnCancelEdit',()=>close('editOverlay'));
  // Reminder records carry their own content and due time, separate from medication instructions.
  function reminders(){
    const list=DemoDomain.reminders(app.USER_ID);el('taskList').innerHTML=list.length?list.map(t=>`<div class="task-row"><div><strong>${escapeHtml(t.text)}</strong><p>${escapeHtml(new Date(t.at).toLocaleString('zh-CN'))} · ${t.status==='completed'?'已提醒':'待提醒'}</p></div><button class="btn" data-task-edit="${t.id}" ${t.status==='completed'?'disabled':''}>编辑</button><button class="btn" data-task-delete="${t.id}">删除</button></div>`).join(''):'<p class="empty">还没有本机提醒。可以从聊天消息添加。</p>';
  }
  let editing=null;
  app.openReminder=(message='')=>{editing=null;el('taskText').value=message;const soon=new Date(Date.now()+3600000);el('taskAt').value=new Date(soon.getTime()-soon.getTimezoneOffset()*60000).toISOString().slice(0,16);reminders();open('medRemindOverlay');};
  on('saveMedTimes',()=>{try{if(Date.parse(el('taskAt').value)<=Date.now())return showToast('请选择未来的提醒时间');if(DemoDomain.saveReminder(app.USER_ID,{id:editing,text:el('taskText').value,at:new Date(el('taskAt').value).toISOString()})){editing=null;el('taskText').value='';reminders();showToast('已保存本机提醒；页面打开时才会提示');}}catch(e){showToast(e.message);}});
  on('cancelMedTimes',()=>close('medRemindOverlay'));
  on('taskList',event=>{
    const remove=event.target.closest('[data-task-delete]'),edit=event.target.closest('[data-task-edit]');
    if(remove){DemoDomain.removeReminder(app.USER_ID,remove.dataset.taskDelete);reminders();}
    if(edit){const task=DemoDomain.reminders(app.USER_ID).find(x=>x.id===edit.dataset.taskEdit);editing=task.id;el('taskText').value=task.text;const at=new Date(task.at);el('taskAt').value=new Date(at.getTime()-at.getTimezoneOffset()*60000).toISOString().slice(0,16);el('taskText').focus();}
  });
  setInterval(()=>{DemoDomain.dueReminders(app.USER_ID).forEach(t=>{showToast(`提醒：${t.text}`);AudioUtils.speak(t.text);});if(!el('medRemindOverlay').hidden)reminders();},15000);
  function emergency(){const value=loadJSON(userKey(KEYS.EMERGENCY,app.USER_ID),{});for(let i=1;i<=3;i++){el('emName'+i).value=value['n'+i]||'';el('emPhone'+i).value=value['p'+i]||'';}open('emergencyOverlay');}
  on('saveEmergency',()=>{const value={};for(let i=1;i<=3;i++){value['n'+i]=el('emName'+i).value.trim();value['p'+i]=el('emPhone'+i).value.trim();}if(DemoDomain.write(userKey(KEYS.EMERGENCY,app.USER_ID),value)){close('emergencyOverlay');showToast('仅保存联系人，没有发送通知');}});
  on('cancelEmergency',()=>close('emergencyOverlay'));
  const appointmentKey=userKey(KEYS.APPOINTMENTS,app.USER_ID);
  function appointments(){const list=loadJSON(appointmentKey,[]);el('apptList').innerHTML=list.length?list.map((x,i)=>`<div class="appt-item"><div class="appt-info">${escapeHtml(x.title)}<div>${escapeHtml(x.date)} ${escapeHtml(x.time)}</div></div><button class="btn" data-delete-appt="${i}">删除</button></div>`).join(''):'<p class="empty">没有本机行程，添加不会向机构预约。</p>';}
  on('addAppt',()=>{const title=el('apptTitle').value.trim(),date=el('apptDate').value;if(!title||!date)return showToast('请填写行程名称和日期');const list=loadJSON(appointmentKey,[]);list.push({title,date,time:el('apptTime').value||'09:00'});if(DemoDomain.write(appointmentKey,list)){appointments();el('apptTitle').value='';}});
  on('apptList',event=>{const button=event.target.closest('[data-delete-appt]');if(button){const list=loadJSON(appointmentKey,[]);list.splice(Number(button.dataset.deleteAppt),1);DemoDomain.write(appointmentKey,list);appointments();}});
  on('apptClose',()=>close('apptOverlay'));
  on('aiPersona',()=>el('customPromptGroup').hidden=el('aiPersona').value!=='custom','change');
  on('btnSaveAI',async()=>{
    const button=el('btnSaveAI');button.disabled=true;
    try {
      await AIChat.saveConfig(el('aiApiKey').value.trim(),el('aiPersona').value,el('aiCustomPrompt').value.trim(),el('aiPersistKey').checked,el('aiShareHealth').checked);
      close('aiOverlay');app.updateAIStatus(AIChat.isEnabled()?'online':'offline');
      showToast(AIChat.isEnabled()?'配置已保存，连接状态将在发送时确认':'已保存：使用本地规则回复');
    } catch(error) {showToast(error.message || '配置保存失败，请重试');}
    finally {button.disabled=false;}
  });
  on('btnCancelAI',()=>close('aiOverlay'));
  on('btnClearAI',async()=>{
    try {await AIChat.clear();el('aiApiKey').value='';el('aiShareHealth').checked=false;el('aiPersistKey').checked=false;showToast('已清除当前账号的密钥与AI配置');}
    catch(error){showToast(error.message || '清除失败，请重试');}
    finally {app.updateAIStatus(AIChat.isEnabled()?'online':'offline');}
  });
  const actions={
    editProfile(){const p=app.currentPatient;if(!p)return;for(const [id,key]of [['editName','name'],['editAge','age'],['editHeight','height'],['editWeight','weight']])el(id).value=profile[key]||p[key]||'';document.querySelectorAll('[name="editBlood"]').forEach(x=>x.checked=x.value===(profile.bloodType||p.medicalHistory.bloodType||'').replace('型血',''));open('editOverlay');},
    trends:()=>app.reports.openTrends(),healthReport:()=>tab('health'),medReminder:()=>app.openReminder(),emergency,
    appointments(){appointments();open('apptOverlay');},
    async aiSettings(){await AIChat.ready();const c=AIChat.getConfig();el('aiApiKey').value='';el('aiPersona').value=c.persona;el('aiCustomPrompt').value=c.customPrompt;el('aiPersistKey').checked=c.persistKey;el('aiShareHealth').checked=!!c.shareHealth;el('aiApiKey').placeholder=c.hasKey?'已配置；留空保留，清除请用下方按钮':'sk-…';el('customPromptGroup').hidden=c.persona!=='custom';open('aiOverlay');},
    admin:()=>location.href='管理端.html',switchUser:()=>LoginModule.logout()
  };
  document.querySelectorAll('.me-menu-item,[data-home-action]').forEach(node=>node.addEventListener('click',()=>actions[node.dataset.action||node.dataset.homeAction]?.()));
  const avatarKey='u'+app.USER_ID;
  on('avatarWrapMe',()=>el('avatarFileInput').click());
  on('avatarFileInput',()=>readImageFile(el('avatarFileInput').files[0],async image=>{app.avatars[avatarKey]=image;applyAvatarImg(el('avatarImgMe'),el('avatarFallbackMe'),image);await AvatarStore.save(avatarKey,image);}),'change');
  AvatarStore.load(avatarKey).then(image=>{app.avatars[avatarKey]=image||avatarSVG(login.name,'');applyAvatarImg(el('avatarImgMe'),el('avatarFallbackMe'),app.avatars[avatarKey]);});
  document.querySelectorAll('.overlay').forEach(x=>x.addEventListener('click',event=>{if(event.target===x)x.hidden=true;}));
  function calendar(){const c=YiheCalendar.current();text('todayDate',`${c.year}年${c.month}月${c.day}日`);if(!c.available)return;const t=c.content;document.querySelector('.home-term').textContent=c.term.name;document.querySelector('.home-season').textContent=t?`${t.phase} · ${t.hou[0]}`:'';document.querySelector('.home-note').textContent=t?.note||'顺时而居，从容照顾每一天。';document.querySelector('.home-roundel span').textContent=t?.season||'养';document.querySelector('.home-meta').textContent=`${c.lunar.gzYear}年 · ${c.lunar.monthCn}${c.lunar.dayCn} · ${c.lunar.ncWeek}`;el('seasonFoods').innerHTML=(t?.food||[]).map(name=>`<div class="food-item"><span class="food-leaf" aria-hidden="true">❧</span><strong>${escapeHtml(name)}</strong><span>当令食材 · 饮食文化</span></div>`).join('');text('foodHeading',c.term.name+' · 四时食养');}
  calendar();setInterval(calendar,60000);
  Promise.all([app.chat.loadChatHistory(),AIChat.ready()]).then(()=>{app.chat.renderContacts();app.updateAIStatus(AIChat.isEnabled()?'online':'offline');}).catch(()=>showToast('聊天存储暂不可用'));
  refresh();setInterval(refresh,10000);setInterval(API.tick,3000);tab(location.hash.slice(1)||'home');
})();
