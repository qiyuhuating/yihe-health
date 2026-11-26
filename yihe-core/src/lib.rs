//! Rebuildable analytics over an authorized, committed MySQL snapshot.
//! This crate does not own persistent data, connections, or authorization.

mod baseline;
mod ffi;
mod simd;

/// Identity of one instantaneous heart-rate series. The caller must resolve and
/// authorize the resident before constructing or querying a projection.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SeriesKey {
    pub resident_id: i64,
    pub source: String,
    pub source_device: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Window {
    /// Inclusive Unix nanoseconds.
    pub from: i64,
    /// Exclusive Unix nanoseconds.
    pub to: i64,
}

#[derive(Debug, Clone, Copy)]
pub struct Point {
    pub at: i64,
    pub value: f64,
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Statistics {
    pub count: usize,
    pub min: f64,
    pub max: f64,
    pub mean: f64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AdmissionError {
    InvalidSnapshot,
    UnsupportedPoint,
    OutOfOrder,
    TooManyPoints,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Miss {
    Identity,
    Revision,
    Window,
}

/// Maximum candidate count accepted by the existing statistics API.
pub const MAX_POINTS: usize = 10_000;

/// Immutable, revision-bound SoA projection. No data is written to disk.
///
/// Admission requires the complete, authorized series for the coverage window,
/// with the original MySQL revision order preserved. The initial fast path
/// accepts only nonnegative finite, instantaneous heart-rate values in
/// nondecreasing timestamp order. Unsupported snapshots remain on the Go path.
pub struct HeartRateSeries {
    key: SeriesKey,
    revision: i64,
    coverage: Window,
    timestamps: Box<[i64]>,
    values: Box<[f64]>,
    exact_integer: bool,
}

impl HeartRateSeries {
    pub fn from_committed(
        key: SeriesKey,
        revision: i64,
        coverage: Window,
        points: &[Point],
    ) -> Result<Self, AdmissionError> {
        if key.resident_id <= 0
            || key.source.is_empty()
            || key.source_device.is_empty()
            || revision <= 0
            || coverage.from >= coverage.to
        {
            return Err(AdmissionError::InvalidSnapshot);
        }
        if points.len() > MAX_POINTS {
            return Err(AdmissionError::TooManyPoints);
        }
        let mut timestamps = Vec::with_capacity(points.len());
        let mut values = Vec::with_capacity(points.len());
        let mut exact_integer = true;
        for p in points {
            if p.at < coverage.from || p.at >= coverage.to || !p.value.is_finite() || p.value < 0.0
            {
                return Err(AdmissionError::UnsupportedPoint);
            }
            if timestamps.last().is_some_and(|&last| p.at < last) {
                return Err(AdmissionError::OutOfOrder);
            }
            exact_integer &= simd::exact_point(p.value);
            timestamps.push(p.at);
            values.push(p.value);
        }
        Ok(Self {
            key,
            revision,
            coverage,
            timestamps: timestamps.into_boxed_slice(),
            values: values.into_boxed_slice(),
            exact_integer,
        })
    }

    /// A caller-supplied, freshly validated MySQL revision is mandatory.
    /// An empty covered range is a successful result, distinct from a miss.
    pub fn query(
        &self,
        key: &SeriesKey,
        committed_revision: i64,
        window: Window,
    ) -> Result<Option<Statistics>, Miss> {
        if &self.key != key {
            return Err(Miss::Identity);
        }
        if committed_revision != self.revision {
            return Err(Miss::Revision);
        }
        if window.from >= window.to
            || window.from < self.coverage.from
            || window.to > self.coverage.to
        {
            return Err(Miss::Window);
        }
        Ok(query_columns(
            &self.timestamps,
            &self.values,
            window,
            self.exact_integer,
        ))
    }

    /// Payload memory only; cache metadata and allocator overhead are excluded.
    pub fn column_bytes(&self) -> usize {
        self.timestamps.len() * size_of::<i64>() + self.values.len() * size_of::<f64>()
    }
}

fn query_columns(
    timestamps: &[i64],
    values: &[f64],
    window: Window,
    exact_integer: bool,
) -> Option<Statistics> {
    let first = timestamps.partition_point(|&at| at < window.from);
    let end = timestamps[first..].partition_point(|&at| at < window.to) + first;
    let values = &values[first..end];
    if exact_integer && let Some((total, min, max)) = simd::reduce(values) {
        return Some(Statistics {
            count: values.len(),
            min,
            max,
            mean: total / values.len() as f64,
        });
    }
    aggregate(values)
}

fn aggregate(values: &[f64]) -> Option<Statistics> {
    let mut low = *values.first()?;
    let mut high = low;
    let mut total = 0.0;
    // Preserve original floating-point addition order and signed-zero extrema.
    for &value in values {
        total += value;
        if value < low {
            low = value;
        }
        if value > high {
            high = value;
        }
    }
    let count = values.len();
    let mean = if total.is_infinite() {
        let mut scaled = 0.0;
        for &value in values {
            scaled += value / high;
        }
        (scaled / count as f64) * high
    } else {
        total / count as f64
    };
    Some(Statistics {
        count,
        min: low,
        max: high,
        mean,
    })
}
