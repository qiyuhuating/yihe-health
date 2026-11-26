//! Reassociation is allowed only for verified nonnegative integers whose
//! entire sum fits the exact f64 integer range. Fractional values, negative
//! zero and large values use the original ordered reduction.

pub(crate) const MAX_EXACT_POINT: f64 = 900_719_925_474.0;

pub(crate) fn exact_point(value: f64) -> bool {
    !value.is_sign_negative() && value <= MAX_EXACT_POINT && value.fract() == 0.0
}

pub(crate) fn reduce(values: &[f64]) -> Option<(f64, f64, f64)> {
    if values.len() < 128 {
        return None;
    }
    #[cfg(target_arch = "x86_64")]
    {
        if std::arch::is_x86_feature_detected!("avx512f") {
            return Some(unsafe { avx512(values) });
        }
        if std::arch::is_x86_feature_detected!("avx2") {
            return Some(unsafe { avx2(values) });
        }
    }
    #[cfg(target_arch = "aarch64")]
    if std::arch::is_aarch64_feature_detected!("neon") {
        return Some(unsafe { neon(values) });
    }
    None
}

fn finish<const N: usize>(
    sums: [f64; N],
    lows: [f64; N],
    highs: [f64; N],
    tail: &[f64],
) -> (f64, f64, f64) {
    let mut total = sums.into_iter().sum::<f64>();
    let mut low = lows.into_iter().fold(f64::INFINITY, f64::min);
    let mut high = highs.into_iter().fold(0.0, f64::max);
    for &v in tail {
        total += v;
        low = low.min(v);
        high = high.max(v);
    }
    (total, low, high)
}

#[cfg(target_arch = "x86_64")]
#[target_feature(enable = "avx2")]
unsafe fn avx2(values: &[f64]) -> (f64, f64, f64) {
    use std::arch::x86_64::*;
    let mut sum = _mm256_setzero_pd();
    let mut low = _mm256_set1_pd(f64::INFINITY);
    let mut high = _mm256_setzero_pd();
    let (chunks, tail) = values.as_chunks::<4>();
    for chunk in chunks {
        let v = unsafe { _mm256_loadu_pd(chunk.as_ptr()) };
        sum = _mm256_add_pd(sum, v);
        low = _mm256_min_pd(low, v);
        high = _mm256_max_pd(high, v);
    }
    let (mut sums, mut lows, mut highs) = ([0.; 4], [0.; 4], [0.; 4]);
    unsafe {
        _mm256_storeu_pd(sums.as_mut_ptr(), sum);
        _mm256_storeu_pd(lows.as_mut_ptr(), low);
        _mm256_storeu_pd(highs.as_mut_ptr(), high);
    }
    finish(sums, lows, highs, tail)
}

#[cfg(target_arch = "x86_64")]
#[target_feature(enable = "avx512f")]
unsafe fn avx512(values: &[f64]) -> (f64, f64, f64) {
    use std::arch::x86_64::*;
    let mut sum = _mm512_setzero_pd();
    let mut low = _mm512_set1_pd(f64::INFINITY);
    let mut high = _mm512_setzero_pd();
    let (chunks, tail) = values.as_chunks::<8>();
    for chunk in chunks {
        let v = unsafe { _mm512_loadu_pd(chunk.as_ptr()) };
        sum = _mm512_add_pd(sum, v);
        low = _mm512_min_pd(low, v);
        high = _mm512_max_pd(high, v);
    }
    let (mut sums, mut lows, mut highs) = ([0.; 8], [0.; 8], [0.; 8]);
    unsafe {
        _mm512_storeu_pd(sums.as_mut_ptr(), sum);
        _mm512_storeu_pd(lows.as_mut_ptr(), low);
        _mm512_storeu_pd(highs.as_mut_ptr(), high);
    }
    finish(sums, lows, highs, tail)
}

#[cfg(target_arch = "aarch64")]
#[target_feature(enable = "neon")]
unsafe fn neon(values: &[f64]) -> (f64, f64, f64) {
    use std::arch::aarch64::*;
    let mut sum = vdupq_n_f64(0.);
    let mut low = vdupq_n_f64(f64::INFINITY);
    let mut high = vdupq_n_f64(0.);
    let (chunks, tail) = values.as_chunks::<2>();
    for chunk in chunks {
        let v = unsafe { vld1q_f64(chunk.as_ptr()) };
        sum = vaddq_f64(sum, v);
        low = vminq_f64(low, v);
        high = vmaxq_f64(high, v);
    }
    let (mut sums, mut lows, mut highs) = ([0.; 2], [0.; 2], [0.; 2]);
    unsafe {
        vst1q_f64(sums.as_mut_ptr(), sum);
        vst1q_f64(lows.as_mut_ptr(), low);
        vst1q_f64(highs.as_mut_ptr(), high);
    }
    finish(sums, lows, highs, tail)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn native_reductions_match_exact_order() {
        for n in [128, 129, 255, 256, 257, 4320, 9999, 10_000] {
            let values: Vec<_> = (0..n)
                .map(|i| ((i * 7919) % 1024) as f64 + MAX_EXACT_POINT - 1024.)
                .collect();
            let expected = (
                values.iter().sum::<f64>(),
                values.iter().copied().fold(f64::INFINITY, f64::min),
                values.iter().copied().fold(0., f64::max),
            );
            if let Some(actual) = reduce(&values) {
                assert_eq!(actual, expected);
            }
            #[cfg(target_arch = "x86_64")]
            {
                if std::arch::is_x86_feature_detected!("avx2") {
                    assert_eq!(unsafe { avx2(&values) }, expected);
                }
                if std::arch::is_x86_feature_detected!("avx512f") {
                    assert_eq!(unsafe { avx512(&values) }, expected);
                }
            }
        }
    }
}
