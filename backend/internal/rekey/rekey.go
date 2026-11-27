package rekey

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"yihe-care/internal/health"
	"yihe-care/internal/securestore"
)

type Options struct {
	Apply            bool
	BatchSize        int
	MaxRowsPerColumn int64
	Pause            time.Duration
}

type Counts struct {
	Scanned   int64 `json:"scanned"`
	Values    int64 `json:"values"`
	Empty     int64 `json:"empty"`
	Active    int64 `json:"active"`
	OldKey    int64 `json:"old_key"`
	Legacy    int64 `json:"legacy_envelope"`
	Plaintext int64 `json:"plaintext"`
	Invalid   int64 `json:"invalid"`
}

type Column struct {
	Name string `json:"column"`
	Counts
	Complete  bool  `json:"complete"`
	Rewritten int64 `json:"rewritten"`
	Conflicts int64 `json:"conflicts"`
}

type Report struct {
	Version               int      `json:"version"`
	Mode                  string   `json:"mode"`
	Scope                 string   `json:"scope"`
	SourceRef             string   `json:"source_ref,omitempty"`
	ActiveKey             string   `json:"active_key"`
	StartedAt             string   `json:"started_at"`
	FinishedAt            string   `json:"finished_at"`
	Stage                 string   `json:"stage"`
	Before                []Column `json:"before"`
	Applied               []Column `json:"applied,omitempty"`
	After                 []Column `json:"after,omitempty"`
	EncryptedColumnsClean bool     `json:"encrypted_columns_clean"`
	RetirementAuthorized  bool     `json:"retirement_authorized"`
	Exclusions            []string `json:"exclusions"`
}

// Run verifies every known envelope before the first mutation, then rewraps
// with bounded pages and binary compare-and-swap. It never logs row values or
// keys and never authorizes retirement based on a live scan alone.
func Run(ctx context.Context, pool *sql.DB, current, previous *securestore.Box, options Options) (report Report, err error) {
	report = Report{Version: 1, Mode: "scan", Scope: "mysql_registered_fields", ActiveKey: current.ActiveKeyID(), StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Stage: "validation",
		Exclusions: []string{"plaintext fields and lookup-index migration", "blob objects and their references", "MySQL binary logs, replicas and PostgreSQL projections/WAL", "backups and rollback binaries", "writer key-version barrier and independent review"}}
	defer func() { report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano) }()
	if pool == nil || current.ActiveKeyID() == "" || !current.SameLookupKey(previous) {
		return report, errors.New("rekey requires a versioned active keyring and an unchanged previous lookup key")
	}
	if options.BatchSize < 1 || options.BatchSize > 1000 || options.MaxRowsPerColumn < 1 || options.Pause < 0 || options.Pause > time.Minute {
		return report, errors.New("invalid rekey bounds")
	}
	if options.Apply {
		report.Mode = "apply"
	}
	report.Stage = "preflight"
	report.Before, err = walkAll(ctx, pool, current, options, false)
	if err != nil {
		return report, err
	}
	if invalid(report.Before) {
		return report, errors.New("preflight authentication failed; no fields were changed")
	}
	if !options.Apply {
		report.EncryptedColumnsClean = clean(report.Before)
		report.Stage = "complete"
		return report, nil
	}
	report.Stage = "apply"
	report.Applied, err = walkAll(ctx, pool, current, options, true)
	if err != nil {
		return report, err
	}
	// Always obtain a fresh high-water mark and authenticate active ciphertexts
	// again. Concurrent inserts behind the cursor require another scan; this is
	// explicitly not an atomic retirement barrier or a backup verification.
	report.Stage = "residual_scan"
	report.After, err = walkAll(ctx, pool, current, options, false)
	if err != nil {
		return report, err
	}
	report.EncryptedColumnsClean = clean(report.After)
	if invalid(report.Applied) || invalid(report.After) {
		return report, errors.New("authentication failed during rekey; inspect aggregate counts and rerun after investigation")
	}
	for _, column := range report.Applied {
		if column.Conflicts > 0 {
			return report, errors.New("concurrent updates were preserved; a fresh scan/apply is required")
		}
	}
	if !report.EncryptedColumnsClean {
		return report, errors.New("known field residuals remain; plaintext requires its own migration")
	}
	report.Stage = "complete"
	return report, nil
}

