//! Disposable-schema consumer of the existing step-only encrypted lane.
//! No claim of full telemetry or production rules/notification parity.
use crate::{native, outbox::Keys};
use async_nats::jetstream::{AckKind, consumer::PullConsumer};
use chrono::{DateTime, SecondsFormat, Utc};
use futures_util::StreamExt;
use serde::{Deserialize, Serialize};
use sqlx::{MySql, MySqlPool, Row, Transaction};
use std::{net::SocketAddr, time::Duration};

pub(crate) type Error = Box<dyn std::error::Error + Send + Sync>;
#[derive(Deserialize, Serialize)]
#[serde(rename_all = "PascalCase", deny_unknown_fields)]
struct Event {
    #[serde(rename = "DeviceID")]
    device_id: i64,
    #[serde(rename = "ResidentID")]
    resident_id: i64,
    binding_epoch: i64,
    auth_generation: i64,
    #[serde(rename = "RequestID")]
    request_id: String,
    payload: Payload,
}
#[derive(Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct Payload {
    received_at: String,
    reported_at: String,
    step: i64,
}
pub(crate) fn go_time(value: &str) -> Result<(DateTime<Utc>, String), Error> {
    // This experiment admits only UTC times; Go's RFC3339Nano omits
    // trailing zeroes in fractional seconds. Reject other time bases
    // rather than assign a different receipt digest to the same event.
    if !value.ends_with('Z') {
        return Err("non-UTC event time".into());
    }
    let at = DateTime::parse_from_rfc3339(value)?.with_timezone(&Utc);
    if at.timestamp() <= 0 {
        return Err("invalid event time".into());
    }
    let full = at.to_rfc3339_opts(SecondsFormat::Nanos, true);
    let canon = full
        .strip_suffix('Z')
        .ok_or("invalid time")?
        .trim_end_matches('0')
        .trim_end_matches('.');
    Ok((at, format!("{canon}Z")))
}
async fn reject(
    mut tx: Transaction<'_, MySql>,
    e: &Event,
    digest: &str,
    reason: &str,
) -> Result<(), Error> {
    sqlx::query("INSERT INTO broker_telemetry_rejection(device_id,binding_epoch,request_id,payload_digest,reason) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE reason=reason")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).bind(digest.as_bytes()).bind(reason).execute(&mut *tx).await?;
    tx.commit().await?;
    Ok(())
}
async fn commit(pool: &MySqlPool, keys: &Keys, raw: &[u8]) -> Result<(), Error> {
    let plain = keys.open(raw).map_err(|_| "event authentication failed")?;
    let mut e: Event = serde_json::from_slice(&plain)?;
    if e.device_id <= 0
        || e.resident_id <= 0
        || e.binding_epoch <= 0
        || e.auth_generation <= 0
        || e.request_id.is_empty()
        || e.request_id.len() > 64
        || !e
            .request_id
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"_.:-".contains(&b))
        || !(0..=200000).contains(&e.payload.step)
    {
        return Err("unsupported step event".into());
    }
    let (received, rc) = go_time(&e.payload.received_at)?;
    let (reported, pc) = go_time(&e.payload.reported_at)?;
    e.payload.received_at = rc;
    e.payload.reported_at = pc;
    let digest = keys.receipt_digest(&serde_json::to_vec(&e)?);
    let mut tx = pool.begin().await?;
    // Same resident -> device -> authorization generation lock order as Go.
    if sqlx::query_scalar::<_, i64>("SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE")
        .bind(e.resident_id)
        .fetch_optional(&mut *tx)
        .await?
        .is_none()
    {
        return reject(tx, &e, &digest, "resident_missing").await;
    }
    let Some(row) =
        sqlx::query("SELECT old_id,status,health_epoch FROM device WHERE device_id=? FOR UPDATE")
            .bind(e.device_id)
            .fetch_optional(&mut *tx)
            .await?
    else {
        return reject(tx, &e, &digest, "device_missing").await;
    };
    let owner: Option<i64> = row.try_get("old_id")?;
    let status: Option<i64> = row.try_get("status")?;
    let epoch: i64 = row.try_get("health_epoch")?;
    if owner != Some(e.resident_id)
        || status.is_none()
        || status == Some(0)
        || epoch != e.binding_epoch
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
    if sqlx::query_scalar::<_,String>("SELECT reason FROM broker_telemetry_rejection WHERE device_id=? AND binding_epoch=? AND request_id=? AND payload_digest=? FOR UPDATE")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).bind(digest.as_bytes()).fetch_optional(&mut *tx).await?.is_some(){tx.commit().await?;return Ok(())}
    if let Some(previous)=sqlx::query_scalar::<_,Vec<u8>>("SELECT payload_digest FROM broker_telemetry_receipt WHERE device_id=? AND binding_epoch=? AND request_id=? FOR UPDATE")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).fetch_optional(&mut *tx).await?{
        if previous!=digest.as_bytes(){return reject(tx,&e,&digest,"request_id_conflict").await}
        tx.commit().await?;return Ok(())
    }
    native::persist_step(
        &mut tx,
        e.device_id,
        e.resident_id,
        e.binding_epoch,
        reported.naive_utc(),
        e.payload.step,
    )
    .await
    .map_err(|_| "step transaction failed")?;
    native::touch(&mut tx, e.device_id, e.binding_epoch, received.naive_utc())
        .await
        .map_err(|_| "heartbeat transaction failed")?;
    sqlx::query("INSERT INTO broker_telemetry_receipt(device_id,binding_epoch,request_id,payload_digest) VALUES(?,?,?,?)")
        .bind(e.device_id).bind(e.binding_epoch).bind(e.request_id.as_bytes()).bind(digest.as_bytes()).execute(&mut *tx).await?;
    tx.commit().await?;
    Ok(())
}
async fn one(
    pool: &MySqlPool,
    keys: &Keys,
    consumer: &PullConsumer,
    crash: bool,
    vital: bool,
) -> Result<bool, Error> {
    let mut messages = consumer
        .fetch()
        .max_messages(1)
        .expires(Duration::from_secs(2))
        .messages()
        .await?;
    let Some(message) = messages.next().await else {
        return Ok(false);
    };
    let msg = message?;
    // AckWait is 30s. A bounded attempt cannot monopolize the consumer.
    let work = async {
        if vital {
            crate::vitals::commit(pool, keys, &msg.payload).await
        } else {
            commit(pool, keys, &msg.payload).await
        }
    };
    match tokio::time::timeout(Duration::from_secs(20), work).await {
        Ok(Ok(())) => {}
        _ => {
            msg.ack_with(AckKind::Nak(Some(Duration::from_secs(1))))
                .await?;
            return Err("consumer transaction not confirmed; event retained".into());
        }
    }
    if crash {
        std::process::exit(78)
    }
    msg.double_ack().await?;
    Ok(true)
}
pub async fn run(
    address: SocketAddr,
    worker: bool,
    crash: bool,
    vital: bool,
) -> Result<(), Box<dyn std::error::Error>> {
    if !address.ip().is_loopback() || address.port() == 0 {
        return Err("test broker must be loopback".into());
    }
    let pool = native::test_pool().await?;
    let keys = Keys::load()?;
    let client = tokio::time::timeout(
        Duration::from_secs(5),
        async_nats::connect(format!("nats://{address}")),
    )
    .await??;
    let js = async_nats::jetstream::new(client);
    let consumer: PullConsumer = js
        .get_stream(if vital {
            "YIHE_vitals"
        } else {
            "YIHE_telemetry"
        })
        .await?
        .get_consumer("commit")
        .await
        .map_err(|e| e.to_string())?;
    let config = &consumer.cached_info().config;
    if config.filter_subject
        != (if vital {
            "yihe.next.vitals"
        } else {
            "yihe.next.telemetry"
        })
        || config.ack_policy != async_nats::jetstream::consumer::AckPolicy::Explicit
        || config.ack_wait != Duration::from_secs(30)
        || config.max_deliver != -1
    {
        return Err("unexpected consumer configuration".into());
    }
    if worker {
        let stop = tokio::signal::ctrl_c();
        tokio::pin!(stop);
        loop {
            let delay = match one(&pool, &keys, &consumer, false, vital).await {
                Ok(_) => Duration::ZERO,
                Err(_) => {
                    eprintln!("consumer attempt failed; event retained");
                    Duration::from_secs(1)
                }
            };
            tokio::select! {biased; result=&mut stop=>{result?;break;},_=tokio::time::sleep(delay)=>{}}
        }
    } else {
        // A process that exited after a failed commit may have lost its
        // asynchronous NAK. Allow the full broker AckWait for redelivery.
        // An actual transaction/transport error still exits immediately.
        let deadline = tokio::time::Instant::now() + Duration::from_secs(35);
        loop {
            match one(&pool, &keys, &consumer, crash, vital).await {
                Ok(true) => {
                    println!("committed");
                    break;
                }
                Ok(false) if tokio::time::Instant::now() < deadline => continue,
                Ok(false) => return Err("no event received before redelivery deadline".into()),
                Err(e) => return Err(e.to_string().into()),
            }
        }
    }
    pool.close().await;
    Ok(())
}
