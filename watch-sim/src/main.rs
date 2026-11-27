mod local;
use serde::Deserialize;
use std::{
    collections::VecDeque,
    path::PathBuf,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
    time::Duration,
};
use watch_sim::{
    device::DeviceSession,
    discovery,
    physio::Physiology,
    protocol::{Credentials, HealthSample, Platform, ReportPayload},
    rng::Pcg32,
    scenario::{Scenario, Timeline},
    transport::Client,
};

#[derive(Deserialize)]
#[serde(default, deny_unknown_fields)]
struct Config {
    server: Option<String>,
    device_no: String,
    secret: String,
    interval: u64,
    scenario: String,
    app_version: String,
    longitude: f64,
    latitude: f64,
    seed: u64,
    sign: bool,
    harmonyos: bool,
    allow_sos: bool,
    health_samples: bool,
    ca_file: Option<PathBuf>,
    local: Option<PathBuf>,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            server: None,
            device_no: "WATCH0001".into(),
            secret: String::new(),
            interval: 3,
            scenario: "demo".into(),
            app_version: "2.0.0-sim".into(),
            longitude: 113.0625,
            latitude: 28.1975,
            seed: 1,
            sign: false,
            harmonyos: false,
            allow_sos: false,
            health_samples: false,
            ca_file: None,
            local: None,
        }
    }
}

