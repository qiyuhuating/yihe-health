package auth

import "testing"

func TestPatchCredentialValidation(t *testing.T) {
	if ValidNewPassword(string([]byte{'A', 'a', '1', 0xff, 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i'})) {
		t.Fatal("invalid UTF-8 must not be accepted as a password")
	}
	if ValidPhone("1234567890") || !ValidPhone("13800138000") {
		t.Fatal("current eleven-digit phone contract must remain unchanged")
	}
	if Matches("", "$2b$12$invalid") {
		t.Fatal("empty password must not authenticate")
	}
}
