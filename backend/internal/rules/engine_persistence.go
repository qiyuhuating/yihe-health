package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"yihe-care/internal/db"
	"yihe-care/internal/notify"
	"time"
)

func (e *Engine) persist(ctx context.Context, tx *sql.Tx, device, old, epoch int64, c CompactCandidate, now time.Time, scratch []byte) error {
	var previous string
	var streak int
	var active bool
	var incident *int64
	var prior []byte
	err := tx.QueryRowContext(ctx, "SELECT observation_key,streak,active,incident_id,evidence FROM rule_state WHERE device_id=? AND epoch=? AND kind=? FOR UPDATE", device, epoch, c.Kind).Scan(&previous, &streak, &active, &incident, &prior)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previous == c.Key {
		return nil
	}
	if len(prior) > 0 {
		var data map[string]any
		json.Unmarshal(prior, &data)
		if cipher, ok := data["encrypted"].(string); ok {
			plain, err := e.Box.Open(cipher, "rule-evidence")
			if err != nil {
				return err
			}
			json.Unmarshal([]byte(plain), &data)
		}
		if data["source"] != c.evidence.sourceValue() {
			streak = 0
			active = false
		}
	}
	if !c.Breach {
		streak = 0
		active = false
	} else {
		streak++
	}
	if c.Breach && streak >= c.Required && !active {
		id, err := db.InsertID(ctx, tx, "INSERT INTO care_incident(old_id,device_id,kind,severity) VALUES(?,?,?,?)", old, device, "rule_"+c.Kind, c.Severity)
		if err != nil {
			return err
		}
		incident = &id
		active = true
		note := "服务端规则触发：" + c.Kind
		if e.Box != nil {
			note, err = e.Box.Seal(note, "care-text")
			if err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO care_incident_event(incident_id,actor,action,note) VALUES(?,'rule','received',?)", id, note); err != nil {
			return err
		}
		if _, err = notify.QueueCareSMS(ctx, tx, e.Box, id, old); err != nil {
			return err
		}
		if err = notify.CareInbox(ctx, tx, e.Box, old, id); err != nil {
			return err
		}
	}
	b, err := c.appendEvidenceFast(scratch, streak, old, now)
	if err != nil {
		return err
	}
	if e.Box != nil {
		sealed, err := e.Box.Seal(string(b), "rule-evidence")
		if err != nil {
			return err
		}
		b, _ = json.Marshal(map[string]string{"encrypted": sealed})
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO rule_state(device_id,epoch,kind,observation_key,streak,active,incident_id,evidence,updated_at) VALUES(?,?,?,?,?,?,?,?,?) AS new ON DUPLICATE KEY UPDATE observation_key=new.observation_key,streak=new.streak,active=new.active,incident_id=new.incident_id,evidence=new.evidence,updated_at=new.updated_at`, device, epoch, c.Kind, c.Key, streak, active, incident, json.RawMessage(b), now)
	return err
}
