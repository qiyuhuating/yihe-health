package residentprofile

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"yihe-care/internal/securestore"
)

func testBox(t *testing.T, encryption, lookup string) *securestore.Box {
	t.Helper()
	key := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(value, 32))) }
	box, err := securestore.NewKeyring("A", map[string]string{"A": key(encryption)}, key(lookup))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func TestResidentProfilePreservesValuesAndPhoneEquality(t *testing.T) {
	box := testBox(t, "A", "B")
	for _, values := range []Values{{}, Strings("", "", "", "", "", "", ""), Strings("010-12345678", "A栋301室", strings.Repeat("病", 200), "禁忌", "米饭", "花生", "药品")} {
		record, err := Protect(box, 1, values)
		if err != nil {
			t.Fatal("valid profile was rejected", err)
		}
		opened, err := record.Open(box, 1)
		if err != nil || !reflect.DeepEqual(opened, values) || record.Legacy.HasValues() {
			t.Fatal("protected profile lost NULL, empty string, Unicode or field ordering", err)
		}
	}
	if !PhoneEqual("13800000001 ", "13800000001") || !PhoneEqual("", " ") || PhoneEqual(" 13800000001", "13800000001") || PhoneEqual("13800000001\t", "13800000001") {
		t.Fatal("phone comparison changed VARCHAR PAD SPACE semantics")
	}
	if phoneIndex(box, "13800000001 ") != phoneIndex(box, "13800000001") {
		t.Fatal("equivalent phone values generated different indexes")
	}
	tooLong := Strings("", "", strings.Repeat("病", 201), "", "", "", "")
	if _, err := Protect(box, 1, tooLong); err == nil {
		t.Fatal("protection bypassed existing field limits")
	}
}

func TestResidentProfileRejectsUnauthenticatedValues(t *testing.T) {
	box := testBox(t, "A", "B")
	values := Strings("13800000001", "301", "condition", "caution", "rice", "peanut", "medication")
	valid, err := Protect(box, 1, values)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"missing key", "wrong AEAD", "wrong lookup", "wrong row", "raw mixed", "missing lookup", "unexpected lookup", "plaintext", "damaged", "version", "unknown field", "missing fields", "case variant field"} {
		t.Run(scenario, func(t *testing.T) {
			record, reader, id := valid, box, int64(1)
			switch scenario {
			case "missing key":
				reader = nil
			case "wrong AEAD":
				reader = testBox(t, "C", "B")
			case "wrong lookup":
				reader = testBox(t, "A", "C")
			case "wrong row":
				id = 2
			case "raw mixed":
				record.Legacy = values
			case "missing lookup":
				record.Lookup = nil
			case "unexpected lookup":
				record, err = Protect(box, 1, Values{})
				record.Lookup = valid.Lookup
			case "plaintext":
				plain := "synthetic plaintext"
				record.Cipher = &plain
			case "damaged":
				broken := "sne1:v2:A:***"
				record.Cipher = &broken
			default:
				payload, _ := json.Marshal(struct {
					Version int    `json:"version"`
					Fields  Values `json:"fields"`
				}{2, values})
				if scenario == "unknown field" {
					payload = []byte(`{"version":1,"fields":{"unknown":"value"}}`)
				} else if scenario == "missing fields" {
					payload = []byte(`{"version":1,"fields":{"phone":"13800000001"}}`)
				} else if scenario == "case variant field" {
					payload, _ = json.Marshal(struct {
						Version int    `json:"version"`
						Fields  Values `json:"fields"`
					}{1, values})
					payload = []byte(strings.Replace(string(payload), `"phone":`, `"PHONE":`, 1))
				}
				cipher, sealErr := box.Seal(string(payload), scope(1))
				if sealErr != nil {
					t.Fatal(sealErr)
				}
				record.Cipher = &cipher
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := record.Open(reader, id); err == nil {
				t.Fatal("invalid protected profile was accepted")
			}
		})
	}
}
