const fs = require('fs');
const path = require('path');
const run = require('./browser-harness.cjs');
const root = path.resolve(__dirname, '../..');
const images = path.resolve(__dirname, 'artifacts/screenshots');
const familyRoutes = ['/family-portal/overview', '/family-portal/vitals', '/family-portal/activity', '/family-portal/location', '/family-portal/profile', '/family-portal/notice?textId=1'];
const adminRoutes = ['/operations/overview', '/operations/vitals', '/operations/activity', '/operations/locations', '/operations/life', '/operations/monitoring-profiles', '/operations/residents', '/operations/guardians', '/operations/notices/edit?textId=1', '/operations/notices/new', '/operations/notices/detail?textId=1', '/operations/notices', '/operations/families', '/operations/comments', '/operations/invitations', '/operations/devices'];
const check = (report, name, pass, details) => report.checks.push({ name, pass: Boolean(pass), details });
run(async ({ page, context, base, report, setRole, setSignedIn, setMode }) => {
  let navigation=0;
  const visit = async (route, role) => {
    setRole(role); setSignedIn(true); setMode('normal');
    await page.goto(base + '/?review=' + (++navigation) + '#' + route);
    await page.locator('#app > *').waitFor();
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(130);
  };
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: width===390 ? 844 : 1000 });
    for (const [route, role] of [['/sign-in','FAMILY'],['/join-family','FAMILY'], ...familyRoutes.map(r=>[r,'FAMILY']), ...adminRoutes.map(r=>[r,'ADMIN'])]) {
      await visit(route, role);
      check(report, `${width} ${route}: requested route reached`, page.url().endsWith('#'+route),page.url());
      const layout = await page.evaluate(() => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth, title: document.title, logo: !!document.querySelector('img[src*="yihe-logo"]') }));
      check(report, `${width} ${route}: no horizontal overflow`, layout.scrollWidth <= width, layout);
      check(report, `${width} ${route}: brand image loaded`, await page.locator('img[src*="yihe-logo"]').evaluate(image => image.complete && image.naturalWidth > 0));
      check(report, `${width} ${route}: new page title`, (layout.title.startsWith('颐和') || layout.title.includes(' · 颐和 · ')));
      if (['/sign-in','/join-family','/family-portal/overview','/operations/overview'].includes(route)) await page.screenshot({ path: path.join(images, route.slice(1)+'-'+width+'.png'), fullPage: true });
    }
  }
  for (const width of [320,768]) {
    await page.setViewportSize({ width, height: 900 });
    await visit('/family-portal/overview', 'FAMILY');
    await page.getByRole('button', { name: '切换为更大字号', exact: true }).click();
    check(report, `${width} large text: no horizontal overflow`, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    check(report, `${width} brand returns to family home`, await page.getByRole('link', {name:'颐和，返回首页'}).getAttribute('href') === '#/family-portal/overview');
    await page.getByRole('link', { name:'颐和，返回首页' }).focus();
    check(report, `${width} brand keyboard focus`, await page.getByRole('link', { name:'颐和，返回首页' }).evaluate(link => link === document.activeElement));
    check(report, `${width} brand visible keyboard focus`, await page.getByRole('link', { name:'颐和，返回首页' }).evaluate(link => parseFloat(getComputedStyle(link).outlineWidth)>0));
    await page.getByRole('button', { name: '切换为标准字号', exact:true }).click();
  }
  // 图表库升级后的实际 Canvas 渲染和关闭清理。
  await page.setViewportSize({width:390,height:844});
  await visit('/family-portal/vitals', 'FAMILY');
  const chartButton = page.getByRole('button', {name:/详情|趋势/}).first();
  await chartButton.click();
  await page.waitForTimeout(300);
  check(report, 'Updated chart library renders Canvas', await page.locator('.el-dialog canvas').count() > 0);
  check(report, 'Chart retains accessible text description', await page.locator('.el-dialog [role="img"][aria-label]').count()>0);
  await page.locator('.el-dialog__headerbtn').click();
  await page.waitForFunction(() => document.querySelectorAll('.el-dialog canvas').length===0, null, {timeout:2000});
  check(report, 'Chart disposal on close', await page.locator('.el-dialog canvas').count() === 0);
  await visit('/family-portal/profile', 'FAMILY');
  const uploadResponse=page.waitForResponse(response => response.url().includes('/api/v1/image/saveImage'));
  const profileUpdate=page.waitForRequest(request => request.url().includes('/api/v1/family/updateTouPic'));
  const [uploaded,updated]=await Promise.all([uploadResponse,profileUpdate,page.locator('.avatar-uploader input[type="file"]').setInputFiles({name:'avatar.png',mimeType:'image/png',buffer:fs.readFileSync(path.join(root,'frontend/public/static/brand/yihe-apple-touch-180.png'))})]);
  check(report, 'Native avatar XHR sends CSRF header', uploaded.request().headers()['x-csrf-token']==='mock-csrf-token');
  check(report, 'Avatar update uses server generated filename', updated.postDataJSON().familyTouPic==='saved-image.png');
  // 篡改 localStorage 无法替代服务器登录状态。
  setSignedIn(false);
  await page.goto(base+'/?review='+(++navigation)+'#/operations/devices');
  await page.waitForURL(url => url.hash==='#/sign-in');
  check(report, 'Forged localStorage token cannot open administration', page.url().endsWith('#/sign-in'));
  setSignedIn(true); setRole('FAMILY');
  await page.goto(base+'/?review='+(++navigation)+'#/operations/devices');
  await page.waitForURL(url => url.hash==='#/family-portal/overview');
  check(report, 'Family navigation cannot enter administrator routes', page.url().endsWith('#/family-portal/overview'));
  // 登录必须刷新服务端会话，再进入家属页面。
  setSignedIn(false); setMode('login-success');
  await page.goto(base+'/?review='+(++navigation)+'#/sign-in');
  await page.getByLabel('手机号', {exact:true}).fill('13000000000');
  await page.getByLabel('密码', {exact:true}).fill('good-secret');
  await page.getByRole('button', {name:'登录',exact:true}).click();
  await page.waitForURL(url => url.hash==='#/family-portal/overview');
  check(report, 'Login refreshes server session before navigation', page.url().endsWith('#/family-portal/overview'));
  await page.getByRole('button', {name:'退出',exact:true}).click();
  await page.locator('.el-message-box').getByRole('button', {name:'退出',exact:true}).click();
  await page.waitForURL(url => url.hash==='#/sign-in');
  check(report, 'Logout calls server and clears local display cache', await page.evaluate(() => !localStorage.getItem('familyPhone')));
  const before = JSON.parse(fs.readFileSync(path.join(__dirname,'font-hashes-before.json')));
  const crypto = require('crypto');
  // The baseline was captured from Windows working-tree bytes. Git checks out
  // text licenses/manifests/CSS with platform-specific EOLs, so allow only an
  // exact LF <-> CRLF transformation, never arbitrary content changes.
  const checksum = buffer => crypto.createHash('sha256').update(buffer).digest('hex');
  const changedFonts = before.flatMap(item => {
    const bytes = fs.readFileSync(path.join(root,item.file));
    const actual = checksum(bytes);
    if (actual === item.sha256) return [];
    const textFile = /\.(?:css|json|md|txt)$/i.test(item.file);
    if (textFile) {
      const source = bytes.toString('utf8');
      // Do not normalize files with decoding errors or lone CR bytes.
      if (Buffer.from(source, 'utf8').equals(bytes) && !/(^|[^\r])\r(?!\n)/.test(source)) {
        const lf = source.replace(/\r\n/g, '\n');
        const crlf = lf.replace(/\n/g, '\r\n');
        if (checksum(Buffer.from(lf)) === item.sha256 || checksum(Buffer.from(crlf)) === item.sha256) return [];
      }
    }
    return [{ file: item.file, expected: item.sha256, actual }];
  });
  check(report, 'All existing font files unchanged', changedFonts.length === 0, { count:before.length, changedFonts });
  const fontCSS = fs.readFileSync(path.join(root, 'frontend/src/assets/css/fonts.css'), 'utf8');
  const referencedFonts = [...fontCSS.matchAll(/url\(["']?(\/static\/fonts\/[^"')]+)["']?\)/g)]
    .map(match => 'frontend/public' + match[1]);
  const missingReferences = referencedFonts.filter(file =>
    !fs.existsSync(path.join(root, file)) || !before.some(item => item.file === file));
  check(report, 'All CSS font assets exist and have an integrity baseline', missingReferences.length === 0, missingReferences);
  check(report, 'No browser JavaScript errors', report.pageErrors.length===0,report.pageErrors);
  check(report, 'No missing static resources', report.missingAssets.length===0,report.missingAssets);
  check(report, 'No unexpected API calls', report.unexpectedApi.length===0,report.unexpectedApi);
  fs.writeFileSync(path.join(__dirname,'artifacts/browser-acceptance.json'), JSON.stringify(report,null,2));
  const failed = report.checks.filter(item => !item.pass);
  console.log(JSON.stringify({ checks:report.checks.length, failed, screenshots:images },null,2));
  if (failed.length) process.exitCode=1;
}).catch(error => { console.error(error); process.exitCode=1; });
