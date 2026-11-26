package handlers

import (
	"container/list"
	"encoding/json"
	"sync"
	"time"
)

const (
	statisticsCacheTTL           = 30 * time.Second
	statisticsCacheMaxEntries    = 2048
	statisticsCacheMaxValueBytes = 64 << 10
	statisticsCacheEntryOverhead = 256
)

// statisticsCache keeps serialized statistics in memory while samples remain in
// the database. Its budget accounts for payloads and approximate metadata; it is
// not a hard limit on process heap or RSS.
type statisticsCache struct {
	core     *coreCache
	mu       sync.Mutex
	maxBytes int64
	bytes    int64
	entries  map[[32]byte]*list.Element
	lru      list.List
}

type statisticsCacheEntry struct {
	key       [32]byte
	revision  int64
	value     json.RawMessage
	expiresAt time.Time
}

func newStatisticsCache(maxBytes int64) *statisticsCache {
	if maxBytes <= 0 {
		return nil
	}
	return &statisticsCache{maxBytes: maxBytes, entries: make(map[[32]byte]*list.Element)}
}

func newHealthStatisticsCache(maxBytes int64) *statisticsCache {
	core := newCoreCache(maxBytes / 2)
	if core == nil {
		return newStatisticsCache(maxBytes)
	}
	cache := newStatisticsCache(maxBytes - maxBytes/2)
	cache.core = core
	return cache
}

// get returns immutable bytes owned by the cache. Callers may encode them but
// must not modify the returned slice.
func (c *statisticsCache) get(key [32]byte, revision int64, now time.Time) (json.RawMessage, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	element := c.entries[key]
	if element == nil {
		return nil, false
	}
	entry := element.Value.(*statisticsCacheEntry)
	if !now.Before(entry.expiresAt) || entry.revision < revision {
		c.remove(element)
		return nil, false
	}
	if entry.revision != revision {
		// An older request must not discard a newer completed result.
		return nil, false
	}
	c.lru.MoveToFront(element)
	return entry.value, true
}

func (c *statisticsCache) put(key [32]byte, revision int64, value json.RawMessage, now time.Time) {
	if c == nil || len(value) > statisticsCacheMaxValueBytes {
		return
	}
	cost := int64(len(value)) + statisticsCacheEntryOverhead
	if cost > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if previous := c.entries[key]; previous != nil {
		if previous.Value.(*statisticsCacheEntry).revision > revision {
			return
		}
		c.remove(previous)
	}
	for c.bytes > c.maxBytes-cost || c.lru.Len() >= statisticsCacheMaxEntries {
		c.remove(c.lru.Back())
	}
	entry := &statisticsCacheEntry{
		key:       key,
		revision:  revision,
		value:     append(json.RawMessage(nil), value...),
		expiresAt: now.Add(statisticsCacheTTL),
	}
	c.entries[key] = c.lru.PushFront(entry)
	c.bytes += cost
}

func (c *statisticsCache) remove(element *list.Element) {
	entry := element.Value.(*statisticsCacheEntry)
	delete(c.entries, entry.key)
	c.bytes -= int64(len(entry.value)) + statisticsCacheEntryOverhead
	c.lru.Remove(element)
}
