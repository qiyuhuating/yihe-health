package handlers

import (
	"container/list"
	"hash/maphash"
	"math"
	"sort"
	"sync"
	"time"

	"yihe-care/internal/health"
	"yihe-care/internal/observability"
)

const coreShardCount = 16

type coreKey struct {
	resident       int64
	source, device string
}

type coreSeries struct {
	key                coreKey
	revision, from, to int64
	unit               string
	timestamps         []int64
	values             []float64
	ids                []string
	exactInteger       bool
	expires            time.Time
}

func (s *coreSeries) cost() int64 {
	return int64(cap(s.timestamps)*8 + cap(s.values)*8 + cap(s.ids)*80 + len(s.key.source) + len(s.key.device) + len(s.unit) + statisticsCacheEntryOverhead)
}

type coreShard struct {
	mu      sync.Mutex
	entries map[coreKey]*list.Element
	lru     list.List
	bytes   int64
}

type coreCache struct {
	seed     maphash.Seed
	shards   [coreShardCount]coreShard
	perShard int64
	advance  coreAdvanceQueue
}

func newCoreCache(budget int64) *coreCache {
	if !coreEnabled || budget < coreShardCount*statisticsCacheEntryOverhead {
		return nil
	}
	c := &coreCache{seed: maphash.MakeSeed(), perShard: budget / coreShardCount}
	for i := range c.shards {
		c.shards[i].entries = make(map[coreKey]*list.Element)
	}
	return c
}

func (c *coreCache) shard(key coreKey) *coreShard {
	return &c.shards[maphash.Comparable(c.seed, key)%coreShardCount]
}

func (c *coreCache) get(key coreKey, revision, from, to int64, now time.Time) (health.Statistics, bool) {
	if c == nil || revision <= 0 || from >= to {
		return health.Statistics{}, false
	}
	shard := c.shard(key)
	shard.mu.Lock()
	e := shard.entries[key]
	if e == nil {
		shard.mu.Unlock()
		return health.Statistics{}, false
	}
	s := e.Value.(*coreSeries)
	if !now.Before(s.expires) {
		shard.remove(e)
		shard.mu.Unlock()
		return health.Statistics{}, false
	}
	if s.revision != revision || from < s.from || to > s.to {
		shard.mu.Unlock()
		return health.Statistics{}, false
	}
	shard.lru.MoveToFront(e)
	shard.mu.Unlock()
	// The immutable slices remain alive for this query even after eviction.
	return coreQuery(s, from, to)
}

