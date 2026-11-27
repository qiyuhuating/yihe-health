//! Local, bounded, revision-fenced projection of experimental receipt rows.
//! A probe may suppress work, but only a locked committed snapshot is emitted.
use super::Protected;
use crate::{native, outbox::Keys};
use chrono::NaiveDateTime;
use serde_json::{Value, json};
use sqlx::{MySqlPool, Row};
use std::{collections::VecDeque, io::Write, time::Duration};
use yihe_core::{Point, SeriesKey, Window, rolling::RollingHeartRate};

const CAP: usize = 256;
type Error = Box<dyn std::error::Error>;
struct Projection {
    key: SeriesKey,
    epoch: i64,
    revision: i64,
    rows: VecDeque<Option<Point>>,
    ordered: bool,
    state: Option<RollingHeartRate>,
    window: Window,
    appends: usize,
    rebuilds: usize,
}
impl Projection {
    fn new(owner: i64, device: i64, epoch: i64) -> Self {
        Self {
            key: SeriesKey {
                resident_id: owner,
                source: "vital-event".into(),
                source_device: format!("{device}:{epoch}"),
            },
            epoch,
            revision: 0,
            rows: VecDeque::with_capacity(CAP),
            ordered: true,
            state: None,
            window: Window { from: 0, to: 1 },
            appends: 0,
            rebuilds: 0,
        }
    }
    fn matches(&self, owner: i64, epoch: i64) -> bool {
        self.key.resident_id == owner && self.epoch == epoch
    }
    fn push(&mut self, rev: i64, point: Option<Point>) -> Result<(), Error> {
        if rev <= 0 || (self.revision != 0 && self.revision.checked_add(1) != Some(rev)) {
            return Err("noncontiguous committed revisions".into());
        }
        let dropped = if self.rows.len() == CAP {
            self.rows.pop_front().flatten()
        } else {
            None
        };
        self.rows.push_back(point);
        self.revision = rev;
        if let (Some(state), Some(point)) = (&mut self.state, point) {
            // If a retained chronological prefix leaves the ring, expire
            // exactly that prefix. Equal timestamps or historical ordering
            // require a rebuild to preserve count-window membership.
            let from = match dropped {
                None => Some(self.window.from),
                Some(old) if self.ordered => self
                    .rows
                    .iter()
                    .flatten()
                    .next()
                    .filter(|p| p.at > old.at)
                    .map(|p| p.at),
                Some(_) => None,
            };
            if let Some(from) = from {
                let next = Window {
                    from,
                    to: self
                        .window
                        .to
                        .max(point.at.checked_add(1).ok_or("timestamp overflow")?),
                };
                if state.append(&self.key, rev, next, point).is_ok() {
                    self.window = next;
                    self.appends += 1;
                    return Ok(());
                }
            }
        }
        let mut points: Vec<_> = self.rows.iter().flatten().copied().collect();
        self.ordered = points.windows(2).all(|p| p[0].at <= p[1].at);
        if points.is_empty() {
            self.state = None;
            return Ok(());
        }
        points.sort_by_key(|p| p.at);
        self.window = Window {
            from: points[0].at,
            to: points
                .last()
                .expect("nonempty")
                .at
                .checked_add(1)
                .ok_or("timestamp overflow")?,
        };
        self.state = Some(
            RollingHeartRate::from_committed(self.key.clone(), rev, self.window, &points, CAP)
                .map_err(|_| "invalid committed heart-rate snapshot")?,
        );
        self.rebuilds += 1;
        Ok(())
    }
    fn result(&self) -> Result<Value, Error> {
        match &self.state {
            Some(s) => {
                let v = s
                    .query(&self.key, self.revision, self.window)
                    .map_err(|_| "projection revision miss")?
                    .ok_or("empty projection")?;
                Ok(
                    json!({"revision":self.revision,"count":v.count,"min":v.min,"max":v.max,"mean":v.mean,"appends":self.appends,"rebuilds":self.rebuilds}),
                )
            }
            None => Ok(
                json!({"revision":self.revision,"count":0,"appends":self.appends,"rebuilds":self.rebuilds}),
            ),
        }
    }
}
async fn refresh(
    pool: &MySqlPool,
    keys: &Keys,
    device: i64,
    cache: &mut Option<Projection>,
    probe: bool,
) -> Result<Option<Value>, Error> {
    if probe && let Some(p) = cache.as_ref() {
        let row=sqlx::query("SELECT d.old_id,d.health_epoch,d.status,(SELECT revision FROM rust_vital_receipt r WHERE r.device_id=d.device_id AND r.binding_epoch=d.health_epoch ORDER BY revision DESC LIMIT 1) AS revision FROM device d WHERE d.device_id=?").bind(device).fetch_one(pool).await?;
        let owner: Option<i64> = row.try_get("old_id")?;
        let epoch: i64 = row.try_get("health_epoch")?;
        let status: Option<i64> = row.try_get("status")?;
        let rev: Option<i64> = row.try_get("revision")?;
        if owner == Some(p.key.resident_id)
            && p.epoch == epoch
            && status.is_some_and(|s| s != 0)
            && rev.unwrap_or(0) == p.revision
        {
            return Ok(None);
        }
    }
    let owner: Option<i64> = sqlx::query_scalar("SELECT old_id FROM device WHERE device_id=?")
        .bind(device)
        .fetch_one(pool)
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
    let latest:Option<i64>=sqlx::query_scalar("SELECT revision FROM rust_vital_receipt WHERE device_id=? AND binding_epoch=? ORDER BY revision DESC LIMIT 1").bind(device).bind(epoch).fetch_optional(&mut *tx).await?;
    let latest = latest.unwrap_or(0);
    let keep = cache.as_ref().is_some_and(|p| {
        p.matches(owner, epoch) && latest >= p.revision && latest - p.revision <= CAP as i64
    });
    if !keep {
        *cache = Some(Projection::new(owner, device, epoch))
    }
    let p = cache.as_mut().expect("projection initialized");
    let rows=sqlx::query("SELECT r.revision,r.measurement_id,m.old_id,m.device_id,m.create_time,m.measurement_cipher,m.hr,m.bp FROM rust_vital_receipt r LEFT JOIN ratepre m ON m.id=r.measurement_id WHERE r.device_id=? AND r.binding_epoch=? AND r.revision>? ORDER BY r.revision DESC LIMIT 256").bind(device).bind(epoch).bind(p.revision).fetch_all(&mut *tx).await?;
    for row in rows.iter().rev() {
        let rev: i64 = row.try_get("revision")?;
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
        p.push(
            rev,
            value.hr.map(|v| Point {
                at,
                value: v as f64,
            }),
        )?;
    }
    if p.revision != latest {
        return Err("projection did not reach committed revision".into());
    }
    let result = p.result()?;
    tx.commit().await?;
    Ok(Some(result))
}
pub async fn stats(device: i64) -> Result<(), Error> {
    let pool = native::test_pool().await?;
    let keys = Keys::load()?;
    let result = refresh(&pool, &keys, device, &mut None, false)
        .await?
        .ok_or("no projection")?;
    println!("{result}");
    pool.close().await;
    Ok(())
}
/// One explicitly selected disposable device. This is a local diagnostic
/// stream, not a production patient-facing cache or notification source.
pub async fn stats_worker(device: i64) -> Result<(), Error> {
    let pool = native::test_pool().await?;
    let keys = Keys::load()?;
    let mut cache = None;
    let mut idle = Duration::from_millis(100);
    let stop = tokio::signal::ctrl_c();
    tokio::pin!(stop);
    loop {
        match refresh(&pool, &keys, device, &mut cache, true).await {
            Ok(Some(result)) => {
                println!("{result}");
                std::io::stdout().flush()?;
                idle = Duration::from_millis(100);
            }
            Ok(None) => {
                idle = (idle * 2).min(Duration::from_secs(2));
            }
            Err(_) => {
                if cache.take().is_some() {
                    println!("{{\"state\":\"unavailable\"}}");
                    std::io::stdout().flush()?;
                }
                idle = Duration::from_secs(2);
            }
        }
        tokio::select! {biased;result=&mut stop=>{result?;break;},_=tokio::time::sleep(idle)=>{}}
    }
    pool.close().await;
    Ok(())
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn bounded_receipt_ring_matches_rebuild() {
        let mut p = Projection::new(1, 1, 1);
        for i in 1..=300 {
            p.push(
                i,
                Some(Point {
                    at: i,
                    value: ((i % 200) + 20) as f64,
                }),
            )
            .unwrap();
        }
        assert_eq!(p.rows.len(), CAP);
        assert_eq!(p.rebuilds, 1);
        assert_eq!(p.appends, 299);
        let v = p.result().unwrap();
        assert_eq!(v["count"], CAP);
        // Equal-time eviction and historical/BP-only changes preserve
        // exact membership even when the incremental fast path misses.
        p.push(
            301,
            Some(Point {
                at: 300,
                value: 80.,
            }),
        )
        .unwrap();
        p.push(302, Some(Point { at: 50, value: 60. })).unwrap();
        p.push(303, None).unwrap();
        let points: Vec<_> = p.rows.iter().flatten().map(|p| p.value).collect();
        let v = p.result().unwrap();
        assert_eq!(v["count"], points.len());
        assert_eq!(v["mean"], points.iter().sum::<f64>() / points.len() as f64);
        assert_eq!(v["min"], points.iter().copied().reduce(f64::min).unwrap());
        assert_eq!(v["max"], points.iter().copied().reduce(f64::max).unwrap());
    }
}
