import { common } from '@kit.AbilityKit';
import { WatchApi } from './WatchApi';
import { WatchStore } from './WatchStore';
import { HealthSensors } from './HealthSensors';
import { NativeSampling } from '../model/NativeSampling';
import { Binding, WatchConfig, DeviceReply, DeviceRequestError, HealthSample,
  bindingFrom, batchRequest, enqueue, sameBinding } from '../model/Protocol';

export interface SyncResult {
  binding: Binding;
  sampled: number;
  uploaded: number;
  queued: number;
  offline: boolean;
  note: string;
  heart?: number;
  counter?: number;
}

// The UI and automatic scheduler use this same transaction boundary. No
// background extension opens a second Preferences writer.
export class WatchSynchronizer {
  private store: WatchStore;
  private api: WatchApi;
  private sensors: HealthSensors;
  private context: common.UIAbilityContext;
  private sampling: NativeSampling = new NativeSampling();
  private owner: Binding | undefined = undefined;
  private generation: number = 0;
  private active: boolean = true;
  private failures: number = 0;
  private retryAt: number = 0;
  private observedTime: number = 0;

  constructor(store: WatchStore, api: WatchApi, sensors: HealthSensors, context: common.UIAbilityContext) {
    this.store = store; this.api = api; this.sensors = sensors; this.context = context;
  }
  reset(): void {
    this.generation++; this.sampling.reset(); this.owner = undefined;
    this.failures = 0; this.retryAt = 0;
    this.sensors.cancel(); this.api.cancel();
  }
  suspend(): void { this.active = false; this.reset(); this.sensors.suspend(); }
  resume(): void { this.active = true; this.sensors.resume(); }
  private current(generation: number): void {
    if (!this.active || generation !== this.generation) { throw new DeviceRequestError('CANCELLED', '操作已取消'); }
  }
  private validate(secret: string): number {
    this.current(this.generation);
    if (secret.length < 24 || secret.length > 128) { throw new DeviceRequestError('AUTH_REQUIRED', '请先认证设备，再启用自动监测'); }
    if (!this.store.usable()) { throw new DeviceRequestError('STORAGE_ERROR', '本地保存状态未确认，请重启恢复记录，自动监测已暂停'); }
    // A quarantined store must block network work as well as new samples.
    this.store.batch();
    return this.generation;
  }
  private now(): number {
    const now = Date.now();
    if (now < this.observedTime) { this.retryAt = 0; this.failures = 0; }
    this.observedTime = now;
    return now;
  }
  private transient(error: DeviceRequestError): boolean { return error.code === 'NETWORK_ERROR' || error.status >= 500; }
  private deferNetwork(): void {
    this.failures = Math.min(this.failures + 1, 6);
    this.retryAt = this.now() + Math.min(5000 * Math.pow(2, this.failures - 1), 300000);
  }
  async authenticate(config: WatchConfig, secret: string, clearBackoff: boolean = true): Promise<DeviceReply> {
    const generation = this.validate(secret);
    try {
      const reply = await this.api.login(config, secret);
      this.current(generation);
      await this.store.saveCredential(config, secret);
      this.current(generation);
      const binding = reply.bound === 1 ? bindingFrom(config, reply) : undefined;
      await this.store.rememberBinding(binding);
      this.current(generation);
      if (binding) {
        if (await this.store.discardExpiredBatch(binding)) { this.sampling.reset(); }
        this.current(generation);
      }
      if (clearBackoff) { this.failures = 0; this.retryAt = 0; }
      return reply;
    } catch (error) {
      this.current(generation);
      const failure = error as DeviceRequestError;
      if (failure.status === 401 || failure.status === 403) { await this.store.rememberBinding(undefined); }
      if (error instanceof Error) { throw error; }
      throw new Error('同步未完成，待上传记录已保留');
    }
  }
  private async binding(config: WatchConfig, secret: string, offline: boolean): Promise<Binding> {
    const cached = this.store.credential(config) === secret ? this.store.verifiedBinding(config) : undefined;
    if (offline && cached && this.now() < this.retryAt) { return cached; }
    try {
      const reply = await this.authenticate(config, secret, false);
      if (reply.bound !== 1) { throw new DeviceRequestError('NOT_BOUND', '设备尚未绑定，自动监测已暂停'); }
      return bindingFrom(config, reply);
    } catch (error) {
      if (!offline || !cached || !this.transient(error as DeviceRequestError)) {
        if (error instanceof Error) { throw error; }
        throw new Error('同步未完成，待上传记录已保留');
      }
      this.deferNetwork();
      return cached;
    }
  }
  private async capture(binding: Binding, permission: boolean, generation: number): Promise<SyncResult> {
    this.current(generation);
    const existing = this.store.batch();
    if (existing && !sameBinding(existing.binding, binding)) {
      throw new DeviceRequestError('EPOCH_EXPIRED', '旧绑定记录尚未核实，禁止混入新采样');
    }
    const report: SyncResult = { binding: binding, sampled: 0, uploaded: 0,
      queued: existing ? existing.samples.length : 0, offline: this.retryAt > this.now(), note: '' };
    if (report.queued > 198) { report.note = '本机队列已满，保留全部旧记录，联网确认后继续采样'; return report; }
    if (!this.owner || !sameBinding(this.owner, binding)) { this.sampling.reset(); this.owner = binding; }
    const read = await this.sensors.read(this.context, this.sensors.session(), permission);
    this.current(generation);
    const candidate = this.sampling.copy();
    let samples: HealthSample[] = [];
    for (const observation of read.observations) {
      samples = samples.concat(candidate.accept(binding, this.store.nextID(), observation));
      if (observation.metric === 'heart_rate') { report.heart = observation.value; }
      if (observation.metric === 'steps') { report.counter = observation.value; }
    }
    if (samples.length) {
      const batch = enqueue(existing, binding, samples);
      await this.store.saveBatch(batch);
      this.current(generation);
      report.queued = batch.samples.length;
    }
    this.sampling = candidate;
    report.sampled = samples.length; report.note = read.status;
    return report;
  }
  private async send(config: WatchConfig, secret: string, binding: Binding, generation: number): Promise<number> {
    this.current(generation);
    const batch = this.store.batch();
    if (!batch) { return 0; }
    let reply: DeviceReply;
    try { reply = await this.api.upload(config, batchRequest(batch, binding, secret)); }
    catch (error) {
      this.current(generation);
      const failure = error as DeviceRequestError;
      if (failure.status === 401 || failure.status === 403) { await this.store.rememberBinding(undefined); }
      if (error instanceof Error) { throw error; }
      throw new Error('上传未完成，待上传记录已保留');
    }
    this.current(generation);
    if (reply.accepted !== batch.samples.length) { throw new DeviceRequestError('ACK_MISMATCH', '上传确认数量不匹配，记录已保留'); }
    await this.store.saveBatch(undefined);
    this.current(generation);
    this.failures = 0; this.retryAt = 0;
    return batch.samples.length;
  }
  async sample(config: WatchConfig, secret: string, permission: boolean = true): Promise<SyncResult> {
    const generation = this.validate(secret);
    const binding = await this.binding(config, secret, true);
    this.current(generation);
    return this.capture(binding, permission, generation);
  }
  async upload(config: WatchConfig, secret: string): Promise<SyncResult> {
    const generation = this.validate(secret);
    const binding = await this.binding(config, secret, false);
    this.current(generation);
    const uploaded = await this.send(config, secret, binding, generation);
    return { binding: binding, sampled: 0, uploaded: uploaded, queued: 0, offline: false, note: '' };
  }
  async cycle(config: WatchConfig, secret: string): Promise<SyncResult> {
    const generation = this.validate(secret);
    const binding = await this.binding(config, secret, true);
    this.current(generation);
    let uploaded = 0;
    if (this.now() >= this.retryAt) {
      try { uploaded += await this.send(config, secret, binding, generation); }
      catch (error) {
        this.current(generation);
        if (!this.transient(error as DeviceRequestError)) {
          if (error instanceof Error) { throw error; }
          throw new Error('同步未完成，待上传记录已保留');
        }
        this.deferNetwork();
      }
    }
    const result = await this.capture(binding, false, generation);
    if (this.now() >= this.retryAt) {
      try { uploaded += await this.send(config, secret, binding, generation); }
      catch (error) {
        this.current(generation);
        if (!this.transient(error as DeviceRequestError)) {
          if (error instanceof Error) { throw error; }
          throw new Error('同步未完成，待上传记录已保留');
        }
        this.deferNetwork();
      }
    }
    result.uploaded = uploaded;
    const queued = this.store.batch(); result.queued = queued ? queued.samples.length : 0;
    result.offline = this.now() < this.retryAt;
    if (!result.offline && !result.queued) { this.failures = 0; this.retryAt = 0; }
    return result;
  }
}
