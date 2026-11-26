use crate::protocol::{BatchView, Credentials, HealthSampleData};

/// Scratch space belongs to one device session; encoded strings never cross sessions.
#[derive(Default)]
pub struct BatchEncoder {
    device: Vec<u8>,
    start: Vec<u8>,
    end: Vec<u8>,
}

impl BatchEncoder {
    pub fn write<R: serde::Serialize>(
        &mut self,
        output: &mut Vec<u8>,
        credentials: &Credentials,
        binding_version: i64,
        samples: &[HealthSampleData<&str, R>],
    ) -> Result<(), serde_json::Error> {
        output.clear();
        let common = !samples.is_empty()
            && samples
                .iter()
                .all(|sample| canonical(sample) && sample.source_device == credentials.device_no);
        if !common {
            return serde_json::to_writer(
                output,
                &BatchView {
                    device_no: &credentials.device_no,
                    secret: &credentials.secret,
                    binding_version,
                    samples,
                },
            );
        }
        self.device.clear();
        serde_json::to_writer(&mut self.device, &credentials.device_no)?;
        output.extend_from_slice(b"{\"deviceNo\":");
        output.extend_from_slice(&self.device);
        output.extend_from_slice(b",\"secret\":");
        serde_json::to_writer(&mut *output, &credentials.secret)?;
        output.extend_from_slice(b",\"bindingVersion\":");
        let mut number = itoa::Buffer::new();
        output.extend_from_slice(number.format(binding_version).as_bytes());
        output.extend_from_slice(b",\"samples\":[");
        let mut cached_start = None;
        let mut cached_end = None;
        for (index, sample) in samples.iter().enumerate() {
            if cached_start != Some(sample.start) {
                self.start.clear();
                serde_json::to_writer(&mut self.start, sample.start)?;
                cached_start = Some(sample.start);
            }
            let end = if sample.start == sample.end {
                &self.start
            } else {
                if cached_end != Some(sample.end) {
                    self.end.clear();
                    serde_json::to_writer(&mut self.end, sample.end)?;
                    cached_end = Some(sample.end);
                }
                &self.end
            };
            if index != 0 {
                output.push(b',');
            }
            output.extend_from_slice(b"{\"recordId\":");
            serde_json::to_writer(&mut *output, &sample.record_id)?;
            output.extend_from_slice(b",\"source\":\"yihe-watch-sim\",\"sourceDevice\":");
            output.extend_from_slice(&self.device);
            let heart_rate = sample.metric == "heart_rate";
            if heart_rate {
                output.extend_from_slice(b",\"sourceVersion\":\"2.0.0\",\"metric\":\"heart_rate\",\"unit\":\"count/min\",\"value\":");
            } else {
                output.extend_from_slice(b",\"sourceVersion\":\"2.0.0\",\"metric\":\"steps\",\"unit\":\"count\",\"value\":");
            }
            output.extend_from_slice(number.format(sample.value as i64).as_bytes());
            output.extend_from_slice(b",\"start\":");
            output.extend_from_slice(&self.start);
            output.extend_from_slice(b",\"end\":");
            output.extend_from_slice(end);
            if heart_rate {
                output.extend_from_slice(b",\"semantics\":\"instant\",\"metadata\":{\"simulated\":\"true\",\"sensor\":\"watch-sim:hr\",\"timestampBasis\":\"app-received\"},\"version\":1}");
            } else {
                output.extend_from_slice(b",\"semantics\":\"daily_total\",\"metadata\":{\"simulated\":\"true\",\"counterTimezone\":\"UTC\",\"sensor\":\"watch-sim:pedometer\",\"timestampBasis\":\"app-received\",\"counterBasis\":\"utc-day\"},\"version\":1}");
            }
        }
        output.extend_from_slice(b"]}");
        Ok(())
    }
}

fn canonical<R>(sample: &HealthSampleData<&str, R>) -> bool {
    // Exhaustive patterns make protocol additions a compile error here until the fast path is reviewed.
    let crate::protocol::HealthSampleData {
        record_id: _,
        source: _,
        source_device: _,
        source_version: _,
        metric: _,
        unit: _,
        value: _,
        start: _,
        end: _,
        semantics: _,
        metadata,
        version: _,
    } = sample;
    let crate::protocol::SampleMetadata {
        simulated: _,
        counter_timezone: _,
        sensor: _,
        sensor_timestamp: _,
        timestamp_basis: _,
        counter_basis: _,
    } = metadata;
    if sample.source != "yihe-watch-sim"
        || sample.source_version != "2.0.0"
        || sample.version != 1
        || !sample.value.is_finite()
        || sample.value.fract() != 0.0
        || sample.value.abs() >= 9.0e15
        || metadata.simulated != "true"
        || metadata.sensor_timestamp.is_some()
        || metadata.timestamp_basis.as_deref() != Some("app-received")
    {
        return false;
    }
    match sample.metric.as_ref() {
        "heart_rate" => {
            sample.unit == "count/min"
                && sample.semantics == "instant"
                && metadata.counter_timezone.is_none()
                && metadata.counter_basis.is_none()
                && metadata.sensor.as_deref() == Some("watch-sim:hr")
        }
        "steps" => {
            sample.unit == "count"
                && sample.semantics == "daily_total"
                && metadata.counter_timezone.as_deref() == Some("UTC")
                && metadata.counter_basis.as_deref() == Some("utc-day")
                && metadata.sensor.as_deref() == Some("watch-sim:pedometer")
        }
        _ => false,
    }
}
