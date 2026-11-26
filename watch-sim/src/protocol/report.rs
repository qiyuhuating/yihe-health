// ---------------------------------------------------------------------------
// 旧版扁平遥测（POST /api/v1/device/report）
// ---------------------------------------------------------------------------

use serde::Serialize;

use super::Credentials;

/// 扁平遥测负载。字段可为空，但**一旦提供**就必须落在后端允许的范围内。
#[derive(Debug, Clone, Default, Serialize)]
pub struct ReportPayload {
    #[serde(rename = "deviceNo")]
    pub device_no: String,
    pub secret: String,
    #[serde(rename = "beTime", skip_serializing_if = "String::is_empty")]
    pub be_time: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub step: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sleep: Option<i64>,
    #[serde(rename = "sleepState", skip_serializing_if = "Option::is_none")]
    pub sleep_state: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub hr: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub bp: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub lng: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub lat: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub battery: Option<i64>,
    #[serde(rename = "appVersion", skip_serializing_if = "Option::is_none")]
    pub app_version: Option<String>,
    /// `"1"` 或 `"fall"`。
    #[serde(skip_serializing_if = "Option::is_none")]
    pub alarm: Option<String>,
    #[serde(rename = "positionSource", skip_serializing_if = "Option::is_none")]
    pub position_source: Option<String>,
    #[serde(rename = "positionSampledAt", skip_serializing_if = "Option::is_none")]
    pub position_sampled_at: Option<String>,
    #[serde(rename = "positionAccuracy", skip_serializing_if = "Option::is_none")]
    pub position_accuracy: Option<f64>,
    /// 带上它就进入后端持久队列分支（需服务端 `YIHE_DURABLE_TELEMETRY=1`）。
    #[serde(rename = "requestId", skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
    #[serde(rename = "bindingVersion", skip_serializing_if = "Option::is_none")]
    pub binding_version: Option<i64>,
}

impl ReportPayload {
    pub fn new(cred: &Credentials, be_time: String) -> Self {
        ReportPayload {
            device_no: cred.device_no.clone(),
            secret: cred.secret.clone(),
            be_time,
            ..Default::default()
        }
    }

    /// 本地预校验，把后端会 400 的情况提前拦下来，压测时不必浪费往返。
    pub fn validate(&self) -> Result<(), String> {
        if !self.be_time.is_empty() && self.be_time.len() != 19 {
            return Err(format!(
                "beTime 必须是 yyyy-MM-dd HH:mm:ss，实际 {:?}",
                self.be_time
            ));
        }
        check_metric("hr", self.hr, 20, 250)?;
        check_metric("bp", self.bp, 40, 300)?;
        check_metric("step", self.step, 0, 200_000)?;
        check_metric("sleep", self.sleep, 0, 24)?;
        check_metric("battery", self.battery, 0, 100)?;
        if let Some(s) = &self.sleep_state {
            if s != "0" && s != "1" {
                return Err("sleepState 应为 0 或 1".into());
            }
        }
        if let (Some(lng), Some(lat)) = (&self.lng, &self.lat) {
            if lng.chars().count() > 20 || lat.chars().count() > 20 {
                return Err("经纬度超过 20 字符".into());
            }
            let lo: f64 = lng.parse().map_err(|_| "lng 不是数字")?;
            let la: f64 = lat.parse().map_err(|_| "lat 不是数字")?;
            if !lo.is_finite() || !la.is_finite() || lo.abs() > 180.0 || la.abs() > 90.0 {
                return Err("经纬度超出有效范围".into());
            }
        } else if self.lng.is_some() || self.lat.is_some() {
            return Err("经纬度必须成对提供".into());
        }
        if let Some(src) = &self.position_source {
            const ALLOWED: [&str; 6] =
                ["gps", "network", "fused", "manual", "simulated", "unknown"];
            if !ALLOWED.contains(&src.as_str()) {
                return Err(format!("positionSource 非法: {src}"));
            }
        }
        if let Some(v) = &self.app_version {
            if v.chars().count() > 20 {
                return Err("appVersion 超过 20 字符".into());
            }
        }
        if let Some(v) = &self.source {
            if v.chars().count() > 20 {
                return Err("source 超过 20 字符".into());
            }
        }
        if let Some(a) = &self.alarm {
            if a != "1" && !a.eq_ignore_ascii_case("fall") {
                return Err("alarm 只能是 \"1\" 或 \"fall\"".into());
            }
        }
        if self.position_accuracy.is_some() && self.lng.is_none() {
            return Err("定位元数据必须与完整经纬度一起提供".into());
        }
        Ok(())
    }

    /// 是否够格进入后端持久队列（`durableTelemetryEligible` 的客户端镜像）。
    ///
    /// 后端判定是**排他白名单**：只要带了 `hr/bp/alarm/lng/lat/battery/appVersion`
    /// 任意一个，就静默走普通上报，**不报错**。等回执发现已经太晚，所以这里先自查。
    pub fn durable_eligible(&self) -> bool {
        let high_value_absent = self.hr.is_none()
            && self.bp.is_none()
            && self.alarm.is_none()
            && self.lng.is_none()
            && self.lat.is_none()
            && self.battery.is_none()
            && self.app_version.is_none();
        let low_value_present =
            self.step.is_some() || self.sleep.is_some() || self.sleep_state.is_some();
        let id_ok = self
            .request_id
            .as_deref()
            .map(request_id_ok)
            .unwrap_or(false);
        high_value_absent && low_value_present && id_ok
    }
}

/// 后端 `durableRequestIDPattern` / `sosRequestIDPattern`：`^[A-Za-z0-9_.:-]{1,64}$`。
pub fn request_id_ok(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 64
        && id
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '_' | '.' | ':' | '-'))
}

fn check_metric(name: &str, value: Option<i64>, min: i64, max: i64) -> Result<(), String> {
    match value {
        // 后端 `parseMetric` 对 0 直接放行，不参与区间判断。
        Some(0) | None => Ok(()),
        Some(v) if v < min || v > max => Err(format!("{name}={v} 超出 {min}..{max}")),
        Some(_) => Ok(()),
    }
}
