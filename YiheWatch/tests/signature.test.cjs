const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const test = require('node:test');
const load = require('./load-ets.cjs');
const { signatureBoundary } = require('./signature-fixture.cjs');
const { Request } = require('./rcp-fixture.cjs');
const config = { server: 'https://care.example.com', deviceNo: 'WATCH001' };
const secret = 'valid-variable-length-device-secret-012345678901234567890123456789';

test('signer matches independent HMAC over the exact UTF-8 body and imports variable-length secrets', async context => {
  context.mock.timers.enable({ apis: ['Date'], now: 1800000000000 });
  const boundary = signatureBoundary(); boundary.state.random = Uint8Array.from({ length: 16 }, (_, i) => i);
  const { DeviceSigner } = load('services/DeviceSigner', boundary.mocks);
  const raw = JSON.stringify({ deviceNo: ' watch001 ', secret, name: '田风', samples: ['原记录'.repeat(14000)] });
  const signed = await new DeviceSigner().sign(' watch001 ', secret, raw);
  assert.equal(signed.timestamp, '1800000000'); assert.equal(signed.nonce, '000102030405060708090a0b0c0d0e0f');
  const digest = crypto.createHash('sha256').update(raw, 'utf8').digest('hex');
  assert.equal(signed.signature, crypto.createHmac('sha256', secret).update('WATCH001:1800000000:' + signed.nonce + ':' + digest).digest('hex'));
  assert.equal(boundary.state.cleared, 1); assert.ok(boundary.state.imported.every(data => data.every(byte => byte === 0)));
});

test('signer generates a fresh secure nonce per attempt without changing the durable payload', async () => {
  const boundary = signatureBoundary(); const { DeviceSigner } = load('services/DeviceSigner', boundary.mocks);
  const signer = new DeviceSigner(), raw = JSON.stringify({ deviceNo: config.deviceNo, secret, requestId: 'same-durable-sos-id' });
  const first = await signer.sign(config.deviceNo, secret, raw), retry = await signer.sign(config.deviceNo, secret, raw);
  assert.notEqual(first.nonce, retry.nonce); assert.notEqual(first.signature, retry.signature);
  assert.match(first.nonce, /^[a-f0-9]{32}$/); assert.match(first.signature, /^[a-f0-9]{64}$/);
});

test('missing crypto or RNG/key/MAC failure cannot fall back to an unsigned HTTP request', async () => {
  for (const failure of ['unsupported', 'random', 'key', 'mac', 'short-random']) {
    const boundary = signatureBoundary(); boundary.state.fail = failure;
    if (failure === 'short-random') boundary.state.random = new Uint8Array(15);
    let created = 0;
    const { WatchApi } = load('services/WatchApi', { ...boundary.mocks, canIUse: () => failure !== 'unsupported',
      '@kit.RemoteCommunicationKit': { rcp: { createSession: () => { created++; throw new Error('Must not allocate HTTP'); } } } });
    await assert.rejects(new WatchApi().login(config, secret), error => error.code === 'SIGNING_UNAVAILABLE');
    assert.equal(created, 0);
    if (failure === 'mac') assert.equal(boundary.state.cleared, 1);
    assert.ok(boundary.state.imported.every(data => data.every(byte => byte === 0)));
  }
});

test('cancelling during native signing prevents later HTTP creation after the RNG completes', async () => {
  for (const fail of ['', 'random']) {
    const boundary = signatureBoundary(); let finish;
    boundary.state.fail = fail;
    boundary.state.beforeRandom = new Promise(resolve => { finish = resolve; });
    let created = 0;
    const { WatchApi } = load('services/WatchApi', { ...boundary.mocks,
      '@kit.RemoteCommunicationKit': { rcp: { createSession: () => { created++; throw new Error('Must not allocate HTTP'); } } } });
    const api = new WatchApi(), pending = api.login(config, secret);
    api.cancel(); finish();
    await assert.rejects(pending, error => error.code === 'CANCELLED'); assert.equal(created, 0);
  }
});

test('signing rejects oversized bytes and invalid clock before requesting transport', async context => {
  const boundary = signatureBoundary(); const { DeviceSigner } = load('services/DeviceSigner', boundary.mocks);
  const signer = new DeviceSigner();
  await assert.rejects(signer.sign(config.deviceNo, secret, '长'.repeat(350000)), error => error.code === 'SIGNING_UNAVAILABLE');
  context.mock.method(Date, 'now', () => -1000);
  await assert.rejects(signer.sign(config.deviceNo, secret, '{}'), error => error.code === 'SIGNING_UNAVAILABLE');
  assert.equal(boundary.state.imported.length, 0);
});

test('cancellation aborts each active native session and closes it once without waiting for its timeout', async () => {
  let opened = 0, cancelled = 0, closed = 0;
  const boundary = signatureBoundary();
  const { WatchApi } = load('services/WatchApi', { ...boundary.mocks, '@kit.RemoteCommunicationKit': { rcp: {
    Request,
    createSession: () => {
      opened++; let reject;
      return { fetch: () => new Promise((resolve, fail) => { reject = fail; }),
        cancel: () => { cancelled++; reject(new Error('Native request aborted')); }, close: () => { closed++; } };
    }
  } } });
  const api = new WatchApi();
  const requests = [api.login(config, secret), api.login(config, secret)];
  const rejected = requests.map(request => assert.rejects(request, error => error.code === 'CANCELLED'));
  while (opened !== 2) await new Promise(resolve => setImmediate(resolve));
  api.cancel(); await Promise.all(rejected); api.cancel();
  assert.equal(cancelled, 2); assert.equal(closed, 2);
});

test('missing cancellable transport capability never opens a network session', async () => {
  const boundary = signatureBoundary(); let opened = 0;
  const { WatchApi } = load('services/WatchApi', { ...boundary.mocks,
    canIUse: name => name !== 'SystemCapability.Collaboration.RemoteCommunication',
    '@kit.RemoteCommunicationKit': { rcp: { createSession: () => { opened++; throw new Error('Must not allocate session'); } } } });
  await assert.rejects(new WatchApi().login(config, secret), error => error.code === 'NETWORK_UNAVAILABLE');
  assert.equal(opened, 0);
});
