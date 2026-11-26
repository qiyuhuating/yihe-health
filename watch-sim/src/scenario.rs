//! 场景时间轴。
//!
//! 旧实现只有 `normal` 与 `demo` 两档，`demo` 是四状态互相跳的固定循环，
//! 覆盖不到血压异常、离线补传、重绑历元这些后端真实分支。这里把场景做成
//! 带时长区间的脚本：每一步声明目标生理量与行为开关，循环播放。

use serde::Serialize;

use crate::physio::Targets;
use crate::rng::Pcg32;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Scenario {
    /// 日常活动：正常心率血压，持续走位。
    Normal,
    /// 静卧：活动接近零，心率血压偏低。
    Rest,
    /// 心动过速：心率越过 110，后端 rateState 应置 1。
    Tachycardia,
    /// 高血压：收缩压越过 140，后端 preState 应置 1。
    Hypertension,
    /// 摔倒：本拍带 alarm，且伴随短促心率抬升。
    Fall,
    /// 一键求救：走 `/device/sos`，请求带幂等 requestId。
    Sos,
    /// 信号丢失：不发送任何请求，样本进入离线队列。
    SignalLoss,
    /// 低电量：电量逼近 5%，用于验证前端低电提示。
    LowBattery,
    /// 夜间睡眠：睡眠判定为真，步数几乎不增长。
    NightSleep,
}

impl Scenario {
    pub fn label(self) -> &'static str {
        match self {
            Scenario::Normal => "日常活动",
            Scenario::Rest => "静卧休息",
            Scenario::Tachycardia => "心动过速",
            Scenario::Hypertension => "高血压",
            Scenario::Fall => "摔倒告警",
            Scenario::Sos => "一键求救",
            Scenario::SignalLoss => "信号丢失",
            Scenario::LowBattery => "低电量",
            Scenario::NightSleep => "夜间睡眠",
        }
    }

    pub fn code(self) -> &'static str {
        match self {
            Scenario::Normal => "normal",
            Scenario::Rest => "rest",
            Scenario::Tachycardia => "tachycardia",
            Scenario::Hypertension => "hypertension",
            Scenario::Fall => "fall",
            Scenario::Sos => "sos",
            Scenario::SignalLoss => "signal-loss",
            Scenario::LowBattery => "low-battery",
            Scenario::NightSleep => "night-sleep",
        }
    }

    pub fn parse(s: &str) -> Option<Scenario> {
        let key = s.trim().to_ascii_lowercase().replace('_', "-");
        Some(match key.as_str() {
            "normal" | "demo-normal" => Scenario::Normal,
            "rest" => Scenario::Rest,
            "tachycardia" | "high-hr" => Scenario::Tachycardia,
            "hypertension" | "high-bp" => Scenario::Hypertension,
            "fall" => Scenario::Fall,
            "sos" => Scenario::Sos,
            "signal-loss" | "offline" => Scenario::SignalLoss,
            "low-battery" => Scenario::LowBattery,
            "night-sleep" | "sleep" => Scenario::NightSleep,
            _ => return None,
        })
    }

    pub fn targets(self) -> Targets {
        match self {
            Scenario::Normal => Targets {
                hr: 76.0,
                bp: 120.0,
                spo2: 97.5,
                activity: 0.45,
            },
            Scenario::Rest => Targets {
                hr: 63.0,
                bp: 110.0,
                spo2: 97.0,
                activity: 0.02,
            },
            Scenario::Tachycardia => Targets {
                hr: 133.0,
                bp: 138.0,
                spo2: 96.0,
                activity: 0.25,
            },
            Scenario::Hypertension => Targets {
                hr: 88.0,
                bp: 168.0,
                spo2: 96.5,
                activity: 0.2,
            },
            Scenario::Fall => Targets {
                hr: 108.0,
                bp: 150.0,
                spo2: 94.0,
                activity: 0.0,
            },
            Scenario::Sos => Targets {
                hr: 112.0,
                bp: 148.0,
                spo2: 95.0,
                activity: 0.05,
            },
            Scenario::SignalLoss => Targets {
                hr: 79.0,
                bp: 122.0,
                spo2: 97.0,
                activity: 0.3,
            },
            Scenario::LowBattery => Targets {
                hr: 70.0,
                bp: 114.0,
                spo2: 97.0,
                activity: 0.05,
            },
            Scenario::NightSleep => Targets {
                hr: 55.0,
                bp: 104.0,
                spo2: 97.0,
                activity: 0.0,
            },
        }
    }

    /// 本拍是否携带摔倒告警。
    pub fn emits_alarm(self) -> bool {
        matches!(self, Scenario::Fall)
    }

    /// 本拍是否走独立的 SOS 接口而不是普通上报。
    pub fn asks_sos(self) -> bool {
        matches!(self, Scenario::Sos)
    }

    /// 本拍是否模拟断网（样本只入离线队列）。
    pub fn offline(self) -> bool {
        matches!(self, Scenario::SignalLoss)
    }

    pub fn forces_low_battery(self) -> bool {
        matches!(self, Scenario::LowBattery)
    }

    /// 是否强制把本地时钟拨到夜间。
    pub fn is_night(self) -> bool {
        matches!(self, Scenario::NightSleep)
    }
}

