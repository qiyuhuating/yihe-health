// Only use with an empty disposable loopback Go/MySQL backend. The SDK suite
// replaces physical sensors; HMAC, HTTP, HUKS and durable storage remain native.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const crypto = require('node:crypto');
const { spawn, spawnSync } = require('node:child_process');
const base = process.env.WATCH_TEST_URL, bootstrap = process.env.WATCH_TEST_SETUP_TOKEN;
const sdk = process.env.WATCH_NATIVE_COMMANDLINE_HOME, buildReport = process.env.WATCH_NATIVE_BUILD_REPORT;
const evidence = process.env.WATCH_NATIVE_REPORT_DIR, pwsh = process.env.WATCH_NATIVE_PWSH;
const target = process.env.WATCH_NATIVE_TARGET || '127.0.0.1:5555';
if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(base || '') || !bootstrap || !sdk || !buildReport || !evidence || !pwsh || !/^[A-Za-z0-9_.:-]+$/.test(target)) {
  throw new Error('Dedicated loopback backend, SDK/build report, private evidence directory and PowerShell 7 are required');
}
const hdc = path.join(sdk, 'sdk/default/openharmony/toolchains/hdc.exe');
const repo = path.resolve(__dirname, '../..');
const build = JSON.parse(fs.readFileSync(buildReport, 'utf8').replace(/^\uFEFF/, ''));
const git = args => {
  const result = spawnSync('git', args, { cwd: repo, encoding: 'utf8', windowsHide: true });
  if (result.status !== 0) throw new Error('Cannot verify native source');
  return result.stdout.trim();
};
const source = git(['rev-parse', 'HEAD']);
assert.equal(build.source, source, 'test HAP must match current commit');
const dirty = !!git(['status', '--porcelain', '--untracked-files=no']) ||
  !!git(['status', '--porcelain', '--untracked-files=normal', '--', 'YiheWatch']);
