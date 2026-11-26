use serde::Serialize;

use super::failure::{classify, confirm_batch_reply};
use super::{DeviceSession, Failure, ReportOutcome};
use crate::protocol::{
    BatchView, HealthSample, HealthSampleData, Reply, ReportPayload, SosRequest, MAX_BATCH_SIZE,
};

#[derive(Serialize)]
struct QueuedBatch<'a> {
    #[serde(rename = "deviceNo")]
    device_no: &'a str,
    secret: &'a str,
    #[serde(rename = "bindingVersion")]
    binding_version: i64,
    samples: &'a [&'a HealthSample],
}

impl DeviceSession {
    /// 设备状态上报。携带 `requestId` 时由后端处理持久化与幂等。
    pub fn report(&mut self, payload: &ReportPayload) -> Result<ReportOutcome, Failure> {
        payload.validate().map_err(|m| Failure::Rejected {
            code: "CLIENT".into(),
            message: m,
        })?;
        self.wire.clear();
        serde_json::to_writer(&mut self.wire, payload).map_err(|e| Failure::Rejected {
            code: "CLIENT".into(),
            message: format!("负载序列化失败：{e}"),
        })?;
        let result = self
            .post_raw("api/v1/device/report", &self.wire)
            .map_err(Failure::Transport)
            .and_then(classify);
        self.touch(&result);
        let reply = result?;
        Ok(ReportOutcome {
            rate_state: reply.get_i64("rateState"),
            pre_state: reply.get_i64("preState"),
            queued: reply.get_bool("queued").unwrap_or(false),
            server_time: reply.get_str("serverTime").unwrap_or_default(),
            duplicate_request: false,
        })
    }

    /// 把样本放进离线队列。
    pub fn enqueue(
        &mut self,
        samples: impl IntoIterator<Item = HealthSample>,
    ) -> Result<(), Failure> {
        let epoch = self.binding.as_ref().map(|b| b.version).unwrap_or(0);
        let samples: Vec<_> = samples.into_iter().collect();
        if self.queue.is_stale_for(epoch) || self.queue.len() + samples.len() > MAX_BATCH_SIZE {
            return Err(Failure::Rejected {
                code: "QUEUE_BLOCKED".into(),
                message: "待传队列已满或属于旧绑定，原记录保留；本拍未加入队列".into(),
            });
        }
        self.stats.queued_in += samples.len() as u64;
        for sample in samples {
            self.queue.push(sample, epoch);
        }
        Ok(())
    }

    /// 上传离线队列。绑定版本不匹配的一律不传（后端也会 409）。
    pub fn flush(&mut self) -> Result<(usize, usize), Failure> {
        let Some(binding_version) = self.binding.as_ref().map(|b| b.version) else {
            return Err(Failure::Rejected {
                code: "DEVICE_NOT_BOUND".into(),
                message: "设备尚未绑定长者".into(),
            });
        };
        if self.queue.is_stale_for(binding_version) {
            return Err(Failure::Rejected {
                code: "CONFLICT".into(),
                message: "待传记录属于旧绑定，已停止补传；记录仍保留在本进程队列中".into(),
            });
        }
        if self.queue.is_empty() {
            return Ok((0, 0));
        }
        let count = self.queue.len().min(self.max_batch_size);
        self.batch_binding(count)?;
        self.wire.clear();
        self.wire.reserve(count * 400 + 200);
        {
            let batch: Vec<_> = self.queue.iter_pending(self.max_batch_size).collect();
            let request = QueuedBatch {
                device_no: &self.cred.device_no,
                secret: &self.cred.secret,
                binding_version,
                samples: &batch,
            };
            serde_json::to_writer(&mut self.wire, &request).map_err(|e| Failure::Rejected {
                code: "CLIENT".into(),
                message: format!("批次序列化失败：{e}"),
            })?;
        }
        let result = self
            .post_raw("api/v1/device/healthBatch", &self.wire)
            .map_err(Failure::Transport)
            .and_then(classify)
            .and_then(|reply| confirm_batch_reply(reply, count));
        self.touch(&result);
        let reply = result?;
        let duplicates = reply.get_i64("duplicates").unwrap_or(0);
        if duplicates > 0 {
            self.stats.duplicates += duplicates as u64;
        }
        // 完整回执确认后才出队；失败时保留原 recordId 供显式重试。
        self.queue.commit(count);
        self.stats.queued_flushed += count as u64;
        Ok((count, 0))
    }

    /// 直接把一批样本发出去，不经离线队列。
    ///
    /// 压测与批量演示用：队列是为"断网补偿"设计的，走队列会额外压一次
    /// VecDeque 的入队/出队和容量裁剪，对纯吞吐测量是干扰。
    pub fn send_batch(&mut self, samples: Vec<HealthSample>) -> Result<(usize, usize), Failure> {
        self.send_samples(&samples)
    }

