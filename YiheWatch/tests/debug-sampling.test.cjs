const test = require('node:test');
const assert = require('node:assert/strict');
const load = require('./load-ets.cjs');

test('release profile rejects the simulation operation', () => {
  const { requireDebugSampling } = load('services/DebugSampling', { BuildProfile: { DEBUG: false } });
  assert.throws(() => requireDebugSampling(), /正式构建禁止生成模拟采样/);
});

test('debug build retains explicitly requested simulator sampling', () => {
  const { requireDebugSampling } = load('services/DebugSampling', { BuildProfile: { DEBUG: true } });
  assert.doesNotThrow(() => requireDebugSampling());
});
