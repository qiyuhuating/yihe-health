window.ReportMath = {
  range(kind, now = new Date()) {
    const p=YiheCalendar.parts(now), today=new Date(Date.UTC(p.year,p.month-1,p.day));
    const start=kind==='month'?Date.UTC(p.year,p.month-1,1)-28800000:today.getTime()-((today.getUTCDay()+6)%7)*86400000-28800000;
    return {start,end:today.getTime()+86400000-28800000,timeZone:'Asia/Shanghai'};
  },
  aggregate(samples,kind,now=new Date()) {
    const range=this.range(kind,now);
    const data=samples.filter(x=>Date.parse(x.at)>=range.start&&Date.parse(x.at)<range.end&&Date.parse(x.at)<=now.getTime());
    const days=new Set(data.map(x=>new Intl.DateTimeFormat('en-CA',{timeZone:range.timeZone,year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date(x.at))));
    const average=field=>{const v=data.map(x=>x[field]).filter(Number.isFinite);return v.length?v.reduce((a,b)=>a+b,0)/v.length:null;};
    return {...range,data,count:data.length,days:days.size,missingDays:Math.max(0,Math.round((range.end-range.start)/86400000)-days.size),hr:average('hr'),sys:average('sys')};
  }
};
window.ReportsModule=function(app){
  let download='';const key=()=>`yihe-v2-trends-${app.USER_ID}`,samples=()=>loadJSON(key(),[]);
  const date=at=>new Date(at).toLocaleDateString('zh-CN',{timeZone:'Asia/Shanghai'});
  function trends(target){
    const root=document.getElementById(target);if(!root)return;const data=samples().slice(-12);
    root.innerHTML=data.length?'<p class="data-note">本机模拟采样 · 最近12次 · 北京时间</p><div class="table-scroll"><table><caption class="sr-only">模拟健康记录</caption><thead><tr><th>采样时间</th><th>心率</th><th>血氧</th><th>血压</th></tr></thead><tbody>'+data.map(x=>`<tr><td>${escapeHtml(new Date(x.at).toLocaleString('zh-CN',{timeZone:'Asia/Shanghai'}))}</td><td>${x.hr.toFixed(1)} bpm</td><td>${x.bo.toFixed(1)}%</td><td>${x.sys.toFixed(0)}/${x.dia.toFixed(0)}</td></tr>`).join('')+'</tbody></table></div>':'<p class="empty">尚无本机采样记录。</p>';
  }
  function generate(kind){
    const r=ReportMath.aggregate(samples(),kind);
    const lines=[`${kind==='week'?'本周':'本月'}健康记录摘要 — ${app.currentPatient?.name||'演示居民'}`,'演示数据 · 仅本机记录 · 不作为诊疗依据',`范围：${date(r.start)} 至 ${date(r.end-1)}（北京时间，截至当前）`,`有效样本：${r.count}条，覆盖${r.days}天，缺失${r.missingDays}天`,r.count?`平均心率：${r.hr?.toFixed(1)??'--'} bpm；平均收缩压：${r.sys?.toFixed(1)??'--'} mmHg`:'此时间范围没有有效记录。','统计不补全缺失数据；旧版无年份的记录不参与统计。'];
    download=lines.join('\n');document.getElementById('reportContentTab').innerHTML='<article class="report-card">'+lines.map((l,i)=>i?`<p>${escapeHtml(l)}</p>`:`<h3>${escapeHtml(l)}</h3>`).join('')+'</article>';
  }
  document.getElementById('genWeekTab').addEventListener('click',()=>generate('week'));
  document.getElementById('genMonthTab').addEventListener('click',()=>generate('month'));
  document.getElementById('trendsClose').addEventListener('click',()=>document.getElementById('trendsOverlay').hidden=true);
  document.getElementById('btnDownloadReport').addEventListener('click',()=>{
    if(!download)return showToast('请先生成本周或本月摘要');
    const url=URL.createObjectURL(new Blob(['\ufeff'+download],{type:'text/plain;charset=utf-8'})),a=document.createElement('a');a.href=url;a.download='演示健康摘要.txt';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
  });
  app.reports={recordTrend(p){
    if(!p)return;const data=samples(),now=Date.now(),bucket=Math.floor(now/3600000);
    const s={at:new Date(now).toISOString(),bucket,source:'demo',hr:p.heartRate,bo:p.bloodOxygen,sys:p.systolic,dia:p.diastolic,bs:p.bloodSugar};
    if(data.at(-1)?.bucket===bucket)data[data.length-1]=s;else data.push(s);
    saveJSON(key(),data.filter(x=>Date.parse(x.at)>now-90*86400000));
  },openTrends(){trends('trendBody');document.getElementById('trendsOverlay').hidden=false;},refreshTrendInline(){trends('trendBodyInline');}};
};
