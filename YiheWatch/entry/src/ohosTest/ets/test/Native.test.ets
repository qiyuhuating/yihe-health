import { describe, expect, it, Level, Size, TestType } from '@ohos/hypium';
import { abilityDelegatorRegistry } from '@kit.TestKit';
import { preferences } from '@kit.ArkData';
import { DeviceSigner } from '../../../main/ets/services/DeviceSigner';
import { WatchVault } from '../../../main/ets/services/WatchVault';
import { WatchStore } from '../../../main/ets/services/WatchStore';
import { bindingIdentity, makeSamples, enqueue, PendingBatch, WatchConfig } from '../../../main/ets/model/Protocol';

interface Envelope { version: number; nonce: string; ciphertext: string; }

export default function nativeTests(): void {
  describe('YiheNative', () => {
    it('native_signing', TestType.SECURITY | Size.SMALLTEST | Level.LEVEL0, async () => {
      // Deliberately public test key/data. Never use a provisioned credential in
      // this proof: the host verifier independently checks the native result.
      const secret = 'public-native-test-secret-0123456789';
      const raw = JSON.stringify({ deviceNo: 'WATCH_NATIVE_TEST', secret: secret, name: '田风', pad: 'Native汉'.repeat(6000) });
      const signer = new DeviceSigner();
      const first = await signer.sign(' watch_native_test ', secret, raw);
      const second = await signer.sign('WATCH_NATIVE_TEST', secret, raw);
      expect(/^[a-f0-9]{32}$/.test(first.nonce)).assertTrue();
      expect(/^[a-f0-9]{64}$/.test(first.signature)).assertTrue();
      expect(first.nonce !== second.nonce).assertTrue();
      await abilityDelegatorRegistry.getAbilityDelegator().print('SIGNATURE_PROOF:' + JSON.stringify(first));
    });
    it('native_vault_authentication', TestType.SECURITY | Size.MEDIUMTEST | Level.LEVEL0, async () => {
      const vault = new WatchVault();
      const raw = 'public synthetic health data 田风 '.repeat(3000);
      const encrypted = await vault.seal(raw);
      expect(await vault.open(encrypted)).assertEqual(raw);
      const envelope = JSON.parse(encrypted) as Envelope;
      envelope.ciphertext = (envelope.ciphertext[0] === 'A' ? 'B' : 'A') + envelope.ciphertext.slice(1);
      let rejected = false;
      try { await vault.open(JSON.stringify(envelope)); } catch (error) { rejected = true; }
      expect(rejected).assertTrue();
      // A failed authentication must not replace the key or damage good data.
      expect(await vault.open(encrypted)).assertEqual(raw);
    });
    it('native_store_disk_reload', TestType.RELIABILITY | Size.MEDIUMTEST | Level.LEVEL0, async () => {
      const context = abilityDelegatorRegistry.getAbilityDelegator().getAppContext().createModuleContext('entry_test');
      // API 24 can share a Preferences directory across module contexts. Use a
      // distinct file name as well; never treat production records as a fixture.
      const name = 'yihe_watch_native_test_v1';
      const store = await WatchStore.open(context, name);
      const config: WatchConfig = { server: 'https://native.example.com', deviceNo: 'WATCH_NATIVE_TEST' };
      const binding = bindingIdentity(config, 7, 3);
      const secret = 'public-native-test-secret-0123456789';
      let batch: PendingBatch | undefined = undefined;
      for (let i = 0; i < 100; i++) { batch = enqueue(batch, binding, makeSamples(config.deviceNo, 'native-test-' + i, new Date(), 72, i)); }
      await store.saveConfig(config); await store.saveCredential(config, secret);
      await store.rememberBinding(binding); await store.saveMonitoringEnabled(true);
      await store.saveBatch(batch); await store.saveSOS([{ binding: binding, requestId: 'native-test-sos' }]);
      const disk = preferences.getPreferencesSync(context, { name: name });
      const encrypted = disk.getSync('encryptedStateV1', '') as string;
      expect(encrypted.length > 0).assertTrue();
      expect(encrypted.includes(secret) || encrypted.includes('native-test-0')).assertFalse();
      expect(disk.getSync('batch', '')).assertEqual('');
      await preferences.removePreferencesFromCache(context, { name: name });
      const recovered = await WatchStore.open(context, name);
      expect(recovered.batch()?.samples.length).assertEqual(200);
      expect(recovered.credential(config)).assertEqual(secret);
      expect(recovered.monitoringEnabled()).assertTrue();
      expect(recovered.verifiedBinding(config)?.version).assertEqual(3);
      expect(recovered.pendingSOS()[0].requestId).assertEqual('native-test-sos');
      await recovered.forgetCredential();
      expect(recovered.credential(config)).assertEqual('');
      expect(recovered.batch()?.samples.length).assertEqual(200);
      expect(recovered.pendingSOS()[0].requestId).assertEqual('native-test-sos');
    });
  });
}
