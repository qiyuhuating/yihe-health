package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

// Fixed reporting offsets match the existing statistics API. No implicit local
// timezone or data-source merging is performed.
type DailyKey struct {
	OldID         int64  `json:"oldId"`
	Source        string `json:"source"`
	SourceDevice  string `json:"sourceDevice"`
	Metric        string `json:"metric"`
	Semantics     string `json:"semantics"`
	Day           string `json:"day"`
	OffsetMinutes int    `json:"offsetMinutes"`
}

const DailyAlgorithmVersion = 2

func (k DailyKey) ID() string {
	b, _ := json.Marshal(struct {
		DailyKey
		AlgorithmVersion int `json:"algorithmVersion"`
	}{k, DailyAlgorithmVersion})
	return Digest(b)
}
func (k DailyKey) Bounds() (time.Time, time.Time, error) {
	if k.OldID < 1 || k.Source == "" || len(k.Source) > 256 || k.SourceDevice == "" || len(k.SourceDevice) > 4096 || len(k.Metric) > 64 || k.OffsetMinutes < -840 || k.OffsetMinutes > 840 {
		return time.Time{}, time.Time{}, fmt.Errorf("日统计身份或偏移无效")
	}
	loc := time.FixedZone("reporting", k.OffsetMinutes*60)
	from, err := time.ParseInLocation("2006-01-02", k.Day, loc)
	if err != nil {
		return from, from, fmt.Errorf("日期应为 yyyy-MM-dd")
	}
	s := Sample{RecordID: "validation", Source: k.Source, SourceDevice: k.SourceDevice, Metric: k.Metric, Semantics: k.Semantics, Start: from, End: from, Version: 1, Deleted: true}
	switch k.Metric {
	case "heart_rate":
		s.Unit = "count/min"
	case "oxygen_saturation":
		s.Unit = "%"
	case "body_temperature":
		s.Unit = "degC"
	case "body_mass":
		s.Unit = "kg"
	case "steps":
		s.Unit = "count"
		if k.Semantics == "interval" {
			s.End = from.Add(time.Minute)
		}
	case "sleep_stage":
		s.Unit = "min"
		s.Stage = "asleep"
		s.End = from.Add(time.Minute)
	case "sleep_duration":
		s.Unit = "min"
	default:
		s.Unit = "mmHg"
	}
	if n, e := s.Normalize(); e != nil || n.Semantics != k.Semantics {
		return from, from, fmt.Errorf("指标与采样语义不匹配")
	}
	return from, from.AddDate(0, 0, 1), nil
}

const dailyCacheSelectSQL = `SELECT old_id,source,source_device,metric,semantics,CAST(record_day AS CHAR),offset_minutes,dirty,
 computed_revision,result_cipher FROM metric_daily WHERE cache_key=?`

// Equality predicates enable the existing series index. Binary predicates
// remain authoritative, including on deployments with insensitive collations.
const dailySamplesSQL = `SELECT LOWER(HEX(sample_id)),revision,payload,` + SampleCipherSQL + ` FROM health_sample WHERE old_id=?
 AND source=? AND source_device=? AND metric=? AND semantics=?
 AND BINARY source=BINARY ? AND BINARY source_device=BINARY ? AND BINARY metric=BINARY ? AND BINARY semantics=BINARY ? AND NOT deleted
 AND sample_start<? AND (sample_start>=? OR (?='interval' AND sample_end>=?)) ORDER BY revision LIMIT 100001`

type dailyRefreshRow struct {
	key              DailyKey
	dirty            bool
	previousRevision int64
	previous         ProjectionPayload
}

func readDailyRefreshRow(ctx context.Context, tx *sql.Tx, id, query string) (dailyRefreshRow, error) {
	var row dailyRefreshRow
	k := &row.key
	err := tx.QueryRowContext(ctx, query, id).Scan(&k.OldID, &k.Source, &k.SourceDevice, &k.Metric, &k.Semantics, &k.Day, &k.OffsetMinutes, &row.dirty, &row.previousRevision, &row.previous.Cipher)
	return row, err
}

type dailyComputation struct {
	key      DailyKey
	revision int64
	stored   ProjectionPayload
	message  string
}

// RefreshDaily normally performs the expensive decoding and aggregation in
// a nonlocking consistent snapshot. The short publication transaction
// rechecks the owner clock before touching the daily row. A concurrent write
// invalidates the snapshot; a single locked fallback ensures progress even
// for continuously reporting owners. No stale computation is published.
func RefreshDaily(ctx context.Context, pool *db.Database, id string, box *securestore.Box) error {
	result, fallback, err := computeDailySnapshot(ctx, pool, id, box)
	if err != nil {
		return err
	}
	if !fallback {
		if result == nil {
			return nil
		}
		done, err := publishDailySnapshot(ctx, pool, id, box, result)
		if err != nil || done {
			return err
		}
	}
	return refreshDailyLocked(ctx, pool, id, box)
}

