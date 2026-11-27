//! Append-only, revision-fenced rolling projection of integer heart rates.
//! Unsupported corrections, gaps, or values require an authoritative rebuild.
use crate::{AdmissionError, MAX_POINTS, Miss, Point, SeriesKey, Statistics, Window};

pub struct RollingHeartRate {
    key: SeriesKey,
    revision: i64,
    window: Window,
    timestamps: Box<[i64]>,
    values: Box<[u8]>,
    head: usize,
    len: usize,
    counts: [u16; 256],
    occupied: [u64; 4],
    sum: u64,
    squares: u64,
}

fn bpm(value: f64) -> Result<u8, AdmissionError> {
    if !value.is_finite() || value.is_sign_negative() || value > 250. || value.fract() != 0. {
        return Err(AdmissionError::UnsupportedPoint);
    }
    Ok(value as u8)
}
impl RollingHeartRate {
    /// The complete authorized snapshot must be supplied, ordered by time and
    /// committed revision. Capacity never grows, and values remain exact.
    pub fn from_committed(
        key: SeriesKey,
        revision: i64,
        window: Window,
        points: &[Point],
        capacity: usize,
    ) -> Result<Self, AdmissionError> {
        if capacity == 0 || capacity > MAX_POINTS || points.len() > capacity {
            return Err(AdmissionError::TooManyPoints);
        }
        if key.resident_id <= 0
            || key.source.is_empty()
            || key.source_device.is_empty()
            || revision <= 0
            || window.from >= window.to
        {
            return Err(AdmissionError::InvalidSnapshot);
        }
        let mut state = Self {
            key,
            revision,
            window,
            timestamps: vec![0; capacity].into_boxed_slice(),
            values: vec![0; capacity].into_boxed_slice(),
            head: 0,
            len: 0,
            counts: [0; 256],
            occupied: [0; 4],
            sum: 0,
            squares: 0,
        };
        for p in points {
            if p.at < window.from || p.at >= window.to {
                return Err(AdmissionError::UnsupportedPoint);
            }
            if state.last().is_some_and(|at| p.at < at) {
                return Err(AdmissionError::OutOfOrder);
            }
            state.insert(p.at, bpm(p.value)?);
        }
        Ok(state)
    }
    fn last(&self) -> Option<i64> {
        (self.len != 0).then(|| self.timestamps[(self.head + self.len - 1) % self.values.len()])
    }
    fn insert(&mut self, at: i64, value: u8) {
        let tail = self.head + self.len;
        let tail = if tail >= self.values.len() {
            tail - self.values.len()
        } else {
            tail
        };
        self.timestamps[tail] = at;
        self.values[tail] = value;
        self.len += 1;
        let i = usize::from(value);
        self.counts[i] += 1;
        self.occupied[i / 64] |= 1 << (i % 64);
        let v = u64::from(value);
        self.sum += v;
        self.squares += v * v;
    }
    fn expire(&mut self, from: i64) {
        while self.len != 0 && self.timestamps[self.head] < from {
            let value = self.values[self.head];
            self.head += 1;
            if self.head == self.values.len() {
                self.head = 0;
            }
            self.len -= 1;
            let i = usize::from(value);
            self.counts[i] -= 1;
            if self.counts[i] == 0 {
                self.occupied[i / 64] &= !(1 << (i % 64));
            }
            let v = u64::from(value);
            self.sum -= v;
            self.squares -= v * v;
        }
    }
    /// Exactly one newly committed append. The caller must certify that
    /// newly covered time contains no other previously committed points.
    /// Missing revisions, historical inserts, corrections, deletions and
    /// identity changes must rebuild.
    /// A rejection leaves the entire projection unchanged.
    pub fn append(
        &mut self,
        key: &SeriesKey,
        revision: i64,
        window: Window,
        point: Point,
    ) -> Result<Statistics, AdmissionError> {
        let value = bpm(point.value)?;
        if key != &self.key
            || self.revision.checked_add(1) != Some(revision)
            || window.from < self.window.from
            || window.to < self.window.to
            || window.from >= window.to
        {
            return Err(AdmissionError::InvalidSnapshot);
        }
        if point.at < window.from
            || point.at >= window.to
            || point.at < self.window.to.saturating_sub(1)
        {
            return Err(AdmissionError::OutOfOrder);
        }
        if self.len == self.values.len() && self.timestamps[self.head] >= window.from {
            return Err(AdmissionError::TooManyPoints);
        }
        self.expire(window.from);
        self.insert(point.at, value);
        self.revision = revision;
        self.window = window;
        Ok(self.statistics().expect("just inserted"))
    }
    /// Time-only expiration requires a freshly validated unchanged revision
    /// and proof that newly covered time contains no additional points.
    pub fn advance(&mut self, key: &SeriesKey, revision: i64, window: Window) -> Result<(), Miss> {
        if key != &self.key {
            return Err(Miss::Identity);
        }
        if revision != self.revision {
            return Err(Miss::Revision);
        }
        if window.from < self.window.from || window.to < self.window.to || window.from >= window.to
        {
            return Err(Miss::Window);
        }
        self.expire(window.from);
        self.window = window;
        Ok(())
    }
    pub fn query(
        &self,
        key: &SeriesKey,
        revision: i64,
        window: Window,
    ) -> Result<Option<Statistics>, Miss> {
        if key != &self.key {
            return Err(Miss::Identity);
        }
        if revision != self.revision {
            return Err(Miss::Revision);
        }
        if window != self.window {
            return Err(Miss::Window);
        }
        Ok(self.statistics())
    }
    fn statistics(&self) -> Option<Statistics> {
        if self.len == 0 {
            return None;
        }
        let low = self
            .occupied
            .iter()
            .position(|&w| w != 0)
            .expect("nonempty");
        let high = self
            .occupied
            .iter()
            .rposition(|&w| w != 0)
            .expect("nonempty");
        Some(Statistics {
            count: self.len,
            min: (low * 64 + self.occupied[low].trailing_zeros() as usize) as f64,
            max: (high * 64 + 63 - self.occupied[high].leading_zeros() as usize) as f64,
            mean: self.sum as f64 / self.len as f64,
        })
    }
    /// Exact integer second moment; population variance uses the integer
    /// numerator to avoid cancellation on nearly constant readings.
    pub fn population_variance(
        &self,
        key: &SeriesKey,
        revision: i64,
        window: Window,
    ) -> Result<Option<f64>, Miss> {
        if self.query(key, revision, window)?.is_none() {
            return Ok(None);
        }
        let n = self.len as u64;
        Ok(Some(
            (n * self.squares - self.sum * self.sum) as f64 / (n * n) as f64,
        ))
    }
    pub fn payload_bytes(&self) -> usize {
        self.timestamps.len() * 8
            + self.values.len()
            + size_of_val(&self.counts)
            + size_of_val(&self.occupied)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn key() -> SeriesKey {
        SeriesKey {
            resident_id: 1,
            source: "watch".into(),
            source_device: "fixture".into(),
        }
    }
    #[test]
    fn append_expiry_and_rebuild_match() {
        let key = key();
        let mut window = Window { from: 0, to: 1 };
        let mut state = RollingHeartRate::from_committed(key.clone(), 1, window, &[], 64).unwrap();
        let mut points = Vec::new();
        for i in 1..=1000 {
            let point = Point {
                at: i,
                value: ((i * 31) % 251) as f64,
            };
            window = Window {
                from: (i - 63).max(0),
                to: i + 1,
            };
            state.append(&key, i + 1, window, point).unwrap();
            points.push(point);
            let eligible: Vec<_> = points
                .iter()
                .copied()
                .filter(|p| p.at >= window.from)
                .collect();
            let reference =
                crate::HeartRateSeries::from_committed(key.clone(), i + 1, window, &eligible)
                    .unwrap();
            assert_eq!(
                state.query(&key, i + 1, window),
                reference.query(&key, i + 1, window)
            );
            let mean = eligible.iter().map(|p| p.value).sum::<f64>() / eligible.len() as f64;
            let variance = eligible
                .iter()
                .map(|p| (p.value - mean).powi(2))
                .sum::<f64>()
                / eligible.len() as f64;
            assert!(
                (state
                    .population_variance(&key, i + 1, window)
                    .unwrap()
                    .unwrap()
                    - variance)
                    .abs()
                    < 1e-9
            );
        }
        let before = state.query(&key, 1001, window);
        assert!(
            state
                .append(
                    &key,
                    1003,
                    window,
                    Point {
                        at: 1000,
                        value: 1.
                    }
                )
                .is_err()
        );
        assert!(
            state
                .append(
                    &key,
                    1002,
                    window,
                    Point {
                        at: 1000,
                        value: 1.5
                    }
                )
                .is_err()
        );
        assert!(
            state
                .append(&key, 1002, window, Point { at: 999, value: 1. })
                .is_err()
        );
        assert_eq!(state.query(&key, 1001, window), before);
        let next = Window {
            from: 1001,
            to: 1002,
        };
        state.advance(&key, 1001, next).unwrap();
        assert_eq!(state.query(&key, 1001, next), Ok(None));
        assert_eq!(state.query(&key, 1000, next), Err(Miss::Revision));
    }
    #[test]
    fn capacity_and_same_timestamp() {
        let key = key();
        let window = Window { from: 0, to: 2 };
        let mut state = RollingHeartRate::from_committed(
            key.clone(),
            1,
            window,
            &[Point { at: 1, value: 0. }],
            2,
        )
        .unwrap();
        state
            .append(&key, 2, window, Point { at: 1, value: 250. })
            .unwrap();
        assert_eq!(
            state.append(&key, 3, window, Point { at: 1, value: 50. }),
            Err(AdmissionError::TooManyPoints)
        );
        assert_eq!(state.query(&key, 2, window).unwrap().unwrap().mean, 125.);
    }
}