func invalid(columns []Column) bool {
	for _, column := range columns {
		if !column.Complete || column.Invalid != 0 {
			return true
		}
	}
	return false
}

func clean(columns []Column) bool {
	if len(columns) != len(targets()) || invalid(columns) {
		return false
	}
	for _, column := range columns {
		if column.OldKey+column.Legacy+column.Plaintext != 0 {
			return false
		}
	}
	return true
}

func walkAll(ctx context.Context, pool *sql.DB, box *securestore.Box, options Options, apply bool) ([]Column, error) {
	if err := health.VerifySampleReferenceIntegrity(ctx, pool); err != nil {
		return nil, err
	}
	columns := make([]Column, 0, len(targets()))
	for _, t := range targets() {
		column, err := walk(ctx, pool, box, t, options, apply)
		columns = append(columns, column)
		if err != nil {
			return columns, fmt.Errorf("%s: %w", t.name(), err)
		}
	}
	return columns, nil
}

const maxFieldBytes = 8 << 20
const maxPageBytes = 16 << 20

type item struct {
	keys      []any
	stored    sql.NullString
	oversized bool
}

func keyTuple(keys []string) string { return "(" + strings.Join(keys, ",") + ")" }
func placeholders(n int) string     { return "(" + strings.TrimSuffix(strings.Repeat("?,", n), ",") + ")" }

func readKeys(row *sql.Row, count int) ([]any, error) {
	values, dest := keyDestinations(count)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return normalizeKeys(values), nil
}

func keyDestinations(count int) ([]any, []any) {
	values, dest := make([]any, count), make([]any, count)
	for i := range values {
		dest[i] = &values[i]
	}
	return values, dest
}

func normalizeKeys(values []any) []any {
	for i, value := range values {
		if text, ok := value.([]byte); ok {
			values[i] = string(text)
		}
	}
	return values
}

func walk(ctx context.Context, pool *sql.DB, box *securestore.Box, t target, options Options, apply bool) (result Column, err error) {
	result.Name = t.name()
	upper, err := readKeys(pool.QueryRowContext(ctx, "SELECT "+strings.Join(t.keys, ",")+" FROM "+t.table+" WHERE "+t.condition()+" ORDER BY "+strings.Join(t.keys, " DESC,")+" DESC LIMIT 1"), len(t.keys))
	if errors.Is(err, sql.ErrNoRows) {
		result.Complete = true
		return result, nil
	}
	if err != nil {
		return result, errors.New("cannot establish column high-water mark")
	}
	var cursor []any
	for {
		query := "SELECT " + strings.Join(t.keys, ",") + ",CASE WHEN OCTET_LENGTH(" + t.column + ")<=? THEN " + t.column + " ELSE NULL END,COALESCE(OCTET_LENGTH(" + t.column + ")>?,false) FROM " + t.table + " WHERE " + t.condition() + " AND " + keyTuple(t.keys) + "<=" + placeholders(len(upper))
		args := append([]any{maxFieldBytes, maxFieldBytes}, t.keyArgs(upper)...)
		if cursor != nil {
			query += " AND " + keyTuple(t.keys) + ">" + placeholders(len(cursor))
			args = append(args, t.keyArgs(cursor)...)
		}
		query += " ORDER BY " + strings.Join(t.keys, ",") + " LIMIT ?"
		args = append(args, options.BatchSize)
		page, err := readPage(ctx, pool, query, args, len(t.keys))
		if err != nil {
			return result, errors.New("column page read failed")
		}
		if len(page) == 0 {
			result.Complete = true
			return result, nil
		}
		for _, row := range page {
			if result.Scanned >= options.MaxRowsPerColumn {
				return result, errors.New("column row budget exceeded; scan is incomplete")
			}
			result.Scanned++
			cursor = row.keys
			if row.oversized {
				result.Values++
				result.Invalid++
				continue
			}
			replacement, change, err := convert(box, t, row.keys, row.stored, apply, &result.Counts)
			if err != nil {
				return result, errors.New("cannot create replacement envelope")
			}
			if change {
				changed, err := compareSwap(ctx, pool, t, row.keys, row.stored.String, replacement)
				if err != nil {
					return result, errors.New("field compare-and-swap failed")
				}
				if changed {
					result.Rewritten++
				} else {
					result.Conflicts++
				}
			}
		}
		if options.Pause > 0 {
			timer := time.NewTimer(options.Pause)
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, errors.New("column scan cancelled")
			case <-timer.C:
			}
		}
	}
}

