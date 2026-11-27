//! Isolated durable admission and Rust JetStream publisher.
use crate::native::{self, Failure, Verified};
use aes_gcm::{
    Aes256Gcm, KeyInit, Nonce,
    aead::{Aead, Payload},
};
use axum::{
    Json, Router,
    body::to_bytes,
    extract::{Request, State},
    response::{IntoResponse, Response},
    routing::post,
};
use base64::{
    Engine,
    engine::general_purpose::{STANDARD, STANDARD_NO_PAD},
};
use chrono::{DateTime, SecondsFormat, Utc};
use hmac::{Hmac, Mac};
use serde_json::json;
use sha2::{Digest, Sha256};
use sqlx::{MySql, MySqlPool, Row, Transaction};
use std::{net::SocketAddr, sync::Arc, time::Duration};
use tokio::sync::Semaphore;

const SCOPE: &[u8] = b"yihe-next-telemetry-event-v1";
const SUBJECT: &str = "yihe.next.telemetry";
const STREAM: &str = "YIHE_telemetry";

pub(crate) struct Keys {
    cipher: Aes256Gcm,
    lookup: [u8; 32],
}
impl Keys {
    pub(crate) fn load() -> Result<Self, Box<dyn std::error::Error>> {
        // Only the unversioned single-key fixture is supported. No keyring
        // downgrade or accidental use of an unrelated production key.
        let key: [u8; 32] = STANDARD
            .decode(std::env::var("YIHE_NEXT_TEST_EVENT_KEY")?)?
            .try_into()
            .map_err(|_| "expected 32-byte test key")?;
        Ok(Self {
            cipher: Aes256Gcm::new_from_slice(&key).map_err(|_| "invalid key")?,
            lookup: key,
        })
    }
    fn seal(&self, plaintext: &[u8]) -> Result<Vec<u8>, Failure> {
        let mut nonce = [0u8; 12];
        getrandom::fill(&mut nonce).map_err(|_| Failure::Database)?;
        let ciphertext = self
            .cipher
            .encrypt(
                Nonce::from_slice(&nonce),
                Payload {
                    msg: plaintext,
                    aad: SCOPE,
                },
            )
            .map_err(|_| Failure::Database)?;
        let mut data = nonce.to_vec();
        data.extend(ciphertext);
        Ok(format!("sne1:{}", STANDARD_NO_PAD.encode(data)).into_bytes())
    }
    pub(crate) fn open(&self, encoded: &[u8]) -> Result<Vec<u8>, Box<dyn std::error::Error>> {
        if encoded.len() > 256 * 1024 {
            return Err("oversize event".into());
        }
        let data = STANDARD_NO_PAD.decode(
            encoded
                .strip_prefix(b"sne1:")
                .ok_or("unsupported event envelope")?,
        )?;
        if data.len() < 28 {
            return Err("short event envelope".into());
        }
        self.cipher
            .decrypt(
                Nonce::from_slice(&data[..12]),
                Payload {
                    msg: &data[12..],
                    aad: SCOPE,
                },
            )
            .map_err(|_| "event authentication failed".into())
    }
    pub(crate) fn receipt_digest(&self, canonical: &[u8]) -> String {
        let mut mac = <Hmac<Sha256> as Mac>::new_from_slice(&self.lookup).expect("32-byte key");
        mac.update(b"yihe-next-telemetry-receipt-v1\0");
        mac.update(canonical);
        hex::encode(mac.finalize().into_bytes())
    }
    fn fingerprint(&self, data: &[u8]) -> [u8; 32] {
        let mut mac = <Hmac<Sha256> as Mac>::new_from_slice(&self.lookup).expect("32-byte key");
        mac.update(b"yihe-next-outbox-admission-v1\0");
        mac.update(data);
        mac.finalize().into_bytes().into()
    }
}

