import { abilityDelegatorRegistry, TestRunner } from '@kit.TestKit';
import { Hypium } from '@ohos/hypium';
import backendTests from '../test/Backend.test';

// Explicit separate suite: absence of the dedicated real backend must fail,
// never silently skip a native integration case.
export default class NativeBackendRunner implements TestRunner {
  onPrepare(): void {}
  onRun(): void {
    Hypium.hypiumTest(abilityDelegatorRegistry.getAbilityDelegator(), abilityDelegatorRegistry.getArguments(), backendTests);
  }
}