func readPage(ctx context.Context, pool *sql.DB, query string, args []any, keyCount int) (page []item, err error) {
	rows, err := pool.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			page, err = nil, closeErr
		}
	}()
	bytes := 0
	for rows.Next() {
		keys, dest := keyDestinations(keyCount)
		row := item{}
		dest = append(dest, &row.stored, &row.oversized)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row.keys = normalizeKeys(keys)
		page = append(page, row)
		bytes += len(row.stored.String)
		if bytes >= maxPageBytes {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return page, nil
}

// compareSwap deliberately compares bytes, including case and trailing spaces,
// even if an old deployment uses a case-insensitive or PAD SPACE collation.
func compareSwap(ctx context.Context, pool *sql.DB, t target, keys []any, old, replacement string) (bool, error) {
	query := "UPDATE " + t.table + " SET " + t.column + "=? WHERE " + t.condition() + " AND " + keyTuple(t.keys) + "=" + placeholders(len(keys)) + " AND CAST(" + t.column + " AS BINARY)=CAST(? AS BINARY)"
	args := append([]any{replacement}, t.keyArgs(keys)...)
	args = append(args, old)
	result, err := pool.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count > 1 {
		return false, errors.New("unexpected compare-and-swap result")
	}
	return count == 1, nil
}

func convert(box *securestore.Box, t target, keys []any, stored sql.NullString, apply bool, counts *Counts) (string, bool, error) {
	if t.format == coordinateProjection && stored.Valid && stored.String != "" {
		return convertCoordinates(box, t, keys, stored.String, apply, counts)
	}
	counts.Values++
	if !stored.Valid || stored.String == "" {
		counts.Empty++
		return "", false, nil
	}
	envelope := stored.String
	var wrapper map[string]json.RawMessage
	if t.format == ruleEnvelope {
		if json.Unmarshal([]byte(envelope), &wrapper) != nil || wrapper == nil {
			counts.Invalid++
			return "", false, nil
		}
		if _, exists := wrapper["encrypted"]; !exists {
			counts.Plaintext++
			return "", false, nil
		}
		if json.Unmarshal(wrapper["encrypted"], &envelope) != nil || !strings.HasPrefix(envelope, securestore.Prefix) {
			counts.Invalid++
			return "", false, nil
		}
	}
	if !strings.HasPrefix(envelope, securestore.Prefix) {
		counts.Plaintext++
		return "", false, nil
	}
	plain, err := box.Open(envelope, t.fieldScope(keys))
	if err != nil {
		counts.Invalid++
		return "", false, nil
	}
	if strings.HasPrefix(envelope, securestore.Prefix+"v2:"+box.ActiveKeyID()+":") {
		counts.Active++
		return "", false, nil
	}
	if strings.HasPrefix(envelope, securestore.Prefix+"v2:") {
		counts.OldKey++
	} else {
		counts.Legacy++
	}
	if !apply {
		return "", false, nil
	}
	replacement, err := box.Seal(plain, t.fieldScope(keys))
	if err != nil {
		return "", false, err
	}
	if t.format == ruleEnvelope {
		wrapper["encrypted"], err = json.Marshal(replacement)
		if err != nil {
			return "", false, err
		}
		encoded, err := json.Marshal(wrapper)
		return string(encoded), true, err
	}
	return replacement, true, nil
}
