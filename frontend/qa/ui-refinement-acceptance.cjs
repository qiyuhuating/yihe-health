const fs = require('node:fs')
const path = require('node:path')
const run = require('./browser-harness.cjs')

run(async ({ page, context, base, report, setRole, setSignedIn }) => {
  const checks = []
  const check = (name, pass, details) => checks.push({ name, pass: Boolean(pass), details })
  const visit = async route => {
    await page.goto(base + '/?ui=' + Date.now() + '#' + route)
    await page.locator('.care-app').waitFor()
  }
  const opened = () => page.locator('#admin-navigation > .el-sub-menu').evaluateAll(nodes =>
    nodes.map((node, i) => node.classList.contains('is-opened') ? ['health', 'records', 'service'][i] : null).filter(Boolean))
  setRole('ADMIN')
  await context.route('**/api/v1/care/incidents', route => route.fulfill({ json: {returnCode:'1111', items: [], total: 0, page: 1, pageSize: 20, duty: 'supervisor', actorId: 1 } }))
  await context.route('**/api/v1/care/diagnostics', route => route.fulfill({ status: 403, json: {returnCode:'0000',code:'INVALID_REQUEST',message:'请求未完成',} }))
  const elder = { oldId: 1, name: '李奶奶', bothdate: '1948-03-01', sex: '女', phone: '13000000000', room: '301', beTime: '2026-10-01', customerName: '张女士', deviceId: 1, familyName: '小明', familySex: '男', familyRelation: '子女', familyPhone: '13900000000', familyAddress: '很长的家属联系地址'.repeat(12), illness: '病史资料'.repeat(30), warn: '青霉素', foodLike: '米饭', foodHate: '花生', drag: '常用药物资料' }
  const lists = [
    ['/operations/residents', 'residents/details', 'oldPersonDetails', elder],
    ['/operations/guardians', 'guardians/list', 'customers', { customerId: 1, customerName: '张女士' }],
    ['/operations/families', 'family/select', 'familyDtos', { familyId: 1, familyName: '小明' }],
    ['/operations/monitoring-profiles', 'monitoring-profiles/list', 'watchDtos', { deviceId: 1, name: '李奶奶' }],
    ['/operations/comments', 'notice-comments/search', 'moderatedComments', { saidId: 1, personName: '小明', said: '请及时联系家属', touPic: '' }]
  ]
  let failure = '', malformed = '', empty = '', hold = '', release
  for (const [, endpoint, key, row] of lists) {
    await context.route('**/api/v1/' + endpoint, async route => {
      if (hold === endpoint) await new Promise(resolve => { release = resolve })
      if (failure === endpoint) return route.fulfill({ status: 503, json: {returnCode:'0000',code:'SERVICE_UNAVAILABLE',message:'请求未完成',} })
      await route.fulfill({ json: {returnCode:'1111', [key]: malformed === endpoint ? null : empty === endpoint ? [] : [row], total: empty === endpoint ? 0 : 1 } })
    })
  }
  await context.route('**/api/v1/image/download?*', route => route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII=', 'base64') }))

  await page.setViewportSize({ width: 1440, height: 1000 })
  for (const [route, title, group] of [
    ['/operations/overview', '运营总览', []], ['/operations/vitals', '心率与血压', ['health']],
    ['/operations/activity', '睡眠与步数', ['health']], ['/operations/locations', '位置与告警', ['health']],
    ['/operations/residents', '老人档案', ['records']], ['/operations/monitoring-profiles', '监测阈值档案', ['records']],
    ['/operations/families', '家属账号', ['records']], ['/operations/invitations', '家属注册邀请码', ['records']],
    ['/operations/comments', '评论管理', ['service']], ['/operations/devices', '设备管理', []]
  ]) {
    await visit(route)
    check(route + ' route title matches navigation', await page.locator('.el-tabs__item.is-active').textContent().then(text => text.includes(title)))
    check(route + ' opens only its navigation group', JSON.stringify(await opened()) === JSON.stringify(group), await opened())
  }
  await visit('/operations/residents')
  await page.locator('#admin-navigation > .el-sub-menu > .el-sub-menu__title').last().click()
  check('manually opening another group closes the previous one', JSON.stringify(await opened()) === '["service"]')
  await page.locator('#admin-navigation .el-menu-item').filter({ hasText: '运营总览' }).click()
  await page.waitForURL(url => url.hash === '#/operations/overview')
  check('navigation back to overview closes all groups', (await opened()).length === 0)
  await page.goBack()
  await page.locator('.view-intro h1').filter({ hasText: '老人档案' }).waitFor()
  check('browser back restores the route group', JSON.stringify(await opened()) === '["records"]')

  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    for (const [route, endpoint] of lists) {
      hold = endpoint; release = undefined
      await visit(route)
      for (let i = 0; i < 100 && !release; i++) await page.waitForTimeout(10)
      check(route + ' ' + width + ' exposes loading', await page.locator('.roles').getAttribute('aria-busy') === 'true' && await page.locator('.roles .el-loading-mask').isVisible())
      hold = ''; release?.()
      await page.waitForFunction(() => !document.querySelector('.roles .el-loading-mask') || getComputedStyle(document.querySelector('.roles .el-loading-mask')).display === 'none')
      check(route + ' ' + width + ' clears loading after success', await page.locator('.roles').getAttribute('aria-busy') === 'false')
      failure = endpoint
      await visit(route)
      await page.waitForTimeout(150)
      const retry = page.getByRole('button', { name: '重新加载', exact: true })
      check(route + ' ' + width + ' distinguishes errors from empty data', await page.locator('.roles [role="alert"]').count() > 0 && await retry.count() === 1)
      check(route + ' ' + width + ' clears loading after failure', await page.locator('.roles').getAttribute('aria-busy') === 'false')
      failure = ''
      if (await retry.count()) {
        await retry.click()
        await page.waitForFunction(() => document.querySelector('.roles')?.getAttribute('aria-busy') === 'false')
        check(route + ' ' + width + ' recovers using retry', await page.locator('.roles [role="alert"]').count() === 0)
      }
    }
  }
  for (const [route, endpoint] of lists) {
    malformed = endpoint
    await visit(route)
    await page.waitForTimeout(100)
    check(route + ' invalid response ends in recoverable error', await page.locator('.roles [role="alert"]').count() === 1 && await page.locator('.roles').getAttribute('aria-busy') === 'false')
    malformed = ''
    empty = endpoint
    await visit(route)
    await page.waitForTimeout(100)
    check(route + ' successful empty response stays distinct from failure', await page.locator('.roles [role="alert"]').count() === 0 && await page.locator('.roles .care-empty-state:visible').count() === 1 && await page.locator('.roles').getAttribute('aria-busy') === 'false')
    empty = ''
  }
  await visit('/operations/residents')
  await page.locator('.adaptive-record-table .el-table__body tr').first().waitFor()
  failure = 'residents/details'
  await page.getByRole('button', { name: '查询长者姓名', exact: true }).click()
  await page.locator('.roles [role="alert"]').waitFor()
  check('failed search clears the previous record and its actions', await page.locator('.admin-record').count() === 0 && await page.locator('.adaptive-record-table').count() === 0)
  failure = ''
  for (const large of [false, true]) {
    for (const width of [360, 390, 1024, 1920]) {
      await page.setViewportSize({ width, height: 844 })
      await page.goto(base + '/?large=' + Number(large) + '&ui=' + Date.now() + '#/operations/residents')
      await page.locator('.roles').waitFor()
      await page.waitForTimeout(100)
      if (width <= 900) {
        await page.locator('.admin-record summary').click()
        check('person ' + width + ' large=' + large + ' retains all details', await page.locator('.admin-record dd').count() === 17 && (await page.locator('.admin-record').textContent()).includes(elder.familyAddress))
        check('person ' + width + ' large=' + large + ' retains edit and delete', await page.locator('.admin-record').getByRole('button', { name: '编辑', exact: true }).isVisible() && await page.locator('.admin-record').getByRole('button', { name: '删除', exact: true }).isVisible())
      }
      check('person ' + width + ' large=' + large + ' has no horizontal overflow', await page.locator('.workspace-main').evaluate(el => el.scrollWidth <= el.clientWidth + 1))
      if (width === 390 && !large) {
        await page.screenshot({ path: path.join(__dirname, 'artifacts/screenshots/person-ui-390.png') })
        await page.locator('.admin-record').getByRole('button', { name: '编辑', exact: true }).click()
        await page.locator('.el-dialog').filter({ hasText: '老人编辑' }).waitFor()
        check('mobile edit opens the correct record', await page.locator('.el-dialog input').first().inputValue() === elder.name)
      }
    }
  }

  let failSummary = false, malformedSummary = false
  let summaryRequests = 0
  await context.route('**/api/v1/operations/snapshot', route => {
    summaryRequests++
    if (failSummary) return route.fulfill({ status: 503, json: { returnCode: '0000', code: 'SERVICE_UNAVAILABLE', message: 'Snapshot unavailable' } })
    return route.fulfill({ json: { returnCode: '1111', snapshot: {
      health: { heartRate: malformedSummary ? null : 2, bloodPressure: 1, sleep: 1 },
      totalResidents: 101, activeLocationAlerts: 2,
      activeCareIncidents: 1, overdueCareIncidents: 0,
      pendingNotifications: 0, failedNotifications: 0, staleNotifications: 0,
      activeDevices: 5, offlineDevices: 1, lowBatteryDevices: 0, unboundDevices: 0,
      capturedAt: '2026-11-11T00:00:00Z'
    } } })
  })
  for (const width of [390, 1440, 1920, 2560]) {
    const requestsBefore = summaryRequests
    await page.setViewportSize({ width, height: 1080 })
    await page.goto(base + '/?large=0&ui=' + Date.now() + '#/operations/overview')
    await page.locator('.overview-grid').waitFor()
    await page.waitForFunction(() => !document.querySelector('.overview-card .el-loading-mask'))
    check('overview ' + width + ' compares real categories',
      (await page.locator('.alert-card').textContent()).includes('心率 2') &&
      (await page.locator('.alert-card strong').first().textContent()) === '4')
    check('overview ' + width + ' loads one atomic snapshot',
      (await page.locator('.fall-card strong').first().textContent()) === '2' &&
      (await page.locator('.fall-card').textContent()).includes('101') &&
      summaryRequests === requestsBefore + 1)
    check('overview ' + width + ' has no horizontal overflow', await page.locator('.workspace-main').evaluate(el => el.scrollWidth <= el.clientWidth + 1))
    const sideBySide = await page.locator('.announcement-panel').evaluate(el => el.getBoundingClientRect().left > document.querySelector('.overview-grid').getBoundingClientRect().right)
    check('overview ' + width + ' uses the intended grid', sideBySide === (width >= 1800))
    if (width === 1920) await page.screenshot({ path: path.join(__dirname, 'artifacts/screenshots/overview-ui-1920.png') })
  }
  malformedSummary = true
  await visit('/operations/overview')
  await page.locator('.metric-error').first().waitFor()
  check('malformed atomic snapshot shows error instead of partial totals',
    await page.locator('.overview-card .metric-error').count() === 4)
  malformedSummary = false
  await page.locator('.overview-card').first().getByRole('button').click()
  await page.waitForFunction(() => document.querySelectorAll('.metric-error').length === 0)
  check('atomic snapshot retry recovers every category',
    (await page.locator('.alert-card strong').first().textContent()) === '4' &&
    (await page.locator('.fall-card strong').first().textContent()) === '2')
  failSummary = true
  await visit('/operations/overview')
  await page.locator('.metric-error').first().waitFor()
  check('HTTP snapshot failure never shows stale totals',
    await page.locator('.overview-card .metric-error').count() === 4)
  failSummary = false
  await page.locator('.overview-card').first().getByRole('button').click()
  await page.waitForFunction(() => document.querySelectorAll('.metric-error').length === 0)
  check('HTTP failure retry reloads the atomic snapshot',
    (await page.locator('.alert-card strong').first().textContent()) === '4')

  setSignedIn(false)
  await page.goto(base + '/#/sign-in')
  await page.locator('#login-password').waitFor()
  check('password uses a visible lock', await page.locator('.el-form-item').filter({ has: page.locator('#login-password') }).locator('.el-input__prefix svg').isVisible())
  await page.getByRole('button', { name: '显示密码', exact: true }).click()
  check('password visibility still works', await page.locator('#login-password').getAttribute('type') === 'text')
  check('no JavaScript errors', report.pageErrors.length === 0, report.pageErrors)
  check('no unexpected APIs or missing assets', report.unexpectedApi.length === 0 && report.missingAssets.length === 0, { apis: report.unexpectedApi, assets: report.missingAssets })
  fs.writeFileSync(path.join(__dirname, 'artifacts/ui-refinement.json'), JSON.stringify({ environment: report.environment, checks }, null, 2))
  const failed = checks.filter(item => !item.pass)
  console.log(JSON.stringify({ checks: checks.length, failed }, null, 2))
  if (failed.length) process.exitCode = 1
}).catch(error => { console.error(error); process.exitCode = 1 })
