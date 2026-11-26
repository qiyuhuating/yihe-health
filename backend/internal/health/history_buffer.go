package health

import "sync"

// History slices stay valid until the synchronous Exec returns. Pools retain
// only zeroed bytes, with small and oversized records using the original path.
type historyBuffer struct {
	bytes []byte
	tier  int
}

var historyLimits = [...]int{16 << 10, 24 << 10, 32 << 10, 64 << 10, 80 << 10, 96 << 10, 128 << 10, 256 << 10}
var historyPools [len(historyLimits)]sync.Pool

func borrowHistoryBuffer(n int) *historyBuffer {
	if n < 8<<10 {
		return nil
	}
	for tier, limit := range historyLimits {
		if n <= limit {
			if item := historyPools[tier].Get(); item != nil {
				return item.(*historyBuffer)
			}
			return &historyBuffer{bytes: make([]byte, limit), tier: tier}
		}
	}
	return nil
}
func releaseHistoryBuffer(item *historyBuffer) {
	if item == nil {
		return
	}
	clear(item.bytes)
	historyPools[item.tier].Put(item)
}
