// Command protect-notes converts legacy care notes in bounded offline batches.
// Stop writers and take a verified backup before --apply or --decrypt.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

type noteTarget struct{ table, column string }

var noteTargets = []noteTarget{
	{"care_incident", "resolution_note"},
	{"care_incident_event", "note"},
	{"alert_review", "reason"},
}

type noteRow struct {
	id    int64
	value string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("protect-notes", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	apply := flags.Bool("apply", false, "apply after stopping writers and taking a tested backup")
	decrypt := flags.Bool("decrypt", false, "restore plaintext for an explicitly approved rollback")
	limit := flags.Int("limit", 100, "maximum total note rows to inspect or convert, 1..1000")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return nil
		}
		return errors.New("invalid protect-notes arguments; use --help")
	}
	if flags.NArg() != 0 || *limit < 1 || *limit > 1000 {
		return errors.New("provide a limit between 1 and 1000 without extra arguments")
	}
	cfg := config.Load()
	box, err := securestore.New(cfg.DataKey)
	if err != nil {
		return errors.New("valid DATA_ENCRYPTION_KEY required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := db.OpenMySQL(ctx, cfg.MySQLDSN)
	if err != nil {
		return errors.New("cannot open explicitly configured MySQL")
	}
	defer pool.Close()
	return convertNotes(ctx, pool.DB, box, *limit, *apply, *decrypt, output)
}

// convertNotes never holds a cursor-wide FOR UPDATE lock. SELECT is read-only,
// and each bounded transaction verifies the exact original bytes for every row.
// A failed or interrupted command is resumable, with a maximum of limit rows
// read into bounded 64-row batches in a single invocation.
func convertNotes(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply, decrypt bool, out io.Writer) error {
	budget := limit
	for _, target := range noteTargets {
		candidates, applied := 0, 0
		var lastID int64
		op := "NOT LIKE"
		if decrypt {
			op = "LIKE"
		}
		// Table and column names come only from noteTargets above.
		selectSQL := fmt.Sprintf("SELECT id,%s FROM %s WHERE id>? AND %s IS NOT NULL AND %s %s ? ORDER BY id LIMIT ?",
			target.column, target.table, target.column, target.column, op)
		updateSQL := fmt.Sprintf("UPDATE %s SET %s=? WHERE id=? AND BINARY %s = BINARY ?",
			target.table, target.column, target.column)
		for budget > 0 {
			batchSize := budget
			if batchSize > 64 {
				batchSize = 64
			}
			rows, err := database.QueryContext(ctx, selectSQL, lastID, securestore.Prefix+"%", batchSize)
			if err != nil {
				return fmt.Errorf("%s read failed: %w", target.table, err)
			}
			items := make([]noteRow, 0, batchSize)
			for rows.Next() {
				var row noteRow
				if err = rows.Scan(&row.id, &row.value); err != nil {
					break
				}
				items = append(items, row)
			}
			if err == nil {
				err = rows.Err()
			}
			if closeErr := rows.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				return fmt.Errorf("%s scan failed: %w", target.table, err)
			}
			if len(items) == 0 {
				break
			}
			lastID = items[len(items)-1].id
			candidates += len(items)
			budget -= len(items)
			if apply {
				// Commit at most 64 rows together. A changed value rolls back the
				// whole chunk, while earlier chunks remain safe to resume.
				tx, beginErr := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if beginErr != nil {
					return fmt.Errorf("%s note transaction failed: %w", target.table, beginErr)
				}
				batchErr := func() error {
					defer tx.Rollback()
					for _, item := range items {
						var text string
						if decrypt {
							text, err = box.Open(item.value, "care-text")
						} else {
							text, err = box.Seal(item.value, "care-text")
						}
						if err != nil {
							return fmt.Errorf("%s note conversion failed: %w", target.table, err)
						}
						result, err := tx.ExecContext(ctx, updateSQL, text, item.id, item.value)
						if err != nil {
							return fmt.Errorf("%s note update failed: %w", target.table, err)
						}
						changed, err := result.RowsAffected()
						if err != nil || changed != 1 {
							return fmt.Errorf("%s note changed during migration; verify writers are stopped", target.table)
						}
					}
					return tx.Commit()
				}()
				if batchErr != nil {
					return batchErr
				}
				applied += len(items)
			}
			if len(items) < batchSize {
				break
			}
		}
		if _, err := fmt.Fprintf(out, "%s.%s candidates=%d applied=%d decrypt=%t capped=%t\n",
			target.table, target.column, candidates, applied, decrypt, budget == 0); err != nil {
			return err
		}
	}
	return ctx.Err()
}