func (c *coreCache) put(s *coreSeries) {
	if c == nil || s == nil {
		return
	}
	cost := s.cost()
	if cost > c.perShard {
		return
	}
	shard := c.shard(s.key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if prev := shard.entries[s.key]; prev != nil {
		old := prev.Value.(*coreSeries)
		if old.revision > s.revision || (old.revision == s.revision && old.from <= s.from && old.to >= s.to && old.expires.After(s.expires.Add(-statisticsCacheTTL))) {
			return
		}
		// Replacement never grows the entry count. Reuse the existing map
		// slot and list node, then evict from the unchanged LRU tail.
		shard.bytes += cost - old.cost()
		prev.Value = s
		shard.lru.MoveToFront(prev)
		for shard.bytes > c.perShard {
			shard.remove(shard.lru.Back())
		}
		return
	}
	for shard.bytes > c.perShard-cost || shard.lru.Len() >= statisticsCacheMaxEntries/coreShardCount {
		shard.remove(shard.lru.Back())
	}
	shard.entries[s.key] = shard.lru.PushFront(s)
	shard.bytes += cost
}
func (s *coreShard) remove(e *list.Element) {
	v := e.Value.(*coreSeries)
	delete(s.entries, v.key)
	s.bytes -= v.cost()
	s.lru.Remove(e)
}

// UnixNano is only defined within its int64 range. Require microsecond-aligned
// bounds so a subset cannot introduce extra SQL candidates at a rounded edge.
func coreWindow(req healthStatisticsRequest) (coreKey, int64, int64, bool) {
	key := coreKey{req.OldID, req.Source, req.SourceDevice}
	from, to := req.From.UnixNano(), req.To.UnixNano()
	ok := req.Metric == "heart_rate" && req.Semantics == "instant" &&
		req.From.Nanosecond()%1000 == 0 && req.To.Nanosecond()%1000 == 0 &&
		time.Unix(0, from).Equal(req.From) && time.Unix(0, to).Equal(req.To) && from < to
	return key, from, to, ok
}

func makeCoreSeries(key coreKey, revision, from, to int64, samples []health.Sample, ids []string, candidates int, now time.Time) *coreSeries {
	if revision <= 0 || len(samples) != candidates || len(samples) > 10000 || (len(ids) != 0 && len(ids) != len(samples)) {
		return nil
	}
	s := &coreSeries{key: key, revision: revision, from: from, to: to, exactInteger: true,
		timestamps: make([]int64, 0, len(samples)), values: make([]float64, 0, len(samples)), ids: ids, expires: now.Add(statisticsCacheTTL)}
	ordered := true
	for i := range samples {
		p := &samples[i]
		at := p.Start.UnixNano()
		if p.Source != key.source || p.SourceDevice != key.device || p.Metric != "heart_rate" ||
			p.Semantics != "instant" || p.TimeBasis != "" || p.Deleted || p.Value == nil ||
			p.Unit != "count/min" || !time.Unix(0, at).Equal(p.Start) ||
			at < from || at >= to {
			return nil
		}
		v := *p.Value
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		s.exactInteger = s.exactInteger && !math.Signbit(v) && v <= 900719925474 && math.Trunc(v) == v
		ordered = ordered && (i == 0 || at >= s.timestamps[i-1])
		s.unit = p.Unit
		s.timestamps = append(s.timestamps, at)
		s.values = append(s.values, v)
	}
	// SQL preserves revision order for scalar floating-point parity. A
	// historical correction can put a timestamp out of order. Exact,
	// bounded nonnegative integers can safely be sorted for binary search.
	if !ordered {
		if !s.exactInteger {
			return nil
		}
		points := make([]corePoint, len(samples))
		for i := range points {
			points[i].at, points[i].value = s.timestamps[i], s.values[i]
			if len(ids) != 0 {
				points[i].id = ids[i]
			}
		}
		sort.Slice(points, func(i, j int) bool { return points[i].at < points[j].at })
		for i, point := range points {
			s.timestamps[i], s.values[i] = point.at, point.value
			if len(ids) != 0 {
				s.ids[i] = point.id
			}
		}
	}
	return s
}

// stale returns an immutable projection only when it can be updated using
// committed changes without changing floating-point arithmetic.
func (c *coreCache) stale(key coreKey, revision, from, to int64, now time.Time) *coreSeries {
	if c == nil {
		return nil
	}
	shard := c.shard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	e := shard.entries[key]
	if e == nil {
		return nil
	}
	s := e.Value.(*coreSeries)
	if !now.Before(s.expires) || s.revision >= revision || from < s.from || to > s.to ||
		!s.exactInteger || len(s.ids) != len(s.values) {
		return nil
	}
	return s
}

// atRevision is deliberately stricter than stale: a local write cannot fill
// a gap left by another replica or a previously missed publication.
func (c *coreCache) atRevision(key coreKey, revision int64, now time.Time) *coreSeries {
	shard := c.shard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	e := shard.entries[key]
	if e == nil {
		return nil
	}
	s := e.Value.(*coreSeries)
	if s.revision != revision || !now.Before(s.expires) || !s.exactInteger || len(s.ids) != len(s.values) {
		return nil
	}
	return s
}

// mayAdvance accepts an older local revision because an earlier queued
// commit may still be processing. The worker requires the exact predecessor.
func (c *coreCache) mayAdvance(key coreKey, before int64, now time.Time) bool {
	shard := c.shard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	e := shard.entries[key]
	if e == nil {
		return false
	}
	s := e.Value.(*coreSeries)
	return s.revision <= before && now.Before(s.expires) && s.exactInteger && len(s.ids) == len(s.values)
}

// advanceCoreAfterCommit transfers an immutable normalized batch to a bounded
// best-effort queue. The caller enqueues before releasing the owner domain.
func (h *Handler) advanceCoreAfterCommit(o health.Owner, batch *health.PreparedBatch, samples []health.Sample) {
	if h.healthCache == nil || h.healthCache.core == nil {
		return
	}
	before, through := batch.StatisticsProjectionRange()
	if before <= 0 || through <= before {
		return
	}
	cache := h.healthCache.core
	now := time.Now()
	seen := make(map[coreKey]struct{}, 1)
	var keys []coreKey
	for _, sample := range samples {
		key := coreKey{o.OldID, sample.Source, sample.SourceDevice}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if cache.mayAdvance(key, before, now) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return
	}
	if !cache.advance.submit(func() {
		now := time.Now()
		var candidates []*coreSeries
		for _, key := range keys {
			if s := cache.atRevision(key, before, now); s != nil {
				candidates = append(candidates, s)
			}
		}
		if len(candidates) == 0 {
			return
		}
		changes, err := batch.StatisticsProjectionChanges(o)
		if err != nil {
			return
		}
		for _, s := range candidates {
			if next := applyCoreChanges(s, through, changes, now); next != nil {
				cache.put(next)
				observability.Default.Add("yihe_health_core_commit_advances_total", 1)
			}
		}
	}) {
		observability.Default.Add("yihe_health_core_commit_advance_dropped_total", 1)
	}
}

type corePoint struct {
	id    string
	at    int64
	value float64
}

// applyCoreChanges is restricted to exact nonnegative integer series so
// timestamp sorting cannot change the scalar sum or signed-zero extrema.
// Each changed identity replaces its old observation, including tombstones
// and corrections that move a point across the coverage boundary.
func applyCoreChanges(s *coreSeries, revision int64, changes []health.Record, now time.Time) *coreSeries {
	if !s.exactInteger || len(s.ids) != len(s.values) || len(changes) == 0 || changes[len(changes)-1].Revision != revision {
		return nil
	}
	updated := make(map[string]corePoint, len(changes))
	previous := s.revision
	for _, record := range changes {
		p := record.Sample
		if record.OldID != s.key.resident || len(record.SampleID) != 64 || record.Revision <= previous || record.Revision > revision {
			return nil
		}
		previous = record.Revision
		if p.Source != s.key.source || p.SourceDevice != s.key.device || p.Metric != "heart_rate" {
			continue
		}
		point := corePoint{id: record.SampleID}
		at := p.Start.UnixNano()
		if !time.Unix(0, at).Equal(p.Start) {
			return nil
		}
		if !p.Deleted && p.Semantics == "instant" && at >= s.from && at < s.to {
			if p.TimeBasis != "" || p.Unit != "count/min" || p.Value == nil {
				return nil
			}
			v := *p.Value
			if math.Signbit(v) || !(v <= 900719925474) || math.Trunc(v) != v {
				return nil
			}
			point.at, point.value = at, v
		} else {
			point.id = ""
		}
		updated[record.SampleID] = point
	}
	next := *s
	next.revision = revision
	next.expires = now.Add(statisticsCacheTTL)
	if len(updated) == 0 {
		return &next
	}
	changedPoints := make([]corePoint, 0, len(updated))
	for _, point := range updated {
		if point.id != "" {
			changedPoints = append(changedPoints, point)
		}
	}
	if len(changedPoints) == 0 {
		affected := false
		for _, id := range s.ids {
			if _, exists := updated[id]; exists {
				affected = true
				break
			}
		}
		if !affected {
			return &next
		}
	}
	sort.Slice(changedPoints, func(i, j int) bool { return changedPoints[i].at < changedPoints[j].at })
	// Merge directly into the immutable destination columns. Charge the actual
	// allocated capacity to the cache, including space for replaced points.
	capacity := min(10000, len(s.ids)+len(changedPoints))
	next.timestamps = make([]int64, 0, capacity)
	next.values = make([]float64, 0, capacity)
	next.ids = make([]string, 0, capacity)
	next.unit = "count/min"
	appendPoint := func(id string, at int64, value float64) {
		next.ids = append(next.ids, id)
		next.timestamps = append(next.timestamps, at)
		next.values = append(next.values, value)
	}
	j := 0
	for i, id := range s.ids {
		if _, changed := updated[id]; changed {
			continue
		}
		for j < len(changedPoints) && changedPoints[j].at < s.timestamps[i] {
			p := changedPoints[j]
			appendPoint(p.id, p.at, p.value)
			j++
		}
		appendPoint(id, s.timestamps[i], s.values[i])
	}
	for _, p := range changedPoints[j:] {
		appendPoint(p.id, p.at, p.value)
	}
	if len(next.ids) > 10000 {
		return nil
	}
	return &next
}
