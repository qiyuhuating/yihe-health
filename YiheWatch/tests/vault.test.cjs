const assert = require('node:assert/strict');
const test = require('node:test');
const load = require('./load-ets.cjs');
const { cryptoBoundary, storeFixture } = require('./storage-fixture.cjs');

test('storage namespace rejects paths and invalid names before opening preferences', () => {
  const { WatchStore } = storeFixture();
  for (const name of ['', '../yihe_watch_sim_v1', '/data/preferences', 'a/b', 'a\\b', 'x'.repeat(65)]) {
    assert.throws(() => new WatchStore({}, name), /名称无效/);
  }
});
const p = load('model/Protocol');
const config = p.normalizeConfig('https://care.example.com', 'WATCH001');
const binding = p.bindingFrom(config, { bound: 1, residentId: 7, bindingVersion: 1 });
const batch = { binding, samples: p.makeSamples('WATCH001', 'persisted-id', new Date(), 72, 110) };
const pending = [{ binding, requestId: 'same-emergency-press' }];
const secret = 'device-secret-that-must-not-be-on-disk';

test('fresh encrypted storage preserves connection and counter defaults', async () => {
  const store = await storeFixture().open();
  assert.deepEqual(store.config(), { server: 'http://127.0.0.1:18796', deviceNo: 'WATCH5_SIM_001' });
  assert.deepEqual(store.simulator(), { day: '', steps: 1280 });
  assert.deepEqual(store.pendingSOS(), []);
  assert.equal(store.batch(), undefined);
});

test('migration encrypts old health and SOS only after a durable copy and restores scoped credentials', async () => {
  const fixture = storeFixture({ server: config.server, deviceNo: config.deviceNo, batch: JSON.stringify(batch), sos: JSON.stringify(pending), sequence: 12 });
  const commits = [];
  fixture.setFlush(async () => commits.push(structuredClone(fixture.values)));
  const store = await fixture.open();
  assert.ok(commits[0].encryptedStateV1);
  assert.equal(commits[0].batch, JSON.stringify(batch), 'plaintext must survive until encrypted commit');
  assert.equal(commits[1].batch, undefined);
  assert.equal(commits[1].sos, undefined);
  await store.saveCredential(config, secret);
  assert.deepEqual(store.batch(), batch);
  const disk = JSON.stringify(fixture.values);
  for (const sensitive of [secret, batch.samples[0].recordId, pending[0].requestId, config.server]) assert.ok(!disk.includes(sensitive));
  const recovered = await fixture.open();
  assert.deepEqual(recovered.config(), config);
  assert.equal(recovered.credential(config), secret);
  assert.equal(recovered.credential({ ...config, deviceNo: 'WATCH002' }), '');
  assert.equal(recovered.credential({ ...config, server: 'https://other.example.com' }), '');
  assert.deepEqual(recovered.batch(), batch);
  assert.deepEqual(recovered.pendingSOS(), pending);
  assert.match(recovered.nextID(), /-d$/);
  await recovered.forgetCredential();
  const forgotten = await fixture.open();
  assert.equal(forgotten.credential(config), '');
  assert.deepEqual(forgotten.pendingSOS(), pending);
  assert.deepEqual(forgotten.batch(), batch);
  assert.equal(fixture.boundary.calls.filter(x => x === 'generate').length, 1);
});

test('changing an authenticated credential invalidates the old offline binding atomically', async () => {
  const fixture = storeFixture();
  const store = await fixture.open();
  await store.saveCredential(config, secret);
  await store.rememberBinding(binding);
  await store.saveBatch(batch);
  await store.saveSOS(pending);

  // A process may stop after the credential flush and before the new login
  // response's binding is persisted. The previous epoch must not be trusted.
  await store.saveCredential(config, 'rotated-device-secret-1234567890');
  const recovered = await fixture.open();
  assert.equal(recovered.credential(config), 'rotated-device-secret-1234567890');
  assert.equal(recovered.verifiedBinding(config), undefined);
  assert.deepEqual(recovered.batch(), batch);
  assert.deepEqual(recovered.pendingSOS(), pending);
});

