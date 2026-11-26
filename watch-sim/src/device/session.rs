use serde_json::json;

use super::failure::classify;
use super::{Binding, DeviceSession, Failure, HeartbeatOutcome};
use crate::protocol::{login_body, Platform, Reply, MAX_BATCH_SIZE, PROTOCOL_VERSION};
use crate::transport::TransportError;

impl DeviceSession {
    pub(super) fn post_raw(&self, path: &str, raw: &[u8]) -> Result<Reply, TransportError> {
        self.client.post_raw_prepared(path, raw, &self.signer)
    }

    fn post_json(&self, path: &str, body: &serde_json::Value) -> Result<Reply, TransportError> {
        let raw = serde_json::to_vec(body).map_err(|e| TransportError::Decode(e.to_string()))?;
        self.post_raw(path, &raw)
    }

    /// 更新绑定身份；旧绑定队列保留，flush 会拒绝跨版本补传。
    fn sync_binding(&mut self, version: i64, resident_id: i64, resident_name: String) -> bool {
        let changed = self
            .binding
            .as_ref()
            .map(|b| b.version != version)
            .unwrap_or(false);
        self.binding = Some(Binding {
            resident_id,
            resident_name,
            version,
        });
        changed
    }

    pub(super) fn touch(&mut self, result: &Result<Reply, Failure>) {
        self.stats.requests += 1;
        match result {
            Ok(_) => self.stats.ok += 1,
            Err(Failure::Rejected { .. }) => self.stats.rejected += 1,
            Err(Failure::Transport(_)) => self.stats.failed += 1,
        }
    }

    /// 登录。声明 HarmonyOS 平台时会核验后端是否真的支持协议 1 与 200 条批量。
    pub fn login(&mut self) -> Result<Reply, Failure> {
        let body = login_body(&self.cred, self.platform);
        let result = self
            .post_json("api/v1/device/login", &body)
            .map_err(Failure::Transport)
            .and_then(classify);
        self.touch(&result);

        if let Ok(reply) = &result {
            if self.platform == Platform::HarmonyOs {
                let version = reply.get_i64("protocolVersion");
                let max_batch = reply.get_i64("maxBatchSize");
                if version != Some(PROTOCOL_VERSION) || max_batch != Some(MAX_BATCH_SIZE as i64) {
                    return Err(Failure::Rejected {
                        code: "PROTOCOL".into(),
                        message: "后端未协商出协议 1 / 200 条批量，请先确认后端版本".into(),
                    });
                }
                if let Some(n) = max_batch {
                    self.max_batch_size = n as usize;
                }
            }
            let version = reply.binding_version().unwrap_or(0);
            if reply.bound() && version > 0 {
                let resident_id = reply.get_i64("residentId").unwrap_or(0);
                let resident_name = reply.get_str("residentName").unwrap_or_default();
                if resident_id <= 0 {
                    return Err(Failure::Rejected {
                        code: "INVALID_RESPONSE".into(),
                        message: "登录缺少有效长者身份".into(),
                    });
                }
                self.sync_binding(version, resident_id, resident_name);
            } else {
                self.binding = None;
            }
        }
        result
    }

    pub fn bind(&mut self, code: &str) -> Result<Reply, Failure> {
        let body = json!({
            "deviceNo": self.cred.device_no,
            "secret": self.cred.secret,
            "code": code.trim().to_uppercase(),
        });
        let result = self
            .post_json("api/v1/device/bind", &body)
            .map_err(Failure::Transport)
            .and_then(classify);
        self.touch(&result);
        // 绑定成功后必须重新登录才能拿到新的 bindingVersion。
        if result.is_ok() {
            self.login()?;
        }
        result
    }

    pub fn heartbeat(&mut self, battery: i64) -> Result<HeartbeatOutcome, Failure> {
        let body = json!({
            "deviceNo": self.cred.device_no,
            "secret": self.cred.secret,
            "battery": battery,
            "appVersion": self.app_version,
        });
        let result = self
            .post_json("api/v1/device/heartbeat", &body)
            .map_err(Failure::Transport)
            .and_then(classify);
        self.touch(&result);
        let reply = result?;
        let previous = self.binding.clone();
        if !reply.bound() {
            self.binding = None;
        } else if let (Some(v), Some(id)) = (
            reply.binding_version().filter(|v| *v > 0),
            reply.get_i64("residentId").filter(|v| *v > 0),
        ) {
            let name = previous
                .as_ref()
                .filter(|b| b.resident_id == id)
                .map(|b| b.resident_name.clone())
                .unwrap_or_default();
            self.sync_binding(v, id, name);
        } else {
            return Err(Failure::Rejected {
                code: "INVALID_RESPONSE".into(),
                message: "心跳缺少绑定身份".into(),
            });
        }
        let epoch_changed = previous != self.binding;
        Ok(HeartbeatOutcome {
            // 后端协商的上报间隔。
            report_interval: reply.get_i64("reportInterval").unwrap_or(5).max(1) as u64,
            pending_alarm: reply.get_i64("pendingAlarm").unwrap_or(0),
            bound: reply.bound() || reply.get_i64("bound") == Some(1),
            epoch_changed,
        })
    }
}
