//! Local transaction differential for trusted committed-observation fixtures.
//! No production scheduler, arbitrary network candidate API or policy store.
use crate::{analytics, native, outbox::Keys, sos};
use serde::Deserialize;
use serde_json::{Value, json};
use sqlx::{MySql, MySqlPool, Row, Transaction};
use std::io::{BufRead, Write};
use yihe_core::rules::{Candidate, State, Transition};
type Error = Box<dyn std::error::Error>;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Evidence {
    kind: String,
    key: String,
    data: serde_json::Map<String, Value>,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Batch {
    device: i64,
    owner: i64,
    epoch: i64,
    request_id: String,
    input: Value,
    evidence: Vec<Evidence>,
}

async fn persist(
    tx: &mut Transaction<'_, MySql>,
    keys: &Keys,
    b: &Batch,
    c: Candidate,
    e: &Evidence,
    now: chrono::DateTime<chrono::Utc>,
) -> Result<(), Error> {
    let prior=sqlx::query("SELECT observation_key,streak,active,incident_id,CAST(evidence AS CHAR) AS evidence FROM rule_state WHERE device_id=? AND epoch=? AND kind=? FOR UPDATE")
        .bind(b.device).bind(b.epoch).bind(c.kind.name()).fetch_optional(&mut **tx).await?;
    let mut state = State::default();
    let mut incident: Option<i64> = None;
    if let Some(row) = prior {
        let previous: String = row.try_get("observation_key")?;
        if previous == e.key {
            return Ok(());
        }
        let streak: i64 = row.try_get("streak")?;
        let active: bool = row.try_get("active")?;
        incident = row.try_get("incident_id")?;
        let encoded: Option<String> = row.try_get("evidence")?;
        let mut source = None;
        if let Some(encoded) = encoded {
            let outer: Value = serde_json::from_str(&encoded)?;
            let cipher = outer
                .get("encrypted")
                .and_then(Value::as_str)
                .ok_or("protected prior evidence required")?;
            let plain = keys
                .open_scoped(cipher.as_bytes(), b"rule-evidence")
                .map_err(|_| "prior evidence authentication failed")?;
            let data: Value = serde_json::from_slice(&plain)?;
            source = data
                .get("source")
                .and_then(Value::as_str)
                .map(str::to_owned);
        }
        state = State {
            key: Some(previous),
            source,
            streak: u64::try_from(streak)?,
            active,
        };
    }
    let source = e.data.get("source").and_then(Value::as_str);
    let change = state.apply(c, &e.key, source);
    if change == Transition::Activate {
        let id = i64::try_from(
            sqlx::query(
                "INSERT INTO care_incident(old_id,device_id,kind,severity) VALUES(?,?,?,?)",
            )
            .bind(b.owner)
            .bind(b.device)
            .bind(format!("rule_{}", c.kind.name()))
            .bind(c.severity.name())
            .execute(&mut **tx)
            .await?
            .last_insert_id(),
        )?;
        let note = keys
            .seal_scoped(
                format!("服务端规则触发：{}", c.kind.name()).as_bytes(),
                b"care-text",
            )
            .map_err(|_| "rule note encryption failed")?;
        sqlx::query("INSERT INTO care_incident_event(incident_id,actor,action,note) VALUES(?,'rule','received',?)").bind(id).bind(String::from_utf8(note)?).execute(&mut **tx).await?;
        sos::notify(tx, keys, id, b.owner)
            .await
            .map_err(|_| "rule notification failed")?;
        incident = Some(id);
    }
    let mut data = e.data.clone();
    data.insert("breach".into(), json!(c.breach));
    data.insert("streak".into(), json!(state.streak));
    data.insert("required".into(), json!(c.required));
    data.insert("ownerId".into(), json!(b.owner));
    let canonical =
        crate::consumer::go_time(&now.to_rfc3339_opts(chrono::SecondsFormat::Nanos, true))
            .map_err(|_| "invalid observation clock")?
            .1;
    data.insert("at".into(), json!(canonical));
    let cipher = keys
        .seal_scoped(&serde_json::to_vec(&data)?, b"rule-evidence")
        .map_err(|_| "rule evidence encryption failed")?;
    let wrapped = json!({"encrypted":String::from_utf8(cipher)?}).to_string();
    sqlx::query("INSERT INTO rule_state(device_id,epoch,kind,observation_key,streak,active,incident_id,evidence,updated_at) VALUES(?,?,?,?,?,?,?,?,?) AS new ON DUPLICATE KEY UPDATE observation_key=new.observation_key,streak=new.streak,active=new.active,incident_id=new.incident_id,evidence=new.evidence,updated_at=new.updated_at")
        .bind(b.device).bind(b.epoch).bind(c.kind.name()).bind(&e.key).bind(i64::try_from(state.streak)?).bind(state.active).bind(incident).bind(wrapped).bind(now.naive_utc()).execute(&mut **tx).await?;
    Ok(())
}
async fn commit(pool: &MySqlPool, keys: &Keys, raw: &str) -> Result<(), Error> {
    let b: Batch = serde_json::from_str(raw)?;
    if b.device <= 0
        || b.owner <= 0
        || b.epoch <= 0
        || b.request_id.is_empty()
        || b.request_id.len() > 64
        || !b
            .request_id
            .bytes()
            .all(|v| v.is_ascii_alphanumeric() || b"_.:-".contains(&v))
    {
        return Err("invalid rule batch".into());
    }
    let now_ns = b
        .input
        .get("now_ns")
        .and_then(Value::as_i64)
        .ok_or("missing observation clock")?;
    let now = chrono::DateTime::<chrono::Utc>::from_timestamp_nanos(now_ns);
    let candidates = analytics::evaluate_input(b.input.clone())?;
    if candidates.as_slice().len() != b.evidence.len() {
        return Err("candidate evidence mismatch".into());
    }
    for (c, e) in candidates.as_slice().iter().zip(&b.evidence) {
        if c.kind.name() != e.kind || e.key.is_empty() || e.key.len() > 96 {
            return Err("candidate identity mismatch".into());
        }
    }
    let digest = keys.digest_scoped(b"yihe-next-rule-batch-v1", raw.as_bytes());
    let mut tx = pool.begin().await?;
    sqlx::query_scalar::<_, i64>("SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE")
        .bind(b.owner)
        .fetch_one(&mut *tx)
        .await?;
    let row =
        sqlx::query("SELECT old_id,status,health_epoch FROM device WHERE device_id=? FOR UPDATE")
            .bind(b.device)
            .fetch_one(&mut *tx)
            .await?;
    let owner: Option<i64> = row.try_get("old_id")?;
    let status: Option<i64> = row.try_get("status")?;
    let epoch: i64 = row.try_get("health_epoch")?;
    if owner != Some(b.owner) || status.is_none() || status == Some(0) || epoch != b.epoch {
        return Err("rule binding unavailable".into());
    }
    if let Some(prior)=sqlx::query_scalar::<_,Vec<u8>>("SELECT payload_digest FROM rust_rule_receipt WHERE device_id=? AND epoch=? AND request_id=? FOR UPDATE").bind(b.device).bind(b.epoch).bind(b.request_id.as_bytes()).fetch_optional(&mut *tx).await?{
        if prior!=digest.as_bytes(){return Err("rule batch conflict".into())}
        tx.commit().await?;return Ok(())
    }
    for (c, e) in candidates.as_slice().iter().zip(&b.evidence) {
        persist(&mut tx, keys, &b, *c, e, now).await?;
    }
    sqlx::query(
        "INSERT INTO rust_rule_receipt(device_id,epoch,request_id,payload_digest) VALUES(?,?,?,?)",
    )
    .bind(b.device)
    .bind(b.epoch)
    .bind(b.request_id.as_bytes())
    .bind(digest.as_bytes())
    .execute(&mut *tx)
    .await?;
    tx.commit().await?;
    Ok(())
}
pub async fn run() -> Result<(), Error> {
    let pool = native::test_pool().await?;
    let keys = Keys::load()?;
    let stdin = std::io::stdin().lock();
    let mut out = std::io::BufWriter::new(std::io::stdout().lock());
    for line in stdin.lines() {
        let line = line?;
        if line.len() > 64 * 1024 {
            return Err("rule batch too large".into());
        }
        // Fixed local diagnostic response. Never log protected evidence.
        let result = commit(&pool, &keys, &line).await;
        writeln!(
            out,
            "{}",
            if result.is_ok() {
                "committed"
            } else {
                "rejected"
            }
        )?;
        out.flush()?;
    }
    pool.close().await;
    Ok(())
}
