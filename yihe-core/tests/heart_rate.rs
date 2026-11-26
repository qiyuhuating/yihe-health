use yihe_core::{AdmissionError, HeartRateSeries, Miss, Point, SeriesKey, Window};

fn key() -> SeriesKey {
    SeriesKey {
        resident_id: 42,
        source: "watch".into(),
        source_device: "synthetic".into(),
    }
}

fn window(from: i64, to: i64) -> Window {
    Window { from, to }
}

#[test]
fn windows_and_revision_are_exact() {
    let points: Vec<_> = (0..4320)
        .map(|i| Point {
            at: i * 60,
            value: (60 + i % 31) as f64,
        })
        .collect();
    let coverage = window(0, 4320 * 60);
    let series = HeartRateSeries::from_committed(key(), 7, coverage, &points).unwrap();
    for i in 0..200 {
        let from = i * 997;
        let to = from + 3600;
        let expected: Vec<_> = points
            .iter()
            .filter(|p| p.at >= from && p.at < to)
            .map(|p| p.value)
            .collect();
        let got = series.query(&key(), 7, window(from, to)).unwrap().unwrap();
        assert_eq!(got.count, expected.len());
        assert_eq!(got.min, expected.iter().copied().reduce(f64::min).unwrap());
        assert_eq!(got.max, expected.iter().copied().reduce(f64::max).unwrap());
        assert_eq!(
            got.mean.to_bits(),
            (expected.iter().sum::<f64>() / expected.len() as f64).to_bits()
        );
    }
    assert_eq!(series.query(&key(), 8, coverage), Err(Miss::Revision));
    assert_eq!(
        series.query(
            &SeriesKey {
                resident_id: 43,
                ..key()
            },
            7,
            coverage
        ),
        Err(Miss::Identity)
    );
    assert_eq!(
        series.query(
            &SeriesKey {
                source_device: "other".into(),
                ..key()
            },
            7,
            coverage
        ),
        Err(Miss::Identity)
    );
    assert_eq!(series.query(&key(), 7, window(-1, 1)), Err(Miss::Window));
    assert_eq!(series.query(&key(), 7, window(1, 59)), Ok(None));
    assert_eq!(series.column_bytes(), points.len() * 16);
}

#[test]
fn admission_rejects_unsupported_data() {
    let point = Point { at: 4, value: 70.0 };
    assert!(matches!(
        HeartRateSeries::from_committed(key(), 0, window(0, 10), &[point]),
        Err(AdmissionError::InvalidSnapshot)
    ));
    assert!(matches!(
        HeartRateSeries::from_committed(
            key(),
            1,
            window(0, 10),
            &[point, Point { at: 3, ..point }]
        ),
        Err(AdmissionError::OutOfOrder)
    ));
    assert!(matches!(
        HeartRateSeries::from_committed(
            key(),
            1,
            window(0, 10),
            &[Point {
                value: f64::NAN,
                ..point
            }]
        ),
        Err(AdmissionError::UnsupportedPoint)
    ));
    assert!(matches!(
        HeartRateSeries::from_committed(key(), 1, window(0, 10), &[Point { at: 10, ..point }]),
        Err(AdmissionError::UnsupportedPoint)
    ));
}

#[test]
fn duplicate_times_signed_zero_and_overflow_preserve_go_arithmetic() {
    let points = [
        Point { at: 1, value: -0.0 },
        Point { at: 1, value: 0.0 },
        Point {
            at: 2,
            value: f64::MAX,
        },
        Point {
            at: 3,
            value: f64::MAX,
        },
    ];
    let s = HeartRateSeries::from_committed(key(), 1, window(0, 4), &points).unwrap();
    let zero = s.query(&key(), 1, window(1, 2)).unwrap().unwrap();
    assert_eq!(zero.count, 2);
    assert_eq!(zero.min.to_bits(), (-0.0f64).to_bits());
    assert_eq!(zero.max.to_bits(), (-0.0f64).to_bits());
    let huge = s.query(&key(), 1, window(2, 4)).unwrap().unwrap();
    assert_eq!(huge.mean, f64::MAX);
}
