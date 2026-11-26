import { Binding, HealthSample, sameBinding, WATCH_APP_VERSION } from './Protocol';

export interface SensorObservation {
  metric: string;
  value: number;
  receivedAt: number;
  sensorTimestamp: number;
}

// Pedometer readings are cumulative counters, not daily totals. The first
// observation anchors a session; only later deltas become interval samples.
export class NativeSampling {
  private owner: Binding | undefined = undefined;
  private previousSteps: SensorObservation | undefined = undefined;

  reset(): void { this.owner = undefined; this.previousSteps = undefined; }

  copy(): NativeSampling {
    const next = new NativeSampling();
    next.owner = this.owner;
    next.previousSteps = this.previousSteps;
    return next;
  }

  accept(binding: Binding, id: string, observation: SensorObservation): HealthSample[] {
    if (!this.owner || !sameBinding(this.owner, binding)) {
      this.previousSteps = undefined;
      this.owner = binding;
    }
    if (!Number.isFinite(observation.value) || !Number.isFinite(observation.receivedAt) ||
        !Number.isFinite(observation.sensorTimestamp)) {
      throw new Error('传感器返回无效数值，本次不保存');
    }
    const at = new Date(observation.receivedAt).toISOString();
    let start = at;
    let value = observation.value;
    let unit = 'count/min';
    let semantics = 'instant';
    if (observation.metric === 'heart_rate') {
      if (value <= 0) { return []; }
    } else if (observation.metric === 'steps') {
      if (!Number.isSafeInteger(value) || value < 0) { throw new Error('计步器计数无效'); }
      const previous = this.previousSteps;
      this.previousSteps = observation;
      if (!previous || value < previous.value || observation.sensorTimestamp <= previous.sensorTimestamp ||
          observation.receivedAt <= previous.receivedAt) { return []; }
      start = new Date(previous.receivedAt).toISOString();
      value -= previous.value;
      unit = 'count';
      semantics = 'interval';
    } else { throw new Error('未适配的传感器类型'); }
    return [{
      recordId: id, source: 'yihe-harmonyos', sourceDevice: binding.deviceNo,
      sourceVersion: WATCH_APP_VERSION, metric: observation.metric, unit: unit, value: value,
      start: start, end: at, semantics: semantics, version: 1,
      metadata: { simulated: 'false', sensor: observation.metric,
        sensorTimestamp: observation.sensorTimestamp.toString(), timestampBasis: 'app-received',
        counterBasis: observation.metric === 'steps' ? 'since-boot-delta' : 'not-applicable' }
    }];
  }
}
