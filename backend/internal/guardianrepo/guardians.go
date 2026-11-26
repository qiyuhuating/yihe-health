// Package guardianrepo owns guardian profile persistence.
package guardianrepo

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/guardiancontact"
	"yihe-care/internal/securestore"
)

// Guardian is the typed profile returned by the guardian management API.
type Guardian struct {
	ID      int64   `json:"customerId"`
	Name    *string `json:"customerName"`
	Age     *int64  `json:"customerAge"`
	Sex     *string `json:"customerSex"`
	Phone   *string `json:"customerPhone"`
	Address *string `json:"customerAddress"`
}

type Fields struct {
	Name    string
	Age     int
	Sex     string
	Phone   string
	Address string
}

// List reads a page and its count together, including when the page is empty.
func List(ctx context.Context, store db.ReadQuerier, box *securestore.Box, name string, page, pageSize int) ([]Guardian, int64, error) {
	const filter = ` FROM customer WHERE (?='' OR customer_name LIKE CONCAT('%',?,'%'))`
	const countSQL = `SELECT COUNT(*) AS total` + filter
	items := []Guardian{}
	var total int64
	if err := guardiancontact.VerifyKey(ctx, store, box); err != nil {
		return items, total, err
	}
	err := db.QueryEach(ctx, store, func(rows *sql.Rows) error {
		var id *int64
		var item Guardian
		var cipher *string
		var protected *bool
		if err := rows.Scan(&id, &item.Name, &item.Age, &item.Sex, &item.Phone, &item.Address, &cipher, &protected, &total); err != nil {
			return err
		}
		if id != nil {
			if protected == nil {
				return errors.New("guardian contact metadata is missing")
			}
			values, err := (guardiancontact.Record{Legacy: guardiancontact.Values{Phone: item.Phone, Address: item.Address}, Cipher: cipher, Protected: *protected}).Open(box, *id)
			if err != nil {
				return err
			}
			item.Phone, item.Address = values.Phone, values.Address
			item.ID = *id
			items = append(items, item)
		}
		return nil
	}, `SELECT page.*, totals.total FROM (`+countSQL+`) AS totals LEFT JOIN (
		 SELECT customer_id,customer_name,customer_age,customer_sex,customer_phone,customer_address,contact_cipher,contact_protected`+filter+`
		 ORDER BY customer_id LIMIT ? OFFSET ?) AS page
		 ON TRUE ORDER BY page.customer_id`, name, name, name, name, pageSize, (page-1)*pageSize)
	return items, total, err
}

func Create(ctx context.Context, store *sql.Tx, box *securestore.Box, fields Fields) (int64, error) {
	if err := guardiancontact.PrepareWrite(ctx, store, box); err != nil {
		return 0, err
	}
	id, err := db.InsertID(ctx, store,
		`INSERT INTO customer(customer_name,customer_age,customer_sex) VALUES(?,?,?)`,
		fields.Name, fields.Age, fields.Sex)
	if err != nil {
		return 0, err
	}
	if err := guardiancontact.Store(ctx, store, box, id, guardiancontact.Strings(fields.Phone, fields.Address)); err != nil {
		return 0, err
	}
	return 1, nil
}

func Update(ctx context.Context, store *sql.Tx, box *securestore.Box, id int64, fields Fields) (int64, error) {
	if err := guardiancontact.PrepareWrite(ctx, store, box); err != nil {
		return 0, err
	}
	var record guardiancontact.Record
	err := store.QueryRowContext(ctx, `SELECT customer_phone,customer_address,contact_cipher,contact_protected FROM customer WHERE customer_id=? FOR UPDATE`, id).
		Scan(&record.Legacy.Phone, &record.Legacy.Address, &record.Cipher, &record.Protected)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if _, err := record.Open(box, id); err != nil {
		return 0, err
	}
	if _, err := store.ExecContext(ctx,
		`UPDATE customer SET customer_name=?,customer_age=?,customer_sex=? WHERE customer_id=?`, fields.Name, fields.Age, fields.Sex, id); err != nil {
		return 0, err
	}
	if err := guardiancontact.Store(ctx, store, box, id, guardiancontact.Strings(fields.Phone, fields.Address)); err != nil {
		return 0, err
	}
	return 1, nil
}

var ErrGuardianNotFound = errors.New("guardian not found")
var ErrGuardianAssigned = errors.New("guardian has assigned residents")

// Delete locks the guardian before checking resident assignments and removing it.
func Delete(ctx context.Context, tx *sql.Tx, id int64) error {

	var found int64
	if err := tx.QueryRowContext(ctx, "SELECT customer_id FROM customer WHERE customer_id=? FOR UPDATE", id).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrGuardianNotFound
		}
		return err
	}
	var assigned bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM oldperson WHERE customer_id=?)", id).Scan(&assigned); err != nil {
		return err
	}
	if assigned {
		return ErrGuardianAssigned
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM customer WHERE customer_id=?", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrGuardianNotFound
	}
	return nil
}
