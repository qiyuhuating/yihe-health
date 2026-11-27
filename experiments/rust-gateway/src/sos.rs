//! Isolated SOS consumer for trusted, authenticated no-position events.
//! Incident receipt and both notification outboxes share the SQL transaction.
use crate::{
    consumer::{Error, go_time},
    native,
    outbox::Keys,
};
use serde::{Deserialize, Serialize};
use sqlx::{MySql, MySqlPool, QueryBuilder, Row, Transaction};
use subtle::ConstantTimeEq;

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
}

async fn reject(
    mut tx: Transaction<'_, MySql>,
    e: &Event,
    digest: &str,
    reason: &str,
) -> Result<(), Error> {
    sqlx::query("INSERT INTO rust_sos_rejection(device_id,binding_epoch,request_id,payload_digest,reason) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE reason=reason")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).bind(digest.as_bytes()).bind(reason).execute(&mut *tx).await?;
    tx.commit().await?;
    Ok(())
}
fn valid_phone(phone: &str) -> bool {
    phone.len() == 11 && phone.starts_with('1') && phone.bytes().all(|b| b.is_ascii_digit())
}
pub(crate) async fn notify(
    tx: &mut Transaction<'_, MySql>,
    keys: &Keys,
    incident: i64,
    owner: i64,
) -> Result<(), Error> {
    notify_batch(tx, keys, &[incident], owner).await
}

/// One coherent recipient snapshot per batch. Each recipient envelope still
/// receives its own random nonce; no plaintext recipient is persisted.
pub(crate) async fn notify_batch(
    tx: &mut Transaction<'_, MySql>,
    keys: &Keys,
    incidents: &[i64],
    owner: i64,
) -> Result<(), Error> {
    if incidents.is_empty() {
        return Ok(());
    }
    let Some(row)=sqlx::query("SELECT f.family_id,f.family_phone,f.family_phone_cipher,f.family_phone_lookup,f.family_phone_protected FROM oldperson o JOIN family f ON f.family_id=o.family_id WHERE o.old_id=?").bind(owner).fetch_optional(&mut **tx).await? else{return Ok(())};
    let id: i64 = row.try_get("family_id")?;
    let legacy: Option<String> = row.try_get("family_phone")?;
    let cipher: Option<String> = row.try_get("family_phone_cipher")?;
    let lookup: Option<String> = row.try_get("family_phone_lookup")?;
    let protected: bool = row.try_get("family_phone_protected")?;
    let phone = if protected {
        if legacy.is_some() {
            return Err("inconsistent protected phone".into());
        }
        match (cipher, lookup) {
            (None, None) => String::new(),
            (Some(cipher), Some(lookup)) => {
                let raw = keys
                    .open_scoped(
                        cipher.as_bytes(),
                        format!("account-phone:family:{id}").as_bytes(),
                    )
                    .map_err(|_| "phone authentication failed")?;
                let phone = String::from_utf8(raw)?;
                if !valid_phone(&phone)
                    || !bool::from(
                        keys.digest_scoped(b"account-phone", phone.as_bytes())
                            .as_bytes()
                            .ct_eq(lookup.as_bytes()),
                    )
                {
                    return Err("phone authentication failed".into());
                }
                phone
            }
            _ => return Err("inconsistent protected phone".into()),
        }
    } else {
        if cipher.is_some() || lookup.is_some() {
            return Err("inconsistent legacy phone".into());
        }
        legacy.unwrap_or_default()
    };
    let seal = |scope: &[u8]| -> Result<String, Error> {
        let cipher = keys
            .seal_scoped(phone.as_bytes(), scope)
            .map_err(|_| "recipient encryption failed")?;
        Ok(String::from_utf8(cipher)?)
    };
    if valid_phone(&phone) {
        let scope = b"care-notification-recipient";
        let index = keys.digest_scoped(scope, phone.as_bytes());
        let mut recipients = Vec::with_capacity(incidents.len());
        for incident in incidents {
            recipients.push((*incident, seal(scope)?));
        }
        let mut sql = QueryBuilder::<MySql>::new(
            "INSERT INTO care_notification_outbox(incident_id,channel,recipient,recipient_key) ",
        );
        sql.push_values(&recipients, |mut row, (incident, cipher)| {
            row.push_bind(*incident)
                .push("'sms'")
                .push_bind(cipher)
                .push_bind(&index);
        });
        sql.push(" ON DUPLICATE KEY UPDATE id=id");
        sql.build().execute(&mut **tx).await?;
    }
    let mut recipients = Vec::with_capacity(incidents.len());
    for incident in incidents {
        recipients.push((incident.to_string(), seal(b"notification-recipient")?));
    }
    let mut sql = QueryBuilder::<MySql>::new(
        "INSERT INTO message_outbox(topic,source_key,family_id,channel,recipient,status,delivered_at) ",
    );
    sql.push_values(&recipients, |mut row, (incident, cipher)| {
        row.push("'care'")
            .push_bind(incident)
            .push_bind(id)
            .push("'in_app'")
            .push_bind(cipher)
            .push("'delivered'")
            .push("CURRENT_TIMESTAMP(6)");
    });
    sql.push(" ON DUPLICATE KEY UPDATE id=id");
    sql.build().execute(&mut **tx).await?;
    Ok(())
}

