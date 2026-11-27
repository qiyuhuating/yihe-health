package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"yihe-care/internal/auth"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

// Read the family and authenticate its stored phone once per incident. Both
// notification channels use this same snapshot of the bound recipient.
func incidentRecipient(ctx context.Context, tx *sql.Tx, box *securestore.Box, residentID int64) (int64, string, bool, error) {
	var familyID int64
	var record userphone.Record
	err := tx.QueryRowContext(ctx, "SELECT f.family_id,f.family_phone,f.family_phone_cipher,f.family_phone_lookup,f.family_phone_protected FROM oldperson o JOIN family f ON f.family_id=o.family_id WHERE o.old_id=?", residentID).Scan(&familyID, &record.Legacy, &record.Cipher, &record.Lookup, &record.Protected)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	phone, err := record.OpenFamilyStored(box, familyID)
	if err != nil {
		return 0, "", false, err
	}
	if phone == nil {
		return familyID, "", true, nil
	}
	return familyID, *phone, true, nil
}

func queueIncidentPhone(ctx context.Context, tx *sql.Tx, box *securestore.Box, incidentID int64, phone string) (int64, error) {
	if !auth.ValidPhone(phone) {
		return 0, nil
	}
	sealed, key, err := SealNotificationRecipient(box, phone)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO care_notification_outbox(incident_id,channel,recipient,recipient_key) VALUES (?,'sms',?,?) ON DUPLICATE KEY UPDATE id=id", incidentID, sealed, key)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// EnqueueIncident writes both the care SMS outbox and in-app inbox under
// the caller-owned incident transaction. Acknowledge only after COMMIT.
func EnqueueIncident(ctx context.Context, tx *sql.Tx, box *securestore.Box, incidentID, residentID int64) (string, error) {
	familyID, phone, found, err := incidentRecipient(ctx, tx, box, residentID)
	if err != nil {
		return "", err
	}
	if !found {
		return "no_recipient", nil
	}
	count, err := queueIncidentPhone(ctx, tx, box, incidentID, phone)
	if err != nil {
		return "", err
	}
	if err = EnqueueChannels(ctx, tx, box, "care", fmt.Sprint(incidentID), familyID, phone, []string{"in_app"}); err != nil {
		return "", err
	}
	if count > 0 {
		return "pending", nil
	}
	return "no_recipient", nil
}
