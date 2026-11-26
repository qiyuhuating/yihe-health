// Package ratelimit 按来源 IP 和路由执行 10 分钟滑动窗口限流。
package ratelimit

import (
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"yihe-care/internal/response"
)

const (
	window     = 10 * time.Minute
	shardCount = 32
)

// 每条路由在十分钟窗口内允许的请求数。
var limits = map[string]int{
	"/api/v1/setup":                30,
	"/api/v1/auth/login":           30,
	"/api/v1/inviteCode/join":      30,
	"/api/v1/device/bind":          30,
	"/api/v1/inviteCode/checkCode": 60,
	"/api/v1/device/login":         60,
	// 硬件端点免会话鉴权，也按 IP 限流：上限刻意宽松（合法网关 NAT 后多设备共享
	// 一个出口 IP），只用于拦住未认证洪水消耗数据库事务，不用于精确配额。
	"/api/v1/device/heartbeat":   1800,
	"/api/v1/device/report":      1800,
	"/api/v1/device/healthBatch": 1800,
	"/api/v1/device/commandPull": 1800,
	"/api/v1/device/commandAck":  1800,
	"/api/v1/device/fencePoint":  1800,
	"/api/v1/device/sos":         300,
	"/api/v1/care/receipt":       600,
	// 桌面令牌端点：令牌校验失败无独立锁定，靠路径限流兜底防弱口令线速尝试。
	"/api/v1/desktop/exit": 30,
}

type Limiter struct {
	shards    [shardCount]limiterShard
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	trusted   []netip.Prefix
}

type limitKey struct{ ip, path string }

type limiterShard struct {
	mu                                            sync.Mutex
	hits                                          map[limitKey]*hitWindow
	maxKeys, maxSlots, slots, highKeys, highSlots int
	capacityDenied, quotaDenied                   uint64
}

// A bounded FIFO preserves the exact sliding window. Every accepted timestamp
// is removed at most once; denied attempts never extend the window.
type hitWindow struct {
	times      []time.Time
	head, size int
	limit      int
}

func (w *hitWindow) allowBudget(now time.Time, available int) bool {
	for w.size > 0 && now.Sub(w.times[w.head]) >= window {
		w.head = (w.head + 1) % len(w.times)
		w.size--
	}
	if w.size == w.limit {
		return false
	}
	if w.size == len(w.times) {
		if available <= 0 {
			return false
		}
		// Sparse clients start with one slot. Grow geometrically up to the
		// quota, preserving FIFO order even when the old ring has wrapped.
		times := make([]time.Time, min(w.limit, min(len(w.times)+available, max(1, len(w.times)*2))))
		n := copy(times, w.times[w.head:])
		copy(times[n:], w.times[:w.head])
		w.times, w.head = times, 0
	}
	w.times[(w.head+w.size)%len(w.times)] = now
	w.size++
	return true
}

func New() *Limiter {
	l, err := NewWithOptions(Options{})
	if err != nil {
		panic(err)
	}
	return l
}

// Options bounds all retained keys and timestamp capacity, not just each key.
// Caps are partitioned among shards; admission never takes a global lock.
type Options struct {
	MaxKeys           int
	MaxSlots          int
	TrustedProxyCIDRs []string
}

func normalized(o Options) (Options, []netip.Prefix, error) {
	if len(o.TrustedProxyCIDRs) > 64 {
		return o, nil, fmt.Errorf("at most 64 trusted proxy CIDRs are allowed")
	}
	if o.MaxKeys == 0 {
		o.MaxKeys = 8192
	}
	if o.MaxSlots == 0 {
		o.MaxSlots = 262144
	}
	if o.MaxKeys < shardCount || o.MaxKeys > 1048576 || o.MaxSlots < o.MaxKeys || o.MaxSlots > 16777216 {
		return o, nil, fmt.Errorf("invalid limiter caps: keys must be 32..1048576, slots must be keys..16777216")
	}
	var trusted []netip.Prefix
	for _, cidr := range o.TrustedProxyCIDRs {
		if cidr == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return o, nil, fmt.Errorf("invalid trusted proxy CIDR %q", cidr)
		}
		trusted = append(trusted, prefix.Masked())
	}
	return o, trusted, nil
}

func ValidateOptions(o Options) error { _, _, err := normalized(o); return err }

func NewWithOptions(o Options) (*Limiter, error) {
	o, trusted, err := normalized(o)
	if err != nil {
		return nil, err
	}
	l := &Limiter{stop: make(chan struct{}), done: make(chan struct{})}
	l.trusted = trusted
	for i := range l.shards {
		l.shards[i].hits = make(map[limitKey]*hitWindow)
		l.shards[i].maxKeys = o.MaxKeys / shardCount
		if i < o.MaxKeys%shardCount {
			l.shards[i].maxKeys++
		}
		l.shards[i].maxSlots = o.MaxSlots / shardCount
		if i < o.MaxSlots%shardCount {
			l.shards[i].maxSlots++
		}
	}
	go l.janitor()
	return l, nil
}

