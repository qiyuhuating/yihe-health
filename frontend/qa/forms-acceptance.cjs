const fs=require('fs'),path=require('path'),run=require('./browser-harness.cjs');
run(async({page,base,report,setRole,setSignedIn})=>{
 setRole('ADMIN');setSignedIn(true);const checks=[];const check=(name,pass,details)=>checks.push({name,pass:Boolean(pass),details});
 for(const width of [390,1440]){
  await page.setViewportSize({width,height:900});
  for(const [route,button,title]of[['/operations/residents','添加老人','老人添加'],['/operations/guardians','添加监护人','监护人添加'],['/operations/monitoring-profiles','添加手表','设备添加'],['/operations/families',/添加家属|添加监护人/,'家属添加']]){
   await page.goto(base+'/?forms='+Date.now()+'#'+route);await page.getByRole('button',{name:button,exact:true}).click();
   const dialog=page.getByRole('dialog',{name:title,exact:true});await dialog.waitFor();
   check(`${width} ${route}: active add form still opens`,await dialog.locator('input').count()>0);
   check(`${width} ${route}: no horizontal overflow`,await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
   const icons=await page.locator('i[class*="el-icon-"]:visible').evaluateAll(nodes=>nodes.map(n=>({icon:n.className,content:getComputedStyle(n,'::before').content,font:getComputedStyle(n).fontFamily})));
   check(`${width} ${route}: rendered icons retain glyph styles`,icons.every(i=>!['none','normal','""'].includes(i.content)&&i.font.includes('element-icons')),icons);
   await dialog.getByRole('button',{name:/取\s*消/}).click();await dialog.waitFor({state:'hidden'});
  }
 }
 const family={familyId:1,familyName:'小明',familyRelation:'子女',familySex:'男',familyPhone:'13000000000',familyAddress:'示例地址',name:'李奶奶',oldId:1};
 await page.route('**/api/v1/family/select',r=>r.fulfill({contentType:'application/json',body:JSON.stringify({returnCode:'1111',familyDtos:[family]})}));
 let payload;
 await page.route('**/api/v1/family/updateFamily',r=>{payload=r.request().postDataJSON();check('Family edit retains CSRF protection',r.request().headers()['x-csrf-token']==='mock-csrf-token');return r.fulfill({contentType:'application/json',body:JSON.stringify({returnCode:'1111'})});});
 await page.goto(base+'/?edit-family=1#/operations/families');await page.getByRole('button',{name:/编\s*辑/}).first().click();
 const active=page.locator('.el-dialog:visible');await active.waitFor();
 const nameInput=active.locator('input').first();check('Family edit loads the selected family row',await nameInput.inputValue()==='小明');await nameInput.fill('家属测试');
 await active.getByRole('button',{name:/确\s*定/}).click();await active.waitFor({state:'hidden'});
 check('Family edit sends family fields and identifier without obsolete customer defaults',payload?.familyId===1&&payload.familyName==='家属测试'&&payload.familyPhone==='13000000000'&&!Object.keys(payload).some(k=>k.startsWith('customer')),payload);
 check('No form JavaScript errors',report.pageErrors.length===0,report.pageErrors);check('No missing form assets',report.missingAssets.length===0,report.missingAssets);check('No unexpected form API calls',report.unexpectedApi.length===0,report.unexpectedApi);
 fs.writeFileSync(path.join(__dirname,'artifacts/forms-acceptance.json'),JSON.stringify({checks},null,2));console.log(JSON.stringify({checks:checks.length,failed:checks.filter(c=>!c.pass)},null,2));if(checks.some(c=>!c.pass))process.exitCode=1;
}).catch(e=>{console.error(e);process.exitCode=1});
