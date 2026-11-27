//! A deliberately narrow, signed-only step-report writer for disposable schemas.
use axum::{
    Json, Router,
    body::to_bytes,
    extract::{Request, State},
    http::{HeaderMap, StatusCode},
    response::{IntoResponse, Response},
    routing::post,
};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::{DateTime, Duration as ChronoDuration, NaiveDateTime, Utc};
use hmac::{Hmac, Mac};
use serde::Deserialize;
use serde_json::json;
use sha2::{Digest, Sha256};
use sqlx::{
    MySql, MySqlPool, Row, Transaction,
    mysql::{MySqlConnectOptions, MySqlPoolOptions, MySqlSslMode},
};
use std::{net::IpAddr, sync::Arc, time::Duration};
use subtle::ConstantTimeEq;
use tokio::sync::Semaphore;

#[derive(Clone, Copy, Debug)]
enum Failure {
    Signature,
    Invalid,
    Credentials,
    Disabled,
    Unbound,
    Epoch,
    Binding,
    Replay,
    Database,
    Busy,
}
impl IntoResponse for Failure {
    fn into_response(self) -> Response {
        let (status, code, message) = match self {
            Self::Signature => (401, "DEVICE_SIGNATURE_INVALID", "设备请求签名无效或已过期"),
            Self::Invalid => (400, "INVALID_REQUEST", "请求字段不完整或超出允许范围"),
            Self::Credentials => (401, "DEVICE_AUTH_FAILED", "设备号或密钥不正确"),
            Self::Disabled => (403, "DEVICE_DISABLED", "设备已停用"),
            Self::Unbound => (409, "DEVICE_NOT_BOUND", "设备未绑定"),
            Self::Epoch => (409, "EPOCH_EXPIRED", "绑定纪元已过期"),
            Self::Binding => (409, "BINDING_CHANGED", "绑定已变化"),
            Self::Replay => (409, "NONCE_REPLAY", "设备请求已处理，请使用新的随机数重试"),
            Self::Database => (500, "INTERNAL_ERROR", "服务处理失败"),
            Self::Busy => (503, "SERVICE_UNAVAILABLE", "服务暂不可用"),
        };
        (
            StatusCode::from_u16(status).unwrap(),
            Json(json!({"returnCode":"0000","code":code,"message":message})),
        )
            .into_response()
    }
}
impl From<sqlx::Error> for Failure {
    fn from(_: sqlx::Error) -> Self {
        Self::Database
    }
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct StepReport {
    #[serde(rename = "deviceNo")]
    number: String,
    secret: String,
    #[serde(rename = "bindingVersion")]
    epoch: i64,
    #[serde(rename = "beTime")]
    reported: String,
    step: i64,
}
struct Verified {
    report: StepReport,
    observed: NaiveDateTime,
    nonce: [u8; 32],
    expires: NaiveDateTime,
}

fn header<'a>(headers: &'a HeaderMap, name: &str) -> Result<&'a str, Failure> {
    let mut all = headers.get_all(name).iter();
    let value = all
        .next()
        .and_then(|v| v.to_str().ok())
        .ok_or(Failure::Signature)?;
    if all.next().is_some() {
        return Err(Failure::Signature);
    }
    Ok(value.trim())
}
fn verify(headers: &HeaderMap, raw: &[u8], now: DateTime<Utc>) -> Result<Verified, Failure> {
    let signature = header(headers, "x-device-signature")?;
    let ts = header(headers, "x-device-timestamp")?;
    let nonce = header(headers, "x-device-nonce")?;
    if signature.len() != 64
        || !(16..=64).contains(&nonce.len())
        || !nonce
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
    {
        return Err(Failure::Signature);
    }
    let seconds = ts.parse::<i64>().map_err(|_| Failure::Signature)?;
    let signed = DateTime::from_timestamp(seconds, 0).ok_or(Failure::Signature)?;
    if signed < now - ChronoDuration::minutes(5) || signed > now + ChronoDuration::minutes(5) {
        return Err(Failure::Signature);
    }
    // Use a strict typed subset, rejecting duplicate/unknown fields. No
    // permissive legacy interpretation can silently discard a medical field.
    let mut report: StepReport = serde_json::from_slice(raw).map_err(|_| Failure::Invalid)?;
    if report.number.is_empty()
        || report.number.len() > 32
        || !report
            .number
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
        || !(24..=64).contains(&report.secret.len())
        || report.secret.trim() != report.secret
    {
        return Err(Failure::Invalid);
    }
    report.number.make_ascii_uppercase();
    let mut mac =
        Hmac::<Sha256>::new_from_slice(report.secret.as_bytes()).map_err(|_| Failure::Signature)?;
    mac.update(
        format!(
            "{}:{}:{}:{}",
            report.number,
            ts,
            nonce,
            hex::encode(Sha256::digest(raw))
        )
        .as_bytes(),
    );
    let signature = hex::decode(signature).map_err(|_| Failure::Signature)?;
    mac.verify_slice(&signature)
        .map_err(|_| Failure::Signature)?;
    if report.epoch < 1 || !(0..=200000).contains(&report.step) {
        return Err(Failure::Invalid);
    }
    let observed = NaiveDateTime::parse_from_str(&report.reported, "%Y-%m-%d %H:%M:%S")
        .map_err(|_| Failure::Invalid)?;
    if observed.format("%Y-%m-%d %H:%M:%S").to_string() != report.reported
        || observed > now.naive_utc() + ChronoDuration::minutes(5)
    {
        return Err(Failure::Invalid);
    }
    let mut digest = Sha256::new();
    digest.update(report.number.as_bytes());
    digest.update([0]);
    digest.update(nonce.as_bytes());
    Ok(Verified {
        report,
        observed,
        nonce: digest.finalize().into(),
        expires: (signed + ChronoDuration::minutes(5)).naive_utc(),
    })
}
fn matches(secret: &str, stored: &str) -> bool {
    if !(24..=64).contains(&secret.len()) {
        return false;
    }
    let mut h = Sha256::new();
    h.update(b"yihe-device-secret-v1\0");
    h.update(secret.as_bytes());
    let expected = format!("sha256${}", URL_SAFE_NO_PAD.encode(h.finalize()));
    bool::from(expected.as_bytes().ct_eq(stored.as_bytes()))
}
struct Device {
    id: i64,
    number: String,
    secret_hash: String,
    owner: Option<i64>,
    status: Option<i64>,
    epoch: i64,
}
fn read_device(row: sqlx::mysql::MySqlRow) -> Result<Device, Failure> {
    Ok(Device {
        id: row.try_get("device_id")?,
        number: row.try_get("device_no")?,
        secret_hash: row.try_get("secret")?,
        owner: row.try_get("old_id")?,
        status: row.try_get("status")?,
        epoch: row.try_get("health_epoch")?,
    })
}
fn check(d: &Device, secret: &str) -> Result<(), Failure> {
    if !matches(secret, &d.secret_hash) {
        return Err(Failure::Credentials);
    }
    if d.status == Some(0) {
        return Err(Failure::Disabled);
    }
    Ok(())
}
async fn commit(pool: &MySqlPool, v: Verified, now: DateTime<Utc>) -> Result<(), Failure> {
    let r = &v.report;
    // No write based on the unlocked snapshot. Re-read credential and
    // ownership under the existing resident -> device lock order.
    let first=sqlx::query("SELECT device_id,device_no,secret,old_id,status,health_epoch FROM device WHERE device_no=?")
        .bind(&r.number).fetch_optional(pool).await?.ok_or(Failure::Credentials).and_then(read_device)?;
    check(&first, &r.secret)?;
    let owner = first.owner.ok_or(Failure::Unbound)?;
    let mut tx = pool.begin().await?;
    sqlx::query("SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE")
        .bind(owner)
        .fetch_one(&mut *tx)
        .await?;
    let current=read_device(sqlx::query("SELECT device_id,device_no,secret,old_id,status,health_epoch FROM device WHERE device_id=? FOR UPDATE")
        .bind(first.id).fetch_one(&mut *tx).await?)?;
    check(&current, &r.secret)?;
    if current.number != r.number {
        return Err(Failure::Credentials);
    }
    if r.epoch < current.epoch {
        return Err(Failure::Epoch);
    }
    if current.owner != Some(owner) || current.epoch != r.epoch {
        return Err(Failure::Binding);
    }
    // This is the existing shared production replay table, not a local cache.
    let inserted = sqlx::query(
        "INSERT INTO device_request_nonces(device_id,nonce_digest,expires_at) VALUES(?,?,?)",
    )
    .bind(current.id)
    .bind(v.nonce.as_slice())
    .bind(v.expires)
    .execute(&mut *tx)
    .await;
    if let Err(e) = inserted {
        if e.as_database_error()
            .is_some_and(|d| d.is_unique_violation())
        {
            return Err(Failure::Replay);
        }
        return Err(Failure::Database);
    }
    persist_step(&mut tx, current.id, owner, r.epoch, v.observed, r.step).await?;
    let seen = now.naive_utc();
    sqlx::query("UPDATE device SET last_time=GREATEST(COALESCE(last_time,?),?),last_online_at=GREATEST(COALESCE(last_online_at,?),?) WHERE device_id=? AND health_epoch=? AND ?>=COALESCE(last_online_at,?)")
        .bind(seen).bind(seen).bind(seen).bind(seen).bind(current.id).bind(r.epoch).bind(seen).bind(seen).execute(&mut *tx).await?;
    tx.commit().await?;
    Ok(())
}