test('forgetting the device secret removes offline trust but retains emergency records', async () => {
  const fixture = storeFixture();
  const store = await fixture.open();
  await store.saveCredential(config, secret);
  await store.rememberBinding(binding);
  await store.saveSOS(pending);
  await store.forgetCredential();
  const recovered = await fixture.open();
  assert.equal(recovered.credential(config), '');
  assert.equal(recovered.verifiedBinding(config), undefined);
  assert.deepEqual(recovered.pendingSOS(), pending);
});

test('each encryption uses fresh random nonce and authenticates multi-chunk UTF-8 data', async () => {
  const boundary = cryptoBoundary();
  const { WatchVault } = load('services/WatchVault', boundary.mocks);
  const vault = new WatchVault();
  const content = '颐和健康数据'.repeat(20000);
  const a = await vault.seal(content), b = await vault.seal(content);
  assert.notEqual(JSON.parse(a).nonce, JSON.parse(b).nonce);
  assert.notEqual(a, b);
  assert.equal(await vault.open(a), content);
  assert.equal(await vault.open(b), content);
  assert.equal(boundary.sessions.size, 0);
});

test('native HUKS null finish result authenticates bytes already returned by update', async () => {
  const boundary = cryptoBoundary();
  let nullFinishes = 0;
  const huks = boundary.mocks['@kit.UniversalKeystoreKit'].huks;
  const finish = huks.finishSession;
  huks.finishSession = async (...args) => { const result = await finish(...args); if (result === null) nullFinishes++; return result; };
  const { WatchVault } = load('services/WatchVault', boundary.mocks);
  const vault = new WatchVault();
  assert.equal(await vault.open(await vault.seal('authenticated record')), 'authenticated record');
  assert.equal(nullFinishes, 1);
  assert.equal(boundary.sessions.size, 0);
});

test('ciphertext, tag, nonce and version corruption cannot fall back to legacy plaintext', async () => {
  const fixture = storeFixture({ batch: JSON.stringify(batch) });
  await fixture.open();
  const original = fixture.values.encryptedStateV1;
  for (const field of ['ciphertext', 'tag', 'nonce', 'version']) {
    const envelope = JSON.parse(original);
    if (field === 'version') envelope.version = 99;
    else {
      const key = field === 'tag' ? 'ciphertext' : field;
      const value = Buffer.from(envelope[key], 'base64');
      value[field === 'tag' ? value.length - 1 : 0] ^= 1;
      envelope[key] = value.toString('base64');
    }
    const corrupted = JSON.stringify(envelope);
    fixture.values.encryptedStateV1 = corrupted;
    fixture.values.batch = 'legacy-must-not-be-used';
    await assert.rejects(fixture.open(), /原记录已保留/);
    assert.equal(fixture.values.encryptedStateV1, corrupted);
    assert.equal(fixture.values.batch, 'legacy-must-not-be-used');
    assert.equal(fixture.boundary.sessions.size, 0);
  }
  fixture.values.encryptedStateV1 = original;
  assert.deepEqual((await fixture.open()).batch(), batch);
});

test('lost or inaccessible HUKS key never generates a replacement over existing ciphertext', async () => {
  const fixture = storeFixture();
  const store = await fixture.open();
  await store.saveBatch(batch);
  const original = fixture.values.encryptedStateV1;
  const generationCount = fixture.boundary.calls.filter(x => x === 'generate').length;
  fixture.boundary.keys.clear();
  await assert.rejects(fixture.open(), /原记录已保留/);
  assert.equal(fixture.values.encryptedStateV1, original);
  assert.equal(fixture.boundary.calls.filter(x => x === 'generate').length, generationCount);
  fixture.boundary.mocks['@kit.UniversalKeystoreKit'].huks.isKeyItemExist = async () => { throw { code: 12000005 }; };
  await assert.rejects(fixture.open(), /原记录已保留/);
  assert.equal(fixture.boundary.calls.filter(x => x === 'generate').length, generationCount);
});

