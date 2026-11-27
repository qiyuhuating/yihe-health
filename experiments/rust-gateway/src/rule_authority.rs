//! Read-only authority snapshot of the actual Go schema, not a scheduler.
//! The owner clock and payloads are read from one REPEATABLE READ snapshot.
//! Do not acquire the clock after a device lock: late-lock Go writers can
//! hold the clock while waiting for the device. This command never writes.
use crate::{native, outbox::Keys};
use chrono::{DateTime, NaiveDateTime, Utc};
use serde_json::{Value, json};
use sqlx::{Acquire, MySql, Row, Transaction};
use std::io::{BufRead, Write};

type Error = Box<dyn std::error::Error>;
const MAX_REVISION: i64 = (1i64 << 53) - 1;
// All three branches are static SQL; request values are bound separately.
const LATEST: &str = r#"(SELECT 0 AS metric_order,LOWER(HEX(sample_id)) AS sample_id,revision,CAST(payload AS CHAR) AS payload,CASE WHEN health_sample.payload_cipher=0x01 THEN CONCAT(0x01,COALESCE((SELECT hc.payload_cipher FROM health_change hc WHERE hc.revision=health_sample.revision AND hc.old_id=health_sample.old_id AND hc.sample_id=health_sample.sample_id),_binary'')) ELSE health_sample.payload_cipher END AS payload_cipher FROM health_sample WHERE old_id=? AND device_id=? AND epoch=? AND metric=? AND NOT deleted AND sample_start>=? AND sample_start<=? ORDER BY sample_start DESC,revision DESC LIMIT 1)
UNION ALL
(SELECT 1 AS metric_order,LOWER(HEX(sample_id)) AS sample_id,revision,CAST(payload AS CHAR) AS payload,CASE WHEN health_sample.payload_cipher=0x01 THEN CONCAT(0x01,COALESCE((SELECT hc.payload_cipher FROM health_change hc WHERE hc.revision=health_sample.revision AND hc.old_id=health_sample.old_id AND hc.sample_id=health_sample.sample_id),_binary'')) ELSE health_sample.payload_cipher END AS payload_cipher FROM health_sample WHERE old_id=? AND device_id=? AND epoch=? AND metric=? AND NOT deleted AND sample_start>=? AND sample_start<=? ORDER BY sample_start DESC,revision DESC LIMIT 1)
UNION ALL
(SELECT 2 AS metric_order,LOWER(HEX(sample_id)) AS sample_id,revision,CAST(payload AS CHAR) AS payload,CASE WHEN health_sample.payload_cipher=0x01 THEN CONCAT(0x01,COALESCE((SELECT hc.payload_cipher FROM health_change hc WHERE hc.revision=health_sample.revision AND hc.old_id=health_sample.old_id AND hc.sample_id=health_sample.sample_id),_binary'')) ELSE health_sample.payload_cipher END AS payload_cipher FROM health_sample WHERE old_id=? AND device_id=? AND epoch=? AND metric=? AND NOT deleted AND sample_start>=? AND sample_start<=? ORDER BY sample_start DESC,revision DESC LIMIT 1)
ORDER BY metric_order"#;

