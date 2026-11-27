// Package rules evaluates operational care signals. It does not diagnose disease.
package rules

import (
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata"
)

type Policy struct {
	Enabled         bool    `json:"enabled"`
	HRLow           float64 `json:"hrLow"`
	HRHigh          float64 `json:"hrHigh"`
	BPLow           float64 `json:"bpLow"`
	BPHigh          float64 `json:"bpHigh"`
	BatteryLow      float64 `json:"batteryLow"`
	OfflineSeconds  int     `json:"offlineSeconds"`
	Consecutive     int     `json:"consecutive"`
	Timezone        string  `json:"timezone"`
	NightBegin      int     `json:"nightBegin"`
	NightEnd        int     `json:"nightEnd"`
	NightSteps      float64 `json:"nightSteps"`
	ActivityMinutes int     `json:"activityMinutes"`
	BaselineDays    int     `json:"baselineDays"`
	BaselineMinimum int     `json:"baselineMinimum"`
	BaselinePercent float64 `json:"baselinePercent"`
	BaselineSigma   float64 `json:"baselineSigma"`
}

func Default() Policy {
	return Policy{true, 55, 110, 90, 140, 20, 300, 3, "Asia/Shanghai", 22 * 3600, 6 * 3600, 200, 15, 7, 20, 25, 3}
}
func (p Policy) Validate() error {
	if p.HRLow <= 0 || p.HRHigh <= p.HRLow || p.BPLow <= 0 || p.BPHigh <= p.BPLow || p.BatteryLow < 0 || p.BatteryLow > 100 || p.OfflineSeconds < 60 || p.OfflineSeconds > 86400 || p.Consecutive < 1 || p.Consecutive > 20 || p.NightBegin < 0 || p.NightBegin >= 86400 || p.NightEnd < 0 || p.NightEnd >= 86400 || p.NightBegin == p.NightEnd || p.NightSteps <= 0 || p.ActivityMinutes < 1 || p.ActivityMinutes > 120 || p.BaselineDays < 1 || p.BaselineDays > 30 || p.BaselineMinimum < 5 || p.BaselineMinimum > 100000 || p.BaselinePercent <= 0 || p.BaselineSigma <= 0 {
		return fmt.Errorf("规则阈值、窗口或连续次数无效")
	}
	if _, err := ruleLocation(p.Timezone); err != nil || p.Timezone == "" || p.Timezone == "Local" {
		return fmt.Errorf("须明确规则时区")
	}
	return nil
}
func (p Policy) Night(at time.Time) bool {
	loc, err := ruleLocation(p.Timezone)
	if err != nil {
		return false
	}
	hour, minute, second := at.In(loc).Clock()
	s := hour*3600 + minute*60 + second
	if p.NightBegin < p.NightEnd {
		return s >= p.NightBegin && s < p.NightEnd
	}
	return s >= p.NightBegin || s < p.NightEnd
}

// Locations are immutable. Hot lookups share a read-only bounded snapshot.
// Cache misses serialize publication only; failed lookups are not retained.
type locationCache struct {
	values atomic.Pointer[map[string]*time.Location]
	mu     sync.Mutex
	keys   [64]string
	next   int
}

var locations locationCache

func ruleLocation(name string) (*time.Location, error) {
	return locations.get(name)
}

func (c *locationCache) get(name string) (*time.Location, error) {
	if snapshot := c.values.Load(); snapshot != nil {
		if loc := (*snapshot)[name]; loc != nil {
			return loc, nil
		}
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var next map[string]*time.Location
	if snapshot := c.values.Load(); snapshot != nil {
		if cached := (*snapshot)[name]; cached != nil {
			return cached, nil
		}
		next = maps.Clone(*snapshot)
	} else {
		next = make(map[string]*time.Location)
	}
	if len(next) == len(c.keys) {
		delete(next, c.keys[c.next])
	}
	next[name] = loc
	c.keys[c.next] = name
	c.next = (c.next + 1) % len(c.keys)
	c.values.Store(&next)
	return loc, nil
}

type Value struct {
	Value               float64
	Key, Source, Device string
	At                  time.Time
}
type Baseline struct {
	Count           int
	Mean, Deviation float64
}
type Input struct {
	Now, LastReport                 time.Time
	Heart, Blood, Battery, Activity *Value
	Baselines                       map[string]Baseline
	// A latest steps observation may temporarily have an invalid rolling
	// window; it still needs time-driven re-evaluation while night is active.
	activityObserved bool
}
