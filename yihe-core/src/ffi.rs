use crate::baseline::{self, BaselineResult};
use crate::{MAX_POINTS, Window, query_columns};

/// Daily baseline over caller-owned, already filtered observations.
/// # Safety
/// For nonzero count, both arrays must be valid for count elements; day
/// indices must be 0..28 and all values finite, strictly positive and at most
/// 1e6. Output must be aligned, writable, and not alias input arrays.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn yihe_core_baseline_v1(
    current: f64,
    days: *const u8,
    values: *const f64,
    count: usize,
    output: *mut BaselineResult,
) -> i32 {
    if count == 0 || count > 5000 || days.is_null() || values.is_null() || output.is_null() {
        return -1;
    }
    let days = unsafe { std::slice::from_raw_parts(days, count) };
    let values = unsafe { std::slice::from_raw_parts(values, count) };
    unsafe {
        output.write(baseline::evaluate(current, days, values));
    }
    1
}

#[repr(C)]
pub struct QueryResult {
    count: u64,
    min: f64,
    max: f64,
    mean: f64,
}

/// Synchronous read-only query over caller-owned columns. Returns 1 for a
/// nonempty result, 0 for empty, and -1 for invalid arguments.
///
/// # Safety
/// For nonzero count, both pointers must address aligned, initialized,
/// immutable arrays of count elements for this call. Timestamps must be
/// sorted, values finite and nonnegative. exact_integer may be 1 only when
/// every value is a nonnegative exact integer at most 900719925474, excluding
/// negative zero. Output must be valid, aligned,
/// writable and disjoint from both input arrays. No pointer is retained.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn yihe_core_query_v1(
    timestamps: *const i64,
    values: *const f64,
    count: usize,
    from: i64,
    to: i64,
    exact_integer: u8,
    output: *mut QueryResult,
) -> i32 {
    if count > MAX_POINTS || from >= to || output.is_null() {
        return -1;
    }
    if count == 0 {
        return 0;
    }
    if timestamps.is_null() || values.is_null() {
        return -1;
    }
    let timestamps = unsafe { std::slice::from_raw_parts(timestamps, count) };
    let values = unsafe { std::slice::from_raw_parts(values, count) };
    match query_columns(timestamps, values, Window { from, to }, exact_integer == 1) {
        None => 0,
        Some(s) => {
            unsafe {
                output.write(QueryResult {
                    count: s.count as u64,
                    min: s.min,
                    max: s.max,
                    mean: s.mean,
                });
            }
            1
        }
    }
}
