package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/health"
	"yihe-care/internal/observability"
	"yihe-care/internal/response"
)

func (h *Handler) healthStatistics(w http.ResponseWriter, r *http.Request) {
	var req healthStatisticsRequest
	if !decodeHealth(w, r, &req) {
		return
	}
	if message := req.validationMessage(); message != "" {
		write400(w, message)
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
	// The binding is checked once, under the transaction's shared lock.
	// A separate unlocked family lookup cannot authorize the actual read.
	pool := h.DB.StatisticsPool()
	var revision int64
	var scopedRevision *int64
	if h.healthCache != nil && p.Role == auth.RoleFamily {
		scopedRevision = &revision
	}
	tx, ok := beginScopedResidentReadOn(w, r, pool, oldID, carepolicy.ReadHealthData, sql.LevelRepeatableRead, scopedRevision)
	if !ok {
		return
	}
	defer tx.Rollback()
	// Authorization always runs before reading the cache. The committed clock
	// also detects corrections and deletes written by another server instance.
	var key [32]byte
	cacheable := false
	var seriesKey coreKey
	var from, to int64
	var eligible bool
	if h.healthCache != nil {
		req.OldID = oldID
		b, err := json.Marshal(req) // Keep reporting offsets for daily totals.
		if err == nil && scopedRevision == nil {
			revision, err = health.StatisticsRevision(r.Context(), tx, oldID)
		}
		if err == nil && revision > 0 {
			cacheable = true
			key = sha256.Sum256(b)
			if cached, hit := h.healthCache.get(key, revision, time.Now()); hit {
				if internalError(w, tx.Commit()) {
					return
				}
				observability.Default.Add("yihe_health_cache_hits_total", 1)
				response.OK(w, map[string]any{"statistics": cached, "source": req.Source, "sourceDevice": req.SourceDevice, "metric": req.Metric, "semantics": req.Semantics})
				return
			}
			observability.Default.Add("yihe_health_cache_misses_total", 1)
			if h.healthCache.core != nil {
				seriesKey, from, to, eligible = coreWindow(req)
				if eligible {
					if cached, hit := h.healthCache.core.get(seriesKey, revision, from, to, time.Now()); hit {
						if internalError(w, tx.Commit()) {
							return
						}
						observability.Default.Add("yihe_health_core_hits_total", 1)
						response.OK(w, map[string]any{"statistics": cached, "source": req.Source, "sourceDevice": req.SourceDevice, "metric": req.Metric, "semantics": req.Semantics})
						return
					}
					if previous := h.healthCache.core.stale(seriesKey, revision, from, to, time.Now()); previous != nil {
						next := h.refreshCoreProjection(r.Context(), tx, previous, revision)
						if next != nil {
							if result, ok := coreQuery(next, from, to); ok {
								if internalError(w, tx.Commit()) {
									return
								}
								h.healthCache.core.put(next)
								observability.Default.Add("yihe_health_core_incremental_hits_total", 1)
								response.OK(w, map[string]any{"statistics": result, "source": req.Source, "sourceDevice": req.SourceDevice, "metric": req.Metric, "semantics": req.Semantics})
								return
							}
						}
					}
				}
			}
		} else if err != nil {
			// A cache validation failure must never serve an unverified hit.
			observability.Default.Add("yihe_health_cache_validation_errors_total", 1)
		}
	}
	observability.Default.Add("yihe_health_statistics_db_loads_total", 1)
	samples, ids, candidates, projectionEligible, err := health.LoadStatisticsSamples(r.Context(), tx, oldID, health.StatisticsQuery{
		Source: req.Source, SourceDevice: req.SourceDevice, Metric: req.Metric,
		Semantics: req.Semantics, From: req.From, To: req.To,
	}, h.Box, eligible)
	if internalError(w, err) {
		return
	}
	if candidates > 10000 {
		response.JSON(w, 413, map[string]any{"message": "候选采样超过 10000 条，请缩短统计范围"})
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	out, err := health.Aggregate(samples, req.From, req.To)
	if err != nil {
		conflict409(w, err.Error())
		return
	}
	if cacheable {
		// The clock and samples came from the same repeatable-read view.
		// New requests still validate the current committed revision; a
		// concurrent commit cannot cause this snapshot to be mislabeled.
		if eligible && projectionEligible {
			if series := makeCoreSeries(seriesKey, revision, from, to, samples, ids, candidates, time.Now()); series != nil {
				h.healthCache.core.put(series)
			}
		}
		b, err := json.Marshal(out)
		if err == nil {
			h.healthCache.put(key, revision, b, time.Now())
		}
	}
	response.OK(w, map[string]any{"statistics": out, "source": req.Source, "sourceDevice": req.SourceDevice, "metric": req.Metric, "semantics": req.Semantics})
}

// Refresh only a complete committed change range within the same read view
// as the caller's revision. The ordinary snapshot path remains available.
func (h *Handler) refreshCoreProjection(ctx context.Context, tx *sql.Tx, previous *coreSeries, revision int64) *coreSeries {
	records, err := health.StatisticsChanges(ctx, tx, previous.key.resident, previous.revision, revision, h.Box)
	if err != nil {
		return nil
	}
	return applyCoreChanges(previous, revision, records, time.Now())
}

type healthStatisticsRequest struct {
	OldID        int64     `json:"oldId"`
	Source       string    `json:"source"`
	SourceDevice string    `json:"sourceDevice"`
	Metric       string    `json:"metric"`
	Semantics    string    `json:"semantics"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
}

func (req healthStatisticsRequest) validationMessage() string {
	if req.Source == "" || req.SourceDevice == "" || req.Metric == "" || req.Semantics == "" || req.From.IsZero() || !req.To.After(req.From) || req.To.Sub(req.From) > 366*24*time.Hour {
		return "请明确来源、设备、指标、采样语义及不超过 366 天的时间范围"
	}
	if req.Semantics == "daily_total" {
		isMidnight := func(t time.Time) bool {
			return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
		}
		_, fromOffset := req.From.Zone()
		_, toOffset := req.To.Zone()
		if !isMidnight(req.From) || !isMidnight(req.To) || fromOffset != toOffset {
			return "日累计统计须使用相同 UTC 偏移的完整自然日"
		}
	}
	return ""
}
