// Command protect-user-phones migrates legacy administrator phones in bounded,
// restartable transactions. Stop account writers and take a tested backup first.
package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
	"yihe-care/internal/config"

	_ "github.com/go-sql-driver/mysql"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

func main() {
	apply := flag.Bool("apply", false, "apply encrypted phone backfill")
	limit := flag.Int("limit", 100, "maximum accounts to migrate per invocation (1..1000)")
	flag.Parse()
	if *limit < 1 || *limit > 1000 {
		fmt.Fprintln(os.Stderr, "limit must be between 1 and 1000")
		os.Exit(2)
	}
	dsn := config.Secret("MYSQL_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "MYSQL_DSN is required")
		os.Exit(2)
	}
	database, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err = database.PingContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var pending int
	if err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM users
 WHERE user_phone_protected=false AND user_phone IS NOT NULL`).Scan(&pending); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !*apply {
		fmt.Printf("pending=%d (dry-run, no writes)\n", pending)
		return
	}
	box, err := securestore.New(config.Secret("DATA_ENCRYPTION_KEY"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "valid DATA_ENCRYPTION_KEY required")
		os.Exit(1)
	}
	migrated := 0
	for migrated < *limit {
		changed, err := protectOne(ctx, database, box)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if !changed {
			break
		}
		migrated++
	}
	fmt.Printf("migrated=%d pending_before=%d\n", migrated, pending)
}

func protectOne(ctx context.Context, database *sql.DB, box *securestore.Box) (bool, error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id int64
	var phone string
	err = tx.QueryRowContext(ctx, `SELECT user_id,user_phone FROM users
 WHERE user_phone_protected=false AND user_phone IS NOT NULL
 ORDER BY user_id LIMIT 1 FOR UPDATE`).Scan(&id, &phone)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	protected, err := userphone.Protect(box, id, phone)
	if err != nil {
		return false, errors.New("legacy administrator phone requires correction before backfill")
	}
	fingerprint, err := userphone.KeyBinding(box)
	if err != nil {
		return false, err
	}
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT lookup_verifier FROM account_phone_key_state
 WHERE singleton=1 FOR UPDATE`).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		var existing bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE user_phone_protected=true)`).Scan(&existing); err != nil {
			return false, err
		}
		if existing {
			return false, errors.New("protected rows exist without a registered lookup key")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO account_phone_key_state(singleton,lookup_verifier)
 VALUES(1,?)`, fingerprint); err != nil {
			return false, err
		}
	} else if err != nil {
		return false, err
	} else if subtle.ConstantTimeCompare([]byte(stored), []byte(fingerprint)) != 1 {
		return false, errors.New("administrator phone lookup key mismatch")
	}
	result, err := tx.ExecContext(ctx, `UPDATE users SET user_phone=NULL,
 user_phone_cipher=?,user_phone_lookup=?,user_phone_protected=true
 WHERE user_id=? AND user_phone=? AND user_phone_protected=false`,
		protected.Ciphertext, protected.Lookup, id, phone)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, errors.New("administrator phone row changed during backfill")
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
