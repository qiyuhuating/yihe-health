import { rcp } from '@kit.RemoteCommunicationKit';
import { DeviceSigner, DeviceSignature } from './DeviceSigner';
import { WatchConfig, Credentials, DeviceReply, BindRequest, HeartbeatRequest,
  BatchRequest, SOSRequest, LoginRequest, DeviceRequestError, WATCH_APP_VERSION, WATCH_PROTOCOL_VERSION, verifyHandshake } from '../model/Protocol';

// A single request has a bounded lifetime; requests never log the device secret.
export class WatchApi {
  private active: Set<rcp.Session> = new Set<rcp.Session>();
  private generation: number = 0;
  private signer: DeviceSigner = new DeviceSigner();
  cancel(): void {
    this.generation++;
    for (const session of this.active) {
      try { session.cancel(); } catch (error) { /* Already cancelled by the platform. */ }
      try { session.close(); } catch (error) { /* Already closed by the platform. */ }
    }
    this.active.clear();
  }
  private async post(config: WatchConfig, path: string,
    body: Credentials | LoginRequest | BindRequest | HeartbeatRequest | BatchRequest | SOSRequest): Promise<DeviceReply> {
    const generation = this.generation;
    const serialized = JSON.stringify(body);
    let signature: DeviceSignature;
    try { signature = await this.signer.sign(config.deviceNo, body.secret, serialized); }
    catch (error) {
      if (generation !== this.generation) { throw new DeviceRequestError('CANCELLED', '操作已取消'); }
      if (error instanceof Error) { throw error; }
      throw new DeviceRequestError('SIGNING_UNAVAILABLE', '系统安全签名失败，未发送请求，记录保留');
    }
    if (generation !== this.generation) { throw new DeviceRequestError('CANCELLED', '操作已取消'); }
    if (!canIUse('SystemCapability.Collaboration.RemoteCommunication')) {
      throw new DeviceRequestError('NETWORK_UNAVAILABLE', '系统不支持可取消的网络会话，未发送请求，记录保留');
    }
    const session = rcp.createSession({ requestConfiguration: {
      transfer: { autoRedirect: false, timeout: { connectMs: 5000, transferMs: 10000 } }
    } });
    this.active.add(session);
    try {
      let response: rcp.Response;
      try { response = await session.fetch(new rcp.Request(config.server + '/api/v1/device/' + path, 'POST', {
        'content-type': 'application/json', 'cache-control': 'no-store', 'X-Device-Timestamp': signature.timestamp,
        'X-Device-Nonce': signature.nonce, 'X-Device-Signature': signature.signature
      }, serialized)); } catch (error) {
        if (generation !== this.generation) { throw new DeviceRequestError('CANCELLED', '操作已取消'); }
        throw new DeviceRequestError('NETWORK_ERROR', '网络不可用或请求超时，待上传记录已保留');
      }
      if (generation !== this.generation) { throw new DeviceRequestError('CANCELLED', '操作已取消'); }
      let reply: DeviceReply;
      try { reply = JSON.parse(response.toString() || '') as DeviceReply; }
      catch (error) { throw new DeviceRequestError('PROTOCOL_ERROR', '服务响应格式无效，待上传记录已保留', response.statusCode); }
      if (!reply || typeof reply !== 'object' || Array.isArray(reply)) {
        throw new DeviceRequestError('PROTOCOL_ERROR', '服务响应格式无效，待上传记录已保留', response.statusCode);
      }
      if (response.statusCode < 200 || response.statusCode >= 300 || reply.returnCode !== '1111') {
        const message = reply.message || '服务返回 HTTP ' + response.statusCode;
        throw new DeviceRequestError(reply.code || 'HTTP_ERROR', message + (reply.reference ? '（编号：' + reply.reference + '）' : ''), response.statusCode);
      }
      return reply;
    } finally {
      if (this.active.delete(session)) {
        try { session.close(); } catch (error) { /* Cleanup must not replace a response or its original error. */ }
      }
    }
  }
  async login(config: WatchConfig, secret: string): Promise<DeviceReply> {
    const reply = await this.post(config, 'login', { deviceNo: config.deviceNo, secret: secret,
      platform: 'harmonyos', protocolVersion: WATCH_PROTOCOL_VERSION });
    verifyHandshake(reply);
    return reply;
  }
  bind(config: WatchConfig, secret: string, code: string): Promise<DeviceReply> {
    return this.post(config, 'bind', { deviceNo: config.deviceNo, secret: secret, code: code.trim().toUpperCase() });
  }
  heartbeat(config: WatchConfig, secret: string): Promise<DeviceReply> {
    return this.post(config, 'heartbeat', { deviceNo: config.deviceNo, secret: secret, appVersion: WATCH_APP_VERSION });
  }
  upload(config: WatchConfig, body: BatchRequest): Promise<DeviceReply> {
    return this.post(config, 'healthBatch', body);
  }
  sos(config: WatchConfig, body: SOSRequest): Promise<DeviceReply> {
    return this.post(config, 'sos', body);
  }
}