async fn admit(
    pool: &MySqlPool,
    keys: &Keys,
    v: Verified,
    now: DateTime<Utc>,
) -> Result<(), Failure> {
    let r = &v.report;
    let request_id = r
        .request_id
        .as_deref()
        .filter(|id| {
            !id.is_empty()
                && id.len() <= 64
                && id
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b"_.:-".contains(&b))
        })
        .ok_or(Failure::Invalid)?;
    let (tx, id, owner) = native::authorize(pool, &v).await?;
    admit_locked(
        tx,
        id,
        owner,
        keys,
        StepInput {
            epoch: r.epoch,
            request_id,
            observed: v.observed.and_utc(),
            step: r.step,
        },
        now,
    )
    .await
}

struct StepInput<'a> {
    epoch: i64,
    request_id: &'a str,
    observed: DateTime<Utc>,
    step: i64,
}

async fn admit_locked(
    mut tx: Transaction<'_, MySql>,
    id: i64,
    owner: i64,
    keys: &Keys,
    r: StepInput<'_>,
    now: DateTime<Utc>,
) -> Result<(), Failure> {
    let request_id = r.request_id;
    let generation: i64 = sqlx::query_scalar(
        "SELECT generation FROM broker_device_generation WHERE device_id=? FOR UPDATE",
    )
    .bind(id)
    .fetch_one(&mut *tx)
    .await?;
    if generation <= 0 {
        return Err(Failure::Database);
    }
    let observed = r.observed.to_rfc3339_opts(SecondsFormat::Secs, true);
    let semantic = json!([id, owner, r.epoch, generation, request_id, observed, r.step]);
    let fingerprint =
        keys.fingerprint(&serde_json::to_vec(&semantic).map_err(|_| Failure::Database)?);
    // The device row serializes admissions; the durable request record
    // survives dispatch. A fresh nonce retry must not re-seal or change time.
    if let Some(row)=sqlx::query("SELECT fingerprint FROM rust_step_outbox WHERE device_id=? AND binding_epoch=? AND request_id=?")
        .bind(id).bind(r.epoch).bind(request_id.as_bytes()).fetch_optional(&mut *tx).await? {
        let prior:Vec<u8>=row.try_get("fingerprint")?;
        if prior!=fingerprint{return Err(Failure::Conflict)}
        tx.commit().await?;
        return Ok(())
    }
    let pending: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM rust_step_outbox WHERE device_id=? AND published_at IS NULL",
    )
    .bind(id)
    .fetch_one(&mut *tx)
    .await?;
    if pending >= 64 {
        return Err(Failure::Busy);
    }
    let event = json!({"DeviceID":id,"ResidentID":owner,"BindingEpoch":r.epoch,"AuthGeneration":generation,
        "RequestID":request_id,"Payload":{"receivedAt":now.to_rfc3339_opts(SecondsFormat::Micros,true),
        "reportedAt":observed,"step":r.step}});
    let encoded = keys.seal(&serde_json::to_vec(&event).map_err(|_| Failure::Database)?)?;
    sqlx::query("INSERT INTO rust_step_outbox(device_id,binding_epoch,request_id,fingerprint,encoded) VALUES(?,?,?,?,?)")
        .bind(id).bind(r.epoch).bind(request_id.as_bytes()).bind(fingerprint.as_slice()).bind(encoded)
        .execute(&mut *tx).await?;
    tx.commit().await?;
    Ok(())
}

