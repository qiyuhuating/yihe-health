package handlers

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func statisticsTestKey(n uint64) (key [32]byte) {
	binary.LittleEndian.PutUint64(key[:], n)
	return key
}

func TestStatisticsCacheDisabled(t *testing.T) {
	for _, budget := range []int64{-1, 0} {
		cache := newStatisticsCache(budget)
		if cache != nil {
			t.Fatalf("budget %d enabled cache", budget)
		}
		cache.put(statisticsTestKey(1), 0, json.RawMessage(`{}`), time.Time{})
		if _, hit := cache.get(statisticsTestKey(1), 0, time.Time{}); hit {
			t.Fatal("disabled cache returned a hit")
		}
	}
}

func TestStatisticsCacheRevisionAndOwnedInput(t *testing.T) {
	cache := newStatisticsCache(1024)
	key := statisticsTestKey(1)
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	value := json.RawMessage(`{"count":1}`)
	cache.put(key, 0, value, now)
	value[9] = '9'
	if got, hit := cache.get(key, 0, now); !hit || string(got) != `{"count":1}` {
		t.Fatalf("cache did not retain its own bytes at revision zero: %s, hit=%v", got, hit)
	}
	if _, hit := cache.get(key, 1, now); hit {
		t.Fatal("newer committed revision returned stale statistics")
	}
	if _, hit := cache.get(key, 0, now); hit {
		t.Fatal("invalidated entry was retained")
	}
}

func TestStatisticsCacheTTLDoesNotSlideOnRead(t *testing.T) {
	cache := newStatisticsCache(1024)
	key := statisticsTestKey(1)
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	cache.put(key, 2, json.RawMessage(`{}`), now)
	if _, hit := cache.get(key, 2, now.Add(30*time.Second-time.Nanosecond)); !hit {
		t.Fatal("entry expired before the TTL boundary")
	}
	if _, hit := cache.get(key, 2, now.Add(30*time.Second)); hit {
		t.Fatal("entry survived at the TTL boundary")
	}
	if _, hit := cache.get(key, 2, now); hit {
		t.Fatal("expired entry was retained")
	}
}

func TestStatisticsCacheByteBudgetAndLRU(t *testing.T) {
	value := json.RawMessage(`{}`)
	cost := int64(len(value) + statisticsCacheEntryOverhead)
	cache := newStatisticsCache(2 * cost)
	now := time.Time{}
	cache.put(statisticsTestKey(1), 1, value, now)
	cache.put(statisticsTestKey(2), 1, value, now)
	if _, hit := cache.get(statisticsTestKey(1), 1, now); !hit {
		t.Fatal("exact byte budget rejected the first entry")
	}
	cache.put(statisticsTestKey(3), 1, value, now)
	if _, hit := cache.get(statisticsTestKey(2), 1, now); hit {
		t.Fatal("least recently used entry survived byte-budget eviction")
	}
	for _, n := range []uint64{1, 3} {
		if _, hit := cache.get(statisticsTestKey(n), 1, now); !hit {
			t.Fatalf("entry %d was unexpectedly evicted", n)
		}
	}
}

func TestStatisticsCacheRejectsOversizedBeforeEviction(t *testing.T) {
	now := time.Time{}
	key := statisticsTestKey(1)
	for _, tc := range []struct {
		name   string
		budget int64
		large  json.RawMessage
	}{
		{"byte-budget", 258, json.RawMessage(`{"count":1}`)},
		{"value-limit", 1 << 20, json.RawMessage(strings.Repeat(" ", (64<<10)+1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := newStatisticsCache(tc.budget)
			cache.put(key, 1, json.RawMessage(`{}`), now)
			cache.put(key, 2, tc.large, now)
			cache.put(statisticsTestKey(2), 1, tc.large, now)
			if got, hit := cache.get(key, 1, now); !hit || string(got) != `{}` {
				t.Fatalf("oversized admission removed a useful entry: %s, hit=%v", got, hit)
			}
		})
	}
	cache := newStatisticsCache((64 << 10) + statisticsCacheEntryOverhead)
	cache.put(key, 1, json.RawMessage(strings.Repeat(" ", 64<<10)), now)
	if got, hit := cache.get(key, 1, now); !hit || len(got) != 64<<10 {
		t.Fatal("exact maximum value size was rejected")
	}
}

func TestStatisticsCacheEntryLimit(t *testing.T) {
	cache := newStatisticsCache(2 << 20)
	now := time.Time{}
	for n := uint64(0); n <= statisticsCacheMaxEntries; n++ {
		cache.put(statisticsTestKey(n), 1, json.RawMessage(`{}`), now)
	}
	if _, hit := cache.get(statisticsTestKey(0), 1, now); hit {
		t.Fatal("oldest entry survived entry-count eviction")
	}
	for _, n := range []uint64{1, statisticsCacheMaxEntries} {
		if _, hit := cache.get(statisticsTestKey(n), 1, now); !hit {
			t.Fatalf("entry %d unexpectedly missing at count limit", n)
		}
	}
}

func TestStatisticsCacheLateOlderRequest(t *testing.T) {
	cache := newStatisticsCache(1024)
	key := statisticsTestKey(1)
	now := time.Time{}
	cache.put(key, 3, json.RawMessage(`{"count":3}`), now)
	cache.put(key, 2, json.RawMessage(`{"count":2}`), now.Add(time.Second))
	if _, hit := cache.get(key, 2, now.Add(2*time.Second)); hit {
		t.Fatal("older request received a different revision")
	}
	if got, hit := cache.get(key, 3, now.Add(2*time.Second)); !hit || string(got) != `{"count":3}` {
		t.Fatalf("older request displaced a newer result: %s, hit=%v", got, hit)
	}
}

func TestStatisticsCacheReplacementBudget(t *testing.T) {
	cache := newStatisticsCache(2 * (statisticsCacheEntryOverhead + 2))
	now := time.Time{}
	key := statisticsTestKey(1)
	cache.put(key, 1, json.RawMessage(`{}`), now)
	cache.put(key, 2, json.RawMessage(`{}`), now)
	cache.put(statisticsTestKey(2), 1, json.RawMessage(`{}`), now)
	if _, hit := cache.get(key, 2, now); !hit {
		t.Fatal("replacement counted its previous payload against the budget")
	}
	if _, hit := cache.get(statisticsTestKey(2), 1, now); !hit {
		t.Fatal("second entry missing after replacement")
	}
}

func TestStatisticsCacheConcurrent(t *testing.T) {
	cache := newStatisticsCache(16 << 10)
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for worker := 0; worker < 24; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				key := statisticsTestKey(uint64((i + worker) % 128))
				revision := int64(i / 128)
				cache.put(key, revision, json.RawMessage(`{"count":1}`), now)
				if got, hit := cache.get(key, revision, now); hit && string(got) != `{"count":1}` {
					t.Errorf("concurrent hit returned damaged JSON: %s", got)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	key := statisticsTestKey(1000)
	cache.put(key, 10, json.RawMessage(`{}`), now)
	if _, hit := cache.get(key, 10, now); !hit {
		t.Fatal("cache unavailable after concurrent accesses")
	}
}