/// 脚本中的一步：场景 + 持续秒数区间。
///
/// 按秒而不是按拍定义：`--interval` 是可调的，按拍算会让 `--interval 1` 把
/// 一个 30 秒的场景压成 3 秒，心率还没升上来就切走了。
#[derive(Debug, Clone, Copy)]
struct Step {
    scenario: Scenario,
    min_secs: f64,
    max_secs: f64,
}

const fn step(scenario: Scenario, min_secs: f64, max_secs: f64) -> Step {
    Step {
        scenario,
        min_secs,
        max_secs,
    }
}

/// 演示脚本：一条能走完后端全部告警与状态分支的行政查房路线。
const DEMO_SCRIPT: &[Step] = &[
    step(Scenario::Normal, 24.0, 36.0),
    step(Scenario::Tachycardia, 18.0, 24.0),
    step(Scenario::Normal, 12.0, 18.0),
    step(Scenario::Hypertension, 15.0, 21.0),
    step(Scenario::Normal, 12.0, 18.0),
    step(Scenario::Fall, 3.0, 6.0),
    step(Scenario::Sos, 6.0, 9.0),
    step(Scenario::Normal, 12.0, 18.0),
    step(Scenario::Normal, 9.0, 15.0),
    step(Scenario::LowBattery, 9.0, 12.0),
    step(Scenario::Normal, 9.0, 15.0),
    step(Scenario::NightSleep, 12.0, 18.0),
    step(Scenario::Normal, 9.0, 15.0),
    step(Scenario::Normal, 18.0, 30.0),
];

pub struct Timeline {
    /// `Some` 表示固定场景（`--scenario normal` 等），`None` 表示跑演示脚本。
    fixed: Option<Scenario>,
    current: Scenario,
    remaining_secs: f64,
    cursor: usize,
}

impl Timeline {
    /// `mode` 为 `"demo"` 时循环脚本，其余按 `Scenario::parse` 固定。
    pub fn new(mode: &str, rng: &mut Pcg32) -> Self {
        match Scenario::parse(mode) {
            Some(s) => Timeline {
                fixed: Some(s),
                current: s,
                remaining_secs: f64::INFINITY,
                cursor: 0,
            },
            None => {
                let mut t = Timeline {
                    fixed: None,
                    current: Scenario::Normal,
                    remaining_secs: 0.0,
                    cursor: 0,
                };
                t.advance(rng);
                t
            }
        }
    }

    pub fn current(&self) -> Scenario {
        self.current
    }

    pub fn is_demo(&self) -> bool {
        self.fixed.is_none()
    }

    /// 推进 `dt` 秒，返回本拍生效的场景。
    pub fn tick(&mut self, dt: f64, rng: &mut Pcg32) -> Scenario {
        if self.fixed.is_some() {
            return self.current;
        }
        self.remaining_secs -= dt;
        while self.remaining_secs <= 0.0 {
            self.advance(rng);
        }
        self.current
    }

    /// 手动切场景（面板按钮 / TUI 快捷键），会切到固定模式。
    pub fn force(&mut self, scenario: Scenario) {
        self.fixed = Some(scenario);
        self.current = scenario;
        self.remaining_secs = f64::INFINITY;
    }

    pub fn resume_demo(&mut self, rng: &mut Pcg32) {
        self.fixed = None;
        self.cursor = 0;
        self.remaining_secs = 0.0;
        self.advance(rng);
    }

    fn advance(&mut self, rng: &mut Pcg32) {
        let s = DEMO_SCRIPT[self.cursor % DEMO_SCRIPT.len()];
        self.cursor += 1;
        self.current = s.scenario;
        self.remaining_secs = rng.range_f64(s.min_secs, s.max_secs).max(1.0);
    }
}