async fn persist_step(
    tx: &mut Transaction<'_, MySql>,
    device: i64,
    owner: i64,
    epoch: i64,
    observed: NaiveDateTime,
    step: i64,
) -> Result<(), Failure> {
    let day = observed.date();
    if let Some(row)=sqlx::query("SELECT id,old_id,binding_epoch FROM watch_day_info WHERE device_id=? AND create_time=? FOR UPDATE")
        .bind(device).bind(day).fetch_optional(&mut **tx).await? {
        let id:i64=row.try_get("id")?;
        let old:Option<i64>=row.try_get("old_id")?;
        let old_epoch:i64=row.try_get("binding_epoch")?;
        if old!=Some(owner)||old_epoch!=epoch{
            sqlx::query("INSERT INTO watch_day_info_archive(device_id,sleep,step,sleep_sate,create_time,step_observed_at,old_id,binding_epoch,sleep_observed_at,sleep_state_observed_at) SELECT device_id,sleep,step,sleep_sate,create_time,step_observed_at,old_id,binding_epoch,sleep_observed_at,sleep_state_observed_at FROM watch_day_info WHERE id=?")
                .bind(id).execute(&mut **tx).await?;
            sqlx::query("UPDATE watch_day_info SET sleep=NULL,step=NULL,sleep_sate=NULL,step_observed_at=NULL,sleep_observed_at=NULL,sleep_state_observed_at=NULL,old_id=?,binding_epoch=? WHERE id=?")
                .bind(owner).bind(epoch).bind(id).execute(&mut **tx).await?;
        }
    }
    sqlx::query("INSERT INTO watch_day_info(device_id,step,create_time,step_observed_at,old_id,binding_epoch) VALUES(?,?,?,?,?,?) AS new ON DUPLICATE KEY UPDATE step=CASE WHEN watch_day_info.step_observed_at IS NULL OR new.step_observed_at>=watch_day_info.step_observed_at THEN new.step ELSE watch_day_info.step END,step_observed_at=COALESCE(GREATEST(watch_day_info.step_observed_at,new.step_observed_at),watch_day_info.step_observed_at,new.step_observed_at)")
        .bind(device).bind(step).bind(day).bind(observed).bind(owner).bind(epoch).execute(&mut **tx).await?;
    Ok(())
}
#[derive(Clone)]
struct StateData {
    pool: MySqlPool,
    gate: Arc<Semaphore>,
}
async fn step(State(state): State<StateData>, request: Request) -> Result<Response, Failure> {
    let _permit = state.gate.try_acquire().map_err(|_| Failure::Busy)?;
    let (parts, body) = request.into_parts();
    let raw = tokio::time::timeout(Duration::from_secs(10), to_bytes(body, 1 << 20))
        .await
        .map_err(|_| Failure::Invalid)?
        .map_err(|_| Failure::Invalid)?;
    let now = Utc::now();
    let verified = verify(&parts.headers, &raw, now)?;
    commit(&state.pool, verified, now).await?;
    Ok(Json(json!({"returnCode":"1111","rateState":null,"preState":null,"serverTime":now.format("%Y-%m-%d %H:%M:%S").to_string()})).into_response())
}

