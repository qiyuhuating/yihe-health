//! Disposable-schema hybrid scheduler. Rust owns the fenced SQL transaction;
//! a trusted local Go adapter supplies the existing complex observation/window
//! and IANA-timezone semantics. No public route or production task takeover.
use crate::{
    analytics, native,
    outbox::Keys,
    rule_authority,
    rule_tx::{Batch, persist_batch},
};
use chrono::{DateTime, NaiveDateTime, Utc};
use serde::Deserialize;
use serde_json::{Value, json};
use sqlx::{MySqlPool, Row};
use std::{io::Write, time::Duration};
use tokio::io::{AsyncBufReadExt, BufReader, Lines, Stdin};
use yihe_core::rules;
type Error = Box<dyn std::error::Error>;
type Input = Lines<BufReader<Stdin>>;

#[derive(Deserialize)]
struct Reply {
    challenge: String,
    fallback: bool,
    owner_revision: Option<i64>,
    policy_revision: Option<i64>,
    policy: Option<Value>,
    next_due_ns: Option<i64>,
    batch: Option<Batch>,
}
fn number(v: &Value, k: &str) -> Result<f64, Error> {
    v[k].as_f64().ok_or_else(|| "invalid numeric policy".into())
}
fn integer(v: &Value, k: &str) -> Result<i64, Error> {
    v[k].as_i64().ok_or_else(|| "invalid integer policy".into())
}
fn native_policy(v: &Value) -> Result<rules::Policy, Error> {
    Ok(rules::Policy {
        enabled: v["enabled"].as_bool().ok_or("missing enabled")?,
        hr_low: number(v, "hrLow")?,
        hr_high: number(v, "hrHigh")?,
        bp_low: number(v, "bpLow")?,
        bp_high: number(v, "bpHigh")?,
        battery_low: number(v, "batteryLow")?,
        offline_seconds: integer(v, "offlineSeconds")?,
        consecutive: u8::try_from(integer(v, "consecutive")?)?,
        night_steps: number(v, "nightSteps")?,
        baseline_minimum: usize::try_from(integer(v, "baselineMinimum")?)?,
        baseline_percent: number(v, "baselinePercent")?,
        baseline_sigma: number(v, "baselineSigma")?,
    })
}
fn equal_policy(a: &Value, b: &Value) -> bool {
    match (a.as_object(), b.as_object()) {
        (Some(a), Some(b)) => {
            a.len() == b.len()
                && a.iter().all(|(k, v)| {
                    b.get(k).is_some_and(|w| {
                        if v.is_number() && w.is_number() {
                            v.as_f64() == w.as_f64()
                        } else {
                            v == w
                        }
                    })
                })
        }
        _ => false,
    }
}
fn use_authoritative_observations(snapshot: &Value, b: &mut Batch) -> Result<(), Error> {
    if b.input["last_report_ns"] != snapshot["lastReportNs"] {
        return Err("heartbeat fence mismatch".into());
    }
    if b.input["battery"]["value"].as_f64() != snapshot["battery"].as_f64()
        || (!snapshot["battery"].is_null()
            && b.input["battery"]["at_ns"] != snapshot["lastReportNs"])
    {
        return Err("battery fence mismatch".into());
    }
    if !snapshot["battery"].is_null() {
        b.input["battery"] = json!({"value":snapshot["battery"],"at_ns":snapshot["lastReportNs"]});
    }
    let observations = snapshot["observations"]
        .as_array()
        .ok_or("missing native observations")?;
    for v in observations {
        let (input, kind) = match v["metric"].as_str() {
            Some("heart_rate") => ("heart", "heart_rate"),
            Some("blood_pressure_systolic") => ("blood", "blood_pressure"),
            _ => continue,
        };
        let actual = json!({"value":v["value"],"at_ns":v["atNs"]});
        if b.input[input]["value"].as_f64() != actual["value"].as_f64()
            || b.input[input]["at_ns"] != actual["at_ns"]
        {
            return Err("collector observation mismatch".into());
        }
        if let Some(e) = b.evidence.iter().find(|e| e.kind == kind)
            && (Some(e.key.as_str()) != v["key"].as_str()
                || e.data.get("source") != Some(&v["source"])
                || e.data.get("sourceDevice") != Some(&v["sourceDevice"]))
        {
            return Err("collector observation identity mismatch".into());
        }
        // Native authenticated value, rather than caller JSON, drives the rule.
        b.input[input] = actual;
    }
    Ok(())
}
fn emit(out: &mut impl Write, v: &Value) -> Result<(), Error> {
    writeln!(out, "{v}")?;
    out.flush()?;
    Ok(())
}

