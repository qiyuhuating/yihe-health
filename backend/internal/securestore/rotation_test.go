package securestore

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestKeyRotationAndRetirementFailClosed(t *testing.T) {
	key := func(n byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{n}, 32)) }
	a, b, lookup := key(1), key(2), key(3)
	first, err := NewKeyring("A", map[string]string{"A": a}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	old, err := first.Seal("sensitive historical record", "care-note")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewKeyring("B", map[string]string{"A": a, "B": b}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := rotated.Open(old, "care-note")
	if err != nil || plain != "sensitive historical record" {
		t.Fatal("old key unreadable", err)
	}
	newValue, err := rotated.Seal(plain, "care-note")
	if err != nil || !strings.HasPrefix(newValue, Prefix+"v2:B:") {
		t.Fatal("new data did not use B", err)
	}
	retired, err := NewKeyring("B", map[string]string{"B": b}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := retired.Open(newValue, "care-note"); err != nil || got != plain {
		t.Fatal("re-encrypted record failed after A retirement", err)
	}
	if _, err := retired.Open(old, "care-note"); err == nil {
		t.Fatal("retired key was silently substituted")
	}
	if first.Index("recipient", "13800000001") != retired.Index("recipient", "13800000001") {
		t.Fatal("encryption rotation changed deduplication index")
	}
	if _, err := retired.Open(newValue, "other-scope"); err == nil {
		t.Fatal("scope authentication bypass")
	}
	if _, err := rotated.Open(strings.Replace(old, "v2:A:", "v2:B:", 1), "care-note"); err == nil {
		t.Fatal("key-ID tampering accepted")
	}
	wrong, _ := NewKeyring("B", map[string]string{"B": key(4)}, lookup)
	if _, err := wrong.Open(newValue, "care-note"); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err := NewKeyring("B", map[string]string{"A": a}, lookup); err == nil {
		t.Fatal("missing active key accepted")
	}
	legacy, _ := New(a)
	legacyValue, _ := legacy.Seal("legacy record", "care-note")
	if got, err := rotated.Open(legacyValue, "care-note"); err != nil || got != "legacy record" {
		t.Fatal("legacy migration read failed", err)
	}
}
