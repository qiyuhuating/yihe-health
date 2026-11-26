import { common } from '@kit.AbilityKit';
import { EncryptedPreferences } from './EncryptedPreferences';
import { WatchConfig, PendingBatch, PendingSOS, SimulatorState, Binding, bindingIdentity, sameBinding, isExpiredBinding, readPendingSOS } from '../model/Protocol';

export class WatchStore {
  private prefs: EncryptedPreferences;
  private sosStorageFailed: boolean = false;
  private batchStorageFailed: boolean = false;
  constructor(context: common.Context, preferencesName: string = 'yihe_watch_sim_v1') {
    this.prefs = new EncryptedPreferences(context, preferencesName);
  }
  static async open(context: common.Context, preferencesName: string = 'yihe_watch_sim_v1'): Promise<WatchStore> {
    const store = new WatchStore(context, preferencesName);
    await store.prefs.open();
    return store;
  }
  usable(): boolean { return this.prefs.usable() && !this.sosStorageFailed && !this.batchStorageFailed; }
  credential(config: WatchConfig): string {
    if (this.prefs.getSync('credentialServer', '') !== config.server || this.prefs.getSync('credentialDevice', '') !== config.deviceNo) { return ''; }
    return this.prefs.getSync('credential', '') as string;
  }
  async saveCredential(config: WatchConfig, secret: string): Promise<void> {
    if (secret.length < 24 || secret.length > 128) { throw new Error('设备密钥长度无效'); }
    if (this.credential(config) === secret) { return; }
    this.prefs.putSync('credentialServer', config.server);
    this.prefs.putSync('credentialDevice', config.deviceNo);
    this.prefs.putSync('credential', secret);
    // A changed secret cannot inherit an offline binding from the old credential.
    // Commit the invalidation with the replacement secret, before authentication resumes.
    this.prefs.putSync('verifiedBinding', '');
    await this.prefs.flush();
  }
  async forgetCredential(): Promise<void> {
    this.prefs.putSync('credential', '');
    this.prefs.putSync('verifiedBinding', '');
    await this.prefs.flush();
  }
  monitoringEnabled(): boolean { return this.prefs.getSync('monitoringEnabled', '0') === '1'; }
  async saveMonitoringEnabled(enabled: boolean): Promise<void> {
    this.prefs.putSync('monitoringEnabled', enabled ? '1' : '0');
    await this.prefs.flush();
  }
  verifiedBinding(config: WatchConfig): Binding | undefined {
    const raw = this.prefs.getSync('verifiedBinding', '') as string;
    if (!raw) { return undefined; }
    const saved = JSON.parse(raw) as Binding;
    if (!saved || saved.server !== config.server || saved.deviceNo !== config.deviceNo) { return undefined; }
    return bindingIdentity(config, saved.residentId, saved.version);
  }
  async rememberBinding(binding: Binding | undefined): Promise<void> {
    if (binding) {
      const config: WatchConfig = { server: binding.server, deviceNo: binding.deviceNo };
      const previous = this.verifiedBinding(config);
      if (previous && sameBinding(previous, binding)) { return; }
    } else if (!this.prefs.getSync('verifiedBinding', '')) { return; }
    this.prefs.putSync('verifiedBinding', binding ? JSON.stringify(binding) : '');
    await this.prefs.flush();
  }
  config(): WatchConfig {
    return {
      server: this.prefs.getSync('server', 'http://127.0.0.1:18796') as string,
      deviceNo: this.prefs.getSync('deviceNo', 'WATCH5_SIM_001') as string
    };
  }
  async saveConfig(config: WatchConfig): Promise<void> {
    this.prefs.putSync('server', config.server);
    this.prefs.putSync('deviceNo', config.deviceNo);
    await this.prefs.flush();
  }
  batch(): PendingBatch | undefined {
    if (this.batchStorageFailed) { throw new Error('采样保存状态未确认，请重启应用恢复记录，勿重复采样'); }
    const raw = this.prefs.getSync('batch', '') as string;
    return raw ? JSON.parse(raw) as PendingBatch : undefined;
  }
  pendingSOS(): PendingSOS[] {
    if (this.sosStorageFailed) { throw new Error('求助记录保存失败，请联系管理员核查，勿重复发送'); }
    const raw = this.prefs.getSync('sos', '') as string;
    return readPendingSOS(raw);
  }
  async saveBatch(batch: PendingBatch | undefined): Promise<void> {
    if (this.batchStorageFailed) { throw new Error('采样保存状态未确认，请重启应用恢复记录'); }
    try {
      this.prefs.putSync('batch', batch ? JSON.stringify(batch) : '');
      await this.prefs.flush();
    } catch (error) {
      this.batchStorageFailed = true;
      throw new Error('采样保存状态未确认，请重启应用恢复记录，勿重复采样');
    }
  }
  async discardExpiredBatch(live: Binding): Promise<boolean> {
    const batch = this.batch();
    if (!batch || !isExpiredBinding(batch.binding, live)) { return false; }
    await this.saveBatch(undefined);
    return true;
  }
  async saveSOS(sos: PendingSOS[]): Promise<void> {
    if (this.sosStorageFailed) { throw new Error('求助记录保存失败，请联系管理员核查，勿重复发送'); }
    const previous = this.prefs.getSync('sos', '') as string;
    try {
      this.prefs.putSync('sos', sos.length ? JSON.stringify(sos) : '');
      await this.prefs.flush();
    } catch (error) {
      this.sosStorageFailed = true;
      try { this.prefs.putSync('sos', previous); } catch (restoreError) { /* Keep sending disabled. */ }
      throw new Error('求助记录保存失败，请联系管理员核查，勿重复发送');
    }
  }
  simulator(): SimulatorState {
    return { day: this.prefs.getSync('counterDay', '') as string,
      steps: this.prefs.getSync('counterSteps', 1280) as number };
  }
  async saveSimulator(state: SimulatorState): Promise<void> {
    this.prefs.putSync('counterDay', state.day);
    this.prefs.putSync('counterSteps', state.steps);
    await this.prefs.flush();
  }
  nextID(): string {
    // Monotonic persistent sequence prevents collisions across restart / clock rollback.
    const sequence = (this.prefs.getSync('sequence', 0) as number) + 1;
    this.prefs.putSync('sequence', sequence);
    return Date.now().toString(36) + '-' + sequence.toString(36);
  }
}
