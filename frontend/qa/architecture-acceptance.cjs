const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const run = require('./browser-harness.cjs')

run(async ({ page, context, base, report, setRole }) => {
  setRole('ADMIN')
  const checks = []
  const check = (name, pass) => { checks.push({ name, pass: Boolean(pass) }); assert.ok(pass, name) }
  let duty = 'supervisor', actorId = 1, failList = false, failTransition = false, loseTransitionResponse = false, workerHealthy = true, pgReady = false
  let taskRequests = [], transitions = [], diagnosticsRequests = 0
  const tasks = Array.from({ length: 61 }, (_, i) => ({ id: i + 1, oldId: 1, deviceId: 1, oldPersonName: '李奶奶', state: 'received', createdAt: '2026-10-19T08:00:00+08:00', dueAt: '2026-10-19T08:03:00+08:00', notificationState: 'pending' }))
  await context.route('**/api/v1/care/incidents', route => {
    const input = route.request().postDataJSON()
    taskRequests.push(input)
    if (failList) return route.fulfill({ status: 503, json: {returnCode:'0000',code:'SERVICE_UNAVAILABLE',message:'请求未完成',} })
    const filtered = tasks.filter(t => (t.state === 'verified') === (input.state === 'verified'))
    const start = (input.page - 1) * input.pageSize
    return route.fulfill({ json: {returnCode:'1111', items: filtered.slice(start, start + input.pageSize), total: filtered.length, page: input.page, pageSize: input.pageSize, duty, actorId } })
  })
  await context.route('**/api/v1/care/diagnostics', route => {
    diagnosticsRequests++
    return route.fulfill({ json: {returnCode:'1111', primaryEngine: 'mysql', analytics: { configured: true, ready: pgReady, pending: pgReady ? 0 : 205, lastSyncedAt: pgReady ? '2026-10-19T08:00:00Z' : null }, workerHealthy, notificationConfigured: false, receiptConfigured: false, pendingNotifications: 2, failedNotifications: 1, staleSending: 0, overdueIncidents: 1 } })
  })
  await context.route('**/api/v1/care/events', route => {
    const input = route.request().postDataJSON()
    return route.fulfill({ json: {returnCode:'1111', events: Array.from({ length: 105 }, (_, i) => ({ action: '事件' + (i + 1), actor: '1', note: '测试记录', createdAt: '2026-10-19T08:00:00+08:00' })).slice((input.page - 1) * input.pageSize, input.page * input.pageSize), total: 105 } })
  })
  await context.route('**/api/v1/care/transition', route => {
    const input = route.request().postDataJSON()
    transitions.push(input)
    assert.equal(route.request().headers()['x-csrf-token'], 'mock-csrf-token')
    if (failTransition) return route.fulfill({ status: 409, json: {returnCode:'0000',code:'CONFLICT',message:'当前状态不允许此操作'} })
    const task = tasks.find(t => t.id === Number(input.id))
    task.state = { acknowledge: 'acknowledged', start: 'in_progress', resolve: 'resolved', verify: 'verified', reopen: 'in_progress' }[input.action]
    if (input.action === 'resolve') task.resolvedBy = String(actorId)
    if (loseTransitionResponse) return route.abort('timedout')
    return route.fulfill({ json: { returnCode: '1111', state: task.state } })
  })
  const visit = async () => {
    await page.goto(base + '/?architecture=' + Date.now() + '#/operations/overview')
    await page.locator('.care-task').first().waitFor()
    await page.waitForFunction(() => document.querySelector('.care-inbox')?.getAttribute('aria-busy') === 'false')
  }
  const inbox = page.locator('.care-inbox')
  await visit()
  check('overview registers care and requests its complete contract', taskRequests.length === 1 && await page.locator('care-inbox').count() === 0 && await page.locator('.care-task').count() === 20)
  check('unconfigured gateway is explicit', await page.getByRole('alert').filter({ hasText: '通知渠道尚未配置' }).count() === 1)
  check('missing receipt verification is explicit', await page.getByRole('alert').filter({ hasText: '送达回执校验尚未配置' }).count() === 1)
  check('PG failure explains retained events and independent MySQL core', await page.getByRole('alert').filter({ hasText: 'PG 分析库暂不可用' }).count() === 1 && await page.getByText('核心业务主库：MySQL。', { exact: true }).count() === 1)
  const seen = new Set()
  for (let number = 1; number <= 4; number++) {
    for (const text of await page.locator('.care-task-main > strong').allTextContents()) seen.add(text)
    if (number < 4) {
      await inbox.locator(':scope > .el-pagination .btn-next').click()
      await page.waitForFunction(expected => document.querySelector('.care-task-main > strong')?.textContent.includes('#' + expected + ' ·'), number * 20 + 1)
    }
  }
  check('every task is reachable beyond the first 50', seen.size === 61 && taskRequests.at(-1).page === 4)
  await page.getByRole('button', { name: '处置记录', exact: true }).first().click()
  const history = page.getByRole('dialog', { name: '照护事件履历' })
  await history.locator('.care-history li').first().waitFor()
  check('history starts at its first page', await history.locator('.care-history li').count() === 20)
  await history.locator('.el-pagination .btn-next').click()
  await history.getByText('事件21', { exact: true }).waitFor()
  check('history requests later records', await history.getByText('事件40', { exact: true }).isVisible())
  await history.locator('.el-dialog__headerbtn').click()
  duty = 'auditor'
  await visit()
  check('auditor can read history without writes', await page.getByRole('button', { name: '处置记录', exact: true }).count() === 20 && await page.getByRole('button', { name: '接单', exact: true }).count() === 0)
  duty = 'caregiver'; tasks[0].state = 'resolved'; tasks[0].resolvedBy = '2'
  await visit()
  check('caregiver cannot verify or reopen', await page.getByRole('button', { name: '复核关闭', exact: true }).count() === 0 && await page.getByRole('button', { name: '重新开启', exact: true }).count() === 0)
  duty = 'supervisor'; actorId = 2
  await visit()
  check('resolver cannot verify own case', await page.getByRole('button', { name: '复核关闭', exact: true }).count() === 0)
  actorId = 1
  await visit()
  check('independent supervisor can verify', await page.getByRole('button', { name: '复核关闭', exact: true }).count() === 1)
  tasks[0].state = 'in_progress'
  await visit()
  await page.getByRole('button', { name: '登记处置', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '登记处置措施' })
  await dialog.getByRole('button', { name: '确认记录', exact: true }).click()
  check('note validation is visible inside the dialog', await dialog.getByRole('alert').filter({ hasText: '必须填写记录' }).count() === 1 && transitions.length === 0)
  await dialog.locator('textarea').fill('已联系现场核实安全')
  failTransition = true
  await dialog.getByRole('button', { name: '确认记录', exact: true }).click()
  await dialog.getByRole('alert').filter({ hasText: '当前状态不允许此操作' }).waitFor()
  check('write failure preserves the note and task', await dialog.locator('textarea').inputValue() === '已联系现场核实安全' && tasks[0].state === 'in_progress')
  failTransition = false
  await dialog.getByRole('button', { name: '确认记录', exact: true }).click()
  await dialog.waitFor({ state: 'hidden' })
  check('successful transition refreshes its task', tasks[0].state === 'resolved')
  await page.waitForFunction(() => document.querySelector('.care-inbox')?.getAttribute('aria-busy') === 'false')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: '接单', exact: true }).first().click()
  const lostAck = page.getByRole('dialog', { name: '确认接单' })
  await lostAck.locator('textarea').fill('保留现场核对记录')
  loseTransitionResponse = true
  const beforeLostAck = transitions.length
  await lostAck.getByRole('button', { name: '确认记录', exact: true }).click()
  await lostAck.getByRole('status').filter({ hasText: '当前列表状态：已接单' }).waitFor()
  check('lost write response preserves the note and blocks a stale action', await lostAck.locator('textarea').inputValue() === '保留现场核对记录' && await lostAck.getByRole('button', { name: '确认记录', exact: true }).isDisabled() && transitions.length === beforeLostAck + 1)
  await lostAck.getByRole('button', { name: '重新核对', exact: true }).click()
  await lostAck.getByRole('status').filter({ hasText: '当前列表状态：已接单' }).waitFor()
  check('explicit reconciliation performs no duplicate transition', transitions.length === beforeLostAck + 1)
  await lostAck.getByRole('button', { name: '取消', exact: true }).click()
  loseTransitionResponse = false
  await page.setViewportSize({ width: 1440, height: 1000 })
  failList = true
  await inbox.getByRole('button', { name: '刷新任务', exact: true }).click()
  await inbox.getByRole('alert').filter({ hasText: '请求未完成' }).waitFor()
  check('failed refresh removes stale task actions', await page.locator('.care-task').count() === 0)
  failList = false
  await inbox.getByRole('button', { name: '刷新任务', exact: true }).click()
  await page.locator('.care-task').first().waitFor()
  check('task refresh recovers', await page.locator('.care-task').count() === 20)
  await page.clock.install()
  const before = diagnosticsRequests
  workerHealthy = false
  pgReady = true
  await page.clock.fastForward(15000)
  await page.getByRole('alert').filter({ hasText: '进程心跳异常或尚未启动' }).waitFor()
  check('heartbeat updates without reopening the page', diagnosticsRequests > before)
  await page.getByRole('status').filter({ hasText: 'PG 分析库可用' }).waitFor()
  check('PG recovery updates queue diagnostics without reopening', (await page.getByRole('status').filter({ hasText: 'PG 分析库可用' }).textContent()).includes('0'))
  for (const large of [false, true]) {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto(base + '/?large=' + Number(large) + '&architecture=' + Date.now() + '#/operations/overview')
    await page.locator('.care-task').first().waitFor()
    check('queue fits 390px large=' + large, await page.locator('.workspace-main').evaluate(el => el.scrollWidth <= el.clientWidth + 1))
  }
  let failGuardianPage = true
  await context.route('**/api/v1/guardians/list', route => {
    const { page: number = 1, pageSize = 20 } = route.request().postDataJSON()
    if (failGuardianPage && number === 2) return route.fulfill({ status: 503, json: {returnCode:'0000',code:'SERVICE_UNAVAILABLE',message: 'temporarily unavailable' } })
    const start = (number - 1) * pageSize
    return route.fulfill({ json: {returnCode:'1111', customers: Array.from({ length: Math.max(0, Math.min(pageSize, 205 - start)) }, (_, i) => ({ customerId: start + i + 1, customerName: '监护人' + (start + i + 1) })), total: 205 } })
  })
  await page.goto(base + '/?large=0&architecture=guardians#/operations/residents')
  await page.getByRole('button', { name: '添加老人', exact: true }).click()
  const elder = page.getByRole('dialog', { name: '老人添加' })
  await elder.getByRole('alert').filter({ hasText: 'temporarily unavailable' }).waitFor()
  await elder.locator('.el-select').click()
  check('failed guardian page does not expose a partial list', await page.getByRole('option').count() === 0)
  await elder.locator('.el-select').click()
  failGuardianPage = false
  await elder.getByRole('button', { name: '重新加载监护人', exact: true }).click()
  await page.waitForFunction(() => document.querySelector('.guardian-select')?.getAttribute('aria-busy') !== 'true')
  await elder.locator('.el-select').click()
  check('elder creation can select guardians beyond the first page', await page.getByRole('option', { name: '监护人205', exact: true }).count() === 1)
  check('no JavaScript errors or unexpected APIs', report.pageErrors.length === 0 && report.unexpectedApi.length === 0)
  fs.writeFileSync(path.join(__dirname, 'artifacts/architecture-acceptance.json'), JSON.stringify({ checks }, null, 2))
  console.log(JSON.stringify({ checks: checks.length, failed: [] }))
}).catch(error => { console.error(error); process.exitCode = 1 })
