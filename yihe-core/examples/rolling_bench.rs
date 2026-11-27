//! Same deterministic integer samples and windows as BenchmarkNextGoWindow.
use std::{hint::black_box, time::Instant};
use yihe_core::{HeartRateSeries, Point, SeriesKey, Window, rolling::RollingHeartRate};
const EVENTS: usize = 4096;
fn key() -> SeriesKey {
    SeriesKey {
        resident_id: 1,
        source: "watch".into(),
        source_device: "fixture".into(),
    }
}
fn main() {
    let rounds: usize = std::env::args()
        .nth(1)
        .map(|s| s.parse().expect("rounds"))
        .unwrap_or(3);
    assert!(rounds > 0);
    for width in [64, 256, 1024, 4096] {
        let points: Vec<_> = (1..=width + EVENTS)
            .map(|i| Point {
                at: i as i64,
                value: ((i * 31) % 251) as f64,
            })
            .collect();
        let key = key();
        let full = HeartRateSeries::from_committed(
            key.clone(),
            1,
            Window {
                from: 1,
                to: points.len() as i64 + 1,
            },
            &points,
        )
        .unwrap();
        let mut rolling_ns = 0u128;
        let mut snapshot_ns = 0u128;
        let mut rolling_checksum = 0.;
        let mut snapshot_checksum = 0.;
        for _ in 0..rounds {
            let mut state = RollingHeartRate::from_committed(
                key.clone(),
                width as i64,
                Window {
                    from: 1,
                    to: width as i64 + 1,
                },
                &points[..width],
                width,
            )
            .unwrap();
            let start = Instant::now();
            for (i, &point) in points.iter().enumerate().skip(width) {
                let window = Window {
                    from: (i - width + 2) as i64,
                    to: i as i64 + 2,
                };
                let revision = i as i64 + 1;
                let s = black_box(
                    state
                        .append(black_box(&key), revision, window, black_box(point))
                        .unwrap(),
                );
                rolling_checksum += s.count as f64 + s.min + s.max + s.mean;
            }
            rolling_ns += start.elapsed().as_nanos();
            let start = Instant::now();
            for i in width..points.len() {
                let window = Window {
                    from: (i - width + 2) as i64,
                    to: i as i64 + 2,
                };
                let s = black_box(
                    full.query(black_box(&key), 1, black_box(window))
                        .unwrap()
                        .unwrap(),
                );
                snapshot_checksum += s.count as f64 + s.min + s.max + s.mean;
            }
            snapshot_ns += start.elapsed().as_nanos();
        }
        assert_eq!(rolling_checksum, snapshot_checksum);
        let n = (rounds * EVENTS) as f64;
        println!(
            "width={width} rolling_ns/event={:.3} snapshot_ns/event={:.3} checksum={rolling_checksum:.6}",
            rolling_ns as f64 / n,
            snapshot_ns as f64 / n
        );
    }
}
