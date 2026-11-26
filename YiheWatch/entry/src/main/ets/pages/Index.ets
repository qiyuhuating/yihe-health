import { common } from '@kit.AbilityKit';
import { DEBUG } from 'BuildProfile';
import { requireDebugSampling } from '../services/DebugSampling';
import { WatchApi } from '../services/WatchApi';
import { WatchStore } from '../services/WatchStore';
import { HealthSensors } from '../services/HealthSensors';
import { WatchSynchronizer, SyncResult } from '../services/WatchSynchronizer';
import { AutoMonitor, MonitorDecision } from '../services/AutoMonitor';
import { Binding, WatchConfig, DeviceRequestError,
  normalizeConfig, bindingFrom, enqueue, makeSamples, sameBinding, pendingForBinding } from '../model/Protocol';

@Entry
@Component
struct Index {
  @State private page: number = 0;
  @State private server: string = 'http://127.0.0.1:18796';
  @State private deviceNo: string = 'WATCH5_SIM_001';
  @State private secret: string = '';
  @State private bindCode: string = '';
  @State private person: string = '尚未绑定';
  @State private connected: boolean = false;
  @State private heart: number = 72;
  @State private steps: number = 1280;
  @State private nativeHeart: number = 0;
  @State private nativeCounter: number = -1;
  @State private queued: number = 0;
  @State private busy: boolean = false;
  @State private status: string = '本地预览 · 尚未连接后台';
  @State private lastUpload: string = '尚未上传';
  @State private sosPending: number = 0;
  @State private lastSOS: string = '';
  private api: WatchApi = new WatchApi();
  private store: WatchStore | undefined = undefined;
  private binding: Binding | undefined = undefined;
  private counterDay: string = new Date().toISOString().slice(0, 10);
  private sensors: HealthSensors = new HealthSensors();
  private synchronizer: WatchSynchronizer | undefined = undefined;
  private automatic: AutoMonitor = new AutoMonitor(async () => this.automaticCycle());
  @State private autoEnabled: boolean = false;
  @State private autoStatus: string = '自动监测已关闭';
  @StorageLink('yiheWatchForeground') @Watch('foregroundChanged') private foreground: boolean = false;
  private visible: boolean = true;
  private urgent: Array<() => Promise<void>> = [];
  private appearance: number = 0;
  private pendingRestore: number | undefined = undefined;

