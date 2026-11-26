package health

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestBatchStatementsMatchOriginalAssembly(t *testing.T) {
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rows := 1; rows <= 200; rows++ {
				want := [...]string{
					expectedLookupSQL(rows),
					batchUpsertPrefix + valueRows(rows, 15) + batchUpsertSuffix,
					batchChangesPrefix + valueRows(rows, 5),
				}
				for kind := range want {
					if got := batchSQL(batchSQLKind(kind), rows); got != want[kind] {
						t.Errorf("kind=%d rows=%d SQL differs", kind, rows)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

var batchSQLSink string

func BenchmarkCompleteBatchSQL(b *testing.B) {
	for _, rows := range []int{1, 50, 200} {
		b.Run(fmt.Sprint(rows), func(b *testing.B) {
			valueRows(rows, 15)
			valueRows(rows, 5)
			valueRows(1, rows)
			for kind := batchLookup; kind <= batchChanges; kind++ {
				batchSQL(kind, rows)
			}
			b.Run("fragments", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					batchSQLSink = expectedLookupSQL(rows)
					batchSQLSink = batchUpsertPrefix + valueRows(rows, 15) + batchUpsertSuffix
					batchSQLSink = batchChangesPrefix + valueRows(rows, 5)
				}
			})
			b.Run("statements", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					batchSQLSink = batchSQL(batchLookup, rows)
					batchSQLSink = batchSQL(batchUpsert, rows)
					batchSQLSink = batchSQL(batchChanges, rows)
				}
			})
		})
	}
}

func expectedLookupSQL(rows int) string {
	parameters := make([]string, rows)
	for i := range parameters {
		parameters[i] = "?"
	}
	return batchLookupPrefix + "?" + batchLookupSuffix + "(" + strings.Join(parameters, ",") + ")"
}

// BenchmarkCompleteBatchSQL compares this retired fragment cache with complete statements.
var batchValueTemplates [4][201]atomic.Pointer[string]

func valueRows(rows, columns int) string {
	var slot *atomic.Pointer[string]
	if rows == 1 && columns > 0 && columns <= 200 {
		slot = &batchValueTemplates[3][columns]
	} else if rows > 0 && rows <= 200 {
		switch columns {
		case 1:
			slot = &batchValueTemplates[0][rows]
		case 5:
			slot = &batchValueTemplates[1][rows]
		case 15:
			slot = &batchValueTemplates[2][rows]
		}
	}
	if slot == nil {
		return buildValueRows(rows, columns)
	}
	if cached := slot.Load(); cached != nil {
		return *cached
	}
	value := buildValueRows(rows, columns)
	if slot.CompareAndSwap(nil, &value) {
		return value
	}
	return *slot.Load()
}