struct Binding {
    owner: i64,
    device: i64,
    epoch: i64,
    revision: Option<i64>,
}
type Thresholds = (Option<f64>, Option<f64>, Option<f64>, Option<f64>);
fn open_sample(
    keys: &Keys,
    b: &Binding,
    id: &str,
    revision: i64,
    plain: Option<String>,
    cipher: Option<Vec<u8>>,
) -> Result<Value, Error> {
    let (owner, device, epoch) = (b.owner, b.device, b.epoch);
    if let Some(cipher) = cipher {
        if plain.is_some() || cipher.is_empty() || cipher.len() > 8 * 1024 * 1024 + 1 {
            return Err("invalid protected sample".into());
        }
        if cipher[0] == 1 {
            if cipher.len() == 1 {
                return Err("missing sample history".into());
            }
            let scope = format!("health-change:{revision}:{owner}:{id}");
            let raw = keys
                .open_scoped(&cipher[1..], scope.as_bytes())
                .map_err(|_| "history authentication failed")?;
            let value: Value = serde_json::from_slice(&raw)?;
            if value["sampleId"].as_str() != Some(id)
                || value["oldId"].as_i64() != Some(owner)
                || value["deviceId"].as_i64() != Some(device)
                || value["bindingVersion"].as_i64() != Some(epoch)
                || value["revision"].as_i64() != Some(revision)
                || !value["sample"].is_object()
            {
                return Err("history identity mismatch".into());
            }
            return Ok(value["sample"].clone());
        }
        let scope = format!("health-sample:{id}:{owner}:{revision}");
        let raw = keys
            .open_scoped(&cipher, scope.as_bytes())
            .map_err(|_| "sample authentication failed")?;
        Ok(serde_json::from_slice(&raw)?)
    } else {
        Ok(serde_json::from_str(
            &plain.ok_or("sample payload missing")?,
        )?)
    }
}
async fn latest(
    tx: &mut Transaction<'_, MySql>,
    keys: &Keys,
    b: &Binding,
    now: DateTime<Utc>,
    activity_minutes: i64,
) -> Result<Vec<Value>, Error> {
    let (owner, device, epoch, revision) = (b.owner, b.device, b.epoch, b.revision);
    let end = now.timestamp_micros();
    let vital_start = (now - chrono::Duration::minutes(5)).timestamp_micros();
    let activity_start = (now - chrono::Duration::minutes(activity_minutes)).timestamp_micros();
    // Independent indexed ranges retain the original Go latest-observation
    // contract: newest by sample_start then revision, not by insertion time.
    let mut q = sqlx::query(LATEST);
    for (metric, start) in [
        ("heart_rate", vital_start),
        ("blood_pressure_systolic", vital_start),
        ("steps", activity_start),
    ] {
        q = q
            .bind(owner)
            .bind(device)
            .bind(epoch)
            .bind(metric)
            .bind(start)
            .bind(end);
    }
    let rows = q.fetch_all(&mut **tx).await?;
    let mut result = Vec::with_capacity(rows.len());
    for row in rows {
        let ord: i32 = row.try_get("metric_order")?;
        let metric = match ord {
            0 => "heart_rate",
            1 => "blood_pressure_systolic",
            2 => "steps",
            _ => return Err("invalid metric ordinal".into()),
        };
        let id: String = row.try_get("sample_id")?;
        let rev: i64 = row.try_get("revision")?;
        if rev <= 0 || rev > revision.ok_or("owner clock missing")? {
            return Err("sample beyond owner revision".into());
        }
        let value = open_sample(
            keys,
            b,
            &id,
            rev,
            row.try_get("payload")?,
            row.try_get("payload_cipher")?,
        )?;
        if value["metric"].as_str() != Some(metric) || value["deleted"].as_bool() == Some(true) {
            return Err("sample metric/identity mismatch".into());
        }
        if value["timeBasis"].as_str() == Some("legacy-unzoned") || value["value"].is_null() {
            continue;
        }
        let observed = value["start"].as_str().ok_or("sample clock missing")?;
        let at = DateTime::parse_from_rfc3339(observed)?.with_timezone(&Utc);
        result.push(json!({"metric":metric,"key":format!("{id}:{rev}"),"revision":rev,"value":value["value"],"source":value["source"],"sourceDevice":value["sourceDevice"],"atNs":at.timestamp_nanos_opt().ok_or("sample clock outside native range")?}));
    }
    Ok(result)
}
fn policy(payload: Option<String>, thresholds: Thresholds) -> Result<Value, Error> {
    let mut defaults = json!({"enabled":true,"hrLow":55.0,"hrHigh":110.0,"bpLow":90.0,"bpHigh":140.0,
        "batteryLow":20.0,"offlineSeconds":300,"consecutive":3,"timezone":"Asia/Shanghai",
        "nightBegin":79200,"nightEnd":21600,"nightSteps":200.0,"activityMinutes":15,
        "baselineDays":7,"baselineMinimum":20,"baselinePercent":25.0,"baselineSigma":3.0});
    if let Some(payload) = payload {
        let raw: Value = serde_json::from_str(&payload)?;
        let object = raw.as_object().ok_or("invalid rule policy")?;
        let defaults = defaults.as_object_mut().expect("default policy object");
        for (k, v) in object {
            if let Some((_, slot)) = defaults
                .iter_mut()
                .find(|(name, _)| name.eq_ignore_ascii_case(k))
                && !v.is_null()
            {
                *slot = v.clone();
            }
        }
    } else {
        let (a, b, c, d) = thresholds;
        if let (Some(a), Some(b)) = (a, b) {
            defaults["hrLow"] = json!(a.min(b));
            defaults["hrHigh"] = json!(a.max(b));
        }
        if let (Some(c), Some(d)) = (c, d) {
            defaults["bpLow"] = json!(c.min(d));
            defaults["bpHigh"] = json!(c.max(d));
        }
    }
    // Numeric policy validation. Timezone resolution is deliberately not part
    // of this read-only bridge; return the configured IANA name unchanged.
    let number = |k: &str| defaults[k].as_f64().ok_or("invalid numeric policy");
    let integer = |k: &str| defaults[k].as_i64().ok_or("invalid integer policy");
    if defaults["enabled"].as_bool().is_none()
        || number("hrLow")? <= 0.
        || number("hrHigh")? <= number("hrLow")?
        || number("bpLow")? <= 0.
        || number("bpHigh")? <= number("bpLow")?
        || !(0.0..=100.0).contains(&number("batteryLow")?)
        || !(60..=86400).contains(&integer("offlineSeconds")?)
        || !(1..=20).contains(&integer("consecutive")?)
        || !(0..86400).contains(&integer("nightBegin")?)
        || !(0..86400).contains(&integer("nightEnd")?)
        || integer("nightBegin")? == integer("nightEnd")?
        || number("nightSteps")? <= 0.
        || !(1..=120).contains(&integer("activityMinutes")?)
        || !(1..=30).contains(&integer("baselineDays")?)
        || !(5..=100000).contains(&integer("baselineMinimum")?)
        || number("baselinePercent")? <= 0.
        || number("baselineSigma")? <= 0.
        || defaults["timezone"]
            .as_str()
            .is_none_or(|s| s.is_empty() || s == "Local")
    {
        return Err("invalid rule policy".into());
    }
    Ok(defaults)
}
async fn snapshot(
    pool: &sqlx::MySqlPool,
    keys: &Keys,
    device: i64,
    now_ns: i64,
) -> Result<Value, Error> {
    if device <= 0 {
        return Err("invalid device".into());
    }
    let mut conn = pool.acquire().await?;
    sqlx::query("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY")
        .execute(&mut *conn)
        .await?;
    let mut tx = conn.begin().await?;
    let result = read_snapshot(&mut tx, keys, device, now_ns).await?;
    tx.commit().await?;
    Ok(result)
}

