import { sensor } from '@kit.SensorServiceKit';
import { abilityAccessCtrl, common, Permissions } from '@kit.AbilityKit';
import { SensorObservation } from '../model/NativeSampling';

export interface SensorRead {
  observations: SensorObservation[];
  status: string;
}

// Foreground manual or scheduled reads. Permission denial or missing hardware never
// falls back to generated measurements. Each one-shot listener has a timeout.
export class HealthSensors {
  private generation: number = 0;
  private active: boolean = true;
  private cancelHeart: (() => void) | undefined = undefined;
  private cancelSteps: (() => void) | undefined = undefined;

  cancel(): void {
    this.generation++;
    this.cancelHeart?.();
    this.cancelSteps?.();
  }

  session(): number {
    if (!this.active) { throw new Error('读取已取消'); }
    return this.generation;
  }

  suspend(): void { this.active = false; this.cancel(); }
  resume(): void { this.active = true; }

  async read(context: common.UIAbilityContext, session: number = this.generation, requestPermission: boolean = true): Promise<SensorRead> {
    if (!this.active || session !== this.generation) { throw new Error('读取已取消'); }
    this.cancel();
    const generation = this.generation;
    const list = await sensor.getSensorList();
    const heart = list.some((item: sensor.Sensor) => item.sensorId === sensor.SensorId.HEART_RATE);
    const steps = list.some((item: sensor.Sensor) => item.sensorId === sensor.SensorId.PEDOMETER);
    const permissions: Permissions[] = [];
    if (heart) { permissions.push('ohos.permission.READ_HEALTH_DATA'); }
    if (steps) { permissions.push('ohos.permission.ACTIVITY_MOTION'); }
    if (generation !== this.generation) { throw new Error('读取已取消'); }
    if (!permissions.length) { return { observations: [], status: '当前设备未提供心率或计步传感器' }; }
    const manager = abilityAccessCtrl.createAtManager();
    const authResults: number[] = [];
    if (requestPermission) {
      const grant = await manager.requestPermissionsFromUser(context, permissions);
      for (const result of grant.authResults) { authResults.push(result); }
    } else {
      // Timer callbacks never open permission dialogs. The user authorizes once
      // through the foreground action; revoked grants stop sensor access.
      for (const permission of permissions) {
        authResults.push(await manager.checkAccessToken(context.applicationInfo.accessTokenId, permission));
      }
    }
    if (generation !== this.generation) { throw new Error('读取已取消'); }
    const observations: SensorObservation[] = [];
    const notes: string[] = [];
    if (heart && authResults[0] === 0) {
      try { observations.push(await this.readHeart()); }
      catch (error) { notes.push('心率未取得有效读数，请检查佩戴或稍后重试'); }
    } else { notes.push(heart ? '心率权限未授权' : '无心率传感器'); }
    if (generation !== this.generation) { throw new Error('读取已取消'); }
    const stepIndex = heart ? 1 : 0;
    if (steps && authResults[stepIndex] === 0) {
      try { observations.push(await this.readSteps()); }
      catch (error) { notes.push('计步读数暂不可用'); }
    } else { notes.push(steps ? '活动权限未授权' : '无计步传感器'); }
    if (generation !== this.generation) { throw new Error('读取已取消'); }
    return { observations: observations, status: notes.join('；') };
  }

  private readHeart(): Promise<SensorObservation> {
    return new Promise<SensorObservation>((resolve, reject) => {
      let timer: number = -1;
      const finish = (): void => {
        clearTimeout(timer);
        this.cancelHeart = undefined;
        try { sensor.off(sensor.SensorId.HEART_RATE, callback); }
        catch (error) { /* The permission may have been revoked during a read. */ }
      };
      const callback = (data: sensor.HeartRateResponse): void => {
        const receivedAt = Date.now();
        finish();
        if (!Number.isFinite(data.heartRate) || data.heartRate <= 0) { reject(new Error('尚无有效心率')); return; }
        resolve({ metric: 'heart_rate', value: data.heartRate, receivedAt: receivedAt, sensorTimestamp: data.timestamp });
      };
      this.cancelHeart = (): void => { finish(); reject(new Error('心率读取取消')); };
      timer = setTimeout(() => { finish(); reject(new Error('心率读取超时')); }, 10000);
      try { sensor.once(sensor.SensorId.HEART_RATE, callback); }
      catch (error) { clearTimeout(timer); this.cancelHeart = undefined; reject(error); }
    });
  }

  private readSteps(): Promise<SensorObservation> {
    return new Promise<SensorObservation>((resolve, reject) => {
      let timer: number = -1;
      const finish = (): void => {
        clearTimeout(timer);
        this.cancelSteps = undefined;
        try { sensor.off(sensor.SensorId.PEDOMETER, callback); }
        catch (error) { /* The permission may have been revoked during a read. */ }
      };
      const callback = (data: sensor.PedometerResponse): void => {
        const receivedAt = Date.now();
        finish();
        resolve({ metric: 'steps', value: data.steps, receivedAt: receivedAt, sensorTimestamp: data.timestamp });
      };
      this.cancelSteps = (): void => { finish(); reject(new Error('计步读取取消')); };
      timer = setTimeout(() => { finish(); reject(new Error('计步读取超时')); }, 10000);
      try { sensor.once(sensor.SensorId.PEDOMETER, callback); }
      catch (error) { clearTimeout(timer); this.cancelSteps = undefined; reject(error); }
    });
  }
}