enum Attempt {
    Committed,
    Deferred,
    Fallback,
}
async fn process_task(
    pool: &MySqlPool,
    keys: &Keys,
    device: i64,
    due: NaiveDateTime,
    now: DateTime<Utc>,
    input: &mut Input,
    out: &mut impl Write,
) -> Result<Attempt, Error> {
    // Candidate query is advisory and has no locks. Every acquired lock in
    // this path is nonblocking; especially never wait on the owner clock
    // while holding a device needed by the optional late-lock Go writer.
    let Some(owner) =
        sqlx::query_scalar::<_, Option<i64>>("SELECT old_id FROM device WHERE device_id=?")
            .bind(device)
            .fetch_optional(pool)
            .await?
            .flatten()
    else {
        return Ok(Attempt::Deferred);
    };
    let mut tx = pool.begin().await?;
    sqlx::query_scalar::<_, i64>("SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE NOWAIT")
        .bind(owner)
        .fetch_one(&mut *tx)
        .await?;
    let row = sqlx::query(
        "SELECT old_id,status,health_epoch FROM device WHERE device_id=? FOR UPDATE NOWAIT",
    )
    .bind(device)
    .fetch_one(&mut *tx)
    .await?;
    let current: Option<i64> = row.try_get("old_id")?;
    let status: Option<i64> = row.try_get("status")?;
    let epoch: i64 = row.try_get("health_epoch")?;
    if current != Some(owner) || status.is_none() || status == Some(0) || epoch <= 0 {
        return Ok(Attempt::Deferred);
    }
    // Policy and watch thresholds are changed under the device lock in Go;
    // their triggers also take the rule_task row. Lock existing rows
    // explicitly to reject out-of-order concurrent maintenance.
    let _ = sqlx::query("SELECT device_id FROM rule_policy WHERE device_id=? FOR UPDATE NOWAIT")
        .bind(device)
        .fetch_optional(&mut *tx)
        .await?;
    let _ = sqlx::query("SELECT device_id FROM watch_info WHERE device_id=? FOR UPDATE NOWAIT")
        .bind(device)
        .fetch_optional(&mut *tx)
        .await?;
    let task = sqlx::query(
        "SELECT epoch,eligible,due_at FROM rule_task WHERE device_id=? FOR UPDATE NOWAIT",
    )
    .bind(device)
    .fetch_one(&mut *tx)
    .await?;
    let actual: Option<NaiveDateTime> = task.try_get("due_at")?;
    if task.try_get::<i64, _>("epoch")? != epoch
        || !task.try_get::<bool, _>("eligible")?
        || actual != Some(due)
        || due > now.naive_utc()
    {
        return Ok(Attempt::Deferred);
    }
    let Some(revision) = sqlx::query_scalar::<_, i64>(
        "SELECT revision FROM health_owner_clock WHERE old_id=? FOR UPDATE NOWAIT",
    )
    .bind(owner)
    .fetch_optional(&mut *tx)
    .await?
    else {
        return Ok(Attempt::Fallback);
    };
    let now_ns = now.timestamp_nanos_opt().ok_or("invalid evaluation time")?;
    let snapshot = rule_authority::read_snapshot(&mut tx, keys, device, now_ns).await?;
    if snapshot["owner"].as_i64() != Some(owner)
        || snapshot["epoch"].as_i64() != Some(epoch)
        || snapshot["ownerRevision"].as_i64() != Some(revision)
        || snapshot["taskDue"] != true
    {
        return Err("authority fence mismatch".into());
    }
    let mut nonce = [0u8; 16];
    getrandom::fill(&mut nonce).map_err(|_| "challenge generation failed")?;
    let challenge = hex::encode(nonce);
    emit(
        out,
        &json!({"state":"collect","device":device,"owner":owner,"epoch":epoch,"now_ns":now_ns,"owner_revision":revision,"policy_revision":snapshot["policyRevision"],"challenge":challenge}),
    )?;
    // Bound the lifetime of held locks while reading the trusted local
    // adapter's response. On transport failure abort the whole session.
    let raw = match tokio::time::timeout(Duration::from_secs(10), input.next_line()).await {
        Ok(Ok(Some(raw))) if raw.len() <= 64 * 1024 => raw,
        _ => {
            tx.rollback().await?;
            return Err("collector transport failed".into());
        }
    };
    let reply: Reply = serde_json::from_str(&raw)?;
    if reply.challenge != challenge {
        return Err("collector challenge mismatch".into());
    }
    if reply.fallback {
        return Ok(Attempt::Fallback);
    }
    if reply.owner_revision != Some(revision)
        || reply.policy_revision != snapshot["policyRevision"].as_i64()
        || !reply
            .policy
            .as_ref()
            .is_some_and(|p| equal_policy(p, &snapshot["policy"]))
    {
        return Err("collector revision mismatch".into());
    }
    let mut b = reply.batch.ok_or("missing collector batch")?;
    if b.device != device
        || b.owner != owner
        || b.epoch != epoch
        || b.input["now_ns"].as_i64() != Some(now_ns)
    {
        return Err("collector binding mismatch".into());
    }
    let policy = native_policy(&snapshot["policy"])?;
    if policy.enabled {
        use_authoritative_observations(&snapshot, &mut b)?;
    }
    let candidates = analytics::evaluate_with_policy(b.input.clone(), policy)?;
    // The adapter preserves the legacy evidence format; require the
    // independently computed Rust decisions to agree before persisting.
    if candidates.as_slice().len() != b.evidence.len()
        || candidates.as_slice().iter().zip(&b.evidence).any(|(c, e)| {
            e.kind != c.kind.name()
                || e.data.get("breach").and_then(Value::as_bool) != Some(c.breach)
                || e.data.get("required").and_then(Value::as_u64) != Some(u64::from(c.required))
        })
    {
        return Err("native candidate disagreement".into());
    }
    let next = reply.next_due_ns.map(DateTime::<Utc>::from_timestamp_nanos);
    if policy.enabled != next.is_some() || next.is_some_and(|n| n <= now) {
        return Err("invalid next due".into());
    }
    let digest = keys.digest_scoped(b"yihe-next-scheduled-rule-v1", raw.as_bytes());
    b.request_id = format!("due-{}-{}", due.and_utc().timestamp_micros(), &digest[..32]);
    persist_batch(&mut tx, keys, &b, &candidates, now, &digest).await?;
    let affected = sqlx::query(
        "UPDATE rule_task SET due_at=? WHERE device_id=? AND epoch=? AND eligible AND due_at=?",
    )
    .bind(next.map(|n| n.naive_utc()))
    .bind(device)
    .bind(epoch)
    .bind(due)
    .execute(&mut *tx)
    .await?
    .rows_affected();
    if affected != 1 {
        return Err("task reschedule conflict".into());
    }
    tx.commit().await?;
    Ok(Attempt::Committed)
}