/// Credentials are passed only through the local process environment.
pub async fn test_router() -> Result<Router, Box<dyn std::error::Error>> {
    let host: IpAddr = std::env::var("YIHE_NEXT_DB_HOST")?.parse()?;
    let schema = std::env::var("YIHE_NEXT_DB_SCHEMA")?;
    if !host.is_loopback()
        || !schema.starts_with("yihe_test_")
        || schema.len() != 18
        || !schema[10..].bytes().all(|b| b.is_ascii_hexdigit())
    {
        return Err("native writer requires a disposable loopback yihe_test_ schema".into());
    }
    let options = MySqlConnectOptions::new()
        .host(&host.to_string())
        .port(std::env::var("YIHE_NEXT_DB_PORT")?.parse()?)
        .username(&std::env::var("YIHE_NEXT_DB_USER")?)
        .password(&std::env::var("YIHE_NEXT_DB_PASSWORD")?)
        .database(&schema)
        .ssl_mode(MySqlSslMode::Disabled);
    let pool = MySqlPoolOptions::new()
        .max_connections(8)
        .acquire_timeout(Duration::from_secs(5))
        .after_connect(|conn, _| {
            Box::pin(async move {
                sqlx::query("SET SESSION TRANSACTION ISOLATION LEVEL READ COMMITTED")
                    .execute(&mut *conn)
                    .await?;
                sqlx::query("SET time_zone='+00:00'")
                    .execute(&mut *conn)
                    .await?;
                sqlx::query("SET SESSION innodb_lock_wait_timeout=5")
                    .execute(&mut *conn)
                    .await?;
                Ok(())
            })
        })
        .connect_with(options)
        .await?;
    Ok(Router::new()
        .route("/api/v1/device/report", post(step))
        .with_state(StateData {
            pool,
            gate: Arc::new(Semaphore::new(8)),
        }))
}
