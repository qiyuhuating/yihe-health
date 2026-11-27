// Command protect-health-projections migrates persisted health copies.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/health"
	"yihe-care/internal/securestore"
	"yihe-care/internal/vitalrepo"
)

func main() {
	apply := flag.Bool("apply", false, "apply after stopping projection writers and verifying the backup and reader inventory")
	limit := flag.Int("limit", 100, "maximum total rows per run, 1..1000")
	samples := flag.Bool("samples", false, "migrate original samples")
	vitals := flag.Bool("legacy-vitals", false, "migrate legacy heart rate and blood pressure")
	deduplicate := flag.Bool("deduplicate", false, "authenticate matching current/history copies and replace current ciphertext with references")
	materialize := flag.Bool("materialize", false, "authenticate and reseal current references for an offline downgrade")
	flag.Parse()
	if err := run(*limit, *apply, *samples, *vitals, *deduplicate, *materialize); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(limit int, apply, samples, vitals, deduplicate, materialize bool) error {
	modes := 0
	for _, selected := range []bool{samples, vitals, deduplicate, materialize} {
		if selected {
			modes++
		}
	}
	if modes > 1 {
		return errors.New("select at most one maintenance operation")
	}
	if flag.NArg() != 0 || limit < 1 || limit > 1000 {
		return errors.New("provide a limit between 1 and 1000 without extra arguments")
	}
	box, err := securestore.New(config.Secret("DATA_ENCRYPTION_KEY"))
	if err != nil {
		return errors.New("valid service encryption key required for health projection inventory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := db.OpenMySQL(ctx, config.Secret("MYSQL_DSN"))
	if err != nil {
		return errors.New("cannot open explicitly configured MySQL")
	}
	defer pool.Close()
	backfill := health.BackfillProjections
	if samples {
		backfill = health.BackfillHealthSamples
	}
	var columns []health.ProjectionBackfillReport
	var operationErr error
	if deduplicate || materialize {
		columns, operationErr = health.RewriteHistoryReferences(ctx, pool.DB, box, limit, apply, materialize)
	} else if vitals {
		var measurements vitalrepo.MeasurementBackfillReport
		measurements, operationErr = vitalrepo.BackfillMeasurements(ctx, pool.DB, box, limit, apply)
		columns = []health.ProjectionBackfillReport{{Table: "ratepre", Before: measurements.Before,
			Migrated: measurements.Migrated, Remaining: measurements.Remaining}}
	} else {
		columns, operationErr = backfill(ctx, pool.DB, box, limit, apply)
	}
	mode := "scan"
	if apply {
		mode = "apply"
	}
	report := struct {
		Mode   string                            `json:"mode"`
		Tables []health.ProjectionBackfillReport `json:"tables"`
	}{mode, columns}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return errors.New("cannot write health projection aggregate report")
	}
	return operationErr
}
