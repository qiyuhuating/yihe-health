// Platform-independent protocol logic. Sample IDs and binding identity survive retries.
export interface WatchConfig {
  server: string;
  deviceNo: string;
}
export class DeviceRequestError extends Error {
  code: string;
  status: number;
  constructor(code: string, message: string, status: number = 0) {
    super(message);
    this.code = code;
    this.status = status;
  }
}
export interface SimulatorState { day: string; steps: number; }
export interface DeviceReply {
  returnCode: string;
  code?: string;
  reference?: string;
  message?: string;
  deviceId?: number;
  bound?: number;
  residentId?: number;
  residentName?: string;
  bindingVersion?: number;
  accepted?: number;
  duplicates?: number;
  duplicate?: boolean;
  incidentId?: number;
  incidentState?: string;
  notificationState?: string;
  protocolVersion?: number;
  maxBatchSize?: number;
}
export interface Credentials { deviceNo: string; secret: string; }
export interface LoginRequest extends Credentials { platform: string; protocolVersion: number; }
export interface Binding {
  server: string;
  deviceNo: string;
  residentId: number;
  version: number;
}
export interface SampleMetadata {
  simulated: string;
  counterTimezone?: string;
  sensor?: string;
  sensorTimestamp?: string;
  timestampBasis?: string;
  counterBasis?: string;
}
export interface HealthSample {
  recordId: string;
  source: string;
  sourceDevice: string;
  sourceVersion: string;
  metric: string;
  unit: string;
  value: number;
  start: string;
  end: string;
  semantics: string;
  metadata: SampleMetadata;
  version: number;
}
export interface PendingBatch { binding: Binding; samples: HealthSample[]; }
export interface BatchRequest {
  deviceNo: string;
  secret: string;
  bindingVersion: number;
  samples: HealthSample[];
}
export interface PendingSOS { binding: Binding; requestId: string; incidentId?: number; }
export interface SOSRequest extends Credentials { requestId: string; bindingVersion: number; }
export function readPendingSOS(raw: string): PendingSOS[] {
  if (!raw) { return []; }
  const saved = JSON.parse(raw) as PendingSOS | PendingSOS[];
  return Array.isArray(saved) ? saved : [saved];
}
export function pendingForBinding(items: PendingSOS[], binding: Binding): PendingSOS | undefined {
  return items.find((item: PendingSOS) => !item.incidentId && sameBinding(item.binding, binding));
}
export interface HeartbeatRequest extends Credentials { appVersion: string; }
export interface BindRequest extends Credentials { code: string; }

export function normalizeConfig(server: string, deviceNo: string): WatchConfig {
  const url = server.trim().replace(/\/+$/, '');
  if (!/^https?:\/\/(?:\[[0-9a-fA-F:]+\]|[A-Za-z0-9.-]+)(?::[0-9]{1,5})?$/.test(url)) {
    throw new Error('服务器地址须为 http(s)://主机:端口，不含路径或账号');
  }
  if (url.startsWith('http:') && !/^http:\/\/(?:127\.0\.0\.1|localhost|\[::1\])(?::[0-9]{1,5})?$/.test(url)) {
    throw new Error('真机连接请使用 HTTPS；HTTP 仅用于本机模拟器端口转发');
  }
  const no = deviceNo.trim().toUpperCase();
  if (!/^[A-Z0-9_-]{1,32}$/.test(no)) {
    throw new Error('设备号须为 1–32 位字母、数字、下划线或短横线');
  }
  return { server: url, deviceNo: no };
}

export const WATCH_APP_VERSION: string = 'harmonyos-0.2.0';
export const WATCH_PROTOCOL_VERSION: number = 1;

export function verifyHandshake(reply: DeviceReply): void {
  if (reply.protocolVersion !== WATCH_PROTOCOL_VERSION || reply.maxBatchSize !== 200) {
    throw new Error('后台不支持当前鸿蒙采样协议，请更新后台后重试');
  }
}

export function bindingFrom(config: WatchConfig, reply: DeviceReply): Binding {
  if (reply.bound !== 1 || reply.residentId === undefined || reply.bindingVersion === undefined) {
    throw new Error('设备尚未绑定长者，请输入后台生成的设备绑定码');
  }
  return bindingIdentity(config, reply.residentId, reply.bindingVersion);
}
export function bindingIdentity(config: WatchConfig, residentId: number, version: number): Binding {
  if (!Number.isSafeInteger(residentId) || residentId <= 0 || !Number.isSafeInteger(version) || version <= 0) {
    throw new Error('设备绑定身份无效，请核查设备与绑定版本');
  }
  return { server: config.server, deviceNo: config.deviceNo,
    residentId: residentId, version: version };
}
export function sameBinding(a: Binding, b: Binding): boolean {
  return a.server === b.server && a.deviceNo === b.deviceNo && a.residentId === b.residentId && a.version === b.version;
}
export function isExpiredBinding(saved: Binding, live: Binding): boolean {
  return saved.server === live.server && saved.deviceNo === live.deviceNo && saved.version < live.version;
}
export function enqueue(current: PendingBatch | undefined, binding: Binding,
  samples: HealthSample[]): PendingBatch {
  if (current && !sameBinding(current.binding, binding)) {
    throw new Error('待上传记录属于旧绑定，请先处理旧记录；不会归入新长者');
  }
  const prior: HealthSample[] = current ? current.samples : [];
  if (prior.length + samples.length > 200) {
    throw new Error('待上传记录已满，请先上传；本次采样未入队');
  }
  return { binding: binding, samples: prior.concat(samples) };
}
export function batchRequest(batch: PendingBatch, live: Binding, secret: string): BatchRequest {
  if (isExpiredBinding(batch.binding, live)) {
    throw new DeviceRequestError('EPOCH_EXPIRED', '绑定纪元已过期，禁止补传旧记录');
  }
  if (!sameBinding(batch.binding, live)) {
    throw new Error('服务器或设备绑定已变化，禁止补传旧记录');
  }
  return { deviceNo: live.deviceNo, secret: secret,
    bindingVersion: batch.binding.version, samples: batch.samples };
}
export function makeSamples(deviceNo: string, recordId: string, at: Date,
  heart: number, steps: number): HealthSample[] {
  const instant = at.toISOString();
  // Counter resets at a UTC day boundary; declare the same day basis to the server.
  const dayStart = new Date(Date.UTC(at.getUTCFullYear(), at.getUTCMonth(), at.getUTCDate())).toISOString();
  const heartSample: HealthSample = {
    recordId: recordId + '-hr', source: 'yihe-watch-sim', sourceDevice: deviceNo,
    sourceVersion: '0.1.0', metric: 'heart_rate', unit: 'count/min', value: heart,
    start: instant, end: instant, semantics: 'instant', version: 1,
    metadata: { simulated: 'true' }
  };
  const stepSample: HealthSample = {
    recordId: recordId + '-steps', source: 'yihe-watch-sim', sourceDevice: deviceNo,
    sourceVersion: '0.1.0', metric: 'steps', unit: 'count', value: steps,
    start: dayStart, end: instant, semantics: 'daily_total', version: 1,
    metadata: { simulated: 'true', counterTimezone: 'UTC' }
  };
  return [heartSample, stepSample];
}
