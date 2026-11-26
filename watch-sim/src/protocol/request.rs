use serde::Serialize;

use super::{Credentials, HealthSample, MAX_BATCH_SIZE};

#[derive(Debug, Clone, Serialize)]
pub struct BatchRequest {
    #[serde(rename = "deviceNo")]
    pub device_no: String,
    pub secret: String,
    #[serde(rename = "bindingVersion")]
    pub binding_version: i64,
    pub samples: Vec<HealthSample>,
}

impl BatchRequest {
    pub fn new(cred: &Credentials, binding_version: i64, samples: Vec<HealthSample>) -> Self {
        BatchRequest {
            device_no: cred.device_no.clone(),
            secret: cred.secret.clone(),
            binding_version,
            samples,
        }
    }

    pub fn validate(&self) -> Result<(), String> {
        if self.binding_version < 1 {
            return Err("bindingVersion 必须 ≥1，请先登录取得绑定版本".into());
        }
        if self.samples.len() > MAX_BATCH_SIZE {
            return Err(format!(
                "单批最多 {MAX_BATCH_SIZE} 条，实际 {}",
                self.samples.len()
            ));
        }
        if self.samples.is_empty() {
            return Err("批次不能为空".into());
        }
        Ok(())
    }
}

// ---------------------------------------------------------------------------
// SOS（POST /api/v1/device/sos）
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize)]
pub struct SosRequest {
    #[serde(rename = "deviceNo")]
    pub device_no: String,
    pub secret: String,
    #[serde(rename = "requestId")]
    pub request_id: String,
    #[serde(rename = "bindingVersion")]
    pub binding_version: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub lng: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub lat: Option<String>,
    #[serde(rename = "positionSource")]
    pub position_source: &'static str,
}

impl SosRequest {
    pub fn validate(&self) -> Result<(), String> {
        if self.binding_version < 1 {
            return Err("SOS 必须携带 bindingVersion".into());
        }
        if self.request_id.is_empty() || self.request_id.len() > 64 {
            return Err("requestId 长度必须为 1–64".into());
        }
        if !self
            .request_id
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '_' | '.' | ':' | '-'))
        {
            return Err("requestId 只允许字母、数字与 _ . : - ".into());
        }
        Ok(())
    }
}
