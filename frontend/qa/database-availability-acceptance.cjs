const fs = require('node:fs')
const path = require('node:path')
const run = require('./browser-harness.cjs')
run(async ({ page, context, base }) => {
  let available = false
  let authenticated = false
  let writes = 0
  const checks = []
  const check = (name, pass) => { checks.push({ name, pass }); if (!pass) throw new Error(name) }
  await context.route('**/api/v1/**', async route => {
    const endpoint = new URL(route.request().url()).pathname
    if (endpoint === '/api/v1/service/status') return route.fulfill({ json: {returnCode:'1111', databaseReady: available } })
    if (!available) return route.fulfill({ status: 503, json: {returnCode:'0000',code: 'DATABASE_UNAVAILABLE', message: '数据库尚未连接' } })
    if (endpoint === '/api/v1/auth/session') return route.fulfill({ json: {returnCode:'1111', authenticated, account: authenticated ? { role: 'ADMIN', id: 1, duty: 'supervisor', displayName: '管理员' } : null, setupRequired: false, csrfToken: 'mock-csrf-token' } })
    if (route.request().method() === 'POST' && /\/(login|setup|create|update|delete|add|bind|insert)$/.test(endpoint)) writes++
    return route.fallback()
  })
  await page.goto(base + '/#/sign-in')
  await page.getByRole('heading', { name: '欢迎回来' }).waitFor({ timeout: 5000 })
  check('Database outage does not replace login with a connection wall', page.url().endsWith('/sign-in'))
  await page.setViewportSize({ width: 320, height: 560 })
  check('Offline login has no horizontal overflow at 320px', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  const loginButton = await page.getByRole('button', { name: '登录', exact: true }).boundingBox()
  check('Offline login action stays visible on a short screen', loginButton.y >= 0 && loginButton.y + loginButton.height <= 560)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.locator('#login-phone').fill('13000000000')
  await page.locator('#login-password').fill('LocalTest-2026!')
  await page.getByRole('button', { name: '大字', exact: true }).click()
  check('Local text-size control works offline', await page.locator('html').evaluate(el => el.classList.contains('large-text')))
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.locator('.auth-error').filter({ hasText: '数据库' }).waitFor()
  check('Unavailable login explains its database requirement without navigating', page.url().endsWith('/sign-in'))
  await page.goto(base + '/#/operations/devices')
  await page.waitForURL(url => url.hash === '#/sign-in')
  check('Database outage never bypasses authentication', page.url().endsWith('/sign-in'))
  await page.locator('#login-phone').fill('13000000000')
  available = true
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await page.locator('.database-notice').waitFor({ state: 'hidden' })
  check('Online event reconnects without clearing an unfinished login', await page.locator('#login-phone').inputValue() === '13000000000')
  authenticated = true
  await page.goto(base + '/?admin=1#/operations/overview')
  await page.getByRole('heading', { name: '运营总览', exact: true }).waitFor({ timeout: 5000 }).catch(async e => { console.error('Current page', page.url(), await page.locator('body').innerText()); throw e })
  available = false
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await page.locator('.database-notice').waitFor()
  check('Authenticated workspace remains visible during an outage', page.url().endsWith('/operations/overview'))
  await page.goto(base + '/?admin=1#/operations/devices')
  await page.getByRole('heading', { name: '设备管理', exact: true }).waitFor()
  check('Authenticated navigation is retained while database features show errors', page.url().endsWith('/operations/devices'))
  await page.locator('#device-query').fill('未保存的查询')
  available = true
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await page.locator('.database-notice').waitFor({ state: 'hidden' })
  check('Recovery preserves local input', await page.locator('#device-query').inputValue() === '未保存的查询')
  check('Recovery preserves the current feature route', page.url().endsWith('/operations/devices'))
  check('Recovery never replays failed writes', writes === 0)
  available = false
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await page.locator('.database-notice').waitFor()
  authenticated = false
  available = true
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await page.waitForURL(url => url.hash === '#/sign-in')
  check('Expired session is not treated as authenticated after recovery', page.url().endsWith('/sign-in'))
  fs.writeFileSync(path.join(__dirname, 'artifacts/database-availability.json'), JSON.stringify({ checks }, null, 2))
  console.log(JSON.stringify({ checks: checks.length, failed: checks.filter(c => !c.pass) }))
}).catch(error => { console.error(error); process.exitCode = 1 })
