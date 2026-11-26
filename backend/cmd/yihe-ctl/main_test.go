package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUnifiedProtectDispatchValidatesCommandWithoutDatabase(t *testing.T) {
	for _, args := range [][]string{nil, {"other", "family-phones"}, {"protect", "unknown"}} {
		if err := dispatch(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid command: %v", args)
		}
	}
	var out bytes.Buffer
	if err := dispatch([]string{"protect", "family-phones", "--help"}, &out); err != nil ||
		!strings.Contains(out.String(), "1..1000") {
		t.Fatalf("help failed: %v", err)
	}
}
