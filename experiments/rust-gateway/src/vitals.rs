//! Isolated vital-event lane, using the existing protected ratepre format.
//! A trusted producer must authenticate the device before sealing an event.
use crate::{
    consumer::{Error, go_time},
    native,
    outbox::Keys,
};
use chrono::NaiveDateTime;
use serde::{Deserialize, Serialize};
use serde_json::json;
use sqlx::{MySql, MySqlPool, Row, Transaction};
use yihe_core::{Point, SeriesKey, Window, rolling::RollingHeartRate};

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

/// Test-only local read of the last <=256 committed vital events in the
/// current binding, not the complete production health_sample history.
/// No cached result survives a change in identity, revision or process.
pub async fn stats(device: i64) -> Result<(), Box<dyn std::error::Error>> {
    let pool = native::test_pool().await?;
    let keys = Keys::load()?;
    let owner: Option<i64> = sqlx::query_scalar("SELECT old_id FROM device WHERE device_id=?")
        .bind(device)
        .fetch_one(&pool)
        .await?;
    let owner = owner.ok_or("unbound device")?;
    let mut tx = pool.begin().await?;
    sqlx::query_scalar::<_, i64>("SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE")
        .bind(owner)
        .fetch_one(&mut *tx)
        .await?;
    let row =
        sqlx::query("SELECT old_id,health_epoch,status FROM device WHERE device_id=? FOR UPDATE")
            .bind(device)
            .fetch_one(&mut *tx)
            .await?;
    let current: Option<i64> = row.try_get("old_id")?;
    let epoch: i64 = row.try_get("health_epoch")?;
    let status: Option<i64> = row.try_get("status")?;
    if current != Some(owner) || status.is_none() || status == Some(0) {
        return Err("binding unavailable".into());
    }
    let rows=sqlx::query("SELECT r.revision,r.measurement_id,m.old_id,m.device_id,m.create_time,m.measurement_cipher,m.hr,m.bp FROM rust_vital_receipt r LEFT JOIN ratepre m ON m.id=r.measurement_id WHERE r.device_id=? AND r.binding_epoch=? ORDER BY r.revision DESC LIMIT 256").bind(device).bind(epoch).fetch_all(&mut *tx).await?;
    let key = SeriesKey {
        resident_id: owner,
        source: "vital-event".into(),
        source_device: format!("{device}:{epoch}"),
    };
    let mut revision = 0i64;
    let mut previous = None;
    let mut points = Vec::new();
    let mut state: Option<RollingHeartRate> = None;
    let mut window = Window { from: 0, to: 1 };
    let mut appends = 0;
    let mut rebuilds = 0;
    for row in rows.iter().rev() {
        let rev: i64 = row.try_get("revision")?;
        if previous.is_some_and(|p: i64| p.checked_add(1) != Some(rev)) {
            return Err("noncontiguous committed revisions".into());
        }
        previous = Some(rev);
        revision = rev;
        let id: i64 = row.try_get("measurement_id")?;
        let old: Option<i64> = row.try_get("old_id")?;
        let dev: Option<i64> = row.try_get("device_id")?;
        let legacy_hr: Option<i64> = row.try_get("hr")?;
        let legacy_bp: Option<i64> = row.try_get("bp")?;
        let cipher: Option<String> = row.try_get("measurement_cipher")?;
        if old != Some(owner) || dev != Some(device) || legacy_hr.is_some() || legacy_bp.is_some() {
            return Err("invalid committed measurement identity".into());
        }
        let plain = keys.open_scoped(
            cipher.ok_or("missing protected measurement")?.as_bytes(),
            format!("vital-measurements:{id}").as_bytes(),
        )?;
        let value: Protected = serde_json::from_slice(&plain)?;
        if value.owner != owner.to_string() || value.device != device.to_string() {
            return Err("protected identity mismatch".into());
        }
        let at: NaiveDateTime = row.try_get("create_time")?;
        let at = at
            .and_utc()
            .timestamp_nanos_opt()
            .ok_or("unsupported observation time")?;
        let point = value.hr.map(|v| Point {
            at,
            value: v as f64,
        });
        if let Some(point) = point {
            points.push(point);
        }
        if let (Some(s), Some(point)) = (&mut state, point) {
            let next = Window {
                from: window.from,
                to: window
                    .to
                    .max(at.checked_add(1).ok_or("timestamp overflow")?),
            };
            if s.append(&key, rev, next, point).is_ok() {
                window = next;
                appends += 1;
                continue;
            }
        }
        if points.is_empty() {
            continue;
        }
        points.sort_by_key(|p| p.at);
        window = Window {
            from: points[0].at,
            to: points
                .last()
                .expect("nonempty")
                .at
                .checked_add(1)
                .ok_or("timestamp overflow")?,
        };
        state = Some(
            RollingHeartRate::from_committed(key.clone(), rev, window, &points, 256)
                .map_err(|_| "invalid committed heart-rate snapshot")?,
        );
        rebuilds += 1;
    }
    let result = match state {
        Some(s) => {
            let v = s
                .query(&key, revision, window)
                .map_err(|_| "projection revision miss")?
                .ok_or("empty projection")?;
            json!({"revision":revision,"count":v.count,"min":v.min,"max":v.max,"mean":v.mean,"appends":appends,"rebuilds":rebuilds})
        }
        None => json!({"revision":revision,"count":0,"appends":appends,"rebuilds":rebuilds}),
    };
    tx.commit().await?;
    println!("{result}");
    pool.close().await;
    Ok(())
}
