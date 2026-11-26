const fs = require('node:fs')
const path = require('node:path')
const run = require('./browser-harness.cjs')

run(async ({ page, base, report, setRole, setSignedIn }) => {
  const checks = []
  const check = (name, pass, details) => checks.push({ name, pass: Boolean(pass), details })
  let resumeStalled
  const stalled = new Promise(resolve => { resumeStalled = resolve })
  let resumeHistory
  const stalledHistory = new Promise(resolve => { resumeHistory = resolve })
  let incidents = []
  let histories = 0

  await page.route('**/api/v1/care/incidents', async route => {
    const query = route.request().postDataJSON()
    incidents.push(query.page)
    if (query.page === 2) {
      await stalled
      try { await route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({returnCode:'1111', items: [{ id: 'stale', oldPersonName: 'STALE-F02', state: 'received' }], total: 100, duty: 'supervisor', actorId: 7 })
      }) } catch (_) { /* request should have been canceled */ }
      return
    }
    const label = query.page === 3 ? 'F02-LATEST' : 'F02-INITIAL'
    await route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({returnCode:'1111', items: [{ id: 'f02-' + query.page, oldPersonName: label, state: 'received' }], total: 100, duty: 'supervisor', actorId: 7 })
    })
  })
  await page.route('**/api/v1/care/events', async route => {
    histories++
    if (histories === 1) {
      await stalledHistory
      try { await route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({returnCode:'1111', events: [{ action: 'STALE-HISTORY', actor: '值守', createdAt: '2026-10-28T00:00:00Z' }], total: 1 })
      }) } catch (_) { /* dismissed dialog cancels transport */ }
      return
    }
    await route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({returnCode:'1111', events: [{ action: 'F02-HISTORY-LATEST', actor: '值守', createdAt: '2026-10-28T00:00:00Z' }], total: 1 })
    })
  })

  try {
    setRole('ADMIN')
    setSignedIn(true)
    await page.goto(base + '/?f02=' + Date.now() + '#/operations/overview')
    await page.getByText('F02-INITIAL').waitFor({ timeout: 10000 })
    const next = page.locator('.care-inbox .el-pagination .btn-next')
    await next.click()
    await page.waitForFunction(() => document.querySelector('.care-inbox')?.getAttribute('aria-busy') === 'true')
    await next.click()
    await page.getByText('F02-LATEST').waitFor({ timeout: 5000 })
    check('F02: new page renders while obsolete page request never returns',
      incidents.join(',') === '1,2,3', { incidents })
    check('F02: loading indicator clears without waiting for obsolete request',
      await page.locator('.care-inbox').getAttribute('aria-busy') === 'false')
    resumeStalled()
    await page.waitForTimeout(200)
    check('F02: late response cannot replace latest page',
      await page.getByText('F02-LATEST').count() === 1 &&
      await page.getByText('STALE-F02').count() === 0)

    await page.getByRole('button', { name: '处置记录' }).first().click()
    await page.getByText('正在读取记录…').waitFor()
    await page.locator('.el-dialog:visible .el-dialog__headerbtn').click()
    await page.getByRole('button', { name: '处置记录' }).first().click()
    await page.getByText('F02-HISTORY-LATEST').waitFor({ timeout: 5000 })
    resumeHistory()
    await page.waitForTimeout(200)
    check('F02: dismissing history aborts stale request; reopening works',
      histories === 2 && await page.getByText('STALE-HISTORY').count() === 0, { histories })
    check('F02: no browser JS exceptions', report.pageErrors.length === 0, report.pageErrors)
  } finally {
    resumeStalled()
    resumeHistory()
  }
  fs.writeFileSync(path.join(__dirname, 'artifacts/f02-acceptance.json'), JSON.stringify({ checks }, null, 2))
  const failed = checks.filter(item => !item.pass)
  console.log(JSON.stringify({ checks: checks.length, failed }, null, 2))
  if (failed.length) process.exitCode = 1
}).catch(error => { console.error(error); process.exitCode = 1 })
