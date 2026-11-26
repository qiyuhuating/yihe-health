package db

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"testing"
)

// This is the historical row encoding used by schemaDigest.
func legacySchemaFields(raw []sql.RawBytes) []*string {
	record := make([]*string, len(raw))
	for i, v := range raw {
		if v != nil {
			s := string(v)
			record[i] = &s
		}
	}
	return record
}

func TestSchemaRecordPreservesFingerprintBytes(t *testing.T) {
	rows := [][]sql.RawBytes{
		{nil, {}, []byte("table"), []byte("<>&"), []byte("\"\\\n")},
		{[]byte("中文"), []byte{0xff, 0x80}, []byte{0}, []byte("\u2028\u2029"), nil},
		{[]byte(""), nil, []byte("0"), []byte("a"), []byte("NULL")},
	}
	var old, new bytes.Buffer
	a, b := json.NewEncoder(&old), json.NewEncoder(&new)
	record := newSchemaRecord(len(rows[0]))
	for _, row := range rows {
		copy(record.raw, row)
		if err := a.Encode(legacySchemaFields(row)); err != nil {
			t.Fatal(err)
		}
		if err := b.Encode(record.values()); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(old.Bytes(), new.Bytes()) {
		t.Fatalf("fingerprint bytes changed: %q != %q", old.Bytes(), new.Bytes())
	}
}

func BenchmarkSchemaRecordEncoding(b *testing.B) {
	raw := []sql.RawBytes{[]byte("health_sample"), []byte("3"), []byte("sample_start"), []byte("datetime(6)"), []byte("NO"), nil, {}, []byte("generated"), []byte("utf8mb4"), []byte("utf8mb4_bin")}
	for _, reuse := range []bool{false, true} {
		name := "legacy"
		if reuse {
			name = "reused"
		}
		b.Run(name, func(b *testing.B) {
			enc := json.NewEncoder(io.Discard)
			record := newSchemaRecord(len(raw))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if reuse {
					copy(record.raw, raw)
					if err := enc.Encode(record.values()); err != nil {
						b.Fatal(err)
					}
				} else {
					// Include the per-row scan destination construction from schemaDigest.
					current := make([]sql.RawBytes, len(raw))
					dest := make([]any, len(raw))
					for i := range current {
						dest[i] = &current[i]
					}
					for i, v := range raw {
						*dest[i].(*sql.RawBytes) = v
					}
					if err := enc.Encode(legacySchemaFields(current)); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
