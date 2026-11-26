// Package guardiancontact protects guardian display contacts without changing
// name search, age validation or resident relationship identifiers.
package guardiancontact

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"yihe-care/internal/securestore"
)

type Values struct {
	Phone   *string `json:"phone"`
	Address *string `json:"address"`
}

func Strings(phone, address string) Values { return Values{&phone, &address} }
func (v Values) HasValues() bool           { return v.Phone != nil || v.Address != nil }
func (v Values) valid() bool {
	for _, field := range []struct {
		value *string
		limit int
	}{{v.Phone, 12}, {v.Address, 200}} {
		if field.value != nil && (!utf8.ValidString(*field.value) || utf8.RuneCountInString(*field.value) > field.limit) {
			return false
		}
	}
	return true
}

type Record struct {
	Legacy    Values
	Cipher    *string
	Protected bool
}

func scope(id int64) string { return "guardian-contact:" + strconv.FormatInt(id, 10) }

func Protect(box *securestore.Box, id int64, values Values) (Record, error) {
	if box == nil || id < 1 || !values.valid() {
		return Record{}, errors.New("invalid guardian contact protection input")
	}
	payload, err := json.Marshal(struct {
		Version int    `json:"version"`
		Fields  Values `json:"fields"`
	}{1, values})
	if err != nil {
		return Record{}, errors.New("cannot encode guardian contact")
	}
	cipher, err := box.Seal(string(payload), scope(id))
	if err != nil {
		return Record{}, err
	}
	return Record{Cipher: &cipher, Protected: true}, nil
}

func strictJSON(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing guardian contact data")
	}
	return nil
}

func (r Record) Open(box *securestore.Box, id int64) (Values, error) {
	if !r.Protected {
		if r.Cipher != nil || !r.Legacy.valid() {
			return Values{}, errors.New("inconsistent legacy guardian contact")
		}
		return r.Legacy, nil
	}
	if box == nil || id < 1 || r.Legacy.HasValues() || r.Cipher == nil || len(*r.Cipher) > 8192 || !strings.HasPrefix(*r.Cipher, securestore.Prefix) {
		return Values{}, errors.New("invalid protected guardian contact")
	}
	plain, err := box.Open(*r.Cipher, scope(id))
	if err != nil || len(plain) > 4096 {
		return Values{}, errors.New("guardian contact authentication failed")
	}
	var wire struct {
		Version int             `json:"version"`
		Fields  json.RawMessage `json:"fields"`
	}
	var values Values
	var fields map[string]json.RawMessage
	if strictJSON([]byte(plain), &wire) != nil || wire.Version != 1 || strictJSON(wire.Fields, &values) != nil ||
		json.Unmarshal(wire.Fields, &fields) != nil || len(fields) != 2 || !values.valid() {
		return Values{}, errors.New("invalid guardian contact payload")
	}
	for _, key := range []string{"phone", "address"} {
		if _, present := fields[key]; !present {
			return Values{}, errors.New("guardian contact payload is missing a field")
		}
	}
	return values, nil
}