async fn tick(
    pool: &MySqlPool,
    keys: &Keys,
    now: DateTime<Utc>,
    input: &mut Input,
    out: &mut impl Write,
    crash_after_commit: bool,
) -> Result<bool, Error> {
    let mut cursor: Option<(NaiveDateTime, i64)> = None;
    let (mut committed, mut deferred, mut fallback) = (0, 0, 0);
    loop {
        let rows = if let Some((after, id)) = cursor {
            sqlx::query("SELECT device_id,due_at FROM rule_task WHERE due_at<=? AND (due_at>? OR (due_at=? AND device_id>?)) ORDER BY due_at,device_id LIMIT 100").bind(now.naive_utc()).bind(after).bind(after).bind(id).fetch_all(pool).await?
        } else {
            sqlx::query("SELECT device_id,due_at FROM rule_task WHERE due_at<=? ORDER BY due_at,device_id LIMIT 100").bind(now.naive_utc()).fetch_all(pool).await?
        };
        if rows.is_empty() {
            break;
        }
        for row in &rows {
            let device: i64 = row.try_get("device_id")?;
            let due: NaiveDateTime = row.try_get("due_at")?;
            cursor = Some((due, device));
            match process_task(pool, keys, device, due, now, input, out).await {
                Ok(Attempt::Committed) => {
                    if crash_after_commit {
                        std::process::exit(79)
                    }
                    committed += 1;
                }
                Ok(Attempt::Fallback) => fallback += 1,
                Ok(Attempt::Deferred) => deferred += 1,
                Err(e) if e.to_string() == "collector transport failed" => return Err(e),
                Err(_) => deferred += 1,
            }
        }
        if rows.len() < 100 {
            break;
        }
    }
    emit(
        out,
        &json!({"state":"tick","committed":committed,"deferred":deferred,"fallback":fallback}),
    )?;
    Ok(deferred > 0 || fallback > 0)
}

