package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/db"
	"yihe-care/internal/familyaddress"
	"yihe-care/internal/residentprofile"
	"yihe-care/internal/response"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

const oldPersonCols = `old_id, name, age, sex, phone, room, illness, warn, food_like,
	food_hate, drag, family_id, customer_id, be_time, device_id,care_profile_cipher,phone_lookup,care_profile_protected,
	bothdate,birthdate_cipher,birthdate_protected`

func scanOldPersonRow(row *sql.Rows, box *securestore.Box) (map[string]any, error) {
	var id int64
	var name, sex, phone, room, illness, warn, drag, foodLike, foodHate *string
	var profile residentprofile.Record
	var birthday residentprofile.BirthdateRecord
	var age *int
	var familyID, customerID, deviceID *int64
	var beTime *time.Time
	if err := row.Scan(&id, &name, &age, &sex, &phone, &room, &illness, &warn,
		&foodLike, &foodHate, &drag, &familyID, &customerID, &beTime, &deviceID,
		&profile.Cipher, &profile.Lookup, &profile.Protected, &birthday.Legacy, &birthday.Cipher, &birthday.Protected); err != nil {
		return nil, err
	}
	profile.Legacy = residentprofile.Values{Phone: phone, Room: room, Illness: illness, Warn: warn, FoodLike: foodLike, FoodHate: foodHate, Drag: drag}
	values, err := profile.Open(box, id)
	if err != nil {
		return nil, err
	}
	bothdate, err := birthday.Open(box, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"oldId": id, "name": name, "age": currentPersonAge(bothdate, age, time.Now()), "sex": sex, "phone": values.Phone,
		"room": values.Room, "illness": values.Illness, "warn": values.Warn, "drag": values.Drag,
		"foodLike": values.FoodLike, "foodHate": values.FoodHate, "familyId": familyID,
		"customerId": customerID, "beTime": dateOrNil(beTime),
		"bothdate": dateOrNil(bothdate), "deviceId": deviceID,
	}, nil
}

// POST /api/v1/residents/list —— 按姓名分页查询老人档案（ADMIN）。
func (h *Handler) listResidents(w http.ResponseWriter, r *http.Request) {
	name, p, ps, ok := namedPage(w, r)
	if !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadResidents)
	if !ok {
		return
	}
	defer tx.Rollback()
	ctx := r.Context()
	if internalError(w, residentprofile.VerifyKey(ctx, tx, h.Box)) {
		return
	}
	base := `SELECT ` + oldPersonCols + ` FROM oldperson WHERE (?='' OR name LIKE CONCAT('%',?,'%')) ORDER BY old_id`
	var total int64
	if internalError(w, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+base+`) AS t`, name, name).Scan(&total)) {
		return
	}
	list, err := db.QueryList(ctx, tx, func(rows *sql.Rows) (map[string]any, error) {
		return scanOldPersonRow(rows, h.Box)
	}, base+` LIMIT ? OFFSET ?`, name, name, ps, (p-1)*ps)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{
		"oldPeople": list, "total": total, "page": p, "pageSize": ps,
	})
}

