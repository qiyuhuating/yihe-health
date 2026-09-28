window.CareUI = (() => {
  const h = escapeHtml;
  const time = value => value&&Number.isFinite(new Date(value).getTime()) ? new Date(value).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}) : '未记录';
  const staff = id => CareAPI.STAFF.find(s=>s.id===id)?.name || '未分配';
  const mount = (id,markup) => {document.getElementById(id).innerHTML=markup;};
  const empty = text => '<p class="empty">'+h(text)+'</p>';
  const device = p => Date.now()-Date.parse(p.lastSeen)>300000?'离线 / 数据可能过期':'模拟设备在线';
  const metric = value => Number.isFinite(value)?String(value):'—';
  function recovery(retry) {
    const error=document.getElementById('careError'),button=document.createElement('button');
    button.id='retryCare';button.type='button';button.className='btn';button.textContent='重新读取';button.hidden=true;
    error.after(button);
    let blocked=false;
    function block(value) {
      blocked=value;document.body.toggleAttribute('data-care-unavailable',value);
      document.querySelectorAll('.staff-care .view,#eventBody,#residentBody,#residentProfileForm,#contactForm,#todayDoses').forEach(n=>n.inert=value);
      if(value){
        document.querySelectorAll('dialog[open]').forEach(n=>n.close());
        const title=document.getElementById('statusTitle');if(title)title.textContent='状态暂不可用，请重新读取';
      }
    }
    function show(e) {
      error.textContent=e.message||'读取失败，请重试';error.hidden=false;button.hidden=false;
      if(['DATA_INVALID','STORAGE_UNAVAILABLE','ENV_UNSUPPORTED'].includes(e.code))block(true);
    }
    function clear(){block(false);error.hidden=true;button.hidden=true;}
    button.onclick=async()=>{button.disabled=true;try{await retry();clear();}catch(e){show(e);}finally{button.disabled=false;}};
    return {show,clear,restored:()=>{if(blocked)clear();}};
  }
  function trend(p) {
    if(p.trend.length<2)return empty('采样不足两次，暂不能判断趋势');
    return '<div class="care-table-wrap"><table><caption>最近模拟采样</caption><thead><tr><th>时间</th><th>心率 bpm</th><th>血氧 %</th><th>血压 mmHg</th></tr></thead><tbody>'+p.trend.slice(-6).map(t=>`<tr><td>${h(time(t.at))}</td><td>${metric(t.heartRate)}</td><td>${metric(t.bloodOxygen)}</td><td>${metric(t.systolic)}/${metric(t.diastolic)}</td></tr>`).join('')+'</tbody></table></div>';
  }
  function direction(p) {
    if(p.trend.length<2)return '趋势：采样不足';
    const [a,b]=p.trend.slice(-2),delta=b.bloodOxygen-a.bloodOxygen;
    if(!Number.isFinite(a.bloodOxygen)||!Number.isFinite(b.bloodOxygen))return '趋势：数据不足，无法比较';
    return '血氧较上次 '+(delta>0?'↑ '+delta:delta<0?'↓ '+Math.abs(delta):'持平')+' · '+time(a.at)+' → '+time(b.at);
  }
  function contacts(p,masked=false) {
    if(!p.contacts.length)return empty('尚未登记紧急联系人，请到“我的”或联系工作人员补充');
    return p.contacts.map(c=>`<div class="contact-row"><span>${h(displayName(c.name,masked))}</span>${/^\+?[\d -]{5,22}$/.test(c.phone)?`<a class="btn" href="tel:${h(c.phone.replace(/[ -]/g,''))}">拨打 ${h(c.phone)}</a>`:'<span>号码需要核对</span>'}</div>`).join('');
  }
  function vitals(p) {
    const stale=Date.now()-Date.parse(p.lastSeen)>300000;
    return [['heartRate','心率',metric(p.heartRate),'bpm'],['bloodOxygen','血氧',metric(p.bloodOxygen),'%'],['temperature','体温',metric(p.temperature),'°C'],['systolic','血压',metric(p.systolic)+'/'+metric(p.diastolic),'mmHg']].map(([key,label,value,unit])=>{
      const levels=key==='systolic'?[Metrics.metricStatus(key,p.systolic),Metrics.metricStatus('diastolic',p.diastolic)]:[Metrics.metricStatus(key,p[key])];
      const status=stale?'stale':levels.includes('danger')?'danger':levels.includes('unknown')?'unknown':levels.includes('warning')?'warning':'normal';
      const text={stale:'离线前采样',unknown:'数据不足，请核实采样',danger:'需及时核实',warning:'需关注',normal:'演示范围内'}[status];
      return `<div class="vital-card" data-status="${status}"><div class="vital-label">${h(label)} <span class="vital-unit">${h(unit)}</span></div><div class="vital-number">${h(value)}</div><span class="vital-status">${text}</span></div>`;
    }).join('');
  }
  function record(p) {
    return [['慢病',p.chronic],['既往病史',p.medicalHistory?.pastHistory],['过敏史',p.medicalHistory?.allergies],['档案用药',p.medicalHistory?.medications]].map(([label,value])=>`<div class="rec-row"><strong>${h(label)}</strong><span>${h(value||'未记录')}</span></div>`).join('');
  }
  function eventCard(e,s,masked=false) {
    const p=s.residents[e.residentId];
    const open=CareAPI.isOpen(e),minutes=Math.ceil((Date.parse(e.dueAt)-Date.now())/60000);
    const deadline=open?(minutes<=0?'已超时 '+Math.abs(minutes)+' 分钟':'剩余 '+minutes+' 分钟'):'已归档';
    return `<article class="care-event" data-state="${h(e.state)}" data-severity="${h(e.severity)}"><div class="event-identity"><div class="event-tags"><span class="severity-tag">${e.severity==='danger'?'紧急':'关注'}</span><span class="badge">${h(CareAPI.STATES[e.state])}</span></div><h3>${h(displayName(p.name,masked))}</h3><span class="event-type">${h(CareAPI.TYPES[e.type])}</span></div><div class="event-detail"><p>${h(e.detail)}</p><div class="event-meta">发现 ${h(time(e.createdAt))}</div><div class="signal-note">${e.sourceRecoveredAt?'信号恢复 · 仍需人工复核':'待核实的异常信号'}</div></div><div class="event-owner"><span class="event-meta">当前负责人</span><strong>${h(staff(e.owner))}</strong><span class="event-deadline ${open&&minutes<=0?'is-overdue':''}" title="处理时限 ${h(time(e.dueAt))}">${h(deadline)}</span><span class="event-meta">责任人 ${h(staff(p.responsible))}</span></div><div class="event-action"><button class="btn" data-event="${h(e.id)}" aria-label="${h(displayName(p.name,masked))}，${h(CareAPI.TYPES[e.type])}，${open?'进入处置':'查看留痕'}">${open?'进入处置':'查看留痕'} <span aria-hidden="true">→</span></button></div></article>`;
  }
  return {h,time,staff,mount,empty,device,metric,recovery,trend,direction,contacts,vitals,record,eventCard};
})();
