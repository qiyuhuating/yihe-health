#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod display_refresh;

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
#[cfg(windows)]
use std::os::windows::process::CommandExt;
use std::{
    fs::{self, OpenOptions},
    io::{Read, Write},
    net::{SocketAddr, TcpStream},
    path::PathBuf,
    process::{Child, Command, Stdio},
    sync::Mutex,
    thread,
    time::{Duration, Instant},
};
use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};

struct Backend {
    child: Mutex<Option<Child>>,
    origin: String,
    token: String,
}

impl Backend {
    fn stop(&self) {
        if let Ok(mut guard) = self.child.lock() {
            if let Some(mut child) = guard.take() {
                // Ask the authenticated local endpoint to exit before forcing termination.
                if let Ok(addr) = self
                    .origin
                    .trim_start_matches("http://")
                    .parse::<SocketAddr>()
                {
                    if let Ok(mut socket) =
                        TcpStream::connect_timeout(&addr, Duration::from_secs(1))
                    {
                        let _ = socket.set_write_timeout(Some(Duration::from_secs(1)));
                        let _ = write!(socket, "POST /api/v1/desktop/exit HTTP/1.1\r\nHost: {addr}\r\nX-Yihe-Desktop: {}\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", self.token);
                    }
                }
                let until = Instant::now() + Duration::from_secs(3);
                while Instant::now() < until {
                    if matches!(child.try_wait(), Ok(Some(_))) {
                        return;
                    }
                    thread::sleep(Duration::from_millis(50));
                }
                let _ = child.kill();
                let _ = child.wait();
            }
        }
    }
}
impl Drop for Backend {
    fn drop(&mut self) {
        self.stop();
    }
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Account {
    name: String,
    phone: String,
    password: String,
}

#[tauri::command]
async fn setup_admin(
    window: tauri::WebviewWindow,
    state: tauri::State<'_, Backend>,
    account: Account,
) -> Result<serde_json::Value, String> {
    let url = window.url().map_err(|_| "无法验证应用页面")?;
    if window.label() != "main" || url.origin().ascii_serialization() != state.origin {
        return Err("请求未获授权".into());
    }
    let client = reqwest::Client::builder()
        .timeout(Duration::from_secs(10))
        .no_proxy()
        .build()
        .map_err(|_| "无法创建连接")?;
    let response = client
        .post(format!("{}/api/v1/setup", state.origin))
        .header("X-Yihe-Desktop", &state.token)
        .json(&account)
        .send()
        .await
        .map_err(|_| "数据库或照护服务暂不可用")?;
    let status = response.status().as_u16();
    let data: serde_json::Value = response.json().await.map_err(|_| "服务响应无效")?;
    Ok(serde_json::json!({"status": status, "data": data}))
}

// Only HTTPS destinations with a normal DNS name may leave the local desktop shell.
// Actual navigation happens in the system browser, never inside the care WebView.
fn permitted_external_link(raw: &str) -> Result<reqwest::Url, String> {
    let url = reqwest::Url::parse(raw).map_err(|_| "无效的网页地址")?;
    if url.scheme() != "https"
        || !url.username().is_empty()
        || url.password().is_some()
        || url.port().is_some()
    {
        return Err("只允许安全的 HTTPS 网页链接".into());
    }
    let host = url.host_str().ok_or("缺少网页域名")?;
    if host.parse::<std::net::IpAddr>().is_ok()
        || host == "localhost"
        || !host.contains('.')
        || host.ends_with(".local")
        || host.ends_with(".internal")
    {
        return Err("不允许打开本地或 IP 地址链接".into());
    }
    // Reject known advertising and redirect gateways; other external sites
    // still require the user's explicit confirmation in the UI.
    const BLOCKED: &[&str] = &[
        "doubleclick.net",
        "googlesyndication.com",
        "googleadservices.com",
        "adservice.google.com",
        "taboola.com",
        "outbrain.com",
    ];
    if BLOCKED.iter().any(|domain| host == *domain || host.ends_with(&format!(".{domain}"))) {
        return Err("已拦截广告跳转域名".into());
    }
    Ok(url)
}

#[tauri::command]
fn open_external_link(
    window: tauri::WebviewWindow,
    state: tauri::State<'_, Backend>,
    url: String,
) -> Result<(), String> {
    let page = window.url().map_err(|_| "无法验证应用页面")?;
    if window.label() != "main" || page.origin().ascii_serialization() != state.origin {
        return Err("请求未获授权".into());
    }
    let destination = permitted_external_link(&url)?;
    Command::new("explorer.exe")
        .arg(destination.as_str())
        .spawn()
        .map_err(|_| "无法启动系统浏览器".to_string())?;
    Ok(())
}


#[cfg(test)]
mod external_link_tests {
    use super::permitted_external_link;

    #[test]
    fn accepts_confirmed_public_https_destinations() {
        assert!(permitted_external_link("https://www.amap.com/ssr/embed/place?lat=30").is_ok());
        assert!(permitted_external_link("https://example.org/help").is_ok());
    }

