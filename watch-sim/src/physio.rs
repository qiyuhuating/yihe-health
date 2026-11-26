//! 生理模型。
//!
//! 旧实现每次 `randomInt(62, 92)` 独立取心率，相邻两次可能从 62 跳到 91，
//! 曲线是白噪声，放在真实监护界面上像故障。这里改用带回归项的一阶随机微分
//! （离散化后的 Ornstein–Uhlenbeck）：数值围绕目标缓慢漂移，短期有惯性、
//! 长期由场景牵引，符合穿戴设备的观感。

use crate::rng::Pcg32;

/// 场景对生理量的牵引目标。
#[derive(Debug, Clone, Copy)]
pub struct Targets {
    pub hr: f64,
    pub bp: f64,
    pub spo2: f64,
    /// 0.0 静卧 … 1.0 剧烈活动，驱动步数累计与心率回弹速度。
    pub activity: f64,
}

impl Default for Targets {
    fn default() -> Self {
        Targets {
            hr: 74.0,
            bp: 118.0,
            spo2: 97.0,
            activity: 0.35,
        }
    }
}

/// 一次采样后的可上报快照。
#[derive(Debug, Clone, Copy)]
pub struct Vitals {
    pub hr: i64,
    pub bp: i64,
    pub spo2: i64,
    pub battery: i64,
    pub steps: i64,
    pub sleep_hours: i64,
    /// true 表示当前处于睡眠判定。
    pub asleep: bool,
    pub lng: f64,
    pub lat: f64,
}

pub struct Physiology {
    rng: Pcg32,
    hr: f64,
    bp: f64,
    spo2: f64,
    battery: f64,
    steps: i64,
    /// 步数的小数累加器：`--interval 1` 时每拍不足一步，直接取整会让步数永远停住。
    steps_frac: f64,
    activity: f64,
    lng: f64,
    lat: f64,
    /// 行走时的朝向（度），让轨迹像一条路而不是随机游走。
    heading: f64,
    hour_of_day: f64,
}

impl Physiology {
    pub fn new(seed: u64, start_lng: f64, start_lat: f64, hour_of_day: f64) -> Self {
        let mut rng = Pcg32::new(seed);
        Physiology {
            hr: 72.0 + rng.range_f64(-4.0, 4.0),
            bp: 116.0 + rng.range_f64(-5.0, 5.0),
            spo2: 97.0 + rng.range_f64(-1.0, 1.0),
            battery: rng.range_f64(62.0, 96.0),
            steps: rng.range_i64(800, 2400),
            steps_frac: 0.0,
            activity: 0.3,
            lng: start_lng,
            lat: start_lat,
            heading: rng.range_f64(0.0, 360.0),
            hour_of_day,
            rng,
        }
    }

    /// 把时间往前推 `dt` 秒，返回本次采样。
    ///
    /// `theta` 是回归速度（越大越快贴回目标），`sigma` 是扩散强度。
    pub fn tick(&mut self, dt: f64, target: Targets) -> Vitals {
        let dt = dt.clamp(0.05, 3600.0);
        self.hour_of_day = (self.hour_of_day + dt / 3600.0) % 24.0;

        // 昼夜节律：凌晨 3 点最低，下午 4 点最高，振幅约 6 bpm。
        let circadian = -6.0 * ((self.hour_of_day - 16.0) / 24.0 * std::f64::consts::TAU).cos();

        let hr_target = target.hr + circadian;
        // 心率贴回目标以秒为单位（约 5 秒半衰），血压慢一个量级。
        self.hr = ou_step(
            &mut self.rng,
            self.hr,
            hr_target,
            dt,
            0.12,
            1.0,
            30.0,
            210.0,
        );
        self.bp = ou_step(
            &mut self.rng,
            self.bp,
            target.bp,
            dt,
            0.04,
            0.8,
            60.0,
            240.0,
        );
        self.spo2 = ou_step(
            &mut self.rng,
            self.spo2,
            target.spo2,
            dt,
            0.12,
            0.3,
            82.0,
            100.0,
        );

        // 活动强度本身也平滑过渡，避免场景切换时步频突变。
        self.activity += (target.activity - self.activity) * (1.0 - (-2.0 * dt).exp());

        // 步数：按活动强度折算步频。线性映射——静卧约 6 步/分，日常步行约
        // 90 步/分，满强度约 200 步/分，和真实计步器同一个量级。
        let cadence = 2.0 + 195.0 * self.activity;
        self.steps_frac += cadence * dt / 60.0;
        if self.steps_frac >= 1.0 {
            let whole = self.steps_frac.floor();
            self.steps += whole as i64;
            self.steps_frac -= whole;
        }

        // 电量：基础放电 + 定位/上报开销，屏幕常亮再扣一点。
        let drain_per_hour = 1.6 + 2.4 * self.activity;
        self.battery = (self.battery - drain_per_hour * dt / 3600.0).clamp(0.0, 100.0);

        self.move_along(dt);

        let sleep_hours = self.sleep_hours();
        Vitals {
            hr: self.hr.round() as i64,
            bp: self.bp.round() as i64,
            spo2: self.spo2.round() as i64,
            battery: self.battery.round() as i64,
            steps: self.steps,
            sleep_hours,
            asleep: self.hour_of_day < 6.0 || self.hour_of_day >= 22.0,
            lng: round6(self.lng),
            lat: round6(self.lat),
        }
    }

    /// 按朝向推进位置：约 1.3 m/s 有效位移，叠加小幅横向抖动。
    fn move_along(&mut self, dt: f64) {
        let speed_mps = 1.35 * self.activity;
        let meters = speed_mps * dt;
        self.heading += self.rng.range_f64(-6.0, 6.0);
        if self.heading > 360.0 {
            self.heading -= 360.0;
        }
        let rad = self.heading.to_radians();
        // 1 度纬度≈111_320 m；经度按纬度收缩。
        let dlat = meters * rad.cos() / 111_320.0;
        let dlon = meters * rad.sin() / (111_320.0 * self.lat.to_radians().cos().abs().max(0.1));
        self.lat += dlat + self.rng.range_f64(-2e-6, 2e-6);
        self.lng += dlon + self.rng.range_f64(-2e-6, 2e-6);
    }

    fn sleep_hours(&self) -> i64 {
        let h = self.hour_of_day;
        if h >= 22.0 {
            ((h - 22.0) + 2.0).round() as i64
        } else if h < 6.0 {
            (8.0 - (6.0 - h)).round().max(0.0) as i64
        } else {
            7
        }
    }

    pub fn set_battery(&mut self, pct: f64) {
        self.battery = pct.clamp(0.0, 100.0);
    }

    pub fn battery(&self) -> i64 {
        self.battery.round() as i64
    }

    pub fn set_hour(&mut self, hour: f64) {
        self.hour_of_day = hour;
    }

    pub fn reset_steps(&mut self) {
        self.steps = 0;
    }

    pub fn steps(&self) -> i64 {
        self.steps
    }
}

fn ou_step(
    rng: &mut Pcg32,
    current: f64,
    target: f64,
    dt: f64,
    theta: f64,
    sigma: f64,
    lo: f64,
    hi: f64,
) -> f64 {
    // 回归系数必须钳住：`theta * dt` 一旦超过 1 就会过冲，心率会一步跳到底。
    let k = (theta * dt).clamp(0.0, 0.6);
    let drift = k * (target - current);
    let diffusion = sigma * dt.sqrt() * rng.gauss();
    (current + drift + diffusion).clamp(lo, hi)
}

fn round6(v: f64) -> f64 {
    (v * 1_000_000.0).round() / 1_000_000.0
}
