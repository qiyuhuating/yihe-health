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
	// Existing rows are read in one committed snapshot. A write concurrent
	// with this read may publish a newer revision, but cannot make the
	// result/dirty/revision fields come from different committed states.
	// The duty or family binding remains locked by the enclosing transaction.
	cached := make(map[string]dailyCacheRow, req.Days)
	if err := readDailyRows(ctx, tx, keys, cached, false); internalError(w, err) {
		return
	}
	if len(cached) != len(ids) {
		// Cold creation still takes owner -> daily locks. Re-read all rows
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
	query := "SELECT cache_key,dirty,computed_revision,result,result_cipher,error_text,updated_at FROM metric_daily WHERE cache_key IN (" +
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
		if err := rows.Scan(&id, &item.dirty, &item.rev, &item.stored.JSON, &item.stored.Cipher, &item.message, &item.updated); err != nil {
			return err
		}
		cached[id] = item
	}
	return rows.Err()
}
