package socialrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

type AnnouncementRecipient struct {
	FamilyID int64
	Phone    string
}

// InsertAnnouncement writes the notice and its image group in one transaction.
func InsertAnnouncement(ctx context.Context, tx *sql.Tx, title, body string, names []string) (int64, error) {
	group, err := db.InsertID(ctx, tx, "INSERT INTO picture_group () VALUES ()")
	if err != nil {
		return 0, err
	}
	for _, name := range names {
		result, err := tx.ExecContext(ctx, "INSERT INTO pic(pic_id,pic_name) VALUES(?,?)", group, name)
		if err != nil {
			return 0, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if affected != 1 {
			return 0, errors.New("announcement image insert affected unexpected number of rows")
		}
	}
	return db.InsertID(ctx, tx, "INSERT INTO `text`(title,text_main,time,pic_id) VALUES(?,?,?,?)",
		title, body, time.Now(), group)
}

func AnnouncementRecipients(ctx context.Context, tx *sql.Tx, box *securestore.Box) ([]AnnouncementRecipient, error) {
	items := []AnnouncementRecipient{}
	err := db.QueryEach(ctx, tx, func(rows *sql.Rows) error {
		var id int64
		var record userphone.Record
		if err := rows.Scan(&id, &record.Legacy, &record.Cipher, &record.Lookup, &record.Protected); err != nil {
			return err
		}
		phone, err := record.OpenFamilyStored(box, id)
		if err != nil {
			return err
		}
		if phone != nil {
			items = append(items, AnnouncementRecipient{FamilyID: id, Phone: *phone})
		}
		return nil
	}, `SELECT DISTINCT f.family_id,f.family_phone,f.family_phone_cipher,f.family_phone_lookup,f.family_phone_protected
 FROM family f INNER JOIN oldperson o ON o.family_id=f.family_id`)
	return items, err
}
