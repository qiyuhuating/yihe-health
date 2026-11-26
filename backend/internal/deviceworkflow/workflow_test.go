package deviceworkflow

import (
	"strings"
	"testing"
	"time"

	"yihe-care/internal/devicecredential"
	"yihe-care/internal/devicerepo"
)

func TestVerifyDevice(t *testing.T) {
	secret := strings.Repeat("A", 32)
	hash, err := devicecredential.Hash(secret)
	if err != nil {
		t.Fatal(err)
	}
	disabled := 0
	device := &devicerepo.DeviceRecord{Secret: hash, Status: &disabled}
	if _, failure, err := Verify(device, strings.Repeat("B", 32)); err != nil || failure != InvalidCredential {
		t.Fatalf("invalid secret: failure=%q, err=%v", failure, err)
	}
	if _, failure, err := Verify(device, secret); err != nil || failure != DeviceDisabled {
		t.Fatalf("disabled device: failure=%q, err=%v", failure, err)
	}
	if _, failure, err := Verify(nil, secret); err != nil || failure != InvalidCredential {
		t.Fatalf("missing device: failure=%q, err=%v", failure, err)
	}
}

func TestBindCodeFailure(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	residentID := int64(1)
	cases := []struct {
		code *devicerepo.BindCode
		want Failure
	}{
		{nil, CodeNotFound},
		{&devicerepo.BindCode{Used: 1, OldID: &residentID}, CodeUsed},
		{&devicerepo.BindCode{OldID: &residentID, ExpireTime: &past}, CodeExpired},
		{&devicerepo.BindCode{}, ResidentMissing},
		{&devicerepo.BindCode{OldID: &residentID}, ""},
	}
	for _, tc := range cases {
		if got := bindCodeFailure(tc.code, now); got != tc.want {
			t.Errorf("bindCodeFailure(%+v) = %q, want %q", tc.code, got, tc.want)
		}
	}
}
