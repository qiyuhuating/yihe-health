import { describe, expect, it, Level, Size, TestType } from '@ohos/hypium';
import { abilityDelegatorRegistry } from '@kit.TestKit';
import { common } from '@kit.AbilityKit';
import { http } from '@kit.NetworkKit';
import { preferences } from '@kit.ArkData';
import { WatchApi } from '../../../main/ets/services/WatchApi';
import { WatchStore } from '../../../main/ets/services/WatchStore';
import { WatchSynchronizer, SyncResult } from '../../../main/ets/services/WatchSynchronizer';
import { AutoMonitor } from '../../../main/ets/services/AutoMonitor';
import { HealthSensors, SensorRead } from '../../../main/ets/services/HealthSensors';
import { WatchConfig, SOSRequest } from '../../../main/ets/model/Protocol';

interface BridgeReply {
  ok: boolean; server?: string; deviceNo?: string; secret?: string; storeName?: string;
  residentId?: number; bindingVersion?: number;
}
interface Phase { action: string; ids?: string[]; }

const delay = (ms: number): Promise<void> => new Promise<void>((resolve) => { setTimeout(resolve, ms); });
async function until(ready: () => boolean): Promise<void> {
  const limit = Date.now() + 15000;
  while (!ready()) {
    if (Date.now() > limit) { throw new Error('Native automatic pipeline timed out'); }
    await delay(50);
  }
}
async function bridge(token: string, phase: Phase): Promise<BridgeReply> {
  const request = http.createHttp();
  try {
    const reply = await request.request('http://127.0.0.1:18797/control', {
      method: http.RequestMethod.POST, usingCache: false,
      header: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token },
      extraData: JSON.stringify(phase), expectDataType: http.HttpDataType.STRING,
      connectTimeout: 5000, readTimeout: 10000
    });
    if (reply.responseCode !== 200) { throw new Error('Dedicated backend rejected native phase: ' + phase.action); }
    const result = JSON.parse(reply.result as string) as BridgeReply;
    if (result.ok !== true) { throw new Error('Dedicated backend did not confirm native phase'); }
    return result;
  } finally { request.destroy(); }
}

// Only the physical sensor boundary is controlled. The scheduler, protocol,
// system HMAC, real HTTP transport, HUKS and Preferences remain production code.
class ControlledSensors extends HealthSensors {
  reads: number = 0;
  private received: number = 0;
  async read(context: common.UIAbilityContext, session: number = this.session(), requestPermission: boolean = true): Promise<SensorRead> {
    if (requestPermission || session !== this.session()) { throw new Error('Unexpected permission request or stale sensor session'); }
    this.reads++;
    this.received = Math.max(Date.now(), this.received + 1);
    return { observations: [
      { metric: 'heart_rate', value: 73, receivedAt: this.received, sensorTimestamp: this.reads * 1000000000 },
      { metric: 'steps', value: 100 + this.reads * 5, receivedAt: this.received, sensorTimestamp: this.reads * 1000000000 }
    ], status: 'controlled native test sensor boundary' };
  }
}

async function uiContext(): Promise<common.UIAbilityContext> {
  const delegator = abilityDelegatorRegistry.getAbilityDelegator();
  const monitor: abilityDelegatorRegistry.AbilityMonitor = { abilityName: 'NativeTestAbility', moduleName: 'entry_test' };
  await delegator.addAbilityMonitor(monitor);
  try {
    await delegator.startAbility({ bundleName: 'com.yihe.watch', moduleName: 'entry_test', abilityName: 'NativeTestAbility' });
    const ability = await delegator.waitAbilityMonitor(monitor, 5000);
    if (ability.context.abilityInfo.name !== 'NativeTestAbility') { throw new Error('Unexpected native test ability'); }
    return ability.context;
  } finally {
    await delegator.removeAbilityMonitor(monitor);
  }
}