test('a different application keystore cannot decrypt a copied preferences file', async () => {
  const fixture = storeFixture();
  await (await fixture.open()).saveBatch(batch);
  const copy = structuredClone(fixture.values);
  const outsider = storeFixture(copy);
  await assert.rejects(outsider.open(), /原记录已保留/);
  assert.equal(outsider.boundary.keys.size, 0);
});

test('unsupported secure random capability preserves old plaintext and fails closed', async () => {
  const boundary = cryptoBoundary();
  boundary.mocks.canIUse = () => false;
  const fixture = storeFixture({ batch: JSON.stringify(batch) }, boundary);
  await assert.rejects(fixture.open(), /原记录已保留/);
  assert.equal(fixture.values.batch, JSON.stringify(batch));
  assert.equal(fixture.values.encryptedStateV1, undefined);
});

test('failed initial encryption commit cannot remove old records', async () => {
  const fixture = storeFixture({ batch: JSON.stringify(batch), sos: JSON.stringify(pending) });
  fixture.setFlush(async () => { throw new Error('commit failed'); });
  await assert.rejects(fixture.open());
  assert.equal(fixture.values.batch, JSON.stringify(batch));
  assert.equal(fixture.values.sos, JSON.stringify(pending));
});

test('crash during plaintext cleanup recovers the committed encrypted IDs and records', async () => {
  const fixture = storeFixture({ batch: JSON.stringify(batch), sos: JSON.stringify(pending) });
  let durable, flushes = 0;
  fixture.setFlush(async () => {
    if (++flushes === 1) durable = structuredClone(fixture.values);
    else throw new Error('cleanup acknowledgement lost');
  });
  await assert.rejects(fixture.open());
  Object.assign(fixture.values, durable);
  fixture.setFlush(async () => {});
  const recovered = await fixture.open();
  assert.deepEqual(recovered.batch(), batch);
  assert.deepEqual(recovered.pendingSOS(), pending);
  assert.equal(fixture.values.batch, undefined);
});

test('full 200-sample queue is encrypted without dropping records or retry IDs', async () => {
  const fixture = storeFixture();
  const store = await fixture.open();
  const samples = Array.from({ length: 200 }, (_, i) => ({ ...batch.samples[i % 2], recordId: 'original-' + i }));
  await store.saveBatch({ binding, samples });
  const recovered = await fixture.open();
  assert.deepEqual(recovered.batch().samples, samples);
  assert.ok(!JSON.stringify(fixture.values).includes('original-199'));
});

test('overlapping save is rejected without polluting the snapshot being committed', async () => {
  const fixture = storeFixture();
  const store = await fixture.open();
  const save = store.saveBatch(batch);
  await assert.rejects(store.saveSOS(pending), /勿重复发送/);
  await save;
  const recovered = await fixture.open();
  assert.deepEqual(recovered.batch(), batch);
  assert.deepEqual(recovered.pendingSOS(), []);
});

test('monitor intent and last verified binding survive encrypted restart with scope isolation', async () => {
  const fixture = storeFixture(); const store = await fixture.open();
  assert.equal(store.monitoringEnabled(), false);
  await store.saveMonitoringEnabled(true); await store.rememberBinding(binding);
  const recovered = await fixture.open();
  assert.equal(recovered.monitoringEnabled(), true);
  assert.deepEqual(recovered.verifiedBinding(config), binding);
  assert.equal(recovered.verifiedBinding({ ...config, deviceNo: 'OTHER' }), undefined);
  await recovered.saveMonitoringEnabled(false); await recovered.rememberBinding(undefined);
  const disabled = await fixture.open();
  assert.equal(disabled.monitoringEnabled(), false); assert.equal(disabled.verifiedBinding(config), undefined);
});
