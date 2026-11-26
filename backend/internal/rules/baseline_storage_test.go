package rules

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/health"
	"yihe-care/internal/securestore"
)

func TestProtectedBaselineMatchesLegacyMySQL(t *testing.T) {
	database, now := seed(t)
	ctx := context.Background()
	box, err := securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("H", 32))))
	if err != nil {
		t.Fatal(err)
	}
	device := ruleDevice{id: 1, oldID: 1, epoch: 1}
	for _, sourceDevice := range []string{"RULE001", strings.Repeat("d", 65)} {
		t.Run(sourceDevice[:7], func(t *testing.T) {
			owner := health.Owner{OldID: device.oldID, DeviceID: &device.id, Epoch: device.epoch}
			write := func(secret *securestore.Box, samples []health.Sample) {
				t.Helper()
				batch, err := health.PrepareBatch(samples)
				if err != nil {
					t.Fatal(err)
				}
				tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, _, err = batch.SaveProtected(ctx, tx, owner, secret); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			newSample := func(id string, observed time.Time, value float64) health.Sample {
				return health.Sample{RecordID: id, Source: "watch", SourceDevice: sourceDevice, Metric: "heart_rate",
					Unit: "count/min", Semantics: "instant", Value: health.Number(value), Start: observed, End: observed, Version: 1}
			}
			var samples []health.Sample
			for i, value := range []float64{60, 70, 80, 84, 92, 75} {
				samples = append(samples, newSample(fmt.Sprint(i), now.Add(-time.Duration(i+1)*time.Hour), value))
			}
			tooRecent := newSample("cutoff", now.Add(-15*time.Minute), 120)
			otherSource := newSample("other", now.Add(-time.Hour), 130)
			otherSource.Source = "WATCH"
			unzoned := newSample("unzoned", now.Add(-time.Hour), 140)
			unzoned.TimeBasis = "legacy-unzoned"
			write(nil, append(samples, tooRecent, otherSource, unzoned))
			read := func(reader *securestore.Box) (Baseline, error) {
				t.Helper()
				tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				return readRuleBaseline(ctx, tx, device, "heart_rate", Value{Source: "watch", Device: sourceDevice}, now, 7, reader)
			}
			legacy, err := read(box)
			if err != nil || legacy.Count != len(samples) {
				t.Fatalf("legacy baseline: %+v %v", legacy, err)
			}
			for i := range samples {
				samples[i].Version = 2
			}
			tooRecent.Version, otherSource.Version, unzoned.Version = 2, 2, 2
			write(box, append(samples[:3:3], tooRecent, otherSource, unzoned))
			mixed, err := read(box)
			if err != nil || mixed.Count != legacy.Count || math.Abs(mixed.Mean-legacy.Mean) > 1e-9 ||
				math.Abs(mixed.Deviation-legacy.Deviation) > 1e-9 {
				t.Fatalf("mixed-format baseline changed: legacy=%+v mixed=%+v err=%v", legacy, mixed, err)
			}
			if _, err := read(nil); err == nil {
				t.Fatal("missing reader ignored eligible encrypted samples")
			}
			write(box, samples[3:])
			encrypted, err := read(box)
			if err != nil || encrypted.Count != legacy.Count || math.Abs(encrypted.Mean-legacy.Mean) > 1e-9 ||
				math.Abs(encrypted.Deviation-legacy.Deviation) > 1e-9 {
				t.Fatalf("all-encrypted baseline changed: legacy=%+v encrypted=%+v err=%v", legacy, encrypted, err)
			}
		})
	}
}
