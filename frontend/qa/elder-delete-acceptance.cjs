const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const run = require('./browser-harness.cjs');

run(async ({ page, context, base, report, setRole }) => {
  setRole('ADMIN');
  let requests = 0;
  let status = 409;
  let body = { returnCode: '0000', code: 'CONFLICT', message: '请先解除该长者名下设备绑定' };
  let refreshes = 0;
  page.on('request', request => {
    if (request.url().includes('/api/v1/residents/details')) refreshes++;
  });
  await context.route('**/api/v1/residents/delete', async route => {
    requests++;
    assert.equal(route.request().headers()['x-csrf-token'], 'mock-csrf-token');
    assert.equal(route.request().postDataJSON().oldId, 1);
    await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
  });
  await page.goto(base + '/#/operations/residents');
  const remove = page.getByRole('button', { name: '删除', exact: true }).first();
  await remove.waitFor();
  const confirm = async () => {
    await remove.click();
    await page.locator('.el-message-box').getByRole('button', { name: '确定', exact: true }).click();
  };
  const message = async text => {
    await page.locator('.el-message').filter({ hasText: text }).waitFor();
    report.checks.push({ name: text, pass: true });
  };
  const initialRefreshes = refreshes;
  await confirm();
  await message('请先解除该长者名下设备绑定');
  assert.equal(refreshes, initialRefreshes);
  assert.equal(await page.locator('.el-message--success').count(), 0);

  const beforeCancel = requests;
  await remove.click();
  await page.locator('.el-message-box').getByRole('button', { name: '取消', exact: true }).click();
  assert.equal(await page.locator('.el-message').filter({ hasText: '已取消删除' }).count(), 0);
  assert.equal(requests, beforeCancel);

  status = 500;
  body = { message: 'SQL internal details must not be displayed' };
  await confirm();
  await message('服务处理失败');
  assert.equal(await page.getByText(body.message).count(), 0);
  assert.equal(refreshes, initialRefreshes);

  await page.waitForFunction(() => document.querySelectorAll('.el-message--error').length === 0);
  status = 400;
  body = { returnCode: '0000', code: 'INVALID_REQUEST', message: 'record cannot be deleted' };
  await confirm();
  await message('record cannot be deleted');
  assert.equal(refreshes, initialRefreshes);

  status = 200;
  body = { returnCode: '1111' };
  const refreshed = page.waitForRequest(request => request.url().includes('/api/v1/residents/details'));
  await confirm();
  await message('删除信息成功');
  await refreshed;
  assert.equal(refreshes, initialRefreshes + 1);
  assert.equal(requests, 4);
  assert.deepEqual(report.pageErrors, []);
  assert.deepEqual(report.unexpectedApi, []);
  fs.writeFileSync(path.join(__dirname, 'artifacts/elder-delete.json'), JSON.stringify(report, null, 2));
  console.log('Elder deletion: conflict, cancel, HTTP failure, business failure and success passed.');
}).catch(error => { console.error(error); process.exitCode = 1; });
