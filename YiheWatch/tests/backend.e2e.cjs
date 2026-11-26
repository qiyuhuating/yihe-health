// Run ONLY against a new disposable backend/database with no real recipients.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const Module = require('node:module');
const http = require('node:http');
const os = require('node:os');
const base = process.env.WATCH_TEST_URL;
const bootstrap = process.env.WATCH_TEST_SETUP_TOKEN;
if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(base || '') || !bootstrap) {
  throw new Error('需要专用本机测试后端 WATCH_TEST_URL 和 WATCH_TEST_SETUP_TOKEN；不要指向现有业务库');
}
const ts = require('typescript');
const source = path.resolve(__dirname, '../entry/src/main/ets/model/Protocol.ets');
const mod = new Module(source, module);
mod._compile(ts.transpileModule(fs.readFileSync(source, 'utf8'), {
  compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS }
}).outputText, source);
const protocol = mod.exports;
const load = require('./load-ets.cjs');
const { signatureBoundary } = require('./signature-fixture.cjs');
const { Request } = require('./rcp-fixture.cjs');
let transportProxy = '';
const signedAttempts = [];
// Real HTTP to the disposable Go server through the production WatchApi.
const { WatchApi } = load('services/WatchApi', { ...signatureBoundary().mocks, '@kit.RemoteCommunicationKit': { rcp: {
  Request,
  createSession: configuration => ({ fetch: async ({ url, method, headers, content }) => {
    signedAttempts.push({ path: new URL(url).pathname, headers: { ...headers } });
    const response = await fetch(transportProxy ? transportProxy + new URL(url).pathname : url, { method, headers, body: content, signal: AbortSignal.timeout(configuration.requestConfiguration.transfer.timeout.transferMs) });
    const text = await response.text();
    return { statusCode: response.status, toString: () => text };
  }, close() {}, cancel() {} })
} } });
const { NativeSampling } = load('model/NativeSampling');
const watch = new WatchApi();
let cookie = '', csrf = '';
const checks = [];
function referenceSignature(body, nonce = crypto.randomBytes(16).toString('hex'), seconds = Math.floor(Date.now() / 1000)) {
  const timestamp = String(seconds), raw = JSON.stringify(body);
  const digest = crypto.createHash('sha256').update(raw).digest('hex');
  return { 'X-Device-Timestamp': timestamp, 'X-Device-Nonce': nonce,
    'X-Device-Signature': crypto.createHmac('sha256', body.secret).update(body.deviceNo.trim().toUpperCase() + ':' + timestamp + ':' + nonce + ':' + digest).digest('hex') };
}
async function request(route, body, headers = {}, expected = 200, signDevice = true) {
  const signature = signDevice && typeof body?.deviceNo === 'string' && typeof body?.secret === 'string' ? referenceSignature(body) : {};
  const response = await fetch(base + route, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json', Cookie: cookie, 'X-CSRF-TOKEN': csrf, ...signature, ...headers },
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(15000)
  });
  const setCookie = response.headers.get('set-cookie');
  if (setCookie) cookie = setCookie.split(';')[0];
  const result = await response.json();
  assert.equal(response.status, expected, route + ' status');
  if (expected === 200 && result.returnCode !== undefined) assert.equal(result.returnCode, '1111', route + ' rejected: ' + (result.message || result.returnCode));
  return result;
}
async function main() {
  assert.equal((await request('/api/v1/service/status')).databaseReady, true);
  const initial = await request('/api/v1/auth/session');
  assert.equal(initial.setupRequired, true, 'test must use an empty disposable database');
  csrf = initial.csrfToken;
  const password = 'Test' + crypto.randomBytes(8).toString('hex');
  await request('/api/v1/setup', { name: '模拟测试', phone: '13900000001', password }, { 'X-Yihe-Desktop': bootstrap });
  await request('/api/v1/auth/login', { phone: '13900000001', password });
  csrf = (await request('/api/v1/auth/session')).csrfToken;
  await request('/api/v1/guardians/create', { customerName: '模拟监护', customerAge: '40', customerSex: '男', customerPhone: '13900000002', customerAddress: '模拟器测试' });
  const customer = (await request('/api/v1/guardians/list', { customerName: '模拟监护', page: 1, pageSize: 20 })).customers[0];
  await request('/api/v1/residents/create', { name: '模拟长者', bothdate: '1950-01-01', sex: '男', phone: '13900000003', room: 'T01', illness: '', warn: '', foodLike: '', foodHate: '', drag: '', customerId: String(customer.customerId) });
  const elder = (await request('/api/v1/residents/list', { name: '模拟长者', page: 1, pageSize: 20 })).oldPeople[0];
  const device = await request('/api/v1/device/create', { alias: 'WATCH5 模拟协议测试', model: 'WATCH5-SIM' });
  const credentials = { deviceNo: device.deviceNo, secret: device.secret };
  assert.equal((await request('/api/v1/device/login', credentials, {}, 401, false)).code, 'DEVICE_SIGNATURE_INVALID', 'backend must require signed devices');
  const replayHeaders = referenceSignature(credentials);
  await request('/api/v1/device/login', credentials, replayHeaders);
  assert.equal((await request('/api/v1/device/login', credentials, replayHeaders, 409)).code, 'NONCE_REPLAY');
  await request('/api/v1/device/login', credentials, referenceSignature(credentials, undefined, Math.floor(Date.now() / 1000) - 601), 401);
  await request('/api/v1/device/login', { ...credentials, secret: credentials.secret + 'x' }, referenceSignature(credentials), 401);
  checks.push('strict backend rejected unsigned, expired and modified-body signatures plus replayed nonce');
  const before = await request('/api/v1/device/login', credentials);
  assert.equal(before.bound, 0);
  checks.push('new device authenticated and reported unbound');
  const code = await request('/api/v1/device/createBindCode', { oldId: String(elder.oldId), expireDays: '1' });
  const config = protocol.normalizeConfig(base, device.deviceNo);
  await watch.bind(config, device.secret, code.code);
  const live = protocol.bindingFrom(config, await watch.login(config, device.secret));
  assert.equal(live.residentId, elder.oldId);
  checks.push('bind code attached device to test elder');
  assert.equal((await watch.heartbeat(config, device.secret)).bound, 1);
  checks.push('heartbeat acknowledged');
  const samples = protocol.makeSamples(device.deviceNo, 'e2e-fixed-record', new Date(), 72, 1280);
  const queue = protocol.enqueue(undefined, live, samples);
  const body = protocol.batchRequest(queue, live, device.secret);
  assert.equal((await request('/api/v1/device/healthBatch', body)).accepted, 2);
  assert.equal((await request('/api/v1/device/healthBatch', body)).duplicates, 2);
  const changes = await request('/api/v1/health/changes', { oldId: elder.oldId, after: 0, limit: 100 });
  assert.equal(changes.changes.length, 2);
  checks.push('two simulated samples persisted; retry acknowledged two duplicates without extra changes');
  const native = new NativeSampling();
  const at = Date.now();
  assert.deepEqual(native.accept(live, 'native-anchor', { metric: 'steps', value: 100, receivedAt: at - 1000, sensorTimestamp: 1000000 }), []);
  const nativeSamples = native.accept(live, 'native-steps', { metric: 'steps', value: 105, receivedAt: at, sensorTimestamp: 2000000 })
    .concat(native.accept(live, 'native-hr', { metric: 'heart_rate', value: 74, receivedAt: at, sensorTimestamp: 2000000 }));
  const nativeQueue = protocol.enqueue(undefined, live, nativeSamples);
  // Real disk backing at the Preferences boundary, not a claim about device flash.
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'yihe-watch-e2e-'));
  const prefsFile = path.join(temporary, 'preferences.json');
  const { cryptoBoundary } = require('./storage-fixture.cjs');
  const { WatchStore } = load('services/WatchStore', { ...cryptoBoundary().mocks, '@kit.ArkData': { preferences: { getPreferencesSync: () => {
    const values = fs.existsSync(prefsFile) ? JSON.parse(fs.readFileSync(prefsFile, 'utf8')) : {};
    return { hasSync: key => Object.hasOwn(values, key), getSync: (key, fallback) => values[key] ?? fallback, putSync: (key, value) => { values[key] = value; },
      deleteSync: key => { delete values[key]; }, flush: async () => fs.writeFileSync(prefsFile, JSON.stringify(values)) };
  } } } });
  let forwarded = 0;
  let upstreamAck;
  const proxy = http.createServer(async (incoming, outgoing) => {
    try {
      assert.equal(incoming.url, '/api/v1/device/healthBatch');
      let payload = '';
      for await (const chunk of incoming) payload += chunk;
      forwarded++;
      const response = await fetch(base + incoming.url, { method: 'POST', headers: { 'Content-Type': 'application/json',
        'X-Device-Timestamp': incoming.headers['x-device-timestamp'], 'X-Device-Nonce': incoming.headers['x-device-nonce'],
        'X-Device-Signature': incoming.headers['x-device-signature'] }, body: payload, signal: AbortSignal.timeout(10000) });
      assert.equal(response.status, 200);
      upstreamAck = await response.json();
      // The database committed, but the watch never receives its acknowledgement.
      outgoing.destroy();
    } catch (error) { outgoing.destroy(error); }
  });
  try {
    const store = await WatchStore.open({});
    await store.saveConfig(config);
    await store.saveCredential(config, device.secret);
    await store.saveBatch(nativeQueue);
    const persisted = fs.readFileSync(prefsFile, 'utf8');
    assert.ok(!persisted.includes(device.secret), 'disk-backed preferences exposed the device credential');
    for (const sample of nativeQueue.samples) assert.ok(!persisted.includes(sample.recordId), 'disk-backed preferences exposed health records');
    await new Promise(resolve => proxy.listen(0, '127.0.0.1', resolve));
    transportProxy = 'http://127.0.0.1:' + proxy.address().port;
    await assert.rejects(watch.upload(config, protocol.batchRequest(store.batch(), live, device.secret)));
    transportProxy = '';
    assert.equal(forwarded, 1, 'a failed POST must not be replayed automatically');
    assert.equal(upstreamAck.accepted, 2, 'failure must occur after the real backend committed');
    const recovered = await WatchStore.open({});
    assert.deepEqual(recovered.batch(), nativeQueue, 'restart must retain the original IDs, values and binding');
    assert.equal(recovered.credential(config), device.secret);
    const retry = await watch.upload(config, protocol.batchRequest(recovered.batch(), live, recovered.credential(config)));
    assert.equal(retry.accepted, 2);
    assert.equal(retry.duplicates, 2);
    await recovered.saveBatch(undefined);
    assert.equal((await WatchStore.open({})).batch(), undefined);
    checks.push('real socket loss after database commit retained the encrypted disk queue and scoped credential; restart retry deduplicated original records before clearing (Node crypto SDK adapter, not native HUKS)');
  } finally {
    transportProxy = '';
    proxy.closeAllConnections();
    await new Promise(resolve => proxy.close(resolve));
    if (fs.existsSync(prefsFile)) fs.unlinkSync(prefsFile);
    fs.rmdirSync(temporary);
  }
  const latest = await request('/api/v1/health/latest', { oldId: elder.oldId });
  const nativeRecords = latest.items.filter(item => item.sample.source === 'yihe-harmonyos');
  assert.equal(nativeRecords.length, 2);
  assert.equal(nativeRecords.find(item => item.sample.metric === 'steps').sample.value, 5);
  assert.ok(nativeRecords.every(item => item.sample.metadata.timestampBasis === 'app-received'));
  checks.push('production HarmonyOS transport negotiated, uploaded native-shaped heart/step samples, deduplicated retry and read latest with provenance');
  const sos = { ...credentials, requestId: 'watch-sim-e2e-same-press', bindingVersion: live.version };
  await request('/api/v1/device/sos', { ...credentials, requestId: 'missing-binding-version' }, {}, 400);
  await request('/api/v1/device/sos', { ...credentials, bindingVersion: live.version }, {}, 400);
  const first = await watch.sos(config, sos);
  const retry = await watch.sos(config, sos);
  assert.equal(retry.incidentId, first.incidentId);
  assert.equal(retry.duplicate, true);
  assert.equal(first.notificationState, 'no_recipient');
  checks.push('same SOS request ID reused one incident; no real notification recipient');
  await request('/api/v1/device/unbind', { deviceId: String(device.deviceId) });
  const code2 = await request('/api/v1/device/createBindCode', { oldId: String(elder.oldId), expireDays: '1' });
  await request('/api/v1/device/bind', { ...credentials, code: code2.code });
  const rebound = protocol.bindingFrom(config, await request('/api/v1/device/login', credentials));
  assert.notEqual(rebound.version, live.version);
  assert.throws(() => protocol.batchRequest(queue, rebound, device.secret), error => error.code === 'EPOCH_EXPIRED');
  assert.equal((await request('/api/v1/device/healthBatch', body, {}, 409)).code, 'EPOCH_EXPIRED');
  assert.equal((await request('/api/v1/device/sos', sos, {}, 409)).code, 'EPOCH_EXPIRED');
  await assert.rejects(watch.upload(config, body), error => error.code === 'EPOCH_EXPIRED');
  const { storeFixture } = require('./storage-fixture.cjs');
  const epochStore = await storeFixture().open();
  await epochStore.saveBatch(queue);
  await epochStore.saveSOS([{ binding: live, requestId: sos.requestId }]);
  assert.equal(await epochStore.discardExpiredBatch(rebound), true);
  assert.equal(epochStore.batch(), undefined);
  assert.equal(epochStore.pendingSOS()[0].requestId, sos.requestId);
  const currentSOS = await request('/api/v1/device/sos', { ...credentials, requestId: 'watch-sim-e2e-rebound', bindingVersion: rebound.version });
  assert.notEqual(currentSOS.incidentId, first.incidentId);
  checks.push('rebind returned EPOCH_EXPIRED through the production transport; verified handshake cleared only old telemetry and retained SOS history');
  checks.push('rebound device rejected old SOS retry and accepted a new SOS for the current binding');
  const automaticStore = await storeFixture().open();
  await automaticStore.saveConfig(config);
  await automaticStore.saveCredential(config, device.secret);
  await automaticStore.rememberBinding(rebound);
  await automaticStore.saveMonitoringEnabled(true);
  let dialogs = 0, ticks = 0;
  const { HealthSensors } = load('services/HealthSensors', {
    '@kit.SensorServiceKit': { sensor: {
      SensorId: { HEART_RATE: 1, PEDOMETER: 2 },
      getSensorList: async () => [{ sensorId: 1 }, { sensorId: 2 }],
      once: (id, callback) => queueMicrotask(() => {
        if (id === 1) { ticks++; callback({ heartRate: 73, timestamp: ticks * 1e9 }); }
        else callback({ steps: 100 + ticks * 5, timestamp: ticks * 1e9 });
      }), off() {}
    } }, '@kit.AbilityKit': { abilityAccessCtrl: { createAtManager: () => ({
      checkAccessToken: async () => 0,
      requestPermissionsFromUser: async () => { dialogs++; throw new Error('Automatic cycles must not open permission dialogs'); }
    }) } }
  });
  const { WatchSynchronizer } = load('services/WatchSynchronizer');
  const { AutoMonitor } = load('services/AutoMonitor');
  const synchronizer = new WatchSynchronizer(automaticStore, watch, new HealthSensors(), { applicationInfo: { accessTokenId: 100 } });
  const results = [];
  let completed, failed;
  const finished = new Promise((resolve, reject) => { completed = resolve; failed = reject; });
  const automatic = new AutoMonitor(async () => {
    try {
      results.push(await synchronizer.cycle(config, device.secret));
      if (results.length === 2) completed();
      return 'ok';
    } catch (error) { failed(error); return 'halt'; }
  }, 1000); // Accelerate only the test clock policy; production defaults to 60 s.
  let deadline;
  try {
    automatic.setVisible(true); automatic.setEnabled(automaticStore.monitoringEnabled());
    await Promise.race([finished, new Promise((_, reject) => { deadline = setTimeout(() => reject(new Error('Automatic cycles did not complete')), 15000); })]);
  } finally { automatic.setEnabled(false); synchronizer.suspend(); clearTimeout(deadline); }
  assert.deepEqual(results.map(item => item.sampled), [1, 2]);
  assert.ok(results.every(item => item.queued === 0 && !item.offline));
  assert.equal(automaticStore.batch(), undefined); assert.equal(dialogs, 0);
  const automaticChanges = await request('/api/v1/health/changes', { oldId: elder.oldId, after: 0, limit: 100 });
  assert.equal(automaticChanges.changes.filter(item => item.sample.source === 'yihe-harmonyos').length, 5);
  checks.push('production AutoMonitor and WatchSynchronizer ran two automatic controlled-sensor cycles through real HTTP/Go/MySQL without per-cycle clicks or permission dialogs; SDK/crypto adapters are test boundaries');
  const nonces = signedAttempts.map(item => item.headers['X-Device-Nonce']);
  assert.equal(new Set(nonces).size, nonces.length, 'each transport attempt must use a fresh nonce, including lost-ack retries');
  for (const path of ['login', 'bind', 'heartbeat', 'healthBatch', 'sos']) assert.ok(signedAttempts.some(item => item.path.endsWith('/' + path)));
  checks.push('production HarmonyOS transport signed login, bind, heartbeat, healthBatch and SOS; lost-ack retries used fresh transport nonces with stable durable record IDs');
  console.log(JSON.stringify({ passed: checks.length, checks }, null, 2));
}
main().catch(error => { console.error(error.message); process.exitCode = 1; });
