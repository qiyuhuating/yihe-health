// ---------------------------------------------------------------------------
// 返回包络
// ---------------------------------------------------------------------------

use serde_json::Value;

/// 后端统一包络：HTTP 200 也可能是业务失败，必须看 `returnCode`。
#[derive(Debug, Clone)]
pub struct Reply {
    pub status: u16,
    pub body: Value,
}

impl Reply {
    pub fn code(&self) -> String {
        if self.ok() {
            return crate::CODE_OK.into();
        }
        self.body
            .get("code")
            .and_then(Value::as_str)
            .unwrap_or("INVALID_RESPONSE")
            .into()
    }
    pub fn ok(&self) -> bool {
        self.status == 200
            && self.body.get("returnCode").and_then(Value::as_str) == Some(crate::CODE_OK)
    }
    pub fn message(&self) -> String {
        let message = self
            .body
            .get("message")
            .and_then(Value::as_str)
            .unwrap_or("请求未完成");
        match self.body.get("reference").and_then(Value::as_str) {
            Some(id) if !id.is_empty() => format!("{message}（编号：{id}）"),
            _ => message.into(),
        }
    }

    pub fn get_i64(&self, key: &str) -> Option<i64> {
        self.body.get(key).and_then(|v| match v {
            Value::Number(n) => n.as_i64(),
            Value::String(s) => s.parse().ok(),
            _ => None,
        })
    }

    pub fn get_str(&self, key: &str) -> Option<String> {
        self.body.get(key).and_then(|v| match v {
            Value::String(s) => Some(s.clone()),
            Value::Number(n) => Some(n.to_string()),
            _ => None,
        })
    }

    pub fn get_bool(&self, key: &str) -> Option<bool> {
        self.body.get(key).and_then(|v| match v {
            Value::Bool(b) => Some(*b),
            Value::Number(n) => Some(n.as_i64().unwrap_or(0) != 0),
            _ => None,
        })
    }

    /// 绑定版本（历元）。重绑后后端会返回新值，旧批次必须作废。
    pub fn binding_version(&self) -> Option<i64> {
        self.get_i64("bindingVersion")
    }

    pub fn bound(&self) -> bool {
        self.get_bool("bound").unwrap_or(false) || self.get_i64("bound") == Some(1)
    }
}
