import { cryptoFramework } from '@kit.CryptoArchitectureKit';
import { util } from '@kit.ArkTS';
import { DeviceRequestError } from '../model/Protocol';

export interface DeviceSignature { timestamp: string; nonce: string; signature: string; }

// Matches the existing Go device signature protocol. Each transport attempt gets
// a fresh nonce; durable health/SOS IDs are independent of transport signatures.
export class DeviceSigner {
  private encoder: util.TextEncoder = new util.TextEncoder();
  private hex(bytes: Uint8Array): string {
    let result = '';
    for (const byte of bytes) { result += byte.toString(16).padStart(2, '0'); }
    return result;
  }
  async sign(deviceNo: string, secret: string, serialized: string): Promise<DeviceSignature> {
    if (!canIUse('SystemCapability.Security.CryptoFramework')) {
      throw new DeviceRequestError('SIGNING_UNAVAILABLE', '系统安全签名不可用，未发送请求，记录保留');
    }
    const raw = this.encoder.encodeInto(serialized);
    const keyBytes = this.encoder.encodeInto(secret);
    let key: cryptoFramework.SymKey | undefined = undefined;
    try {
      if (!raw.length || raw.length > 1048576 || !keyBytes.length || keyBytes.length > 4096) {
        throw new Error('Invalid signing input');
      }
      const seconds = Math.floor(Date.now() / 1000);
      if (!Number.isSafeInteger(seconds) || seconds < 0) { throw new Error('Invalid device clock'); }
      const random = (await cryptoFramework.createRandom().generateRandom(16)).data;
      if (random.length !== 16) { throw new Error('Invalid secure nonce'); }
      const timestamp = seconds.toString();
      const nonce = this.hex(random);
      const digest = cryptoFramework.createMd('SHA256');
      for (let offset = 0; offset < raw.length; offset += 16384) {
        await digest.update({ data: raw.slice(offset, offset + 16384) });
      }
      const hash = (await digest.digest()).data;
      if (hash.length !== 32) { throw new Error('Invalid SHA256 result'); }
      // Bare HMAC imports the original variable-length secret; HMAC|SHA256
      // would require exactly 32 bytes and reject valid provisioned secrets.
      key = await cryptoFramework.createSymKeyGenerator('HMAC').convertKey({ data: keyBytes });
      const mac = cryptoFramework.createMac('SHA256');
      await mac.init(key);
      await mac.update({ data: this.encoder.encodeInto(deviceNo.trim().toUpperCase() + ':' + timestamp + ':' + nonce + ':' + this.hex(hash)) });
      const signature = (await mac.doFinal()).data;
      if (signature.length !== 32) { throw new Error('Invalid HMAC result'); }
      return { timestamp: timestamp, nonce: nonce, signature: this.hex(signature) };
    } catch (error) {
      throw new DeviceRequestError('SIGNING_UNAVAILABLE', '系统安全签名失败，未发送请求，记录保留；请检查设备时间与系统能力');
    } finally {
      raw.fill(0); keyBytes.fill(0);
      try { key?.clearMem(); } catch (error) { /* Never replace the original failure with cleanup errors. */ }
    }
  }
}
