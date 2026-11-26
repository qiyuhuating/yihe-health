const fs=require('fs'),path=require('path'),run=require('./browser-harness.cjs');
const routes=[['/operations/activity','activity/latest','name'],['/operations/vitals','vitals/latest','name'],['/operations/locations','position/select','name'],['/operations/life','residents/list','name'],['/operations/monitoring-profiles','monitoring-profiles/list','oldName'],['/operations/residents','residents/details','name'],['/operations/guardians','guardians/list','customerName'],['/operations/families','family/select','familyName'],['/operations/overview','notices/list','title'],['/operations/notices','notices/list','title']];
run(async({page,base,report,setRole,setSignedIn})=>{
 setRole('ADMIN');setSignedIn(true);const checks=[];let navigation=0;
 const check=(name,pass,details)=>checks.push({name,pass:Boolean(pass),details});
 const visit=async(route,width=1440,large=false)=>{await page.setViewportSize({width,height:1000});await page.goto(base+'/?large='+(large?1:0)+'&controls='+(++navigation)+'#'+route);await page.locator('.care-app').waitFor();await page.evaluate(()=>document.fonts.ready);await page.waitForTimeout(120);};
 const measure=group=>group.evaluate(root=>{
  const style=e=>{const s=getComputedStyle(e),r=e.getBoundingClientRect();return{border:[s.borderTopWidth,s.borderRightWidth,s.borderBottomWidth,s.borderLeftWidth].map(parseFloat),shadow:s.boxShadow,outline:s.outlineStyle,outlineWidth:parseFloat(s.outlineWidth),radius:s.borderRadius,color:s.color,background:s.backgroundColor,width:r.width,height:r.height,y:r.y,bottom:r.bottom}};
  return{outer:style(root),inner:Array.from(root.querySelectorAll('.el-input__wrapper,.el-input__inner,.el-input-group__append,.el-button')).map(style),button:style(root.querySelector('.el-button'))};
 });
 const innerClean=m=>m.inner.every(x=>x.border.every(v=>v===0)&&x.shadow==='none'&&x.outline==='none');
 for(const width of [320,390,768,1024,1440])for(const large of [false,true])for(const [route,endpoint,key]of routes){
  await visit(route,width,large);const prefix=width+(large?' large ':' standard ')+route;
  const group=page.locator('.el-input-group').first(),input=group.locator('input'),button=group.locator('button');const normal=await measure(group);
  check(prefix+': exactly one outer frame',normal.outer.border.every(v=>v===1)&&normal.outer.shadow==='none'&&normal.button.radius==='0px'&&innerClean(normal),normal);
  check(prefix+': search touch area at least 44 by 44',normal.button.width>=44&&normal.button.height>=44,normal.button);
  check(prefix+': no horizontal overflow',await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
  await button.hover();check(prefix+': hover retains single frame',innerClean(await measure(group)));
  await input.focus();const focus=await measure(group);check(prefix+': focus belongs only to the outer group',innerClean(focus)&&focus.outer.outline==='solid'&&focus.outer.outlineWidth>=2,focus);
  const arrangement=await group.evaluate(el=>{const row=el.closest('.row-search');if(!row)return true;const controls=[...row.querySelectorAll('.el-input-group,.row-search > .el-button,.row-search > .el-badge > .el-button,.toolbar-actions > .el-button,.record-summary')].map(n=>n.getBoundingClientRect()).filter(r=>r.width&&r.height);return controls.every((a,i)=>controls.slice(i+1).every(b=>Math.min(a.right,b.right)-Math.max(a.left,b.left)<=0.5||Math.min(a.bottom,b.bottom)-Math.max(a.top,b.top)<=0.5));});
  check(prefix+': wrapped toolbar controls do not overlap',arrangement);
  if(width===1440&&!large){
   await input.press('Tab');check(prefix+': keyboard reaches search action',await button.evaluate(b=>b===document.activeElement));
   const alignment=await page.locator('.row-search,.announcement-actions,.panel-heading').first().evaluate(el=>{
    const nodes=[...el.querySelectorAll('.el-input-group,.record-summary,.toolbar-actions > .el-button,.row-search > .el-button,.row-search > .el-badge > .el-button,.announcement-actions > .el-button')];return nodes.map(n=>({bottom:n.getBoundingClientRect().bottom}));
   });
   check(prefix+': toolbar bottom edges align',alignment.length<2||Math.max(...alignment.map(x=>x.bottom))-Math.min(...alignment.map(x=>x.bottom))<=1,alignment);
   let sent=[];const listener=request=>{if(request.url().endsWith('/api/v1/'+endpoint))sent.push(request.postDataJSON());};page.on('request',listener);
   await input.fill('验收查询');await input.press('Enter');await page.waitForTimeout(180);
   check(prefix+': Enter triggers exactly one query with the entered value',sent.length===1&&sent[0][key]==='验收查询',sent);
   sent=[];await button.click();await page.waitForTimeout(180);check(prefix+': clicking triggers exactly one query',sent.length===1,sent);page.off('request',listener);
  }
 }
 // Numeric controls and stacked dialog labels share one boundary as well.
 await visit('/operations/invitations',390,true);await page.getByRole('button',{name:'生成邀请码',exact:true}).click();const invite=page.getByRole('dialog',{name:'生成邀请码',exact:true});await invite.waitFor();
 const count=invite.locator('.el-input-number');
 check('Quantity field has no internal borders',await count.evaluate(el=>[...el.querySelectorAll('.el-input__inner,.el-input-number__increase,.el-input-number__decrease')].every(n=>{const s=getComputedStyle(n);return parseFloat(s.borderLeftWidth)===0&&parseFloat(s.borderRightWidth)===0&&s.boxShadow==='none'})));
 await count.getByRole('button',{name:/增加|Increase/}).click();check('Quantity increment still works',await count.locator('input').inputValue()==='2');
 await count.getByRole('button',{name:/减少|Decrease/}).click();check('Quantity decrement still works',await count.locator('input').inputValue()==='1');
 await invite.locator('.el-select__wrapper').click();await page.getByRole('option',{name:/李奶奶/}).click();await page.locator('.el-select-dropdown:visible').waitFor({state:'hidden'});check('Select remains usable',await invite.locator('.el-select').innerText().then(t=>t.includes('李奶奶')));
 check('Dialog labels sit above their controls without overlap',await invite.locator('.el-form-item:visible').evaluateAll(items=>items.every(item=>{const label=item.querySelector('.el-form-item__label'),content=item.querySelector('.el-form-item__content');return !label||!content||label.getBoundingClientRect().bottom<=content.getBoundingClientRect().top+1})));
 check('Large text dialog has no horizontal overflow',await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await page.screenshot({path:path.join(__dirname,'artifacts/screenshots','invite-dialog-large-390.png')});
 await invite.getByRole('button',{name:/取\s*消/}).click();
 // Error and disabled fields retain one frame, with error state visible.
 setRole('FAMILY');await page.goto(base+'/?large=0&profile=1#/family-portal/profile');await page.getByRole('button',{name:'修改资料',exact:true}).click();const profile=page.getByRole('dialog',{name:'编辑资料',exact:true});await profile.waitFor();
 const phone=profile.locator('.el-input.is-disabled');check('Disabled field has a single wrapper frame',await phone.evaluate(el=>{const wrapper=getComputedStyle(el.querySelector('.el-input__wrapper')),inner=getComputedStyle(el.querySelector('input'));return parseFloat(wrapper.borderLeftWidth)===1&&wrapper.boxShadow==='none'&&parseFloat(inner.borderLeftWidth)===0&&inner.boxShadow==='none'}));
 await profile.getByRole('textbox',{name:'姓名',exact:true}).fill('');await profile.getByRole('button',{name:/确\s*定/}).click();await profile.locator('.el-form-item__error').first().waitFor();
 check('Invalid field keeps a single red boundary',await profile.locator('.el-form-item.is-error .el-input__wrapper').first().evaluate(el=>{const s=getComputedStyle(el);return s.borderLeftColor==='rgb(164, 38, 44)'&&parseFloat(s.borderLeftWidth)===1&&s.boxShadow==='none'}));
 await page.screenshot({path:path.join(__dirname,'artifacts/screenshots','profile-error-390.png')});
 // Calendar and multi-line content use the same external focus treatment.
 setRole('ADMIN');await visit('/operations/residents',390,true);await page.getByRole('button',{name:'添加老人',exact:true}).click();const person=page.getByRole('dialog',{name:'老人添加',exact:true});await person.waitFor();
 const date=person.getByPlaceholder('选择日期');await date.click();await page.locator('.el-date-picker:visible').waitFor();await page.locator('.el-date-picker:visible .el-date-table td.available').first().click();check('Date selection still fills the field',/^\d{4}-\d{2}-\d{2}$/.test(await date.inputValue()));await person.getByRole('button',{name:/取\s*消/}).click();
 await visit('/operations/notices/new',390,true);const body=page.locator('textarea');await body.fill('清晰的照护提醒');await body.focus();
 check('Textarea retains one outer border and visible focus',await body.evaluate(el=>{const s=getComputedStyle(el);return parseFloat(s.borderLeftWidth)===1&&s.boxShadow==='none'&&s.outlineStyle==='solid'&&parseFloat(s.outlineWidth)>=2}));
 check('Large text editor has no horizontal overflow',await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await page.emulateMedia({reducedMotion:'reduce'});check('Control transitions respect reduced motion',await body.evaluate(el=>getComputedStyle(el).transitionDuration.split(',').every(d=>parseFloat(d)<=.01)));await page.emulateMedia({reducedMotion:'no-preference'});
 const ratio=await page.evaluate(()=>{const rgb=hex=>hex.match(/\w\w/g).map(x=>parseInt(x,16)/255).map(v=>v<=.04045?v/12.92:Math.pow((v+.055)/1.055,2.4));const lum=h=>rgb(h).reduce((s,v,i)=>s+v*[.2126,.7152,.0722][i],0);const contrast=(a,b)=>(Math.max(lum(a),lum(b))+.05)/(Math.min(lum(a),lum(b))+.05);return{label:contrast('53645c','ffffff'),action:contrast('235442','f1f4ed'),boundary:contrast('81917b','ffffff'),focus:contrast('235442','ffffff')};});
 check('Control text and visual boundaries meet contrast thresholds',ratio.label>=4.5&&ratio.action>=4.5&&ratio.boundary>=3&&ratio.focus>=3,ratio);
 check('No control JavaScript errors',report.pageErrors.length===0,report.pageErrors);check('No missing control assets',report.missingAssets.length===0,report.missingAssets);check('No unexpected control API calls',report.unexpectedApi.length===0,report.unexpectedApi);
 fs.writeFileSync(path.join(__dirname,'artifacts/controls-acceptance.json'),JSON.stringify({checks},null,2));console.log(JSON.stringify({checks:checks.length,failed:checks.filter(c=>!c.pass)},null,2));if(checks.some(c=>!c.pass))process.exitCode=1;
}).catch(e=>{console.error(e);process.exitCode=1});