func computeDailySnapshot(ctx context.Context, pool *db.Database, id string, box *securestore.Box) (*dailyComputation, bool, error) {
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	row, err := readDailyRefreshRow(ctx, tx, id, dailyCacheSelectSQL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !row.dirty {
		return nil, false, nil
	}
	var clock ownerClock
	err = tx.QueryRowContext(ctx, "SELECT revision,range_end FROM health_owner_clock WHERE old_id=?", row.key.OldID).Scan(&clock.revision, &clock.rangeEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if clock.revision < 0 || clock.rangeEnd < clock.revision || clock.rangeEnd > maxSafeRevision {
		return nil, false, fmt.Errorf("health revision exceeds safe cursor range")
	}
	if _, err = row.previous.OpenDailyResult(box, id, row.previousRevision); err != nil {
		return nil, false, err
	}
	stored, message, err := computeDaily(ctx, tx, id, row.key, clock.revision, box)
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return &dailyComputation{row.key, clock.revision, stored, message}, false, nil
}

func publishDailySnapshot(ctx context.Context, pool *db.Database, id string, box *securestore.Box, result *dailyComputation) (bool, error) {
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	revision, err := LockOwner(ctx, tx, result.key.OldID)
	if err != nil {
		return false, err
	}
	if revision != result.revision {
		return false, nil
	}
	row, err := readDailyRefreshRow(ctx, tx, id, dailyCacheSelectSQL+" FOR UPDATE")
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !row.dirty {
		return true, nil
	}
	if row.key != result.key {
		return false, nil
	}
	if _, err = row.previous.OpenDailyResult(box, id, row.previousRevision); err != nil {
		return false, err
	}
	if err = writeDailyResult(ctx, tx, id, result); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func refreshDailyLocked(ctx context.Context, pool *db.Database, id string, box *securestore.Box) error {
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldID int64
	if err = tx.QueryRowContext(ctx, "SELECT old_id FROM metric_daily WHERE cache_key=?", id).Scan(&oldID); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	revision, err := LockOwner(ctx, tx, oldID)
	if err != nil {
		return err
	}
	row, err := readDailyRefreshRow(ctx, tx, id, dailyCacheSelectSQL+" FOR UPDATE")
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !row.dirty {
		return nil
	}
	if _, err = row.previous.OpenDailyResult(box, id, row.previousRevision); err != nil {
		return err
	}
	stored, message, err := computeDaily(ctx, tx, id, row.key, revision, box)
	if err != nil {
		return err
	}
	if err = writeDailyResult(ctx, tx, id, &dailyComputation{row.key, revision, stored, message}); err != nil {
		return err
	}
	return tx.Commit()
}

func computeDaily(ctx context.Context, tx *sql.Tx, id string, k DailyKey, revision int64, box *securestore.Box) (ProjectionPayload, string, error) {
	from, to, err := k.Bounds()
	if err != nil {
		return ProjectionPayload{}, "", err
	}
	rows, err := tx.QueryContext(ctx, dailySamplesSQL, k.OldID, k.Source, k.SourceDevice, k.Metric, k.Semantics, k.Source, k.SourceDevice, k.Metric, k.Semantics, to.UnixMicro(), from.UnixMicro(), k.Semantics, from.UnixMicro())
	if err != nil {
		return ProjectionPayload{}, "", err
	}
	var series dailySeries
	var sampleID string
	var sampleRevision int64
	var raw sql.RawBytes
	var cipher sql.NullString
	var scratch []byte
	var s Sample
	candidates := 0
	for rows.Next() {
		candidates++
		s = Sample{}
		if err = rows.Scan(&sampleID, &sampleRevision, &raw, &cipher); err == nil {
			stored := ProjectionPayload{JSON: raw}
			if cipher.Valid {
				stored.Cipher = &cipher.String
			}
			var payload []byte
			payload, err = stored.openSample(box, sampleID, k.OldID, sampleRevision, &scratch)
			if err == nil {
				err = json.Unmarshal(payload, &s)
			}
		}
		if err != nil {
			rows.Close()
			return ProjectionPayload{}, "", err
		}
		if s.InWindow(from, to) {
			series.add(s)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ProjectionPayload{}, "", err
	}
	var result Statistics
	var computation error
	if candidates > 100000 {
		computation = fmt.Errorf("单日候选采样超过 100000 条，须离线处理")
	} else {
		result, computation = series.result(from, to)
	}
	b, _ := json.Marshal(result)
	message := ""
	if computation != nil {
		message = computation.Error()
		b = []byte("null")
	}
	stored, err := protectProjection(box, b, dailyResultScope(id, revision))
	if err != nil {
		return ProjectionPayload{}, "", err
	}
	return stored, message, nil
}

func writeDailyResult(ctx context.Context, tx *sql.Tx, id string, result *dailyComputation) error {
	_, err := tx.ExecContext(ctx, "UPDATE metric_daily SET result=?,result_cipher=?,error_text=?,computed_revision=?,dirty=false,updated_at=CURRENT_TIMESTAMP(6) WHERE cache_key=?", result.stored.jsonArgument(), result.stored.Cipher, result.message, result.revision, id)
	return err
}
