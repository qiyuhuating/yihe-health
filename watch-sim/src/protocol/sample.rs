// ---------------------------------------------------------------------------
// HarmonyOS 批量采样（POST /api/v1/device/healthBatch）
// ---------------------------------------------------------------------------

use std::borrow::Cow;

use serde::{Deserialize, Serialize};

use super::ser_compact_number;

/// 常量元数据借用静态字符串；设备和时间字段可由同一批次共享。
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct SampleMetadata {
    pub simulated: Cow<'static, str>,
    #[serde(rename = "counterTimezone", skip_serializing_if = "Option::is_none")]
    pub counter_timezone: Option<Cow<'static, str>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sensor: Option<Cow<'static, str>>,
    #[serde(rename = "sensorTimestamp", skip_serializing_if = "Option::is_none")]
    pub sensor_timestamp: Option<Cow<'static, str>>,
    #[serde(rename = "timestampBasis", skip_serializing_if = "Option::is_none")]
    pub timestamp_basis: Option<Cow<'static, str>>,
    #[serde(rename = "counterBasis", skip_serializing_if = "Option::is_none")]
    pub counter_basis: Option<Cow<'static, str>>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct HealthSampleData<S, R = String> {
    #[serde(rename = "recordId")]
    pub record_id: R,
    pub source: Cow<'static, str>,
    #[serde(rename = "sourceDevice")]
    pub source_device: S,
    #[serde(rename = "sourceVersion")]
    pub source_version: Cow<'static, str>,
    pub metric: Cow<'static, str>,
    pub unit: Cow<'static, str>,
    /// 整数指标（心率、步数、血氧）序列化成 `72` 而不是 `72.0`。
    /// 200 条批次能省约 400 字节，同时后端仍按 float64 解析。
    #[serde(serialize_with = "ser_compact_number")]
    pub value: f64,
    pub start: S,
    pub end: S,
    pub semantics: Cow<'static, str>,
    pub metadata: SampleMetadata,
    pub version: i64,
}

pub type HealthSample = HealthSampleData<String>;
pub type SampleView<'a> = HealthSampleData<&'a str>;
pub type SampleRef<'a> = HealthSampleData<&'a str, &'a str>;

/// Reuse caller-owned storage while retaining the run/sequence/metric ID grammar.
pub fn record_id_into(output: &mut String, prefix: &str, sequence: u64, suffix: &str) {
    output.clear();
    output.push_str(prefix);
    output.push('-');
    output.push_str(itoa::Buffer::new().format(sequence));
    output.push('-');
    output.push_str(suffix);
}

impl<S: Clone, R> HealthSampleData<S, R> {
    fn heart_rate_fields(record_id: R, device_no: S, value: i64, at: S) -> Self {
        Self {
            record_id,
            source: Cow::Borrowed("yihe-watch-sim"),
            source_device: device_no,
            source_version: Cow::Borrowed("2.0.0"),
            metric: Cow::Borrowed("heart_rate"),
            unit: Cow::Borrowed("count/min"),
            value: value as f64,
            start: at.clone(),
            end: at,
            semantics: Cow::Borrowed("instant"),
            metadata: SampleMetadata {
                simulated: Cow::Borrowed("true"),
                counter_timezone: None,
                sensor: Some(Cow::Borrowed("watch-sim:hr")),
                sensor_timestamp: None,
                timestamp_basis: Some(Cow::Borrowed("app-received")),
                counter_basis: None,
            },
            version: 1,
        }
    }

    fn steps_fields(record_id: R, device_no: S, value: i64, day_start: S, at: S) -> Self {
        Self {
            record_id,
            source: Cow::Borrowed("yihe-watch-sim"),
            source_device: device_no,
            source_version: Cow::Borrowed("2.0.0"),
            metric: Cow::Borrowed("steps"),
            unit: Cow::Borrowed("count"),
            value: value as f64,
            start: day_start,
            end: at,
            semantics: Cow::Borrowed("daily_total"),
            metadata: SampleMetadata {
                simulated: Cow::Borrowed("true"),
                counter_timezone: Some(Cow::Borrowed("UTC")),
                sensor: Some(Cow::Borrowed("watch-sim:pedometer")),
                sensor_timestamp: None,
                timestamp_basis: Some(Cow::Borrowed("app-received")),
                counter_basis: Some(Cow::Borrowed("utc-day")),
            },
            version: 1,
        }
    }
}

impl HealthSample {
    pub fn heart_rate(record_id: String, device_no: &str, value: i64, at: &str) -> Self {
        Self::heart_rate_fields(record_id, device_no.into(), value, at.into())
    }
    pub fn steps(
        record_id: String,
        device_no: &str,
        value: i64,
        day_start: &str,
        at: &str,
    ) -> Self {
        Self::steps_fields(
            record_id,
            device_no.into(),
            value,
            day_start.into(),
            at.into(),
        )
    }
    /// 精简版：省略 `sensor` 与 `timestampBasis` 两个可选元数据。
    ///
    /// 单条约省 60 字节（占比 18%）。后端不校验这两个字段，但前端"数据来源"
    /// 面板会少两项信息，所以只在压测/容量评估时开，默认不启用。
    pub fn heart_rate_lean(record_id: String, device_no: &str, value: i64, at: &str) -> Self {
        let mut sample = Self::heart_rate(record_id, device_no, value, at);
        sample.metadata.sensor = None;
        sample.metadata.timestamp_basis = None;
        sample
    }
}
impl<'a> SampleView<'a> {
    pub fn heart_rate(record_id: String, device_no: &'a str, value: i64, at: &'a str) -> Self {
        Self::heart_rate_fields(record_id, device_no, value, at)
    }
    pub fn steps(
        record_id: String,
        device_no: &'a str,
        value: i64,
        day_start: &'a str,
        at: &'a str,
    ) -> Self {
        Self::steps_fields(record_id, device_no, value, day_start, at)
    }
}

impl<'a> SampleRef<'a> {
    pub fn heart_rate(record_id: &'a str, device_no: &'a str, value: i64, at: &'a str) -> Self {
        Self::heart_rate_fields(record_id, device_no, value, at)
    }
    pub fn steps(
        record_id: &'a str,
        device_no: &'a str,
        value: i64,
        day_start: &'a str,
        at: &'a str,
    ) -> Self {
        Self::steps_fields(record_id, device_no, value, day_start, at)
    }
}

#[derive(Serialize)]
pub struct BatchView<'a, S, R = String> {
    #[serde(rename = "deviceNo")]
    pub device_no: &'a str,
    pub secret: &'a str,
    #[serde(rename = "bindingVersion")]
    pub binding_version: i64,
    pub samples: &'a [HealthSampleData<S, R>],
}