fn run() -> Result<(), String> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.iter().any(|a| a == "--help") {
        println!("颐和手表模拟器 2.0\n--config <JSON> --server <URL> --device-no <编号> --secret <密钥>\n--bind <绑定码> --interval <秒> --scenario <demo|normal|rest|tachycardia|hypertension|fall|sos|low-battery|night-sleep>\n--app-version <版本> --longitude <经度> --latitude <纬度> --seed <整数>\n--sign --harmonyos --health-samples --ca-file <PEM> --allow-sos --once\n--local <目录> 无需网络与密钥，在本地保存数据和查看页面\n密钥也可通过 WATCH_DEVICE_SECRET 提供。默认发送完整 report，间隔 3 秒。");
        return Ok(());
    }
    let mut config_path = None;
    let mut i = 0;
    while i < args.len() {
        if args[i] == "--config" {
            i += 1;
            config_path = Some(args.get(i).ok_or("--config 缺少文件路径")?.clone());
        }
        i += 1;
    }
    let mut c: Config = match config_path {
        Some(p) => {
            serde_json::from_slice(&std::fs::read(p).map_err(|e| format!("读取配置失败：{e}"))?)
                .map_err(|e| format!("配置无效：{e}"))?
        }
        None => Config::default(),
    };
    if c.secret.is_empty() {
        c.secret = std::env::var("WATCH_DEVICE_SECRET").unwrap_or_default();
    }
    let mut once = false;
    let mut bind = None;
    let mut i = 0;
    while i < args.len() {
        let key = &args[i];
        match key.as_str() {
            "--once" => once = true,
            "--sign" => c.sign = true,
            "--health-samples" => c.health_samples = true,
            "--harmonyos" => c.harmonyos = true,
            "--allow-sos" => c.allow_sos = true,
            _ => {
                i += 1;
                let value = args.get(i).ok_or_else(|| format!("{key} 缺少参数"))?;
                match key.as_str() {
                    "--config" => {}
                    "--server" => c.server = Some(value.clone()),
                    "--device-no" => c.device_no = value.clone(),
                    "--secret" => c.secret = value.clone(),
                    "--bind" => bind = Some(value.clone()),
                    "--scenario" => c.scenario = value.clone(),
                    "--app-version" => c.app_version = value.clone(),
                    "--interval" => c.interval = value.parse().map_err(|_| "间隔必须为整数")?,
                    "--longitude" => c.longitude = value.parse().map_err(|_| "经度无效")?,
                    "--latitude" => c.latitude = value.parse().map_err(|_| "纬度无效")?,
                    "--seed" => c.seed = value.parse().map_err(|_| "随机种子无效")?,
                    "--ca-file" => c.ca_file = Some(value.into()),
                    "--local" => c.local = Some(value.into()),
                    _ => return Err(format!("未知选项：{key}")),
                }
            }
        }
        i += 1;
    }
    if !(1..=3600).contains(&c.interval) {
        return Err("上报间隔应为 1–3600 秒".into());
    }
    if !c.longitude.is_finite()
        || !c.latitude.is_finite()
        || c.longitude.abs() > 180.0
        || c.latitude.abs() > 90.0
    {
        return Err("起点经纬度超出范围".into());
    }
    if c.app_version.is_empty() || c.app_version.chars().count() > 20 {
        return Err("应用版本应为 1–20 个字符".into());
    }
    if c.scenario != "demo" && Scenario::parse(&c.scenario).is_none() {
        return Err("未知场景".into());
    }
    if c.local.is_none() && matches!(Scenario::parse(&c.scenario), Some(Scenario::SignalLoss)) {
        return Err("此场景尚未接入业务流程".into());
    }
    if c.local.is_none() && matches!(c.scenario.as_str(), "sos" | "fall") && !c.allow_sos {
        return Err("跌倒和 SOS 场景需要 --allow-sos".into());
    }
    let cred = Credentials::new(
        c.device_no,
        if c.local.is_some() {
            String::new()
        } else {
            c.secret
        },
    );
    if cred.device_no.is_empty() || cred.device_no.chars().count() > 32 {
        return Err("设备号必须为 1–32 个字符".into());
    }
    if c.local.is_none() {
        cred.validate()?;
    }
    let mut local = c.local.as_deref().map(local::LocalRun::new).transpose()?;
    let stopped = Arc::new(AtomicBool::new(false));
    let signal = stopped.clone();
    ctrlc::set_handler(move || {
        signal.store(true, Ordering::Relaxed);
    })
    .map_err(|e| format!("安装退出处理失败：{e}"))?;
    let mut session = if local.is_some() {
        None
    } else {
        let server = match c.server {
            Some(s) => s,
            None => match discovery::record_path() {
                Some(p) if p.exists() => discovery::discover()?.base_url,
                _ => "http://127.0.0.1:8795".into(),
            },
        };
        if !(server.starts_with("http://") || server.starts_with("https://")) {
            return Err("服务地址必须使用 HTTP 或 HTTPS".into());
        }
        let client = Arc::new(Client::with_ca(
            &server,
            Duration::from_secs(10),
            c.sign,
            c.ca_file.as_deref(),
        )?);
        let mut session = DeviceSession::new(
            cred.clone(),
            client,
            if c.harmonyos {
                Platform::HarmonyOs
            } else {
                Platform::Unspecified
            },
            c.app_version.clone(),
        );
        loop {
            if stopped.load(Ordering::Relaxed) {
                return Ok(());
            }
            match session.login() {
                Ok(_) => break,
                Err(e) if !once => {
                    eprintln!("{}，10 秒后重新连接", e.message());
                    for _ in 0..100 {
                        if stopped.load(Ordering::Relaxed) {
                            return Ok(());
                        }
                        std::thread::sleep(Duration::from_millis(100));
                    }
                }
                Err(e) => return Err(e.message()),
            }
        }
        if let Some(code) = bind {
            session.bind(&code).map_err(|e| e.message())?;
        }
        if !session.is_bound() {
            return Err("设备尚未绑定长者，请使用 --bind 提供绑定码".into());
        }
        session.heartbeat(88).map_err(|e| e.message())?;
        Some(session)
    };
    let mut rng = Pcg32::new(c.seed);
    let mut timeline = Timeline::new(&c.scenario, &mut rng);
    let mut physio = Physiology::new(c.seed, c.longitude, c.latitude, 9.0);
    let mut last_heartbeat = std::time::Instant::now();
    let mut pending_falls = VecDeque::new();
    let mut pending_sos = None;
    let mut previous_scene = None;
    let device_no = cred.device_no.clone();
    let run_id = uuid::Uuid::new_v4().simple().to_string();
    let mut record_ids = [String::with_capacity(64), String::with_capacity(64)];
    let mut sequence = 0u64;
    let mut health_retry_at = std::time::Instant::now();
    let mut day = watch_sim::now_unix() / 86400;
    loop {
        if stopped.load(Ordering::Relaxed) {
            break;
        }
        let scene = timeline.current();
        let entering_scene = previous_scene != Some(scene);
        if entering_scene {
            physio.set_hour(if scene.is_night() { 5.5 } else { 9.0 });
        }
        previous_scene = Some(scene);
        if scene.forces_low_battery() {
            physio.set_battery(5.0);
        }
        let now = watch_sim::now_unix();
        if now / 86400 != day {
            physio.reset_steps();
            day = now / 86400;
        }
        let v = physio.tick(c.interval as f64, scene.targets());
        if let Some(session) = session.as_mut() {
            if last_heartbeat.elapsed() >= Duration::from_secs(60) {
                match session.heartbeat(v.battery) {
                    Ok(heartbeat) => {
                        if heartbeat.epoch_changed
                            && (!pending_falls.is_empty() || pending_sos.is_some())
                        {
                            return Err("绑定版本已改变，停止发送旧绑定的待确认事件".into());
                        }
                        if !session.is_bound() {
                            return Err("设备绑定已解除，停止上报".into());
                        }
                        last_heartbeat = std::time::Instant::now();
                    }
                    Err(e)
                        if !once
                            && e.retryable()
                            && (!pending_falls.is_empty() || pending_sos.is_some()) =>
                    {
                        eprintln!("{}，待确认事件保留，下一拍重试心跳", e.message());
                    }
                    Err(e) => return Err(e.message()),
                }
            }
        }
        // Online reports use the server wall clock; local exports retain their own time.
        let mut p = ReportPayload::new(
            &cred,
            if local.is_some() {
                watch_sim::format_local(now)
            } else {
                String::new()
            },
        );
        p.hr = Some(v.hr);
        p.bp = Some(v.bp);
        p.step = Some(v.steps);
        p.sleep = Some(v.sleep_hours);
        p.sleep_state = Some(if v.sleep_hours >= 7 { "0" } else { "1" }.into());
        p.lng = Some(format!("{:.6}", v.lng));
        p.lat = Some(format!("{:.6}", v.lat));
        p.source = Some("gps".into());
        p.position_source = Some("simulated".into());
        p.battery = Some(v.battery);
        p.app_version = Some(c.app_version.clone());
        if local.is_some() && scene.emits_alarm() {
            p.alarm = Some("fall".into());
        }
        if let Some(session) = session.as_mut() {
            p.binding_version = session.binding().map(|b| b.version);
            if entering_scene && scene.emits_alarm() && c.allow_sos {
                let mut event = p.clone();
                event.alarm = Some("fall".into());
                event.request_id = Some(uuid::Uuid::new_v4().simple().to_string());
                pending_falls.push_back(event);
            }
            if entering_scene && scene.asks_sos() && c.allow_sos && pending_sos.is_none() {
                pending_sos = Some((uuid::Uuid::new_v4().simple().to_string(), v.lng, v.lat));
            }
            // Keep the complete original event until its acknowledgement arrives.
            match session.report(pending_falls.front().unwrap_or(&p)) {
                Ok(_) => {
                    pending_falls.pop_front();
                }
                Err(e) if !once && !e.is_auth() && !e.is_unbound() => {
                    eprintln!("{}，下一拍重试待确认上报", e.message());
                }
                Err(e) => return Err(e.message()),
            }
        }
        // Only the explicit pre-transaction admission rejection is retried here.
        // Keep the original sample IDs; never create a second batch while pending.
        if let Some(session) = session.as_mut() {
            if !session.queue().is_empty()
                && pending_falls.is_empty()
                && pending_sos.is_none()
                && std::time::Instant::now() >= health_retry_at
            {
                match session.flush() {
                    Ok(_) => {}
                    Err(e) if e.is_health_write_busy() && !once => {
                        health_retry_at = std::time::Instant::now()
                            + Duration::from_millis(
                                1000 + (uuid::Uuid::new_v4().as_u128() % 4001) as u64,
                            );
                    }
                    Err(e) => return Err(e.message()),
                }
            }
        }
        if c.health_samples
            && pending_falls.is_empty()
            && pending_sos.is_none()
            && session
                .as_ref()
                .is_none_or(|session| session.queue().is_empty())
        {
            sequence += 1;
            let at = watch_sim::format_utc(now);
            let start = watch_sim::format_utc(day * 86400);
            let device = &device_no;
            for (buffer, metric) in record_ids.iter_mut().zip(["hr", "steps"]) {
                watch_sim::protocol::record_id_into(buffer, &run_id, sequence, metric);
            }
            let samples = [
                watch_sim::protocol::SampleRef::heart_rate(&record_ids[0], device, v.hr, &at),
                watch_sim::protocol::SampleRef::steps(&record_ids[1], device, v.steps, &start, &at),
            ];
            if let Some(session) = session.as_mut() {
                match session.send_sample_views(&samples) {
                    Ok(_) => {}
                    Err(e) if e.is_health_write_busy() && !once => {
                        session
                            .enqueue([
                                HealthSample::heart_rate(record_ids[0].clone(), device, v.hr, &at),
                                HealthSample::steps(
                                    record_ids[1].clone(),
                                    device,
                                    v.steps,
                                    &start,
                                    &at,
                                ),
                            ])
                            .map_err(|e| e.message())?;
                        health_retry_at = std::time::Instant::now()
                            + Duration::from_millis(
                                1000 + (uuid::Uuid::new_v4().as_u128() % 4001) as u64,
                            );
                        eprintln!("{}; health samples retained for retry", e.message());
                    }
                    Err(e) => return Err(e.message()),
                }
            }
            if let Some(local) = local.as_mut() {
                local.health_samples(&samples)?;
            }
        }
        if let Some(session) = session.as_mut() {
            if let Some((id, lng, lat)) = pending_sos.as_ref() {
                match session.sos(id, Some(*lng), Some(*lat)) {
                    Ok(_) => pending_sos = None,
                    Err(e) if !once && !e.is_auth() && !e.is_unbound() => {
                        eprintln!("{}，保留 SOS 事件编号等待重试", e.message());
                    }
                    Err(e) => return Err(e.message()),
                }
            }
            println!(
                "{} hr={} bp={} steps={} requests={} ok={}",
                scene.code(),
                v.hr,
                v.bp,
                v.steps,
                session.stats.requests,
                session.stats.ok
            );
        }
        if let Some(local) = local.as_mut() {
            local.report(&p, scene)?;
            println!(
                "本地 {} hr={} bp={} steps={}",
                scene.code(),
                v.hr,
                v.bp,
                v.steps
            );
        }
        if once {
            break;
        }
        for _ in 0..c.interval * 10 {
            if stopped.load(Ordering::Relaxed) {
                break;
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        timeline.tick(c.interval as f64, &mut rng);
    }
    if let Some(session) = session.as_ref() {
        if !session.queue().is_empty() {
            return Err(format!(
                "Stopped with {} unacknowledged health samples",
                session.queue().len()
            ));
        }
        println!(
            "已结束：请求 {}，成功 {}，业务拒绝 {}，通信失败 {}",
            session.stats.requests, session.stats.ok, session.stats.rejected, session.stats.failed
        );
    } else {
        println!("本地模拟已结束，数据已保存。");
    }
    Ok(())
}
fn main() {
    if let Err(e) = run() {
        eprintln!("{e}");
        std::process::exit(1);
    }
}
