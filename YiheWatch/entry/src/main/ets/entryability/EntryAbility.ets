import { UIAbility } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { hilog } from '@kit.PerformanceAnalysisKit';

export default class EntryAbility extends UIAbility {
  onForeground(): void { AppStorage.setOrCreate('yiheWatchForeground', true); }
  onBackground(): void { AppStorage.setOrCreate('yiheWatchForeground', false); }
  onDestroy(): void { AppStorage.setOrCreate('yiheWatchForeground', false); }

  onWindowStageCreate(stage: window.WindowStage): void {
    try {
      stage.loadContent('pages/Index', (error) => {
        if (error.code) {
          hilog.error(0x0001, 'YiheWatch', 'loadContent failed: %{public}d', error.code);
          return;
        }
        hilog.info(0x0001, 'YiheWatch', 'Index page loaded');
      });
    } catch (error) {
      hilog.error(0x0001, 'YiheWatch', 'Unable to initialize window');
    }
  }
}
