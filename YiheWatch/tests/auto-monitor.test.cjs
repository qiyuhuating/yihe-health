const assert = require('node:assert/strict');
const test = require('node:test');
const { AutoMonitor } = require('./load-ets.cjs')('services/AutoMonitor');
const settle = () => new Promise(resolve => setImmediate(resolve));

test('foreground timer runs automatically, pauses hidden, and resumes without another start', async context => {
  context.mock.timers.enable({ apis: ['setTimeout'] });
  let cycles = 0;
  const loop = new AutoMonitor(async () => { cycles++; return 'ok'; });
  loop.setEnabled(true);
  context.mock.timers.tick(120000); await settle(); assert.equal(cycles, 0);
  loop.setVisible(true); context.mock.timers.tick(0); await settle(); assert.equal(cycles, 1);
  context.mock.timers.tick(60000); await settle(); assert.equal(cycles, 2);
  loop.setVisible(false); context.mock.timers.tick(600000); await settle(); assert.equal(cycles, 2);
  loop.setVisible(true); context.mock.timers.tick(0); await settle(); assert.equal(cycles, 3);
  loop.setEnabled(false); context.mock.timers.tick(600000); await settle(); assert.equal(cycles, 3);
});

test('slow cycle and stop/start cannot overlap or let stale halt stop a new session', async context => {
  context.mock.timers.enable({ apis: ['setTimeout'] });
  let finish, active = 0, max = 0, cycles = 0;
  const loop = new AutoMonitor(async () => {
    cycles++; max = Math.max(max, ++active);
    const decision = await new Promise(resolve => { finish = resolve; }); active--; return decision;
  });
  loop.setVisible(true); loop.setEnabled(true); context.mock.timers.tick(0); await settle();
  context.mock.timers.tick(600000); await settle(); assert.equal(cycles, 1);
  loop.setEnabled(false); loop.setEnabled(true);
  context.mock.timers.tick(0); await settle(); assert.equal(cycles, 1);
  finish('halt'); await settle(); context.mock.timers.tick(0); await settle();
  assert.equal(cycles, 2); assert.equal(max, 1);
  loop.setVisible(false); finish('ok'); await settle();
  context.mock.timers.tick(600000); await settle(); assert.equal(cycles, 2);
});

test('busy UI retries shortly; failures back off; success resets cadence; halt stays stopped', async context => {
  context.mock.timers.enable({ apis: ['setTimeout'] });
  const outcomes = ['busy', 'retry', 'retry', 'ok', 'halt'];
  let calls = 0;
  const loop = new AutoMonitor(async () => { calls++; return outcomes.shift(); });
  loop.setVisible(true); loop.setEnabled(true); context.mock.timers.tick(0); await settle();
  assert.equal(calls, 1);
  context.mock.timers.tick(999); await settle(); assert.equal(calls, 1);
  context.mock.timers.tick(1); await settle(); assert.equal(calls, 2);
  context.mock.timers.tick(119999); await settle(); assert.equal(calls, 2);
  context.mock.timers.tick(1); await settle(); assert.equal(calls, 3);
  context.mock.timers.tick(240000); await settle(); assert.equal(calls, 4);
  context.mock.timers.tick(60000); await settle(); assert.equal(calls, 5);
  context.mock.timers.tick(3600000); await settle(); assert.equal(calls, 5);
});

test('rejected cycles never produce unhandled rejection and stop removes pending retry', async context => {
  context.mock.timers.enable({ apis: ['setTimeout'] });
  let calls = 0;
  const loop = new AutoMonitor(async () => { calls++; throw new Error('transient failure'); });
  loop.setVisible(true); loop.setEnabled(true); context.mock.timers.tick(0); await settle();
  loop.setEnabled(false); context.mock.timers.tick(3600000); await settle(); assert.equal(calls, 1);
  assert.throws(() => new AutoMonitor(async () => 'ok', 0));
});
