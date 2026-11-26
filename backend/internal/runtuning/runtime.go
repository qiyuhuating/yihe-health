// Package runtuning offers conservative opt-in tuning for Go 1.23 services
// running under Linux cgroup v2 limits. It never controls OS CPU affinity,
// thread pinning, the kernel scheduler, or the database connection pool.
package runtuning

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"yihe-care/internal/config"
)

// Decision describes settings modified during a one-time initialization.
// Zero values mean the corresponding Go runtime setting was left unchanged.
type Decision struct {
	Parallelism   int
	GoMemoryLimit int64
}

// ApplyOptIn checks an explicit opt-in to avoid altering desktop/host behavior.
// Respect GOMAXPROCS and GOMEMLIMIT when administrators have set them.
func ApplyOptIn() Decision {
	if !config.Enabled("YIHE_RUNTIME_AUTOTUNE") || runtime.GOOS != "linux" {
		return Decision{}
	}
	return apply("/sys/fs/cgroup", os.Getenv, runtime.NumCPU(), runtime.GOMAXPROCS, debug.SetMemoryLimit)
}

// cgroup v2 CPU quota may be fractional; at least one P is required.
// ceil(quota/period) is an upper bound, not a throughput promise.
func cpuParallelism(text string, online int) int {
	f := strings.Fields(text)
	if len(f) != 2 || f[0] == "max" || online < 1 {
		return 0
	}
	quota, err1 := strconv.ParseUint(f[0], 10, 64)
	period, err2 := strconv.ParseUint(f[1], 10, 64)
	if err1 != nil || err2 != nil || period == 0 || quota == 0 {
		return 0
	}
	// Compute ceil using division/remainder to avoid integer overflow.
	count := quota / period
	if quota%period != 0 {
		count++
	}
	if count < 1 {
		count = 1
	}
	if count >= uint64(online) {
		return 0
	}
	return int(count)
}

// memory.max is a hard cgroup limit whereas GOMEMLIMIT is a *soft* target
// for the Go heap and runtime. Reserve 30% for stacks, native allocations,
// mapped files, kernel overhead and co-located processes; even that is not
// a guaranteed prevention of OOM. Skip very small containers.
func goMemoryBudget(text string) int64 {
	text = strings.TrimSpace(text)
	if text == "max" || text == "" {
		return 0
	}
	limit, err := strconv.ParseInt(text, 10, 64)
	if err != nil || limit < 256<<20 {
		return 0
	}
	return (limit/10)*7 + (limit%10)*7/10 // avoid overflow; floor(0.7 * limit)
}

func apply(root string, getenv func(string) string, online int, setCPU func(int) int, setMemory func(int64) int64) Decision {
	var decision Decision
	if getenv("GOMAXPROCS") == "" {
		if content, err := os.ReadFile(filepath.Join(root, "cpu.max")); err == nil {
			n := cpuParallelism(string(content), online)
			if n > 0 {
				setCPU(n)
				decision.Parallelism = n
			}
		}
	}
	if getenv("GOMEMLIMIT") == "" {
		if content, err := os.ReadFile(filepath.Join(root, "memory.max")); err == nil {
			n := goMemoryBudget(string(content))
			if n > 0 {
				setMemory(n)
				decision.GoMemoryLimit = n
			}
		}
	}
	return decision
}
