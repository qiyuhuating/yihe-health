//! Deterministic simulator A/B: rolling median versus an anchored health baseline.
//! Run: cargo run --locked --example baseline_replay
use watch_sim::physio::{Physiology, Targets};

fn median(values: &[f64]) -> f64 {
    let mut sorted = values.to_vec();
    sorted.sort_by(f64::total_cmp);
    let middle = sorted.len() / 2;
    if sorted.len() % 2 == 0 {
        (sorted[middle - 1] + sorted[middle]) / 2.0
    } else {
        sorted[middle]
    }
}

fn replay() -> Vec<f64> {
    let mut model = Physiology::new(20261108, 116.4, 39.9, 12.0);
    (0..21)
        .map(|day| {
            let target = Targets {
                hr: if day < 7 { 70.0 } else { 112.0 },
                bp: 118.0,
                spo2: 97.0,
                activity: 0.05,
            };
            let mut bpm = 0;
            for _ in 0..30 {
                bpm = model.tick(5.0, target).hr;
            }
            bpm as f64
        })
        .collect()
}

fn compare(days: &[f64]) -> (f64, f64, usize, bool) {
    let anchor = median(&days[..7]);
    let scale = (anchor.abs() * 0.05).max(5.0);
    let rejected = days[7..]
        .iter()
        .filter(|&&bpm| (bpm - anchor).abs() >= 3.5 * scale)
        .count();
    let rolling = median(days);
    let stale = rejected > 0 && days.len() - (7 + rejected) > 7;
    (rolling, anchor, rejected, stale)
}

fn main() {
    let days = replay();
    let (rolling, anchored, rejected, _) = compare(&days);
    // With fourteen sustained high-HR days, the conventional rolling median
    // normalizes the event, while the first-week anchor remains unchanged.
    let early = &days[..11];
    let (_, early_anchor, early_rejected, _) = compare(early);
    let late_stale = days[7..]
        .iter()
        .all(|v| (*v - anchored).abs() >= 3.5 * (anchored.abs() * 0.05).max(5.0));
    println!("seed=20261108 days={} early_anchor={early_anchor:.1} early_rejected={early_rejected} rolling={rolling:.1} anchor={anchored:.1} quarantined={rejected} late_stale={late_stale}", days.len());
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn sustained_excursion_contaminates_rolling_but_not_anchor() {
        let days = replay();
        let (rolling, anchor, rejected, _) = compare(&days);
        assert!(rolling > 95.0, "rolling baseline: {rolling}");
        assert!(anchor < 90.0, "anchored baseline: {anchor}");
        assert!(rejected >= 10, "rejected days: {rejected}");
        let (_, early_anchor, early_rejected, _) = compare(&days[..11]);
        assert!(early_anchor < 90.0 && early_rejected >= 2);
    }
}