// POST /api/v1/residents/details —— 老人详情（含家属/客户）分页查询（ADMIN）。
func (h *Handler) getResidentDetails(w http.ResponseWriter, r *http.Request) {
	name, p, ps, ok := namedPage(w, r)
	if !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadResidents)
	if !ok {
		return
	}
	defer tx.Rollback()
	ctx := r.Context()
	if internalError(w, residentprofile.VerifyKey(ctx, tx, h.Box)) {
		return
	}
	base := `SELECT o.old_id, o.name, o.age, o.sex, o.phone, o.room, o.illness, o.warn,
		o.food_like, o.food_hate, o.drag, o.family_id, o.customer_id, o.be_time,
		o.device_id, f.family_name, f.family_relation, f.family_phone, f.family_sex,
		f.family_address,f.family_address_cipher,f.family_address_protected,c.customer_name,
		f.family_phone_cipher,f.family_phone_lookup,f.family_phone_protected,
		o.care_profile_cipher,o.phone_lookup,o.care_profile_protected,o.bothdate,o.birthdate_cipher,o.birthdate_protected
		FROM oldperson o LEFT JOIN family f ON f.family_id=o.family_id
		LEFT JOIN customer c ON c.customer_id=o.customer_id
		WHERE (?='' OR o.name LIKE CONCAT('%',?,'%')) ORDER BY o.old_id`
	var total int64
	if internalError(w, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+base+`) AS t`, name, name).Scan(&total)) {
		return
	}
	list, err := db.QueryList(ctx, tx, func(rows *sql.Rows) (map[string]any, error) {
		var id int64
		var name, sex, phone, room, illness, warn, drag, foodLike, foodHate *string
		var age *int
		var familyID, customerID, deviceID *int64
		var beTime *time.Time
		var familyName, familyRelation, familyPhone, familySex, familyAddress, customerName *string
		var record userphone.Record
		var address familyaddress.Record
		var profile residentprofile.Record
		var birthday residentprofile.BirthdateRecord
		var protected, addressProtected sql.NullBool
		if err := rows.Scan(&id, &name, &age, &sex, &phone, &room, &illness, &warn,
			&foodLike, &foodHate, &drag, &familyID, &customerID, &beTime, &deviceID,
			&familyName, &familyRelation, &familyPhone, &familySex, &address.Legacy, &address.Cipher, &addressProtected,
			&customerName, &record.Cipher, &record.Lookup, &protected,
			&profile.Cipher, &profile.Lookup, &profile.Protected, &birthday.Legacy, &birthday.Cipher, &birthday.Protected); err != nil {
			return nil, err
		}
		profile.Legacy = residentprofile.Values{Phone: phone, Room: room, Illness: illness, Warn: warn, FoodLike: foodLike, FoodHate: foodHate, Drag: drag}
		values, err := profile.Open(h.Box, id)
		if err != nil {
			return nil, err
		}
		bothdate, err := birthday.Open(h.Box, id)
		if err != nil {
			return nil, err
		}
		if familyID != nil {
			record.Legacy = familyPhone
			record.Protected = protected.Bool
			var err error
			familyPhone, err = record.OpenFamilyStored(h.Box, *familyID)
			if err != nil {
				return nil, err
			}
			address.Protected = addressProtected.Bool
			familyAddress, err = address.Open(h.Box, *familyID)
			if err != nil {
				return nil, err
			}
		}
		return map[string]any{
			"oldId": id, "name": name, "age": currentPersonAge(bothdate, age, time.Now()), "sex": sex, "phone": values.Phone,
			"room": values.Room, "illness": values.Illness, "warn": values.Warn, "drag": values.Drag,
			"foodLike": values.FoodLike, "foodHate": values.FoodHate, "familyId": familyID,
			"customerId": customerID, "beTime": dateOrNil(beTime),
			"bothdate": dateOrNil(bothdate), "deviceId": deviceID,
			"familyName": familyName, "familyRelation": familyRelation,
			"familyPhone": familyPhone, "familySex": familySex,
			"familyAddress": familyAddress, "customerName": customerName,
		}, nil
	}, base+` LIMIT ? OFFSET ?`, name, name, ps, (p-1)*ps)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{
		"oldPersonDetails": list, "total": total, "page": p, "pageSize": ps,
	})
}

func namedPage(w http.ResponseWriter, r *http.Request) (string, int, int, bool) {
	m, ok := bodyMap(w, r)
	if !ok {
		return "", 0, 0, false
	}
	name, present := vstr(m, "name")
	if !present {
		write400(w, "请求参数不完整")
		return "", 0, 0, false
	}
	page, size, ok := pageFromBody(w, m)
	return name, page, size, ok
}
