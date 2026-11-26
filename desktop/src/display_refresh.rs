//! Reads the current Windows display mode of the monitor hosting the main
//! window. This is the OS refresh rate, not the WebView rendering frame rate.
use crate::Backend;
#[cfg(windows)]
use std::mem;
use tauri::WebviewWindow;
#[cfg(windows)]
use windows_sys::Win32::Graphics::Gdi::{
    DEVMODEW, ENUM_CURRENT_SETTINGS, EnumDisplaySettingsW, GetMonitorInfoW,
    MONITORINFO, MONITORINFOEXW, MONITOR_DEFAULTTONEAREST, MonitorFromWindow,
};

#[cfg(windows)]
#[tauri::command]
pub async fn monitor_refresh_rate(
    window: WebviewWindow,
    state: tauri::State<'_, Backend>,
) -> Result<u32, String> {
    let url = window.url().map_err(|_| "无法验证应用页面")?;
    if window.label() != "main" || url.origin().ascii_serialization() != state.origin {
        return Err("请求未获授权".into());
    }
    let hwnd = window.hwnd().map_err(|_| "无法读取窗口句柄")?;
    // The Tauri HWND wrapper exposes the underlying Win32 handle as .0.
    // SAFETY: hwnd belongs to this live WebviewWindow. Both Win32 functions
    // only read the current monitor mode; the structures are correctly sized.
    let hz = unsafe {
        let monitor = MonitorFromWindow(hwnd.0, MONITOR_DEFAULTTONEAREST);
        if monitor.is_null() {
            return Err("无法定位当前显示器".into());
        }
        let mut info: MONITORINFOEXW = mem::zeroed();
        info.monitorInfo.cbSize = mem::size_of::<MONITORINFOEXW>() as u32;
        if GetMonitorInfoW(monitor, &mut info as *mut _ as *mut MONITORINFO) == 0 {
            return Err("无法读取显示器信息".into());
        }
        let mut mode: DEVMODEW = mem::zeroed();
        mode.dmSize = mem::size_of::<DEVMODEW>() as u16;
        if EnumDisplaySettingsW(info.szDevice.as_ptr(), ENUM_CURRENT_SETTINGS, &mut mode) == 0 {
            return Err("无法读取当前显示模式".into());
        }
        mode.dmDisplayFrequency
    };
    if hz <= 1 {
        return Err("系统未报告有效刷新率".into());
    }
    Ok(hz)
}

#[cfg(not(windows))]
#[tauri::command]
pub async fn monitor_refresh_rate(
    _window: WebviewWindow,
    _state: tauri::State<'_, Backend>,
) -> Result<u32, String> {
    Err("当前系统没有可用的原生刷新率查询".into())
}
