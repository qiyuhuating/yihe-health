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
 computed_revision,result_cipher FROM metric_daily WHERE cache_key=? FOR UPDATE`

const dailySamplesSQL = `SELECT sample_id,revision,payload,payload_cipher FROM health_sample WHERE old_id=? AND BINARY source=BINARY ? AND BINARY source_device=BINARY ? AND BINARY metric=BINARY ? AND BINARY semantics=BINARY ? AND NOT deleted
 AND sample_start<? AND (sample_start>=? OR (?='interval' AND sample_end>=?)) ORDER BY revision LIMIT 100001`

// RefreshDaily is serialized with Save's owner clock, so a completed result
// cannot overwrite a concurrent invalidation. Error results are explicit, never 0.
func RefreshDaily(ctx context.Context, pool *db.Database, id string, box *securestore.Box) error {
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Read the immutable owner before taking its clock lock. Keep the same lock
	// order as sample invalidation: owner clock, then daily cache row.
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
	var k DailyKey
	var dirty bool
	var previous ProjectionPayload
	var previousRevision int64
	err = tx.QueryRowContext(ctx, dailyCacheSelectSQL, id).Scan(&k.OldID, &k.Source, &k.SourceDevice, &k.Metric, &k.Semantics, &k.Day, &k.OffsetMinutes, &dirty,
		&previousRevision, &previous.Cipher)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !dirty {
		return nil
	}
	if _, err := previous.OpenDailyResult(box, id, previousRevision); err != nil {
		return err
	}
	from, to, err := k.Bounds()
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, dailySamplesSQL, k.OldID, k.Source, k.SourceDevice, k.Metric, k.Semantics, to.UnixMicro(), from.UnixMicro(), k.Semantics, from.UnixMicro())
	if err != nil {
		return err
	}
	samples := []Sample{}
	candidates := 0
	for rows.Next() {
		candidates++
		var id string
		var revision int64
		var stored ProjectionPayload
		var s Sample
		if err = rows.Scan(&id, &revision, &stored.JSON, &stored.Cipher); err == nil {
			var payload []byte
			payload, err = stored.OpenSample(box, id, oldID, revision)
			if err == nil {
				err = json.Unmarshal(payload, &s)
			}
		}
		if err != nil {
			rows.Close()
			return err
		}
		if s.InWindow(from, to) {
			samples = append(samples, s)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var result Statistics
	var computation error
	if candidates > 100000 {
		computation = fmt.Errorf("单日候选采样超过 100000 条，须离线处理")
	} else {
		result, computation = Aggregate(samples, from, to)
	}
	b, _ := json.Marshal(result)
	message := ""
	if computation != nil {
		message = computation.Error()
		b = []byte("null")
	}
	stored, err := protectProjection(box, b, dailyResultScope(id, revision))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE metric_daily SET result=?,result_cipher=?,error_text=?,computed_revision=?,dirty=false,updated_at=CURRENT_TIMESTAMP(6) WHERE cache_key=?",
		stored.JSON, stored.Cipher, message, revision, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