// A committed trigger wakes the durable due-index row. Poll only the
// index head, not the device inventory; bound wake detection to 2 s.
// Failed/fallback tasks keep their durable due_at and back off.
async fn worker(
    pool: &MySqlPool,
    keys: &Keys,
    input: &mut Input,
    out: &mut impl Write,
) -> Result<(), Error> {
    let minimum = Duration::from_millis(100);
    let maximum = Duration::from_secs(2);
    let mut idle = minimum;
    emit(out, &json!({"state":"ready"}))?;
    loop {
        let head=sqlx::query_scalar::<_,NaiveDateTime>("SELECT due_at FROM rule_task WHERE due_at IS NOT NULL ORDER BY due_at,device_id LIMIT 1").fetch_optional(pool).await?;
        let now = Utc::now();
        let wait = match head {
            Some(due) if due <= now.naive_utc() => {
                if !tick(pool, keys, now, input, out, false).await? {
                    idle = minimum;
                    continue;
                }
                let wait = idle;
                idle = (idle * 2).min(maximum);
                wait
            }
            Some(due) => {
                idle = minimum;
                (due.and_utc() - now).to_std()?.min(maximum)
            }
            None => {
                let wait = idle;
                idle = (idle * 2).min(maximum);
                wait
            }
        };
        tokio::select! {
            _=tokio::time::sleep(wait)=>{},
            line=input.next_line()=>match line?{
                None=>return Ok(()),
                Some(line) if line=="stop"=>return Ok(()),
                _=>return Err("unexpected idle collector message".into()),
            }
        }
    }
}
pub async fn run(continuous: bool, crash_after_commit: bool) -> Result<(), Error> {
    let pool = native::test_pool_with_limit(4).await?;
    let keys = Keys::load()?;
    let mut input = BufReader::new(tokio::io::stdin()).lines();
    let mut out = std::io::BufWriter::new(std::io::stdout().lock());
    if continuous {
        worker(&pool, &keys, &mut input, &mut out).await?;
        pool.close().await;
        return Ok(());
    }
    while let Some(line) = input.next_line().await? {
        if line.len() > 64 {
            return Err("invalid cutoff request".into());
        }
        let now_ns: i64 = serde_json::from_str(&line)?;
        tick(
            &pool,
            &keys,
            DateTime::<Utc>::from_timestamp_nanos(now_ns),
            &mut input,
            &mut out,
            crash_after_commit,
        )
        .await?;
    }
    pool.close().await;
    Ok(())
}
