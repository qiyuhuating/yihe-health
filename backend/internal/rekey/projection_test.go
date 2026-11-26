package rekey

import (
	"database/sql"
	"encoding/json"
	"testing"
)

func TestCoordinateProjectionRewrapsBothOrNeither(t *testing.T) {
	old, current, retired, legacy := boxes(t)
	target := target{table: "analytics_outbox", column: "payload", scope: "location", format: coordinateProjection}
	oldLng := sealed(t, old, "113.123", "location")
	oldLat := sealed(t, legacy, "28.123", "location")
	encode := func(lng, lat any) string {
		payload, _ := json.Marshal(map[string]any{"lng": lng, "lat": lat, "device_id": 7, "source": "gps", "future": map[string]int{"revision": 4}})
		return string(payload)
	}
	var counts Counts
	result, changed, err := convert(current, target, []any{int64(1)}, sql.NullString{String: encode(oldLng, oldLat), Valid: true}, true, &counts)
	if err != nil || !changed || counts.Values != 2 || counts.OldKey != 1 || counts.Legacy != 1 {
		t.Fatalf("conversion failed: %+v %v", counts, err)
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal([]byte(result), &payload) != nil || string(payload["device_id"]) != "7" || string(payload["future"]) != `{"revision":4}` {
		t.Fatal("non-coordinate payload changed")
	}
	for key, want := range map[string]string{"lng": "113.123", "lat": "28.123"} {
		var envelope string
		if json.Unmarshal(payload[key], &envelope) != nil {
			t.Fatal("coordinate was not a string")
		}
		if plain, err := retired.Open(envelope, "location"); err != nil || plain != want {
			t.Fatal("coordinate payload changed or A remains")
		}
	}
	for _, tc := range []struct {
		name, value    string
		invalid, plain int64
	}{
		{"one corrupt", encode(oldLng, "sne1:v2:MISSING:AAAA"), 1, 0},
		{"one wrong scope", encode(oldLng, sealed(t, old, "private", "session")), 1, 0},
		{"number", encode(oldLng, 28.123), 1, 0},
		{"missing field", `{"lng":null}`, 1, 0},
		{"array", `[]`, 1, 0},
		{"json null", `null`, 1, 0},
		{"plaintext", encode("113.123", "28.123"), 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var counts Counts
			_, changed, err := convert(current, target, []any{int64(1)}, sql.NullString{String: tc.value, Valid: true}, true, &counts)
			if err != nil || changed || counts.Invalid != tc.invalid || counts.Plaintext != tc.plain {
				t.Fatalf("unsafe projection accepted: %+v", counts)
			}
		})
	}
	counts = Counts{}
	if _, changed, err := convert(current, target, nil, sql.NullString{}, true, &counts); err != nil || changed || counts.Empty != 1 {
		t.Fatal("delete tombstone was not preserved")
	}
	counts = Counts{}
	if _, changed, err := convert(current, target, nil, sql.NullString{String: encode(nil, nil), Valid: true}, true, &counts); err != nil || changed || counts.Empty != 2 || counts.Values != 2 {
		t.Fatal("null coordinates were changed")
	}
}
