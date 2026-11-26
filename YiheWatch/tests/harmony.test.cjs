const assert = require('node:assert/strict');
const test = require('node:test');
const load = require('./load-ets.cjs');
const { storeFixture } = require('./storage-fixture.cjs');
const { signatureBoundary } = require('./signature-fixture.cjs');
const { Request } = require('./rcp-fixture.cjs');
const p = load('model/Protocol');
const { NativeSampling } = load('model/NativeSampling');
const config = p.normalizeConfig('https://care.example.com', 'WATCH001');
const binding = p.bindingFrom(config, { bound: 1, residentId: 7, bindingVersion: 1 });
const observation = (value, seconds, metric = 'steps') => ({ metric, value, receivedAt: 1800000000000 + seconds * 1000, sensorTimestamp: seconds * 1000000000 });

test('native counters anchor, produce intervals, and reset across reboot, rollback and rebind', () => {
  const sampler = new NativeSampling();
  assert.deepEqual(sampler.accept(binding, 'a', observation(100, 1)), []);
  const [delta] = sampler.accept(binding, 'b', observation(105, 2));
  assert.equal(delta.value, 5);
  assert.equal(delta.semantics, 'interval');
  assert.equal(delta.source, 'yihe-harmonyos');
  assert.equal(delta.metadata.simulated, 'false');
  assert.equal(delta.metadata.timestampBasis, 'app-received');
  assert.notEqual(delta.start, delta.end);
  for (const input of [observation(2, 3), observation(3, 2), observation(4, 1)]) {
    assert.deepEqual(sampler.accept(binding, 'reset', input), []);
  }
  assert.deepEqual(sampler.accept({ ...binding, version: 2 }, 'new-owner', observation(9, 5)), []);
  assert.throws(() => sampler.accept(binding, 'invalid', observation(NaN, 6)));
});

test('uncommitted sampling candidate cannot lose the next step delta', () => {
  const sampler = new NativeSampling();
  sampler.accept(binding, 'a', observation(100, 1));
  sampler.copy().accept(binding, 'not-saved', observation(110, 2));
  assert.equal(sampler.accept(binding, 'saved', observation(115, 3))[0].value, 15);
  const [heart] = sampler.accept(binding, 'hr', observation(73, 4, 'heart_rate'));
  assert.equal(heart.value, 73);
  assert.equal(heart.start, heart.end);
  assert.equal(heart.semantics, 'instant');
  assert.deepEqual(sampler.accept(binding, 'no-reading', observation(0, 5, 'heart_rate')), []);
});

test('transport negotiates capabilities, closes sessions, and never replays rejected writes', async () => {
  const calls = []; let destroyed = 0; let status = 200;
  let reply = { returnCode: '1111', protocolVersion: 1, maxBatchSize: 200 };
  const { WatchApi } = load('services/WatchApi', { ...signatureBoundary().mocks, '@kit.RemoteCommunicationKit': { rcp: {
    Request,
    createSession: configuration => ({ fetch: async request => { calls.push({ ...request, configuration }); return { statusCode: status, toString: () => JSON.stringify(reply) }; }, close: () => destroyed++ })
  } } });
  const api = new WatchApi();
  await api.login(config, 'not-logged');
  assert.equal(calls[0].url, config.server + '/api/v1/device/login');
  assert.equal(JSON.parse(calls[0].content).platform, 'harmonyos');
  assert.equal(calls[0].headers['cache-control'], 'no-store');
  assert.deepEqual(calls[0].configuration.requestConfiguration.transfer, { autoRedirect: false, timeout: { connectMs: 5000, transferMs: 10000 } });
  reply = { returnCode: '1111' };
  await assert.rejects(api.login(config, 'not-logged'), /协议|兼容/);
  status = 409;
  reply = { returnCode: '0000', code: 'EPOCH_EXPIRED', message: '绑定版本已变化', reference: 'watch-test' };
  await assert.rejects(api.upload(config, { deviceNo: config.deviceNo, secret: 'not-logged', samples: [] }), error => error.code === 'EPOCH_EXPIRED' && /绑定版本/.test(error.message));
  assert.equal(calls.length, 3);
  assert.equal(destroyed, 3);
  assert.throws(() => p.normalizeConfig('http://192.168.1.2', 'WATCH001'));
});

test('a verified newer epoch clears only its old telemetry and preserves SOS history', async () => {
  const batch = { binding, samples: p.makeSamples('WATCH001', 'old', new Date(), 72, 100) };
  const sos = JSON.stringify([{ binding, requestId: 'unconfirmed-sos' }]);
  const fixture = storeFixture({ batch: JSON.stringify(batch), sos });
  const store = await fixture.open();
  let flushes = 0;
  fixture.setFlush(async () => { flushes++; });
  for (const live of [binding, { ...binding, version: 0 },
    { ...binding, server: 'https://other.example.com', version: 2 },
    { ...binding, deviceNo: 'OTHER', version: 2 }]) {
    assert.equal(await store.discardExpiredBatch(live), false);
    assert.deepEqual(store.batch(), batch);
  }
  assert.throws(() => p.batchRequest(batch, { ...binding, version: 2 }, 'secret'), error => error.code === 'EPOCH_EXPIRED');
  assert.equal(await store.discardExpiredBatch({ ...binding, version: 2 }), true);
  assert.equal(store.batch(), undefined);
  assert.equal(JSON.stringify(store.pendingSOS()), sos);
  assert.equal(fixture.values.sos, undefined);
  assert.ok(fixture.values.encryptedStateV1);
  assert.equal(flushes, 1);
  assert.equal(await store.discardExpiredBatch({ ...binding, version: 2 }), false);
});

