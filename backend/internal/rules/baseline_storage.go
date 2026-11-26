package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"time"

	"yihe-care/internal/health"
	"yihe-care/internal/securestore"
)

const baselineLegacyValue = "CAST(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.value')) AS DOUBLE)"

const baselineLegacyLongSQL = `SELECT count(*) AS sample_count,count(` + baselineLegacyValue + `) AS value_count,
 COALESCE(avg(` + baselineLegacyValue + `),0) AS mean,COALESCE(stddev_pop(` + baselineLegacyValue + `),0) AS deviation
 FROM health_sample WHERE old_id=? AND device_id=? AND metric=? AND deleted=false AND payload_encrypted=false
 AND source=? AND source_device=? AND BINARY source=BINARY ? AND BINARY source_device=BINARY ?
 AND sample_start>=? AND sample_start<? AND (JSON_UNQUOTE(JSON_EXTRACT(payload,'$.timeBasis')) IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(payload,'$.timeBasis'))<>'legacy-unzoned')`

const baselineProtectedShortSQL = `SELECT sample_id,revision,payload_cipher,0,0,0,0 FROM health_sample FORCE INDEX (idx_health_rule_cover)
 WHERE old_id=? AND device_id=? AND metric=? AND deleted=false AND payload_encrypted=true
 AND source=? AND rule_source_device=? AND BINARY source=BINARY ? AND BINARY rule_source_device=BINARY ?
 AND sample_start>=? AND sample_start<?`

const baselineProtectedLongSQL = `SELECT sample_id,revision,payload_cipher,0,0,0,0 FROM health_sample
 WHERE old_id=? AND device_id=? AND metric=? AND deleted=false AND payload_encrypted=true
 AND source=? AND source_device=? AND BINARY source=BINARY ? AND BINARY source_device=BINARY ?
 AND sample_start>=? AND sample_start<?`

func readRuleBaseline(ctx context.Context, tx *sql.Tx, device ruleDevice, metric string, value Value, now time.Time, days int, box *securestore.Box) (Baseline, error) {
	args := []any{device.oldID, device.id, metric, value.Source, value.Device, value.Source, value.Device,
		now.AddDate(0, 0, -days).UnixMicro(), now.Add(-15 * time.Minute).UnixMicro()}
	legacyQuery, protectedQuery := baselineCoverSQL, baselineProtectedShortSQL
	if len(value.Device) > 64 {
		legacyQuery, protectedQuery = baselineLegacyLongSQL, baselineProtectedLongSQL
	}
	// One statement gives both storage formats the same snapshot, including
	// when a backfill moves a sample from the legacy result to ciphertext.
	query := "SELECT NULL,0,NULL, legacy.* FROM (" + legacyQuery + ") AS legacy UNION ALL " + protectedQuery
	rows, err := tx.QueryContext(ctx, query, append(args, args...)...)
	if err != nil {
		return Baseline{}, err
	}
	defer rows.Close()
	var base Baseline
	var legacy Baseline
	var legacyValues int
	var protected populationMoments
	for rows.Next() {
		var id sql.NullString
		var revision int64
		var count, numeric int
		var mean, deviation float64
		var stored health.ProjectionPayload
		if err := rows.Scan(&id, &revision, &stored.Cipher, &count, &numeric, &mean, &deviation); err != nil {
			return Baseline{}, err
		}
		if !id.Valid {
			legacy, legacyValues = Baseline{Count: count, Mean: mean, Deviation: deviation}, numeric
			continue
		}
		payload, err := stored.OpenSample(box, id.String, device.oldID, revision)
		if err != nil {
			return Baseline{}, err
		}
		var sample struct {
			Value     *float64 `json:"value"`
			TimeBasis string   `json:"timeBasis"`
		}
		if err := json.Unmarshal(payload, &sample); err != nil {
			return Baseline{}, err
		}
		if sample.TimeBasis != "legacy-unzoned" {
			base.Count++
			if sample.Value != nil {
				protected.add(*sample.Value)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return Baseline{}, err
	}
	base.Count += legacy.Count
	base.Mean, base.Deviation = legacy.Mean, legacy.Deviation
	if protected.n != 0 {
		base.Mean, base.Deviation = protected.combine(legacyValues, base.Mean, base.Deviation)
	}
	return base, nil
}

// Use scaled population moments so adding finite observations cannot overflow
// the intermediate sum or squared residual. The original SQL count includes
// eligible rows with missing values, while mean and deviation use numeric values.
type populationMoments struct {
	n                  int
	scale, mean, sumM2 float64
}

func (m *populationMoments) add(value float64) {
	m.n++
	m.rescale(math.Abs(value))
	if m.scale == 0 {
		return
	}
	normalized := value / m.scale
	delta := normalized - m.mean
	m.mean += delta / float64(m.n)
	m.sumM2 += delta * (normalized - m.mean)
}

func (m *populationMoments) rescale(scale float64) {
	if scale > m.scale {
		ratio := m.scale / scale
		m.mean *= ratio
		m.sumM2 *= ratio * ratio
		m.scale = scale
	}
}

func (m populationMoments) combine(legacyN int, mean, deviation float64) (float64, float64) {
	if legacyN > 0 {
		m.rescale(max(math.Abs(mean), deviation))
	}
	if m.scale == 0 {
		return 0, 0
	}
	if legacyN > 0 {
		currentN, oldN := float64(m.n), float64(legacyN)
		total := currentN + oldN
		oldDeviation := deviation / m.scale
		delta := mean/m.scale - m.mean
		m.mean += delta * oldN / total
		m.sumM2 += oldN*oldDeviation*oldDeviation + delta*delta*currentN*oldN/total
		m.n += legacyN
	}
	return m.mean * m.scale, math.Sqrt(max(0, m.sumM2/float64(m.n))) * m.scale
}
