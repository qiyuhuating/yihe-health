package socialworkflow

import (
	"context"
	"strconv"

	"yihe-care/internal/blob"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db"
	"yihe-care/internal/notify"
	"yihe-care/internal/securestore"
	"yihe-care/internal/socialrepo"
)

// PublishAnnouncement commits its images, notice, durable notifications and
// access audit atomically after locking the publisher's current duty.
func PublishAnnouncement(ctx context.Context, store *db.Database, actor int64, manager *blob.Manager, box *securestore.Box, title, body string, names []string) error {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.ManageContent)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(names) > 0 {
		if err := manager.Require(ctx, tx, names); err != nil {
			return err
		}
	}
	id, err := socialrepo.InsertAnnouncement(ctx, tx, title, body, names)
	if err != nil {
		return err
	}
	recipients, err := socialrepo.AnnouncementRecipients(ctx, tx, box)
	if err != nil {
		return err
	}
	messages := make([]notify.Message, 0, len(recipients))
	for _, target := range recipients {
		messages = append(messages, notify.Message{
			Topic: "announcement", Key: strconv.FormatInt(id, 10), FamilyID: target.FamilyID, Phone: target.Phone,
		})
	}
	if err := notify.EnqueueBatch(ctx, tx, box, messages); err != nil {
		return err
	}
	if err := auditContentChange(ctx, tx, actor, "notice_created"); err != nil {
		return err
	}
	return tx.Commit()
}
