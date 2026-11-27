package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"yihe-care/internal/health"
	"yihe-care/internal/response"
)

// Existing daily rows need only a shared read. Only absent days are inserted.
const metricDailyInsertPrefix = `INSERT INTO metric_daily(cache_key,old_id,source,source_device,metric,semantics,record_day,offset_minutes) VALUES `
const metricDailyEnsureSQL = metricDailyInsertPrefix + `(?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE cache_key=cache_key`

type dailyCacheRow struct {
	dirty   bool
	queued  bool
	rev     int64
	stored  health.ProjectionPayload
	message string
	updated time.Time
}

func (h *Handler) healthDaily(w http.ResponseWriter, r *http.Request) {
	var req struct {
		health.DailyKey
		Days int `json:"days"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	oldID := req.OldID
	if oldID == 0 {
		oldID = p.OldID
	}
	req.OldID = oldID
	from, _, err := req.DailyKey.Bounds()
	if err != nil {
		write400(w, err.Error())
		return
	}
	if req.Days < 1 || req.Days > 31 {
		write400(w, "一次查询 1–31 天")
		return
	}
	ids := make([]string, req.Days)
	days := make([]string, req.Days)
	keys := make([]any, req.Days)
	for n := 0; n < req.Days; n++ {
		k := req.DailyKey
		k.Day = from.AddDate(0, 0, n).Format("2006-01-02")
		ids[n], days[n] = k.ID(), k.Day
		keys[n] = ids[n]
	}
	ctx := r.Context()
	tx, ok := h.beginScopedHealthRead(w, r, oldID)
	if !ok {
		return
	}
	defer tx.Rollback()
	// Each row and its owner clock are read by the same SQL statement.
	// A changed owner revision makes the cached result pending without
	// requiring the upload transaction to update any daily rows.
	// The duty or family binding remains locked by the enclosing transaction.
	cached := make(map[string]dailyCacheRow, req.Days)
	if err := readDailyRows(ctx, tx, keys, cached, false); internalError(w, err) {
		return
	}
	needsQueue := len(cached) != len(ids)
	for _, item := range cached {
		needsQueue = needsQueue || (item.dirty && !item.queued)
	}
	if needsQueue {
		// Cold creation and demand-driven refresh take owner -> daily locks.
		// Re-read all rows
		// after acquiring the clock; a writer may have invalidated the first
		// snapshot, or another reader may have inserted the missing days.
		if _, err := health.LockOwner(ctx, tx, oldID); internalError(w, err) {
			return
		}
		clear(cached)
		if err := readDailyRows(ctx, tx, keys, cached, true); internalError(w, err) {
			return
		}
		missing := make([]any, 0, len(ids)-len(cached))
		values := make([]any, 0, cap(missing)*8)
		for n, id := range ids {
			if _, found := cached[id]; found {
				continue
			}
			missing = append(missing, id)
			values = append(values, id, oldID, req.Source, req.SourceDevice, req.Metric, req.Semantics, days[n], req.OffsetMinutes)
		}
		if len(missing) != 0 {
			placeholders := strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?,?),", len(missing)), ",")
			_, err = tx.ExecContext(ctx, metricDailyInsertPrefix+placeholders+" ON DUPLICATE KEY UPDATE cache_key=cache_key", values...)
			if internalError(w, err) {
				return
			}
			if err := readDailyRows(ctx, tx, missing, cached, true); internalError(w, err) {
				return
			}
		}
		// A stale cached day is queued only when requested. The durable dirty
		// bit is already indexed by the worker; unrelated history stays idle.
		stale := make([]any, 0, req.Days)
		for _, id := range ids {
			if item := cached[id]; item.dirty && !item.queued {
				stale = append(stale, id)
			}
		}
		if len(stale) != 0 {
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(stale)), ",")
			_, err = tx.ExecContext(ctx, "UPDATE metric_daily SET dirty=true WHERE dirty=false AND cache_key IN ("+placeholders+")", stale...)
			if internalError(w, err) {
				return
			}
		}
	}
	items := make([]map[string]any, 0, req.Days)
	for n, id := range ids {
		item, found := cached[id]
		if !found {
			internalError(w, sql.ErrNoRows)
			return
		}
		status := "ready"
		var result any
		if item.dirty {
			status = "pending"
		} else if item.message != "" {
			status = "conflict"
		} else {
			payload, err := item.stored.OpenDailyResult(h.Box, id, item.rev)
			if internalError(w, err) || internalError(w, json.Unmarshal(payload, &result)) {
				return
			}
		}
		items = append(items, map[string]any{"day": days[n], "status": status, "statistics": result, "message": item.message, "computedRevision": item.rev, "updatedAt": item.updated})
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"items": items, "source": req.Source, "sourceDevice": req.SourceDevice, "metric": req.Metric, "semantics": req.Semantics, "offsetMinutes": req.OffsetMinutes, "algorithmVersion": health.DailyAlgorithmVersion})
}

func readDailyRows(ctx context.Context, tx *sql.Tx, keys []any, cached map[string]dailyCacheRow, locked bool) error {
	query := "SELECT d.cache_key,d.dirty,(d.dirty OR d.computed_revision<>COALESCE(c.revision,0)),d.computed_revision,d.result,d.result_cipher,d.error_text,d.updated_at FROM metric_daily d LEFT JOIN health_owner_clock c ON c.old_id=d.old_id WHERE d.cache_key IN (" +
		strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + ")"
	if locked {
		query += " FOR SHARE"
	}
	rows, err := tx.QueryContext(ctx, query, keys...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var item dailyCacheRow
		if err := rows.Scan(&id, &item.queued, &item.dirty, &item.rev, &item.stored.JSON, &item.stored.Cipher, &item.message, &item.updated); err != nil {
			return err
		}
		cached[id] = item
	}
	return rows.Err()
}