pub(crate) async fn commit(pool: &MySqlPool, keys: &Keys, raw: &[u8]) -> Result<(), Error> {
    let plain = keys
        .open_scoped(raw, b"yihe-next-sos-event-v1")
        .map_err(|_| "SOS authentication failed")?;
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
    {
        return Err("invalid SOS event".into());
    }
    let (received, canonical) = go_time(&e.received_at)?;
    e.received_at = canonical;
    let digest = keys.digest_scoped(b"yihe-next-sos-receipt-v1", &serde_json::to_vec(&e)?);
    let mut tx = pool.begin().await?;
    if sqlx::query_scalar::<_, i64>("SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE")
        .bind(e.resident_id)
        .fetch_optional(&mut *tx)
        .await?
        .is_none()
    {
        return reject(tx, &e, &digest, "resident_missing").await;
    }
    let Some(row) = sqlx::query(
        "SELECT old_id,status,health_epoch,bind_time FROM device WHERE device_id=? FOR UPDATE",
    )
    .bind(e.device_id)
    .fetch_optional(&mut *tx)
    .await?
    else {
        return reject(tx, &e, &digest, "device_missing").await;
    };
    let owner: Option<i64> = row.try_get("old_id")?;
    let status: Option<i64> = row.try_get("status")?;
    let epoch: i64 = row.try_get("health_epoch")?;
    let bound: Option<chrono::NaiveDateTime> = row.try_get("bind_time")?;
    if owner != Some(e.resident_id)
        || status.is_none()
        || status == Some(0)
        || epoch != e.binding_epoch
        || bound.is_some_and(|t| received.naive_utc() < t)
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
    if sqlx::query_scalar::<_,String>("SELECT reason FROM rust_sos_rejection WHERE device_id=? AND binding_epoch=? AND request_id=? AND payload_digest=? FOR UPDATE")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).bind(digest.as_bytes()).fetch_optional(&mut *tx).await?.is_some(){tx.commit().await?;return Ok(())}
    if let Some(previous)=sqlx::query_scalar::<_,Vec<u8>>("SELECT payload_digest FROM rust_sos_receipt WHERE device_id=? AND binding_epoch=? AND request_id=? FOR UPDATE")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).fetch_optional(&mut *tx).await?{
        if previous!=digest.as_bytes(){return reject(tx,&e,&digest,"request_id_conflict").await}
        tx.commit().await?;return Ok(())
    }
    // Reuse the actual Go incident request identity, including historical
    // receipts. Do not enqueue a second set of notifications on a retry.
    let prior: Option<(i64, i64, String)> = sqlx::query_as(
        "SELECT id,old_id,kind FROM care_incident WHERE device_id=? AND request_id=? FOR UPDATE",
    )
    .bind(e.device_id)
    .bind(&e.request_id)
    .fetch_optional(&mut *tx)
    .await?;
    let incident = if let Some((id, owner, kind)) = prior {
        if owner != e.resident_id || kind != "sos" {
            return reject(tx, &e, &digest, "request_id_conflict").await;
        }
        id
    } else {
        let metadata = keys
            .seal_scoped(br#"{"version":1,"validity":"missing"}"#, b"location")
            .map_err(|_| "location encryption failed")?;
        let alarm=i64::try_from(sqlx::query("INSERT INTO alarm_precess(status,old_id,device_id,be_time,lng,lat,kind,position_metadata) VALUES('1',?,?,?,NULL,NULL,'sos',?)")
            .bind(e.resident_id).bind(e.device_id).bind(received.naive_utc()).bind(String::from_utf8(metadata)?).execute(&mut *tx).await?.last_insert_id())?;
        sqlx::query("INSERT INTO resident_location_alert_state(old_id,alarm_id,active,observed_at) VALUES(?,?,true,?) AS new ON DUPLICATE KEY UPDATE active=CASE WHEN resident_location_alert_state.observed_at IS NULL OR new.observed_at>resident_location_alert_state.observed_at OR (new.observed_at=resident_location_alert_state.observed_at AND new.alarm_id>resident_location_alert_state.alarm_id) THEN true ELSE resident_location_alert_state.active END,alarm_id=CASE WHEN resident_location_alert_state.observed_at IS NULL OR new.observed_at>resident_location_alert_state.observed_at OR (new.observed_at=resident_location_alert_state.observed_at AND new.alarm_id>resident_location_alert_state.alarm_id) THEN new.alarm_id ELSE resident_location_alert_state.alarm_id END,observed_at=CASE WHEN resident_location_alert_state.observed_at IS NULL OR new.observed_at>resident_location_alert_state.observed_at OR (new.observed_at=resident_location_alert_state.observed_at AND new.alarm_id>=resident_location_alert_state.alarm_id) THEN new.observed_at ELSE resident_location_alert_state.observed_at END")
            .bind(e.resident_id).bind(alarm).bind(received.naive_utc()).execute(&mut *tx).await?;
        native::touch(&mut tx, e.device_id, e.binding_epoch, received.naive_utc())
            .await
            .map_err(|_| "heartbeat transaction failed")?;
        let id=i64::try_from(sqlx::query("INSERT INTO care_incident(source_alarm_id,old_id,device_id,request_id) VALUES(?,?,?,?)")
            .bind(alarm).bind(e.resident_id).bind(e.device_id).bind(&e.request_id).execute(&mut *tx).await?.last_insert_id())?;
        sqlx::query("INSERT INTO care_incident_event(incident_id,actor,action,note) VALUES(?,'device','received','SOS 已持久化，尚无通知送达或人工接单确认')")
            .bind(id).execute(&mut *tx).await?;
        notify(&mut tx, keys, id, e.resident_id).await?;
        id
    };
    sqlx::query("INSERT INTO rust_sos_receipt(device_id,binding_epoch,request_id,payload_digest,incident_id) VALUES(?,?,?,?,?)")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).bind(digest.as_bytes()).bind(incident).execute(&mut *tx).await?;
    tx.commit().await?;
    Ok(())
}
