package health

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"yihe-care/internal/securestore"
)

var ErrProjectionBackfillIncomplete = errors.New("health projection backfill is partial; retain the write barrier and rerun")

type ProjectionBackfillReport struct {
	Table     string `json:"table"`
	Before    int64  `json:"pending_before"`
	Migrated  int    `json:"migrated"`
	Verified  int    `json:"verified,omitempty"`
	Remaining *int64 `json:"remaining"`
}

type projectionTarget struct {
	table, plaintext, ciphertext string
	legacy, protected            string
	keys                         []string
	scope                        func([]string) string
}

func projectionTargets() []projectionTarget {
	return []projectionTarget{
		{"health_change", "payload", "payload_cipher", "payload IS NOT NULL", "payload_cipher IS NOT NULL", []string{"revision", "old_id", "sample_id"},
			func(k []string) string { return ChangePayloadScope(k[0], k[1], k[2]) }},
		{"health_sample_latest", "payload", "payload_cipher", "payload IS NOT NULL", "payload_cipher IS NOT NULL", []string{"old_id", "series_key", "revision"},
			func(k []string) string { return LatestPayloadScope(k[0], k[1], k[2]) }},
		{"metric_daily", "result", "result_cipher", "result IS NOT NULL", "result_cipher IS NOT NULL", []string{"cache_key", "computed_revision"},
			func(k []string) string { return DailyResultScope(k[0], k[1]) }},
	}
}

// BackfillProjections migrates bounded rows after an independent full reader
// inventory. Stop projection writers and verify the backup before applying.
func BackfillProjections(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply bool) ([]ProjectionBackfillReport, error) {
	return backfillHealthPayloads(ctx, database, box, projectionTargets(), limit, apply)
}

// BackfillHealthSamples uses the storage-mode index so each invocation can
// resume from existing plaintext without rereading protected payloads.
func BackfillHealthSamples(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply bool) ([]ProjectionBackfillReport, error) {
	target := projectionTarget{"health_sample", "payload", "payload_cipher", "payload_encrypted=false", "payload_encrypted=true",
		[]string{"sample_id", "old_id", "revision"}, func(k []string) string { return SamplePayloadScope(k[0], k[1], k[2]) }}
	return backfillHealthPayloads(ctx, database, box, []projectionTarget{target}, limit, apply)
}

func backfillHealthPayloads(ctx context.Context, database *sql.DB, box *securestore.Box, targets []projectionTarget, limit int, apply bool) ([]ProjectionBackfillReport, error) {
	if database == nil || box == nil || limit < 1 || limit > 1000 {
		return nil, errors.New("valid database, key and bounded health projection backfill required")
	}
	reports := make([]ProjectionBackfillReport, len(targets))
	for i, target := range targets {
		reports[i].Table = target.table
		before, err := target.pendingCount(ctx, database)
		if err != nil {
			return reports, err
		}
		reports[i].Before = before
		if err := target.verifyReader(ctx, database, box); err != nil {
			return reports, err
		}
	}
	if !apply {
		for i := range reports {
			reports[i].Remaining = &reports[i].Before
		}
		return reports, nil
	}
	budget := limit
	for i, target := range targets {
		var cursor []any
		for budget > 0 {
			key, err := target.backfillRow(ctx, database, box, cursor)
			if err != nil {
				return reports, err
			}
			if key == nil {
				break
			}
			cursor = key
			reports[i].Migrated++
			budget--
		}
		remaining, err := target.pendingCount(ctx, database)
		if err != nil {
			return reports, err
		}
		reports[i].Remaining = &remaining
	}
	for _, report := range reports {
		if *report.Remaining != 0 {
			return reports, ErrProjectionBackfillIncomplete
		}
	}
	return reports, nil
}

func (t projectionTarget) pendingCount(ctx context.Context, database *sql.DB) (int64, error) {
	var count int64
	query := "SELECT COUNT(*) FROM " + t.table + " WHERE " + t.legacy
	if err := database.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, errors.New("cannot count pending health projection payloads")
	}
	return count, nil
}

func (t projectionTarget) verifyReader(ctx context.Context, database *sql.DB, box *securestore.Box) error {
	keys := strings.Join(t.keys, ",")
	if t.table == "health_sample" {
		var id string
		var owner, revision int64
		var stored ProjectionPayload
		err := database.QueryRowContext(ctx, "SELECT LOWER(HEX(sample_id)),old_id,revision,"+SampleCipherSQL+" FROM health_sample WHERE payload_encrypted=true ORDER BY sample_id LIMIT 1").Scan(&id, &owner, &revision, &stored.Cipher)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = stored.OpenSample(box, id, owner, revision)
		return err
	}
	var stored ProjectionPayload
	row := database.QueryRowContext(ctx, "SELECT "+keys+","+t.ciphertext+" FROM "+t.table+
		" WHERE "+t.protected+" ORDER BY "+keys+" LIMIT 1")
	_, scope, err := t.scan(row, &stored.Cipher)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errors.New("cannot read health projection key check")
	}
	_, err = stored.Open(box, scope)
	return err
}

func (t projectionTarget) backfillRow(ctx context.Context, database *sql.DB, box *securestore.Box, cursor []any) ([]any, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, errors.New("cannot begin health projection backfill")
	}
	defer tx.Rollback()
	keys := strings.Join(t.keys, ",")
	params := strings.TrimSuffix(strings.Repeat("?,", len(t.keys)), ",")
	query := "SELECT " + keys + "," + t.plaintext + " FROM " + t.table + " WHERE " + t.legacy
	if cursor != nil {
		query += " AND (" + keys + ") > (" + params + ")"
	}
	var raw []byte
	key, scope, err := t.scan(tx.QueryRowContext(ctx, query+" ORDER BY "+keys+" LIMIT 1 FOR UPDATE", cursor...), &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil || !json.Valid(raw) {
		return nil, errors.New("cannot validate legacy health projection")
	}
	stored, err := protectProjection(box, raw, scope)
	if err != nil {
		return nil, err
	}
	update := "UPDATE " + t.table + " SET " + t.plaintext + "=NULL," + t.ciphertext + "=? WHERE (" + keys + ") = (" + params + ")"
	result, err := tx.ExecContext(ctx, update, append([]any{stored.Cipher}, key...)...)
	if err != nil {
		return nil, errors.New("cannot save protected health projection")
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return nil, errors.New("health projection backfill did not update exactly one row")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.New("cannot confirm health projection backfill commit")
	}
	return key, nil
}

func (t projectionTarget) scan(row *sql.Row, payload any) ([]any, string, error) {
	key := make([]any, len(t.keys))
	dest := make([]any, len(key)+1)
	for i := range key {
		dest[i] = &key[i]
	}
	dest[len(key)] = payload
	if err := row.Scan(dest...); err != nil {
		return nil, "", err
	}
	parts := make([]string, len(key))
	for i, value := range key {
		if binary, ok := value.([]byte); ok {
			if t.keys[i] == "sample_id" {
				parts[i] = hex.EncodeToString(binary)
			} else {
				parts[i] = string(binary)
			}
		} else {
			parts[i] = fmt.Sprint(value)
		}
	}
	return key, t.scope(parts), nil
}
