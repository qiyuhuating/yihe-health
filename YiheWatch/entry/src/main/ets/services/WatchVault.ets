import { huks } from '@kit.UniversalKeystoreKit';
import { cryptoFramework } from '@kit.CryptoArchitectureKit';
import { util } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';

interface VaultEnvelope { version: number; nonce: string; ciphertext: string; }

// Only the alias leaves HUKS. No AES key is exported to Preferences or ArkTS.
export class WatchVault {
  private static creating: Promise<void> | undefined = undefined;
  private readonly alias: string = 'yihe.watch.vault.aesgcm.v1';
  private readonly maxBytes: number = 1024 * 1024;
  private encoder: util.TextEncoder = new util.TextEncoder();
  private base64: util.Base64Helper = new util.Base64Helper();

  private properties(purpose: number): huks.HuksParam[] {
    return [
      { tag: huks.HuksTag.HUKS_TAG_ALGORITHM, value: huks.HuksKeyAlg.HUKS_ALG_AES },
      { tag: huks.HuksTag.HUKS_TAG_KEY_SIZE, value: huks.HuksKeySize.HUKS_AES_KEY_SIZE_256 },
      { tag: huks.HuksTag.HUKS_TAG_PURPOSE, value: purpose },
      { tag: huks.HuksTag.HUKS_TAG_PADDING, value: huks.HuksKeyPadding.HUKS_PADDING_NONE },
      { tag: huks.HuksTag.HUKS_TAG_BLOCK_MODE, value: huks.HuksCipherMode.HUKS_MODE_GCM }
    ];
  }

  private async key(create: boolean): Promise<void> {
    let exists: boolean = false;
    try { exists = await huks.isKeyItemExist(this.alias, { properties: [] }); }
    catch (error) {
      // API 9 implementations may reject missing aliases rather than return false.
      if ((error as BusinessError).code !== 12000011) { throw new Error('系统密钥库不可用，未读取或覆盖本地记录'); }
    }
    if (exists) { return; }
    if (!create) { throw new Error('本地加密密钥不可用，原记录已保留，请联系管理员'); }
    if (!WatchVault.creating) {
      WatchVault.creating = huks.generateKeyItem(this.alias, { properties: this.properties(
        huks.HuksKeyPurpose.HUKS_KEY_PURPOSE_ENCRYPT | huks.HuksKeyPurpose.HUKS_KEY_PURPOSE_DECRYPT) });
    }
    try { await WatchVault.creating; }
    finally { WatchVault.creating = undefined; }
  }

  private async crypt(input: Uint8Array, nonce: Uint8Array, encrypt: boolean): Promise<Uint8Array> {
    const properties = this.properties(encrypt ? huks.HuksKeyPurpose.HUKS_KEY_PURPOSE_ENCRYPT : huks.HuksKeyPurpose.HUKS_KEY_PURPOSE_DECRYPT);
    properties.push({ tag: huks.HuksTag.HUKS_TAG_NONCE, value: nonce });
    properties.push({ tag: huks.HuksTag.HUKS_TAG_ASSOCIATED_DATA, value: this.encoder.encodeInto('yihe.watch.preferences.v1') });
    if (!encrypt) { properties.push({ tag: huks.HuksTag.HUKS_TAG_AE_TAG, value: input.slice(input.length - 16) }); }
    const data = encrypt ? input : input.subarray(0, input.length - 16);
    const session = await huks.initSession(this.alias, { properties: properties });
    const pieces: Uint8Array[] = [];
    try {
      // A full 200-sample queue exceeds the single-call HUKS input limit.
      for (let offset = 0; offset < data.length; offset += 16384) {
        const result = await huks.updateSession(session.handle, { properties: properties, inData: data.slice(offset, offset + 16384) });
        if (result && result.outData) { pieces.push(result.outData); }
      }
      const result = await huks.finishSession(session.handle, { properties: properties, inData: new Uint8Array(0) });
      // API 24 can resolve finishSession with null when no final bytes remain.
      // Completion still authenticates the session; never skip the await above.
      if (result && result.outData) { pieces.push(result.outData); }
      let size = 0;
      for (const piece of pieces) { size += piece.length; }
      const output = new Uint8Array(size);
      let offset = 0;
      for (const piece of pieces) { output.set(piece, offset); offset += piece.length; }
      return output;
    } catch (error) {
      try { await huks.abortSession(session.handle, { properties: [] }); } catch (abortError) { /* Preserve the failure; never return partial plaintext. */ }
      throw new Error('本地记录加密校验失败，原记录已保留，请联系管理员');
    } finally {
      for (const piece of pieces) { piece.fill(0); }
    }
  }

  async seal(plaintext: string): Promise<string> {
    const data = this.encoder.encodeInto(plaintext);
    try {
      if (data.length > this.maxBytes) { throw new Error('本地加密记录超过容量，旧记录已保留'); }
      await this.key(true);
      let nonce: Uint8Array;
      if (canIUse('SystemCapability.Security.CryptoFramework')) {
        nonce = (await cryptoFramework.createRandom().generateRandom(12)).data;
      } else { throw new Error('当前设备不支持安全随机数，未保存敏感记录'); }
      if (nonce.length !== 12) { throw new Error('系统随机数不可用'); }
      const ciphertext = await this.crypt(data, nonce, true);
      const envelope: VaultEnvelope = { version: 1, nonce: this.base64.encodeToStringSync(nonce), ciphertext: this.base64.encodeToStringSync(ciphertext) };
      return JSON.stringify(envelope);
    } finally { data.fill(0); }
  }

  async open(serialized: string): Promise<string> {
    if (serialized.length > this.maxBytes * 2) { throw new Error('本地加密记录格式异常，原记录已保留'); }
    const envelope = JSON.parse(serialized) as VaultEnvelope;
    if (envelope.version !== 1 || typeof envelope.nonce !== 'string' || typeof envelope.ciphertext !== 'string') {
      throw new Error('本地加密记录版本不支持，原记录已保留');
    }
    const nonce = this.base64.decodeSync(envelope.nonce);
    const ciphertext = this.base64.decodeSync(envelope.ciphertext);
    if (nonce.length !== 12 || ciphertext.length < 16 || ciphertext.length > this.maxBytes + 16) { throw new Error('本地加密记录不完整，原记录已保留'); }
    await this.key(false);
    const plaintext = await this.crypt(ciphertext, nonce, false);
    try { return util.TextDecoder.create('utf-8', { fatal: true }).decodeToString(plaintext); }
    finally { plaintext.fill(0); }
  }
}
