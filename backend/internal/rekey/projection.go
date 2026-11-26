package rekey

import (
	"database/sql"
	"encoding/json"

	"yihe-care/internal/securestore"
)

// MySQL location triggers copy the original ciphertext into historical events.
// Rewrap both coordinates atomically, retaining all other payload properties.
// Deletions have a SQL NULL payload and are handled as empty by convert.
func convertCoordinates(box *securestore.Box, t target, keys []any, stored string, apply bool, counts *Counts) (string, bool, error) {
	var payload map[string]json.RawMessage
	if json.Unmarshal([]byte(stored), &payload) != nil || payload == nil {
		counts.Values++
		counts.Invalid++
		return "", false, nil
	}
	direct := t
	direct.format = directEnvelope
	invalidBefore := counts.Invalid
	changed := false
	for _, key := range []string{"lng", "lat"} {
		raw, exists := payload[key]
		if !exists {
			counts.Values++
			counts.Invalid++
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) != nil {
			counts.Values++
			counts.Invalid++
			continue
		}
		value := sql.NullString{String: text, Valid: string(raw) != "null"}
		replacement, update, err := convert(box, direct, keys, value, apply, counts)
		if err != nil {
			return "", false, err
		}
		if update {
			payload[key], err = json.Marshal(replacement)
			if err != nil {
				return "", false, err
			}
			changed = true
		}
	}
	if counts.Invalid != invalidBefore || !changed {
		return "", false, nil
	}
	encoded, err := json.Marshal(payload)
	return string(encoded), true, err
}
