package handlers

import (
	"net/http"
	"runtime"
	"sync"
	"time"

	"yihe-care/internal/observability"
	"yihe-care/internal/response"
)

// Process-wide authentication budgets survive handler/database replacement.
// Do not queue expensive authentication work on a saturated CPU.
var loginAdmission = sync.OnceValue(func() chan struct{} {
	return make(chan struct{}, min(8, max(1, runtime.GOMAXPROCS(0)/2)))
})

func tryAdmission(slots chan struct{}) (func(), bool) {
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		return nil, false
	}
}

type bootstrapBudget struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

const bootstrapBurst = 64
const bootstrapRate = 10 // new anonymous sessions per second, per process

var anonymousSessions = bootstrapBudget{tokens: bootstrapBurst}
var bootstrapSlots = make(chan struct{}, 4)

func (b *bootstrapBudget) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.last.IsZero() && now.After(b.last) {
		b.tokens = min(bootstrapBurst, b.tokens+now.Sub(b.last).Seconds()*bootstrapRate)
	}
	if b.last.IsZero() || now.After(b.last) {
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func authenticationBusy(w http.ResponseWriter, metric string) {
	observability.Default.Add(metric, 1)
	w.Header().Set("Retry-After", "1")
	response.JSON(w, http.StatusTooManyRequests, map[string]any{"message": "认证服务繁忙，请稍后重试"})
}
