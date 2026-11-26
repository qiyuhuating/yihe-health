// Package securityaudit persists local recovery evidence and its alert outbox.
package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Actor struct {
	User string `json:"user"`
	UID  string `json:"uid"`
	Host string `json:"host"`
}

type Snapshot struct {
	Exists bool   `json:"exists"`
	Role   string `json:"role"`
	Status string `json:"status"`
	Duty   string `json:"duty"`
}

type Entry struct {
	ID       string    `json:"auditId"`
	At       time.Time `json:"occurredAt"`
	Actor    Actor     `json:"operator"`
	AdminID  int64     `json:"adminId"`
	Reason   string    `json:"reason"`
	Outcome  string    `json:"outcome"`
	Decision string    `json:"decision"`
	Before   Snapshot  `json:"before"`
	After    Snapshot  `json:"after"`
}

// Append requires the caller's transaction so no grant can commit without its
// evidence and durable alert. Database triggers reject ordinary UPDATE/DELETE.
func Append(ctx context.Context, tx *sql.Tx, entry Entry) (string, error) {
	before, err := json.Marshal(entry.Before)
	if err != nil {
		return "", err
	}
	after, err := json.Marshal(entry.After)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO security_audit_log
 (id,action,os_user,os_uid,host,reason,admin_id,outcome,decision,before_state,after_state,occurred_at)
 VALUES(?,'supervisor_recovery',?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))`,
		id, entry.Actor.User, entry.Actor.UID, entry.Actor.Host, entry.Reason, entry.AdminID,
		entry.Outcome, entry.Decision, string(before), string(after))
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO security_alert_outbox(audit_id,available_at) VALUES(?,UTC_TIMESTAMP(6))", id)
	return id, err
}