#[derive(Clone)]
struct Admission {
    pool: MySqlPool,
    keys: Arc<Keys>,
    gate: Arc<Semaphore>,
}
async fn enqueue(State(s): State<Admission>, request: Request) -> Result<Response, Failure> {
    let _permit = s.gate.try_acquire().map_err(|_| Failure::Busy)?;
    let (parts, body) = request.into_parts();
    let raw = tokio::time::timeout(Duration::from_secs(10), to_bytes(body, 1 << 20))
        .await
        .map_err(|_| Failure::Invalid)?
        .map_err(|_| Failure::Invalid)?;
    let now = Utc::now();
    let v = native::verify(&parts.headers, &raw, now)?;
    admit(&s.pool, &s.keys, v, now).await?;
    // Explicit admission only, not a claim of consumer/business commit.
    Ok((
        axum::http::StatusCode::ACCEPTED,
        Json(json!({"returnCode":"1111","state":"admitted"})),
    )
        .into_response())
}
async fn enqueue_binary(State(s): State<Admission>, request: Request) -> Result<Response, Failure> {
    let _permit = s.gate.try_acquire().map_err(|_| Failure::Busy)?;
    let (parts, body) = request.into_parts();
    if parts
        .headers
        .get("content-type")
        .and_then(|v| v.to_str().ok())
        != Some(yihe_device_wire::CONTENT_TYPE)
    {
        return Err(Failure::Invalid);
    }
    let raw = tokio::time::timeout(
        Duration::from_secs(10),
        to_bytes(body, yihe_device_wire::MAX_PACKET),
    )
    .await
    .map_err(|_| Failure::Invalid)?
    .map_err(|_| Failure::Invalid)?;
    let now = Utc::now();
    let (step, envelope) =
        yihe_device_wire::decode(&raw, now.timestamp()).map_err(|e| match e {
            yihe_device_wire::Error::Signature => Failure::Signature,
            yihe_device_wire::Error::Invalid => Failure::Invalid,
        })?;
    // Authenticate before row-lock acquisition, then recheck the same
    // enrolled public key and its generation under resident/device locks.
    let key:Vec<u8>=sqlx::query_scalar("SELECT k.public_key FROM rust_device_key k JOIN device d ON d.device_id=k.device_id JOIN broker_device_generation g ON g.device_id=k.device_id AND g.generation=k.generation WHERE d.device_no=?")
        .bind(&step.device_no).fetch_optional(&s.pool).await?.ok_or(Failure::Credentials)?;
    yihe_device_wire::verify(&envelope, &key).map_err(|_| Failure::Signature)?;
    let (mut tx, id, owner) =
        native::lock_device(&s.pool, &step.device_no, step.binding_epoch, None).await?;
    let generation: i64 = sqlx::query_scalar(
        "SELECT generation FROM broker_device_generation WHERE device_id=? FOR UPDATE",
    )
    .bind(id)
    .fetch_one(&mut *tx)
    .await?;
    let locked_key: Vec<u8> = sqlx::query_scalar(
        "SELECT public_key FROM rust_device_key WHERE device_id=? AND generation=? FOR UPDATE",
    )
    .bind(id)
    .bind(generation)
    .fetch_optional(&mut *tx)
    .await?
    .ok_or(Failure::Credentials)?;
    yihe_device_wire::verify(&envelope, &locked_key).map_err(|_| Failure::Signature)?;
    let expires =
        DateTime::from_timestamp(step.signed_at.checked_add(300).ok_or(Failure::Invalid)?, 0)
            .ok_or(Failure::Invalid)?
            .naive_utc();
    let observed = DateTime::from_timestamp(step.observed_at, 0).ok_or(Failure::Invalid)?;
    let mut hash = Sha256::new();
    hash.update(step.device_no.as_bytes());
    hash.update([0]);
    hash.update(step.nonce.as_bytes());
    native::reserve_nonce(&mut tx, id, &hash.finalize().into(), expires).await?;
    admit_locked(
        tx,
        id,
        owner,
        &s.keys,
        StepInput {
            epoch: step.binding_epoch,
            request_id: &step.request_id,
            observed,
            step: i64::from(step.step.ok_or(Failure::Invalid)?),
        },
        now,
    )
    .await?;
    Ok((
        axum::http::StatusCode::ACCEPTED,
        Json(json!({"returnCode":"1111","state":"admitted"})),
    )
        .into_response())
}

pub async fn router() -> Result<Router, Box<dyn std::error::Error>> {
    let pool = native::test_pool().await?;
    let keys = Arc::new(Keys::load()?);
    Ok(Router::new()
        .route("/api/v1/device/report", post(enqueue))
        .route(yihe_device_wire::PATH, post(enqueue_binary))
        .with_state(Admission {
            pool,
            keys,
            gate: Arc::new(Semaphore::new(8)),
        }))
}

