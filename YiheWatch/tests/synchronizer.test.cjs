const assert = require('node:assert/strict');
const test = require('node:test');
const load = require('./load-ets.cjs');
const { storeFixture } = require('./storage-fixture.cjs');
const { signatureBoundary } = require('./signature-fixture.cjs');
const { Request, reply } = require('./rcp-fixture.cjs');
const p = load('model/Protocol');
const config = p.normalizeConfig('https://care.example.com', 'WATCH001');
const secret = 'authenticated-device-secret-123456';
const settle = () => new Promise(resolve => setImmediate(resolve));

async function fixture(context) {
  context.mock.timers.enable({ apis: ['Date'], now: 1800000000000 });
  const storage = storeFixture(); const store = await storage.open();
  const state = { offline: false, epoch: 1, authStatus: 200, dialogs: 0, listeners: 0, loginGate: undefined,
    steps: 100, sensorTime: 1, permissions: 0, lostAck: false, failUploads: false, uploadStatus: 200, calls: [], records: new Map(), duplicates: 0 };
  const requests = new Set();
  const network = { rcp: { Request, createSession: () => {
    const request = { fetch: async ({ url, content }) => {
      const path = new URL(url).pathname.split('/').pop(), body = JSON.parse(content);
      state.calls.push({ path, body });
      if (state.offline) throw new Error('network offline');
      if (path === 'login') {
        if (state.loginGate) await state.loginGate;
        if (state.authStatus !== 200 || body.secret !== secret) return reply(401, { returnCode: '0000', code: 'UNAUTHORIZED', message: 'unauthorized' });
        return reply(200, { returnCode: '1111', bound: 1, residentId: 7, bindingVersion: state.epoch, protocolVersion: 1, maxBatchSize: 200 });
      }
      assert.equal(path, 'healthBatch');
      if (state.uploadStatus !== 200) return reply(state.uploadStatus, { returnCode: '0000', code: 'DEVICE_AUTH_FAILED' });
      if (state.failUploads) throw new Error('upload connection failure');
      if (body.bindingVersion !== state.epoch) return reply(409, { returnCode: '0000', code: 'EPOCH_EXPIRED' });
      let duplicates = 0;
      for (const sample of body.samples) {
        if (state.records.has(sample.recordId)) duplicates++;
        else state.records.set(sample.recordId, sample);
      }
      state.duplicates += duplicates;
      if (state.lostAck) { state.lostAck = false; throw new Error('connection lost after commit'); }
      return reply(200, { returnCode: '1111', accepted: body.samples.length, duplicates });
    }, close: () => requests.delete(request), cancel() {} };
    requests.add(request); return request;
  } } };
  const sensors = { SensorId: { HEART_RATE: 1, PEDOMETER: 2 },
    getSensorList: async () => [{ sensorId: 1 }, { sensorId: 2 }],
    once: (id, callback) => {
      state.listeners++;
      queueMicrotask(() => callback(id === 1 ? { heartRate: 72, timestamp: state.sensorTime * 1e9 } : { steps: state.steps, timestamp: state.sensorTime * 1e9 }));
    }, off() {} };
  const { WatchApi } = load('services/WatchApi', { ...signatureBoundary().mocks, '@kit.RemoteCommunicationKit': network });
  const { HealthSensors } = load('services/HealthSensors', {
    '@kit.SensorServiceKit': { sensor: sensors }, '@kit.AbilityKit': { abilityAccessCtrl: { createAtManager: () => ({
      checkAccessToken: async () => state.permissions,
      requestPermissionsFromUser: async () => { state.dialogs++; return { authResults: [state.permissions, state.permissions] }; }
    }) } }
  });
  const { WatchSynchronizer } = load('services/WatchSynchronizer');
  const api = new WatchApi(), sensor = new HealthSensors();
  const create = currentStore => new WatchSynchronizer(currentStore, api, sensor, { applicationInfo: { accessTokenId: 100 } });
  const sync = create(store);
  const advance = (seconds = 60) => { context.mock.timers.tick(seconds * 1000); state.sensorTime += seconds; state.steps += 5; };
  return { storage, store, state, sync, advance, create, requests };
}

