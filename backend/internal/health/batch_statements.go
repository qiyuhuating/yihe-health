package health

import (
	"strings"
	"sync/atomic"
)

type batchSQLKind uint8

const (
	batchLookup batchSQLKind = iota
	batchUpsert
	batchChanges
)

const batchLookupPrefix = `SELECT sample_id,version,revision,CASE WHEN version >= `
const batchLookupSuffix = ` THEN payload ELSE NULL END,payload_cipher FROM health_sample WHERE sample_id IN `
const batchUpsertPrefix = `INSERT INTO health_sample(sample_id,old_id,device_id,epoch,version,revision,payload,payload_cipher,source,source_device,metric,semantics,sample_start,sample_end,deleted) VALUES `
const batchUpsertSuffix = `
	 AS new ON DUPLICATE KEY UPDATE version=new.version,revision=new.revision,payload=new.payload,payload_cipher=new.payload_cipher,source=new.source,source_device=new.source_device,metric=new.metric,semantics=new.semantics,sample_start=new.sample_start,sample_end=new.sample_end,deleted=new.deleted`
const batchChangesPrefix = `INSERT INTO health_change(revision,old_id,sample_id,payload,payload_cipher) VALUES `

// Cache complete immutable statements, not just VALUES fragments. This avoids
// rebuilding/re-hashing multi-kilobyte SQL on every batch. The bounded cache
// contains no argument values, identifiers or patient data.
var batchStatements [3][201]atomic.Pointer[string]

func buildBatchSQL(kind batchSQLKind, rows int) string {
	switch kind {
	case batchLookup:
		var out strings.Builder
		out.Grow(len(batchLookupPrefix) + len(batchLookupSuffix) + rows*6 + 4)
		out.WriteString(batchLookupPrefix + "?" + batchLookupSuffix)
		out.WriteByte('(')
		for i := 0; i < rows; i++ {
			if i > 0 {
				out.WriteByte(',')
			}
			out.WriteByte('?')
		}
		out.WriteByte(')')
		return out.String()
	case batchUpsert:
		return batchUpsertPrefix + buildValueRows(rows, 15) + batchUpsertSuffix
	case batchChanges:
		return batchChangesPrefix + buildValueRows(rows, 5)
	default:
		panic("invalid internal batch statement kind")
	}
}

func batchSQL(kind batchSQLKind, rows int) string {
	if rows < 1 || rows > 200 {
		return buildBatchSQL(kind, rows)
	}
	slot := &batchStatements[kind][rows]
	if cached := slot.Load(); cached != nil {
		return *cached
	}
	query := buildBatchSQL(kind, rows)
	if slot.CompareAndSwap(nil, &query) {
		return query
	}
	return *slot.Load()
}

func buildValueRows(rows, columns int) string {
	var out strings.Builder
	out.Grow(rows * (columns*6 + 3))
	for i := 0; i < rows; i++ {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('(')
		for j := 0; j < columns; j++ {
			if j > 0 {
				out.WriteByte(',')
			}
			out.WriteByte('?')
		}
		out.WriteByte(')')
	}
	return out.String()
}
