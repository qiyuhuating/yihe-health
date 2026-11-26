package handlers

import (
	"container/list"
	"hash/maphash"
	"testing"
	"time"
)

func TestCoreCacheReplacementReusesNodeAndBudget(t *testing.T) {
	c := &coreCache{seed: maphash.MakeSeed()}
	for i := range c.shards {
		c.shards[i].entries = make(map[coreKey]*list.Element)
	}
	now := time.Now()
	a := &coreSeries{key: coreKey{1, "watch", "A"}, revision: 1, expires: now.Add(time.Hour)}
	shard := c.shard(a.key)
	key := coreKey{2, "watch", "A"}
	for c.shard(key) != shard {
		key.resident++
	}
	other := &coreSeries{key: key, revision: 1, expires: now.Add(time.Hour)}
	c.perShard = a.cost() + other.cost()
	c.put(a)
	c.put(other)
	node := shard.entries[a.key]
	if node == nil {
		t.Fatal("missing initial entry")
	}
	next := *a
	next.revision = 2
	next.values = make([]float64, 8)
	c.put(&next)
	if shard.entries[a.key] != node || node.Value != &next || shard.entries[key] != nil || shard.bytes != next.cost() || shard.lru.Len() != 1 {
		t.Fatal("replacement changed node, eviction or budget")
	}
	c.put(a)
	if node.Value != &next || shard.bytes != next.cost() {
		t.Fatal("older revision replaced newer entry")
	}
}

func BenchmarkCoreCacheReplace(b *testing.B) {
	c := &coreCache{seed: maphash.MakeSeed(), perShard: 1 << 20}
	for i := range c.shards {
		c.shards[i].entries = make(map[coreKey]*list.Element)
	}
	key := coreKey{1, "watch", "device"}
	a := &coreSeries{key: key, expires: time.Now().Add(time.Hour)}
	second := *a
	slots := [2]*coreSeries{a, &second}
	c.put(a)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 1; i <= b.N; i++ {
		next := slots[i%2]
		next.revision = int64(i)
		c.put(next)
	}
}
