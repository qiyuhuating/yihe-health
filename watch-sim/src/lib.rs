//! 颐和手表模拟器 2.0 —— 核心库。
//!
//! 分层：协议（protocol）→ 模型（physio/scenario）→ 传输（transport）→ 会话（device）。
//! 通过已有设备 HTTP 接口写入 preview 后端。

pub mod device;
pub mod discovery;
pub mod encoding;
pub mod physio;
pub mod protocol;
pub mod queue;
pub mod rng;
pub mod scenario;
pub mod transport;

/// 后端成功包络码（`response.CodeOK`）。
pub const CODE_OK: &str = "1111";

/// 与后端 `tsLayout` 对齐的墙上时间格式：yyyy-MM-dd HH:mm:ss。
pub fn wall_clock(ts: std::time::SystemTime) -> String {
    let secs = ts
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0);
    format_local(secs)
}

/// 把 `yyyy-MM-dd HH:mm:ss`（本地时区）解析成 Unix 秒。
///
/// 后端心跳/登录返回的 `serverTime` 就是这个格式（`tsLayout`）。解析它是为了
/// 算出「本机时钟比服务端快/慢多少」——签名窗口只有 ±5 分钟，超了就 401，
/// 而 401 的文案看起来像密钥错，不先做偏移预检会把排查方向带偏。
pub fn parse_local(text: &str) -> Option<i64> {
    if text.len() != 19 || !text.is_ascii() {
        return None;
    }
    let b = text.as_bytes();
    if b[4] != b'-' || b[7] != b'-' || b[10] != b' ' || b[13] != b':' || b[16] != b':' {
        return None;
    }
    let seg = |from: usize, to: usize| text[from..to].parse::<i64>().ok();
    let (y, mo, d) = (seg(0, 4)?, seg(5, 7)?, seg(8, 10)?);
    let (h, mi, s) = (seg(11, 13)?, seg(14, 16)?, seg(17, 19)?);
    if !(1..=12).contains(&mo) || !(1..=31).contains(&d) || h > 23 || mi > 59 || s > 59 {
        return None;
    }
    let days = days_from_civil(y, mo, d);
    Some(days * 86_400 + h * 3_600 + mi * 60 + s - local_offset_secs())
}

/// `civil_from_unix` 的逆运算（Howard Hinnant 的 days_from_civil）。
fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = y - era * 400;
    let mp = (m + 9) % 12;
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe - 719_468
}

/// 以本地时区把 Unix 秒格式化成 `yyyy-MM-dd HH:mm:ss`。
///
/// 后端按服务器墙上时间解析 `beTime`，并对 "晚于服务器 5 分钟" 直接 400；
/// 因此这里必须用本地时区，不能用 UTC。
///
/// 每秒最多被调用几千次，而 `format_local_uncached` 要查一次时区并拼字符串
/// （微基准 ~317 ns）。按秒缓存后，同一秒内的重复调用只剩一次 clone。
pub fn format_local(unix_secs: i64) -> String {
    use std::cell::RefCell;
    thread_local! {
        static CACHE: RefCell<(i64, String)> = const { RefCell::new((i64::MIN, String::new())) };
    }
    CACHE.with(|cell| {
        let mut slot = cell.borrow_mut();
        if slot.0 != unix_secs {
            *slot = (unix_secs, format_local_uncached(unix_secs));
        }
        slot.1.clone()
    })
}

fn format_local_uncached(unix_secs: i64) -> String {
    let (y, mo, d, h, mi, s) = civil_from_unix(unix_secs + local_offset_secs());
    format!("{y:04}-{mo:02}-{d:02} {h:02}:{mi:02}:{s:02}")
}

/// 本地时区相对 UTC 的偏移秒数（含夏令时，按当前时刻估算）。
fn local_offset_secs() -> i64 {
    #[cfg(windows)]
    {
        // 用 GetTimeZoneInformation 取标准/夏令偏移，避免引入 chrono。
        use std::os::raw::c_void;
        #[repr(C)]
        struct SystemTime {
            year: u16,
            month: u16,
            day_of_week: u16,
            day: u16,
            hour: u16,
            minute: u16,
            second: u16,
            milliseconds: u16,
        }
        #[repr(C)]
        struct TimeZoneInformation {
            bias: i32,
            standard_name: [u16; 32],
            standard_date: SystemTime,
            standard_bias: i32,
            daylight_name: [u16; 32],
            daylight_date: SystemTime,
            daylight_bias: i32,
        }
        unsafe extern "system" {
            fn GetTimeZoneInformation(info: *mut c_void) -> u32;
        }
        let mut info: TimeZoneInformation = unsafe { std::mem::zeroed() };
        let kind = unsafe { GetTimeZoneInformation(&mut info as *mut _ as *mut c_void) };
        let mut minutes = info.bias + info.standard_bias;
        if kind == 2 {
            minutes += info.daylight_bias;
        }
        -(minutes as i64) * 60
    }
    #[cfg(not(windows))]
    {
        // 非 Windows 走 TZ 环境变量；模拟器主要部署在 Windows 桌面端。
        if let Ok(tz) = std::env::var("TZ_OFFSET_MINUTES") {
            if let Ok(v) = tz.parse::<i64>() {
                return v * 60;
            }
        }
        let total = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs() as i64)
            .unwrap_or(0);
        // 无时区库时的保守推断：中国区固定 +8。
        let _ = total;
        8 * 3600
    }
}

/// 由 Unix 秒求公历年月日时分秒（Howard Hinnant 的 civil_from_days 算法）。
fn civil_from_unix(unix_secs: i64) -> (i32, u32, u32, u32, u32, u32) {
    let days = unix_secs.div_euclid(86_400);
    let secs_of_day = unix_secs.rem_euclid(86_400);
    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    let year = (y + if m <= 2 { 1 } else { 0 }) as i32;
    let h = (secs_of_day / 3600) as u32;
    let mi = ((secs_of_day % 3600) / 60) as u32;
    let s = (secs_of_day % 60) as u32;
    (year, m, d, h, mi, s)
}

/// 当前 Unix 秒。
pub fn now_unix() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// 健康样本使用 UTC，避免把本地墙上时间当作 UTC 写入。
pub fn format_utc(unix_secs: i64) -> String {
    let (y, mo, d, h, mi, s) = civil_from_unix(unix_secs);
    format!("{y:04}-{mo:02}-{d:02}T{h:02}:{mi:02}:{s:02}Z")
}
