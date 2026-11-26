package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
)

func TestPatchNumericAndMissingRowHandling(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  int64
		ok    bool
	}{
		{json.Number("9223372036854775807"), 9223372036854775807, true},
		{json.Number("9223372036854775808"), 0, false},
		{json.Number("1.5"), 0, false},
		{" 42 ", 42, true},
		{float64(1.5), 0, false},
		{float64(9223372036854775808), 0, false},
		{float64(1e19), 0, false},
		{float64(-9223372036854775808), -9223372036854775808, true},
	} {
		got, ok := int64Val(map[string]any{"id": tc.value}, "id")
		if got != tc.want || ok != tc.ok {
			t.Fatalf("int64Val(%v): got (%d, %v), want (%d, %v)", tc.value, got, ok, tc.want, tc.ok)
		}
	}
	if !isNotFound(fmt.Errorf("lookup: %w", sql.ErrNoRows)) {
		t.Fatal("wrapped sql.ErrNoRows must be classified as not found")
	}
}
