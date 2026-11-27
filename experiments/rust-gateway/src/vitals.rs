//! Isolated vital-event lane, using the existing protected ratepre format.
//! A trusted producer must authenticate the device before sealing an event.
use crate::{
    consumer::{Error, go_time},
    native,
    outbox::Keys,
};
use chrono::NaiveDateTime;
use serde::{Deserialize, Serialize};
use sqlx::{MySql, MySqlPool, Row, Transaction};

const SCOPE: &[u8] = b"yihe-next-vitals-event-v1";
type Bounds = (Option<i64>, Option<i64>, Option<i64>, Option<i64>);
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Event {
    version: u8,
    device_id: i64,
    resident_id: i64,
    binding_epoch: i64,
    auth_generation: i64,
    request_id: String,
    received_at: String,
    observed_at: String,
    hr: Option<i64>,
    bp: Option<i64>,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Protected {
    owner: String,
    device: String,
    hr: Option<i64>,
    bp: Option<i64>,
}

fn state(
    value: Option<i64>,
    a: Option<i64>,
    b: Option<i64>,
    defaults: (i64, i64),
) -> Option<&'static str> {
    let (lo, hi) = match (a, b) {
        (Some(a), Some(b)) => (a.min(b), a.max(b)),
        _ => defaults,
    };
    value.map(|v| if v < lo || v > hi { "1" } else { "0" })
}
async fn reject(
    mut tx: Transaction<'_, MySql>,
    e: &Event,
    digest: &str,
    reason: &str,
) -> Result<(), Error> {
    sqlx::query("INSERT INTO rust_vital_rejection(device_id,binding_epoch,request_id,payload_digest,reason) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE reason=reason")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).bind(digest.as_bytes()).bind(reason).execute(&mut *tx).await?;
    tx.commit().await?;
    Ok(())
}
pub(crate) async fn commit(pool: &MySqlPool, keys: &Keys, raw: &[u8]) -> Result<(), Error> {
    let plain = keys
        .open_scoped(raw, SCOPE)
        .map_err(|_| "vital envelope authentication failed")?;
    let mut e: Event = serde_json::from_slice(&plain)?;
    if e.version != 1
        || e.device_id <= 0
        || e.resident_id <= 0
        || e.binding_epoch <= 0
        || e.auth_generation <= 0
        || e.request_id.is_empty()
        || e.request_id.len() > 64
        || !e
            .request_id
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"_.:-".contains(&b))
        || (e.hr.is_none() && e.bp.is_none())
        || e.hr.is_some_and(|v| !(20..=250).contains(&v))
        || e.bp.is_some_and(|v| !(40..=300).contains(&v))
    {
        return Err("unsupported vital event".into());
    }
    let (received, rc) = go_time(&e.received_at)?;
    let (observed, oc) = go_time(&e.observed_at)?;
    // The wire contract uses integral seconds for legacy measurement storage.
    if observed.timestamp_subsec_nanos() != 0
        || observed.timestamp_nanos_opt().is_none()
        || observed > received + chrono::Duration::minutes(5)
    {
        return Err("unsupported observation time".into());
    }
    e.received_at = rc;
    e.observed_at = oc;
    let digest = keys.digest_scoped(b"yihe-next-vitals-receipt-v1", &serde_json::to_vec(&e)?);
    let mut tx = pool.begin().await?;
    if sqlx::query_scalar::<_, i64>("SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE")
        .bind(e.resident_id)
        .fetch_optional(&mut *tx)
        .await?
        .is_none()
    {
        return reject(tx, &e, &digest, "resident_missing").await;
    }
    let Some(device) = sqlx::query(
        "SELECT old_id,status,health_epoch,bind_time FROM device WHERE device_id=? FOR UPDATE",
    )
    .bind(e.device_id)
    .fetch_optional(&mut *tx)
    .await?
    else {
        return reject(tx, &e, &digest, "device_missing").await;
    };
    let owner: Option<i64> = device.try_get("old_id")?;
    let status: Option<i64> = device.try_get("status")?;
    let epoch: i64 = device.try_get("health_epoch")?;
    let bound: Option<NaiveDateTime> = device.try_get("bind_time")?;
    if owner != Some(e.resident_id)
        || status.is_none()
        || status == Some(0)
        || epoch != e.binding_epoch
        || bound.is_some_and(|t| observed.naive_utc() < t)
    {
        return reject(tx, &e, &digest, "binding_or_status").await;
    }
    let generation: i64 = sqlx::query_scalar(
        "SELECT generation FROM broker_device_generation WHERE device_id=? FOR UPDATE",
    )
    .bind(e.device_id)
    .fetch_one(&mut *tx)
    .await?;
    if generation != e.auth_generation {
        return reject(tx, &e, &digest, "authorization_generation").await;
    }
    if sqlx::query_scalar::<_,String>("SELECT reason FROM rust_vital_rejection WHERE device_id=? AND binding_epoch=? AND request_id=? AND payload_digest=? FOR UPDATE")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).bind(digest.as_bytes()).fetch_optional(&mut *tx).await?.is_some(){tx.commit().await?;return Ok(())}
    if let Some(prior)=sqlx::query_scalar::<_,Vec<u8>>("SELECT payload_digest FROM rust_vital_receipt WHERE device_id=? AND binding_epoch=? AND request_id=? FOR UPDATE")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).fetch_optional(&mut *tx).await?{
        if prior!=digest.as_bytes(){return reject(tx,&e,&digest,"request_id_conflict").await}
        tx.commit().await?;return Ok(())
    }
    let bounds: Option<Bounds> =
        sqlx::query_as("SELECT hrf,hrl,bpf,bpl FROM watch_info WHERE device_id=?")
            .bind(e.device_id)
            .fetch_optional(&mut *tx)
            .await?;
    let (a, b, c, d) = bounds.unwrap_or_default();
    let hr_state = state(e.hr, a, b, (55, 110));
    let bp_state = state(e.bp, c, d, (90, 140));
    let id=sqlx::query("INSERT INTO ratepre(device_id,old_id,rate_state,pre_state,create_time,hr,bp) VALUES(?,?,?,?,?,NULL,NULL)")
        .bind(e.device_id).bind(e.resident_id).bind(hr_state).bind(bp_state).bind(observed.naive_utc()).execute(&mut *tx).await?.last_insert_id();
    let id = i64::try_from(id)?;
    if id <= 0 {
        return Err("invalid measurement id".into());
    }
    let payload = serde_json::to_vec(&Protected {
        owner: e.resident_id.to_string(),
        device: e.device_id.to_string(),
        hr: e.hr,
        bp: e.bp,
    })?;
    let scope = format!("vital-measurements:{id}");
    let cipher = keys
        .seal_scoped(&payload, scope.as_bytes())
        .map_err(|_| "measurement encryption failed")?;
    let changed=sqlx::query("UPDATE ratepre SET hr=NULL,bp=NULL,measurement_cipher=? WHERE id=? AND measurement_cipher IS NULL").bind(String::from_utf8(cipher)?).bind(id).execute(&mut *tx).await?.rows_affected();
    if changed != 1 {
        return Err("measurement write not confirmed".into());
    }
    // Match healthalertstate.RecordVitals; normal samples never resolve
    // an earlier unresolved alarm, and a new binding clears old flags.
    sqlx::query("INSERT INTO device_health_alert_state(device_id,binding_epoch,heart_rate_alert,blood_pressure_alert,sleep_alert,vitals_observed_at) VALUES(?,?,?,?,false,?) AS new ON DUPLICATE KEY UPDATE heart_rate_alert=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN new.heart_rate_alert ELSE device_health_alert_state.heart_rate_alert OR new.heart_rate_alert END,blood_pressure_alert=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN new.blood_pressure_alert ELSE device_health_alert_state.blood_pressure_alert OR new.blood_pressure_alert END,sleep_alert=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN false ELSE device_health_alert_state.sleep_alert END,vitals_observed_at=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN new.vitals_observed_at ELSE COALESCE(GREATEST(device_health_alert_state.vitals_observed_at,new.vitals_observed_at),device_health_alert_state.vitals_observed_at,new.vitals_observed_at) END,sleep_observed_at=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN NULL ELSE device_health_alert_state.sleep_observed_at END,binding_epoch=new.binding_epoch")
        .bind(e.device_id).bind(e.binding_epoch).bind(hr_state==Some("1")).bind(bp_state==Some("1")).bind(observed.naive_utc()).execute(&mut *tx).await?;
    native::touch(&mut tx, e.device_id, e.binding_epoch, received.naive_utc())
        .await
        .map_err(|_| "heartbeat transaction failed")?;
    let prior:Option<i64>=sqlx::query_scalar("SELECT revision FROM rust_vital_receipt WHERE device_id=? AND binding_epoch=? ORDER BY revision DESC LIMIT 1").bind(e.device_id).bind(e.binding_epoch).fetch_optional(&mut *tx).await?;
    let revision = prior
        .unwrap_or(0)
        .checked_add(1)
        .ok_or("revision overflow")?;
    sqlx::query("INSERT INTO rust_vital_receipt(device_id,binding_epoch,request_id,payload_digest,measurement_id,revision) VALUES(?,?,?,?,?,?)")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).bind(digest.as_bytes()).bind(id).bind(revision).execute(&mut *tx).await?;
    tx.commit().await?;
    Ok(())
}

#[path = "vital_projection.rs"]
mod projection;
pub use projection::{stats, stats_worker};
