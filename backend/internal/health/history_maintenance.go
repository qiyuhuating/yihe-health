package health

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"yihe-care/internal/securestore"
)

const historyMaintenancePage = 16

// RewriteHistoryReferences authenticates both copies before deduplication,
// or reseals a referenced sample under its original independent AAD.
// Run offline with all readers/writers and key rotation stopped and a verified
// backup. Each bounded page commits atomically. No history is removed.
// Only identical sample JSON (ignoring insignificant whitespace) is collapsed.
func RewriteHistoryReferences(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply, materialize bool) ([]ProjectionBackfillReport, error) {
	if database == nil || box == nil || limit < 1 || limit > 1000 {
		return nil, errors.New("valid database, key and bounded reference maintenance required")
	}
	predicate := "s.payload_cipher<>0x01"
	if materialize {
		predicate = "s.payload_cipher=0x01"
	}
	countSQL := "SELECT COUNT(*) FROM health_sample s WHERE s.payload_encrypted=true AND " + predicate
	report := []ProjectionBackfillReport{{Table: "health_sample"}}
	r := &report[0]
	if err := database.QueryRowContext(ctx, countSQL).Scan(&r.Before); err != nil {
		return report, err
	}
	var cursor []byte
	for r.Verified < limit {
		n, next, err := rewriteHistoryPage(ctx, database, box, predicate, cursor, min(historyMaintenancePage, limit-r.Verified), apply, materialize)
		if err != nil {
			return report, err
		}
		if n == 0 {
			break
		}
		r.Verified += n
		if apply {
			r.Migrated += n
		}
		cursor = next
	}
	var remaining int64
	if err := database.QueryRowContext(ctx, countSQL).Scan(&remaining); err != nil {
		return report, err
	}
	r.Remaining = &remaining
	if apply && remaining != 0 {
		return report, ErrProjectionBackfillIncomplete
	}
	if !apply && int64(r.Verified) < r.Before {
		return report, ErrProjectionBackfillIncomplete
	}
	return report, nil
}

type historyRewrite struct {
	key         []byte
	revision    int64
	cipher      string
	replacement string
}

func rewriteHistoryPage(ctx context.Context, database *sql.DB, box *securestore.Box, predicate string, cursor []byte, limit int, apply, materialize bool) (int, []byte, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: !apply})
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	query := "SELECT s.sample_id,s.old_id,s.revision,s.payload_cipher,c.payload_cipher FROM health_sample s LEFT JOIN health_change c ON c.revision=s.revision AND c.old_id=s.old_id AND c.sample_id=s.sample_id WHERE s.payload_encrypted=true AND " + predicate
	var args []any
	if cursor != nil {
		query += " AND s.sample_id>?"
		args = append(args, cursor)
	}
	query += " ORDER BY s.sample_id LIMIT ?"
	args = append(args, limit)
	if apply {
		query += " FOR UPDATE"
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	changes := make([]historyRewrite, 0, limit)
	var retained int
	for rows.Next() {
		var row historyRewrite
		var owner int64
		var history sql.NullString
		if err := rows.Scan(&row.key, &owner, &row.revision, &row.cipher, &history); err != nil {
			return 0, nil, err
		}
		if !history.Valid || len(history.String) > maxProjectionCipherBytes || len(row.cipher) > maxProjectionCipherBytes {
			return 0, nil, errors.New("missing or oversized historical ciphertext")
		}
		id := hex.EncodeToString(row.key)
		resolved := sampleHistoryReference + history.String
		sample, err := (ProjectionPayload{Cipher: &resolved}).OpenSample(box, id, owner, row.revision)
		if err != nil {
			return 0, nil, err
		}
		if materialize {
			sealed, err := protectProjection(box, sample, samplePayloadScope(id, owner, row.revision))
			if err != nil {
				return 0, nil, err
			}
			row.replacement = *sealed.Cipher
		} else {
			current, err := (ProjectionPayload{Cipher: &row.cipher}).OpenSample(box, id, owner, row.revision)
			if err != nil {
				return 0, nil, err
			}
			if !sameHistorySample(current, sample) {
				return 0, nil, errors.New("current and historical sample payloads differ; no page changes committed")
			}
			row.replacement = sampleHistoryReference
		}
		// Bound retained ciphertext memory independently of page row count.
		retained += len(row.cipher) + len(row.replacement)
		if len(changes) > 0 && retained > 16<<20 {
			break
		}
		changes = append(changes, row)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	if err := rows.Close(); err != nil {
		return 0, nil, err
	}
	if len(changes) == 0 {
		return 0, nil, nil
	}
	if apply {
		for _, row := range changes {
			result, err := tx.ExecContext(ctx, "UPDATE health_sample SET payload_cipher=? WHERE sample_id=? AND revision=? AND payload_cipher=?", row.replacement, row.key, row.revision, row.cipher)
			if err != nil {
				return 0, nil, err
			}
			if n, err := result.RowsAffected(); err != nil || n != 1 {
				return 0, nil, errors.New("sample changed during reference maintenance")
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	return len(changes), changes[len(changes)-1].key, nil
}

func sameHistorySample(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var x, y bytes.Buffer
	return json.Compact(&x, a) == nil && json.Compact(&y, b) == nil && bytes.Equal(x.Bytes(), y.Bytes())
}
