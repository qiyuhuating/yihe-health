// Package rules evaluates operational care signals. It does not diagnose disease.
package rules

import (
	"fmt"
	"sync"
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
	t := at.In(loc)
	s := t.Hour()*3600 + t.Minute()*60 + t.Second()
	if p.NightBegin < p.NightEnd {
		return s >= p.NightBegin && s < p.NightEnd
	}
	return s >= p.NightBegin || s < p.NightEnd
}

// Locations are immutable. Bound the cache so policy timezone strings cannot
// grow process memory; failed lookups are never retained.
var locations = struct {
	sync.RWMutex
	values map[string]*time.Location
	keys   [64]string
	next   int
}{values: make(map[string]*time.Location)}

func ruleLocation(name string) (*time.Location, error) {
	locations.RLock()
	loc := locations.values[name]
	locations.RUnlock()
	if loc != nil {
		return loc, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	locations.Lock()
	if cached := locations.values[name]; cached != nil {
		loc = cached
	} else {
		delete(locations.values, locations.keys[locations.next])
		locations.values[name] = loc
		locations.keys[locations.next] = name
		locations.next = (locations.next + 1) % len(locations.keys)
	}
	locations.Unlock()
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
}
