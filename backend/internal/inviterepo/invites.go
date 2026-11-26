package inviterepo

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/userphone"
)

type Resident struct {
	ID    int64   `json:"oldId"`
	Name  *string `json:"name"`
	Phone *string `json:"phone"`
	Room  *string `json:"room"`
}

func ListResidents(ctx context.Context, store db.RowsQuerier) ([]Resident, error) {
	return db.QueryList(ctx, store, func(row *sql.Rows) (Resident, error) {
		var resident Resident
		err := row.Scan(&resident.ID, &resident.Name, &resident.Phone, &resident.Room)
		return resident, err
	}, "SELECT old_id,name,phone,room FROM oldperson")
}

type CodeStatus struct {
	Used         int16
	ExpireTime   *time.Time
	ResidentName *string
}

func ReadCode(ctx context.Context, store *db.Database, code string) (CodeStatus, error) {
	var status CodeStatus
	err := store.QueryRowContext(ctx,
		`SELECT ic.used,ic.expire_time,o.name FROM invite_code ic
		 LEFT JOIN oldperson o ON ic.old_id=o.old_id WHERE ic.code=?`,
		code).Scan(&status.Used, &status.ExpireTime, &status.ResidentName)
	return status, err
}

type LockedCode struct {
	ID         int64
	ResidentID *int64
	Used       int16
	ExpireTime *time.Time
}

func LockCode(ctx context.Context, tx *sql.Tx, code string) (LockedCode, error) {
	var invitation LockedCode
	err := tx.QueryRowContext(ctx,
		"SELECT id,old_id,used,expire_time FROM invite_code WHERE code=? FOR UPDATE",
		code).Scan(&invitation.ID, &invitation.ResidentID, &invitation.Used, &invitation.ExpireTime)
	return invitation, err
}

func PhoneRegistered(ctx context.Context, tx *sql.Tx, phone, lookup string) (bool, error) {
	var registered bool
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE
		    (user_phone_protected=false AND user_phone=?) OR
		    (user_phone_protected=true AND user_phone_lookup=?))
		    OR EXISTS(SELECT 1 FROM family WHERE `+userphone.FamilyMatch+`)`, phone, lookup, phone, lookup).Scan(&registered)
	return registered, err
}

type ResidentBinding struct {
	Name     *string
	FamilyID *int64
}

func LockResident(ctx context.Context, tx *sql.Tx, residentID int64) (ResidentBinding, error) {
	var bound ResidentBinding
	err := tx.QueryRowContext(ctx,
		"SELECT name, family_id FROM oldperson WHERE old_id=? FOR UPDATE",
		residentID).Scan(&bound.Name, &bound.FamilyID)
	return bound, err
}

func LockPasswordlessFamily(ctx context.Context, tx *sql.Tx, familyID int64, phone, lookup string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT family_id FROM family
		 WHERE family_id=? AND `+userphone.FamilyMatch+` AND family_password IS NULL
		 AND NOT EXISTS(SELECT 1 FROM users WHERE (user_phone_protected=false AND user_phone=?) OR (user_phone_protected=true AND user_phone_lookup=?)) FOR UPDATE`,
		familyID, phone, lookup, phone, lookup).Scan(&id)
	return id, err
}

func ConsumeCode(ctx context.Context, tx *sql.Tx, invitationID, familyID int64, at time.Time) (bool, error) {
	result, err := tx.ExecContext(ctx,
		"UPDATE invite_code SET used=1,used_family_id=?,used_time=? WHERE id=? AND used=0",
		familyID, at, invitationID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}
