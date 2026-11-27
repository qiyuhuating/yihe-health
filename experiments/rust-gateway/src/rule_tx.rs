//! Local transaction differential for trusted committed-observation fixtures.
//! No production scheduler, arbitrary network candidate API or policy store.
use crate::{analytics, native, outbox::Keys, sos};
use serde::Deserialize;
use serde_json::{Value, json};
use sqlx::{MySql, MySqlPool, QueryBuilder, Row, Transaction, mysql::MySqlRow};
use std::io::{BufRead, Write};
use yihe_core::rules::{Candidate, State, Transition};
type Error = Box<dyn std::error::Error>;

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct Evidence {
    pub(crate) kind: String,
    pub(crate) key: String,
    pub(crate) data: serde_json::Map<String, Value>,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct Batch {
    pub(crate) device: i64,
    pub(crate) owner: i64,
    pub(crate) epoch: i64,
    pub(crate) request_id: String,
    pub(crate) input: Value,
    pub(crate) evidence: Vec<Evidence>,
}

struct PendingState<'a> {
    kind: &'static str,
    key: &'a str,
    streak: i64,
    active: bool,
    incident: Option<i64>,
    evidence: String,
    activation: Option<(i64, String)>,
}

async fn prepare_state<'a>(
    tx: &mut Transaction<'_, MySql>,
    keys: &Keys,
    b: &Batch,
    c: Candidate,
    e: &'a Evidence,
    prior: Option<&MySqlRow>,
    canonical: &str,
) -> Result<Option<PendingState<'a>>, Error> {
    let mut state = State::default();
    let mut incident: Option<i64> = None;
    if let Some(row) = prior {
        let previous: String = row.try_get("observation_key")?;
        if previous == e.key {
            return Ok(None);
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
    let mut activation = None;
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
        activation = Some((id, String::from_utf8(note)?));
        incident = Some(id);
    }
    let mut data = e.data.clone();
    data.insert("breach".into(), json!(c.breach));
    data.insert("streak".into(), json!(state.streak));
    data.insert("required".into(), json!(c.required));
    data.insert("ownerId".into(), json!(b.owner));
    data.insert("at".into(), json!(canonical));
    let cipher = keys
        .seal_scoped(&serde_json::to_vec(&data)?, b"rule-evidence")
        .map_err(|_| "rule evidence encryption failed")?;
    let wrapped = json!({"encrypted":String::from_utf8(cipher)?}).to_string();
    Ok(Some(PendingState {
        kind: c.kind.name(),
        key: &e.key,
        streak: i64::try_from(state.streak)?,
        active: state.active,
        incident,
        evidence: wrapped,
        activation,
    }))
}

async fn write_activations(
    tx: &mut Transaction<'_, MySql>,
    keys: &Keys,
    b: &Batch,
    pending: &[PendingState<'_>],
) -> Result<(), Error> {
    let activations: Vec<_> = pending
        .iter()
        .filter_map(|p| p.activation.as_ref())
        .collect();
    if activations.is_empty() {
        return Ok(());
    }
    let mut sql = QueryBuilder::<MySql>::new(
        "INSERT INTO care_incident_event(incident_id,actor,action,note) ",
    );
    sql.push_values(&activations, |mut row, (incident, note)| {
        row.push_bind(*incident)
            .push("'rule'")
            .push("'received'")
            .push_bind(note);
    });
    sql.build().execute(&mut **tx).await?;
    let incidents: Vec<_> = activations.iter().map(|(id, _)| *id).collect();
    sos::notify_batch(tx, keys, &incidents, b.owner)
        .await
        .map_err(|_| "rule notification failed")?;
    Ok(())
}
async fn write_states(
    tx: &mut Transaction<'_, MySql>,
    b: &Batch,
    pending: &[PendingState<'_>],
    now: chrono::DateTime<chrono::Utc>,
) -> Result<(), Error> {
    if pending.is_empty() {
        return Ok(());
    }
    let mut query = QueryBuilder::<MySql>::new(
        "INSERT INTO rule_state(device_id,epoch,kind,observation_key,streak,active,incident_id,evidence,updated_at) ",
    );
    query.push_values(pending, |mut row, p| {
        row.push_bind(b.device)
            .push_bind(b.epoch)
            .push_bind(p.kind)
            .push_bind(p.key)
            .push_bind(p.streak)
            .push_bind(p.active)
            .push_bind(p.incident)
            .push_bind(&p.evidence)
            .push_bind(now.naive_utc());
    });
    query.push(" AS new ON DUPLICATE KEY UPDATE observation_key=new.observation_key,streak=new.streak,active=new.active,incident_id=new.incident_id,evidence=new.evidence,updated_at=new.updated_at");
    query.build().execute(&mut **tx).await?;
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
    persist_batch(&mut tx, keys, &b, &candidates, now, &digest).await?;
    tx.commit().await?;
    Ok(())
}
pub(crate) async fn persist_batch(
    tx: &mut Transaction<'_, MySql>,
    keys: &Keys,
    b: &Batch,
    candidates: &yihe_core::rules::Candidates,
    now: chrono::DateTime<chrono::Utc>,
    digest: &str,
) -> Result<(), Error> {
    if candidates.as_slice().len() != b.evidence.len()
        || candidates
            .as_slice()
            .iter()
            .zip(&b.evidence)
            .any(|(c, e)| c.kind.name() != e.kind || e.key.is_empty() || e.key.len() > 96)
    {
        return Err("candidate evidence mismatch".into());
    }
    if !candidates.as_slice().is_empty() {
        // The resident/device fence serializes the whole epoch. Lock existing
        // states in primary-key order once; no per-candidate round trips.
        let prior=sqlx::query("SELECT kind,observation_key,streak,active,incident_id,CAST(evidence AS CHAR) AS evidence FROM rule_state WHERE device_id=? AND epoch=? ORDER BY kind FOR UPDATE")
            .bind(b.device).bind(b.epoch).fetch_all(&mut **tx).await?;
        let canonical =
            crate::consumer::go_time(&now.to_rfc3339_opts(chrono::SecondsFormat::Nanos, true))
                .map_err(|_| "invalid observation clock")?
                .1;
        let mut pending = Vec::with_capacity(candidates.as_slice().len());
        for (c, e) in candidates.as_slice().iter().zip(&b.evidence) {
            let mut previous = None;
            for row in &prior {
                if row.try_get::<&str, _>("kind")? == c.kind.name() {
                    previous = Some(row);
                    break;
                }
            }
            if let Some(update) = prepare_state(tx, keys, b, *c, e, previous, &canonical).await? {
                pending.push(update);
            }
        }
        write_activations(tx, keys, b, &pending).await?;
        write_states(tx, b, &pending, now).await?;
    }
    sqlx::query(
        "INSERT INTO rust_rule_receipt(device_id,epoch,request_id,payload_digest) VALUES(?,?,?,?)",
    )
    .bind(b.device)
    .bind(b.epoch)
    .bind(b.request_id.as_bytes())
    .bind(digest.as_bytes())
    .execute(&mut **tx)
    .await?;
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
