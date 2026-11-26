import { describe, expect, it, Level, Size, TestType } from '@ohos/hypium';
import { abilityDelegatorRegistry, Component, Driver, ON } from '@kit.TestKit';
import { common, UIAbility } from '@kit.AbilityKit';
import { preferences } from '@kit.ArkData';
import { http } from '@kit.NetworkKit';
import { WatchStore } from '../../../main/ets/services/WatchStore';
import { WatchConfig } from '../../../main/ets/model/Protocol';

interface UIFixture { ok: boolean; server: string; deviceNo: string; secret: string; }
const pause = (ms: number): Promise<void> => new Promise<void>((resolve) => { setTimeout(resolve, ms); });
const bundle = 'com.yihe.watch.quality';

async function phase(token: string, action: string): Promise<UIFixture> {
  const request = http.createHttp();
  try {
    const response = await request.request('http://127.0.0.1:18799/control', {
      method: http.RequestMethod.POST, usingCache: false,
      header: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token },
      extraData: JSON.stringify({ action: action }), expectDataType: http.HttpDataType.STRING,
      connectTimeout: 5000, readTimeout: 15000
    });
    if (response.responseCode !== 200) { throw new Error('UI host rejected phase: ' + action); }
    const fixture = JSON.parse(response.result as string) as UIFixture;
    if (!fixture.ok) { throw new Error('UI phase was not verified'); }
    return fixture;
  } finally { request.destroy(); }
}

async function entry(): Promise<UIAbility> {
  const delegator = abilityDelegatorRegistry.getAbilityDelegator();
  const monitor: abilityDelegatorRegistry.AbilityMonitor = { abilityName: 'EntryAbility', moduleName: 'entry' };
  await delegator.addAbilityMonitor(monitor);
  try {
    await delegator.startAbility({ bundleName: bundle, moduleName: 'entry', abilityName: 'EntryAbility' });
    const ability = await delegator.waitAbilityMonitor(monitor, 10000);
    if (ability.context.abilityInfo.name !== 'EntryAbility') { throw new Error('Production EntryAbility was not launched'); }
    return ability;
  } finally { await delegator.removeAbilityMonitor(monitor); }
}

async function component(driver: Driver, id: string): Promise<Component> {
  const found = await driver.waitForComponent(ON.id(id), 10000);
  if (!found) { throw new Error('UI component unavailable: ' + id); }
  return found;
}
async function clickText(driver: Driver, text: string): Promise<void> {
  const scroll = await component(driver, 'watch-scroll');
  const found = await scroll.scrollSearch(ON.text(text));
  if (!found) { throw new Error('UI action unavailable: ' + text); }
  await found.click();
}
async function tab(driver: Driver, index: number): Promise<void> {
  await (await component(driver, 'watch-scroll')).scrollToTop();
  await (await component(driver, 'watch-tab-' + index)).click();
}
async function status(driver: Driver, prefix: string): Promise<void> {
  await (await component(driver, 'watch-scroll')).scrollToBottom();
  const deadline = Date.now() + 20000;
  let observed = '';
  while (Date.now() < deadline) {
    observed = await (await component(driver, 'watch-status')).getText();
    if (observed.startsWith(prefix)) { return; }
    await pause(100);
  }
  throw new Error('UI did not reach expected status: ' + prefix + '; observed: ' + observed);
}
async function visibility(expected: boolean): Promise<void> {
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    if (AppStorage.get<boolean>('yiheWatchForeground') === expected) { return; }
    await pause(50);
  }
  throw new Error('EntryAbility lifecycle did not update foreground state');
}
async function disk(context: common.Context): Promise<WatchStore> {
  await preferences.removePreferencesFromCache(context, { name: 'yihe_watch_sim_v1' });
  return WatchStore.open(context);
}
async function capture(driver: Driver, context: common.Context, token: string, label: string): Promise<void> {
  const file = context.filesDir + '/yihe-quality-ui-' + label + '-' + Date.now() + '.png';
  if (!await driver.screenCap(file)) { throw new Error('Cannot capture actual quality UI'); }
  const request = http.createHttp();
  try {
    const response = await request.request('http://127.0.0.1:18799/capture', {
      method: http.RequestMethod.POST, usingCache: false,
      header: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token },
      extraData: JSON.stringify({ file: file, label: label }), expectDataType: http.HttpDataType.STRING,
      connectTimeout: 5000, readTimeout: 15000
    });
    if (response.responseCode !== 200) { throw new Error('Quality UI screenshot was not preserved'); }
  } finally { request.destroy(); }
}
async function closeEntry(ability: UIAbility): Promise<void> {
  const delegator = abilityDelegatorRegistry.getAbilityDelegator();
  let destroyed = false;
  const monitor: abilityDelegatorRegistry.AbilityMonitor = {
    abilityName: 'EntryAbility', moduleName: 'entry', onAbilityDestroy: () => { destroyed = true; }
  };
  await delegator.addAbilityMonitor(monitor);
  try {
    await ability.context.terminateSelf();
    const limit = Date.now() + 10000;
    while (!destroyed && Date.now() < limit) { await pause(50); }
    if (!destroyed) { throw new Error('Fixture EntryAbility did not close'); }
  } finally { await delegator.removeAbilityMonitor(monitor); }
}

