package commandrepo

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
)

type Existing struct {
	ID     int64
	Action string
	Status string
	TTL    int
}

func ByKey(ctx context.Context, tx *sql.Tx, deviceID, epoch int64, requestKey string) (Existing, error) {
	var out Existing
	err := tx.QueryRowContext(ctx,
		"SELECT id,action,status,ttl_seconds FROM device_command WHERE device_id=? AND epoch=? AND request_key=?",
		deviceID, epoch, requestKey).Scan(&out.ID, &out.Action, &out.Status, &out.TTL)
	return out, err
}

func WireType(ctx context.Context, tx *sql.Tx, deviceID int64) (string, error) {
	var wireType string
	err := tx.QueryRowContext(ctx,
		"SELECT wire_type FROM device_command_profile WHERE device_id=? AND enabled=true AND protocol='yihe-pull-v1'",
		deviceID).Scan(&wireType)
	return wireType, err
}

type Queued struct {
	DeviceID    int64
	ResidentID  int64
	Epoch       int64
	RequestKey  string
	Action      string
	WireType    string
	Channel     string
	WireAction  string
	MaxAttempts int
	CreatedBy   int64
	TTL         int
	Now         time.Time
}

func Insert(ctx context.Context, tx *sql.Tx, c Queued) (int64, error) {
	return db.InsertID(ctx, tx,
		`INSERT INTO device_command(device_id,old_id,epoch,request_key,action,wire_type,channel,wire_action,max_attempts,retry_at,expires_at,created_by,ttl_seconds)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.DeviceID, c.ResidentID, c.Epoch, c.RequestKey, c.Action, c.WireType,
		c.Channel, c.WireAction, c.MaxAttempts, c.Now, c.Now.Add(time.Duration(c.TTL)*time.Second), c.CreatedBy, c.TTL)
}

type Entry struct {
	CommandID int64      `json:"commandId"`
	Action    string     `json:"action"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	AckedAt   *time.Time `json:"ackedAt"`
}

func List(ctx context.Context, tx *sql.Tx, deviceID, after int64) ([]Entry, error) {
	return db.QueryList(ctx, tx, func(rows *sql.Rows) (Entry, error) {
		var entry Entry
		err := rows.Scan(&entry.CommandID, &entry.Action, &entry.Status, &entry.Attempts, &entry.CreatedAt, &entry.ExpiresAt, &entry.AckedAt)
		return entry, err
	}, "SELECT id,action,status,attempts,created_at,expires_at,acked_at FROM device_command WHERE device_id=? AND id>? ORDER BY id LIMIT 100", deviceID, after)
}
