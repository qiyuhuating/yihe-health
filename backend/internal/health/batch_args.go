package health

import "sync"

// A buffer belongs to one Save call. Exec consumes its arguments synchronously;
// release clears the full capacity, including upsert slots beyond history.
type batchArgs struct {
	values []any
	tier   int
}

var batchArgLimits = [...]int{32, 128, 512, 1024, 3000}
var batchArgPools [len(batchArgLimits)]sync.Pool

func borrowBatchArgs(n int) *batchArgs {
	for tier, limit := range batchArgLimits {
		if n <= limit {
			if item := batchArgPools[tier].Get(); item != nil {
				return item.(*batchArgs)
			}
			return &batchArgs{values: make([]any, limit), tier: tier}
		}
	}
	return &batchArgs{values: make([]any, n), tier: -1}
}
func releaseBatchArgs(item *batchArgs) {
	clear(item.values)
	if item.tier >= 0 {
		batchArgPools[item.tier].Put(item)
	}
}
