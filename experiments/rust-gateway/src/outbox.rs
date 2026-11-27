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
use sqlx::{MySqlPool, Row};
use std::{net::SocketAddr, sync::Arc, time::Duration};
use tokio::sync::Semaphore;

const SCOPE: &[u8] = b"yihe-next-telemetry-event-v1";
const SUBJECT: &str = "yihe.next.telemetry";
const STREAM: &str = "YIHE_telemetry";

struct Keys {
    cipher: Aes256Gcm,
    lookup: [u8; 32],
}
impl Keys {
    fn load() -> Result<Self, Box<dyn std::error::Error>> {
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
    let (mut tx, id, owner) = native::authorize(pool, &v).await?;
    let generation: i64 = sqlx::query_scalar(
        "SELECT generation FROM broker_device_generation WHERE device_id=? FOR UPDATE",
    )
    .bind(id)
    .fetch_one(&mut *tx)
    .await?;
    if generation <= 0 {
        return Err(Failure::Database);
    }
    let observed = v
        .observed
        .and_utc()
        .to_rfc3339_opts(SecondsFormat::Secs, true);
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
pub async fn router() -> Result<Router, Box<dyn std::error::Error>> {
    let pool = native::test_pool().await?;
    let keys = Arc::new(Keys::load()?);
    Ok(Router::new()
        .route("/api/v1/device/report", post(enqueue))
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
    if !address.ip().is_loopback() || address.port() == 0 {
        return Err("test broker must be loopback".into());
    }
    let pool = native::test_pool().await?;
    let Some(row) = sqlx::query(
        "SELECT id,encoded FROM rust_step_outbox WHERE published_at IS NULL ORDER BY id LIMIT 1",
    )
    .fetch_optional(&pool)
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
    let client = tokio::time::timeout(
        Duration::from_secs(5),
        async_nats::connect(format!("nats://{address}")),
    )
    .await??;
    let js = async_nats::jetstream::new(client);
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
        .bind(id).bind(encoded).execute(&pool).await?;
    pool.close().await;
    Ok(true)
}
