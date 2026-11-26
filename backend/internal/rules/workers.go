package rules

import (
	"strconv"
	"strings"
)

// deviceWorkers limits the number of simultaneous device transactions per
// application replica. Values outside [1,8] deliberately fall back to the
// previous default, avoiding a zero-worker or unbounded deployment.
func deviceWorkers(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 8
	}
	workers, err := strconv.Atoi(value)
	if err != nil || workers < 1 || workers > 8 {
		return 8
	}
	return workers
}