test('automatic cycles sample actual production sensor adapter and upload without clicks or permission dialogs', async context => {
  const f = await fixture(context);
  const first = await f.sync.cycle(config, secret);
  assert.equal(first.sampled, 1); assert.equal(first.uploaded, 1); assert.equal(first.queued, 0);
  f.advance(); const second = await f.sync.cycle(config, secret);
  assert.equal(second.sampled, 2); assert.equal(second.uploaded, 2);
  assert.equal(f.state.records.size, 3); assert.equal(f.state.dialogs, 0);
  const step = [...f.state.records.values()].find(item => item.metric === 'steps');
  assert.equal(step.value, 5); assert.equal(step.semantics, 'interval');
  assert.equal(f.requests.size, 0);
});

test('offline capture requires scoped verified credentials and preserves encrypted IDs through restart/reconnect', async context => {
  const f = await fixture(context); await f.sync.authenticate(config, secret);
  f.state.offline = true;
  const offline = await f.sync.cycle(config, secret); assert.equal(offline.offline, true); assert.equal(offline.queued, 1);
  const ids = f.store.batch().samples.map(item => item.recordId);
  assert.ok(!JSON.stringify(f.storage.values).includes(ids[0]));
  const recovered = await f.storage.open(); const restarted = f.create(recovered);
  f.advance(); await restarted.cycle(config, recovered.credential(config));
  assert.ok(recovered.batch().samples.some(item => item.recordId === ids[0]));
  f.state.offline = false; f.advance(); const connected = await restarted.cycle(config, secret);
  assert.equal(connected.offline, false); assert.equal(connected.queued, 0);
  for (const id of ids) assert.ok(f.state.records.has(id));
  assert.equal(f.state.dialogs, 0);
});

test('new unverified device or edited credential cannot manufacture offline ownership', async context => {
  const f = await fixture(context); f.state.offline = true;
  await assert.rejects(f.sync.cycle(config, secret), error => error.code === 'NETWORK_ERROR');
  assert.equal(f.state.listeners, 0); assert.equal(f.store.batch(), undefined);
  f.state.offline = false; await f.sync.authenticate(config, secret); f.state.offline = true;
  await assert.rejects(f.sync.cycle({ ...config, deviceNo: 'OTHER' }, secret));
  await assert.rejects(f.sync.cycle(config, 'a-different-unverified-secret-123'));
  assert.equal(f.state.listeners, 0);
});

test('revoked authentication stops offline ownership without deleting pending samples or SOS', async context => {
  const f = await fixture(context); await f.sync.authenticate(config, secret);
  f.state.offline = true; await f.sync.cycle(config, secret);
  const pending = f.store.batch(); const binding = pending.binding;
  await f.store.saveSOS([{ binding, requestId: 'preserved-sos' }]);
  f.state.offline = false; f.state.authStatus = 401; f.advance();
  const listeners = f.state.listeners;
  await assert.rejects(f.sync.cycle(config, secret), error => error.status === 401);
  assert.equal(f.store.verifiedBinding(config), undefined);
  assert.deepEqual(f.store.batch(), pending); assert.equal(f.store.pendingSOS()[0].requestId, 'preserved-sos');
  f.state.offline = true; f.advance(); await assert.rejects(f.sync.cycle(config, secret));
  assert.equal(f.state.listeners, listeners);
});

test('revocation between login and upload invalidates offline ownership and retains the durable queue', async context => {
  for (const status of [401, 403]) {
    const f = await fixture(context);
    f.state.uploadStatus = status;
    await assert.rejects(f.sync.cycle(config, secret), error => error.status === status);
    const pending = f.store.batch();
    assert.equal(pending.samples.length, 1);
    assert.equal(f.store.verifiedBinding(config), undefined);
    const listeners = f.state.listeners;
    f.state.offline = true; f.advance();
    await assert.rejects(f.sync.cycle(config, secret), error => error.code === 'NETWORK_ERROR');
    assert.deepEqual(f.store.batch(), pending); assert.equal(f.state.listeners, listeners);
    context.mock.timers.reset();
  }
});

test('verified rebind discards only stale telemetry before new owner sampling', async context => {
  const f = await fixture(context); await f.sync.authenticate(config, secret);
  const oldBinding = f.store.verifiedBinding(config);
  await f.store.saveSOS([{ binding: oldBinding, requestId: 'old-sos' }]);
  f.state.offline = true; await f.sync.cycle(config, secret);
  const oldIDs = f.store.batch().samples.map(item => item.recordId);
  f.state.epoch = 2; f.state.offline = false; f.advance();
  const result = await f.sync.cycle(config, secret);
  assert.equal(result.binding.version, 2); assert.equal(result.sampled, 1);
  for (const id of oldIDs) assert.ok(!f.state.records.has(id));
  assert.equal(f.store.pendingSOS()[0].requestId, 'old-sos');
  assert.ok(f.state.calls.filter(item => item.path === 'healthBatch').every(item => item.body.bindingVersion === 2));
});

