package health

import (
	"math"
	"time"
)

// dailySeries consumes decoded rows in revision order. Scalar series retain
// only their values for the rare exact overflow fallback, rather than each
// Sample and its variable-length strings, timestamps and metadata. Continue
// decoding every SQL candidate even after validation fails, preserving the
// error precedence of the former materialized RefreshDaily.
type dailySeries struct {
	first            Sample
	count            int
	scalar           bool
	total, low, high float64
	values           []float64
	samples          []Sample
	err              error
}

func (a *dailySeries) add(s Sample) {
	if s.Deleted || s.Value == nil || a.err != nil {
		return
	}
	if a.count == 0 {
		a.first = s
		a.first.Metadata = nil
		a.scalar = s.Semantics != "daily_total" && s.Metric != "sleep_stage" && !(s.Metric == "steps" && s.Semantics == "interval")
		a.low, a.high = *s.Value, *s.Value
	}
	if !a.scalar {
		a.samples = append(a.samples, s)
		a.count++
		return
	}
	if err := validateSeriesSample(&a.first, &s); err != nil {
		a.err = err
		return
	}
	v := *s.Value
	a.total += v
	if v < a.low {
		a.low = v
	}
	if v > a.high {
		a.high = v
	}
	a.count++
	// Only instant means have a rescaling fallback.
	if a.first.Semantics == "instant" {
		a.values = append(a.values, v)
	}
}

func (a *dailySeries) result(from, to time.Time) (Statistics, error) {
	if a.err != nil {
		return Statistics{}, a.err
	}
	if a.count == 0 {
		return Statistics{}, nil
	}
	if !a.scalar {
		return Aggregate(a.samples, from, to)
	}
	out := Statistics{Count: a.count, Unit: a.first.Unit, Min: Number(a.low), Max: Number(a.high)}
	if a.first.Semantics == "instant" {
		out.Mean = Number(a.total / float64(a.count))
		if math.IsInf(a.total, 1) && !math.IsInf(a.high, 0) && a.low >= 0 {
			scaled := 0.0
			for _, v := range a.values {
				scaled += v / a.high
			}
			out.Mean = Number((scaled / float64(a.count)) * a.high)
		}
	} else {
		out.Total = Number(a.total)
	}
	return out, nil
}
