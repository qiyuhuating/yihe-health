const fs = require('fs'),
  path = require('path'),
  vm = require('vm'),
  assert = require('assert');
const current = path.resolve(__dirname, '..'),
  baseline = process.argv[2],
  now = 1790000000000;
function env() {
  let serial = 0,
    clock = now;
  const items = new Map(),
    events = [];
  let chain = Promise.resolve();
  class FixedDate extends Date {
    constructor(...args) {
      super(...(args.length ? args : [clock]));
    }
    static now() {
      return clock;
    }
  }
  const ctx = {
    Date: FixedDate,
    console,
    Math,
    Object,
    Array,
    Set,
    Map,
    Number,
    String,
    JSON,
    Error,
    Promise,
    performance: {
      now: () => 0,
    },
    setTimeout: () => 1,
    clearTimeout: () => {},
    AbortController,
    CustomEvent: class {
      constructor(type) {
        this.type = type;
      }
    },
    crypto: {
      randomUUID: () => 'uuid-' + ++serial,
    },
    navigator: {
      locks: {
        request: (_key, _opts, fn) => {
          const next = chain.then(fn);
          chain = next.catch(() => {});
          return next;
        },
      },
    },
    localStorage: {
      getItem: (k) => items.get(k) ?? null,
      setItem: (k, v) => items.set(k, v),
    },
    YIHE_RUNTIME_CONFIG: {
      mode: 'demo',
    },
    dispatchEvent: (e) => events.push(e.type),
  };
  ctx.window = ctx;
  vm.createContext(ctx);
  return {
    ctx,
    items,
    events,
    advance: (n) => (clock += n),
  };
}
function load(ctx, root, name) {
  vm.runInContext(fs.readFileSync(path.join(root, 'docs/脚本', name), 'utf8'), ctx);
}
async function demo(root) {
  const e = env(),
    { ctx, items, events } = e;
  seedLegacyData(ctx, items);
  load(ctx, root, 'metrics.js');
  load(ctx, root, 'care-api.js');
  const api = ctx.CareAPI,
    out = [];
  async function step(label, fn) {
    let value, error;
    try {
      value = await fn();
    } catch (e) {
      error = {
        code: e.code,
        message: e.message,
      };
    }
    out.push({
      label,
      value,
      error,
      store: items.get(api.KEY),
      events: [...events],
    });
  }
  await checkDemoProfile(api, step);
  await checkDemoTransitions(api, step);
  await checkDemoRecovery(api, step);
  await checkDemoDoses(api, e, step);
  await checkCorruptDemo(api, items, step);
  return JSON.parse(
    JSON.stringify({
      keys: Object.keys(api),
      out,
    }),
  );
}
async function http(root, fixture) {
  const { ctx } = env();
  let response;
  ctx.CareTransport = {
    request: async () => response,
    failure: (code, message) =>
      Object.assign(Error(message), {
        code,
      }),
  };
  load(ctx, root, 'care-http-adapter.js');
  const metadata = {
    TYPES: {
      health: '健康',
      medication: '用药',
      offline: '离线',
      fence: '安全',
    },
    STATES: {
      new: '新预警',
      claimed: '已接单',
      processing: '处理中',
      escalated: '已升级',
      resolved: '已解决',
      false_alarm: '误报',
    },
    METRICS: {},
    isOpen: (e) => !['resolved', 'false_alarm'].includes(e.state),
  };
  const api = ctx.CareHttpAdapter.create(metadata),
    out = [];
  async function step(label, value) {
    response = value;
    try {
      out.push({
        label,
        value: await api.snapshot(),
        staff: api.STAFF,
        now: api.now(),
      });
    } catch (e) {
      out.push({
        label,
        code: e.code,
        message: e.message,
      });
    }
  }
  await step('valid', structuredClone(fixture));
  const changes = [
    (v) => (v.revision = -1),
    (v) => (v.serverTime = 'invalid'),
    (v) => v.staff.push(v.staff[0]),
    (v) => (v.residents[1].id = 9),
    (v) => (v.residents[1].responsible = 'private'),
    (v) =>
      (v.residents[1].metricStates = {
        heartRate: 'invalid',
      }),
    (v) => (v.residents[1].contacts = {}),
    (v) => v.residents[1].doses.push(v.residents[1].doses[0]),
    (v) => v.events.push(v.events[0]),
    (v) => (v.events[0].residentId = 999),
    (v) => (v.events[0].type = 'invalid'),
    (v) => (v.events[0].history[0].note = {}),
    (v) => (v.events[0].notification.cycle = 0),
    (v) => (v.logs[0].residentId = 999),
    (v) => delete v.serverTime,
  ];
  for (const [i, change] of changes.entries()) {
    const value = structuredClone(fixture);
    change(value);
    await step('http ' + i, value);
  }
  return JSON.parse(
    JSON.stringify({
      keys: Object.keys(api),
      out,
    }),
  );
}
function hashes(value) {
  const crypto = require('crypto');
  return Object.fromEntries(
    Object.entries(value).map(([name, result]) => [
      name,
      {
        keys: result.keys,
        scenarios: Object.fromEntries(
          result.out.map((s) => [s.label, crypto.createHash('sha256').update(JSON.stringify(s)).digest('hex')]),
        ),
      },
    ]),
  );
}
(async () => {
  const fixture = JSON.parse(fs.readFileSync(path.join(__dirname, 'fixtures/render-fixture.json'), 'utf8'));
  const after = {
    demo: await demo(current),
    http: await http(current, fixture),
  };
  if (baseline) {
    const before = {
      demo: await demo(baseline),
      http: await http(baseline, fixture),
    };
    assert.deepStrictEqual(after, before);
  }
  const referencePath = path.join(__dirname, 'fixtures/frontend-api-contract.json');
  if (process.env.YIHE_WRITE_REFERENCE === '1' && baseline)
    fs.writeFileSync(referencePath, JSON.stringify(hashes(after), null, 2) + '\n');
  assert.deepStrictEqual(hashes(after), JSON.parse(fs.readFileSync(referencePath, 'utf8')));
  console.log(
    'PASS:',
    after.demo.out.length,
    'demo and',
    after.http.out.length,
    'HTTP scenarios preserve data, errors, storage and public keys',
  );
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
async function checkCorruptDemo(api, items, step) {
  const good = items.get(api.KEY);
  const mutations = [
    (s) => (s.revision = -1),
    (s) => (s.residents[1].responsible = 'unknown'),
    (s) => (s.residents[1].contacts = {}),
    (s) => (s.residents[1].doses[0].dueAt = 'invalid'),
    (s) => (s.events[0].notification.cycle = -1),
    (s) => (s.events[0].history[0].actor = ''),
    (s) => (s.conditions[Object.keys(s.conditions)[0]].eventId = 'missing'),
  ];
  for (const [i, change] of mutations.entries()) {
    const value = JSON.parse(good);
    change(value);
    items.set(api.KEY, JSON.stringify(value));
    await step('invalid demo ' + i, () => api.snapshot());
  }
  items.set(api.KEY, good);
}
async function checkDemoDoses(api, e, step) {
  await step('dose', () => api.addDose(1, '合成用药', new Date(now).toISOString(), 'staff-1', 'intent'));
  await step('dose retry', () => api.addDose(1, '合成用药', new Date(now).toISOString(), 'staff-1', 'intent'));
  await step('dose conflict', () => api.addDose(1, '变化用药', new Date(now).toISOString(), 'staff-1', 'intent'));
  e.advance(40 * 60000);
  await step('overdue scan', () => api.scan());
  const dose = (await api.snapshot()).residents[1].doses[0];
  await step('confirm', () => api.confirmDose(1, dose.id));
  await step('confirm retry', () => api.confirmDose(1, dose.id));
  await step('parallel patches', () =>
    Promise.allSettled([
      api.saveResident(
        1,
        {
          age: 75,
        },
        undefined,
        3,
      ),
      api.saveResident(
        1,
        {
          age: 76,
        },
        undefined,
        3,
      ),
    ]).then((x) =>
      x.map((r) => ({
        status: r.status,
        code: r.reason?.code,
      })),
    ),
  );
}
async function checkDemoRecovery(api, step) {
  await step('repeat health', () => api.simulate(1, 'health'));
  await step('recover', () => api.simulate(1, 'recover'));
  await step('offline', () => api.simulate(1, 'offline'));
  await step('fence', () => api.simulate(1, 'fence'));
}
async function checkDemoTransitions(api, step) {
  await step('health', () => api.simulate(1, 'health'));
  let event = (await api.snapshot()).events.find((x) => x.type === 'health');
  await step('presented', () => api.markPresented([event.id], 'staff-1', [event.notification.cycle]));
  await step('stale presented', () => api.markPresented([event.id], 'staff-1', [99]));
  await step('claim', () => api.transition(event.id, 'claim', 'staff-1', '', event.revision));
  event = (await api.snapshot()).events.find((x) => x.id === event.id);
  await step('processing', () => api.transition(event.id, 'processing', 'staff-1', '', event.revision));
  event = (await api.snapshot()).events.find((x) => x.id === event.id);
  await step('escalated', () => api.transition(event.id, 'escalated', 'staff-1', '备注', event.revision, 'staff-2'));
}
async function checkDemoProfile(api, step) {
  await step('init', () => api.init());
  await step('profile', () =>
    api.saveResident(
      1,
      {
        name: '新名',
        age: 74,
      },
      undefined,
      0,
    ),
  );
  await step('conflict', () =>
    api.saveResident(
      1,
      {
        name: '冲突',
      },
      undefined,
      0,
    ),
  );
  await step('contact updates', () =>
    api.saveResident(
      1,
      {
        contactUpdates: [
          {
            index: 0,
            value: {
              name: '替换',
              phone: '13800138000',
            },
          },
          {
            index: 1,
            value: {
              name: '新增',
              phone: '13900138000',
            },
          },
        ],
      },
      'staff-1',
      1,
    ),
  );
  await step('invalid contacts', () =>
    api.saveResident(
      1,
      {
        contacts: [
          {
            name: '',
            phone: 'x',
          },
        ],
      },
      'staff-1',
      2,
    ),
  );
  await step('responsible', () =>
    api.saveResident(
      1,
      {
        responsible: 'staff-2',
      },
      'staff-1',
      2,
    ),
  );
}
function seedLegacyData(ctx, items) {
  ctx.RESIDENT_DATA = {
    1: {
      id: 1,
      name: '居民一',
      age: 72,
      careLevel: '自理',
      heartRate: 75,
      bloodOxygen: 96,
      temperature: 36.5,
      systolic: 125,
      diastolic: 75,
      bloodSugar: 5.5,
    },
    2: {
      id: 2,
      name: '居民二',
      age: 78,
      careLevel: '护理',
      heartRate: 75,
      bloodOxygen: 96,
      temperature: 36.5,
      systolic: 125,
      diastolic: 75,
      bloodSugar: 5.5,
    },
  };
  items.set(
    'hm-p-profile-1',
    JSON.stringify({
      name: '旧资料',
      age: 73,
    }),
  );
  items.set(
    'hm-p-emergency-1',
    JSON.stringify({
      n1: '家属',
      p1: '13800138000',
    }),
  );
}
