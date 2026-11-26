import { preferences } from '@kit.ArkData';
import { common } from '@kit.AbilityKit';
import { WatchVault } from './WatchVault';

type WatchValue = string | number;

// One encrypted snapshot preserves IDs, credentials and queue state together.
// A write with an uncertain outcome blocks this instance until a real reload.
export class EncryptedPreferences {
  private prefs: preferences.Preferences;
  private vault: WatchVault = new WatchVault();
  private values: Record<string, WatchValue> = {};
  private ready: boolean = false;
  private failed: boolean = false;
  private writing: boolean = false;
  private readonly encryptedKey: string = 'encryptedStateV1';
  private readonly legacyStrings: string[] = ['server', 'deviceNo', 'batch', 'sos', 'counterDay'];
  private readonly legacyNumbers: string[] = ['counterSteps', 'sequence'];

  constructor(context: common.Context, preferencesName: string = 'yihe_watch_sim_v1') {
    if (!/^[A-Za-z0-9_-]{1,64}$/.test(preferencesName)) { throw new Error('本地存储名称无效'); }
    this.prefs = preferences.getPreferencesSync(context, { name: preferencesName });
  }

  async open(): Promise<void> {
    if (this.ready || this.failed) { throw new Error('本地安全存储需要重新打开'); }
    try {
      const encrypted = this.prefs.getSync(this.encryptedKey, '') as string;
      if (encrypted) {
        // Never fall back to legacy plaintext if authentication or key access fails.
        this.values = JSON.parse(await this.vault.open(encrypted)) as Record<string, WatchValue>;
        if (!this.values || typeof this.values !== 'object' || Array.isArray(this.values)) { throw new Error('Invalid encrypted state'); }
      } else {
        for (const key of this.legacyStrings) {
          if (this.prefs.hasSync(key)) { this.values[key] = this.prefs.getSync(key, '') as string; }
        }
        for (const key of this.legacyNumbers) {
          if (this.prefs.hasSync(key)) { this.values[key] = this.prefs.getSync(key, 0) as number; }
        }
        // Commit the encrypted copy before removing any old plaintext record.
        this.prefs.putSync(this.encryptedKey, await this.vault.seal(JSON.stringify(this.values)));
        await this.prefs.flush();
      }
      for (const key of this.legacyStrings.concat(this.legacyNumbers)) { this.prefs.deleteSync(key); }
      await this.prefs.flush();
      this.ready = true;
    } catch (error) {
      this.failed = true;
      throw new Error('安全存储未能打开，原记录已保留；请重启或联系管理员，不要清除应用数据');
    }
  }

  private check(): void {
    if (!this.ready || this.failed) { throw new Error('本地保存状态未确认，请重启恢复记录，勿重复发送或采样'); }
  }
  usable(): boolean { return this.ready && !this.failed; }

  getSync(key: string, fallback: WatchValue): WatchValue {
    this.check();
    const value = this.values[key];
    return value === undefined ? fallback : value;
  }

  putSync(key: string, value: WatchValue): void {
    this.check();
    if (this.writing) { throw new Error('本地记录正在保存，请稍后重试'); }
    this.values[key] = value;
  }

  async flush(): Promise<void> {
    this.check();
    if (this.writing) { throw new Error('本地记录正在保存，请稍后重试'); }
    this.writing = true;
    try {
      const snapshot = JSON.stringify(this.values);
      this.prefs.putSync(this.encryptedKey, await this.vault.seal(snapshot));
      await this.prefs.flush();
    } catch (error) {
      this.failed = true;
      throw new Error('本地保存状态未确认，请重启恢复记录，勿重复发送或采样');
    } finally { this.writing = false; }
  }
}
