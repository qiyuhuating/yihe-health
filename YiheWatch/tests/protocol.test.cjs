const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const Module = require('node:module');
const ts = require('typescript');
const source = path.resolve(__dirname, '../entry/src/main/ets/model/Protocol.ets');
const compiled = ts.transpileModule(fs.readFileSync(source, 'utf8'), {
  compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS }
}).outputText;
const mod = new Module(source, module);
mod._compile(compiled, source);
const p = mod.exports;
const config = p.normalizeConfig('http://127.0.0.1:18796/', 'watch5_sim_001');
const binding = p.bindingFrom(config, { returnCode: '1111', bound: 1, residentId: 7, bindingVersion: 3 });
const samples = p.makeSamples(config.deviceNo, 'stable-001', new Date('2026-10-25T04:00:00Z'), 72, 1280);

test('server validation rejects credentials, paths and query strings', () => {
  assert.equal(config.deviceNo, 'WATCH5_SIM_001');
  for (const url of ['file:///tmp', 'http://u:p@host', 'http://host/path', 'http://host?a=1', 'http://host:abc']) {
    assert.throws(() => p.normalizeConfig(url, 'WATCH1'));
  }
});
test('unbound or missing binding version cannot produce an authenticated owner', () => {
  assert.throws(() => p.bindingFrom(config, { returnCode: '1111', bound: 0 }));
  assert.throws(() => p.bindingFrom(config, { returnCode: '1111', bound: 1, residentId: 7 }));
  for (const value of [-1, 0, 1.5, Number.MAX_SAFE_INTEGER + 1]) {
    assert.throws(() => p.bindingFrom(config, { returnCode: '1111', bound: 1, residentId: 7, bindingVersion: value }));
    assert.throws(() => p.bindingFrom(config, { returnCode: '1111', bound: 1, residentId: value, bindingVersion: 1 }));
  }
});
test('samples declare simulation, units, and UTC cumulative day boundary', () => {
  assert.equal(samples[0].metadata.simulated, 'true');
  assert.equal(samples[0].start, samples[0].end);
  assert.equal(samples[0].unit, 'count/min');
  assert.equal(samples[1].start, '2026-10-25T00:00:00.000Z');
  assert.equal(samples[1].semantics, 'daily_total');
  assert.equal(samples[1].metadata.counterTimezone, 'UTC');
  assert.notEqual(samples[0].recordId, samples[1].recordId);
});
test('serialized offline batch retries preserve sample IDs and original binding version', () => {
  const queue = p.enqueue(undefined, binding, samples);
  const recovered = JSON.parse(JSON.stringify(queue));
  assert.deepEqual(p.batchRequest(queue, binding, 'secret'), p.batchRequest(recovered, binding, 'secret'));
  assert.equal(recovered.samples[0].recordId, 'stable-001-hr');
});
test('server, owner, device or epoch changes prevent uploading and mixing old samples', () => {
  const queue = p.enqueue(undefined, binding, samples);
  for (const change of [{ server: 'http://other' }, { residentId: 8 }, { deviceNo: 'WATCH2' }, { version: 4 }]) {
    const other = { ...binding, ...change };
    assert.throws(() => p.batchRequest(queue, other, 'secret'));
    assert.throws(() => p.enqueue(queue, other, samples));
  }
  assert.equal(queue.samples.length, 2);
});
test('queue capacity never silently evicts old records', () => {
  let queue;
  for (let i = 0; i < 100; i++) queue = p.enqueue(queue, binding, samples);
  assert.equal(queue.samples.length, 200);
  assert.throws(() => p.enqueue(queue, binding, samples));
  assert.equal(queue.samples.length, 200);
});
test('old pending SOS remains available while a new binding can create its own request', () => {
  const old = { binding, requestId: 'old-sos' };
  const restored = p.readPendingSOS(JSON.stringify(old));
  const rebound = { ...binding, residentId: 8, version: 4 };
  assert.equal(p.pendingForBinding(restored, rebound), undefined);
  const next = { binding: rebound, requestId: 'new-sos' };
  const saved = p.readPendingSOS(JSON.stringify([...restored, next]));
  assert.equal(p.pendingForBinding(saved, rebound).requestId, 'new-sos');
  assert.deepEqual(p.pendingForBinding(saved, binding), old);
  const acknowledged = { ...next, incidentId: 42 };
  assert.equal(p.pendingForBinding([old, acknowledged], rebound), undefined);
  assert.equal(p.readPendingSOS(JSON.stringify([old, acknowledged]))[1].incidentId, 42);
  assert.deepEqual(p.readPendingSOS(''), []);
});
test('SOS persistence failure keeps the previous value and blocks subsequent sends', async () => {
  const { storeFixture } = require('./storage-fixture.cjs');
  for (const initial of ['', JSON.stringify({ binding, requestId: 'already-sent' })]) {
    const fixture = storeFixture({ sos: initial });
    const store = await fixture.open();
    const previous = fixture.values.encryptedStateV1;
    fixture.setFlush(async () => { throw new Error('simulated storage failure'); });
    await assert.rejects(store.saveSOS(initial ? [{ binding, requestId: 'already-sent', incidentId: 42 }] : [{ binding, requestId: 'new' }]));
    assert.throws(() => store.pendingSOS(), /勿重复发送/);
    await assert.rejects(store.saveSOS([]), /勿重复发送/);
    fixture.values.encryptedStateV1 = previous;
    fixture.setFlush(async () => {});
    const recovered = await fixture.open();
    assert.deepEqual(recovered.pendingSOS(), p.readPendingSOS(initial));
  }
});

test('SOS acknowledgement retains its request ID when flush writes then fails', async () => {
  const { storeFixture } = require('./storage-fixture.cjs');
  const pending = { binding, requestId: 'uncertain' };
  const fixture = storeFixture({ sos: JSON.stringify([pending]) });
  const store = await fixture.open();
  let durable;
  fixture.setFlush(async () => { durable = fixture.values.encryptedStateV1; throw new Error('completion lost'); });
  await assert.rejects(store.saveSOS([{ ...pending, incidentId: 42 }]));
  fixture.values.encryptedStateV1 = durable;
  fixture.setFlush(async () => {});
  const afterRestart = (await fixture.open()).pendingSOS();
  assert.equal(afterRestart[0].requestId, 'uncertain');
  assert.equal(afterRestart[0].incidentId, 42);
  assert.equal(p.pendingForBinding(afterRestart, binding), undefined);
  assert.equal(p.pendingForBinding(p.readPendingSOS(JSON.stringify([pending])), binding).requestId, 'uncertain');
});
