//! 确定性伪随机数（PCG32）。
//!
//! 旧模拟器用 `Math.random()`，同一场景每次跑出来的数据都不一样，无法写成断言。
//! 这里换成显式种子的 PCG32：`--seed` 固定后整条时间轴可复现，CI 才敢断言数值。

pub struct Pcg32 {
    state: u64,
    inc: u64,
}

const MULTIPLIER: u64 = 6_364_136_223_846_793_005;

impl Pcg32 {
    pub fn new(seed: u64) -> Self {
        let mut rng = Pcg32 {
            state: 0,
            inc: (seed << 1) | 1,
        };
        rng.next_u32();
        rng.state = rng.state.wrapping_add(seed);
        rng.next_u32();
        rng
    }

    #[inline]
    pub fn next_u32(&mut self) -> u32 {
        let old = self.state;
        self.state = old.wrapping_mul(MULTIPLIER).wrapping_add(self.inc);
        let xorshifted = (((old >> 18) ^ old) >> 27) as u32;
        let rot = (old >> 59) as u32;
        xorshifted.rotate_right(rot)
    }

    /// 均匀分布 `[0, 1)`。
    #[inline]
    pub fn next_f64(&mut self) -> f64 {
        (self.next_u32() as f64) / (u32::MAX as f64 + 1.0)
    }

    /// 均匀整数 `[lo, hi]`，两端闭合。
    pub fn range_i64(&mut self, lo: i64, hi: i64) -> i64 {
        if hi <= lo {
            return lo;
        }
        let span = (hi - lo + 1) as u64;
        lo + (self.next_u32() as u64 % span) as i64
    }

    pub fn range_f64(&mut self, lo: f64, hi: f64) -> f64 {
        lo + (hi - lo) * self.next_f64()
    }

    /// 标准正态，Box–Muller。
    pub fn gauss(&mut self) -> f64 {
        let u1 = self.next_f64().max(1e-12);
        let u2 = self.next_f64();
        (-2.0 * u1.ln()).sqrt() * (std::f64::consts::TAU * u2).cos()
    }

    /// `base64url` 字母表里取 `len` 位，用于请求 nonce（后端要求 16–64 位）。
    pub fn nonce(&mut self, len: usize) -> String {
        const ALPHABET: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
        let len = len.clamp(16, 64);
        (0..len)
            .map(|_| ALPHABET[self.next_u32() as usize % ALPHABET.len()] as char)
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::Pcg32;

    #[test]
    fn identical_seeds_yield_identical_sequences() {
        let (mut a, mut b) = (Pcg32::new(20261123), Pcg32::new(20261123));
        for _ in 0..32 {
            assert_eq!(a.next_u32(), b.next_u32());
        }
    }

    #[test]
    fn bounded_ranges_and_nonce_alphabet() {
        let mut rng = Pcg32::new(7);
        assert_eq!(rng.range_i64(10, 10), 10);
        assert_eq!(rng.range_i64(15, 10), 15);
        for _ in 0..100 {
            assert!((-5..=5).contains(&rng.range_i64(-5, 5)));
        }
        assert_eq!(rng.nonce(2).len(), 16);
        assert_eq!(rng.nonce(128).len(), 64);
        assert!(rng
            .nonce(24)
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_'));
    }
}