export default function uiTests(): void {
  const args = abilityDelegatorRegistry.getArguments();
  const restarting = args.parameters['-s scenario'] === 'restart';
  describe('YiheNativeUI', () => {
    it(restarting ? 'native_ui_restart' : 'native_ui_lifecycle', TestType.RELIABILITY | Size.LARGETEST | Level.LEVEL0, async () => {
      // Refuse the production bundle before opening its default Preferences.
      const token = args.parameters['-s fixtureToken'];
      if (args.bundleName !== bundle || typeof token !== 'string' || !/^[a-f0-9]{64}$/.test(token)) {
        throw new Error('Isolated quality bundle and dedicated fixture token required');
      }
      const fixture = await phase(token, restarting ? 'restore-fixture' : 'fixture');
      if (fixture.server !== 'http://127.0.0.1:18800' || !fixture.deviceNo || fixture.secret.length < 24) {
        throw new Error('Unsafe native UI fixture');
      }
      const config: WatchConfig = { server: fixture.server, deviceNo: fixture.deviceNo };
      const driver = Driver.create();
      if (!driver) { throw new Error('Native UI test driver initialization failed'); }
      let ability = await entry();
      await visibility(true);
      await component(driver, 'watch-scroll');
      if (!restarting) {
        await status(driver, '安全存储已就绪');
        // API 24's module context and UIAbility context can resolve different
        // Preferences directories. Seed the actual EntryAbility's context, then
        // close it before the next page restores those durable UI inputs.
        const saved = await WatchStore.open(ability.context);
        await saved.saveConfig(config);
        await saved.saveCredential(config, fixture.secret);
        await saved.saveMonitoringEnabled(false);
        await closeEntry(ability);
        ability = await entry();
        await visibility(true);
        await status(driver, '已恢复安全凭据');
      } else {
        const saved = await WatchStore.open(ability.context);
        if (saved.config().deviceNo !== config.deviceNo || saved.credential(config) !== fixture.secret || !saved.monitoringEnabled()) {
          throw new Error('Process restart did not retain encrypted configuration and policy');
        }
      }
      const context = ability.context;
      const delegator = abilityDelegatorRegistry.getAbilityDelegator();
      if (restarting) {
        await status(driver, '同步完成');
        await phase(token, 'restart-running');
        await clickText(driver, '停止自动监测');
        await status(driver, '已停止');
        if ((await disk(context)).monitoringEnabled()) { throw new Error('UI stop was not durable after restart'); }
        await phase(token, 'restart-stopped');
        await pause(61000);
        await phase(token, 'restart-quiet');
        await tab(driver, 1);
        // On a round display the records label can be below the viewport.
        // Locate and reveal the real label before asserting its visibility;
        // switching a tab alone neither waits for layout nor scrolls to it.
        const label = await (await component(driver, 'watch-scroll')).scrollSearch(ON.text('条采样待上传'));
        if (!label) { throw new Error('Records label unavailable after process restart'); }
        expect(await label.getText()).assertEqual('条采样待上传');
        await driver.assertComponentExist(ON.text('条采样待上传'));
        await capture(driver, context, token, 'records');
        return;
      }

      await tab(driver, 2);
      await clickText(driver, '保存地址并认证');
      await status(driver, '已认证');
      await phase(token, 'authenticated');
      await tab(driver, 0);
      await clickText(driver, '开启自动监测');
      await status(driver, '同步完成');
      await phase(token, 'started');
      await (await component(driver, 'watch-scroll')).scrollToTop();
      await capture(driver, context, token, 'home');
      // Production's actual 60-second scheduler is used, without a test clock
      // or a substituted sensor implementation. No permission prompt is acted on.
      await pause(61000);
      await phase(token, 'periodic');
      await delegator.doAbilityBackground(ability);
      await visibility(false);
      await phase(token, 'background');
      await pause(61000);
      await phase(token, 'background-quiet');
      await delegator.doAbilityForeground(ability);
      await visibility(true);
      await status(driver, '同步完成');
      await phase(token, 'foreground');
      await clickText(driver, '停止自动监测');
      await status(driver, '已停止');
      if ((await disk(context)).monitoringEnabled()) { throw new Error('UI stop did not persist'); }
      await phase(token, 'stopped');
      await pause(61000);
      await phase(token, 'stop-quiet');

      // Cancel an actual settings request through a real onBackground event.
      await tab(driver, 2);
      await phase(token, 'hold-login');
      await clickText(driver, '保存地址并认证');
      await phase(token, 'request-held');
      await delegator.doAbilityBackground(ability);
      await visibility(false);
      await phase(token, 'cancelled');
      await delegator.doAbilityForeground(ability);
      await visibility(true);
      await clickText(driver, '保存地址并认证');
      await status(driver, '已认证');
      await phase(token, 'recovered');
      await tab(driver, 0);
      await clickText(driver, '开启自动监测');
      await status(driver, '同步完成');
      if (!(await disk(context)).monitoringEnabled()) { throw new Error('Restart policy was not persisted'); }
      await phase(token, 'restart-ready');
      expect(AppStorage.get<boolean>('yiheWatchForeground')).assertTrue();
    });
  });
}
