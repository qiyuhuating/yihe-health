//! 设备会话：把协议、队列与传输编排成"一台手表能做的全部动作"。
//!
//! 覆盖 preview 后端实际存在的六个设备侧接口：
//! `login` / `bind` / `heartbeat` / `report` / `healthBatch` / `sos`。

use std::sync::Arc;

use crate::encoding::BatchEncoder;
use crate::protocol::{Credentials, Platform, MAX_BATCH_SIZE};
use crate::queue::OfflineQueue;
use crate::transport::Client;

mod failure;
mod session;
mod upload;

pub use self::failure::Failure;

#[derive(Debug, Clone, PartialEq)]
pub struct Binding {
    pub resident_id: i64,
    pub resident_name: String,
    /// 绑定版本（后端 `device.health_epoch`）。重绑后会变。
    pub version: i64,
}

#[derive(Debug, Default, Clone)]
pub struct Stats {
    pub requests: u64,
    pub ok: u64,
    pub rejected: u64,
    pub failed: u64,
    pub queued_in: u64,
    pub queued_flushed: u64,
    pub duplicates: u64,
    pub incidents: u64,
    pub stale_dropped: u64,
}

#[derive(Debug, Clone)]
pub struct ReportOutcome {
    pub rate_state: Option<i64>,
    pub pre_state: Option<i64>,
    pub queued: bool,
    pub server_time: String,
    /// 报告是否已由服务端确认重复；普通 report 不按文案推断成功。
    pub duplicate_request: bool,
}

#[derive(Debug, Clone)]
pub struct HeartbeatOutcome {
    pub report_interval: u64,
    pub pending_alarm: i64,
    pub bound: bool,
    /// 本次心跳是否发现历元变化（重绑）。上层据此清掉 SOS 会话号等旧归属状态。
    pub epoch_changed: bool,
}

pub struct DeviceSession {
    cred: Credentials,
    client: Arc<Client>,
    signer: crate::protocol::PreparedSigner,
    platform: Platform,
    app_version: String,
    binding: Option<Binding>,
    queue: OfflineQueue,
    wire: Vec<u8>,
    encoder: BatchEncoder,
    pub stats: Stats,
    pub max_batch_size: usize,
}

impl DeviceSession {
    pub fn new(
        cred: Credentials,
        client: Arc<Client>,
        platform: Platform,
        app_version: String,
    ) -> Self {
        let signer = crate::protocol::PreparedSigner::new(&cred);
        DeviceSession {
            cred,
            client,
            signer,
            platform,
            app_version,
            binding: None,
            queue: OfflineQueue::default(),
            wire: Vec::with_capacity(1024),
            encoder: BatchEncoder::default(),
            stats: Stats::default(),
            max_batch_size: MAX_BATCH_SIZE,
        }
    }

    pub fn credentials(&self) -> &Credentials {
        &self.cred
    }

    pub fn binding(&self) -> Option<&Binding> {
        self.binding.as_ref()
    }

    pub fn queue(&self) -> &OfflineQueue {
        &self.queue
    }

    pub fn queue_mut(&mut self) -> &mut OfflineQueue {
        &mut self.queue
    }

    pub fn is_bound(&self) -> bool {
        self.binding.is_some()
    }
}