test('full offline queue pauses sensors and reconnect uploads original records before collecting again', async context => {
  const f = await fixture(context); await f.sync.authenticate(config, secret);
  const binding = f.store.verifiedBinding(config);
  const source = p.makeSamples(config.deviceNo, 'full', new Date(), 72, 120)[0];
  const samples = Array.from({ length: 200 }, (_, i) => ({ ...source, recordId: 'original-full-' + i }));
  await f.store.saveBatch({ binding, samples }); f.state.offline = true;
  const full = await f.sync.cycle(config, secret);
  assert.equal(full.queued, 200); assert.equal(f.state.listeners, 0); assert.deepEqual(f.store.batch().samples, samples);
  f.state.offline = false; f.advance(); const result = await f.sync.cycle(config, secret);
  assert.equal(result.uploaded, 201); assert.equal(f.state.records.size, 201);
  for (const sample of samples) assert.ok(f.state.records.has(sample.recordId));
});

test('lost acknowledgement retains original IDs and only later retry replays the durable batch', async context => {
  const f = await fixture(context); f.state.lostAck = true;
  const result = await f.sync.cycle(config, secret);
  assert.equal(result.offline, true); assert.equal(result.queued, 1);
  assert.equal(f.state.calls.filter(item => item.path === 'healthBatch').length, 1);
  const id = f.store.batch().samples[0].recordId;
  f.advance(); await f.sync.cycle(config, secret);
  assert.ok(f.state.records.has(id)); assert.equal(f.state.duplicates, 1); assert.equal(f.store.batch(), undefined);
});

test('hide/show while login is pending cannot start an old sensor read or save old binding', async context => {
  const f = await fixture(context); let complete;
  f.state.loginGate = new Promise(resolve => { complete = resolve; });
  const cycle = f.sync.cycle(config, secret);
  await settle(); f.sync.suspend(); f.sync.resume(); complete();
  await assert.rejects(cycle, error => error.code === 'CANCELLED');
  assert.equal(f.state.listeners, 0); assert.equal(f.store.verifiedBinding(config), undefined);
  assert.equal(f.requests.size, 0);
});

test('permission revocation never opens another dialog or fabricates readings', async context => {
  const f = await fixture(context); f.state.permissions = -1;
  const result = await f.sync.cycle(config, secret);
  assert.equal(result.sampled, 0); assert.equal(f.state.listeners, 0); assert.equal(f.state.dialogs, 0);
  assert.match(result.note, /未授权/);
  await f.sync.sample(config, secret, true); assert.equal(f.state.dialogs, 1);
});

test('uncertain persistence quarantines automatic network and sensor work until reload', async context => {
  const f = await fixture(context); await f.sync.authenticate(config, secret);
  f.storage.setFlush(async () => { throw new Error('completion uncertain'); });
  await assert.rejects(f.sync.cycle(config, secret));
  assert.equal(f.store.usable(), false);
  const calls = f.state.calls.length, listeners = f.state.listeners;
  await assert.rejects(f.sync.cycle(config, secret), error => error.code === 'STORAGE_ERROR');
  assert.equal(f.state.calls.length, calls); assert.equal(f.state.listeners, listeners);
});

test('clock rollback forces fresh authentication rather than postponing retries indefinitely', async context => {
  const f = await fixture(context); await f.sync.authenticate(config, secret);
  f.state.offline = true; await f.sync.cycle(config, secret);
  const calls = f.state.calls.length;
  context.mock.timers.setTime(1700000000000); f.state.offline = false;
  const result = await f.sync.cycle(config, secret);
  assert.equal(result.offline, false); assert.ok(f.state.calls.length > calls);
});

test('repeated upload failures back off despite successful login, while sampling continues each minute', async context => {
  const f = await fixture(context); f.state.failUploads = true;
  for (let i = 0; i < 5; i++) { await f.sync.cycle(config, secret); f.advance(); }
  const calls = f.state.calls.length, queued = f.store.batch().samples.length;
  const skipped = await f.sync.cycle(config, secret);
  assert.equal(f.state.calls.length, calls, 'healthy logins must not reset write failure backoff');
  assert.equal(skipped.sampled, 2); assert.equal(skipped.queued, queued + 2);
  f.state.failUploads = false; f.advance(300);
  assert.equal((await f.sync.cycle(config, secret)).queued, 0);
});
