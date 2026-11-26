//! 设备协议：负载类型、请求签名、返回包络。
//!
//! 字段上限与取值范围逐条对齐后端 `telemetry_validation.go`、`telemetry_parser.go`、
//! `position_metadata.go`、`handler_device_sos.go`。改动这里之前先读那四个文件。

use serde_json::{json, Map, Value};

mod reply;
mod report;
mod request;
mod sample;
mod signing;

pub use self::reply::Reply;
pub use self::report::{request_id_ok, ReportPayload};
pub use self::request::{BatchRequest, SosRequest};
pub use self::sample::{
    record_id_into, BatchView, HealthSample, HealthSampleData, SampleMetadata, SampleRef,
    SampleView,
};
pub use self::signing::{sign, signature_headers, PreparedSigner, SignatureHeaders};

pub(crate) use self::signing::RequestSigning;

/// 整数值按整数写，省掉 `.0` 两个字节。
fn ser_compact_number<S: serde::Serializer>(value: &f64, ser: S) -> Result<S::Ok, S::Error> {
    if value.fract() == 0.0 && value.abs() < 9.0e15 {
        ser.serialize_i64(*value as i64)
    } else {
        ser.serialize_f64(*value)
    }
}

// ---------------------------------------------------------------------------
// 凭据与登录
// ---------------------------------------------------------------------------

#[derive(Debug, Clone)]
pub struct Credentials {
    pub device_no: String,
    pub secret: String,
}

impl Credentials {
    pub fn new(device_no: impl Into<String>, secret: impl Into<String>) -> Self {
        Credentials {
            device_no: device_no.into().trim().to_uppercase(),
            secret: secret.into(),
        }
    }

    /// 后端 `checkDeviceCredentials`：deviceNo ≤32 字符、secret ≤128 字符，均非空。
    pub fn validate(&self) -> Result<(), String> {
        if self.device_no.is_empty() || self.device_no.chars().count() > 32 {
            return Err("设备号必须为 1–32 个字符".into());
        }
        if self.secret.is_empty() || self.secret.chars().count() > 128 {
            return Err("设备密钥必须为 1–128 个字符".into());
        }
        Ok(())
    }
}

/// 登录时声明的平台。`HarmonyOs` 会让后端回 `protocolVersion` 与 `maxBatchSize`。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Platform {
    /// 未指定设备平台，不协商 HarmonyOS 专用批量能力。
    Unspecified,
    /// platform=harmonyos + protocolVersion=1。
    HarmonyOs,
}

pub const PROTOCOL_VERSION: i64 = 1;
pub const MAX_BATCH_SIZE: usize = 200;

pub fn login_body(cred: &Credentials, platform: Platform) -> Value {
    let mut m = Map::new();
    m.insert("deviceNo".into(), json!(cred.device_no));
    m.insert("secret".into(), json!(cred.secret));
    if platform == Platform::HarmonyOs {
        m.insert("platform".into(), json!("harmonyos"));
        m.insert("protocolVersion".into(), json!(PROTOCOL_VERSION));
    }
    Value::Object(m)
}