    #[test]
    fn rejects_insecure_local_and_advertising_redirects() {
        for url in [
            "http://example.org",
            "javascript:alert(1)",
            "https://user:pass@example.org/",
            "https://127.0.0.1/private",
            "https://localhost/private",
            "https://ad.doubleclick.net/redirect",
            "https://googleadservices.com/pagead/aclk",
        ] {
            assert!(permitted_external_link(url).is_err(), "{url}");
        }
    }
}

fn start_backend(app: &tauri::App) -> Result<Backend, Box<dyn std::error::Error>> {
    let data = if let Some(path) = std::env::var_os("YIHE_USER_DATA") {
        PathBuf::from(path)
    } else if let Some(path) = std::env::var_os("APPDATA") {
        PathBuf::from(path).join("颐和")
    } else {
        app.path().app_data_dir()?
    };
    fs::create_dir_all(data.join("runtime"))?;
    fs::create_dir_all(data.join("images"))?;
    let executable = app.path().resource_dir()?.join("yihe-server.exe");
    let mut file = fs::File::open(&executable)?;
    let mut hasher = Sha256::new();
    let mut buffer = [0u8; 65536];
    loop {
        let count = file.read(&mut buffer)?;
        if count == 0 {
            break;
        }
        hasher.update(&buffer[..count]);
    }
    let actual: String = hasher
        .finalize()
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect();
    if actual != include_str!("../backend-sha256.txt").trim() {
        return Err("后端文件完整性校验失败，请重新安装".into());
    }
    let mut secret = [0u8; 32];
    getrandom::fill(&mut secret).map_err(|_| "无法生成桌面安全令牌")?;
    let token: String = secret.iter().map(|b| format!("{b:02x}")).collect();
    let ready = data
        .join("runtime")
        .join(format!("ready-{}.json", std::process::id()));
    if ready.exists() {
        fs::remove_file(&ready)?;
    }
    let log_path = data.join("server-console.log");
    if fs::metadata(&log_path)
        .map(|m| m.len() > 5 * 1024 * 1024)
        .unwrap_or(false)
    {
        let old = data.join("server-console.previous.log");
        if old.exists() {
            fs::remove_file(&old)?;
        }
        fs::rename(&log_path, old)?;
    }
    let log = OpenOptions::new()
        .create(true)
        .append(true)
        .open(log_path)?;
    let mut command = Command::new(executable);
    command
        .current_dir(&data)
        .stdin(Stdio::null())
        .stdout(Stdio::from(log.try_clone()?))
        .stderr(Stdio::from(log));
    command
        .env("YIHE_DATA_DIR", &data)
        .env("YIHE_READY_FILE", &ready)
        .env("YIHE_PARENT_PID", std::process::id().to_string())
        .env("YIHE_DESKTOP_TOKEN", &token)
        .env("YIHE_PORT", "0");
    for (desktop_key, server_key) in [
        ("YIHE_MYSQL_DSN", "MYSQL_DSN"),
        ("YIHE_PG_ANALYTICS_URL", "PG_ANALYTICS_URL"),
    ] {
        if let Ok(value) = std::env::var(desktop_key) {
            command.env(server_key, value);
        }
    }
    #[cfg(windows)]
    command.creation_flags(0x08000000);
    let child = command.spawn()?;
    let mut backend = Backend {
        child: Mutex::new(Some(child)),
        origin: String::new(),
        token,
    };
    let deadline = Instant::now() + Duration::from_secs(30);
    loop {
        if let Ok(contents) = fs::read(&ready) {
            let value: serde_json::Value = serde_json::from_slice(&contents)?;
            let pid = backend
                .child
                .lock()
                .map_err(|_| "进程锁不可用")?
                .as_ref()
                .ok_or("后端进程不存在")?
                .id();
            let port = value["port"]
                .as_u64()
                .filter(|p| *p > 0 && *p <= 65535)
                .ok_or("无效服务端口")?;
            if value["pid"].as_u64() != Some(pid as u64) {
                return Err("服务进程校验失败".into());
            }
            backend.origin = format!("http://127.0.0.1:{port}");
            let discovery =
                serde_json::json!({"port":port,"pid":pid,"desktopPid":std::process::id()});
            fs::write(
                data.join("runtime/server.tmp"),
                serde_json::to_vec(&discovery)?,
            )?;
            fs::rename(
                data.join("runtime/server.tmp"),
                data.join("runtime/server.json"),
            )?;
            fs::remove_file(&ready)?;
            return Ok(backend);
        }
        if backend
            .child
            .lock()
            .map_err(|_| "进程锁不可用")?
            .as_mut()
            .ok_or("后端进程不存在")?
            .try_wait()?
            .is_some()
        {
            return Err("照护服务启动失败，请检查用户目录中的日志".into());
        }
        if Instant::now() >= deadline {
            return Err("照护服务启动超时".into());
        }
        thread::sleep(Duration::from_millis(50));
    }
}

fn main() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.unminimize();
                let _ = window.set_focus();
            }
        }))
        .invoke_handler(tauri::generate_handler![setup_admin, open_external_link, display_refresh::monitor_refresh_rate])
        .setup(|app| {
            let backend = start_backend(app)?;
            let origin = backend.origin.clone();
            let expected = origin.clone();
            app.manage(backend);
            WebviewWindowBuilder::new(
                app,
                "main",
                WebviewUrl::External(format!("{origin}/index.html").parse()?),
            )
            .title("颐和")
            .inner_size(1360.0, 900.0)
            .min_inner_size(360.0, 560.0)
            .maximized(true)
            .visible(std::env::var_os("YIHE_QA_HEADLESS").is_none())
            .on_navigation(move |url| url.origin().ascii_serialization() == expected)
            .build()?;
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("无法启动颐和，请检查安装文件和用户目录日志");
    app.run(|app, event| {
        if matches!(
            event,
            tauri::RunEvent::ExitRequested { .. } | tauri::RunEvent::Exit
        ) {
            if let Some(backend) = app.try_state::<Backend>() {
                backend.stop();
            }
        }
    });
}