  aboutToAppear(): void {
    this.visible = this.foreground;
    this.automatic.setVisible(false);
    if (this.visible) { this.sensors.resume(); } else { this.sensors.suspend(); }
    const appearance = ++this.appearance;
    this.restoreLocalState(appearance);
  }
  private restoreLocalState(appearance: number): void {
    if (this.busy) { this.pendingRestore = appearance; return; }
    this.act(async () => {
      const context = this.getUIContext().getHostContext();
      if (!context) { throw new Error('无法取得应用上下文'); }
      const store = await WatchStore.open(context as common.UIAbilityContext);
      if (appearance !== this.appearance) { return; }
      this.store = store;
      this.synchronizer = new WatchSynchronizer(store, this.api, this.sensors, context as common.UIAbilityContext);
      if (this.visible) { this.synchronizer.resume(); } else { this.synchronizer.suspend(); }
      const config = this.store.config();
      this.server = config.server;
      this.deviceNo = config.deviceNo;
      this.secret = this.store.credential(config);
      const sosHistory = this.store.pendingSOS();
      this.sosPending = sosHistory.filter((item) => !item.incidentId).length;
      for (let i = sosHistory.length - 1; i >= 0; i--) {
        if (sosHistory[i].incidentId) {
          this.lastSOS = '最近已接收求助 #' + sosHistory[i].incidentId + '；再次发送可能创建新求助';
          break;
        }
      }
      const batch = this.store.batch();
      this.queued = batch ? batch.samples.length : 0;
      const simulator = this.store.simulator();
      if (simulator.day === this.counterDay) { this.steps = simulator.steps; }
      this.status = this.secret ? '已恢复安全凭据，连接后可继续同步' : '安全存储已就绪，请配置设备连接';
      this.autoEnabled = store.monitoringEnabled();
      this.autoStatus = this.autoEnabled ? '自动监测待前台运行' : '自动监测已关闭';
      this.automatic.setEnabled(this.autoEnabled);
      this.automatic.setVisible(this.visible);
    });
  }
  aboutToDisappear(): void {
    this.appearance++;
    this.pendingRestore = undefined;
    this.stopForeground();
  }
  onPageShow(): void {
    if (!this.foreground) { this.stopForeground(); return; }
    this.visible = true; this.sensors.resume(); this.synchronizer?.resume();
    this.automatic.setVisible(true);
  }
  private foregroundChanged(): void {
    if (this.foreground) { this.onPageShow(); } else { this.stopForeground(); }
  }
  private stopForeground(): void {
    this.visible = false; this.automatic.setVisible(false);
    if (this.synchronizer) { this.synchronizer.suspend(); } else { this.sensors.suspend(); }
    if (this.autoEnabled) { this.autoStatus = '后台暂停；回到前台自动恢复'; }
  }
  onPageHide(): void { this.stopForeground(); }
  private sync(): WatchSynchronizer {
    if (!this.synchronizer || !this.store) { throw new Error('安全存储未就绪，请重新打开应用'); }
    return this.synchronizer;
  }
  private changedConnection(): void {
    const wasEnabled = this.autoEnabled;
    this.autoEnabled = false; this.automatic.setEnabled(false); this.synchronizer?.reset();
    this.autoStatus = '连接已修改，重新认证后可开启自动监测'; this.useBinding(undefined);
    if (wasEnabled) {
      this.urgentAction(async () => {
        if (!this.store) { throw new Error('无法确认停止设置，请重启后检查'); }
        await this.store.saveMonitoringEnabled(false);
      });
    }
  }
  private config(): WatchConfig { return normalizeConfig(this.server, this.deviceNo); }
  private useBinding(binding: Binding | undefined): void {
    if (!binding || !this.binding || !sameBinding(this.binding, binding)) {
      this.nativeHeart = 0;
      this.nativeCounter = -1;
      this.person = binding ? '长者 #' + binding.residentId : '尚未绑定';
    }
    this.binding = binding;
    this.connected = binding !== undefined;
  }
  private credentials(): void {
    if (this.secret.length < 24 || this.secret.length > 128) {
      throw new Error('请输入后台生成的设备密钥（24–128 位），不是管理员密码');
    }
    if (!this.store) { throw new Error('本地存储未就绪，请重新打开应用'); }
  }
  private async liveBinding(): Promise<Binding> {
    this.credentials();
    this.connected = false;
    const config = this.config();
    const reply = await this.sync().authenticate(config, this.secret);
    if (reply.bound !== 1) { this.useBinding(undefined); }
    const binding = bindingFrom(config, reply);
    const batch = this.store?.batch(); this.queued = batch ? batch.samples.length : 0;
    this.useBinding(binding);
    this.person = reply.residentName || '长者 #' + binding.residentId;
    return binding;
  }
  private async act(action: () => Promise<void>): Promise<void> {
    if (this.busy) { return; }
    this.busy = true;
    try { await action(); }
    catch (error) {
      if ((error as DeviceRequestError).code === 'CANCELLED') { return; }
      const message = (error as Error).message;
      this.status = message || '请求失败，请检查连接；待上传记录已保留';
      this.connected = false;
    } finally {
      this.busy = false;
      const next = this.urgent.shift();
      if (next) { this.act(next); }
      else {
        const restore = this.pendingRestore; this.pendingRestore = undefined;
        if (restore !== undefined && restore === this.appearance) { this.restoreLocalState(restore); }
        else { this.automatic.setVisible(this.visible); }
      }
    }
  }
  private async connect(): Promise<void> {
    this.credentials();
    const config = this.config();
    await this.store?.saveConfig(config);
    await this.store?.saveMonitoringEnabled(this.autoEnabled);
    const reply = await this.sync().authenticate(config, this.secret);
    if (reply.bound !== 1) {
      this.useBinding(undefined);
      this.status = '设备认证成功，请输入设备绑定码';
      return;
    }
    const binding = bindingFrom(config, reply);
    this.useBinding(binding);
    this.person = reply.residentName || '长者 #' + binding.residentId;
    this.status = '已认证 · ' + this.person;
  }
  private async bind(): Promise<void> {
    this.credentials();
    if (!/^[A-Za-z0-9]{4,20}$/.test(this.bindCode.trim())) { throw new Error('请输入有效的设备绑定码'); }
    await this.api.bind(this.config(), this.secret, this.bindCode);
    this.bindCode = '';
    await this.connect();
  }
  private async sample(): Promise<void> {
    requireDebugSampling();
    const day = new Date().toISOString().slice(0, 10);
    if (day !== this.counterDay) { this.steps = 0; this.counterDay = day; }
    this.heart = 68 + Math.floor(Math.random() * 13);
    this.steps += 8 + Math.floor(Math.random() * 15);
    await this.store?.saveSimulator({ day: this.counterDay, steps: this.steps });
    if (!this.binding || !this.store) {
      this.status = '仅本地模拟，认证并绑定后才能保存上传记录';
      return;
    }
    const batch = enqueue(this.store.batch(), this.binding,
      makeSamples(this.binding.deviceNo, this.store.nextID(), new Date(), this.heart, this.steps));
    await this.store.saveBatch(batch);
    this.queued = batch.samples.length;
    this.status = '已保存 2 条模拟采样，点击“上传记录”提交';
  }
  private showSync(result: SyncResult): void {
    this.useBinding(result.binding); this.queued = result.queued;
    if (result.heart !== undefined) { this.nativeHeart = result.heart; }
    if (result.counter !== undefined) { this.nativeCounter = result.counter; }
    if (result.uploaded > 0) { this.lastUpload = new Date().toLocaleTimeString(); }
    this.status = (result.offline ? '离线缓存' : '同步完成') + ' · 新采样 ' + result.sampled +
      ' · 已上报 ' + result.uploaded + ' · 待上传 ' + result.queued + (result.note ? '；' + result.note : '');
  }
  private async sampleNative(): Promise<void> {
    this.credentials(); this.showSync(await this.sync().sample(this.config(), this.secret));
  }
  private async upload(): Promise<void> {
    if (!this.store?.batch()) { throw new Error('当前没有待上传记录'); }
    this.credentials();
    try { this.showSync(await this.sync().upload(this.config(), this.secret)); }
    catch (error) {
      if ((error as DeviceRequestError).code !== 'EPOCH_EXPIRED') {
        if (error instanceof Error) { throw error; }
        throw new Error('同步未完成，待上传记录已保留');
      }
      this.useBinding(undefined); await this.liveBinding();
      this.status = '绑定已重新同步，旧纪元采样已核查';
    }
  }
  private urgentAction(action: () => Promise<void>): void {
    this.automatic.setVisible(false); this.synchronizer?.reset();
    if (this.busy) { this.urgent.push(action); } else { this.act(action); }
  }
  private async startAutomatic(): Promise<void> {
    this.credentials();
    const config = this.config();
    if (!this.store || this.store.credential(config) !== this.secret || !this.store.verifiedBinding(config)) {
      throw new Error('请先保存地址、认证并绑定设备，再开启自动监测');
    }
    await this.store.saveMonitoringEnabled(true);
    this.autoEnabled = true; this.autoStatus = '自动监测 · 前台每分钟采样并同步';
    this.automatic.setEnabled(true); this.automatic.setVisible(this.visible);
  }
  private stopAutomatic(): void {
    this.autoEnabled = false; this.automatic.setEnabled(false);
    this.autoStatus = '自动监测已暂停，正在保存停止设置';
    this.urgentAction(async () => {
      if (!this.store) { throw new Error('无法确认停止设置，请重启后检查'); }
      await this.store.saveMonitoringEnabled(false);
      this.autoStatus = '自动监测已关闭'; this.status = '已停止，采样及求助记录仍保留';
    });
  }
  private async automaticCycle(): Promise<MonitorDecision> {
    if (this.busy || !this.store || !this.synchronizer) { return 'busy'; }
    if (!this.visible || !this.autoEnabled) { return 'ok'; }
    let decision: MonitorDecision = 'ok';
    await this.act(async () => {
      try {
        const result = await this.sync().cycle(this.config(), this.secret);
        this.showSync(result);
        this.autoStatus = result.offline ? '自动监测 · 离线缓存，联网后补传' : '自动监测 · 前台每分钟采样并同步';
      } catch (error) {
        const failure = error as DeviceRequestError;
        if (failure.code === 'CANCELLED') { return; }
        if (failure.code === 'AUTH_REQUIRED' || failure.code === 'NOT_BOUND' || failure.status === 401 ||
            failure.status === 403 || !this.store?.usable()) {
          decision = 'halt'; this.autoEnabled = false; this.automatic.setEnabled(false);
          this.autoStatus = '自动监测暂停，请检查认证、绑定和本地存储';
          if (this.store?.usable()) { await this.store.saveMonitoringEnabled(false); }
        } else { decision = 'retry'; this.autoStatus = '自动监测遇到异常，将退避重试；记录保留'; }
        if (error instanceof Error) { throw error; }
        throw new Error('同步未完成，待上传记录已保留');
      }
    });
    return decision;
  }
  private confirmSOS(): void {
    this.getUIContext().showAlertDialog({
      title: '确认发送 SOS 求助？',
      message: '当前绑定有未确认请求时将重试；否则会新建求助。旧绑定请求保留待核查。后台接收不代表通知送达或人员到场。',
      primaryButton: { value: '取消', action: () => {} },
      secondaryButton: { value: '确认发送', action: () => { this.urgentAction(async () => { await this.sendSOS(); }); } }
    });
  }
  private async sendSOS(): Promise<void> {
    if (!this.store) { throw new Error('本地存储未就绪'); }
    const config = this.config();
    const cached = this.store.credential(config) === this.secret ? this.store.verifiedBinding(config) : undefined;
    const live = this.visible ? await this.liveBinding() : cached;
    if (!live) { throw new Error('未能核实求助归属，请返回前台连接设备后重试'); }
    let items = this.store.pendingSOS();
    let pending = pendingForBinding(items, live);
    if (!pending) {
      pending = { binding: live, requestId: 'harmonyos-' + this.store.nextID() };
      items = items.concat([pending]);
      await this.store.saveSOS(items);
      this.sosPending = items.filter((item) => !item.incidentId).length;
    }
    if (!this.visible) { this.status = '求助已保存在本机，尚未提交后台；返回前台可重试原请求'; return; }
    const reply = await this.api.sos(this.config(), { deviceNo: live.deviceNo,
      secret: this.secret, requestId: pending.requestId, bindingVersion: pending.binding.version });
    if (!reply.incidentId) { throw new Error('后台确认缺少求助编号，原请求已保留'); }
    const acceptedRequestId = pending.requestId;
    const incidentId = reply.incidentId;
    items = items.map((item) => item.requestId === acceptedRequestId && sameBinding(item.binding, live)
      ? { binding: item.binding, requestId: item.requestId, incidentId: incidentId } : item);
    await this.store.saveSOS(items);
    this.sosPending = items.filter((item) => !item.incidentId).length;
    this.lastSOS = '最近已接收求助 #' + incidentId + '；再次发送可能创建新求助';
    this.status = '求助 #' + (reply.incidentId || '') + ' 已接收；通知状态：' + (reply.notificationState || '待确认') +
      (this.sosPending ? '；另有 ' + this.sosPending + ' 条旧求助待管理员核查' : '');
  }
  private confirmDiscard(): void {
    this.getUIContext().showAlertDialog({ title: '丢弃待上传记录？',
      message: '只清除本机采样，不删除后台数据，也不清除未确认的求助。',
      primaryButton: { value: '保留', action: () => {} },
      secondaryButton: { value: '丢弃', action: () => {
        this.act(async () => { await this.store?.saveBatch(undefined); this.synchronizer?.reset(); this.queued = 0; this.status = '本机记录已丢弃，计步基线已重置'; });
      } }
    });
  }
  @Builder
  private action(label: string, callback: () => void, color: string = '#24463C') {
    Button(label).fontSize(15).fontColor('#F3EDE0').backgroundColor(color)
      .width('100%').height(40).enabled(!this.busy).onClick(callback)
  }
  @Builder
  private caption(label: string) {
    Text(label).fontSize(12).fontColor('#93B1A7').width('100%')
  }
  @Builder
  private home() {
    Text(this.person === '尚未绑定' ? '今天，也安心' : this.person).fontSize(19).fontColor('#F3EDE0')
    Text('心率 · 传感器观测').fontSize(12).fontColor('#93B1A7')
    Row({ space: 4 }) {
      Text(this.nativeHeart > 0 ? this.nativeHeart.toString() : '—').id('watch-heart-rate').fontSize(52).fontWeight(FontWeight.Medium).fontColor('#E6CE9D')
      Text('次/分').fontSize(12).fontColor('#93B1A7').padding({ top: 22 })
    }.justifyContent(FlexAlign.Center)
    Text('计步器累计 ' + (this.nativeCounter >= 0 ? this.nativeCounter.toString() : '尚未读取')).fontSize(15).fontColor('#D8E4DC')
    this.action('授权并读取传感器', () => { this.act(async () => { await this.sampleNative(); }); })
    this.caption('前台读取；仅上传两次计步观测的增量，不冒充全天步数。')
    Button('SOS 求助').fontSize(15).fontColor('#F3EDE0').backgroundColor('#803F37')
      .width('100%').height(40).enabled(!this.busy || this.autoEnabled).onClick(() => { this.confirmSOS(); })
    if (this.sosPending) { this.caption('待核查求助：' + this.sosPending + ' 条') }
    if (this.lastSOS) { this.caption(this.lastSOS) }
    Button(this.autoEnabled ? '停止自动监测' : '开启自动监测').id('watch-auto-toggle').fontSize(15).fontColor('#F3EDE0')
      .backgroundColor('#24463C').width('100%').height(40).enabled(!this.busy || this.autoEnabled)
      .onClick(() => { if (this.autoEnabled) { this.stopAutomatic(); } else { this.act(async () => { await this.startAutomatic(); }); } })
    this.caption(this.autoStatus)
    this.caption('先授权传感器。前台每分钟自动运行；后台暂停，返回后恢复。断网只缓存已验证归属的采样，联网核实后补传。')
  }
  @Builder
  private records() {
    Text('数据同步').fontSize(21).fontColor('#F3EDE0')
    Text(this.queued.toString()).fontSize(42).fontColor('#E6CE9D')
    Text('条采样待上传').fontSize(13).fontColor('#93B1A7')
    this.caption('最近上传：' + this.lastUpload)
    this.action('上传记录', () => { this.act(async () => { await this.upload(); }); })
    this.action('发送心跳', () => { this.act(async () => {
      this.credentials();
      const reply = await this.api.heartbeat(this.config(), this.secret);
      this.useBinding(reply.bound === 1 ? bindingFrom(this.config(), reply) : undefined);
      this.status = '心跳成功 · 当前绑定已校验';
    }); })
    this.action('丢弃本机记录', () => { this.confirmDiscard(); }, '#373B35')
    if (DEBUG) {
      this.action('生成模拟采样（调试）', () => { this.act(async () => { await this.sample(); }); }, '#373B35')
    }
    this.caption('失败不清除队列；重试复用记录 ID，后台去重。绑定变化时禁止补传。')
  }
  @Builder
  private settings() {
    Text('连接与绑定').fontSize(21).fontColor('#F3EDE0')
    this.caption('后端地址')
    TextInput({ text: this.server, placeholder: 'http://127.0.0.1:18796' }).fontSize(12)
      .height(38).enabled(!this.busy).onChange((value: string) => { this.server = value; this.changedConnection(); })
    this.caption('设备号（后台登记）')
    TextInput({ text: this.deviceNo, placeholder: 'WATCH5_SIM_001' }).fontSize(13)
      .height(38).enabled(!this.busy).onChange((value: string) => { this.deviceNo = value; this.changedConnection(); })
    this.caption('设备密钥（认证成功后加密保存）')
    TextInput({ text: this.secret, placeholder: '后台生成的设备密钥' }).type(InputType.Password).fontSize(13)
      .height(38).enabled(!this.busy).onChange((value: string) => { this.secret = value; this.changedConnection(); })
    this.action('保存地址并认证', () => { this.act(async () => { await this.connect(); }); })
    this.action('忘记本机设备密钥', () => { this.act(async () => {
      if (!this.store) { throw new Error('安全存储未就绪，无法确认密钥移除'); }
      this.autoEnabled = false; this.automatic.setEnabled(false); this.sync().reset();
      await this.store.saveMonitoringEnabled(false); await this.store.forgetCredential();
      this.autoStatus = '自动监测已关闭'; this.secret = ''; this.useBinding(undefined);
      this.status = '本机设备密钥已移除，健康记录及求助历史仍保留';
    }); }, '#373B35')
    this.caption('设备绑定码（不是家属邀请码）')
    TextInput({ text: this.bindCode, placeholder: '输入设备绑定码' }).fontSize(15)
      .height(38).enabled(!this.busy).onChange((value: string) => { this.bindCode = value; })
    this.action('绑定长者', () => { this.act(async () => { await this.bind(); }); })
    this.caption('真机使用手表可访问的 HTTPS 地址；模拟器可用 HDC 本机转发。传感器授权与设备密钥认证分别完成。')
  }
  build() {
    Scroll() {
      Column({ space: 9 }) {
        Text('颐和守护').fontSize(15).fontColor('#D5BD89').fontWeight(FontWeight.Medium)
        Text('HarmonyOS · 需授权读取传感器').fontSize(10).fontColor('#D5BD89')
        Row({ space: 7 }) {
          ForEach(['首页', '记录', '设置'], (label: string, index: number) => {
            Button(label).id('watch-tab-' + index).fontSize(12).height(28).backgroundColor(this.page === index ? '#3D5F50' : '#172D26')
              .onClick(() => { this.page = index; })
          }, (label: string) => label)
        }.justifyContent(FlexAlign.Center)
        if (this.page === 0) { this.home(); }
        else if (this.page === 1) { this.records(); }
        else { this.settings(); }
        Text(this.busy ? '正在处理，请稍候…' : this.status).id('watch-status').fontSize(12)
          .fontColor(this.connected ? '#B9D6BC' : '#D8C599').textAlign(TextAlign.Center)
          .width('100%').padding({ top: 4 })
      }.width('76%').padding({ top: 28, bottom: 44 }).constraintSize({ minHeight: '100%' })
    }.id('watch-scroll').width('100%').height('100%').backgroundColor('#0C1917').scrollBar(BarState.Off)
  }
}
