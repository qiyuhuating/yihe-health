const fs=require('node:fs'),path=require('node:path'),run=require('./browser-harness.cjs');
run(async({page,base,report,setRole,setSignedIn})=>{
 const checks=[];let visit=0;
 const check=(name,pass,details)=>checks.push({name,pass:Boolean(pass),details});
 const sizes=[[360,560],[390,844],[760,640],[1024,600],[1366,768],[1440,900],[1920,1080],[2560,1440]];
 fs.mkdirSync(path.join(__dirname,'artifacts/screenshots'),{recursive:true});
 const geometry=()=>page.evaluate(()=>({viewport:[innerWidth,innerHeight],page:[document.documentElement.scrollWidth,document.documentElement.scrollHeight],
   panel:(()=>{const el=document.querySelector('.auth-panel');return el?{height:el.clientHeight,scroll:el.scrollHeight}:null})(),
   main:(()=>{const el=document.querySelector('.workspace-main');return el?{height:el.clientHeight,scroll:el.scrollHeight}:null})()}));
 setSignedIn(false);
 for(const [width,height] of sizes)for(const large of [false,true]){
  const name=`login ${width}x${height} ${large?'large':'standard'}`;
  await page.setViewportSize({width,height});await page.goto(base+`/?large=${large?1:0}&screen=${++visit}#/sign-in`);
  await page.locator('.auth-card').waitFor();await page.evaluate(()=>document.fonts.ready);
  const g=await geometry();check(name+' contains page',g.page[0]<=width&&g.page[1]<=height,g);
  check(name+' fits login content',g.panel.scroll<=g.panel.height+1,g);
  const phone=page.getByRole('textbox',{name:'手机号',exact:true});await phone.focus();await phone.press('Tab');
  check(name+' keyboard reaches password',await page.locator('#login-password').evaluate(el=>el===document.activeElement));
  await page.getByRole('button',{name:'登录',exact:true}).click();
  const errorG=await geometry();check(name+' validation stays inside page',errorG.page[1]<=height,errorG);
  if([[1366,768],[390,844],[360,560]].some(([w,h])=>w===width&&h===height))
    await page.screenshot({path:path.join(__dirname,`artifacts/screenshots/login-${width}-${height}-${large?'large':'standard'}.png`)});
 }
 setSignedIn(true);
 for(const role of ['ADMIN','FAMILY'])for(const [width,height] of sizes)for(const large of [false,true]){
  setRole(role);const route=role==='ADMIN'?'/operations/overview':'/family-portal/overview';
  await page.setViewportSize({width,height});await page.goto(base+`/?large=${large?1:0}&screen=${++visit}#${route}`);
  await page.locator('.workspace-main').waitFor();await page.evaluate(()=>document.fonts.ready);await page.waitForTimeout(100);
  const g=await geometry();const name=`${role} ${width}x${height} ${large?'large':'standard'}`;
  check(name+' contains page',g.page[0]<=width&&g.page[1]<=height,g);
  check(name+' uses available width',await page.locator('.workspace-content').evaluate(el=>{
    const p=el.parentElement,s=getComputedStyle(p);return Math.abs(el.clientWidth-(p.clientWidth-parseFloat(s.paddingLeft)-parseFloat(s.paddingRight)))<2;
  }));
  await page.locator('.workspace-main').evaluate(el=>el.scrollTop=el.scrollHeight);
  check(name+' last content remains reachable',await page.locator('.workspace-content').evaluate(el=>{
    const content=el.getBoundingClientRect(),main=el.parentElement.getBoundingClientRect();return content.bottom<=main.bottom+1;
  }));
  if(role==='FAMILY'&&width<=900){
    check(name+' navigation does not overlap content',await page.locator('.workspace-sidebar').evaluate(el=>{
      const nav=el.getBoundingClientRect(),main=document.querySelector('.workspace-main').getBoundingClientRect();return main.bottom<=nav.top+1;
    }));
  }
  if(width===1366||width===1920||width===390){
    await page.locator('.workspace-main').evaluate(el=>el.scrollTop=0);
    if(!large)await page.screenshot({path:path.join(__dirname,`artifacts/screenshots/${role.toLowerCase()}-${width}-${height}.png`)});
  }
 }
 check('no JavaScript errors',report.pageErrors.length===0,report.pageErrors);
 check('no missing assets',report.missingAssets.length===0,report.missingAssets);
 check('no unexpected APIs',report.unexpectedApi.length===0,report.unexpectedApi);
 fs.writeFileSync(path.join(__dirname,'artifacts/screen-acceptance.json'),JSON.stringify({checks},null,2));
 const failed=checks.filter(c=>!c.pass);console.log(JSON.stringify({checks:checks.length,failed},null,2));if(failed.length)process.exitCode=1;
}).catch(error=>{console.error(error);process.exitCode=1});
