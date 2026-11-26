package rekey

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"yihe-care/internal/securestore"
)

func boxes(t *testing.T) (old, current, retired, legacy *securestore.Box) {
	t.Helper()
	key := func(n byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{n}, 32)) }
	var err error
	old, err = securestore.NewKeyring("A", map[string]string{"A": key(1)}, key(3))
	if err != nil {
		t.Fatal(err)
	}
	current, err = securestore.NewKeyring("B", map[string]string{"A": key(1), "B": key(2)}, key(3))
	if err != nil {
		t.Fatal(err)
	}
	retired, err = securestore.NewKeyring("B", map[string]string{"B": key(2)}, key(3))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err = securestore.New(key(1))
	if err != nil {
		t.Fatal(err)
	}
	return
}

func sealed(t *testing.T, box *securestore.Box, plain, scope string) string {
	t.Helper()
	value, err := box.Seal(plain, scope)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestConvertAuthenticatesAndPreservesScope(t *testing.T) {
	old, current, retired, legacy := boxes(t)
	target := targets()[0]
	plain := "sensitive medical note; never report this value"
	oldValue := sealed(t, old, plain, target.scope)
	active := sealed(t, current, plain, target.scope)
	for _, tc := range []struct {
		name, value string
		valid       bool
		want        Counts
		change      bool
	}{
		{"old", oldValue, true, Counts{Values: 1, OldKey: 1}, true},
		{"legacy", sealed(t, legacy, plain, target.scope), true, Counts{Values: 1, Legacy: 1}, true},
		{"active", active, true, Counts{Values: 1, Active: 1}, false},
		{"null", "", false, Counts{Values: 1, Empty: 1}, false},
		{"empty", "", true, Counts{Values: 1, Empty: 1}, false},
		{"plaintext", plain, true, Counts{Values: 1, Plaintext: 1}, false},
		{"wrong scope", sealed(t, old, plain, "session"), true, Counts{Values: 1, Invalid: 1}, false},
		{"missing key", strings.Replace(oldValue, "v2:A:", "v2:MISSING:", 1), true, Counts{Values: 1, Invalid: 1}, false},
		{"corrupt active", active[:len(active)-3] + "***", true, Counts{Values: 1, Invalid: 1}, false},
		{"bad base64", "sne1:v2:B:***", true, Counts{Values: 1, Invalid: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, apply := range []bool{false, true} {
				var counts Counts
				replacement, change, err := convert(current, target, []any{int64(1)}, sql.NullString{String: tc.value, Valid: tc.valid}, apply, &counts)
				if err != nil || counts != tc.want || change != (apply && tc.change) {
					t.Fatalf("counts=%+v change=%t err=%v", counts, change, err)
				}
				if change {
					got, err := retired.Open(replacement, target.scope)
					if err != nil || got != plain {
						t.Fatal("rewrapped payload or scope changed")
					}
				}
			}
		})
	}
}

func TestJSONWrapperAndCompositeTOTP(t *testing.T) {
	old, current, retired, _ := boxes(t)
	for _, target := range targets() {
		if target.table != "auth_factor" && target.table != "rule_state" {
			continue
		}
		keys := []any{"ADMIN", int64(9)}
		if target.format == ruleEnvelope {
			keys = []any{int64(1), int64(2), "heart_rate"}
		}
		scope := target.fieldScope(keys)
		value := sealed(t, old, "private", scope)
		if target.format == ruleEnvelope {
			wrapped, _ := json.Marshal(map[string]any{"encrypted": value, "future": map[string]int{"revision": 7}})
			value = string(wrapped)
		}
		var counts Counts
		replacement, change, err := convert(current, target, keys, sql.NullString{String: value, Valid: true}, true, &counts)
		if err != nil || !change || counts.OldKey != 1 {
			t.Fatal("conversion rejected valid scoped envelope")
		}
		if target.format == ruleEnvelope {
			var wrapper map[string]json.RawMessage
			if json.Unmarshal([]byte(replacement), &wrapper) != nil || string(wrapper["future"]) != `{"revision":7}` {
				t.Fatal("JSON metadata was lost")
			}
			if json.Unmarshal(wrapper["encrypted"], &replacement) != nil {
				t.Fatal("bad wrapper")
			}
		}
		if got, err := retired.Open(replacement, scope); err != nil || got != "private" {
			t.Fatal("scope or plaintext changed")
		}
		if target.table == "auth_factor" {
			if _, err := retired.Open(replacement, "totp:FAMILY:9"); err == nil {
				t.Fatal("TOTP role binding lost")
			}
		}
	}
	jsonTarget := target{table: "rule_state", format: ruleEnvelope, scope: "rule-evidence"}
	for _, value := range []string{`null`, `not json`, `{"encrypted":2}`, `{"encrypted":"plaintext"}`} {
		var counts Counts
		_, change, err := convert(current, jsonTarget, nil, sql.NullString{String: value, Valid: true}, true, &counts)
		if err != nil || change || counts.Invalid != 1 {
			t.Fatal("malformed wrapper accepted")
		}
	}
}

func TestRotationGuardsAndReports(t *testing.T) {
	old, current, _, legacy := boxes(t)
	other, _ := securestore.NewKeyring("B", map[string]string{"B": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))}, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32)))
	for _, pair := range [][2]*securestore.Box{{nil, old}, {legacy, old}, {current, nil}, {current, other}} {
		if _, err := Run(context.Background(), &sql.DB{}, pair[0], pair[1], Options{BatchSize: 1, MaxRowsPerColumn: 1}); err == nil {
			t.Fatal("unsafe key configuration accepted")
		}
	}
	for _, options := range []Options{{BatchSize: 0, MaxRowsPerColumn: 1}, {BatchSize: 1001, MaxRowsPerColumn: 1}, {BatchSize: 1, MaxRowsPerColumn: 0}, {BatchSize: 1, MaxRowsPerColumn: 1, Pause: -1}} {
		if _, err := Run(context.Background(), &sql.DB{}, current, old, options); err == nil {
			t.Fatal("unbounded options accepted")
		}
	}
	if !current.SameLookupKey(old) || old.SameLookupKey(other) || (*securestore.Box)(nil).SameLookupKey(old) {
		t.Fatal("lookup-key equality is incorrect")
	}
	if clean(nil) || clean([]Column{{Complete: true}}) {
		t.Fatal("incomplete scope accepted as clean")
	}
	seen := map[string]bool{}
	for _, target := range targets() {
		if seen[target.name()] || len(target.keys) == 0 {
			t.Fatal("invalid registry")
		}
		seen[target.name()] = true
	}
}
