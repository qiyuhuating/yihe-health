package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"

	"yihe-care/internal/inviterepo"
	"yihe-care/internal/response"
	"yihe-care/internal/securecode"
)

// ---- InviteCode ----

// normalizeCode 输入归一化：去首尾空格、去所有空格与连字符、转大写。
func normalizeCode(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	return strings.ToUpper(s)
}

// inviteStatus 状态判定：used=1 → used；过期 → expired；否则 unused。
func inviteStatus(used int16, expire *time.Time, now time.Time) string {
	if used == 1 {
		return "used"
	}
	if expire != nil && !expire.After(now) {
		return "expired"
	}
	return "unused"
}

func inviteRemainingDays(expire *time.Time, now time.Time) int {
	if expire == nil {
		return -1
	}
	return int(expire.Sub(now).Hours() / 24)
}

// creatorName 取登录管理员的显示名作为生成人，不信任请求体里的自报姓名。
func creatorName(r *http.Request) string {
	if p := auth.Principal(r); p != nil && p.DisplayName != "" {
		return p.DisplayName
	}
	return "管理员"
}

func fmtTS(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(tsLayout)
}

// POST /api/v1/inviteCode/list —— 邀请码列表（ADMIN）。
func (h *Handler) inviteList(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	state := str(m, "state")
	if state == "" {
		state = "all"
	}
	switch state {
	case "all", "unused", "used", "expired":
	default:
		response.JSON(w, http.StatusBadRequest, map[string]any{"message": "邀请状态不正确"})
		return
	}
	page, pageSize, pageOK := bodyPage(w, m)
	if !pageOK {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadInvites)
	if !ok {
		return
	}
	defer tx.Rollback()
	rows, total, err := inviterepo.ListCodes(r.Context(), tx, state, page, pageSize)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	statusText := map[string]string{"unused": "可使用", "used": "已使用", "expired": "已过期"}
	now := time.Now()
	list := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		status := inviteStatus(row.Used, row.ExpireTime, now)
		name := "（老人已删除）"
		if row.ResidentName != nil {
			name = *row.ResidentName
		}
		list = append(list, map[string]any{
			"id": row.ID, "code": row.Code, "oldId": row.ResidentID, "oldPersonName": name,
			"createUser": row.CreateUser, "createTime": fmtTS(row.CreateTime),
			"expireTime": fmtTS(row.ExpireTime), "remainDays": inviteRemainingDays(row.ExpireTime, now),
			"usedTime": fmtTS(row.UsedTime), "status": status, "statusText": statusText[status],
		})
	}
	response.OK(w, map[string]any{
		"inviteCodes": list, "total": total, "page": page, "pageSize": pageSize,
	})
}

// POST /api/v1/inviteCode/create —— 生成邀请码（ADMIN，事务）。
func (h *Handler) inviteCreate(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	oldID, okID := int64Val(m, "oldId")
	if !okID || oldID <= 0 {
		response.Fail(w, "请选择邀请码绑定的老人")
		return
	}
	ctx := r.Context()
	expire, validExpiry := parseInvitationExpiry(m)
	if !validExpiry {
		write400(w, "有效天数必须为非负整数，留空或 0 表示长期有效")
		return
	}
	count := int64(1)
	if c, ok := int64Val(m, "count"); ok {
		count = c
		if count < 1 {
			count = 1
		}
		if count > 50 {
			count = 50
		}
	}
	createUser := creatorName(r)

	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageInvites)
	if !ok {
		return
	}
	defer tx.Rollback()
	oldName, err := inviterepo.ResidentName(ctx, tx, oldID)
	if isNotFound(err) {
		response.Fail(w, "老人不存在，请重新选择")
		return
	}
	if internalError(w, err) {
		return
	}
	now := time.Now()
	codes := make([]string, 0, 50)
	failed := false
	for i := int64(0); i < count && !failed; i++ {
		code, err := unusedInvitationCode(ctx, tx)
		if err != nil || code == "" {
			failed = true
			break
		}
		if err := inviterepo.InsertCode(ctx, tx, code, oldID, createUser, now, expire); err != nil {
			failed = true
			break
		}
		codes = append(codes, code)
	}
	if failed {
		_ = tx.Rollback()
		response.Error500(w, ref())
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	var expAny any
	if expire != nil {
		expAny = expire.Format(tsLayout)
	}
	nm := ""
	if oldName != nil {
		nm = *oldName
	}
	response.OK(w, map[string]any{
		"codes": codes, "oldId": oldID, "oldPersonName": nm, "expireTime": expAny,
	})
}

// POST /api/v1/inviteCode/delete —— 作废未使用的邀请码（ADMIN）。
func (h *Handler) inviteDelete(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	msg := "邀请码已被使用或不存在，无法作废"
	id, okID := int64Val(m, "id")
	if !okID {
		response.Fail(w, msg)
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageInvites)
	if !ok {
		return
	}
	defer tx.Rollback()
	deleted, err := inviterepo.DeleteUnusedCode(r.Context(), tx, id)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	if deleted {
		response.OK(w, nil)
	} else {
		response.Fail(w, msg)
	}
}

func parseInvitationExpiry(m map[string]any) (*time.Time, bool) {
	var expire *time.Time
	if expiry, _ := vstr(m, "expireDays"); expiry != "" {
		d, err := strconv.ParseInt(expiry, 10, 64)
		if err != nil || d < 0 {
			return nil, false
		}
		if d > 3650 {
			d = 3650
		}
		if d > 0 {
			t := time.Now().Add(time.Duration(d) * 24 * time.Hour)
			expire = &t
		}
	}
	return expire, true
}

func unusedInvitationCode(ctx context.Context, tx *sql.Tx) (string, error) {
	code := ""
	for a := 0; a < 20; a++ { // 最多 20 次重试保唯一
		c, err := securecode.Generate(8)
		if err != nil {
			return "", err
		}
		exists, err := inviterepo.CodeExists(ctx, tx, c)
		if err != nil {
			return "", err
		}
		if !exists {
			code = c
			break
		}
	}
	return code, nil
}
