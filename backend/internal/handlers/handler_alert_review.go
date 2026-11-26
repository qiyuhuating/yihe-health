package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"yihe-care/internal/carepolicy"
	dbstore "yihe-care/internal/db"
	"yihe-care/internal/response"
)

func reviewVerdict(value string) bool { return value == "confirmed" || value == "false_alarm" }

// SOS and fence conclusions are recorded by the existing independent care verify
// transition. This endpoint only reviews a concrete legacy vital-sign record.
func (h *Handler) alertReview(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		RecordID         int64  `json:"recordId"`
		Kind             string `json:"kind"`
		Verdict          string `json:"verdict"`
		Reason           string `json:"reason"`
		ExpectedReviewID int64  `json:"expectedReviewId"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.RecordID < 1 || (req.Kind != "rate" && req.Kind != "pre") || !reviewVerdict(req.Verdict) || req.Reason == "" || utf8.RuneCountInString(req.Reason) > 2000 || req.ExpectedReviewID < 0 {
		write400(w, "须指定具体心率/血压告警、结论、理由和上次复核 ID")
		return
	}
	ctx := r.Context()
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReviewAlerts)
	if !ok {
		return
	}
	defer tx.Rollback()
	var record int64
	var state *string
	// Kind is allowlisted above, never arbitrary SQL input.
	err := tx.QueryRowContext(ctx,
		"SELECT id, CASE WHEN ?='rate' THEN rate_state ELSE pre_state END FROM ratepre WHERE id=? FOR UPDATE",
		req.Kind, req.RecordID).Scan(&record, &state)
	if isNotFound(err) {
		response.JSON(w, 404, map[string]any{"message": "记录不存在"})
		return
	}
	if internalError(w, err) {
		return
	}
	var previous int64
	err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM alert_review WHERE entity='ratepre' AND record_id=? AND kind=?", req.RecordID, req.Kind).Scan(&previous)
	if internalError(w, err) {
		return
	}
	if previous != req.ExpectedReviewID {
		conflict409(w, "复核结论已变化，请刷新")
		return
	}
	if previous == 0 && (state == nil || *state != "1") {
		conflict409(w, "此记录没有可复核的活动告警")
		return
	}
	req.Reason, err = h.sealNote(req.Reason)
	if internalError(w, err) {
		return
	}
	id, err := dbstore.InsertID(ctx, tx, "INSERT INTO alert_review(entity,record_id,kind,verdict,reason,actor_id) VALUES('ratepre',?,?,?,?,?)", req.RecordID, req.Kind, req.Verdict, req.Reason, actor)
	if err == nil {
		err = tx.Commit()
	}
	if internalError(w, err) {
		return
	}
	response.OK(w, map[string]any{"reviewId": id, "verdict": req.Verdict, "alarmStateChanged": false})
}

func (h *Handler) alertReviews(w http.ResponseWriter, r *http.Request) {
	if _, ok := careAdmin(w, r); !ok {
		return
	}
	var req struct {
		Entity   string `json:"entity"`
		RecordID int64  `json:"recordId"`
		Kind     string `json:"kind"`
		After    int64  `json:"after"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if (req.Entity != "care" && req.Entity != "ratepre") || req.RecordID < 1 || req.After < 0 || len(req.Kind) > 32 {
		write400(w, "复核目标或游标无效")
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadReviews)
	if !ok {
		return
	}
	defer tx.Rollback()
	items, err := dbstore.QueryList(r.Context(), tx, func(rows *sql.Rows) (map[string]any, error) {
		var id, actor int64
		var kind, verdict, reason string
		var at time.Time
		if err := rows.Scan(&id, &kind, &verdict, &reason, &actor, &at); err != nil {
			return nil, err
		}
		reason, err := h.openNote(reason)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "kind": kind, "verdict": verdict, "reason": reason, "actorId": actor, "createdAt": at}, nil
	}, "SELECT id,kind,verdict,reason,actor_id,created_at FROM alert_review WHERE entity=? AND record_id=? AND (?='' OR kind=?) AND id>? ORDER BY id LIMIT 100", req.Entity, req.RecordID, req.Kind, req.Kind, req.After)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"items": items})
}
