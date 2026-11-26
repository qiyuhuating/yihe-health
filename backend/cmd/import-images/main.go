// Command import-images migrates local or exported S3 images into encrypted MySQL.
// Stop all previous writers and preserve independent backups before --apply.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"yihe-care/internal/blob"
	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	source := flag.String("source", "", "existing directory containing image objects or an offline S3 export")
	apply := flag.Bool("apply", false, "commit MySQL migration; default checks the source only")
	flag.Parse()
	stat, err := os.Stat(*source)
	if err != nil || !stat.IsDir() {
		return errors.New("an existing image source directory is required")
	}
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if *apply && cfg.MySQLDSN == "" {
		return errors.New("MYSQL_DSN is required")
	}
	box, err := securestore.New(cfg.DataKey)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var pool *db.Database
	if *apply {
		pool, err = db.OpenMySQL(ctx, cfg.MySQLDSN)
		if err != nil {
			return err
		}
		defer pool.Close()
		if err := db.MigrateMySQL(ctx, pool); err != nil {
			return err
		}
	}
	local, err := blob.NewLocal(*source)
	if err != nil {
		return err
	}
	legacy := &blob.Encrypted{Store: local, Box: box, AllowLegacy: true}
	inspected, imported := 0, 0
	err = local.Walk(ctx, func(entry blob.Entry) error {
		if !blob.ValidKey(entry.Key) {
			return blob.ErrIntegrity
		}
		inspected++
		if !*apply {
			if _, err := legacy.Get(ctx, entry.Key); err != nil {
				return fmt.Errorf("source %q: %w", entry.Key, err)
			}
			return nil
		}
		raw, err := local.Get(ctx, entry.Key)
		if err != nil {
			return fmt.Errorf("source %q: %w", entry.Key, err)
		}
		changed, err := blob.ImportLegacy(ctx, pool.DB, box, entry.Key, raw)
		if err != nil {
			return fmt.Errorf("image %q: %w", entry.Key, err)
		}
		if changed {
			imported++
		}
		return nil
	})
	if err != nil {
		return err
	}
	var missing *int64
	if *apply {
		count, err := blob.MissingMySQLReferences(ctx, pool.DB)
		if err != nil {
			return err
		}
		missing = &count
	}
	if *apply && *missing != 0 {
		return fmt.Errorf("migration incomplete: %d referenced images missing; do not delete old storage", *missing)
	}
	if *apply {
		store, err := blob.NewMySQL(pool.DB)
		if err != nil {
			return err
		}
		manager := &blob.Manager{DB: pool, Store: &blob.Encrypted{Store: store, Box: box}}
		if _, err := manager.VerifyReadyCatalog(ctx); err != nil {
			return fmt.Errorf("MySQL image catalog integrity verification: %w", err)
		}
		if _, err := manager.VerifyReferences(ctx); err != nil {
			return fmt.Errorf("image reference verification: %w", err)
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"dry_run": !*apply, "inspected": inspected, "imported": imported,
		"missing_references": missing,
	})
}
