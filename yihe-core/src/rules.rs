//! Pure rule candidates for prevalidated committed observations.
//! Timezone resolution, evidence, authorization and incident persistence
//! remain with the caller. No medical diagnosis or side effects.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Kind {
    HeartRate,
    BaselineHeartRate,
    BloodPressure,
    BaselineBloodPressure,
    Battery,
    NoReport,
    NightActivity,
}
impl Kind {
    pub fn name(self) -> &'static str {
        match self {
            Self::HeartRate => "heart_rate",
            Self::BaselineHeartRate => "baseline_heart_rate",
            Self::BloodPressure => "blood_pressure",
            Self::BaselineBloodPressure => "baseline_blood_pressure",
            Self::Battery => "battery",
            Self::NoReport => "no_report",
            Self::NightActivity => "night_activity",
        }
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Severity {
    Normal,
    High,
    Critical,
}
impl Severity {
    pub fn name(self) -> &'static str {
        match self {
            Self::Normal => "normal",
            Self::High => "high",
            Self::Critical => "critical",
        }
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Candidate {
    pub kind: Kind,
    pub severity: Severity,
    pub breach: bool,
    pub required: u8,
}
#[derive(Clone, Copy)]
pub struct Policy {
    pub enabled: bool,
    pub hr_low: f64,
    pub hr_high: f64,
    pub bp_low: f64,
    pub bp_high: f64,
    pub battery_low: f64,
    pub offline_seconds: i64,
    pub consecutive: u8,
    pub night_steps: f64,
    pub baseline_minimum: usize,
    pub baseline_percent: f64,
    pub baseline_sigma: f64,
}
impl Default for Policy {
    fn default() -> Self {
        Self {
            enabled: true,
            hr_low: 55.,
            hr_high: 110.,
            bp_low: 90.,
            bp_high: 140.,
            battery_low: 20.,
            offline_seconds: 300,
            consecutive: 3,
            night_steps: 200.,
            baseline_minimum: 20,
            baseline_percent: 25.,
            baseline_sigma: 3.,
        }
    }
}
#[derive(Clone, Copy)]
pub struct Value {
    pub value: f64,
    pub at_ns: i64,
}
#[derive(Clone, Copy)]
pub struct Baseline {
    pub count: usize,
    pub mean: f64,
    pub deviation: f64,
}
#[derive(Clone, Copy, Default)]
pub struct Input {
    pub now_ns: i64,
    pub night: bool,
    pub activity_night: bool,
    pub last_report_ns: Option<i64>,
    pub heart: Option<Value>,
    pub blood: Option<Value>,
    pub battery: Option<Value>,
    pub activity: Option<Value>,
    pub heart_baseline: Option<Baseline>,
    pub blood_baseline: Option<Baseline>,
}
pub struct Candidates {
    items: [Candidate; 7],
    len: usize,
}
impl Candidates {
    fn new() -> Self {
        Self {
            items: [Candidate {
                kind: Kind::HeartRate,
                severity: Severity::Normal,
                breach: false,
                required: 0,
            }; 7],
            len: 0,
        }
    }
    fn push(&mut self, c: Candidate) {
        self.items[self.len] = c;
        self.len += 1;
    }
    pub fn as_slice(&self) -> &[Candidate] {
        &self.items[..self.len]
    }
}
const SECOND: i128 = 1_000_000_000;
/// Input and policy are validated upstream. Returns only kind/severity/breach/
/// required, not the production observation key or encrypted evidence.
pub fn evaluate(p: Policy, input: Input) -> Candidates {
    let mut out = Candidates::new();
    if !p.enabled {
        return out;
    }
    let severity = if input.night {
        Severity::Critical
    } else {
        Severity::High
    };
    for (kind, base_kind, value, base, low, high, floor) in [
        (
            Kind::HeartRate,
            Kind::BaselineHeartRate,
            input.heart,
            input.heart_baseline,
            p.hr_low,
            p.hr_high,
            8.,
        ),
        (
            Kind::BloodPressure,
            Kind::BaselineBloodPressure,
            input.blood,
            input.blood_baseline,
            p.bp_low,
            p.bp_high,
            12.,
        ),
    ] {
        let Some(v) = value else { continue };
        let age = i128::from(input.now_ns) - i128::from(v.at_ns);
        if !(-60 * SECOND..=300 * SECOND).contains(&age) {
            continue;
        }
        out.push(Candidate {
            kind,
            severity,
            breach: v.value < low || v.value > high,
            required: p.consecutive,
        });
        if let Some(b) = base
            && b.count >= p.baseline_minimum
            && b.mean > 0.
        {
            let limit = (b.mean.abs() * p.baseline_percent / 100.)
                .max(b.deviation * p.baseline_sigma)
                .max(floor);
            out.push(Candidate {
                kind: base_kind,
                severity,
                breach: (v.value - b.mean).abs() > limit,
                required: p.consecutive,
            });
        }
    }
    if let Some(v) = input.battery {
        out.push(Candidate {
            kind: Kind::Battery,
            severity: Severity::Normal,
            breach: v.value < p.battery_low,
            required: 1,
        });
    }
    if let Some(last) = input.last_report_ns {
        out.push(Candidate {
            kind: Kind::NoReport,
            severity,
            breach: i128::from(input.now_ns) - i128::from(last)
                > i128::from(p.offline_seconds) * SECOND,
            required: 1,
        });
    }
    if let Some(v) = input.activity
        && input.night
        && input.activity_night
    {
        out.push(Candidate {
            kind: Kind::NightActivity,
            severity: Severity::High,
            breach: v.value >= p.night_steps,
            required: 1,
        });
    }
    out
}

/// Rebuildable transition per (device,epoch,kind). The caller must load
/// authoritative state and commit any incident/outbox atomically; this
/// function does not authorize, persist or send notifications.
#[derive(Default, Debug, PartialEq, Eq)]
pub struct State {
    pub key: Option<String>,
    pub source: Option<String>,
    pub streak: u64,
    pub active: bool,
}
#[derive(Debug, PartialEq, Eq)]
pub enum Transition {
    Duplicate,
    Updated,
    Activate,
}
impl State {
    pub fn apply(&mut self, c: Candidate, key: &str, source: Option<&str>) -> Transition {
        if self.key.as_deref() == Some(key) {
            return Transition::Duplicate;
        }
        if self.key.is_some() && self.source.as_deref() != source {
            self.streak = 0;
            self.active = false;
        }
        if !c.breach {
            self.streak = 0;
            self.active = false;
        } else if c.kind == Kind::NoReport {
            self.streak = 1;
        } else {
            self.streak = self.streak.saturating_add(1);
        }
        self.key = Some(key.to_owned());
        self.source = source.map(str::to_owned);
        if c.breach && self.streak >= u64::from(c.required) && !self.active {
            self.active = true;
            Transition::Activate
        } else {
            Transition::Updated
        }
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn transitions_are_event_keyed() {
        let mut s = State::default();
        let mut c = Candidate {
            kind: Kind::HeartRate,
            severity: Severity::High,
            breach: true,
            required: 3,
        };
        assert_eq!(s.apply(c, "1", Some("watch")), Transition::Updated);
        assert_eq!(s.apply(c, "1", Some("watch")), Transition::Duplicate);
        assert_eq!(s.apply(c, "2", Some("watch")), Transition::Updated);
        assert_eq!(s.apply(c, "3", Some("watch")), Transition::Activate);
        assert_eq!(s.apply(c, "4", Some("watch")), Transition::Updated);
        assert_eq!(s.apply(c, "5", Some("other")), Transition::Updated);
        assert_eq!(s.streak, 1);
        assert!(!s.active);
        c.breach = false;
        assert_eq!(s.apply(c, "6", Some("other")), Transition::Updated);
        assert_eq!(s.streak, 0);
    }
}
