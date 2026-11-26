//! Daily-median baseline for a single prevalidated heart-rate series.

const DAYS: usize = 28;
const MINIMUM: usize = 7;
const ALERT_SCORE: f64 = 3.5;

#[repr(C)]
#[derive(Default)]
pub struct BaselineResult {
    pub sample_days: u32,
    pub trusted_days: u32,
    pub rejected_days: u32,
    // 0 ready, 1 insufficient history, 2 stale baseline.
    pub reason: u32,
    // 0 insufficient, 1 moderate, 2 high, 3 guarded.
    pub confidence: u32,
    pub baseline: f64,
    pub scale: f64,
    pub score: f64,
}

fn median(values: &mut [f64]) -> f64 {
    let mid = values.len() / 2;
    let odd = !values.len().is_multiple_of(2);
    let (lower, pivot, _) = values.select_nth_unstable_by(mid, f64::total_cmp);
    if odd {
        *pivot
    } else {
        lower.iter().copied().fold(0., f64::max) / 2. + *pivot / 2.
    }
}

pub fn evaluate(current: f64, days: &[u8], values: &[f64]) -> BaselineResult {
    let mut counts = [0usize; DAYS];
    for &day in days {
        counts[day as usize] += 1;
    }
    let sample_days = counts.iter().filter(|&&n| n != 0).count();
    let mut out = BaselineResult {
        sample_days: sample_days as u32,
        reason: 1,
        ..Default::default()
    };
    if sample_days < MINIMUM {
        return out;
    }

    // One contiguous buffer, no allocation per day. Original order is
    // irrelevant to the median of admitted positive finite measurements.
    let mut offsets = [0usize; DAYS + 1];
    for i in 0..DAYS {
        offsets[i + 1] = offsets[i] + counts[i];
    }
    let mut cursor = offsets;
    let mut ordered = vec![0.; values.len()];
    for (&day, &value) in days.iter().zip(values) {
        let day = day as usize;
        ordered[cursor[day]] = value;
        cursor[day] += 1;
    }
    let mut daily = [0.; DAYS];
    for i in 0..DAYS {
        if counts[i] != 0 {
            daily[i] = median(&mut ordered[offsets[i]..offsets[i + 1]]);
        }
    }
    let mut anchor = [0.; MINIMUM];
    let mut trusted = 0;
    let mut last = 0;
    for (i, &v) in daily.iter().enumerate() {
        if counts[i] != 0 {
            anchor[trusted] = v;
            trusted += 1;
            last = i;
            if trusted == MINIMUM {
                break;
            }
        }
    }
    let center = median(&mut anchor);
    for v in &mut anchor {
        *v = (*v - center).abs();
    }
    let scale = (center.abs() * 0.05)
        .max(5.)
        .max(1.4826 * median(&mut anchor));
    let mut rejected = 0;
    for i in last + 1..DAYS {
        if counts[i] == 0 {
            continue;
        }
        if (daily[i] - center).abs() >= ALERT_SCORE * scale {
            rejected += 1;
        } else {
            trusted += 1;
            last = i;
        }
    }
    out.trusted_days = trusted as u32;
    out.rejected_days = rejected;
    if DAYS - last > 7 {
        out.reason = 2;
        return out;
    }
    out.reason = 0;
    out.confidence = if rejected > 0 {
        3
    } else if trusted >= 14 {
        2
    } else {
        1
    };
    out.baseline = center;
    out.scale = scale;
    out.score = (current - center) / scale;
    out
}
