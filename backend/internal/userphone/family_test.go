package userphone

import "testing"

func TestFamilyPhoneRoleAndRowIsolation(t *testing.T) {
	box := testBox(t, 7)
	value, err := ProtectFamily(box, 42, "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := RevealFamily(box, 42, value); err != nil || got != "13800138000" {
		t.Fatal("family phone cannot be authenticated")
	}
	if _, err := RevealFamily(box, 43, value); err == nil {
		t.Fatal("ciphertext moved between families")
	}
	if _, err := Reveal(box, 42, value); err == nil {
		t.Fatal("family ciphertext accepted for an administrator with the same ID")
	}
	record := Record{Cipher: &value.Ciphertext, Lookup: &value.Lookup, Protected: true}
	if _, err := record.OpenFamilyStored(nil, 42); err == nil {
		t.Fatal("missing family key did not fail closed")
	}
	legacy := "13800138000"
	record.Legacy = &legacy
	if _, err := record.OpenFamilyStored(box, 42); err == nil {
		t.Fatal("protected ciphertext fell back to leftover plaintext")
	}
	record.Protected = false
	if _, err := record.OpenFamilyStored(box, 42); err == nil {
		t.Fatal("legacy flag concealed encrypted metadata")
	}
	if got, err := (Record{Legacy: &legacy}).OpenFamilyStored(nil, 42); err != nil || got == nil || *got != legacy {
		t.Fatal("explicit legacy-only record is no longer readable")
	}
}
