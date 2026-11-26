package health

import (
	"context"
	"database/sql"
	"time"

	fastjson "github.com/goccy/go-json"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

type StatisticsQuery struct {
	Source       string
	SourceDevice string
	Metric       string
	Semantics    string
	From         time.Time
	To           time.Time
}

// StatisticsRevision reads the resident-scoped committed statistics clock.
func StatisticsRevision(ctx context.Context, store db.RowQuerier, residentID int64) (int64, error) {
	var revision int64
	err := store.QueryRowContext(ctx,
		"SELECT COALESCE((SELECT revision FROM health_owner_clock WHERE old_id=?),0)",
		residentID).Scan(&revision)
	return revision, err
}

// LoadStatisticsSamples returns only samples inside the requested window,
// retaining exact-byte matching under MySQL's trailing-space-insensitive collations.
func LoadStatisticsSamples(ctx context.Context, store db.RowsQuerier, residentID int64, query StatisticsQuery, box *securestore.Box, collectIDs bool) ([]Sample, []string, int, bool, error) {
	sourceFilter := "BINARY source=BINARY ? AND BINARY source_device=BINARY ? AND BINARY metric=BINARY ? AND BINARY semantics=BINARY ?"
	upper := query.To.UnixMicro()
	if query.To.Nanosecond()%1000 != 0 {
		upper++
	}
	samples := []Sample{}
	var ids []string
	candidates := 0
	projectionEligible := true
	err := db.QueryEach(ctx, store, func(rows *sql.Rows) error {
		candidates++
		var id string
		var revision, indexedStart int64
		var stored ProjectionPayload
		var sample Sample
		if err := rows.Scan(&id, &revision, &stored.JSON, &stored.Cipher, &indexedStart); err != nil {
			return err
		}
		payload, err := stored.OpenSample(box, id, residentID, revision)
		if err != nil {
			return err
		}
		if err := fastjson.Unmarshal(payload, &sample); err != nil {
			return err
		}
		if sample.Start.UnixMicro() != indexedStart {
			projectionEligible = false
		}
		if sample.InWindow(query.From, query.To) {
			samples = append(samples, sample)
			if collectIDs {
				ids = append(ids, id)
			}
		}
		return nil
	}, `SELECT sample_id,revision,payload,payload_cipher,sample_start FROM health_sample
		 WHERE old_id=? AND `+sourceFilter+` AND NOT deleted
		 AND sample_start<?
		 AND (sample_start>=? OR (?='interval' AND sample_end>=?))
		 ORDER BY revision LIMIT 10001`,
		residentID, query.Source, query.SourceDevice, query.Metric, query.Semantics, upper, query.From.UnixMicro(), query.Semantics, query.From.UnixMicro())
	if err != nil {
		return nil, nil, 0, false, err
	}
	return samples, ids, candidates, projectionEligible, nil
}
