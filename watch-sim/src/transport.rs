//! Bounded HTTP transport. Each call sends one request; writes are never replayed here.
use crate::protocol::{Credentials, PreparedSigner, Reply, RequestSigning};
use serde_json::Value;
use std::{path::Path, sync::Arc, time::Duration};

#[derive(Debug)]
pub enum TransportError {
    Network(String),
    Timeout,
    Server(u16),
    HealthWriteBusy,
    Decode(String),
}
impl TransportError {
    pub fn message(&self) -> String {
        match self {
            Self::Network(m) => format!("连接失败：{m}"),
            Self::Timeout => "请求超时".into(),
            Self::Server(status) => format!("服务暂不可用：HTTP {status}"),
            Self::HealthWriteBusy => "Health upload admission busy".into(),
            Self::Decode(m) => format!("响应格式错误：{m}"),
        }
    }
    pub fn retryable(&self) -> bool {
        matches!(
            self,
            Self::Network(_) | Self::Timeout | Self::Server(_) | Self::HealthWriteBusy
        )
    }
}
pub struct Client {
    agent: ureq::Agent,
    base: String,
    signing: bool,
}
impl Client {
    pub fn new(base: &str, timeout: Duration, signing: bool) -> Self {
        Self::with_ca(base, timeout, signing, None).expect("系统 TLS 初始化失败")
    }
    pub fn with_ca(
        base: &str,
        timeout: Duration,
        signing: bool,
        ca: Option<&Path>,
    ) -> Result<Self, String> {
        let mut tls = native_tls::TlsConnector::builder();
        if let Some(path) = ca {
            let bytes = std::fs::read(path).map_err(|e| format!("无法读取 CA 文件：{e}"))?;
            tls.add_root_certificate(
                native_tls::Certificate::from_pem(&bytes)
                    .map_err(|e| format!("CA 证书无效：{e}"))?,
            );
        }
        let tls = tls.build().map_err(|e| format!("TLS 初始化失败：{e}"))?;
        let agent = ureq::AgentBuilder::new()
            .timeout_connect(Duration::from_secs(5))
            .timeout(timeout)
            .redirects(0)
            .tls_connector(Arc::new(tls))
            .user_agent(concat!("yihe-watch-sim/", env!("CARGO_PKG_VERSION")))
            .build();
        Ok(Self {
            agent,
            base: base.trim_end_matches('/').into(),
            signing,
        })
    }
    pub fn base(&self) -> &str {
        &self.base
    }
    pub fn signing(&self) -> bool {
        self.signing
    }
    pub fn post_raw(
        &self,
        path: &str,
        raw: &[u8],
        cred: Option<&Credentials>,
    ) -> Result<Reply, TransportError> {
        self.post_raw_with(path, raw, cred)
    }
    pub(crate) fn post_raw_prepared(
        &self,
        path: &str,
        raw: &[u8],
        signer: &PreparedSigner,
    ) -> Result<Reply, TransportError> {
        self.post_raw_with(path, raw, Some(signer))
    }
    fn post_raw_with<S: RequestSigning>(
        &self,
        path: &str,
        raw: &[u8],
        cred: Option<&S>,
    ) -> Result<Reply, TransportError> {
        let mut req = self
            .agent
            .post(&format!("{}/{}", self.base, path.trim_start_matches('/')))
            .set("Content-Type", "application/json")
            .set("Accept", "application/json");
        if self.signing || matches!(path, "api/v1/device/report" | "api/v1/device/sos") {
            if let Some(c) = cred {
                let headers = c.headers(
                    raw,
                    crate::now_unix(),
                    uuid::Uuid::new_v4().simple().to_string(),
                );
                for (k, v) in headers.pairs_ref() {
                    req = req.set(k, v);
                }
            }
        }
        let response = match req.send_bytes(raw) {
            Ok(r) => r,
            Err(ureq::Error::Status(_, r)) => r,
            Err(ureq::Error::Transport(e)) => {
                return Err(
                    if e.kind() == ureq::ErrorKind::Io && e.to_string().contains("timed out") {
                        TransportError::Timeout
                    } else {
                        TransportError::Network(e.to_string())
                    },
                )
            }
        };
        let status = response.status();
        // Proxies may return empty or HTML error pages. Preserve the transient
        // status before decoding; the caller owns retries and event identity.
        if status == 503 && path.trim_start_matches('/') == "api/v1/device/healthBatch" {
            use std::io::Read;
            let mut body = Vec::new();
            if response
                .into_reader()
                .take(4097)
                .read_to_end(&mut body)
                .is_ok()
                && body.len() <= 4096
                && explicit_health_busy(&body)
            {
                return Err(TransportError::HealthWriteBusy);
            }
            return Err(TransportError::Server(status));
        }
        if matches!(status, 500 | 502 | 503 | 504) {
            return Err(TransportError::Server(status));
        }
        let mut data = Vec::new();
        use std::io::Read;
        response
            .into_reader()
            .take(1_048_577)
            .read_to_end(&mut data)
            .map_err(|e| TransportError::Decode(e.to_string()))?;
        if data.len() > 1_048_576 {
            return Err(TransportError::Decode("响应超过 1 MiB".into()));
        }
        let body: Value = serde_json::from_slice(&data)
            .map_err(|_| TransportError::Decode(format!("HTTP {status} 未返回 JSON")))?;
        if !body.is_object() {
            return Err(TransportError::Decode("结果必须是对象".into()));
        }
        Ok(Reply { status, body })
    }
    pub fn post(
        &self,
        path: &str,
        body: &Value,
        cred: Option<&Credentials>,
    ) -> Result<Reply, TransportError> {
        let raw = serde_json::to_vec(body).map_err(|e| TransportError::Decode(e.to_string()))?;
        self.post_raw(path, &raw, cred)
    }
    pub fn post_signed_by(
        &self,
        path: &str,
        body: &Value,
        cred: &Credentials,
    ) -> Result<Reply, TransportError> {
        self.post(path, body, Some(cred))
    }
}

fn explicit_health_busy(body: &[u8]) -> bool {
    serde_json::from_slice::<Value>(body)
        .ok()
        .is_some_and(|reply| reply.get("code").and_then(Value::as_str) == Some("HEALTH_WRITE_BUSY"))
}

#[cfg(test)]
mod tests {
    use super::explicit_health_busy;

    #[test]
    fn only_explicit_admission_rejection_is_safe_to_retry() {
        assert!(explicit_health_busy(br#"{"code":"HEALTH_WRITE_BUSY"}"#));
        assert!(!explicit_health_busy(br#"{"code":"DB_UNAVAILABLE"}"#));
        assert!(!explicit_health_busy(b"<html>503</html>"));
    }
}
