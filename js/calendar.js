/* One shared calendar, using the bundled solarLunar yearly term table.
   Term transitions have day precision in Asia/Shanghai, not astronomical minutes. */
window.YiheCalendar = (() => {
  const names = ['小寒','大寒','立春','雨水','惊蛰','春分','清明','谷雨','立夏','小满','芒种','夏至','小暑','大暑','立秋','处暑','白露','秋分','寒露','霜降','立冬','小雪','大雪','冬至'];
  function parts(date) {
    const p = Object.fromEntries(new Intl.DateTimeFormat('en-CA', { timeZone:'Asia/Shanghai', year:'numeric', month:'2-digit', day:'2-digit' }).formatToParts(date).map(x => [x.type,x.value]));
    return { year:+p.year, month:+p.month, day:+p.day };
  }
  function current(date = new Date()) {
    const {year,month,day} = parts(date), lib = window.solarLunar?.default || window.solarLunar;
    if (!lib || year < 1901 || year > 2099) return { available:false, year,month,day };
    const stamp = year * 10000 + month * 100 + day;
    let term = null;
    for (const y of [year - 1, year]) for (let i=0;i<24;i++) {
      const m=Math.floor(i/2)+1, d=lib.getTerm(y,i+1);
      if (y*10000+m*100+d<=stamp) term={ name:names[i], year:y, month:m, day:d };
    }
    const lunar = lib.solar2lunar(year,month,day);
    const terms = window.SolarTerms?.TERMS || [];
    return { available:true,year,month,day,lunar,term,content:terms.find(x=>x.name===term?.name) };
  }
  return { current, parts };
})();
