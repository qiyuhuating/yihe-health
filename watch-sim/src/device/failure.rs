use crate::protocol::Reply;
use crate::transport::TransportError;

#[derive(Debug)]
pub enum Failure {
    Transport(TransportError),
    Rejected { code: String, message: String },
}

impl Failure {
    pub fn message(&self) -> String {
        match self {
            Failure::Transport(e) => e.message(),
            Failure::Rejected { code, message } => format!("[{code}] {message}"),
        }
    }

    pub fn retryable(&self) -> bool {
        match self {
            Failure::Transport(e) => e.retryable(),
            Failure::Rejected { .. } => false,
        }
    }

    pub fn is_auth(&self) -> bool {
        matches!(self, Failure::Rejected { code, .. } if code == "DEVICE_AUTH_FAILED" || code == "DEVICE_DISABLED")
    }

    pub fn is_unbound(&self) -> bool {
        matches!(self, Failure::Rejected { code, .. } if code == "DEVICE_NOT_BOUND")
    }
}

pub(super) fn classify(reply: Reply) -> Result<Reply, Failure> {
    if reply.ok() {
        Ok(reply)
    } else {
        Err(Failure::Rejected {
            code: reply.code(),
            message: reply.message(),
        })
    }
}

pub(super) fn confirm_batch_reply(reply: Reply, count: usize) -> Result<Reply, Failure> {
    if reply.get_i64("accepted") == Some(count as i64)
        && reply
            .get_i64("duplicates")
            .is_some_and(|duplicates| duplicates >= 0 && duplicates <= count as i64)
    {
        Ok(reply)
    } else {
        Err(Failure::Rejected {
            code: "INVALID_RESPONSE".into(),
            message: "批次回执未确认全部样本或重复计数无效".into(),
        })
    }
}
