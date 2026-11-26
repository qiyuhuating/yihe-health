//! 离线队列。
//!
//! 语义对齐后端与 HarmonyOS 端 `Protocol.ets::enqueue`：
//! 队列**不跨绑定版本混传**——绑定一变，旧记录不能再补传，只能留待核查。
//! 容量对齐后端公布的 `maxBatchSize`（200）。

use std::collections::VecDeque;

use crate::protocol::{HealthSample, MAX_BATCH_SIZE};

// Alert samples get priority when the bounded offline queue is congested.
fn is_priority_alert_sample(sample: &HealthSample) -> bool {
    let metric = sample.metric.as_ref();
    let semantics = sample.semantics.as_ref();
    if ["sos", "fall", "alert", "emergency"]
        .iter()
        .any(|tag| semantics.contains(tag) || metric.contains(tag))
    {
        return true;
    }
    if metric.contains("heart_rate") && (sample.value < 45.0 || sample.value > 130.0) {
        return true;
    }
    (metric.contains("spo2") || metric.contains("oxygen")) && sample.value < 90.0
}

#[derive(Debug)]
pub struct OfflineQueue {
    capacity: usize,
    items: VecDeque<HealthSample>,
    /// 入队时锁定的绑定版本；与当前版本不一致即视为陈旧队列。
    epoch: Option<i64>,
    dropped: u64,
}

impl Default for OfflineQueue {
    fn default() -> Self {
        Self::new(MAX_BATCH_SIZE)
    }
}

impl OfflineQueue {
    pub fn new(capacity: usize) -> Self {
        OfflineQueue {
            capacity: capacity.max(1),
            items: VecDeque::with_capacity(capacity.max(1)),
            epoch: None,
            dropped: 0,
        }
    }

    pub fn len(&self) -> usize {
        self.items.len()
    }

    pub fn is_empty(&self) -> bool {
        self.items.is_empty()
    }

    pub fn epoch(&self) -> Option<i64> {
        self.epoch
    }

    /// 累计因容量溢出被丢弃的条数——用于在面板上显示"真的丢了"。
    pub fn dropped(&self) -> u64 {
        self.dropped
    }

    /// 入队。队列满时丢最旧的一条并计数，而不是静默拒绝。
    pub fn push(&mut self, sample: HealthSample, epoch: i64) {
        if self.epoch.is_some_and(|current| current != epoch) {
            self.clear();
        }
        if self.epoch.is_none() {
            self.epoch = Some(epoch);
        }
        if self.items.len() >= self.capacity {
            if let Some(index) = self
                .items
                .iter()
                .position(|existing| !is_priority_alert_sample(existing))
            {
                self.items.remove(index);
            } else if is_priority_alert_sample(&sample) {
                self.items.pop_front();
            } else {
                // Never evict a critical alert to admit routine telemetry.
                self.dropped += 1;
                return;
            }
            self.dropped += 1;
        }
        self.items.push_back(sample);
    }

    /// 借用待上传样本，避免弱网重试时复制整批记录。
    pub fn iter_pending(&self, max: usize) -> impl Iterator<Item = &HealthSample> {
        self.items.iter().take(max.min(self.capacity))
    }

    /// 上传成功后按条数出队。后端对重复 `recordId` 返回 duplicates，
    /// 因此"确认成功"只需按已发送条数推进，不必比对内容。
    pub fn commit(&mut self, n: usize) {
        for _ in 0..n.min(self.items.len()) {
            self.items.pop_front();
        }
        if self.items.is_empty() {
            self.epoch = None;
        }
    }

    pub fn clear(&mut self) {
        self.items.clear();
        self.epoch = None;
    }

    /// 绑定版本变化时清空队列，返回作废条数。
    pub fn invalidate_if_epoch_changed(&mut self, current: i64) -> usize {
        match self.epoch {
            Some(e) if e != current => {
                let n = self.items.len();
                self.clear();
                n
            }
            _ => 0,
        }
    }

    /// 队列内容是否属于旧绑定（决定重连后能不能补传）。
    pub fn is_stale_for(&self, current: i64) -> bool {
        matches!(self.epoch, Some(e) if e != current)
    }
}

#[cfg(test)]
mod priority_eviction_tests {
    use super::*;

    #[test]
    fn preserves_alert_over_oldest_routine_sample() {
        let mut q = OfflineQueue::new(2);
        q.push(
            HealthSample::heart_rate("alert".into(), "watch-1", 155, "2026-11-09T00:00:01Z"),
            1,
        );
        q.push(
            HealthSample::steps(
                "routine".into(),
                "watch-1",
                100,
                "2026-11-09T00:00:00Z",
                "2026-11-09T00:00:02Z",
            ),
            1,
        );
        q.push(
            HealthSample::steps(
                "new".into(),
                "watch-1",
                120,
                "2026-11-09T00:00:00Z",
                "2026-11-09T00:00:03Z",
            ),
            1,
        );
        let ids: Vec<&str> = q
            .iter_pending(2)
            .map(|item| item.record_id.as_str())
            .collect();
        assert_eq!(ids, ["alert", "new"]);
        assert_eq!(q.dropped(), 1);
    }

    #[test]
    fn drops_new_routine_when_only_critical_alerts_remain() {
        let mut q = OfflineQueue::new(1);
        q.push(
            HealthSample::heart_rate("alert".into(), "watch-1", 155, "2026-11-09T00:00:01Z"),
            1,
        );
        q.push(
            HealthSample::steps(
                "routine".into(),
                "watch-1",
                100,
                "2026-11-09T00:00:00Z",
                "2026-11-09T00:00:02Z",
            ),
            1,
        );
        assert_eq!(
            q.iter_pending(1).next().unwrap().record_id.as_str(),
            "alert"
        );
        assert_eq!(q.dropped(), 1);
    }
}