pub(crate) async fn read_snapshot(
    tx: &mut Transaction<'_, MySql>,
    keys: &Keys,
    device: i64,
    now_ns: i64,
) -> Result<Value, Error> {
    let now = DateTime::<Utc>::from_timestamp_nanos(now_ns);
    let row=sqlx::query("SELECT d.old_id,d.health_epoch,d.status,d.last_time,d.bind_time,CAST(d.battery AS DOUBLE) AS battery,c.revision,c.range_end,c.range_start,p.revision AS policy_revision,CAST(p.payload AS CHAR) AS policy,CAST(w.hrf AS DOUBLE) AS hrf,CAST(w.hrl AS DOUBLE) AS hrl,CAST(w.bpf AS DOUBLE) AS bpf,CAST(w.bpl AS DOUBLE) AS bpl,t.epoch AS task_epoch,t.eligible,t.due_at FROM device d LEFT JOIN health_owner_clock c ON c.old_id=d.old_id LEFT JOIN rule_policy p ON p.device_id=d.device_id LEFT JOIN watch_info w ON w.device_id=d.device_id AND p.device_id IS NULL LEFT JOIN rule_task t ON t.device_id=d.device_id WHERE d.device_id=?").bind(device).fetch_one(&mut **tx).await?;
    let owner: Option<i64> = row.try_get("old_id")?;
    let owner = owner.ok_or("unbound device")?;
    let epoch: i64 = row.try_get("health_epoch")?;
    let status: Option<i64> = row.try_get("status")?;
    if epoch <= 0 || status.is_none() || status == Some(0) {
        return Err("inactive device".into());
    }
    let revision: Option<i64> = row.try_get("revision")?;
    let end: Option<i64> = row.try_get("range_end")?;
    let start: Option<i64> = row.try_get("range_start")?;
    if let Some(rev) = revision
        && (rev < 0
            || end.is_none_or(|e| e < rev || e > MAX_REVISION)
            || start.is_none_or(|s| s < 0 || s > end.unwrap_or(0)))
    {
        return Err("invalid owner clock".into());
    }
    let p = policy(
        row.try_get("policy")?,
        (
            row.try_get("hrf")?,
            row.try_get("hrl")?,
            row.try_get("bpf")?,
            row.try_get("bpl")?,
        ),
    )?;
    let binding = Binding {
        owner,
        device,
        epoch,
        revision,
    };
    let policy_revision: Option<i64> = row.try_get("policy_revision")?;
    let observations = latest(
        tx,
        keys,
        &binding,
        now,
        p["activityMinutes"]
            .as_i64()
            .ok_or("invalid activity window")?,
    )
    .await?;
    let last: Option<NaiveDateTime> = row.try_get("last_time")?;
    let bound: Option<NaiveDateTime> = row.try_get("bind_time")?;
    let battery: Option<f64> = row.try_get("battery")?;
    let task_epoch: Option<i64> = row.try_get("task_epoch")?;
    let eligible: Option<bool> = row.try_get("eligible")?;
    let due: Option<NaiveDateTime> = row.try_get("due_at")?;
    let nanos = |t: NaiveDateTime| t.and_utc().timestamp_nanos_opt();
    let result = json!({"owner":owner,"epoch":epoch,"ownerRevision":revision,"policyRevision":policy_revision,"policy":p,"observations":observations,
        "lastReportNs":last.or(bound).and_then(nanos),"battery":if last.is_some(){battery}else{None},
        "taskDue":task_epoch==Some(epoch)&&eligible==Some(true)&&due.is_some_and(|d|d<=now.naive_utc())});
    Ok(result)
}
/// Local disposable-schema diagnostics only. Never log decrypted data on error.
pub async fn run() -> Result<(), Error> {
    let pool = native::test_pool().await?;
    let keys = Keys::load()?;
    let mut output = std::io::BufWriter::new(std::io::stdout().lock());
    for line in std::io::stdin().lock().lines() {
        let line = line?;
        if line.len() > 256 {
            return Err("snapshot request too large".into());
        }
        let [device, now]: [i64; 2] = serde_json::from_str(&line)?;
        let result = snapshot(&pool, &keys, device, now)
            .await
            .unwrap_or_else(|_| json!({"state":"unavailable"}));
        writeln!(output, "{result}")?;
        output.flush()?;
    }
    pool.close().await;
    Ok(())
}
