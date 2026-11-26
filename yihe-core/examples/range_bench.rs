use std::{hint::black_box, time::Instant};
use yihe_core::{HeartRateSeries, Point, SeriesKey, Window};

fn main() {
    let key = SeriesKey {
        resident_id: 1,
        source: "watch".into(),
        source_device: "synthetic".into(),
    };
    let points: Vec<_> = (0..4320)
        .map(|i| Point {
            at: i * 60_000_000_000,
            value: (60 + i % 31) as f64,
        })
        .collect();
    let coverage = Window {
        from: 0,
        to: 4320 * 60_000_000_000,
    };
    let series = HeartRateSeries::from_committed(key.clone(), 1, coverage, &points).unwrap();
    const N: usize = 100_000;
    for width in [60, 1440, 4320] {
        let query = |i: usize| {
            let start = ((i * 7919) % (4321 - width)) as i64 * 60_000_000_000;
            Window {
                from: start,
                to: start + width as i64 * 60_000_000_000,
            }
        };
        let begin = Instant::now();
        for i in 0..N {
            let w = black_box(query(i));
            let (mut count, mut low, mut high, mut total) =
                (0, f64::INFINITY, f64::NEG_INFINITY, 0.0);
            for p in black_box(&points) {
                if p.at >= w.from && p.at < w.to {
                    count += 1;
                    if p.value < low {
                        low = p.value;
                    }
                    if p.value > high {
                        high = p.value;
                    }
                    total += p.value;
                }
            }
            black_box((count, low, high, total / count as f64));
        }
        let scan = begin.elapsed().as_secs_f64() * 1e9 / N as f64;
        let begin = Instant::now();
        for i in 0..N {
            black_box(
                series
                    .query(black_box(&key), black_box(1), black_box(query(i)))
                    .unwrap(),
            );
        }
        let indexed = begin.elapsed().as_secs_f64() * 1e9 / N as f64;
        println!(
            "window={width} points scan={scan:.2} ns indexed={indexed:.2} ns speedup={:.2}x",
            scan / indexed
        );
    }
}
