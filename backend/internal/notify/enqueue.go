package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

func EnqueueChannels(ctx context.Context, tx *sql.Tx, box *securestore.Box, topic, key string, familyID int64, phone string, channels []string) error {
	return EnqueueBatch(ctx, tx, box, []Message{{Topic: topic, Key: key, FamilyID: familyID, Phone: phone, Channels: channels}})
}

// Message is one recipient's durable notification. A nil Channels uses sms and in_app.
type Message struct {
	Topic, Key string
	FamilyID   int64
	Phone      string
	Channels   []string
}

// EnqueueBatch shares the caller's transaction; success still requires its commit.
// Statements are bounded, but no messages are discarded when the input is larger.
func EnqueueBatch(ctx context.Context, tx *sql.Tx, box *securestore.Box, messages []Message) error {
	var values strings.Builder
	args := make([]any, 0, min(len(messages), 256)*10)
	flush := func() error {
		if len(args) == 0 {
			return nil
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO message_outbox(topic,source_key,family_id,channel,recipient,status,delivered_at) VALUES `+values.String()+` ON DUPLICATE KEY UPDATE id=id`, args...)
		values.Reset()
		args = args[:0]
		return err
	}
	for _, message := range messages {
		recipient := message.Phone
		if box != nil {
			var err error
			recipient, err = box.Seal(recipient, "notification-recipient")
			if err != nil {
				return err
			}
		}
		channels := message.Channels
		if channels == nil {
			channels = []string{"sms", "in_app"}
		}
		for _, channel := range channels {
			if len(args) > 0 {
				values.WriteByte(',')
			}
			// The inbox entry becomes available with the parent's commit. This
			// records local availability only; it makes no claim about reading.
			state, delivered := "'pending'", "NULL"
			if channel == "in_app" {
				state, delivered = "'delivered'", "CURRENT_TIMESTAMP(6)"
			}
			fmt.Fprintf(&values, "(?,?,?,?,?,%s,%s)", state, delivered)
			args = append(args, message.Topic, message.Key, message.FamilyID, channel, recipient)
			if len(args) == 512*5 {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	return flush()
}

func CareInbox(ctx context.Context, tx *sql.Tx, box *securestore.Box, oldID, incidentID int64) error {
	var familyID int64
	var record userphone.Record
	err := tx.QueryRowContext(ctx, `SELECT f.family_id,f.family_phone,f.family_phone_cipher,f.family_phone_lookup,f.family_phone_protected
 FROM oldperson o JOIN family f ON f.family_id=o.family_id WHERE o.old_id=?`, oldID).Scan(&familyID, &record.Legacy, &record.Cipher, &record.Lookup, &record.Protected)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	opened, err := record.OpenFamilyStored(box, familyID)
	if err != nil {
		return err
	}
	phone := ""
	if opened != nil {
		phone = *opened
	}
	return EnqueueChannels(ctx, tx, box, "care", fmt.Sprint(incidentID), familyID, phone, []string{"in_app"})
}