if (dirty || build.dirty) throw new Error('Formal native integration requires clean source');
fs.mkdirSync(evidence, { recursive: true });
let cookie = '', csrf = '';
async function api(route, body, extra = {}, expected = 200) {
  const response = await fetch(base + route, { method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json', Cookie: cookie, 'X-CSRF-TOKEN': csrf, ...extra },
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(15000) });
  const set = response.headers.get('set-cookie'); if (set) cookie = set.split(';')[0];
  const result = await response.json(); assert.equal(response.status, expected, route + ' status');
  if (expected === 200 && result.returnCode !== undefined) assert.equal(result.returnCode, '1111', route + ' rejected');
  return result;
}
function signed(body) {
  const timestamp = String(Math.floor(Date.now() / 1000)), nonce = crypto.randomBytes(16).toString('hex');
  const digest = crypto.createHash('sha256').update(JSON.stringify(body)).digest('hex');
  return { 'X-Device-Timestamp': timestamp, 'X-Device-Nonce': nonce,
    'X-Device-Signature': crypto.createHmac('sha256', body.secret).update(body.deviceNo.trim().toUpperCase() + ':' + timestamp + ':' + nonce + ':' + digest).digest('hex') };
}
function callHdc(args) {
  const result = spawnSync(hdc, args, { encoding: 'utf8', timeout: 15000, windowsHide: true });
  if (result.status !== 0) throw new Error('HDC operation failed');
  return result.stdout;
}
function ownsMapping(mapping) {
  const expected = target + ' ' + mapping + ' [Reverse]';
  return callHdc(['fport', 'ls']).split(/\r?\n/).some(line => line.trim().replace(/\s+/g, ' ') === expected);
}
async function bodyOf(request, limit) {
  let size = 0; const chunks = [];
  for await (const chunk of request) { size += chunk.length; if (size > limit) throw new Error('Test request too large'); chunks.push(chunk); }
  return Buffer.concat(chunks);
}
const listen = server => new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
const close = server => new Promise(resolve => { server.closeAllConnections(); server.close(resolve); });
async function main() {
  assert.ok(callHdc(['list', 'targets']).split(/\r?\n/).map(item => item.trim()).includes(target), 'native target must already be connected');
  assert.equal((await api('/api/v1/service/status')).databaseReady, true);
  const session = await api('/api/v1/auth/session');
  assert.equal(session.setupRequired, true, 'native integration must never use an existing business database'); csrf = session.csrfToken;
  const password = 'Native' + crypto.randomBytes(7).toString('hex');
  await api('/api/v1/setup', { name: '原生隔离测试', phone: '13900000001', password }, { 'X-Yihe-Desktop': bootstrap });
  await api('/api/v1/auth/login', { phone: '13900000001', password }); csrf = (await api('/api/v1/auth/session')).csrfToken;
  await api('/api/v1/guardians/create', { customerName: '原生模拟监护', customerAge: '40', customerSex: '男', customerPhone: '13900000002', customerAddress: 'native test only' });
  const guardian = (await api('/api/v1/guardians/list', { customerName: '原生模拟监护', page: 1, pageSize: 20 })).customers[0];
  const elders = [];
  for (let i = 1; i <= 2; i++) {
    const name = '原生测试长者' + i;
    await api('/api/v1/residents/create', { name, bothdate: '1950-01-01', sex: '男', phone: '1390000000' + (i + 2), room: 'N0' + i,
      illness: '', warn: '', foodLike: '', foodHate: '', drag: '', customerId: String(guardian.customerId) });
    elders.push((await api('/api/v1/residents/list', { name, page: 1, pageSize: 20 })).oldPeople[0]);
  }
  const device = await api('/api/v1/device/create', { alias: 'Native WATCH controlled test', model: 'WATCH5-NATIVE-TEST' });
  const credentials = { deviceNo: device.deviceNo, secret: device.secret };
  assert.equal((await api('/api/v1/device/login', credentials, {}, 401)).code, 'DEVICE_SIGNATURE_INVALID', 'strict signing must be enabled');
  async function bind(elder) {
    const code = await api('/api/v1/device/createBindCode', { oldId: String(elder.oldId), expireDays: '1' });
    const body = { ...credentials, code: code.code }; await api('/api/v1/device/bind', body, signed(body));
    return api('/api/v1/device/login', credentials, signed(credentials));
  }
  const initial = await bind(elders[0]);
  async function records(elder) {
    const result = await api('/api/v1/health/changes', { oldId: elder.oldId, after: 0, limit: 200 });
    assert.equal(result.hasMore, false);
    return result.changes.filter(item => item.sample.source === 'yihe-harmonyos' && item.sample.sourceDevice === device.deviceNo);
  }
  const token = crypto.randomBytes(32).toString('hex'), storeName = 'yihe_native_e2e_' + crypto.randomBytes(6).toString('hex');
  const phases = ['fixture', 'automatic', 'hidden', 'resumed', 'offline', 'online', 'uploaded', 'lose-ack', 'lost-ack-check', 'dedup-check', 'offline-again', 'rebind', 'rebound', 'sos'];
  const completed = [], nonces = new Set(), routes = new Set(), forwards = [];
  let mode = 'online', lost, rebound, offlineIDs = [], staleIDs = [], sawDuplicate = false, sos = [], firstError;
  let rejectedTransportReplays = 0;
  const fail = error => { firstError ||= error; };
  const proxy = http.createServer(async (incoming, outgoing) => {
    try {
      assert.equal(incoming.method, 'POST');
      assert.match(incoming.url, /^\/api\/v1\/device\/(login|heartbeat|healthBatch|sos)$/);
      const raw = await bodyOf(incoming, 1048576), body = JSON.parse(raw);
      assert.ok(body.deviceNo === device.deviceNo && body.secret === device.secret, 'Native request credential mismatch');
      const nonce = incoming.headers['x-device-nonce'], timestamp = incoming.headers['x-device-timestamp'];
      assert.match(timestamp || '', /^\d+$/);
      assert.ok(Math.abs(Math.floor(Date.now() / 1000) - Number(timestamp)) <= 300, 'Native timestamp outside server acceptance window');
      assert.match(nonce || '', /^[a-f0-9]{32}$/);
      const transportReplay = nonces.has(nonce);
      if (!transportReplay) nonces.add(nonce);
      const digest = crypto.createHash('sha256').update(raw).digest('hex');
      const expected = crypto.createHmac('sha256', device.secret).update(device.deviceNo + ':' + timestamp + ':' + nonce + ':' + digest).digest('hex');
      assert.equal(incoming.headers['x-device-signature'], expected); routes.add(incoming.url.split('/').pop());
      if (transportReplay) {
        // The native HTTP stack may retry the same POST after an empty socket
        // close. Only this exact injected lost-ACK attempt may do so, and the
        // real backend must reject it. The later app retry needs a fresh nonce.
        assert.equal(mode, 'lose-ack', 'transport replay outside the injected outage');
        assert.ok(lost && incoming.url.endsWith('/healthBatch'));
        assert.equal(nonce, lost.nonce); assert.equal(digest, lost.digest); assert.equal(timestamp, lost.timestamp);
      }
      if (mode === 'offline') { outgoing.writeHead(503, { 'Content-Type': 'application/json' }); outgoing.end(JSON.stringify({ code: 'TEST_OFFLINE', message: 'Controlled native network outage' })); return; }
      const response = await fetch(base + incoming.url, { method: 'POST', headers: { 'Content-Type': 'application/json',
        'X-Device-Nonce': nonce, 'X-Device-Timestamp': timestamp, 'X-Device-Signature': incoming.headers['x-device-signature'] }, body: raw, signal: AbortSignal.timeout(15000) });
      const text = await response.text(), result = JSON.parse(text);
      if (transportReplay) {
        assert.equal(response.status, 409); assert.equal(result.code, 'NONCE_REPLAY');
        rejectedTransportReplays++; outgoing.destroy(); return;
      }
      if (incoming.url.endsWith('/healthBatch')) {
        assert.equal(response.status, 200); assert.equal(result.accepted, body.samples.length);
        if (result.duplicates > 0) sawDuplicate = true;
        if (mode === 'lose-ack') {
          assert.equal(lost, undefined);
          lost = { ids: body.samples.map(item => item.recordId), accepted: result.accepted, nonce, digest, timestamp };
          outgoing.destroy(); return;
        }
      }
      if (incoming.url.endsWith('/sos')) sos.push({ requestId: body.requestId, incidentId: result.incidentId, duplicate: result.duplicate, notification: result.notificationState });
      outgoing.writeHead(response.status, { 'Content-Type': 'application/json' }); outgoing.end(text);
    } catch (error) { fail(error); outgoing.destroy(); }
  });
  const control = http.createServer(async (incoming, outgoing) => {
    try {
      assert.equal(incoming.url, '/control'); assert.equal(incoming.method, 'POST');
      assert.equal(incoming.headers.authorization, 'Bearer ' + token);
      const body = JSON.parse(await bodyOf(incoming, 16384)); assert.equal(body.action, phases[completed.length], 'native phases must run in order');
      const original = await records(elders[0]); let extra = {};
      switch (body.action) {
        case 'fixture': extra = { server: 'http://127.0.0.1:18798', ...credentials, storeName, residentId: elders[0].oldId, bindingVersion: initial.bindingVersion }; break;
        case 'automatic': case 'hidden': assert.equal(original.length, 3); assert.equal(original.find(item => item.sample.metric === 'steps').sample.value, 5); break;
        case 'resumed': case 'offline': assert.equal(original.length, 4); if (body.action === 'offline') mode = 'offline'; break;
        case 'online': assert.equal(original.length, 4); assert.equal(body.ids.length, 2); offlineIDs = body.ids; mode = 'online'; break;
        case 'uploaded': assert.equal(original.length, 6); for (const id of offlineIDs) assert.ok(original.some(item => item.sample.recordId === id)); break;
        case 'lose-ack': assert.equal(original.length, 6); mode = 'lose-ack'; break;
        case 'lost-ack-check': assert.equal(original.length, 7); assert.equal(lost.accepted, 1); assert.deepEqual(body.ids, lost.ids); mode = 'online'; break;
        case 'dedup-check': assert.equal(original.length, 7); assert.equal(sawDuplicate, true); break;
        case 'offline-again': assert.equal(original.length, 7); mode = 'offline'; break;
        case 'rebind':
          assert.equal(original.length, 7); assert.equal(body.ids.length, 2); staleIDs = body.ids;
          await api('/api/v1/device/unbind', { deviceId: String(device.deviceId) }); rebound = await bind(elders[1]);
          assert.ok(rebound.bindingVersion > initial.bindingVersion); mode = 'online';
          extra = { residentId: elders[1].oldId, bindingVersion: rebound.bindingVersion }; break;
        case 'rebound': {
          assert.equal(original.length, 7); const current = await records(elders[1]); assert.equal(current.length, 1);
          for (const id of staleIDs) assert.ok(!original.concat(current).some(item => item.sample.recordId === id));
          assert.equal(current[0].bindingVersion, rebound.bindingVersion); break;
        }
        case 'sos':
          assert.equal(sos.length, 2); assert.equal(sos[0].incidentId, sos[1].incidentId); assert.equal(sos[1].duplicate, true);
          assert.equal(sos[0].notification, 'no_recipient'); break;
      }
      completed.push(body.action); outgoing.writeHead(200, { 'Content-Type': 'application/json' }); outgoing.end(JSON.stringify({ ok: true, ...extra }));
    } catch (error) { fail(error); outgoing.writeHead(500, { 'Content-Type': 'application/json' }); outgoing.end(JSON.stringify({ ok: false })); }
  });
  let status = 'failed', operationError;
  const cleanupErrors = [];
  try {
    await listen(control); await listen(proxy);
    for (const [guest, server] of [[18797, control], [18798, proxy]]) {
      const mapping = 'tcp:' + guest + ' tcp:' + server.address().port;
      assert.ok(!callHdc(['fport', 'ls']).includes('tcp:' + guest + ' '), 'reserved native test port already in use');
      assert.match(callHdc(['-t', target, 'rport', 'tcp:' + guest, 'tcp:' + server.address().port]), /result:OK/); forwards.push(mapping);
    }
    const args = ['-NoProfile', '-File', path.resolve(__dirname, '../scripts/run-native-tests.ps1'), '-BuildReport', path.resolve(buildReport),
      '-CommandLineHome', sdk, '-Target', target, '-Suite', 'backend', '-FixtureToken', token, '-ReportPath', path.join(evidence, 'native-backend-runtime.json')];
    let runnerError;
    await new Promise((resolve, reject) => {
      const log = fs.createWriteStream(path.join(evidence, 'native-backend-console.log'));
      const child = spawn(pwsh, args, { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
      const timeout = setTimeout(() => { child.kill(); }, 210000);
      child.stdout.on('data', data => log.write(data)); child.stderr.on('data', data => log.write(data));
      child.once('error', () => { clearTimeout(timeout); log.end(); reject(new Error('Cannot start native test runner')); });
      child.once('close', code => { clearTimeout(timeout); log.end(); code === 0 ? resolve() : reject(new Error('Native backend runner failed; inspect private runtime log')); });
    }).catch(error => { runnerError = error; });
    if (firstError) throw firstError;
    if (runnerError) throw runnerError;
    assert.deepEqual(completed, phases); for (const route of ['login', 'heartbeat', 'healthBatch', 'sos']) assert.ok(routes.has(route));
    status = 'passed';
  } catch (error) {
    operationError = error;
  } finally {
    for (const server of [control, proxy]) {
      try { await close(server); } catch { cleanupErrors.push('owned HTTP server cleanup failed'); }
    }
    for (const mapping of forwards.reverse()) {
      try {
        if (ownsMapping(mapping)) callHdc(['-t', target, 'fport', 'rm', ...mapping.split(' ')]);
        assert.ok(!ownsMapping(mapping), 'owned native mapping cleanup failed');
      } catch { cleanupErrors.push('owned native mapping cleanup failed'); }
    }
    const summary = { source, dirty: dirty || build.dirty, status: cleanupErrors.length ? 'failed' : status,
      cleanupErrors, phases: completed, nativeSignedRequests: nonces.size, rejectedTransportReplays,
      scope: 'actual HarmonyOS HMAC/HTTP/HUKS/Preferences and production scheduler/synchronizer; controlled sensors; dedicated real Go/MySQL; no real recipients' };
    fs.writeFileSync(path.join(evidence, 'summary.json'), JSON.stringify(summary, null, 2));
    console.log(JSON.stringify(summary, null, 2));
  }
  if (operationError) throw operationError;
  if (cleanupErrors.length) throw new Error(cleanupErrors.join('; '));
}
main().catch(error => { console.error(error.message); process.exitCode = 1; });
