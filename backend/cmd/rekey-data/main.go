// Command rekey-data scans or CAS-rewraps existing encrypted MySQL columns.
// It never decrypts to disk, migrates plaintext or authorizes key retirement.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/rekey"
	"yihe-care/internal/securestore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	apply := flag.Bool("apply", false, "rewrap existing ciphertext after a clean authentication preflight")
	previousFile := flag.String("previous-key-file", "", "private previous DATA_ENCRYPTION_KEY file; lookup key must stay unchanged")
	batch := flag.Int("batch-size", 100, "1..1000 rows per page; pages also have a byte budget")
	maxRows := flag.Int64("max-rows-per-column", 1000000, "stop with an incomplete result if this bound is exceeded")
	pause := flag.Duration("pause", 20*time.Millisecond, "pause between pages, 0..1m")
	timeout := flag.Duration("timeout", 15*time.Minute, "overall operation deadline, at most 24h")
	flag.Parse()
	if flag.NArg() != 0 || *previousFile == "" || *timeout <= 0 || *timeout > 24*time.Hour {
		return errors.New("provide --previous-key-file and a bounded timeout; unexpected arguments are rejected")
	}
	prior, err := os.ReadFile(*previousFile)
	if err != nil {
		return errors.New("cannot read previous key file")
	}
	previous, err := securestore.New(strings.TrimSpace(string(prior)))
	if err != nil {
		return errors.New("invalid previous key configuration")
	}
	current, err := securestore.New(config.Secret("DATA_ENCRYPTION_KEY"))
	if err != nil {
		return errors.New("invalid current key configuration")
	}
	if current.ActiveKeyID() == "" || !current.SameLookupKey(previous) {
		return errors.New("current keyring must have an active ID and preserve the previous lookup key")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pool, err := db.OpenMySQL(ctx, config.Secret("MYSQL_DSN"))
	if err != nil {
		return errors.New("cannot open explicitly configured MySQL")
	}
	defer pool.Close()
	report, operationErr := rekey.Run(ctx, pool.DB, current, previous, rekey.Options{Apply: *apply, BatchSize: *batch, MaxRowsPerColumn: *maxRows, Pause: *pause})
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return errors.New("cannot write rekey aggregate report")
	}
	return operationErr
}
