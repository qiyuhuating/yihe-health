export type MonitorDecision = 'ok' | 'retry' | 'busy' | 'halt';

// Timers are foreground-only. Persisted user intent lives in WatchStore, not in
// a timer handle. There is one in-flight cycle even across hide/show or stop/start.
export class AutoMonitor {
  private enabled: boolean = false;
  private visible: boolean = false;
  private running: boolean = false;
  private generation: number = 0;
  private timer: number = -1;
  private failures: number = 0;
  private cycle: () => Promise<MonitorDecision>;
  private interval: number;

  constructor(cycle: () => Promise<MonitorDecision>, interval: number = 60000) {
    if (!Number.isFinite(interval) || interval < 1000) { throw new Error('自动采样间隔不能小于 1 秒'); }
    this.cycle = cycle;
    this.interval = interval;
  }
  setEnabled(enabled: boolean): void {
    if (enabled === this.enabled) { return; }
    this.enabled = enabled;
    this.changed();
  }
  setVisible(visible: boolean): void {
    if (visible === this.visible) { return; }
    this.visible = visible;
    this.changed();
  }
  private active(): boolean { return this.enabled && this.visible; }
  private changed(): void {
    this.generation++;
    if (this.timer !== -1) { clearTimeout(this.timer); this.timer = -1; }
    this.failures = 0;
    if (this.active() && !this.running) { this.schedule(0); }
  }
  private schedule(delay: number): void {
    const generation = this.generation;
    this.timer = setTimeout(() => { this.tick(generation); }, delay);
  }
  private async tick(generation: number): Promise<void> {
    this.timer = -1;
    if (!this.active() || this.running || generation !== this.generation) { return; }
    this.running = true;
    let decision: MonitorDecision = 'retry';
    try { decision = await this.cycle(); }
    catch (error) { decision = 'retry'; }
    finally {
      this.running = false;
      if (generation === this.generation && decision === 'halt') { this.enabled = false; }
      if (this.active()) {
        let delay = this.interval;
        if (generation !== this.generation) { delay = 0; }
        else if (decision === 'busy') { delay = 1000; }
        else if (decision === 'retry') {
          this.failures = Math.min(this.failures + 1, 5);
          delay = Math.min(this.interval * Math.pow(2, this.failures), 900000);
        } else { this.failures = 0; }
        this.schedule(delay);
      }
    }
  }
}
