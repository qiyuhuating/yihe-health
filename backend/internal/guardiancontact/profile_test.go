package guardiancontact

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"yihe-care/internal/securestore"
)

func contactKey(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(value, 32)))
}
func contactBox(t *testing.T, encryption, lookup string) *securestore.Box {
	t.Helper()
	box, err := securestore.NewKeyring("A", map[string]string{"A": contactKey(encryption)}, contactKey(lookup))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func TestGuardianContactValues(t *testing.T) {
	box := contactBox(t, "A", "C")
	for _, values := range []Values{{}, Strings("", ""), Strings("010-12345678", strings.Repeat("地", 200))} {
		record, err := Protect(box, 1, values)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := record.Open(box, 1)
		if err != nil || !reflect.DeepEqual(opened, values) || record.Legacy.HasValues() {
			t.Fatal("contact lost NULL/empty/fixed-phone/Unicode semantics", err)
		}
	}
	for _, values := range []Values{Strings(strings.Repeat("1", 13), ""), Strings("", strings.Repeat("地", 201)), Strings(string([]byte{255}), "")} {
		if _, err := Protect(box, 1, values); err == nil {
			t.Fatal("contact escaped existing field limits")
		}
	}
}

func TestGuardianContactRejectsUnverified(t *testing.T) {
	box := contactBox(t, "A", "C")
	valid, err := Protect(box, 1, Strings("13800000001", "synthetic address"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"missing key", "wrong key", "wrong row", "mixed", "missing cipher", "plaintext", "damaged", "version", "unknown", "missing", "case", "trailing"} {
		t.Run(scenario, func(t *testing.T) {
			record, reader, id := valid, box, int64(1)
			switch scenario {
			case "missing key":
				reader = nil
			case "wrong key":
				reader = contactBox(t, "B", "C")
			case "wrong row":
				id = 2
			case "mixed":
				record.Legacy = Strings("", "")
			case "missing cipher":
				record.Cipher = nil
			case "plaintext", "damaged":
				value := "synthetic plaintext"
				if scenario == "damaged" {
					value = "sne1:v2:A:***"
				}
				record.Cipher = &value
			default:
				payload := `{"version":2,"fields":{"phone":null,"address":null}}`
				switch scenario {
				case "unknown":
					payload = `{"version":1,"fields":{"phone":null,"address":null,"extra":1}}`
				case "missing":
					payload = `{"version":1,"fields":{"phone":null}}`
				case "case":
					payload = `{"version":1,"fields":{"PHONE":null,"address":null}}`
				case "trailing":
					payload = `{"version":1,"fields":{"phone":null,"address":null}} {}`
				}
				cipher, err := box.Seal(payload, scope(1))
				if err != nil {
					t.Fatal(err)
				}
				record.Cipher = &cipher
			}
			if _, err := record.Open(reader, id); err == nil {
				t.Fatal("unverified contact was accepted")
			}
		})
	}
}

func TestGuardianContactReaderMarker(t *testing.T) {
	old := contactBox(t, "A", "C")
	binding := fingerprint(old)
	marker, err := old.Seal("guardian-contact:v1:"+binding, keyScope)
	if err != nil || verifyBinding(old, binding, marker) != nil {
		t.Fatal("valid reader marker rejected", err)
	}
	for _, reader := range []*securestore.Box{nil, contactBox(t, "B", "C"), contactBox(t, "A", "D")} {
		if verifyBinding(reader, binding, marker) == nil {
			t.Fatal("missing or mismatched reader/binding key accepted")
		}
	}
	current, _ := securestore.NewKeyring("B", map[string]string{"A": contactKey("A"), "B": contactKey("B")}, contactKey("C"))
	retired, _ := securestore.NewKeyring("B", map[string]string{"B": contactKey("B")}, contactKey("C"))
	if verifyBinding(current, binding, marker) != nil || verifyBinding(retired, binding, marker) == nil {
		t.Fatal("reader marker bypassed retirement ordering")
	}
	newMarker, err := current.Seal("guardian-contact:v1:"+binding, keyScope)
	if err != nil || verifyBinding(retired, binding, newMarker) != nil {
		t.Fatal("rewrapped marker requires the old reader", err)
	}
}