/// Dispatch one row, with no transaction or SQL lock held during broker I/O.
/// Two dispatchers can publish the same exact ciphertext safely. The consumer
/// receipt, not the finite broker dedup window, is the final mutation fence.
pub async fn dispatch_one(
    address: SocketAddr,
    exit_after_ack: bool,
) -> Result<bool, Box<dyn std::error::Error>> {
    let pool = native::test_pool().await?;
    let js = connect(address).await?;
    dispatch_pending(&pool, &js, exit_after_ack).await
}

async fn connect(
    address: SocketAddr,
) -> Result<async_nats::jetstream::Context, Box<dyn std::error::Error>> {
    if !address.ip().is_loopback() || address.port() == 0 {
        return Err("test broker must be loopback".into());
    }
    let client = tokio::time::timeout(
        Duration::from_secs(5),
        async_nats::connect(format!("nats://{address}")),
    )
    .await??;
    Ok(async_nats::jetstream::new(client))
}

/// Reuse SQL and NATS connections. Limit idle polling and retry delays; no
/// SQL transaction spans network I/O. Stop between attempts on Ctrl-C.
pub async fn dispatch_worker(address: SocketAddr) -> Result<(), Box<dyn std::error::Error>> {
    if !address.ip().is_loopback() || address.port() == 0 {
        return Err("test broker must be loopback".into());
    }
    let pool = native::test_pool().await?;
    let mut js = None;
    let mut idle = Duration::from_millis(100);
    let mut retry = Duration::from_millis(250);
    let stop = tokio::signal::ctrl_c();
    tokio::pin!(stop);
    loop {
        let result = async {
            if js.is_none() {
                js = Some(connect(address).await?);
            }
            dispatch_pending(&pool, js.as_ref().expect("connected"), false).await
        }
        .await;
        let delay = match result {
            Ok(true) => {
                idle = Duration::from_millis(100);
                retry = Duration::from_millis(250);
                Duration::ZERO
            }
            Ok(false) => {
                retry = Duration::from_millis(250);
                let delay = idle;
                idle = (idle * 2).min(Duration::from_secs(2));
                delay
            }
            Err(_) => {
                eprintln!("dispatch attempt failed; pending event retained");
                js = None;
                let delay = retry;
                retry = (retry * 2).min(Duration::from_secs(5));
                delay
            }
        };
        tokio::select! {
            biased;
            result=&mut stop=>{result?;break;}
            _=tokio::time::sleep(delay)=>{}
        }
    }
    pool.close().await;
    Ok(())
}

async fn dispatch_pending(
    pool: &MySqlPool,
    js: &async_nats::jetstream::Context,
    exit_after_ack: bool,
) -> Result<bool, Box<dyn std::error::Error>> {
    let Some(row) = sqlx::query(
        "SELECT id,encoded FROM rust_step_outbox WHERE published_at IS NULL ORDER BY id LIMIT 1",
    )
    .fetch_optional(pool)
    .await?
    else {
        return Ok(false);
    };
    let id: i64 = row.try_get("id")?;
    let encoded: Vec<u8> = row.try_get("encoded")?;
    if encoded.is_empty() || encoded.len() > 256 * 1024 {
        return Err("invalid stored envelope size".into());
    }
    let digest = hex::encode(Sha256::digest(&encoded));
    let mut headers = async_nats::HeaderMap::new();
    headers.insert("Nats-Msg-Id", digest.as_str());
    headers.insert("Nats-Expected-Stream", STREAM);
    let ack = tokio::time::timeout(Duration::from_secs(5), async {
        js.publish_with_headers(SUBJECT, headers, encoded.clone().into())
            .await?
            .await
    })
    .await??;
    if ack.stream != STREAM {
        return Err("unexpected stream acknowledgement".into());
    }
    if exit_after_ack {
        // Test-only process termination after the real PubAck and before SQL.
        std::process::exit(77);
    }
    sqlx::query("UPDATE rust_step_outbox SET encoded=NULL,published_at=UTC_TIMESTAMP(6) WHERE id=? AND encoded=?")
        .bind(id).bind(encoded).execute(pool).await?;
    Ok(true)
}
