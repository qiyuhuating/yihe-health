import { abilityDelegatorRegistry, TestRunner } from '@kit.TestKit';
import { Hypium } from '@ohos/hypium';

export default class NativeUIRunner implements TestRunner {
  onPrepare(): void {}
  onRun(): void {
    this.loadTests().catch(async () => {
      await abilityDelegatorRegistry.getAbilityDelegator().finishTest('Native UI suite initialization failed', 1);
    });
  }
  private async loadTests(): Promise<void> {
    // UITest initializes its IPC connection at module load. Wait until aa has
    // registered the delegator instead of importing it with the runner class.
    const suite = await import('../test/UI.test');
    Hypium.hypiumTest(abilityDelegatorRegistry.getAbilityDelegator(), abilityDelegatorRegistry.getArguments(), suite.default);
  }
}