export default function backendTests(): void {
  describe('YiheNativeBackend', () => {
    it('native_backend_pipeline', TestType.RELIABILITY | Size.LARGETEST | Level.LEVEL0, async () => {
      // aa preserves custom string arguments with their "-s " prefix.
      const token = abilityDelegatorRegistry.getArguments().parameters['-s fixtureToken'];
      if (typeof token !== 'string' || !/^[a-f0-9]{64}$/.test(token)) { throw new Error('Dedicated backend fixture token required'); }
      const fixture = await bridge(token, { action: 'fixture' });
      const server = fixture.server, deviceNo = fixture.deviceNo, secret = fixture.secret, name = fixture.storeName;
      if (server !== 'http://127.0.0.1:18798' || !deviceNo || !secret || !name || !/^yihe_native_e2e_[a-f0-9]{12}$/.test(name)) {
        throw new Error('Unsafe native fixture scope');
      }
      const context = await uiContext();
      const config: WatchConfig = { server: server, deviceNo: deviceNo };
      let store = await WatchStore.open(context, name);
      await store.saveConfig(config);
      const api = new WatchApi(), sensors = new ControlledSensors();
      let sync = new WatchSynchronizer(store, api, sensors, context);
      const login = await sync.authenticate(config, secret);
      expect(login.residentId).assertEqual(fixture.residentId);
      expect((await api.heartbeat(config, secret)).bound).assertEqual(1);
      await store.saveMonitoringEnabled(true);
      const results: SyncResult[] = [];
      let failure = '';
      const automatic = new AutoMonitor(async () => {
        try { results.push(await sync.cycle(config, secret)); return 'ok'; }
        catch (error) { failure = (error as Error).message; return 'halt'; }
      }, 1000); // Test-only acceleration; production remains 60 seconds.
      try {
        automatic.setVisible(true); automatic.setEnabled(true);
        await until(() => results.length >= 2 || !!failure);
        automatic.setVisible(false); sync.suspend();
        if (failure) { throw new Error(failure); }
        expect(results[0].sampled).assertEqual(1); expect(results[1].sampled).assertEqual(2);
        expect(store.batch()).assertUndefined();
        await bridge(token, { action: 'automatic' });
        const before = sensors.reads;
        await delay(1250);
        expect(sensors.reads).assertEqual(before);
        await bridge(token, { action: 'hidden' });
        sync.resume(); automatic.setVisible(true);
        await until(() => results.length >= 3 || !!failure);
        automatic.setEnabled(false);
        if (failure) { throw new Error(failure); }
        expect(results[2].sampled).assertEqual(1); // New step baseline on resume.
        await store.saveMonitoringEnabled(false);
        await delay(1250);
        expect(results.length).assertEqual(3);
        await bridge(token, { action: 'resumed' });

        await bridge(token, { action: 'offline' });
        const cached = await sync.cycle(config, secret);
        expect(cached.offline).assertTrue(); expect(cached.queued).assertEqual(2);
        const ids = store.batch()?.samples.map((sample) => sample.recordId);
        if (!ids) { throw new Error('Offline native queue not persisted'); }
        sync.suspend();
        await preferences.removePreferencesFromCache(context, { name: name });
        store = await WatchStore.open(context, name);
        expect(store.credential(config)).assertEqual(secret);
        expect(store.batch()?.samples.map((sample) => sample.recordId).join(',')).assertEqual(ids.join(','));
        sync = new WatchSynchronizer(store, api, new ControlledSensors(), context);
        await bridge(token, { action: 'online', ids: ids });
        expect((await sync.upload(config, store.credential(config))).uploaded).assertEqual(2);
        expect(store.batch()).assertUndefined();
        await bridge(token, { action: 'uploaded' });

        await bridge(token, { action: 'lose-ack' });
        const uncertain = await sync.cycle(config, secret);
        expect(uncertain.queued).assertEqual(1);
        const retryIDs = store.batch()?.samples.map((sample) => sample.recordId);
        if (!retryIDs) { throw new Error('Lost-ack queue missing'); }
        await bridge(token, { action: 'lost-ack-check', ids: retryIDs });
        expect((await sync.upload(config, secret)).uploaded).assertEqual(1);
        await bridge(token, { action: 'dedup-check' });

        await bridge(token, { action: 'offline-again' });
        expect((await sync.cycle(config, secret)).queued).assertEqual(2);
        const old = store.batch();
        if (!old) { throw new Error('Old binding queue missing'); }
        await store.saveSOS([{ binding: old.binding, requestId: 'native-old-binding-sos' }]);
        const rebound = await bridge(token, { action: 'rebind', ids: old.samples.map((sample) => sample.recordId) });
        // The preceding controlled outage starts production's five-second
        // backoff. Let it expire so this cycle reauthenticates the new epoch.
        await delay(5100);
        const current = await sync.cycle(config, secret);
        expect(current.binding.residentId).assertEqual(rebound.residentId);
        expect(current.binding.version).assertEqual(rebound.bindingVersion);
        expect(current.sampled).assertEqual(1); expect(store.batch()).assertUndefined();
        expect(store.pendingSOS()[0].requestId).assertEqual('native-old-binding-sos');
        await bridge(token, { action: 'rebound' });
        const sos: SOSRequest = { deviceNo: deviceNo, secret: secret, requestId: 'native-current-binding-sos', bindingVersion: current.binding.version };
        const first = await api.sos(config, sos), retry = await api.sos(config, sos);
        expect(first.incidentId).assertEqual(retry.incidentId);
        expect(retry.duplicate).assertTrue();
        expect(first.notificationState).assertEqual('no_recipient');
        await bridge(token, { action: 'sos' });
      } finally { automatic.setEnabled(false); sync.suspend(); }
    });
  });
}
