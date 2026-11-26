// Actual Index/EntryAbility UI and OS lifecycle, isolated from the production
// bundle. Uses an empty dedicated strict Go/MySQL backend; no sensor substitute.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const crypto = require('node:crypto');
const { spawn, spawnSync } = require('node:child_process');
const { performance } = require('node:perf_hooks');
// HDC/emulator clock synchronization can adjust wall time during a live run.
// Duration assertions use the host monotonic clock; signatures keep Unix time.
const now = () => performance.now();
const base = process.env.WATCH_TEST_URL, bootstrap = process.env.WATCH_TEST_SETUP_TOKEN;
const sdk = process.env.WATCH_NATIVE_COMMANDLINE_HOME, testReport = process.env.WATCH_NATIVE_BUILD_REPORT;
const appReport = process.env.WATCH_NATIVE_APP_REPORT, evidence = process.env.WATCH_NATIVE_REPORT_DIR;
const pwsh = process.env.WATCH_NATIVE_PWSH, target = process.env.WATCH_NATIVE_TARGET || '127.0.0.1:5555';
assert.ok(/^http:\/\/127\.0\.0\.1:\d+$/.test(base || '') && bootstrap && sdk && testReport && appReport && evidence && pwsh);
assert.match(target, /^[A-Za-z0-9_.:-]+$/);
const repo = path.resolve(__dirname, '../..'), hdc = path.join(sdk, 'sdk/default/openharmony/toolchains/hdc.exe');
const git = args => {
  const result = spawnSync('git', args, { cwd: repo, encoding: 'utf8', windowsHide: true });
  assert.equal(result.status, 0, 'Cannot verify UI source'); return result.stdout.trim();
};
const source = git(['rev-parse', 'HEAD']);
const builds = [appReport, testReport].map(file => JSON.parse(fs.readFileSync(file, 'utf8').replace(/^\uFEFF/, '')));
for (const build of builds) { assert.equal(build.source, source); assert.equal(build.product, 'quality'); assert.equal(build.status, 'passed'); }
const dirty = !!git(['status', '--porcelain', '--untracked-files=no']) ||
  !!git(['status', '--porcelain', '--untracked-files=normal', '--', 'YiheWatch']) || builds.some(build => build.dirty);
if (dirty) throw new Error('Formal UI acceptance requires clean source');
const bundle = 'com.yihe.watch.quality';
fs.mkdirSync(evidence, { recursive: true });
function hdcCall(args) {
  const result = spawnSync(hdc, args, { encoding: 'utf8', timeout: 15000, windowsHide: true });
  assert.equal(result.status, 0, 'HDC UI operation failed'); return result.stdout;
}
const owns = mapping => hdcCall(['fport', 'ls']).split(/\r?\n/).some(line =>
  line.trim().replace(/\s+/g, ' ') === target + ' ' + mapping + ' [Reverse]');