    /// 直接序列化借用的设备、时间和批次字段，避免按样本复制公共字符串。
    pub fn send_samples<S: serde::Serialize>(
        &mut self,
        samples: &[HealthSampleData<S>],
    ) -> Result<(usize, usize), Failure> {
        let count = samples.len();
        let binding_version = self.batch_binding(count)?;
        let request = BatchView {
            device_no: &self.cred.device_no,
            secret: &self.cred.secret,
            binding_version,
            samples,
        };
        self.wire.clear();
        self.wire.reserve(count * 400 + 200);
        serde_json::to_writer(&mut self.wire, &request).map_err(|e| Failure::Rejected {
            code: "CLIENT".into(),
            message: format!("批次序列化失败：{e}"),
        })?;
        self.send_batch_buffer(count)
    }

    pub fn send_sample_views<R: serde::Serialize>(
        &mut self,
        samples: &[HealthSampleData<&str, R>],
    ) -> Result<(usize, usize), Failure> {
        let count = samples.len();
        let binding_version = self.batch_binding(count)?;
        self.wire.clear();
        self.wire.reserve(count * 400 + 200);
        self.encoder
            .write(&mut self.wire, &self.cred, binding_version, samples)
            .map_err(|error| Failure::Rejected {
                code: "CLIENT".into(),
                message: format!("批次序列化失败：{error}"),
            })?;
        self.send_batch_buffer(count)
    }

    fn batch_binding(&self, count: usize) -> Result<i64, Failure> {
        let binding_version =
            self.binding
                .as_ref()
                .map(|b| b.version)
                .ok_or_else(|| Failure::Rejected {
                    code: "DEVICE_NOT_BOUND".into(),
                    message: "设备尚未绑定长者".into(),
                })?;
        if count == 0 || count > MAX_BATCH_SIZE || binding_version < 1 {
            return Err(Failure::Rejected {
                code: "CLIENT".into(),
                message: "批次需要有效绑定版本和 1–200 条样本".into(),
            });
        }
        Ok(binding_version)
    }

    fn send_batch_buffer(&mut self, count: usize) -> Result<(usize, usize), Failure> {
        let result = self
            .post_raw("api/v1/device/healthBatch", &self.wire)
            .map_err(Failure::Transport)
            .and_then(classify)
            .and_then(|reply| confirm_batch_reply(reply, count));
        self.touch(&result);
        let reply = result?;
        let duplicates = reply.get_i64("duplicates").unwrap_or(0);
        if duplicates > 0 {
            self.stats.duplicates += duplicates as u64;
        }
        self.stats.queued_flushed += count as u64;
        Ok((count, duplicates as usize))
    }

    /// 发送已经序列化好的批次字节。流水线模式要自己拼 HTTP，用这个走"只发一次
    /// 请求"的路径，避免重复序列化。
    pub fn send_batch_raw(&mut self, raw: &[u8]) -> Result<Reply, Failure> {
        let result = self
            .post_raw("api/v1/device/healthBatch", raw)
            .map_err(Failure::Transport)
            .and_then(classify);
        self.touch(&result);
        result
    }

    pub fn sos(
        &mut self,
        request_id: &str,
        lng: Option<f64>,
        lat: Option<f64>,
    ) -> Result<Reply, Failure> {
        let binding = self.binding.clone().ok_or_else(|| Failure::Rejected {
            code: "DEVICE_NOT_BOUND".into(),
            message: "设备尚未绑定长者，SOS 需要绑定版本".into(),
        })?;
        let request = SosRequest {
            position_source: "simulated",
            device_no: self.cred.device_no.clone(),
            secret: self.cred.secret.clone(),
            request_id: request_id.to_string(),
            binding_version: binding.version,
            lng: lng.map(|v| format!("{v:.6}")),
            lat: lat.map(|v| format!("{v:.6}")),
        };
        request.validate().map_err(|m| Failure::Rejected {
            code: "CLIENT".into(),
            message: m,
        })?;
        let raw = serde_json::to_vec(&request).map_err(|e| Failure::Rejected {
            code: "CLIENT".into(),
            message: format!("SOS 序列化失败：{e}"),
        })?;
        let result = self
            .post_raw("api/v1/device/sos", &raw)
            .map_err(Failure::Transport)
            .and_then(classify);
        self.touch(&result);
        if let Ok(reply) = &result {
            if reply.get_bool("duplicate").unwrap_or(false) {
                // 弱网重试同一 requestId：后端回了同一个 incident。
                self.stats.duplicates += 1;
            } else {
                self.stats.incidents += 1;
            }
        }
        result
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol::{BatchRequest, Credentials};

    #[test]
    fn borrowed_batch_matches_existing_wire_contract() {
        let cred = Credentials::new("watch-1", "secret");
        let samples = vec![HealthSample::heart_rate(
            "sample-1".into(),
            "watch-1",
            72,
            "2026-11-09T00:00:00Z",
        )];
        let refs: Vec<_> = samples.iter().collect();
        let borrowed = QueuedBatch {
            device_no: &cred.device_no,
            secret: &cred.secret,
            binding_version: 3,
            samples: &refs,
        };
        let owned = BatchRequest::new(&cred, 3, samples.clone());
        assert_eq!(
            serde_json::to_value(borrowed).unwrap(),
            serde_json::to_value(owned).unwrap()
        );
    }
}
