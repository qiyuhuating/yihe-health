//! 桌面端端口自动发现。
//!
//! 行为与旧版保持一致（CI 里那三个用例依赖它）：记录有效就用记录里的端口；
//! 记录失效必须**明确报错并且一个请求都不发**，绝不能退回默认端口打给别人。

use std::path::{Path, PathBuf};

pub const DISCOVERY_ERROR: &str = "颐和端口记录已失效，请先启动桌面软件或使用 --server 指定地址";

const MAX_RECORD_BYTES: u64 = 1024;

#[derive(Debug, Clone, serde::Deserialize)]
pub struct ServerRecord {
    pub port: u16,
    pub pid: u32,
    #[serde(rename = "desktopPid")]
    pub desktop_pid: u32,
}

/// 解析出的服务地址，例如 `http://127.0.0.1:8795`。
#[derive(Debug)]
pub struct Discovery {
    pub base_url: String,
    pub record_path: PathBuf,
}

/// 记录文件所在目录，优先 `YIHE_USER_DATA`，否则 `%APPDATA%\颐和`。
pub fn data_dir() -> Option<PathBuf> {
    if let Ok(dir) = std::env::var("YIHE_USER_DATA") {
        if !dir.trim().is_empty() {
            return Some(PathBuf::from(dir));
        }
    }
    let appdata = std::env::var("APPDATA").ok()?;
    Some(Path::new(&appdata).join("颐和"))
}

pub fn record_path() -> Option<PathBuf> {
    Some(data_dir()?.join("runtime").join("server.json"))
}

/// 尝试发现正在运行的桌面端。
///
/// 返回 `Err(DISCOVERY_ERROR)` 表示存在记录但不可信——调用方应当直接退出，
/// 而不是回落到默认地址。
pub fn discover() -> Result<Discovery, String> {
    let path = record_path().ok_or_else(|| DISCOVERY_ERROR.to_string())?;
    if !path.exists() {
        return Err(DISCOVERY_ERROR.to_string());
    }
    let meta = std::fs::metadata(&path).map_err(|_| DISCOVERY_ERROR.to_string())?;
    if meta.len() > MAX_RECORD_BYTES {
        return Err(DISCOVERY_ERROR.to_string());
    }
    let raw = std::fs::read_to_string(&path).map_err(|_| DISCOVERY_ERROR.to_string())?;
    let record: ServerRecord =
        serde_json::from_str(&raw).map_err(|_| DISCOVERY_ERROR.to_string())?;
    if record.port == 0 || record.pid == 0 || record.desktop_pid == 0 {
        return Err(DISCOVERY_ERROR.to_string());
    }
    if !process_alive(record.pid) || !process_alive(record.desktop_pid) {
        return Err(DISCOVERY_ERROR.to_string());
    }
    Ok(Discovery {
        base_url: format!("http://127.0.0.1:{}", record.port),
        record_path: path,
    })
}

/// 进程是否仍在运行。旧版用 `process.kill(pid, 0)`，这里等价地做一次
/// 无副作用的句柄获取。
#[cfg(windows)]
fn process_alive(pid: u32) -> bool {
    use std::os::raw::c_void;
    unsafe extern "system" {
        fn OpenProcess(desired_access: u32, inherit_handle: i32, process_id: u32) -> *mut c_void;
        fn CloseHandle(handle: *mut c_void) -> i32;
    }
    const PROCESS_QUERY_LIMITED_INFORMATION: u32 = 0x1000;
    unsafe {
        let handle = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if handle.is_null() {
            return false;
        }
        CloseHandle(handle);
        true
    }
}

#[cfg(not(windows))]
fn process_alive(pid: u32) -> bool {
    Path::new(&format!("/proc/{pid}")).exists()
}