const wait = async predicate => {
  const limit = now() + 12000;
  while (!predicate()) { if (now() > limit) throw new Error('UI host condition timed out'); await new Promise(resolve => setTimeout(resolve, 50)); }
};
async function bytes(request, limit) {
  let size = 0; const chunks = [];
  for await (const data of request) { size += data.length; assert.ok(size <= limit); chunks.push(data); }
  return Buffer.concat(chunks);
}
let cookie = '', csrf = '';
async function api(route, body, extra = {}, expected = 200) {
  const response = await fetch(base + route, { method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json', Cookie: cookie, 'X-CSRF-TOKEN': csrf, ...extra },
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(15000) });
  const set = response.headers.get('set-cookie'); if (set) cookie = set.split(';')[0];
  const result = await response.json(); assert.equal(response.status, expected, route + ' status');
  if (expected === 200 && result.returnCode !== undefined) assert.equal(result.returnCode, '1111');
  return result;
}
function signed(body) {
  const timestamp = String(Math.floor(Date.now() / 1000)), nonce = crypto.randomBytes(16).toString('hex');
  const digest = crypto.createHash('sha256').update(JSON.stringify(body)).digest('hex');
  return { 'X-Device-Timestamp': timestamp, 'X-Device-Nonce': nonce,
    'X-Device-Signature': crypto.createHmac('sha256', body.secret).update(body.deviceNo + ':' + timestamp + ':' + nonce + ':' + digest).digest('hex') };
}
const listen = server => new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
const close = server => new Promise(resolve => { server.closeAllConnections(); server.close(resolve); });
async function main() {
  assert.ok(hdcCall(['list', 'targets']).split(/\r?\n/).map(s => s.trim()).includes(target));
  const productionFingerprint = () => hdcCall(['-t', target, 'shell', 'if [ -f /data/app/el2/100/base/com.yihe.watch/haps/entry/preferences/yihe_watch_sim_v1 ]; then sha256sum /data/app/el2/100/base/com.yihe.watch/haps/entry/preferences/yihe_watch_sim_v1; else echo ABSENT; fi']).trim();
  const productionBefore = productionFingerprint();
  assert.ok(productionBefore === 'ABSENT' || /^[a-f0-9]{64}\s/.test(productionBefore));
  assert.equal((await api('/api/v1/service/status')).databaseReady, true);
  const session = await api('/api/v1/auth/session');
  assert.equal(session.setupRequired, true, 'UI tests must never use an existing database'); csrf = session.csrfToken;
  const password = 'UI' + crypto.randomBytes(8).toString('hex');
  await api('/api/v1/setup', { name: '原生界面验收', phone: '13900000001', password }, { 'X-Yihe-Desktop': bootstrap });
  await api('/api/v1/auth/login', { phone: '13900000001', password }); csrf = (await api('/api/v1/auth/session')).csrfToken;
  await api('/api/v1/guardians/create', { customerName: '界面测试监护', customerAge: '40', customerSex: '男', customerPhone: '13900000002', customerAddress: 'test only' });
  const guardian = (await api('/api/v1/guardians/list', { customerName: '界面测试监护', page: 1, pageSize: 20 })).customers[0];
  await api('/api/v1/residents/create', { name: '界面测试长者', bothdate: '1950-01-01', sex: '男', phone: '13900000003', room: 'UI01', illness: '', warn: '', foodLike: '', foodHate: '', drag: '', customerId: String(guardian.customerId) });
  const elder = (await api('/api/v1/residents/list', { name: '界面测试长者', page: 1, pageSize: 20 })).oldPeople[0];
  const device = await api('/api/v1/device/create', { alias: 'Native UI isolated', model: 'WATCH5-UI-TEST' });
  const credentials = { deviceNo: device.deviceNo, secret: device.secret };
  assert.equal((await api('/api/v1/device/login', credentials, {}, 401)).code, 'DEVICE_SIGNATURE_INVALID');
  const code = await api('/api/v1/device/createBindCode', { oldId: String(elder.oldId), expireDays: '1' });
  const binding = { ...credentials, code: code.code }; await api('/api/v1/device/bind', binding, signed(binding));
  const token = crypto.randomBytes(32).toString('hex'), mappings = [], requests = [], nonces = new Set();
  const phases = ['fixture', 'authenticated', 'started', 'periodic', 'background', 'background-quiet', 'foreground', 'stopped', 'stop-quiet',
    'hold-login', 'request-held', 'cancelled', 'recovered', 'restart-ready', 'restore-fixture', 'restart-running', 'restart-stopped', 'restart-quiet'];
  const completed = [], timing = {}, failures = [], captures = [], pending = new Set();
  let holding = false, cancelled = 0, baseline = 0, quietBaseline = 0, status = 'failed', firstError, processRestartVerified = false;
  let initialProcessPID = '', restartedProcessPID = '', processTermination = '';
  let heldAt = 0, cancelObservedAt = 0, cancellationArmedAt = 0, qualityProcessStopped = false;
  const fail = error => { firstError ||= error; };
  const logins = () => requests.filter(item => item.route === 'login');
  const proxy = http.createServer(async (incoming, outgoing) => {
    try {
      assert.equal(incoming.method, 'POST'); assert.match(incoming.url, /^\/api\/v1\/device\/(login|heartbeat|healthBatch)$/);
      const raw = await bytes(incoming, 1048576), body = JSON.parse(raw);
      assert.ok(body.deviceNo === device.deviceNo && body.secret === device.secret, 'UI credential mismatch');
      const nonce = incoming.headers['x-device-nonce'], timestamp = incoming.headers['x-device-timestamp'];
      assert.match(nonce || '', /^[a-f0-9]{32}$/); assert.match(timestamp || '', /^\d+$/);
      assert.ok(Math.abs(Date.now() / 1000 - Number(timestamp)) <= 300);
      const digest = crypto.createHash('sha256').update(raw).digest('hex');
      const expected = crypto.createHmac('sha256', device.secret).update(device.deviceNo + ':' + timestamp + ':' + nonce + ':' + digest).digest('hex');
      assert.equal(incoming.headers['x-device-signature'], expected);
      assert.ok(!nonces.has(nonce), 'UI application repeated a signed request'); nonces.add(nonce);
      const route = incoming.url.split('/').pop(); requests.push({ route, at: now(), wallTime: new Date().toISOString() }); assert.ok(requests.length <= 100);
      const response = await fetch(base + incoming.url, { method: 'POST', headers: { 'Content-Type': 'application/json',
        'X-Device-Nonce': nonce, 'X-Device-Timestamp': timestamp, 'X-Device-Signature': incoming.headers['x-device-signature'] }, body: raw, signal: AbortSignal.timeout(15000) });
      const text = await response.text(); assert.equal(response.status, 200, 'Actual Go request failed');
      if (holding && route === 'login') {
        heldAt = now(); pending.add(outgoing);
        outgoing.once('close', () => { if (!outgoing.writableFinished) { cancelled++; cancelObservedAt = now(); } pending.delete(outgoing); });
        return;
      }
      outgoing.writeHead(response.status, { 'Content-Type': 'application/json' }); outgoing.end(text);
    } catch (error) { fail(error); outgoing.destroy(); }
  });
  const control = http.createServer(async (incoming, outgoing) => {
    try {
      if (incoming.method !== 'POST' || incoming.headers.authorization !== 'Bearer ' + token) {
        throw new Error('Unauthenticated UI control request');
      }
      const body = JSON.parse(await bytes(incoming, 4096));
      if (incoming.url === '/capture') {
        assert.match(body.file, /^\/data\/storage\/el2\/base\/(?:haps\/entry\/)?files\/yihe-quality-ui-(?:home|records)-\d{13}\.png$/);
        assert.ok(['home', 'records'].includes(body.label) && !captures.some(item => item.label === body.label));
        const local = path.join(evidence, path.basename(body.file));
        assert.match(hdcCall(['-t', target, 'file', 'recv', '-b', bundle, body.file, local]), /FileTransfer finish/);
        const png = fs.readFileSync(local); assert.equal(png.subarray(0, 8).toString('hex'), '89504e470d0a1a0a');
        captures.push({ label: body.label, file: path.basename(local), sha256: crypto.createHash('sha256').update(png).digest('hex'), bytes: png.length });
        fs.writeFileSync(path.join(evidence, 'captures.json'), JSON.stringify(captures, null, 2));
        outgoing.writeHead(200, { 'Content-Type': 'application/json' }); outgoing.end(JSON.stringify({ ok: true })); return;
      }
      assert.equal(incoming.url, '/control');
      // Select the next phase locally. The request can only confirm it.
      const action = phases[completed.length];
      assert.ok(action, 'No further UI phase is permitted');
      assert.equal(body.action, action);
      switch (action) {
        case 'authenticated': await wait(() => logins().length >= 1); baseline = logins().length; break;
        case 'started': await wait(() => logins().length > baseline); baseline = logins().length; timing.started = logins().at(-1).at; break;
        case 'periodic': await wait(() => logins().length > baseline); timing.periodic = logins().at(-1).at - timing.started; assert.ok(timing.periodic >= 60000); break;
        case 'background': case 'stopped': case 'restart-stopped': baseline = logins().length; quietBaseline = requests.length; timing[action] = now(); break;
        case 'background-quiet': case 'stop-quiet': case 'restart-quiet': {
          const since = action === 'background-quiet' ? 'background' : action === 'stop-quiet' ? 'stopped' : 'restart-stopped';
          timing[action] = now() - timing[since]; assert.ok(timing[action] >= 60000); assert.equal(requests.length, quietBaseline); break;
        }
        case 'foreground': await wait(() => logins().length > baseline); break;
        case 'hold-login': holding = true; break;
        case 'request-held': await wait(() => pending.size > 0); assert.ok(now() - heldAt < 5000); cancellationArmedAt = now(); break;
        case 'cancelled':
          await wait(() => cancelled > 0 && pending.size === 0);
          assert.ok(cancelObservedAt >= cancellationArmedAt && cancelObservedAt - heldAt < 8000, 'Background cancellation must precede the ten-second network timeout');
          holding = false; baseline = logins().length; break;
        case 'recovered': await wait(() => logins().length > baseline); baseline = logins().length; break;
        case 'restart-ready':
          await wait(() => logins().length > baseline); baseline = logins().length;
          initialProcessPID = hdcCall(['-t', target, 'shell', 'pidof', bundle]).trim();
          assert.match(initialProcessPID, /^\d+(?: \d+)*$/); break;
        case 'restore-fixture':
          assert.ok(processTermination);
          restartedProcessPID = hdcCall(['-t', target, 'shell', 'pidof', bundle]).trim();
          assert.match(restartedProcessPID, /^\d+(?: \d+)*$/); assert.notEqual(restartedProcessPID, initialProcessPID);
          processRestartVerified = true; break;
        case 'restart-running': assert.ok(processRestartVerified); await wait(() => logins().length > baseline); break;
      }
      completed.push(action);
      fs.writeFileSync(path.join(evidence, 'progress.json'), JSON.stringify({ source, dirty, phases: completed, nativeSignedRequests: nonces.size, observedAt: new Date().toISOString() }, null, 2));
      outgoing.writeHead(200, { 'Content-Type': 'application/json' });
      outgoing.end(JSON.stringify({ ok: true, server: 'http://127.0.0.1:18800', ...credentials }));
    } catch (error) { fail(error); outgoing.writeHead(500, { 'Content-Type': 'application/json' }); outgoing.end(JSON.stringify({ ok: false })); }
  });
  try {
    await listen(control); await listen(proxy);
    for (const [guest, server] of [[18799, control], [18800, proxy]]) {
      assert.ok(!hdcCall(['fport', 'ls']).includes('tcp:' + guest + ' '), 'Reserved UI port is already in use');
      const mapping = 'tcp:' + guest + ' tcp:' + server.address().port;
      assert.match(hdcCall(['-t', target, 'rport', ...mapping.split(' ')]), /result:OK/); mappings.push(mapping);
    }
    for (const scenario of ['initial', 'restart']) {
      if (scenario === 'restart') {
        const before = hdcCall(['-t', target, 'shell', 'pidof', bundle]).trim();
        if (before) {
          assert.equal(before, initialProcessPID);
          hdcCall(['-t', target, 'shell', 'aa', 'force-stop', bundle]); processTermination = 'force-stop';
        } else { processTermination = 'test-runner-exit'; }
        await wait(() => hdcCall(['-t', target, 'shell', 'pidof', bundle]).trim() === '');
      }
      const args = ['-NoProfile', '-File', path.resolve(__dirname, '../scripts/run-native-ui-tests.ps1'), '-BuildReport', testReport,
        '-AppBuildReport', appReport, '-CommandLineHome', sdk, '-FixtureToken', token, '-Target', target, '-Scenario', scenario,
        '-ReportPath', path.join(evidence, scenario + '.json')];
      await new Promise((resolve, reject) => {
        const log = fs.createWriteStream(path.join(evidence, scenario + '.console.log'));
        const child = spawn(pwsh, args, { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
        const timer = setTimeout(() => child.kill(), 450000);
        child.stdout.on('data', data => log.write(data)); child.stderr.on('data', data => log.write(data));
        child.once('error', () => { clearTimeout(timer); log.end(); reject(new Error('UI runner could not start')); });
        child.once('close', code => { clearTimeout(timer); log.end(); code === 0 ? resolve() : reject(new Error('Native UI runner failed; inspect private log')); });
      });
      if (firstError) throw firstError;
      assert.equal(captures.length, scenario === 'initial' ? 1 : 2, 'Actual quality UI screenshot is required');
    }
    assert.deepEqual(completed, phases); status = 'passed';
  } catch (error) { fail(error); }
  finally {
    try {
      if (hdcCall(['-t', target, 'shell', 'pidof', bundle]).trim()) hdcCall(['-t', target, 'shell', 'aa', 'force-stop', bundle]);
      await wait(() => hdcCall(['-t', target, 'shell', 'pidof', bundle]).trim() === ''); qualityProcessStopped = true;
    } catch { failures.push('quality test process cleanup could not be verified'); }
    for (const response of pending) response.destroy();
    for (const server of [control, proxy]) { try { await close(server); } catch { failures.push('owned UI server cleanup failed'); } }
    for (const mapping of mappings.reverse()) {
      try { if (owns(mapping)) hdcCall(['-t', target, 'fport', 'rm', ...mapping.split(' ')]); assert.ok(!owns(mapping)); }
      catch { failures.push('owned UI mapping cleanup failed'); }
    }
    let productionUnchanged = false;
    try { productionUnchanged = productionFingerprint() === productionBefore; assert.ok(productionUnchanged); }
    catch { failures.push('production Preferences preservation could not be verified'); }
    const summary = { source, dirty, status: firstError || failures.length ? 'failed' : status, bundle, phases: completed, timing,
      nativeSignedRequests: nonces.size, requests, clock: 'host-performance-monotonic-ms', cancelledRequests: cancelled, processRestartVerified, processTermination, initialProcessPID, restartedProcessPID, cleanupErrors: failures,
      productionUnchanged, productionPreferencesSHA256: productionBefore.split(/\s+/)[0], captures,
      qualityProcessStopped, cancellationMilliseconds: cancelObservedAt && heldAt ? cancelObservedAt - heldAt : null,
      scope: 'Actual ArkUI Index and EntryAbility, SDK UI driver, real OS foreground/background and verified process exit/new PID; production 60s interval, actual sensors without automatic authorization; strict disposable Go/MySQL' };
    fs.writeFileSync(path.join(evidence, 'summary.json'), JSON.stringify(summary, null, 2)); console.log(JSON.stringify(summary, null, 2));
  }
  if (firstError) throw firstError;
  assert.equal(failures.length, 0);
}
main().catch(error => { console.error(error.message); process.exitCode = 1; });