// Close stops background cleanup. It is safe to call repeatedly or concurrently.
func (l *Limiter) Close() {
	l.closeOnce.Do(func() { close(l.stop) })
	<-l.done
}

func (l *Limiter) shard(ip string) *limiterShard {
	// All limited paths for one client share a shard, with independent quotas.
	// Hashing the IP avoids allocating a combined string key per request.
	hash := uint32(2166136261)
	for i := range ip {
		hash = (hash ^ uint32(ip[i])) * 16777619
	}
	return &l.shards[hash&(shardCount-1)]
}

// Allow 检查并记录一次访问；返回 false 表示超限。
func (l *Limiter) Allow(r *http.Request) bool {
	limit, ok := limits[r.URL.Path]
	if !ok || r.Method != http.MethodPost {
		return true
	}
	ip := l.clientIP(r)
	key := limitKey{ip, r.URL.Path}
	shard := l.shard(ip)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	// Take the timestamp under the lock so accepted hits remain chronological.
	now := time.Now()
	hits := shard.hits[key]
	if hits == nil {
		if len(shard.hits) >= shard.maxKeys || shard.slots >= shard.maxSlots {
			// Evict expired quotas only. Evicting active clients would allow a
			// rotating-source attacker to reset quotas. Scan at most 16 entries.
			checked := 0
			for k, old := range shard.hits {
				if expired(old, now) {
					shard.slots -= len(old.times)
					delete(shard.hits, k)
				}
				checked++
				if checked == 16 {
					break
				}
			}
			if len(shard.hits) >= shard.maxKeys || shard.slots >= shard.maxSlots {
				shard.capacityDenied++
				return false
			}
		}
		hits = &hitWindow{limit: limit}
		// Do not retain a URL substring backed by an arbitrarily large query.
		key.path = strings.Clone(key.path)
		shard.hits[key] = hits
	}
	before := len(hits.times)
	allowed := hits.allowBudget(now, shard.maxSlots-shard.slots)
	shard.slots += len(hits.times) - before
	shard.highKeys = max(shard.highKeys, len(shard.hits))
	shard.highSlots = max(shard.highSlots, shard.slots)
	if !allowed {
		if hits.size < hits.limit {
			shard.capacityDenied++
		} else {
			shard.quotaDenied++
		}
	}
	return allowed
}

// Middleware 超限返回 429 + Retry-After。
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(r) {
			w.Header().Set("Retry-After", "600")
			response.JSON(w, http.StatusTooManyRequests, map[string]any{
				"message": "尝试次数较多，请稍后再试",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *Limiter) janitor() {
	defer close(l.done)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			l.sweep(time.Now())
		}
	}
}

func (l *Limiter) sweep(now time.Time) {
	for i := range l.shards {
		shard := &l.shards[i]
		shard.mu.Lock()
		for key, hits := range shard.hits {
			if expired(hits, now) {
				shard.slots = max(0, shard.slots-len(hits.times))
				delete(shard.hits, key)
			}
		}
		shard.mu.Unlock()
	}
}

func expired(h *hitWindow, now time.Time) bool {
	return h.size == 0 || now.Sub(h.times[(h.head+h.size-1)%len(h.times)]) >= window
}

// WriteMetrics snapshots bounded counters at scrape time. Request admission only
// updates its shard, avoiding the metrics registry's global lock on every hit.
func (l *Limiter) WriteMetrics(w io.Writer) {
	var keys, slots, keyCap, slotCap, highKeys, highSlots int
	var capacityDenied, quotaDenied uint64
	for i := range l.shards {
		s := &l.shards[i]
		s.mu.Lock()
		keys += len(s.hits)
		slots += s.slots
		keyCap += s.maxKeys
		slotCap += s.maxSlots
		highKeys += s.highKeys
		highSlots += s.highSlots
		capacityDenied += s.capacityDenied
		quotaDenied += s.quotaDenied
		s.mu.Unlock()
	}
	fmt.Fprintf(w, "yihe_ratelimit_active_keys %d\nyihe_ratelimit_timestamp_slots %d\nyihe_ratelimit_key_cap %d\nyihe_ratelimit_slot_cap %d\nyihe_ratelimit_key_high_water %d\nyihe_ratelimit_slot_high_water %d\nyihe_ratelimit_capacity_rejected_total %d\nyihe_ratelimit_quota_rejected_total %d\n", keys, slots, keyCap, slotCap, highKeys, highSlots, capacityDenied, quotaDenied)
}
