// Package residentprofile protects the resident contact and care display fields
// without changing name search, health computations or relationship identifiers.
package residentprofile

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"yihe-care/internal/securestore"
)

type Values struct {
	Phone    *string `json:"phone"`
	Room     *string `json:"room"`
	Illness  *string `json:"illness"`
	Warn     *string `json:"warn"`
	FoodLike *string `json:"foodLike"`
	FoodHate *string `json:"foodHate"`
	Drag     *string `json:"drag"`
}

func Strings(phone, room, illness, warn, foodLike, foodHate, drag string) Values {
	return Values{&phone, &room, &illness, &warn, &foodLike, &foodHate, &drag}
}

func (v Values) HasValues() bool {
	return v.Phone != nil || v.Room != nil || v.Illness != nil || v.Warn != nil || v.FoodLike != nil || v.FoodHate != nil || v.Drag != nil
}

func (v Values) valid() bool {
	for _, field := range []struct {
		value *string
		limit int
	}{{v.Phone, 12}, {v.Room, 50}, {v.Illness, 200}, {v.Warn, 50}, {v.FoodLike, 100}, {v.FoodHate, 100}, {v.Drag, 100}} {
		if field.value != nil && (!utf8.ValidString(*field.value) || utf8.RuneCountInString(*field.value) > field.limit) {
			return false
		}
	}
	return true
}

type Record struct {
	Legacy    Values
	Cipher    *string
	Lookup    *string
	Protected bool
}

func scope(id int64) string { return "resident-care-profile:" + strconv.FormatInt(id, 10) }

// Preserve utf8mb4_bin VARCHAR PAD SPACE equality for the existing phone lookup.
// Keep the original value in ciphertext; no general trimming or mobile-only rule.
func PhoneEqual(a, b string) bool { return strings.TrimRight(a, " ") == strings.TrimRight(b, " ") }

func phoneIndex(box *securestore.Box, phone string) string {
	return box.Index("resident-contact-phone", strings.TrimRight(phone, " "))
}

func Protect(box *securestore.Box, id int64, values Values) (Record, error) {
	if box == nil || id < 1 || !values.valid() {
		return Record{}, errors.New("invalid resident profile protection input")
	}
	payload, err := json.Marshal(struct {
		Version int    `json:"version"`
		Fields  Values `json:"fields"`
	}{1, values})
	if err != nil {
		return Record{}, errors.New("cannot encode resident profile")
	}
	cipher, err := box.Seal(string(payload), scope(id))
	if err != nil {
		return Record{}, err
	}
	record := Record{Cipher: &cipher, Protected: true}
	if values.Phone != nil {
		lookup := phoneIndex(box, *values.Phone)
		record.Lookup = &lookup
	}
	return record, nil
}

func strictJSON(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing resident profile data")
	}
	return nil
}

func (r Record) Open(box *securestore.Box, id int64) (Values, error) {
	if !r.Protected {
		if r.Cipher != nil || r.Lookup != nil || !r.Legacy.valid() {
			return Values{}, errors.New("inconsistent legacy resident profile")
		}
		return r.Legacy, nil
	}
	if box == nil || id < 1 || r.Legacy.HasValues() || r.Cipher == nil || len(*r.Cipher) > 16384 || !strings.HasPrefix(*r.Cipher, securestore.Prefix) {
		return Values{}, errors.New("invalid protected resident profile")
	}
	plain, err := box.Open(*r.Cipher, scope(id))
	if err != nil || len(plain) > 8192 {
		return Values{}, errors.New("resident profile authentication failed")
	}
	var wire struct {
		Version int             `json:"version"`
		Fields  json.RawMessage `json:"fields"`
	}
	var values Values
	var fields map[string]json.RawMessage
	if strictJSON([]byte(plain), &wire) != nil || wire.Version != 1 || strictJSON(wire.Fields, &values) != nil ||
		json.Unmarshal(wire.Fields, &fields) != nil || len(fields) != 7 || !values.valid() {
		return Values{}, errors.New("invalid resident profile payload")
	}
	for _, key := range []string{"phone", "room", "illness", "warn", "foodLike", "foodHate", "drag"} {
		if _, present := fields[key]; !present {
			return Values{}, errors.New("resident profile payload is missing a field")
		}
	}
	if values.Phone == nil {
		if r.Lookup != nil {
			return Values{}, errors.New("unexpected resident phone lookup")
		}
	} else if r.Lookup == nil || subtle.ConstantTimeCompare([]byte(*r.Lookup), []byte(phoneIndex(box, *values.Phone))) != 1 {
		return Values{}, errors.New("resident phone lookup mismatch")
	}
	return values, nil
}