test('failed persistence of epoch cleanup blocks further sampling', async () => {
  const fixture = storeFixture({ batch: JSON.stringify({ binding, samples: [] }) });
  const store = await fixture.open();
  fixture.setFlush(async () => { throw new Error('disk failure'); });
  await assert.rejects(store.discardExpiredBatch({ ...binding, version: 2 }), /重启/);
  assert.throws(() => store.batch(), /重启/);
});

test('native permission denial and missing hardware never fabricate readings', async () => {
  let requested; let listeners = 0;
  const sensor = { SensorId: { HEART_RATE: 1, PEDOMETER: 2 },
    getSensorList: async () => [{ sensorId: 1 }, { sensorId: 2 }],
    once: () => listeners++, off: () => {} };
  const { HealthSensors } = load('services/HealthSensors', {
    '@kit.SensorServiceKit': { sensor }, '@kit.AbilityKit': { abilityAccessCtrl: {
      createAtManager: () => ({ requestPermissionsFromUser: async (_, permissions) => { requested = permissions; return { authResults: [-1, -1] }; } })
    } }
  });
  const result = await new HealthSensors().read({});
  assert.deepEqual(result.observations, []);
  assert.match(result.status, /未授权/);
  assert.equal(requested.length, 2);
  assert.equal(listeners, 0);
  sensor.getSensorList = async () => [];
  assert.match((await new HealthSensors().read({})).status, /未提供/);
});

test('native sensor callbacks and cancellation remove listeners', async () => {
  const removed = []; let callback;
  const sensor = { SensorId: { HEART_RATE: 1, PEDOMETER: 2 }, getSensorList: async () => [{ sensorId: 1 }],
    once: (_, fn) => { callback = fn; }, off: id => removed.push(id) };
  const { HealthSensors } = load('services/HealthSensors', {
    '@kit.SensorServiceKit': { sensor }, '@kit.AbilityKit': { abilityAccessCtrl: { createAtManager: () => ({ requestPermissionsFromUser: async () => ({ authResults: [0] }) }) } }
  });
  const service = new HealthSensors();
  const first = service.read({});
  await new Promise(resolve => setImmediate(resolve));
  callback({ heartRate: 76, timestamp: 5000000 });
  assert.equal((await first).observations[0].value, 76);
  const second = service.read({});
  await new Promise(resolve => setImmediate(resolve));
  service.cancel();
  await assert.rejects(second, /取消/);
  assert.deepEqual(removed, [1, 1]);
});

test('uncertain batch persistence blocks new deltas until recovery', async () => {
  const fixture = storeFixture();
  const store = await fixture.open();
  fixture.setFlush(async () => { throw new Error('disk'); });
  await assert.rejects(store.saveBatch({ samples: [] }), /重启/);
  assert.throws(() => store.batch(), /重启/);
  await assert.rejects(store.saveBatch(undefined), /重启/);
  fixture.setFlush(async () => {});
  assert.ok((await fixture.open()).batch());
});

test('sensor timeout removes the listener and reports unavailability', async (context) => {
  context.mock.timers.enable({ apis: ['setTimeout'] });
  const removed = [];
  const sensor = { SensorId: { HEART_RATE: 1, PEDOMETER: 2 }, getSensorList: async () => [{ sensorId: 1 }], once() {}, off: id => removed.push(id) };
  const { HealthSensors } = load('services/HealthSensors', {
    '@kit.SensorServiceKit': { sensor }, '@kit.AbilityKit': { abilityAccessCtrl: { createAtManager: () => ({ requestPermissionsFromUser: async () => ({ authResults: [0] }) }) } }
  });
  const pending = new HealthSensors().read({});
  await new Promise(resolve => setImmediate(resolve));
  context.mock.timers.tick(10000);
  const result = await pending;
  assert.deepEqual(result.observations, []);
  assert.match(result.status, /未取得有效读数/);
  assert.deepEqual(removed, [1]);
});

test('leaving and reopening while authentication is pending cannot start a stale sensor read', async () => {
  let enumerations = 0;
  const { HealthSensors } = load('services/HealthSensors', { '@kit.SensorServiceKit': { sensor: {
    getSensorList: async () => { enumerations++; return []; }
  } }, '@kit.AbilityKit': {} });
  const service = new HealthSensors();
  const session = service.session();
  let authenticated;
  const login = new Promise(resolve => { authenticated = resolve; });
  const read = (async () => { await login; return service.read({}, session); })();
  service.suspend();
  service.resume();
  authenticated();
  await assert.rejects(read, /取消/);
  assert.equal(enumerations, 0);
});

test('leaving during permission authorization never starts hardware listeners', async () => {
  let authorize; let listeners = 0;
  const { HealthSensors } = load('services/HealthSensors', {
    '@kit.SensorServiceKit': { sensor: { SensorId: { HEART_RATE: 1, PEDOMETER: 2 }, getSensorList: async () => [{ sensorId: 1 }], once: () => listeners++ } },
    '@kit.AbilityKit': { abilityAccessCtrl: { createAtManager: () => ({ requestPermissionsFromUser: () => new Promise(resolve => { authorize = resolve; }) }) } }
  });
  const service = new HealthSensors();
  const read = service.read({});
  await new Promise(resolve => setImmediate(resolve));
  service.suspend();
  authorize({ authResults: [0] });
  await assert.rejects(read, /取消/);
  assert.equal(listeners, 0);
});
