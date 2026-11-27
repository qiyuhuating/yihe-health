//! Local differential oracle only: no unauthenticated HTTP analytics route.
use serde::Deserialize;
use serde_json::{Value, json};
use yihe_core::{Point, SeriesKey, Window, rolling::RollingHeartRate, rules};

#[derive(Deserialize)]
struct Trace {
    width: i64,
    values: Vec<u8>,
}
pub fn rolling(input: Value) -> Result<Value, Box<dyn std::error::Error>> {
    let trace: Trace = serde_json::from_value(input)?;
    let key = SeriesKey {
        resident_id: 1,
        source: "watch".into(),
        source_device: "fixture".into(),
    };
    let mut state = RollingHeartRate::from_committed(
        key.clone(),
        1,
        Window { from: 0, to: 1 },
        &[],
        usize::try_from(trace.width)?,
    )
    .map_err(|_| "invalid rolling snapshot")?;
    let mut results = Vec::with_capacity(trace.values.len());
    for (i, value) in trace.values.into_iter().enumerate() {
        let at = i as i64 + 1;
        let window = Window {
            from: (at - trace.width + 1).max(0),
            to: at + 1,
        };
        let s = state
            .append(
                &key,
                at + 1,
                window,
                Point {
                    at,
                    value: f64::from(value),
                },
            )
            .map_err(|_| "invalid append")?;
        results.push(json!([s.count, s.min, s.max, s.mean]));
    }
    Ok(json!(results))
}
#[derive(Deserialize)]
struct Obs {
    value: f64,
    at_ns: i64,
}
impl From<Obs> for rules::Value {
    fn from(v: Obs) -> Self {
        Self {
            value: v.value,
            at_ns: v.at_ns,
        }
    }
}
#[derive(Deserialize)]
struct Base {
    count: usize,
    mean: f64,
    deviation: f64,
}
impl From<Base> for rules::Baseline {
    fn from(v: Base) -> Self {
        Self {
            count: v.count,
            mean: v.mean,
            deviation: v.deviation,
        }
    }
}
#[derive(Deserialize)]
struct RuleInput {
    enabled: bool,
    night: bool,
    activity_night: bool,
    now_ns: i64,
    last_report_ns: Option<i64>,
    heart: Option<Obs>,
    blood: Option<Obs>,
    battery: Option<Obs>,
    activity: Option<Obs>,
    heart_baseline: Option<Base>,
    blood_baseline: Option<Base>,
}
#[derive(Deserialize)]
struct TransitionInput {
    kind: String,
    key: String,
    source: Option<String>,
    breach: bool,
    required: u8,
}
pub fn transitions(input: Value) -> Result<Value, Box<dyn std::error::Error>> {
    let events: Vec<TransitionInput> = serde_json::from_value(input)?;
    let mut states: [rules::State; 7] = std::array::from_fn(|_| rules::State::default());
    let mut output = Vec::with_capacity(events.len());
    for event in events {
        let kind = match event.kind.as_str() {
            "heart_rate" => rules::Kind::HeartRate,
            "no_report" => rules::Kind::NoReport,
            _ => return Err("unsupported transition fixture".into()),
        };
        let state = &mut states[kind as usize];
        let transition = state.apply(
            rules::Candidate {
                kind,
                severity: rules::Severity::High,
                breach: event.breach,
                required: event.required,
            },
            &event.key,
            event.source.as_deref(),
        );
        let action = match transition {
            rules::Transition::Duplicate => "duplicate",
            rules::Transition::Updated => "updated",
            rules::Transition::Activate => "activate",
        };
        output.push(json!([action, state.streak, state.active]));
    }
    Ok(json!(output))
}
pub(crate) fn evaluate_input(
    input: Value,
) -> Result<rules::Candidates, Box<dyn std::error::Error>> {
    let v: RuleInput = serde_json::from_value(input)?;
    let policy = rules::Policy {
        enabled: v.enabled,
        ..Default::default()
    };
    let input = rules::Input {
        now_ns: v.now_ns,
        night: v.night,
        activity_night: v.activity_night,
        last_report_ns: v.last_report_ns,
        heart: v.heart.map(Into::into),
        blood: v.blood.map(Into::into),
        battery: v.battery.map(Into::into),
        activity: v.activity.map(Into::into),
        heart_baseline: v.heart_baseline.map(Into::into),
        blood_baseline: v.blood_baseline.map(Into::into),
    };
    Ok(rules::evaluate(policy, input))
}
pub fn decisions(input: Value) -> Result<Value, Box<dyn std::error::Error>> {
    let candidates = evaluate_input(input)?;
    Ok(json!(
        candidates
            .as_slice()
            .iter()
            .map(|c| json!([c.kind.name(), c.severity.name(), c.breach, c.required]))
            .collect::<Vec<_>>()
    ))
}
