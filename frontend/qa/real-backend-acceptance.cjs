const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const { execFileSync } = require('node:child_process')
const { chromium, request } = require('playwright')

async function main () {
  const base = process.env.CARE_E2E_BASE
  const schema = process.env.CARE_E2E_SCHEMA
  assert.equal(process.env.CARE_E2E_TEST_ONLY, '1', 'Requires a newly created isolated test schema')
  assert.equal(new URL(base).hostname, '127.0.0.1')
  assert.match(schema, /^qa_arch_[a-f0-9]+$/)
  const checks = []
  const check = (name, pass) => { checks.push({ name, pass: Boolean(pass) }); assert.ok(pass, name) }
  const bootstrap = await request.newContext({ baseURL: base })
  for (let n = 0; n < 100; n++) {
    if ((await (await bootstrap.get('/api/v1/service/status')).json()).databaseReady) break
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert.equal((await (await bootstrap.get('/api/v1/auth/session')).json()).setupRequired, true, 'Refuses to seed a non-empty database')
  const password = 'LocalTestOnly1234'
  const setup = await bootstrap.post('/api/v1/setup', { headers: { 'X-Yihe-Desktop': process.env.CARE_E2E_DESKTOP_TOKEN }, data: { name: '测试主管', phone: '13800000001', password } })
  assert.equal(setup.status(), 200)
  assert.equal((await setup.json()).returnCode, '1111')
  execFileSync(process.env.CARE_E2E_MYSQL, ['--no-defaults', '--host=127.0.0.1', '--port=' + process.env.CARE_E2E_MYSQL_PORT, '--user=' + process.env.CARE_E2E_MYSQL_USER, '--database=' + schema, '--execute=' + "INSERT INTO users(user_name,user_phone,password,role) SELECT 'reviewer','13800000002',password,'1' FROM users WHERE user_phone='13800000001'"], { stdio: 'pipe' })
  await bootstrap.dispose()
  const browser = await chromium.launch({ headless: true, ...(process.env.CARE_CHROME_PATH ? { executablePath: process.env.CARE_CHROME_PATH } : {}) })
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  const login = async phone => {
    await page.goto(base + '/#/sign-in')
    await page.locator('#login-phone').fill(phone)
    await page.locator('#login-password').fill(password)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await page.locator('.care-inbox').waitFor()
  }
  const post = async (endpoint, data) => {
    const session = await (await page.request.get(base + '/api/v1/auth/session')).json()
    const response = await page.request.post(base + '/api/v1/' + endpoint, { headers: { [session.csrfHeader]: session.csrfToken }, data })
    assert.equal(response.status(), 200, endpoint)
    const body = await response.json()
    assert.notEqual(body.returnCode, '0000', endpoint)
    return body
  }
  try {
    await login('13800000001')
    check('real login establishes a server session', await page.locator('.care-inbox').count() === 1)
    await post('customer/add', { customerName: '张女士', customerAge: 50, customerSex: '女', customerPhone: '13800000003', customerAddress: '测试地址' })
    await page.goto(base + '/#/operations/residents')
    await page.getByRole('button', { name: '添加老人', exact: true }).click()
    const elderDialog = page.getByRole('dialog', { name: '老人添加' })
    for (const [label, value] of [['姓名', '测试长者'], ['电话', '13800000004'], ['房间', '101'], ['病史', '暂无'], ['过敏药物', '无'], ['喜好食物', '米饭'], ['忌口食物', '花生'], ['常用药物', '常用药']]) {
      await elderDialog.locator('.el-form-item').filter({ hasText: label }).locator('input').first().fill(value)
    }
    await elderDialog.locator('.el-radio').filter({ hasText: '女' }).click()
    const birth = elderDialog.locator('.el-date-editor input')
    await birth.fill('1950-01-01'); await birth.press('Enter')
    await elderDialog.locator('.el-select').click()
    await page.getByRole('option', { name: '张女士', exact: true }).click()
    await elderDialog.getByRole('button', { name: '确 定', exact: true }).click()
    await elderDialog.waitFor({ state: 'hidden' })
    const details = (await post('oldPerson/selectDetail', { name: '测试长者' })).oldPersonDetails[0]
    check('elder can be created in the real UI before a watch exists', details.deviceId === null)
    check('real stored diet and medication keep their field meanings', details.foodLike === '米饭' && details.foodHate === '花生' && details.drag === '常用药')
    const device = await post('device/create', { alias: '测试手表', model: 'QA', oldId: '' })
    const code = await post('device/createBindCode', { oldId: details.oldId, expireDays: '1' })
    await post('device/bind', { deviceNo: device.deviceNo, secret: device.secret, code: code.code })
    const deviceSession = await post('device/login', { deviceNo: device.deviceNo, secret: device.secret })
    const bound = (await post('device/list', {})).devices[0]
    const linked = (await post('oldPerson/selectDetail', { name: '测试长者' })).oldPersonDetails[0]
    check('binding keeps both inventory and elder references consistent', bound.oldId === details.oldId && linked.deviceId === device.deviceId)
    await page.goto(base + '/#/operations/monitoring-profiles')
    await page.locator('.adaptive-record-table').getByRole('button', { name: '编辑', exact: true }).click()
    const watchDialog = page.getByRole('dialog', { name: '设备编辑' })
    check('real threshold API supplies the editable device identifier', await watchDialog.locator('input').first().inputValue() === String(device.deviceId))
    await watchDialog.locator('.el-form-item').filter({ hasText: '心率高值' }).locator('input').fill('125')
    await watchDialog.getByRole('button', { name: '确 定', exact: true }).click()
    await watchDialog.waitFor({ state: 'hidden' })
    check('real UI threshold editing persists to MySQL', (await post('watchInfo/select', { oldName: '' })).watchDtos[0].hrf === 125)
    await post('device/report', { deviceNo: device.deviceNo, secret: device.secret, hr: 72 })
    const metric = (await post('ratepre/selectRatepre', {})).vitalRecords[0]
    check('real heart-rate-only upload leaves blood pressure unknown', metric.hr === 72 && metric.bp === null && metric.preState === null)
    await post('device/sos', { deviceNo: device.deviceNo, secret: device.secret, requestId: 'qa-press-1', bindingVersion: deviceSession.bindingVersion })
    await page.goto(base + '/#/operations/overview')
    await page.locator('.care-task').first().waitFor()
    for (const [button, title, note] of [['接单', '确认接单', ''], ['开始处置', '开始处置', ''], ['登记处置', '登记处置措施', '护理记录'.repeat(300)]]) {
      await page.getByRole('button', { name: button, exact: true }).click()
      const dialog = page.getByRole('dialog', { name: title, exact: true })
      if (note) await dialog.locator('textarea').fill(note)
      await dialog.getByRole('button', { name: '确认记录', exact: true }).click()
      await dialog.waitFor({ state: 'hidden' })
      await page.waitForFunction(() => document.querySelector('.care-inbox')?.getAttribute('aria-busy') === 'false')
    }
    check('real SOS proceeds through acknowledge, start and resolution', (await post('care/incidents', { state: 'active' })).items[0].state === 'resolved')
    check('resolving operator cannot self-verify in the UI', await page.getByRole('button', { name: '复核关闭', exact: true }).count() === 0)
    await page.getByRole('button', { name: '退出', exact: true }).click()
    await page.locator('.el-message-box').getByRole('button', { name: '退出', exact: true }).click()
    await login('13800000002')
    await page.getByRole('button', { name: '复核关闭', exact: true }).click()
    const verification = page.getByRole('dialog', { name: '复核关闭', exact: true })
    await verification.locator('textarea').fill('已核对处置记录并独立复核')
    await verification.getByRole('button', { name: '确认记录', exact: true }).click()
    await verification.waitFor({ state: 'hidden' })
    const completed = await post('care/incidents', { state: 'verified' })
    const position = await post('position/select', { name: '' })
    check('independent verification closes the durable case and linked fall alarm', completed.total === 1 && completed.items[0].state === 'verified' && position.positionDtos[0].status === '0')
    await page.waitForFunction(() => document.querySelector('.fall-card .metric-copy strong')?.textContent === '0', null, { timeout: 5000 })
    check('verification also refreshes the visible fall summary', await page.locator('.fall-card .metric-copy strong').textContent() === '0')
    await page.locator('.care-inbox-tools .el-select').click()
    await page.getByRole('option', { name: '已复核任务', exact: true }).click()
    await page.locator('.care-task-main > span').filter({ hasText: '已复核关闭' }).waitFor()
    check('completed cases remain reachable in the real UI', await page.getByRole('button', { name: '处置记录', exact: true }).count() === 1)
    await page.keyboard.press('Escape')
    await page.locator('.care-task').scrollIntoViewIfNeeded()
    await page.screenshot({ path: path.join(__dirname, 'artifacts/screenshots/real-care-closed.png'), animations: 'disabled' })
    await page.getByRole('button', { name: '处置记录', exact: true }).click()
    const history = page.getByRole('dialog', { name: '照护事件履历' })
    await history.getByText('已核对处置记录并独立复核', { exact: true }).waitFor()
    check('independent reviewer can retrieve the complete persisted care history', await history.locator('.care-history li').count() >= 5 && await history.getByText('护理记录'.repeat(300), { exact: true }).isVisible())
    if (process.env.CARE_E2E_EXPECT_PG_DOWN === '1') {
      const availability = await (await page.request.get(base + '/api/v1/service/status')).json()
      check('MySQL core workflow completes while PG is offline', availability.databaseReady === true && availability.primaryEngine === 'mysql' && availability.analytics.ready === false)
    }
    check('no JavaScript errors with real responses', errors.length === 0)
    await page.screenshot({ path: path.join(__dirname, 'artifacts/screenshots/real-care-history.png'), animations: 'disabled' })
    fs.writeFileSync(path.join(__dirname, 'artifacts/real-backend-acceptance.json'), JSON.stringify({ environment: 'Production UI + actual Go process + isolated MySQL primary, no API mocks', checks }, null, 2))
    console.log(JSON.stringify({ checks: checks.length, failed: [] }))
  } finally { await browser.close() }
}
main().catch(error => { console.error(error); process.exitCode = 1 })
